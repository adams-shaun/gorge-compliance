"""parquet_export: per-table Parquet from a built carddb SQLite file (CI-only).

    python3 -I cmd/carddb/py/parquet_export.py --db carddb.sqlite --out carddb-parquet.tar.gz

pyarrow is a CI-only dependency: the carddb tool itself is stdlib only and this
script is the only place that imports it. It is standalone (it never imports
the carddb package and nothing in the carddb package imports it), and it is run
only by the carddb workflow, next to the release step.

Output: `<out>` (carddb-parquet.tar.gz beside the carddb.sqlite.zst asset),
a deterministic gzip tar holding

  <table>.parquet    one file per ordinary table of the built file: the schema
                     tables, card_names, build_meta, the src_* sources and the
                     `proven` view. FTS5 (card_fts) and its shadow tables and
                     sqlite's own (sqlite_%) objects are excluded structurally,
                     by their sqlite_master shape, not by a hand-built list.
  MANIFEST.json      the asset name and per object {kind, name, rows, sha256}.

Columns are typed from the declared SQLite types by their documented SQLite
affinity (integer int64, real float64, text string, blob binary), so any
synonym spelling a declaration carries -- `INT`, the type SQLite reports for
`CAST(x AS INTEGER)`, `BIGINT`, `VARCHAR(20)`, ... -- maps, and an empty table
still writes an empty parquet with its real schema and nothing is inferred
from values. Rows are written in
primary-key order (rowid for a table without declared keys, every column for a
view). Deterministic: sorted objects and members, zeroed tar metadata, no wall
clock; with a pinned pyarrow, rerunning on the same file gives the same bytes.

pyarrow is imported lazily, only inside the writer, so a checkout without it
still gets `--list` and the unit tests of everything but the bytes.
"""
import argparse
import gzip
import hashlib
import json
import os
import shutil
import sqlite3
import sys
import tarfile
from urllib.parse import quote

# The arrow type names, keyed by SQLite type AFFINITY rather than by one
# literal declared-type spelling: SQLite has many spellings per affinity
# (`CAST(x AS INTEGER)` declares its result `INT`, not `INTEGER`, and there are
# TINYINT/BIGINT/INT2/INT8/UNSIGNED BIG INT and friends), so matching the
# affinity cannot miss the next synonym. The affinity test below applies
# SQLite's documented classification (https://sqlite.org/datatype3.html 3.1)
# in its documented order. The contract is unchanged: types come from the
# column's declaration, never inferred from values; a column with no declared
# type, or one whose declaration maps to none of these four affinities (e.g.
# NUMERIC), fails loudly.
PARQUET_TYPES = {"integer": "int64", "text": "string", "real": "float64", "blob": "binary"}
PARQUET_ASSET = "carddb-parquet.tar.gz"
BATCH = 65536


class ExportError(Exception):
    pass


def _base_type(declared):
    """The declared type, normalised for the affinity test; a column with no
    declared type (a bare view expression) fails: parquet needs one."""
    base = (declared or "").strip().upper()
    if not base:
        raise ExportError("column has no declared SQLite type; parquet needs one")
    return base


def _affinity(declared):
    """The SQLite type affinity of a declared type, by the documented rules in
    their documented order; None when it maps to no parquet-backed affinity."""
    base = _base_type(declared)
    if "INT" in base:
        return "integer"
    if any(t in base for t in ("CHAR", "CLOB", "TEXT")):
        return "text"
    if "BLOB" in base:
        return "blob"
    if any(t in base for t in ("REAL", "FLOA", "DOUB")):
        return "real"
    return None


def parquet_type(declared):
    """The arrow type name for a declared SQLite type; unknown types fail loudly."""
    affinity = _affinity(declared)
    if affinity is None:
        raise ExportError("declared SQLite type %r has no parquet type "
                          "(want an INTEGER/TEXT/REAL/BLOB affinity declaration)" % declared)
    return PARQUET_TYPES[affinity]


def _q(ident):
    return '"%s"' % ident.replace('"', '""')


def data_objects(conn):
    """Sorted (kind, name) of the ordinary tables and views of a built file.

    Virtual tables (FTS5) are dropped, as are their shadow tables (every name
    prefixed `<virtual>_`) and sqlite's own objects; everything else in
    sqlite_master -- the schema tables, the derived tables and the `proven`
    view -- is exported."""
    objs = conn.execute("SELECT type, name, sql FROM sqlite_master "
                        "WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite\\_%' ESCAPE '\\'"
                        ).fetchall()
    virtual = set()
    for _typ, name, sql in objs:
        if sql and sql.lstrip().upper().startswith("CREATE VIRTUAL TABLE"):
            virtual.add(name)
    return sorted(((typ, name) for typ, name, _sql in objs
                   if name not in virtual
                   and not any(name.startswith(v + "_") for v in virtual)),
                  key=lambda o: o[1])


def columns(conn, name):
    """[(column, declared type, pk order)] by PRAGMA table_info (works for views too)."""
    return [(c[1], c[2], c[5]) for c in conn.execute("PRAGMA table_info(%s)" % _q(name))]


def order_by(conn, name, cols, kind):
    """The deterministic row order of one object: declared keys, else rowid, else every column."""
    pks = [c[0] for c in sorted((c for c in cols if c[2]), key=lambda c: c[2])]
    if pks:
        return ", ".join(_q(p) for p in pks)
    if kind == "table":
        return "rowid"
    return ", ".join(_q(c[0]) for c in cols)


def _pyarrow():
    try:
        import pyarrow as pa  # noqa: F401  (CI-only dependency, see the module docstring)
        import pyarrow.parquet as pq
    except ImportError as e:
        raise ExportError("pyarrow is required to write parquet: it is a CI-only dependency "
                          "(pip install pyarrow; never a carddb runtime one): %s" % e)
    return pa, pq


def write_object(conn, name, kind, path, batch=BATCH):
    """One parquet file for one table or view; -> (rows written, [(column, arrow type)])."""
    pa, pq = _pyarrow()
    cols = columns(conn, name)
    names = [c[0] for c in cols]
    types = [pa.type_for_alias(parquet_type(c[1])) for c in cols]
    sel = "SELECT %s FROM %s ORDER BY %s" % (
        ", ".join(_q(n) for n in names), _q(name), order_by(conn, name, cols, kind))
    schema = pa.schema([pa.field(n, t) for n, t in zip(names, types)])
    writer = pq.ParquetWriter(path, schema, compression="zstd")
    rows = 0
    try:
        cur = conn.execute(sel)
        while True:
            got = cur.fetchmany(batch)
            if not got:
                break
            rows += len(got)
            arrays = [pa.array([r[i] for r in got], type=types[i]) for i in range(len(names))]
            writer.write_batch(pa.RecordBatch.from_arrays(arrays, schema=schema))
    finally:
        writer.close()
    return rows, list(zip(names, (parquet_type(c[1]) for c in cols)))


def _sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def _tar_gz(out, work):
    """A deterministic gzip tar of work/: sorted members, zeroed metadata, mtime 0."""
    with open(out, "wb") as raw:
        # filename="" so the gzip header carries no FNAME field: the bytes
        # depend on the db alone, not on the output path.
        with gzip.GzipFile(fileobj=raw, mode="wb", compresslevel=9, mtime=0, filename="") as gz:
            with tarfile.open(fileobj=gz, mode="w", format=tarfile.GNU_FORMAT) as tf:
                for fn in sorted(os.listdir(work)):
                    p = os.path.join(work, fn)
                    ti = tarfile.TarInfo(fn)
                    ti.size, ti.mtime, ti.mode = os.path.getsize(p), 0, 0o644
                    ti.uid = ti.gid = 0
                    ti.uname = ti.gname = ""
                    ti.type = tarfile.REGTYPE
                    with open(p, "rb") as f:
                        tf.addfile(ti, f)


def _ro_conn(db):
    """The built file opened read-only (mode=ro, query_only), like the tool's read verbs."""
    conn = sqlite3.connect("file:%s?mode=ro" % quote(os.path.abspath(db)), uri=True)
    conn.execute("PRAGMA query_only=ON")
    return conn


def export(db, out, batch=BATCH):
    """The archive at out from the built file db. -> the manifest document."""
    if not os.path.isfile(db):
        raise ExportError("no built database at %s" % db)
    work = out + ".work"
    shutil.rmtree(work, ignore_errors=True)
    os.makedirs(work, exist_ok=True)
    try:
        conn = _ro_conn(db)
        try:
            tables = data_objects(conn)
            if not tables:
                raise ExportError("%s has no ordinary tables to export" % db)
            entries = []
            for kind, name in tables:
                p = os.path.join(work, name + ".parquet")
                rows, cols = write_object(conn, name, kind, p, batch)
                entries.append({"kind": kind, "name": name, "rows": rows, "sha256": _sha256(p),
                                "columns": [{"name": n, "type": t} for n, t in cols]})
            manifest = {"asset": os.path.basename(out), "tables": entries}
            mpath = os.path.join(work, "MANIFEST.json")
            with open(mpath, "w", encoding="utf-8") as f:
                json.dump(manifest, f, sort_keys=True)
                f.write("\n")
            _tar_gz(out, work)
        finally:
            conn.close()
    finally:
        shutil.rmtree(work, ignore_errors=True)
    return manifest


def main(argv=None):
    ap = argparse.ArgumentParser(prog="parquet_export", description=__doc__.split("\n")[0].strip())
    ap.add_argument("--db", required=True, metavar="FILE", help="the built carddb SQLite file")
    ap.add_argument("--out", required=True, metavar="FILE", help="the .tar.gz to write")
    ap.add_argument("--batch", type=int, default=BATCH, help="rows per parquet batch (default %d)" % BATCH)
    ap.add_argument("--list", action="store_true", help="print the exported objects and exit")
    a = ap.parse_args(argv)
    if not os.path.isfile(a.db):
        print("parquet_export: no built database at %s" % a.db, file=sys.stderr)
        return 2
    if not a.out.endswith(".tar.gz"):
        print("parquet_export: --out must be a .tar.gz (got %r)" % a.out, file=sys.stderr)
        return 2
    conn = _ro_conn(a.db)
    try:
        objs = data_objects(conn)
    finally:
        conn.close()
    if a.list:
        for kind, name in objs:
            print("%s\t%s" % (kind, name))
        return 0
    try:
        manifest = export(a.db, a.out, a.batch)
    except ExportError as e:
        print("parquet_export: %s" % e, file=sys.stderr)
        return 1
    for t in manifest["tables"]:
        print("carddb parquet: %s %s %d rows" % (t["kind"], t["name"], t["rows"]))
    print("carddb parquet: %s (%d object(s))" % (a.out, len(manifest["tables"])))
    return 0


if __name__ == "__main__":
    sys.exit(main())
