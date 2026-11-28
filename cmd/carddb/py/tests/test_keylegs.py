import os
import sqlite3

from carddb import keylegs, shard
from carddb.query import KEYPROOF_CARDS_VIEW_SQL, KEYPROOF_KEYS_VIEW_SQL

from .helpers import Base, OID_A, OID_B, OID_C
from .test_query import GORGE, legacy_row, read, write_legacy_shards


def legs(row):
    return [(r["leg"], r["state"], r["refs"], r["reason"], r["witness"]) for r in keylegs.split_legacy(row)]


class TestKeyLegs(Base):
    def test_split_table(self):
        d = "-"
        # agreed: FX, X and F (a +P is stripped first)
        self.assertEqual(legs(legacy_row(OID_A, "cast", "agreed", refs="FX")),
                         [("xmage", "agreed", "X", d, d), ("forge", "agreed", "F", d, d)])
        self.assertEqual(legs(legacy_row(OID_A, "cast", "agreed", refs="FX+P")),
                         [("xmage", "agreed", "X", d, d), ("forge", "agreed", "F", d, d)])
        self.assertEqual(legs(legacy_row(OID_A, "cast", "agreed", refs="X+P")), [("xmage", "agreed", "X", d, d)])
        self.assertEqual(legs(legacy_row(OID_A, "cast", "agreed", refs="F")), [("forge", "agreed", "F", d, d)])
        # diagnosed: a cite row, or an owed cite row without a cite
        self.assertEqual(legs(legacy_row(OID_A, "cast", "diagnosed", cite="CR 702.4b", refs="FX")),
                         [("cite", "diagnosed", d, d, d)])
        self.assertEqual(legs(legacy_row(OID_A, "cast", "diagnosed")),
                         [("cite", "owed", d, "diagnosed-no-cite", d)])
        # diverged, owed (same reason) and excluded: both engine legs
        self.assertEqual(legs(legacy_row(OID_A, "cast", "diverged", refs="FX")),
                         [("xmage", "diverged", d, d, d), ("forge", "diverged", d, d, d)])
        self.assertEqual(legs(legacy_row(OID_A, "cast", "owed", reason="harness")),
                         [("xmage", "owed", d, "harness", d), ("forge", "owed", d, "harness", d)])
        self.assertEqual(legs(legacy_row(OID_A, "cast", "excluded", reason="rng")),
                         [("xmage", "excluded", d, "rng", d), ("forge", "excluded", d, "rng", d)])
        # witness cells: x:/f:/p: split by engine, an unprefixed cell goes to every row, empty is -
        self.assertEqual(legs(legacy_row(OID_A, "cast", "agreed", refs="FX", witness="x:xs:A:b:s1,f:fs:A:b:s2,p:ps:A:b:s3")),
                         [("xmage", "agreed", "X", d, "xs:A:b:s1"), ("forge", "agreed", "F", d, "fs:A:b:s2")])
        self.assertEqual(legs(legacy_row(OID_A, "cast", "agreed", refs="FX", witness="xs:A:b:s1")),
                         [("xmage", "agreed", "X", d, "xs:A:b:s1"), ("forge", "agreed", "F", d, "xs:A:b:s1")])
        self.assertEqual(legs(legacy_row(OID_A, "cast", "diverged", witness="list=gaps")),
                         [("xmage", "diverged", d, d, "list=gaps"), ("forge", "diverged", d, d, "list=gaps")])
        self.assertEqual(legs(legacy_row(OID_A, "cast", "agreed", refs="FX", witness="")),
                         [("xmage", "agreed", "X", d, d), ("forge", "agreed", "F", d, d)])
        # the other columns: seed values for the new ones, the rest copied
        r = keylegs.split_legacy(legacy_row(OID_A, "cast", "agreed", refs="X", desc="Mana", agree_items=3,
                                            gorge_sha=GORGE))[0]
        self.assertEqual((r["seats"], r["multiplayer"], r["replayed"], r["replayed_sha"]), (2, "false", "-", "-"))
        self.assertEqual((r["desc"], r["agree_items"], r["gorge_sha"], r["writer"]), ("Mana", 3, GORGE, "xproof"))
        with self.assertRaises(ValueError):
            keylegs.split_legacy(legacy_row(OID_A, "cast", "agreed", refs="-"))

    def fixture(self):
        a = self.machine("a", init=True)
        write_legacy_shards(a.dir, [
            legacy_row(OID_A, "cast", "agreed", refs="FX", witness="x:xs:1,f:fs:1"),
            legacy_row(OID_A, "f0/a0", "owed", reason="no-generator"),
            legacy_row(OID_B, "cast", "diagnosed", cite="CR 702.4b"),
            legacy_row(OID_C, "cast", "diverged"),
        ])
        return a

    def test_migrate_is_idempotent(self):
        a = self.fixture()
        self.assertEqual(keylegs.migrate(a), 7)
        first = self.snapshot(a.dir)
        text = read(os.path.join(a.dir, "keyproof", "ab.tsv"))
        self.assertEqual(text.split("\n")[1], "\t".join(c.name for c in a.schema.tables["keyproof"].columns))
        keys = [(r[0], r[1], r[2]) for r in shard.parse_text(text)[2]]
        self.assertEqual(keys, sorted(keys))
        self.assertEqual(len(keys), 5)
        self.assertEqual(self.cli(a.home, "validate").returncode, 0)
        # a second run, and the verb, write nothing
        self.assertEqual(keylegs.migrate(a), 0)
        r = self.cli(a.home, "migrate", "keyproof-legs")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("0 rows written", r.stdout)
        self.assertEqual(self.snapshot(a.dir), first)

    def test_migrate_verb_rewrites_a_legacy_clone(self):
        a = self.fixture()
        r = self.cli(a.home, "migrate", "keyproof-legs")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("7 rows written", r.stdout)
        self.assertEqual(sorted((x["oracle_id"], x["key"], x["leg"], x["state"]) for x in a.rows("keyproof")), [
            (OID_A, "cast", "forge", "agreed"), (OID_A, "cast", "xmage", "agreed"),
            (OID_A, "f0/a0", "forge", "owed"), (OID_A, "f0/a0", "xmage", "owed"),
            (OID_B, "cast", "cite", "diagnosed"),
            (OID_C, "cast", "forge", "diverged"), (OID_C, "cast", "xmage", "diverged")])
        self.assertTrue(a.git("diff", "--cached", "--name-only").stdout.strip())  # staged for the publish

    def test_put_accepts_legacy_input(self):
        a = self.machine("a", init=True)
        batch = [legacy_row(OID_A, "cast", "agreed", refs="FX"), legacy_row(OID_B, "cast", "owed", reason="harness")]
        self.assertEqual(a.put("keyproof", batch, writer="xproof"), 4)
        got = sorted((r["oracle_id"], r["leg"], r["state"], r["seats"], r["multiplayer"]) for r in a.rows("keyproof"))
        self.assertEqual(got, [(OID_A, "forge", "agreed", "2", "false"), (OID_A, "xmage", "agreed", "2", "false"),
                               (OID_B, "forge", "owed", "2", "false"), (OID_B, "xmage", "owed", "2", "false")])
        self.assertEqual(a.put("keyproof", batch, writer="xproof"), 0)  # a rerun changes nothing
        # a rewritten key keeps no leg its legacy row no longer holds: FX narrowed to X leaves one agreed row
        a.put("keyproof", [legacy_row(OID_A, "cast", "agreed", refs="X")], writer="xproof")
        self.assertEqual([(r["leg"], r["state"]) for r in a.rows("keyproof") if r["oracle_id"] == OID_A],
                         [("xmage", "agreed")])
        # a leg row batch (a header with `leg`) merges as it is
        a.put("keyproof", [{**dict(zip(keylegs.LEGACY_COLUMNS, ["-"] * 20)), "oracle_id": OID_A, "key": "cast",
                            "leg": "lockstep", "state": "agreed", "seats": 3, "multiplayer": "true",
                            "gorge_sha": GORGE, "measured": "2026-11-23"}], writer="xproof")
        self.assertEqual(sorted(r["leg"] for r in a.rows("keyproof") if r["oracle_id"] == OID_A),
                         ["lockstep", "xmage"])

    def test_rollup_keeps_a_diagnosed_key_without_a_cite_diagnosed(self):
        # the real ledger's diagnosed rows all have cite '-': split, they are a cite leg owed
        # diagnosed-no-cite, and keyproof_keys must still call the key diagnosed (and the card proven)
        conn = sqlite3.connect(":memory:")
        names = [c.name for c in self.schema.tables["keyproof"].columns]
        conn.execute("CREATE TABLE keyproof (%s)" % ", ".join('"%s"' % n for n in names))
        conn.execute("CREATE TABLE curated_exclusions (oracle_id, reason)")
        for row in [legacy_row(OID_A, "cast", "agreed", refs="FX"), legacy_row(OID_A, "f0/a0", "diagnosed", sig="s1"),
                    legacy_row(OID_B, "cast", "agreed", refs="FX"), legacy_row(OID_B, "f0/a0", "owed", reason="harness")]:
            for lr in keylegs.split_legacy(row):
                conn.execute("INSERT INTO keyproof VALUES (%s)" % ",".join("?" * len(names)),
                             [str(lr[n]) for n in names])
        conn.execute(KEYPROOF_KEYS_VIEW_SQL)
        conn.execute(KEYPROOF_CARDS_VIEW_SQL)
        keys = conn.execute('SELECT oracle_id, "key", state, reason, refs, sig FROM keyproof_keys '
                            'ORDER BY oracle_id, "key"').fetchall()
        self.assertEqual(keys, [(OID_A, "cast", "agreed", None, "FX", "-"),
                                (OID_A, "f0/a0", "diagnosed", "diagnosed-no-cite", None, "s1"),
                                (OID_B, "cast", "agreed", None, "FX", "-"),
                                (OID_B, "f0/a0", "owed", "harness", None, "-")])
        self.assertEqual(conn.execute("SELECT oracle_id, diagnosed, owed, proven FROM keyproof_cards "
                                      "ORDER BY oracle_id").fetchall(), [(OID_A, 1, 0, 1), (OID_B, 0, 1, 0)])

    def test_put_legacy_drops_a_stale_cite_leg(self):
        # a key diagnosed then agreed: the legacy row states the whole key, so the cite leg goes; a
        # lockstep leg (no legacy row speaks for it) stays
        a = self.machine("a", init=True)
        a.put("keyproof", [legacy_row(OID_A, "cast", "diagnosed", cite="CR 702.4b")], writer="xproof")
        a.put("keyproof", [{**dict(zip(keylegs.LEGACY_COLUMNS, ["-"] * 20)), "oracle_id": OID_A, "key": "cast",
                            "leg": "lockstep", "state": "agreed", "seats": 2, "multiplayer": "false",
                            "gorge_sha": GORGE, "measured": "2026-11-23"}], writer="xproof")
        self.assertEqual(sorted((r["leg"], r["state"]) for r in a.rows("keyproof")),
                         [("cite", "diagnosed"), ("lockstep", "agreed")])
        a.put("keyproof", [legacy_row(OID_A, "cast", "agreed", refs="FX")], writer="xproof")
        self.assertEqual(sorted((r["leg"], r["state"]) for r in a.rows("keyproof")),
                         [("forge", "agreed"), ("lockstep", "agreed"), ("xmage", "agreed")])

    def test_migrate_reports_a_bad_legacy_row(self):
        a = self.machine("a", init=True)
        write_legacy_shards(a.dir, [legacy_row(OID_A, "cast", "agreed", refs="FX"),
                                    legacy_row(OID_A, "f0/a0", "bogus")])
        before = self.snapshot(a.dir)
        r = self.cli(a.home, "migrate", "keyproof-legs")
        self.assertEqual(r.returncode, 1, r.stdout)
        self.assertIn("carddb migrate: invalid:", r.stderr)
        self.assertIn("keyproof/%s.tsv:4: " % OID_A[:2], r.stderr)
        self.assertIn("unknown state 'bogus'", r.stderr)
        self.assertNotIn("Traceback", r.stderr)
        self.assertEqual(self.snapshot(a.dir), before)  # nothing written

    def test_put_cli_reads_a_legacy_header(self):
        a = self.machine("a", init=True)
        row = legacy_row(OID_A, "cast", "diverged")
        text = "\t".join(keylegs.LEGACY_COLUMNS) + "\n" + "\t".join(str(row[c]) for c in keylegs.LEGACY_COLUMNS) + "\n"
        r = self.cli(a.home, "put", "keyproof", "--source", "xproof", stdin=text)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("2 rows changed", r.stdout)
