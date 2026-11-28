"""keyproof as a leg ledger: one row per (card, key, leg).

The legacy keyproof.tsv held one row per (card, key) with 20 columns. The ledger
holds one row per evidence leg (xmage, forge, lockstep, scenario, cite) and five
more columns (leg, seats, multiplayer, replayed, replayed_sha). `split_legacy`
turns one legacy row into its leg rows, `migrate` rewrites every legacy-shaped
shard of a clone in place, and Store.put runs `split_legacy` over a batch whose
header has no `leg` column (what the xproof loop writes today).
"""
import os
import re

from . import shard
from .schema import EMPTY, PUBLIC
from .shard import ValidationError

TABLE = "keyproof"

LEGACY_COLUMNS = (
    "oracle_id", "key", "desc", "state", "reason", "refs", "source", "agree_items", "diff_items",
    "witness", "sig", "cite", "ticket", "gorge_sha", "corpus_pin", "script_sha", "xmage_ref",
    "forge_ref", "measured", "writer",
)
LEGS = ("xmage", "forge", "lockstep", "scenario", "cite")

# the legs a legacy row speaks for: a legacy put replaces these and no other
LEGACY_LEGS = ("xmage", "forge", "cite")

SEED_WRITER = "seed"

# the owed reasons the seed itself writes: a seed row still carrying one was never worked on
SEED_PRUNE_REASONS = ("untried", "not-in-corpus")

_WITNESS_PREFIX = re.compile(r"^([xfp]):(.*)$")


def _cell(v):
    return EMPTY if v is None or v == "" else v


def _witnesses(cell):
    """(xmage cell, forge cell) of a legacy witness cell: `x:<id>,f:<id>[,p:<id>]` splits by prefix,
    a cell with no x:/f:/p: prefix goes to both, an empty cell is `-`."""
    cell = _cell(cell)
    if cell == EMPTY:
        return EMPTY, EMPTY
    items = str(cell).split(",")
    if not any(_WITNESS_PREFIX.match(it) for it in items):
        return cell, cell
    by = {}
    for it in items:
        m = _WITNESS_PREFIX.match(it)
        if m and m.group(1) not in by:
            by[m.group(1)] = m.group(2) or EMPTY
    return by.get("x", EMPTY), by.get("f", EMPTY)


def split_legacy(row):
    """One legacy row (dict, legacy column names) -> its leg rows (dicts with the new column set)."""
    row = {c: _cell(row.get(c)) for c in LEGACY_COLUMNS}
    state = row["state"]
    refs = str(row["refs"]).replace("+P", "")
    xw, fw = _witnesses(row["witness"])

    def leg(name, st, refs_=EMPTY, reason=None, witness=None):
        out = dict(row)
        out.update(leg=name, state=st, refs=refs_, seats=2, multiplayer="false",
                   replayed=EMPTY, replayed_sha=EMPTY)
        if reason is not None:
            out["reason"] = reason
        if witness is not None:
            out["witness"] = witness
        elif name in ("xmage", "forge"):
            out["witness"] = xw if name == "xmage" else fw
        return out

    if state == "agreed":
        if refs not in ("FX", "X", "F"):
            raise ValueError("%s %s: agreed row with refs %r (want FX, X or F)" % (
                row["oracle_id"], row["key"], row["refs"]))
        rows = []
        if "X" in refs:
            rows.append(leg("xmage", "agreed", "X"))
        if "F" in refs:
            rows.append(leg("forge", "agreed", "F"))
        return rows
    if state == "diagnosed":
        if row["cite"] != EMPTY:
            return [leg("cite", "diagnosed", witness=_witness_all(row["witness"]))]
        return [leg("cite", "owed", reason="diagnosed-no-cite", witness=_witness_all(row["witness"]))]
    if state in ("diverged", "owed", "excluded"):
        return [leg("xmage", state), leg("forge", state)]
    raise ValueError("%s %s: unknown state %r" % (row["oracle_id"], row["key"], state))


def _witness_all(cell):
    """The witness a cite row carries: an unprefixed cell as is, a prefixed one has no single reference."""
    x, f = _witnesses(cell)
    return x if x == f else EMPTY


def split_batch(rows):
    """(rows, legacy) of a put batch: every legacy-shaped row (a dict with no `leg`, or a sequence of the
    20 legacy cells) is replaced by its leg rows; a leg row passes through. legacy maps each legacy
    (oracle_id, key) to the legs its rows speak for, so the put can drop the legs it no longer holds."""
    out, legacy = [], {}
    for r in rows:
        if not isinstance(r, dict):
            r = list(r)
            if len(r) != len(LEGACY_COLUMNS):
                out.append(r)
                continue
            r = dict(zip(LEGACY_COLUMNS, r))
        elif "leg" in r:
            out.append(r)
            continue
        try:
            legs = split_legacy(r)
        except ValueError as e:
            raise ValidationError(["keyproof: " + str(e)])
        legacy.setdefault((legs[0]["oracle_id"], legs[0]["key"]), set()).update(x["leg"] for x in legs)
        out.extend(legs)
    return out, legacy


def seed_prune(stored, batch_keys):
    """stored: iterable of (rel, key, row) where key is the (oracle_id, key, leg) tuple and row a dict
    of column -> cell; batch_keys: set of (oracle_id, key, leg) of the whole seed batch.
    -> (drop, orphans): drop is a sorted list of (rel, key) of the stored rows the seed wrote, still owed
    for one of SEED_PRUNE_REASONS, whose key the batch does not name; orphans is the number of distinct
    (oracle_id, key) absent from the batch whose stored rows include a writer other than seed."""
    named = {(k[0], k[1]) for k in batch_keys}
    drop, orphans = [], set()
    for rel, key, row in stored:
        if row["writer"] != SEED_WRITER:
            if (key[0], key[1]) not in named:
                orphans.add((key[0], key[1]))
        elif row["state"] == "owed" and row["reason"] in SEED_PRUNE_REASONS and key not in batch_keys:
            drop.append((rel, key))
    return sorted(drop), len(orphans)


def migrate(store):
    """Rewrite every legacy-shaped keyproof shard of the clone to leg rows, sorted by the new key, through
    the store's write path (validated, `git add`ed, ledgered). A shard already in the new shape is left
    alone, so a second run writes nothing. -> leg rows written."""
    table = store.schema.tables.get(TABLE)
    if table is None or table.is_private() != (store.kind != PUBLIC):
        return 0
    cols = table.scope_columns(store.kind)
    names = [c.name for c in cols]
    kidx = shard.key_indexes(cols)
    want_hdr = shard.shard_header(TABLE, store.schema.version)
    old_head = list(LEGACY_COLUMNS)
    with store.lock():
        plan, written = {}, 0
        for rel in store._table_shards(table):
            with open(os.path.join(store.dir, rel), encoding="utf-8", newline="") as f:
                text = f.read()
            try:
                hdr, head, rows = shard.parse_text(text)
            except ValueError as e:
                raise ValidationError(["%s: %s" % (rel, e)])
            if hdr != want_hdr or head != old_head:
                continue  # the new shape (or not ours to judge: validation reports it)
            out = {}
            for n, r in enumerate(rows, 3):  # line 1 is the shard header, line 2 the column names
                if len(r) != len(old_head):
                    raise ValidationError(["%s:%d: legacy row has %d cells, want %d" % (rel, n, len(r), len(old_head))])
                try:
                    legs = split_legacy(dict(zip(old_head, r)))
                except ValueError as e:
                    raise ValidationError(["%s:%d: %s" % (rel, n, e)])
                for lr in legs:
                    cells = [str(lr[n]) for n in names]  # shard cells are already escaped: copy as is
                    out[shard.row_key(cells, kidx)] = cells
            plan[rel] = shard.render(TABLE, store.schema.version, names, [out[k] for k in sorted(out)])
            written += len(out)
        if plan:
            store.apply_plan(table, plan, "migrate", written)
    return written
