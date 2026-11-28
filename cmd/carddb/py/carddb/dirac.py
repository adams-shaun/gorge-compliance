"""The dirac writer: gorge's dirac support lists -> support, evidence and lockstep keyproof rows.

gorge's generated dirac/docs/support.md holds counts only; the per-card data it
is generated from is, in the pinned gorge module:

  dirac/support/refused/*.json    a refusal by name (card, class, why)
  dirac/support/evidence/*.txt    one card name per row: clean lockstep games in that
                                  list (_evidence_rows carries kgen's seat rule)
  dirac/support/evidence-keys/*.tsv
                                  per list, the keys each card's games left unused

and the lowering, which needs the GPL Forge corpus and so cannot run in CI. The
lowering's result is the committed snapshot oracle/compliance/dirac-gaps.tsv
(`go run ./cmd/diracsupport`): the corpus cards the two lists cannot place --
unverified, not lowered, and supported-but-two-seat -- measured at one gorge pin.
Nothing regenerates that snapshot when GORGE_PIN moves, so it is expected to trail: a
snapshot at another pin does not stop the publish, its rows degrade instead. The `dirac`
CLI verb reports the pin mismatch, how many rows came from the snapshot, and exits 0.

A corpus card's status with the snapshot at GORGE_PIN, first match:
  refused      a refused entry names it (why = the entry's class and why)
  the snapshot's status (not_lowered, unverified, or supported with a two-seat why)
  supported    some evidence list names it (kgen's rule: lowered, not refused, evidenced)
A card no list, entry or snapshot row names is not in gorge's corpus and gets no
row. Names match on norm_name (the front face, like compliance.py), and a split
card's evidence is found under either spelling because norm_name keeps the front face.

A stale snapshot (its pin is not GORGE_PIN) uses this order, first match:
  refused      a refused entry names it
  supported    an evidence list at GORGE_PIN names it (kgen's rule: a card with
               evidence is lowered), keeping the snapshot's two-seat why when it has one
  the snapshot's not_lowered or unverified
  no row
Every row taken from the stale snapshot ends its why with
` (lowering snapshot at gorge <first 10 hex of the snapshot pin>)`; rows taken from
the refused or evidence lists do not carry the note.

Evidence rows: one per (supported card, evidence list), source `dirac-lockstep`, ref the
list's name, outcome `agree`. Rows this writer wrote that a rerun no longer derives
(a card that left the corpus or lost its evidence) are deleted; every other engine's
and source's rows are left alone. For a reproducible `measured` date the caller passes
`ran` or sets CARDDB_NOW (see compliance.py).

Lockstep rows (keyproof, leg `lockstep`, always writer `dirac`): every ledger key (the
keys of the card's stored keyproof rows, any leg) of a card an evidence list names under
kgen's rule is `agreed` at the largest seats any naming list records (a plain row is 2;
the ledger's seats stop at 4), witness `list=<the first list, by file name, with that
value>`. A key in the `unused` cell of every evidence-keys row of the card is instead
`owed`/`lockstep-unused` at seats 0. `unused` tokens are compared verbatim with ledger
key ids; one that is no key of the card is only counted. A lockstep row this writer wrote
that a rerun no longer derives goes back to `owed`/`untried`; every other lockstep row of
an unlisted card is left alone. A card with no keyproof row gets none (the seed comes
first). The step reads the lists only, so a stale gaps snapshot does not affect it.
"""
import glob
import json
import os
import re
import subprocess
from collections import namedtuple

from . import keylegs, shard
from .norm import norm_name
from .schema import repo_root
from .store import StoreError, today

GORGE_MODULE = "github.com/adams-shaun/gorge"
ENGINE = "dirac"
SOURCE = "dirac-lockstep"
KEY_WRITER = "dirac"  # the writer id of every lockstep keyproof row, whatever --source says
LOCKSTEP = "lockstep"
PLAIN_SEATS = 2  # a plain evidence row's game
MAX_SEATS = 4  # the largest seats value the keyproof ledger holds
GAPS_REL = os.path.join("oracle", "compliance", "dirac-gaps.tsv")
WHY_MAX = 200
GAP_STATUSES = ("not_lowered", "unverified", "supported")


class Unavailable(StoreError):
    """The writer's inputs cannot be had on this machine (gorge not in the module cache, no
    network). The CLI keeps the last dirac rows and warns: the forge/xmage/catalogue publish
    must never depend on dirac."""


class Result(namedtuple("ResultBase", "changed counts evidence pin snapshot_pin stale_rows lockstep")):
    """One run's outcome. `snapshot_pin` is empty when the snapshot is at GORGE_PIN;
    otherwise `stale_rows` counts the rows whose status came from that stale snapshot.
    `lockstep` is lockstep_rows' stats."""

    __slots__ = ()

    @property
    def stale(self):
        return bool(self.snapshot_pin)


def gorge_dir(repo, explicit=None):
    """The pinned gorge module's directory: --gorge-dir, $CARDDB_GORGE_DIR, else
    `go list -m` in the checkout (go.mod pins gorge to GORGE_PIN's commit)."""
    d = explicit or os.environ.get("CARDDB_GORGE_DIR")
    if d:
        return d
    p = subprocess.run(["go", "list", "-m", "-f", "{{.Dir}}", GORGE_MODULE], cwd=repo,
                       capture_output=True, text=True)
    if p.returncode != 0 or not p.stdout.strip():
        raise Unavailable("dirac: cannot locate %s (go list -m: %s); pass --gorge-dir"
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


def _evidence_rows(sdir):
    """(list name, norm_name, seats) of every row of support/evidence/*.txt that evidences
    its card, lists in file name order.

    kgen's readSupport rule: a plain row is evidence (a two-seat game); a `Name<TAB>seats`
    row is pod evidence only when seats >= 4; a `Name<TAB>seats<TAB>xproof` row is evidence
    when seats >= 2. Any other tabbed row (a below-credit pod game) evidences nothing."""
    for fn in sorted(glob.glob(os.path.join(sdir, "evidence", "*.txt"))):
        lst = os.path.splitext(os.path.basename(fn))[0]
        try:
            with open(fn, encoding="utf-8") as f:
                lines = f.read().split("\n")
        except OSError as e:
            raise StoreError("dirac: %s: %s" % (fn, e))
        for line in lines:
            if not line:
                continue
            f = line.split("\t")
            seats = PLAIN_SEATS
            if len(f) >= 2:
                try:
                    seats = int(f[1].strip())
                except ValueError:
                    continue
                xproof = len(f) >= 3 and f[2].strip() == "xproof"
                if not (len(f) == 2 and seats >= 4 or xproof and seats >= 2):
                    continue
            k = norm_name(f[0])
            if k:
                yield lst, k, seats


def load_evidence(sdir):
    """{norm_name -> sorted list names} over support/evidence/*.txt (_evidence_rows' rule)."""
    out = {}
    for lst, k, _ in _evidence_rows(sdir):
        out.setdefault(k, set()).add(lst)
    return {k: sorted(v) for k, v in out.items()}


def read_evidence_seats(gorge_dir):
    """{norm_name -> (seats, list name)}: the largest seats value over the evidence lists
    naming the card (capped at MAX_SEATS) and the first list, by file name, that gave it."""
    out = {}
    for lst, k, seats in _evidence_rows(os.path.join(gorge_dir, "dirac", "support")):
        seats = min(seats, MAX_SEATS)
        if k not in out or seats > out[k][0]:  # lists come in file name order: a tie keeps the first
            out[k] = (seats, lst)
    return out


def read_unused(gorge_dir):
    """{norm_name -> set of key ids unused in every evidence-keys/*.tsv row of the card}.

    LEGACY.txt beside them is a plain name list with no key record and contributes nothing."""
    out = {}
    pattern = os.path.join(gorge_dir, "dirac", "support", "evidence-keys", "*.tsv")
    for fn in sorted(glob.glob(pattern)):
        try:
            with open(fn, encoding="utf-8") as f:
                lines = f.read().split("\n")
        except OSError as e:
            raise StoreError("dirac: %s: %s" % (fn, e))
        head = lines[0].split("\t")
        if "card" not in head or "unused" not in head:
            raise StoreError("dirac: %s: want a header naming the card and unused columns, got %r"
                             % (fn, lines[0]))
        ci, ui = head.index("card"), head.index("unused")
        for n, line in enumerate(lines[1:], 2):
            if not line:
                continue
            f = line.split("\t")
            if len(f) != len(head):
                raise StoreError("dirac: %s:%d: %d cells, want %d" % (fn, n, len(f), len(head)))
            k = norm_name(f[ci])
            if not k:
                continue
            tokens = {t for t in f[ui].split(",") if t}
            out[k] = out[k] & tokens if k in out else tokens
    return out


def _is_pin(s):
    return len(s) == 40 and all(c in "0123456789abcdef" for c in s.lower())


def load_gaps(path):
    """({norm_name -> (status, why)}, snapshot pin) from the snapshot.

    The header names the pin the snapshot was measured at, which need not be
    GORGE_PIN; callers compare it and degrade the rows rather than drop them. A
    file with no driver-readable header is an error: its rows cannot be dated."""
    try:
        with open(path, encoding="utf-8") as f:
            lines = f.read().split("\n")
    except OSError as e:
        raise StoreError("dirac: %s: %s (regenerate with `go run ./cmd/diracsupport`)" % (path, e))
    head = lines[0] if lines else ""
    f = head.split()
    if len(f) != 3 or f[0] != "#dirac-gaps" or f[1] != "gorge-pin" or not _is_pin(f[2]):
        raise StoreError("dirac: %s: want header `#dirac-gaps gorge-pin <40 hex>`, got %r"
                        " (regenerate with `go run ./cmd/diracsupport`)" % (path, head))
    pin = f[2]
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
    return out, pin


def derive(refused, gaps, evidence, stale=False, snapshot_pin=""):
    """{norm_name -> (status, why)} per the module docstring's precedence."""
    out = {}
    if not stale:
        for k, (st, why) in gaps.items():
            out[k] = (st, why)
        for k in evidence:
            out.setdefault(k, ("supported", ""))
        for k, why in refused.items():
            out[k] = ("refused", why)
        return out
    note = " (lowering snapshot at gorge %s)" % snapshot_pin[:10]
    for k in sorted(set(gaps) | set(evidence) | set(refused)):
        if k in refused:
            out[k] = ("refused", refused[k])
            continue
        snap = gaps.get(k)
        if k in evidence:
            # An evidence list at GORGE_PIN is kgen's lowering proof; a snapshot
            # row can still supply the two-seat why the old lowering measured.
            out[k] = ("supported", snap[1] if snap and snap[0] == "supported" else "")
        elif snap and snap[0] in ("not_lowered", "unverified"):
            out[k] = (snap[0], one_line(snap[1], WHY_MAX - len(note)).rstrip() + note)
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


def _seed_row(legs):
    """The stored row of one key ({leg -> row}) that carries the seed's per-key columns. The
    seed writes the lockstep and xmage legs and refreshes both on every reseed; a key
    with neither takes its first leg."""
    for leg in (LOCKSTEP, "xmage"):
        if leg in legs:
            return legs[leg]
    return legs[min(legs)]


def _unescape_cell(v):
    """The value shard.escape_cell stored as `v`: store.rows() yields cells in shard form,
    and a put escapes them again."""
    plain = {"\\": "\\", "t": "\t", "n": "\n", "r": "\r"}
    return re.sub(r"\\([\\tnr])", lambda m: plain[m.group(1)], v)


def lockstep_rows(store, seats, unused, pin12, measured):
    """(keyproof lockstep rows to put as writer `dirac`, stats) per the module docstring.

    seats is read_evidence_seats' map, unused read_unused's. The rows are full, in the
    schema's column order and in API (unescaped) form. stats counts the listed cards that
    have ledger keys, their agreed keys (also by 2 and 4 seats), their unused keys and
    their unused tokens that name no ledger key."""
    cols = [c.name for c in store.schema.tables[keylegs.TABLE].scope_columns(store.kind)]
    ledger = {}  # oracle_id -> key -> leg -> stored row
    for r in store.rows(keylegs.TABLE):
        ledger.setdefault(r["oracle_id"], {}).setdefault(r["key"], {})[r["leg"]] = r
    names = {oid: norm_name(name) for oid, name in store.cards()}
    stats = {"cards": 0, "agreed": 0, "unused": 0, "unmatched_unused": 0, "seats2": 0, "seats4": 0}

    def row(oid, key, src, **cells):
        out = dict.fromkeys(cols, shard.EMPTY)
        out.update(oracle_id=oid, key=key, leg=LOCKSTEP, agree_items=0, diff_items=0,
                   gorge_sha=pin12, measured=measured, writer=KEY_WRITER,
                   desc=_unescape_cell(src["desc"]), script_sha=_unescape_cell(src["script_sha"]),
                   multiplayer=src["multiplayer"])
        out.update(cells)
        return out

    rows = []
    for oid in sorted(ledger):
        keys = ledger[oid]
        name = names.get(oid)
        listed = seats.get(name)
        if listed:
            n, lst = listed
            dead = unused.get(name, set())
            stats["cards"] += 1
            stats["unmatched_unused"] += len(dead - set(keys))
        for key in sorted(keys):
            legs = keys[key]
            if listed:
                if key in dead:
                    stats["unused"] += 1
                    cells = {"state": "owed", "reason": "lockstep-unused", "seats": 0, "witness": shard.EMPTY}
                else:
                    stats["agreed"] += 1
                    if n in (2, 4):
                        stats["seats%d" % n] += 1
                    cells = {"state": "agreed", "reason": shard.EMPTY, "seats": n, "witness": "list=" + lst}
                rows.append(row(oid, key, _seed_row(legs), source="list", **cells))
                continue
            mine = legs.get(LOCKSTEP)
            if not mine or mine["writer"] != KEY_WRITER:
                continue
            back = {"state": "owed", "reason": "untried", "seats": 0, "witness": shard.EMPTY}
            if any(mine[c] != str(v) for c, v in back.items()):  # already back: keep its date
                rows.append(row(oid, key, mine, **back))
    return rows, stats


def _replace_mine(store, table, rows, mine, writer, version, ran):
    """put rows and drop this writer's earlier rows the batch no longer names, leaving every
    row `mine` rejects exactly as it is. store.rows() yields cells already in shard form, so
    the kept rows go back with raw=True (re-escaping would double their backslashes each run)
    and the new rows are escaped here, once."""
    others = [r for r in store.rows(table) if not mine(r)]
    cells = [{k: shard.escape_cell(v) for k, v in r.items()} for r in rows]
    return store.put(table, others + cells, writer=writer, version=version, ran=ran, replace=True, raw=True)


def run(store, repo=None, gorge=None, writer="ci", version=None, ran=None):
    """Compute dirac's support, evidence and lockstep keyproof rows and put them.
    -> (rows changed, {status: n}, evidence rows put)."""
    r = run_report(store, repo=repo, gorge=gorge, writer=writer, version=version, ran=ran)
    return r.changed, r.counts, r.evidence


def run_report(store, repo=None, gorge=None, writer="ci", version=None, ran=None):
    """run, plus the snapshot state the CLI reports: -> Result."""
    repo = repo or repo_root()
    pin = full_pin(repo)
    gdir = gorge_dir(repo, gorge)
    base = os.path.basename(os.path.normpath(gdir))
    if "@" in base and not base.endswith(pin[:12]):
        raise StoreError("dirac: %s is not gorge at GORGE_PIN %s" % (gdir, pin[:12]))
    sdir = os.path.join(gdir, "dirac", "support")
    if not os.path.isdir(os.path.join(sdir, "evidence")):
        raise Unavailable("dirac: %s holds no dirac/support/evidence" % gdir)
    refused = load_refused(sdir)
    evidence = load_evidence(sdir)
    gaps, snapshot_pin = load_gaps(os.path.join(repo, GAPS_REL))
    stale = snapshot_pin != pin
    status = derive(refused, gaps, evidence, stale=stale, snapshot_pin=snapshot_pin)
    cards = store.cards()
    sup, ev = rows_for(cards, status, evidence, pin[:12], today(ran))
    counts = {}
    for r in sup:
        counts[r["status"]] = counts.get(r["status"], 0) + 1
    changed = _replace_mine(store, "support", sup, lambda r: r["engine"] == ENGINE, writer, version, ran)
    changed += _replace_mine(store, "evidence", ev, lambda r: r["source"] == SOURCE, writer, version, ran)
    lrows, lstats = lockstep_rows(store, read_evidence_seats(gdir), read_unused(gdir), pin[:12], today(ran))
    changed += store.put(keylegs.TABLE, lrows, writer=KEY_WRITER, version=version, ran=ran)
    stale_rows = 0
    if stale:
        # derive() only takes a snapshot status when neither a refusal nor an
        # evidence list at GORGE_PIN placed the card; those are the stale rows.
        stale_keys = {k for k, (st, _) in status.items()
                      if st in ("not_lowered", "unverified")
                      and k not in refused and k not in evidence}
        stale_rows = sum(1 for _, name in cards if norm_name(name) in stale_keys)
    return Result(changed, counts, len(ev), pin, snapshot_pin if stale else "", stale_rows, lstats)
