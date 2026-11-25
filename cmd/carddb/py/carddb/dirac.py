"""The dirac support writer: gorge's dirac support lists -> support and evidence rows.

gorge's generated dirac/docs/support.md holds counts only; the per-card data it
is generated from is, in the pinned gorge module:

  dirac/support/refused/*.json    a refusal by name (card, class, why)
  dirac/support/evidence/*.txt    one card name per row: clean lockstep games in that
                                  list (a `Name<TAB>seats` row is pod evidence, which
                                  the support rule does not count)

and the lowering, which needs the GPL Forge corpus and so cannot run in CI. The
lowering's result is the committed snapshot oracle/compliance/dirac-gaps.tsv
(`go run ./cmd/diracsupport`): the corpus cards the two lists cannot place --
unverified, not lowered, and supported-but-two-seat -- measured at one gorge pin.
The writer refuses a snapshot whose pin is not GORGE_PIN (StaleSnapshot, raised before any
write); the `dirac` CLI verb reports that as a warning and exits 0 with the last rows kept, so a
GORGE_PIN bump does not stop the carddb publish before the snapshot is regenerated.

A corpus card's status is, first match:
  refused      a refused entry names it (why = the entry's class and why)
  the snapshot's status (not_lowered, unverified, or supported with a two-seat why)
  supported    some evidence list names it (kgen's rule: lowered, not refused, evidenced)
A card no list, entry or snapshot row names is not in gorge's corpus and gets no
row. Names match on norm_name (the front face, like compliance.py), and a split
card's evidence is found under either spelling because norm_name keeps the front face.

Evidence rows: one per (supported card, evidence list), source `dirac-lockstep`, ref the
list's name, outcome `agree`. Rows this writer wrote that a rerun no longer derives
(a card that left the corpus or lost its evidence) are deleted; every other engine's
and source's rows are left alone. For a reproducible `measured` date the caller passes
`ran` or sets CARDDB_NOW (see compliance.py).
"""
import glob
import json
import os
import subprocess

from . import shard
from .norm import norm_name
from .schema import repo_root
from .store import StoreError, today

GORGE_MODULE = "github.com/adams-shaun/gorge"
ENGINE = "dirac"
SOURCE = "dirac-lockstep"
GAPS_REL = os.path.join("oracle", "compliance", "dirac-gaps.tsv")
WHY_MAX = 200
GAP_STATUSES = ("not_lowered", "unverified", "supported")


class StaleSnapshot(StoreError):
    """The gaps snapshot was measured at another gorge pin. GORGE_PIN is bumped by a bot and the
    snapshot needs the GPL corpus to regenerate, so this is expected, not fatal: the CLI keeps the
    last dirac rows and warns, and a publish must not depend on the operator's regeneration."""


def gorge_dir(repo, explicit=None):
    """The pinned gorge module's directory: --gorge-dir, $CARDDB_GORGE_DIR, else
    `go list -m` in the checkout (go.mod pins gorge to GORGE_PIN's commit)."""
    d = explicit or os.environ.get("CARDDB_GORGE_DIR")
    if d:
        return d
    p = subprocess.run(["go", "list", "-m", "-f", "{{.Dir}}", GORGE_MODULE], cwd=repo,
                       capture_output=True, text=True)
    if p.returncode != 0 or not p.stdout.strip():
        raise StoreError("dirac: cannot locate %s (go list -m: %s); pass --gorge-dir"
                         % (GORGE_MODULE, p.stderr.strip() or "no directory"))
    return p.stdout.strip()


def full_pin(repo):
    try:
        with open(os.path.join(repo, "GORGE_PIN"), encoding="utf-8") as f:
            pin = f.read().strip()
    except OSError as e:
        raise StoreError("dirac: %s" % e)
    if not pin:
        raise StoreError("dirac: %s/GORGE_PIN is empty" % repo)
    return pin


def one_line(s, limit=WHY_MAX):
    s = " ".join(str(s).split())
    return s if len(s) <= limit else s[:limit - 3].rstrip() + "..."


def load_refused(sdir):
    """{norm_name -> why} over support/refused/*.json."""
    out = {}
    for fn in sorted(glob.glob(os.path.join(sdir, "refused", "*.json"))):
        try:
            with open(fn, encoding="utf-8") as f:
                e = json.load(f)
            name, why = e["card"], e["why"]
        except (OSError, ValueError, KeyError) as ex:
            raise StoreError("dirac: %s: %s" % (fn, ex))
        k = norm_name(name)
        if k:
            out[k] = one_line("%s: %s" % (e.get("class") or "refused", why))
    return out


def load_evidence(sdir):
    """{norm_name -> sorted list names} from the plain rows of support/evidence/*.txt."""
    out = {}
    for fn in sorted(glob.glob(os.path.join(sdir, "evidence", "*.txt"))):
        lst = os.path.splitext(os.path.basename(fn))[0]
        try:
            with open(fn, encoding="utf-8") as f:
                lines = f.read().split("\n")
        except OSError as e:
            raise StoreError("dirac: %s: %s" % (fn, e))
        for line in lines:
            if not line or "\t" in line:  # a tabbed row is pod evidence: it does not evidence a card
                continue
            k = norm_name(line)
            if k:
                out.setdefault(k, set()).add(lst)
    return {k: sorted(v) for k, v in out.items()}


def load_gaps(path, pin):
    """{norm_name -> (status, why)} from the snapshot; its pin must be GORGE_PIN."""
    try:
        with open(path, encoding="utf-8") as f:
            lines = f.read().split("\n")
    except OSError as e:
        raise StoreError("dirac: %s: %s (regenerate with `go run ./cmd/diracsupport`)" % (path, e))
    want = "#dirac-gaps gorge-pin " + pin
    if not lines or lines[0] != want:
        raise StaleSnapshot("dirac: %s is not at GORGE_PIN %s (header %r): regenerate it with"
                            " `go run ./cmd/diracsupport`" % (path, pin[:12], lines[0] if lines else ""))
    out = {}
    for n, line in enumerate(lines[1:], 2):
        if not line:
            continue
        f = line.split("\t")
        if len(f) != 3 or f[1] not in GAP_STATUSES:
            raise StoreError("dirac: %s:%d: want name<TAB>%s<TAB>why" % (path, n, "|".join(GAP_STATUSES)))
        k = norm_name(f[0])
        if k:
            out[k] = (f[1], one_line(f[2]))
    return out


def derive(refused, gaps, evidence):
    """{norm_name -> (status, why)} per the module docstring's precedence."""
    out = {}
    for k, (st, why) in gaps.items():
        out[k] = (st, why)
    for k in evidence:
        out.setdefault(k, ("supported", ""))
    for k, why in refused.items():
        out[k] = ("refused", why)
    return out


def rows_for(cards, status, evidence, pin, measured):
    """(support rows, evidence rows) for the (oracle_id, name) corpus."""
    sup, ev = [], []
    for oid, name in cards:
        k = norm_name(name)
        if k not in status:
            continue
        st, why = status[k]
        sup.append({"oracle_id": oid, "engine": ENGINE, "status": st, "pin": pin,
                    "why": why, "measured": measured})
        if st == "supported":
            for lst in evidence.get(k, ()):
                ev.append({"oracle_id": oid, "source": SOURCE, "ref": lst, "outcome": "agree"})
    return sup, ev


def _replace_mine(store, table, rows, mine, writer, version, ran):
    """put rows and drop this writer's earlier rows the batch no longer names, leaving every
    row `mine` rejects exactly as it is. store.rows() yields cells already in shard form, so
    the kept rows go back with raw=True (re-escaping would double their backslashes each run)
    and the new rows are escaped here, once."""
    others = [r for r in store.rows(table) if not mine(r)]
    cells = [{k: shard.escape_cell(v) for k, v in r.items()} for r in rows]
    return store.put(table, others + cells, writer=writer, version=version, ran=ran, replace=True, raw=True)


def run(store, repo=None, gorge=None, writer="ci", version=None, ran=None):
    """Compute dirac's support and evidence rows and put them.
    -> (rows changed, {status: n}, evidence rows put)."""
    repo = repo or repo_root()
    pin = full_pin(repo)
    gdir = gorge_dir(repo, gorge)
    base = os.path.basename(os.path.normpath(gdir))
    if "@" in base and not base.endswith(pin[:12]):
        raise StoreError("dirac: %s is not gorge at GORGE_PIN %s" % (gdir, pin[:12]))
    sdir = os.path.join(gdir, "dirac", "support")
    if not os.path.isdir(os.path.join(sdir, "evidence")):
        raise StoreError("dirac: %s holds no dirac/support/evidence" % gdir)
    refused = load_refused(sdir)
    evidence = load_evidence(sdir)
    gaps = load_gaps(os.path.join(repo, GAPS_REL), pin)
    status = derive(refused, gaps, evidence)
    sup, ev = rows_for(store.cards(), status, evidence, pin[:12], today(ran))
    counts = {}
    for r in sup:
        counts[r["status"]] = counts.get(r["status"], 0) + 1
    changed = _replace_mine(store, "support", sup, lambda r: r["engine"] == ENGINE, writer, version, ran)
    changed += _replace_mine(store, "evidence", ev, lambda r: r["source"] == SOURCE, writer, version, ran)
    return changed, counts, len(ev)
