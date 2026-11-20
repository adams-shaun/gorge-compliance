"""Read-only views over the carddb clone and the built SQLite file.

The certification data is the `cert` table (one current row per oracle_id).
`proven` is the public view of the cards certification has proven: level C2
(agreed) or C3 (certified), and not an exclusion. The built file carries the
same definition as the `proven` SQL view, so the CLI verb and the view can be
checked against each other on one fixture.
"""
import json
import os
import sqlite3

from .store import StoreError

PROVEN_LEVELS = ("C2", "C3")

# The columns `carddb proven` prints by default: what a caller needs to act on
# the row, without the long witness text.
PROVEN_COLUMNS = ("oracle_id", "norm_name", "name", "level", "first_set", "released", "refs", "measured")

PROVEN_VIEW_SQL = ("CREATE VIEW proven AS SELECT * FROM cert WHERE level IN ('C2', 'C3') "
                   "AND (exclusion IS NULL OR exclusion = '-')")


def is_proven(row):
    """A cert row counts as proven when it reached C2 or C3 and is not excluded."""
    return row.get("level") in PROVEN_LEVELS and row.get("exclusion") in (None, "", "-")


def proven_rows(rows):
    """The proven subset of cert rows, sorted by oracle_id then norm_name."""
    return sorted((r for r in rows if is_proven(r)), key=lambda r: (r.get("oracle_id") or "",
                                                                     r.get("norm_name") or ""))


def proven_from_store(store):
    if not store.exists():
        raise StoreError("no public clone at %s: run `carddb init`" % store.dir)
    return proven_rows(store.rows("cert"))


def proven_from_db(path):
    """The built file's `proven` view as row dicts, in a stable order."""
    if not os.path.isfile(path):
        raise StoreError("no built carddb file at %s: run `carddb build`" % path)
    conn = sqlite3.connect(path)
    try:
        conn.execute("PRAGMA query_only=ON")
        cur = conn.execute("SELECT * FROM proven ORDER BY oracle_id, norm_name")
        names = [d[0] for d in cur.description]
        return [dict(zip(names, r)) for r in cur]
    except sqlite3.DatabaseError as e:
        raise StoreError("%s: %s" % (path, e))
    finally:
        conn.close()


def _cell(v):
    return "-" if v is None or v == "" else str(v)


def format_proven(rows, kind="tsv"):
    """Render proven rows as tsv (default), json or count. Deterministic."""
    if kind == "count":
        return "%d\n" % len(rows)
    if kind == "json":
        return json.dumps([{c: (None if r.get(c) in (None, "", "-") else r.get(c)) for c in PROVEN_COLUMNS}
                           for r in rows], sort_keys=True) + "\n"
    lines = ["\t".join(PROVEN_COLUMNS)]
    lines += ["\t".join(_cell(r.get(c)) for c in PROVEN_COLUMNS) for r in rows]
    return "\n".join(lines) + "\n"
