"""inclusion: a deck sample's per-card inclusion table -> curated/commander-inclusion.

The input is the private per-card table of a deck sample (header `card decks
rate rate_precon rate_casual rate_cedh rate_ranking`): card names and shares,
never a deck, list, deck name or URL. Each name resolves to an oracle_id; a
name that answers to no card (an x: pseudo id) is left out and only counted,
and nothing is recorded in _meta/unresolved. The precon and ranking-half
shares are read and dropped. Shares are copied as the input prints them.

The output is the curated shard, rows sorted bytewise by oracle_id, with no
clock and no ambient order: a rerun over the same input writes the same bytes.
"""
import os

from . import shard
from .schema import PUBLIC, repo_root
from .store import StoreError

TABLE = "curated/commander-inclusion"
INPUT_COLUMNS = ("card", "decks", "rate", "rate_precon", "rate_casual", "rate_cedh", "rate_ranking")
# The input columns copied to the output; rate_precon and rate_ranking are dropped.
COPIED = ("decks", "rate", "rate_casual", "rate_cedh")
DEFAULT_OUT = os.path.join("carddb", "curated", "commander-inclusion.tsv")


class Result:
    def __init__(self, rows, names, unresolved, folded):
        self.rows = rows
        self.names = names
        self.unresolved = unresolved
        self.folded = folded

    def summary(self, sample):
        return "inclusion: %d rows from %d names, %d unresolved, %d folded, sample %s" % (
            len(self.rows), self.names, self.unresolved, self.folded, sample)


def read_input(path):
    """The input TSV's rows as dicts keyed by INPUT_COLUMNS; raises StoreError on a bad file."""
    try:
        with open(path, encoding="utf-8", newline="") as f:
            text = f.read()
    except (OSError, UnicodeDecodeError) as e:
        raise StoreError("inclusion: cannot read %s: %s" % (path, e))
    lines = text.split("\n")
    if lines and lines[-1] == "":
        lines.pop()
    if not lines or tuple(lines[0].rstrip("\r").split("\t")) != INPUT_COLUMNS:
        raise StoreError("inclusion: %s: header must be %s" % (path, "\t".join(INPUT_COLUMNS)))
    out = []
    for i, line in enumerate(lines[1:], 2):
        cells = line.rstrip("\r").split("\t")
        if len(cells) != len(INPUT_COLUMNS):
            raise StoreError("inclusion: %s:%d: %d cells, want %d" % (path, i, len(cells), len(INPUT_COLUMNS)))
        if cells[0] == "":
            raise StoreError("inclusion: %s:%d: empty card name" % (path, i))
        out.append(dict(zip(INPUT_COLUMNS, cells)))
    return out


def _decks(row):
    try:
        return int(row["decks"])
    except ValueError:
        raise StoreError("inclusion: card %r: decks %r is not an int" % (row["card"], row["decks"]))


def rows(inputs, resolver, sample):
    """-> Result: one output row (dict of the table's columns) per resolved oracle_id.

    A name resolving only to an x: pseudo id is dropped and counted unresolved; two names
    resolving to one oracle_id keep the row with more decks, ties to the bytewise smaller
    name, and the other is counted folded."""
    best = {}  # oracle_id -> input row
    unresolved = folded = 0
    for r in inputs:
        oid = resolver.resolve(r["card"], record=False)
        if oid.startswith("x:"):
            unresolved += 1
            continue
        cur = best.get(oid)
        if cur is not None:
            folded += 1
            # str order is code-point order, the same as UTF-8 byte order.
            if (-_decks(r), r["card"]) >= (-_decks(cur), cur["card"]):
                continue
        best[oid] = r
    out = []
    for oid in sorted(best):
        r = best[oid]
        row = {"oracle_id": oid, "sample": sample}
        for c in COPIED:
            row[c] = r[c]
        out.append(row)
    return Result(out, len(inputs), unresolved, folded)


def write(path, rows, schema):
    """Render rows as the curated shard at path; raises StoreError on a cell the schema rejects."""
    table = schema.tables.get(TABLE)
    if table is None:
        raise StoreError("inclusion: the schema declares no %s table" % TABLE)
    cols = table.scope_columns(PUBLIC)
    cells = []
    for r in rows:
        line = []
        for c in cols:
            v = r.get(c.name, "")
            why = shard.check_cell(c, v)
            if not why and any(ch in v for ch in "\t\n\r"):
                why = "holds a tab, newline or CR"
            if why:
                raise StoreError("inclusion: %s %s: %s" % (r.get("oracle_id", "?"), c.name, why))
            line.append(v)
        cells.append(line)
    text = shard.render(TABLE, schema.version, [c.name for c in cols],
                        shard.sort_rows(cells, shard.key_indexes(cols)))
    d = os.path.dirname(path)
    if d:
        os.makedirs(d, exist_ok=True)
    with open(path, "w", encoding="utf-8", newline="") as f:
        f.write(text)
    return text


def read_cards(path):
    """(oracle_id, name) pairs of a TSV `oracle_id<TAB>name` (a header line starting `oracle_id` is skipped)."""
    try:
        with open(path, encoding="utf-8", newline="") as f:
            lines = f.read().split("\n")
    except (OSError, UnicodeDecodeError) as e:
        raise StoreError("inclusion: cannot read %s: %s" % (path, e))
    out = []
    for i, line in enumerate(lines, 1):
        line = line.rstrip("\r")
        if line == "":
            continue
        cells = line.split("\t")
        if len(cells) != 2:
            raise StoreError("inclusion: %s:%d: want oracle_id<TAB>name" % (path, i))
        if i == 1 and cells[0] == "oracle_id":
            continue
        out.append((cells[0], cells[1]))
    return out


def default_out():
    return os.path.join(repo_root(), DEFAULT_OUT)
