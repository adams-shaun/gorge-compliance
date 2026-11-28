"""parquet_export: the CI-only per-table Parquet release asset.

Everything but the parquet bytes is tested without pyarrow: the object
selection (structural: virtual tables, their shadow tables and sqlite's own
objects are out, the `proven` view is in), the declared-type mapping and the
deterministic ordering. With pyarrow installed the exporter runs against a
real built file (and a hand-built one with edge shapes) and the archive is
checked for content and byte-determinism; without it those tests skip.
"""
import gzip
import hashlib
import json
import os
import sqlite3
import subprocess
import sys
import tarfile
import unittest

from carddb import build as build_mod

import parquet_export

from .helpers import Base, OID_A, OID_B, OID_C
from .test_build import write

SCRIPT = os.path.normpath(os.path.join(os.path.dirname(__file__), "..", "parquet_export.py"))


def has_pyarrow():
    try:
        import pyarrow  # noqa: F401
        return True
    except ImportError:
        return False


def read_archive(path):
    """{member: bytes} of a tar.gz."""
    out = {}
    with tarfile.open(path, mode="r:gz") as tf:
        for m in tf.getmembers():
            out[m.name] = tf.extractfile(m).read()
    return out


def read_parquet(blob):
    import io
    import pyarrow.parquet as pq
    return pq.read_table(io.BytesIO(blob))


def sha(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def edge_db(path):
    """A hand-built file with the shapes the exporter must survive: NULLs, a
    WITHOUT ROWID table, a rowid table with no keys, a plain view, an
    aggregate/CASE view (the keyproof_cards shape: expression columns the
    exporter can only type if they are CAST, which SQLite then declares `INT`),
    an FTS5 table and sqlite's own objects (via ANALYZE)."""
    conn = sqlite3.connect(path)
    conn.executescript("""
        CREATE TABLE keys (a TEXT NOT NULL, b INTEGER, c REAL, PRIMARY KEY (a, b)) WITHOUT ROWID;
        INSERT INTO keys VALUES ('beta', 2, 2.5), ('alpha', 1, NULL), ('alpha', 0, -0.5);
        CREATE TABLE loose (a TEXT, b BLOB, d INTEGER);
        INSERT INTO loose VALUES ('x', x'00ff', NULL), ('y', NULL, 7), ('a', x'01', NULL);
        CREATE VIEW pick AS SELECT a FROM keys WHERE c IS NOT NULL;
        CREATE VIEW tally AS SELECT a, CAST(COUNT(*) AS INTEGER) AS n,
            CAST(SUM(d) AS INTEGER) AS total,
            CAST(CASE WHEN SUM(d)>1 THEN 1 ELSE 0 END AS INTEGER) AS big
            FROM loose GROUP BY a;
        CREATE VIRTUAL TABLE card_fts USING fts5(body);
        INSERT INTO card_fts VALUES ('hello'), ('world');
    """)
    conn.execute("ANALYZE")
    conn.commit()
    conn.close()
    return path


class Selection(Base):
    """data_objects picks by sqlite_master shape, not by a name list."""

    def test_virtual_shadows_internal_out_views_in(self):
        p = edge_db(os.path.join(self.tmp, "edge.sqlite"))
        conn = sqlite3.connect(p)
        self.addCleanup(conn.close)
        self.assertEqual(parquet_export.data_objects(conn),
                         [("table", "keys"), ("table", "loose"), ("view", "pick"), ("view", "tally")])

    def test_the_objects_of_a_built_file(self):
        a, committed = built_fixture(self)
        out = os.path.join(self.tmp, "carddb.sqlite")
        build_mod.build(out, self.schema, a.dir, None, committed)
        conn = sqlite3.connect(out)
        self.addCleanup(conn.close)
        objs = parquet_export.data_objects(conn)
        names = [n for _k, n in objs]
        self.assertIn("proven", names)
        self.assertIn("card_names", names)
        self.assertIn("build_meta", names)
        for t in ("cards", "printings", "sets", "legality", "labels", "status",
                  "src_manifest_sets", "src_manifest_cards", "src_diagnosed"):
            self.assertIn(t, names)
        self.assertNotIn("card_fts", names)
        self.assertFalse(any(n.startswith("card_fts_") for n in names))
        self.assertEqual(names, sorted(names))

    def test_every_exported_column_declares_a_parquet_type(self):
        """Every column of every object the built file exports maps to a parquet
        type, so a view with a bare expression column fails here, in the pull
        request, not in the nightly release (11-26/11-27: key_legs and friends
        broke the publish job two nights running)."""
        a, committed = built_fixture(self)
        out = os.path.join(self.tmp, "carddb.sqlite")
        build_mod.build(out, self.schema, a.dir, None, committed)
        conn = sqlite3.connect(out)
        self.addCleanup(conn.close)
        if not conn.execute("SELECT 1 FROM sqlite_master WHERE name='decks'").fetchone():
            # the fixture has no decks writer; deck_proven is built whenever a real file has one
            from carddb.query import DECK_PROVEN_VIEW_SQL
            conn.execute("CREATE TABLE decks (deck_id TEXT, oracle_id TEXT)")
            conn.execute(DECK_PROVEN_VIEW_SQL)
        bad = []
        for _kind, name in parquet_export.data_objects(conn):
            for col, declared, _pk in parquet_export.columns(conn, name):
                try:
                    parquet_export.parquet_type(declared)
                except parquet_export.ExportError:
                    bad.append("%s.%s (%r)" % (name, col, declared))
        self.assertEqual(bad, [], "columns with no parquet type: CAST them in the view")

    def test_columns_and_order(self):
        p = edge_db(os.path.join(self.tmp, "edge.sqlite"))
        conn = sqlite3.connect(p)
        self.addCleanup(conn.close)
        self.assertEqual(parquet_export.columns(conn, "keys"),
                         [("a", "TEXT", 1), ("b", "INTEGER", 2), ("c", "REAL", 0)])
        self.assertEqual(parquet_export.columns(conn, "loose"),
                         [("a", "TEXT", 0), ("b", "BLOB", 0), ("d", "INTEGER", 0)])
        self.assertEqual(parquet_export.order_by(conn, "keys", parquet_export.columns(conn, "keys"), "table"),
                         '"a", "b"')
        self.assertEqual(parquet_export.order_by(conn, "loose", parquet_export.columns(conn, "loose"), "table"),
                         "rowid")
        self.assertEqual(parquet_export.order_by(conn, "pick", parquet_export.columns(conn, "pick"), "view"),
                         '"a"')
        # the aggregate/CASE view: CAST gives every expression column a declared
        # type (SQLite reports it as `INT`), so none of them is the untyped ''
        # that used to reach the exporter and kill the publish job.
        self.assertEqual(parquet_export.columns(conn, "tally"),
                         [("a", "TEXT", 0), ("n", "INT", 0), ("total", "INT", 0), ("big", "INT", 0)])
        self.assertEqual(parquet_export.order_by(conn, "tally",
                                                 parquet_export.columns(conn, "tally"), "view"),
                         '"a", "n", "total", "big"')

    def test_unknown_declared_type_fails_loudly(self):
        self.assertEqual(parquet_export.parquet_type("TEXT"), "string")
        self.assertEqual(parquet_export.parquet_type("integer"), "int64")
        self.assertEqual(parquet_export.parquet_type("REAL"), "float64")
        self.assertEqual(parquet_export.parquet_type("BLOB"), "binary")
        # mapped by SQLite affinity, not one literal spelling: INT is what a
        # CAST(x AS INTEGER) view column declares, the rest are affinity synonyms
        for declared in ("INT", "BIGINT", "unsigned big int", "INT2", "INT8", "TINYINT"):
            self.assertEqual(parquet_export.parquet_type(declared), "int64", declared)
        self.assertEqual(parquet_export.parquet_type("VARCHAR(20)"), "string")
        self.assertEqual(parquet_export.parquet_type("DOUBLE PRECISION"), "float64")
        with self.assertRaises(parquet_export.ExportError):
            parquet_export.parquet_type("NUMERIC")
        with self.assertRaises(parquet_export.ExportError):
            parquet_export.parquet_type("")
        with self.assertRaises(parquet_export.ExportError):
            parquet_export.parquet_type("   ")


class CiOnly(unittest.TestCase):
    def test_the_carddb_runtime_never_touches_pyarrow(self):
        """pyarrow is a CI-only dependency: the carddb package and the launcher
        must not import it, and the only importer is this exporter."""
        py = os.path.normpath(os.path.join(os.path.dirname(__file__), ".."))
        offenders = [os.path.join(dp, fn)
                     for dp, _dn, fns in os.walk(os.path.join(py, "carddb")) for fn in fns
                     if fn.endswith(".py")]
        offenders.append(os.path.join(os.path.dirname(py), "main.py"))
        for p in sorted(offenders):
            with open(p, encoding="utf-8") as f:
                text = f.read()
            self.assertNotIn("pyarrow", text, p)
            self.assertNotIn("parquet_export", text, p)


class BuiltFixture:
    """A real built carddb.sqlite from the standard small fixture."""

    def build(self):
        a, committed = built_fixture(self)
        out = os.path.join(self.tmp, "carddb.sqlite")
        build_mod.build(out, self.schema, a.dir, None, committed)
        return out


def built_fixture(self):
    a = self.machine("a", init=True)
    a.put("cards", [
        {"oracle_id": OID_B, "name": "Fire // Ice", "layout": "split", "mana_cost": "{1}{R} // {1}{U}",
         "mana_value": 4, "type_line": "Instant // Instant", "colors": ["R", "U"], "color_identity": ["R", "U"]},
        {"oracle_id": OID_A, "name": "Lightning Bolt", "layout": "normal", "mana_cost": "{R}", "mana_value": 1,
         "type_line": "Instant", "colors": ["R"], "color_identity": ["R"]},
        {"oracle_id": OID_C, "name": "Dáin Ironfoot", "layout": "normal", "mana_cost": "{3}{R}", "mana_value": 4,
         "type_line": "Legendary Creature — Dwarf Warrior", "colors": ["R"], "color_identity": ["R"]},
    ], writer="mtgjson")
    a.put("sets", [{"set_code": "lea", "name": "Limited Edition Alpha", "release_date": "1993-08-05",
                    "set_type": "core"}], writer="mtgjson")
    a.put("printings", [{"printing_id": "p-1", "oracle_id": OID_A, "set_code": "lea",
                         "collector_number": "161", "rarity": "common", "released": "1993-08-05"}],
          writer="mtgjson")
    src = os.path.join(self.tmp, "src")
    write(os.path.join(src, "carddb/curated/labels.tsv"),
          "#carddb curated/labels v1\noracle_id\tlabel\tnote\n%s\tstaple\t-\n" % OID_A)
    write(os.path.join(src, "oracle/xengine/diagnosed.tsv"),
          "kind\tset\tcard\tfield\twrong\tcr\tevidence\tupstream\tticket\n"
          "diverge\tLEA\tLightning Bolt\tp0.hand\tharness\t307.4\twhy\t\txe-1\n")
    return a, build_mod.read_committed(directory=src)


@unittest.skipUnless(has_pyarrow(), "pyarrow is not installed")
class Archive(BuiltFixture, Base):
    def test_round_trip_and_determinism(self):
        import pyarrow

        db = self.build()
        out = os.path.join(self.tmp, "carddb-parquet.tar.gz")
        manifest = parquet_export.export(db, out)

        self.assertEqual(manifest["asset"], "carddb-parquet.tar.gz")
        names = [t["name"] for t in manifest["tables"]]
        self.assertEqual(names, sorted(names))
        self.assertIn("proven", names)
        self.assertNotIn("card_fts", names)
        members = read_archive(out)
        self.assertEqual(sorted(members), ["MANIFEST.json"] + [n + ".parquet" for n in names])
        self.assertEqual(json.loads(members["MANIFEST.json"].decode()), manifest)
        conn = sqlite3.connect(db)
        self.addCleanup(conn.close)
        for t in manifest["tables"]:
            want = conn.execute('SELECT count(*) FROM "%s"' % t["name"]).fetchone()[0]
            self.assertEqual(t["rows"], want, t["name"])
            self.assertEqual(hashlib.sha256(members[t["name"] + ".parquet"]).hexdigest(), t["sha256"])
        # typed columns, deterministic order (schema tables: PRIMARY KEY order)
        cards = read_parquet(members["cards.parquet"])
        self.assertEqual(cards.column("oracle_id").to_pylist(),
                         sorted(cards.column("oracle_id").to_pylist()))
        self.assertEqual(cards.schema.field("mana_value").type, pyarrow.int64())
        self.assertEqual(cards.schema.field("name").type, pyarrow.string())
        # the real keyproof_cards view: its seven aggregate/CASE columns are
        # CAST AS INTEGER in the view DDL, so each declares INT and reaches
        # parquet as int64 (an untyped expression column used to abort the
        # publish job in _base_type with "column has no declared SQLite type")
        self.assertIn("keyproof_cards", names)
        kp = read_parquet(members["keyproof_cards.parquet"])
        self.assertEqual(kp.schema.field("oracle_id").type, pyarrow.string())
        for col in ("keys", "agreed", "diagnosed", "diverged", "owed", "excluded", "proven"):
            self.assertEqual(kp.schema.field(col).type, pyarrow.int64(), col)
        # deterministic: same db, same parquet bytes even to a different
        # basename (the gzip header must not name the output path; only the
        # manifest's asset field, which is designed to name the archive,
        # differs); a changed db, different bytes
        out2 = os.path.join(self.tmp, "other-name.tar.gz")
        parquet_export.export(db, out2)
        m1, m2 = read_archive(out), read_archive(out2)
        j1 = json.loads(m1.pop("MANIFEST.json").decode())
        j2 = json.loads(m2.pop("MANIFEST.json").decode())
        self.assertEqual(m1, m2)
        j2["asset"] = j1["asset"]
        self.assertEqual(j1, j2)
        conn.execute("INSERT INTO build_meta VALUES ('extra', 'row')")
        conn.commit()
        conn.close()
        out3 = os.path.join(self.tmp, "changed.tar.gz")
        parquet_export.export(db, out3)
        self.assertNotEqual(sha(out), sha(out3))

    def test_edge_shapes_round_trip(self):
        import pyarrow

        p = edge_db(os.path.join(self.tmp, "edge.sqlite"))
        out = os.path.join(self.tmp, "edge.tar.gz")
        manifest = parquet_export.export(p, out)
        self.assertEqual([t["name"] for t in manifest["tables"]], ["keys", "loose", "pick", "tally"])
        members = read_archive(out)
        keys = read_parquet(members["keys.parquet"])
        # declared-key order ('a', then 'b'), NULLs are parquet nulls
        self.assertEqual(keys.column("a").to_pylist(), ["alpha", "alpha", "beta"])
        self.assertEqual(keys.column("b").to_pylist(), [0, 1, 2])
        self.assertEqual(keys.column("c").to_pylist(), [-0.5, None, 2.5])
        self.assertEqual(keys.schema.field("c").type, pyarrow.float64())
        loose = read_parquet(members["loose.parquet"])
        self.assertEqual(loose.schema.field("b").type, pyarrow.binary())
        self.assertEqual(loose.column("b").to_pylist(), [b"\x00\xff", None, b"\x01"])
        self.assertEqual(loose.column("d").to_pylist(), [None, 7, None])
        pick = read_parquet(members["pick.parquet"])
        self.assertEqual(pick.column("a").to_pylist(), ["alpha", "beta"])
        # the aggregate/CASE view: every expression column is typed from its
        # CAST declaration (INT -> int64), SUM over a group with no non-NULL
        # values is a parquet null, CASE 1/0 is an integer
        tally = read_parquet(members["tally.parquet"])
        for col in ("n", "total", "big"):
            self.assertEqual(tally.schema.field(col).type, pyarrow.int64(), col)
        self.assertEqual(tally.column("a").to_pylist(), ["a", "x", "y"])
        self.assertEqual(tally.column("n").to_pylist(), [1, 1, 1])
        self.assertEqual(tally.column("total").to_pylist(), [None, None, 7])
        self.assertEqual(tally.column("big").to_pylist(), [0, 0, 1])

    def test_cli(self):
        db = self.build()
        out = os.path.join(self.tmp, "out.tar.gz")
        # python3 -I would drop PYTHONPATH (and so pyarrow), so the child is
        # run with -I off and PYTHONPATH pointed at the pyarrow this test
        # imported; when there is none the test skips.
        env = dict(os.environ)
        env.pop("PYTHONPATH", None)
        try:
            import pyarrow
            env["PYTHONPATH"] = os.path.dirname(os.path.dirname(os.path.abspath(pyarrow.__file__)))
        except ImportError:
            self.skipTest("pyarrow is not installed")
        r = subprocess.run([sys.executable, SCRIPT, "--db", db, "--out", out],
                           capture_output=True, text=True, env=env)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("carddb parquet:", r.stdout)
        self.assertTrue(os.path.isfile(out))
        r = subprocess.run([sys.executable, SCRIPT, "--db", db, "--out", out, "--list"],
                           capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("view\tproven", r.stdout)
        r = subprocess.run([sys.executable, SCRIPT, "--db", os.path.join(self.tmp, "nope.sqlite"),
                            "--out", out], capture_output=True, text=True)
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        r = subprocess.run([sys.executable, SCRIPT, "--db", db,
                            "--out", os.path.join(self.tmp, "x.parquet")],
                           capture_output=True, text=True)
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)


@unittest.skipUnless(has_pyarrow(), "pyarrow is not installed")
class GzipMeta(Base):
    def test_the_archive_has_zeroed_metadata(self):
        out = os.path.join(self.tmp, "edge.tar.gz")
        parquet_export.export(edge_db(os.path.join(self.tmp, "edge.sqlite")), out)
        with open(out, "rb") as f:
            header = f.read(10)
        self.assertEqual(int.from_bytes(header[4:8], "little"), 0)  # gzip mtime: no wall clock
        self.assertEqual(header[3], 0)  # FLG: no FNAME field naming the output
        with tarfile.open(out, mode="r:gz") as tf:
            for m in tf.getmembers():
                self.assertEqual(m.mtime, 0)
                self.assertEqual((m.uid, m.gid, m.uname, m.gname), (0, 0, "", ""))


if __name__ == "__main__":
    unittest.main()
