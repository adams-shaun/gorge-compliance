"""release: upload the built SQLite asset and prune the dated weekly releases.

The `carddb` data branch is the source; this module only touches GitHub
releases. It is deliberately a pure planner plus a thin `gh` wrapper:

  * `carddb-latest` is the floating release. Its `carddb.sqlite.zst` asset and
    `carddb-latest.json` metadata are replaced (`gh release upload --clobber`)
    on every run. Extra `--asset`s ride along (the workflow also passes the
    CI-only per-table Parquet archive, `carddb-parquet.tar.gz`).
  * one dated `carddb-YYYY-MM-DD` release per ISO week (the Monday) keeps a
    stable snapshot; rerunning the same week replaces that week's asset. It
    carries the SQLite asset only: the `carddb-latest.json` beside it would
    describe `carddb-latest`, not the weekly release.
  * dated releases older than `--keep-weeks` (12) are pruned, except the newest
    one in each calendar quarter: the quarterly snapshots stay, the weekly
    noise does not.
  * `--dry-run` prints every mutating `gh` command and calls none of them. The
    read-only `gh release view/list` calls still run so the plan is accurate;
    when `gh` is absent the plan simply has no existing releases. A real run
    that cannot list the releases fails loudly instead of skipping the prune.
  * a branch over the `--max-pack-mb` D7 budget is refused before any release
    mutation, so an over-budget branch cannot ship as if it were within budget.

Dates come from `--now` / `CARDDB_NOW`, else the data branch tip's commit date;
the workflow passes the data tip's date. Sorting is by the date in the tag, so
the same releases and the same budget always give the same plan.
"""
import datetime
import hashlib
import json
import os
import re
import subprocess
import sys

from .store import StoreError, gorge12, packed_mib, today

LATEST_TAG = "carddb-latest"
DATED_RE = re.compile(r"^carddb-(\d{4}-\d{2}-\d{2})$")
KEEP_WEEKS = 12
BUDGET_MIB = 300
PUBLIC_REPO = "adams-shaun/gorge-compliance"


def _date(value):
    if isinstance(value, datetime.datetime):
        return value.date()
    if isinstance(value, datetime.date):
        return value
    return datetime.date.fromisoformat(str(value)[:10])


def week_tag(day):
    """The dated release for the ISO week holding day: that week's Monday."""
    day = _date(day)
    return "carddb-" + (day - datetime.timedelta(days=day.weekday())).isoformat()


def plan_prune(tags, now, keep_weeks=KEEP_WEEKS):
    """Dated release tags to delete: older than keep_weeks, except the newest per quarter.

    Only `carddb-YYYY-MM-DD` tags are considered; `carddb-latest` and any other
    name are never candidates. The result is sorted so the delete order (and the
    log) is deterministic."""
    cutoff = _date(now) - datetime.timedelta(weeks=keep_weeks)
    dated = []
    for tag in tags:
        m = DATED_RE.match(tag)
        if m is None:
            continue
        try:
            day = datetime.date.fromisoformat(m.group(1))
        except ValueError:
            continue
        dated.append((day, tag))
    keep, quarter_newest = set(), {}
    for day, tag in dated:
        if day >= cutoff:
            keep.add(tag)
            continue
        quarter = (day.year, (day.month - 1) // 3)
        prev = quarter_newest.get(quarter)
        if prev is None or day > prev[0]:
            quarter_newest[quarter] = (day, tag)
    keep.update(tag for _day, tag in quarter_newest.values())
    return sorted(tag for _day, tag in dated if tag not in keep)


def _gh(gh, args, dry_run=False, capture=False, out=None):
    """Run one gh command. A mutating command in dry-run is printed, never run.

    Read-only commands run in both modes; when gh is missing or fails, return
    None so the caller can plan without it (a dry run on a laptop)."""
    cmd = [gh] + list(args)
    if dry_run and not capture:
        print("[dry-run] " + " ".join(cmd), file=out or sys.stdout)
        return subprocess.CompletedProcess(cmd, 0, "", "")
    try:
        p = subprocess.run(cmd, capture_output=True, text=True)
    except OSError as e:
        if capture:
            return None
        raise StoreError("gh %s: %s" % (" ".join(args[:3]), e))
    if p.returncode != 0:
        if capture:
            return None
        raise StoreError("gh %s: %s" % (" ".join(args[:3]), (p.stderr or p.stdout).strip()))
    return p


def _releases(gh, repo):
    """The release tags gh reports, or None when it could not answer.

    None is distinct from [] (no releases): the caller must not treat a
    failed `gh release list` as "nothing to prune" in a real run."""
    p = _gh(gh, ["release", "list", "--repo", repo, "--limit", "200", "--json", "tagName"],
            capture=True)
    if p is None:
        return None
    try:
        doc = json.loads(p.stdout)
    except ValueError:
        return None
    if not isinstance(doc, list):
        return None
    return sorted(str(r.get("tagName")) for r in doc
                  if isinstance(r, dict) and r.get("tagName"))


def _exists(gh, repo, tag):
    return _gh(gh, ["release", "view", tag, "--repo", repo, "--json", "tagName"],
               capture=True) is not None


def _ensure(gh, repo, tag, title, notes, dry_run, out=None):
    if _exists(gh, repo, tag):
        return False
    _gh(gh, ["release", "create", tag, "--repo", repo, "--title", title, "--notes", notes,
             "--latest=%s" % ("true" if tag == LATEST_TAG else "false")], dry_run=dry_run, out=out)
    return True


def _upload(gh, repo, tag, assets, dry_run, out=None):
    _gh(gh, ["release", "upload", tag] + list(assets) + ["--repo", repo, "--clobber"],
        dry_run=dry_run, out=out)


def _sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def _data_rev(store):
    p = store.git("rev-parse", "HEAD", check=False)
    return p.stdout.strip()[:12] if p.returncode == 0 else "unknown"


def _commit_date(store):
    """The data branch tip's committer date; the deterministic fallback for --now."""
    p = store.git("log", "-1", "--format=%cs", check=False)
    return p.stdout.strip() or None


def write_meta(path, store, asset, now, packed, budget):
    """The JSON that travels beside the asset: what the release was built from."""
    doc = {
        "asset": os.path.basename(asset),
        "budget_mib": int(budget),
        "built_at": _date(now).isoformat(),
        "data_rev": _data_rev(store),
        "gorge_pin": gorge12(),
        "packed_mib": round(packed, 1),
        "sha256": _sha256(asset),
        "tag": LATEST_TAG,
    }
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(doc, f, sort_keys=True)
        f.write("\n")
    os.replace(tmp, path)
    return doc


def run(store, db, assets=None, repo=None, gh=None, now=None, dry_run=False,
        keep_weeks=KEEP_WEEKS, budget_mib=BUDGET_MIB, weekly=True, out=None):
    """Publish assets and the weekly snapshot, then prune old dated releases.

    -> dict(latest=tag, weekly=tag|None, pruned=[tags], packed_mib=float,
            created_latest=bool, created_weekly=bool)."""
    out = out or sys.stdout
    if not os.path.isfile(db):
        raise StoreError("release: no built database at %s" % db)
    if not store.exists():
        raise StoreError("release: no public clone at %s" % store.dir)
    gh = gh or os.environ.get("CARDDB_GH") or "gh"
    repo = repo or os.environ.get("CARDDB_PUBLIC_REPO") or os.environ.get("GITHUB_REPOSITORY") or PUBLIC_REPO
    now = _date(now or os.environ.get("CARDDB_NOW") or _commit_date(store) or today())
    packed = packed_mib(store.dir)
    if packed is None:
        raise StoreError("release: cannot measure the packed size of %s" % store.dir)

    assets = list(assets) if assets else []
    if not assets:
        zst = db + ".zst"
        assets = [zst] if os.path.isfile(zst) else [db]
    for asset in assets:
        if not os.path.isfile(asset):
            raise StoreError("release: no asset at %s" % asset)
    print("carddb release: data branch packed %.1f MiB (budget %d MiB)" % (packed, int(budget_mib)),
          file=out)
    if packed > budget_mib:
        raise StoreError(
            "release: data branch packed %.1f MiB exceeds the D7 budget of %d MiB; "
            "refusing to publish the release" % (packed, int(budget_mib)))
    meta = os.path.join(os.path.dirname(os.path.abspath(db)), "carddb-latest.json")
    write_meta(meta, store, assets[0], now, packed, budget_mib)
    latest_assets = list(assets) + [meta]

    created_latest = _ensure(gh, repo, LATEST_TAG, "carddb latest",
                             "Machine-updated carddb SQLite asset (carddb branch @%s)." % _data_rev(store),
                             dry_run, out)
    _upload(gh, repo, LATEST_TAG, latest_assets, dry_run, out)
    print("carddb release: %s %s (%s%s)" % (
        "planned" if dry_run else ("created and uploaded" if created_latest else "uploaded"),
        LATEST_TAG, ", ".join(os.path.basename(a) for a in latest_assets),
        " [dry-run]" if dry_run else ""), file=out)

    week = None
    created_weekly = False
    if weekly:
        week = week_tag(now)
        created_weekly = _ensure(gh, repo, week, "carddb weekly snapshot %s" % week[7:],
                                 "carddb SQLite snapshot for the week of %s." % week[7:],
                                 dry_run, out)
        # The weekly release carries the asset only. A `carddb-latest.json`
        # there would say "tag": "carddb-latest" about a dated snapshot.
        _upload(gh, repo, week, assets, dry_run, out)
        print("carddb release: %s %s (%s%s)" % (
            "planned" if dry_run else ("created and uploaded" if created_weekly else "uploaded"), week,
            ", ".join(os.path.basename(a) for a in assets),
            " [dry-run]" if dry_run else ""), file=out)

    tags = _releases(gh, repo)
    if tags is None:
        message = ("release: cannot list the existing releases (gh release list failed); "
                   "refusing to skip the prune silently")
        if not dry_run:
            raise StoreError(message)
        print("!! " + message, file=out)
        pruned = []
    else:
        pruned = plan_prune(tags, now, keep_weeks)
    for tag in pruned:
        _gh(gh, ["release", "delete", tag, "--repo", repo, "--yes", "--cleanup-tag"],
            dry_run=dry_run, out=out)
    print("carddb release: pruned %d dated release(s)%s" % (
        len(pruned), (": " + ", ".join(pruned)) if pruned else ""), file=out)
    return {"latest": LATEST_TAG, "weekly": week, "pruned": pruned, "packed_mib": packed,
            "created_latest": created_latest, "created_weekly": created_weekly}


def size_rows(store, now, budget_mib=BUDGET_MIB):
    """The `_meta/size` rows for the branch's object store and the D7 budget."""
    packed = packed_mib(store.dir)
    if packed is None:
        raise StoreError("size: cannot measure the packed size of %s" % store.dir)
    measured = _date(now or os.environ.get("CARDDB_NOW") or _commit_date(store) or today()).isoformat()
    rows = [
        {"metric": "branch_packed_mib", "amount": int(packed + 0.5), "unit": "MiB", "measured": measured},
        {"metric": "budget_mib", "amount": int(budget_mib), "unit": "MiB", "measured": measured},
    ]
    return packed, rows

