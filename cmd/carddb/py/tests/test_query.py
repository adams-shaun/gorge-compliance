import json
import os
import sqlite3
import unittest

from carddb import build as build_mod
from carddb import query as query_mod
from carddb import shard
from carddb.boundary import boundary_tree
from carddb.store import Store

from .helpers import Base, GOLDEN, OID_A, OID_B, OID_C, ROOT, git

UPDATE = os.environ.get("CARDDB_UPDATE_GOLDEN") == "1"

OID_D = "de" + "3" * 30
OID_E = "ef" + "4" * 30
OID_F = "f0" + "5" * 30

CURATED = {
    "labels": [{"oracle_id": OID_A, "label": "staple", "note": "format fixture"}],
    "game-changers": [{"oracle_id": OID_A, "added": "2023-01-01"}],
    "bracket-flags": [{"oracle_id": OID_A, "flag": "mass-land-denial"}],
    "exclusions": [{"oracle_id": OID_E, "reason": "ante"}],
    "relations": [{"oracle_id": OID_A, "relation": "combos_with", "other_oracle_id": OID_C}],
}


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


class Query(Base):
    """One fixture build, then every read verb against it."""

    def setUp(self):
        super().setUp()
        for k, v in {
            "GIT_AUTHOR_NAME": "carddb", "GIT_AUTHOR_EMAIL": "carddb@localhost",
            "GIT_COMMITTER_NAME": "carddb", "GIT_COMMITTER_EMAIL": "carddb@localhost",
            "GIT_AUTHOR_DATE": "2026-11-19T00:00:00+00:00",
            "GIT_COMMITTER_DATE": "2026-11-19T00:00:00+00:00",
            "GIT_CONFIG_GLOBAL": os.devnull,
        }.items():
            old = os.environ.get(k)
            os.environ[k] = v
            self.addCleanup(lambda k=k, old=old: os.environ.pop(k, None)
                            if old is None else os.environ.__setitem__(k, old))
        self.home = os.path.join(self.tmp, "a")
        self.a = self.machine("a", init=True)

        self.a.put("cards", [
            {"oracle_id": OID_A, "name": "Lightning Bolt", "layout": "normal", "mana_cost": "{R}",
             "mana_value": 1, "type_line": "Instant", "colors": ["R"], "color_identity": ["R"]},
            {"oracle_id": OID_B, "name": "Fire // Ice", "layout": "split",
             "mana_cost": "{1}{R} // {1}{U}", "mana_value": 4, "type_line": "Instant // Instant",
             "colors": ["R", "U"], "color_identity": ["R", "U"]},
            {"oracle_id": OID_C, "name": "Dáin Ironfoot", "layout": "normal", "mana_cost": "{3}{R}",
             "mana_value": 4, "type_line": "Legendary Creature — Dwarf Warrior", "colors": ["R"],
             "color_identity": ["R"], "keywords": ["Haste", "Double strike"]},
            {"oracle_id": OID_D, "name": "Digital Only", "layout": "normal", "mana_cost": "{1}{U}",
             "mana_value": 1, "type_line": "Instant", "colors": ["U"], "color_identity": ["U"]},
            {"oracle_id": OID_E, "name": "Fun Card", "layout": "normal", "mana_cost": "{1}{G}",
             "mana_value": 1, "type_line": "Instant", "colors": ["G"], "color_identity": ["G"]},
            {"oracle_id": OID_F, "name": "No Printing", "layout": "normal", "mana_cost": "{1}",
             "mana_value": 1, "type_line": "Artifact"},
        ], writer="catalogue")
        self.a.put("sets", [
            {"set_code": "lea", "name": "Limited Edition Alpha", "release_date": "1993-08-05",
             "set_type": "core"},
            {"set_code": "2ed", "name": "Unlimited Edition", "release_date": "1993-12-01",
             "set_type": "core"},
            {"set_code": "alch", "name": "Alchemy", "release_date": "2020-01-01", "set_type": "alchemy"},
            {"set_code": "funny", "name": "Funny", "release_date": "2020-01-01", "set_type": "funny"},
        ], writer="catalogue")
        self.a.put("printings", [
            {"printing_id": "p-a1", "oracle_id": OID_A, "set_code": "lea", "collector_number": "161",
             "rarity": "common", "released": "1993-08-05"},
            {"printing_id": "p-a2", "oracle_id": OID_A, "set_code": "2ed", "collector_number": "161",
             "rarity": "common", "released": "1993-12-01"},
            {"printing_id": "p-b1", "oracle_id": OID_B, "set_code": "lea", "collector_number": "162",
             "rarity": "uncommon", "released": "1993-08-05"},
            {"printing_id": "p-c1", "oracle_id": OID_C, "set_code": "lea", "collector_number": "163",
             "rarity": "rare", "released": "1993-08-05"},
            {"printing_id": "p-d1", "oracle_id": OID_D, "set_code": "alch", "collector_number": "1",
             "rarity": "common", "released": "2020-01-01"},
            {"printing_id": "p-e1", "oracle_id": OID_E, "set_code": "funny", "collector_number": "1",
             "rarity": "common", "released": "2020-01-01"},
        ], writer="catalogue")
        self.a.put("legality", [
            {"oracle_id": oid, "format": "modern", "status": status}
            for oid, status in ((OID_A, "legal"), (OID_B, "banned"), (OID_C, "legal"),
                                (OID_D, "legal"), (OID_E, "legal"), (OID_F, "legal"))
        ], writer="catalogue")
        self.a.put("status", [
            {"oracle_id": OID_A, "level": "A", "verdict": "agree", "gorge_pin": "8f7095353a64",
             "measured": "2026-11-19", "writer": "loop"},
            {"oracle_id": OID_B, "level": "B", "verdict": "diverge", "gorge_pin": "8f7095353a64",
             "measured": "2026-11-19", "writer": "loop"},
        ], writer="loop")
        self.a.put("cert", [
            {"oracle_id": OID_A, "level": "C2", "declared": "2026-11-19", "writer": "certification"},
            {"oracle_id": OID_B, "level": "C1", "declared": "2026-11-19", "writer": "certification"},
        ], writer="certification")
        self.a.put("labels", [
            {"oracle_id": OID_A, "label": "burn", "origin": "derived"},
            {"oracle_id": OID_C, "label": "ramp", "origin": "derived"},
        ], writer="labels")
        self.a.put("evidence", [
            {"oracle_id": OID_A, "source": "oracle", "ref": "scen-1", "outcome": "agree", "writer": "ci"},
        ], writer="ci")
        self.a.put("xledger", [
            {"oracle_id": OID_A, "tier": "T1", "state": "agree", "note": "fixture"},
        ], writer="farm")
        self.a.put("_meta/unresolved", [
            {"kind": "card", "ref": "Mystery Card", "reason": "no card of this name; pseudo id x:1"},
        ], writer="resolve")

        self.sla = os.path.join(self.tmp, "writers.tsv")
        write(self.sla, "#carddb-writers v1\nwriter\tsla_hours\n" + "".join(
            "%s\t%d\n" % (w, 24 if w == "catalogue" else 1000)
            for w in ("catalogue", "certification", "ci", "farm", "labels", "loop", "resolve")))
        self.asset = os.path.join(self.tmp, "carddb-latest.json")
        write(self.asset, json.dumps({"published_at": "2026-11-19T00:00:00Z", "unresolved": 1}) + "\n")

        src = os.path.join(self.tmp, "src")
        for table, rows in CURATED.items():
            cols = list(rows[0])
            write(os.path.join(src, "carddb/curated", table + ".tsv"),
                  "#carddb curated/%s v1\n%s\n" % (table, "\t".join(cols)) +
                  "".join("\t".join(str(r[c]) for c in cols) + "\n" for r in rows))
        write(os.path.join(src, "oracle/xengine/diagnosed.tsv"),
              "kind\tset\tcard\tfield\twrong\tcr\tevidence\tupstream\tticket\n")
        committed = build_mod.read_committed(directory=src)

        self.pub = os.path.join(self.tmp, "carddb.sqlite")
        build_mod.build(self.pub, self.schema, self.a.dir, None, committed,
                        revs={"gorge_pin": "8f7095353a64"})
        self.priv = os.path.join(self.tmp, "carddb-private.sqlite")
        os.environ["CARDDB_PRIVATE_REMOTE"] = os.path.join(self.tmp, "private.git")
        self.addCleanup(os.environ.pop, "CARDDB_PRIVATE_REMOTE", None)
        git("init", "--bare", "--quiet", os.environ["CARDDB_PRIVATE_REMOTE"])
        p = Store("private", home=self.home, schema=self.schema).init()
        p.put("decks", [{"deck_id": "d1", "oracle_id": OID_A, "count": 4},
                        {"deck_id": "d1", "oracle_id": OID_C, "count": 2},
                        {"deck_id": "d2", "oracle_id": OID_A, "count": 4}], writer="decklib")
        p.put("text", [{"oracle_id": OID_A, "oracle_text": "Deals 3 damage to any target."}],
              writer="scryfall")
        p.put("tags", [{"oracle_id": OID_A, "tag": "removal", "tagger": "t"}], writer="scryfall")
        p.put("edhrec", [{"oracle_id": OID_A, "edhrec_rank": 42}], writer="scryfall")
        p.put("prims", [{"oracle_id": OID_A, "prims": "api:damage"}], writer="scryfall")
        p.put("values", [{"oracle_id": OID_A, "value": "1.50"}], writer="values")
        p.put("labels_private", [{"oracle_id": OID_A, "label": "cedh", "note": "fixture"}],
              writer="values")
        build_mod.build(self.priv, self.schema, self.a.dir, p.dir, committed,
                        revs={"gorge_pin": "8f7095353a64"})

    def run_cli(self, *args, private=False):
        db = self.priv if private else self.pub
        argv = list(args) + ["--db", db] + (["--private"] if private else [])
        return self.cli(self.home, *argv)

    def golden(self, name, text):
        path = os.path.join(GOLDEN, name)
        if UPDATE:
            write(path, text)
        self.assertEqual(text, read(path), "golden %s differs (CARDDB_UPDATE_GOLDEN=1 rewrites it)" % name)

    def test_card(self):
        r = self.run_cli("card", "lightning bolt")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_card.txt", r.stdout)
        r = self.run_cli("card", "Lightning Bolt", private=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_card_private.txt", r.stdout)

    def test_search(self):
        r = self.run_cli("search", "dwarf")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_search.txt", r.stdout)
        self.assertIn("Dáin Ironfoot", r.stdout)
        for args, want in (
            (("bolt", "--legal", "modern", "--set", "lea", "--status", "level=A"), "Lightning Bolt"),
            (("burn", "--label", "burn"), "Lightning Bolt"),
            (("bolt", "--label", "role:burn"), "Lightning Bolt"),
            (("bolt", "--legal", "legacy"), None),
        ):
            r = self.run_cli("search", *args)
            self.assertEqual(r.returncode, 0, r.stderr)
            if want:
                self.assertIn(want, r.stdout)
            else:
                self.assertEqual(len(r.stdout.splitlines()), 1)  # the column header only
        r = self.run_cli("search", "bolt", "--status", "engine=gorge")
        self.assertEqual(r.returncode, 2, r.stderr)
        self.assertIn("no column", r.stderr)

    def test_blockers(self):
        r = self.run_cli("blockers", "--format", "modern")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_blockers.txt", r.stdout)
        self.assertNotIn("Digital Only", r.stdout)
        self.assertNotIn("Fun Card", r.stdout)
        r = self.run_cli("blockers", "--format", "modern", "--engine", "proven")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_blockers_proven.txt", r.stdout)
        r = self.run_cli("blockers", "--format", "modern", "--by", "decks", private=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_blockers_decks.txt", r.stdout)
        r = self.run_cli("blockers", "--format", "modern", private=True)  # private default: decks
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout, read(os.path.join(GOLDEN, "query_blockers_decks.txt")))
        r = self.run_cli("blockers", "--format", "modern", "--engine", "gorge")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("Dáin Ironfoot", r.stdout)
        self.assertNotIn("Lightning Bolt", r.stdout)  # it has an agreeing status row
        r = self.run_cli("blockers", "--format", "modern", "--by", "decks")
        self.assertEqual(r.returncode, 1, r.stderr)

    def test_proven(self):
        r = self.run_cli("proven", "--format", "modern")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_proven.txt", r.stdout)
        self.assertNotIn("Fire // Ice", r.stdout)  # C1 is below the C2 proven level

    def test_deck(self):
        r = self.run_cli("deck", "d1", private=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_deck.txt", r.stdout)
        r = self.run_cli("deck", "d1")  # the public build has no decks table
        self.assertEqual(r.returncode, 1, r.stderr)

    def test_freshness(self):
        base = ("freshness", "--public-dir", os.path.join(self.home, "public"),
                "--asset-meta", self.asset, "--sla", self.sla)
        r = self.run_cli(*base, "--now", "2026-11-19T00:00:00Z")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.golden("query_freshness.txt", r.stdout)
        r = self.run_cli(*base, "--now", "2026-11-21T12:00:00Z", "--tick",
                         "--unresolved-baseline", "0")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertTrue(all(line.startswith("!! ") for line in r.stdout.splitlines()))
        self.golden("query_freshness_tick.txt", r.stdout)

    def test_freshness_reads_the_clone_without_a_build(self):
        r = self.cli(self.home, "freshness", "--public-dir", os.path.join(self.home, "public"),
                     "--asset-meta", self.asset, "--sla", self.sla, "--now", "2026-11-19T00:00:00Z")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("writer catalogue ran 2026-11-19", r.stdout)

    def test_sql(self):
        r = self.run_cli("sql", "SELECT name, mana_value FROM cards ORDER BY name")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_sql.txt", r.stdout)
        for bad in ("DELETE FROM cards", "UPDATE cards SET name='x'", "PRAGMA user_version",
                    "SELECT 1; DELETE FROM cards"):
            with self.subTest(bad):
                r = self.run_cli("sql", bad)
                self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
                self.assertIn("sql:", r.stderr)
        c = sqlite3.connect(self.pub)
        self.addCleanup(c.close)
        self.assertEqual(c.execute("SELECT count(*) FROM cards").fetchone()[0], 6)

    def test_export(self):
        r = self.run_cli("export", "cards", "--tsv")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_export_cards.txt", r.stdout)
        r = self.run_cli("export", "proven", "--tsv")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_export_proven.txt", r.stdout)
        r = self.run_cli("export", "curated/labels", "--tsv")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("staple", r.stdout)
        r = self.run_cli("export", "cards")
        self.assertEqual(r.returncode, 2, r.stderr)

    def test_missing_database_and_unknown_view(self):
        r = self.cli(self.home, "card", "Lightning Bolt", "--db", os.path.join(self.tmp, "nope.sqlite"))
        self.assertEqual(r.returncode, 2, r.stderr)
        self.assertIn("no built database", r.stderr)
        r = self.run_cli("export", "nope", "--tsv")
        self.assertEqual(r.returncode, 2, r.stderr)
        self.assertIn("unknown view", r.stderr)

    def test_writers_config_is_allowed_under_schema(self):
        files = [("schema/writers.tsv", "#carddb-writers v1\nwriter\tsla_hours\ncatalogue\t24\n")]
        self.assertEqual(shard.validate_tree(self.schema, "public", files), [])
        self.assertEqual(boundary_tree(self.schema, files), [])
        real = list(shard.tree_files(os.path.join(ROOT, "carddb")))
        self.assertEqual(shard.validate_tree(self.schema, "public", real), [])
        self.assertEqual(boundary_tree(self.schema, real), [])
        bad = [("schema/writers.tsv", "#carddb-writers v1\nwriter\tsla_hours\ncatalogue\tzero\n")]
        self.assertTrue(shard.validate_tree(self.schema, "public", bad))
        self.assertTrue(boundary_tree(self.schema, bad))

    def test_writer_sla_parser(self):
        self.assertEqual(query_mod.load_writers(self.sla)["catalogue"], 24)
        bad = os.path.join(self.tmp, "bad-writers.tsv")
        write(bad, "#carddb-writers v1\nwriter\tsla_hours\ncatalogue\tzero\n")
        with self.assertRaises(query_mod.QueryError):
            query_mod.load_writers(bad)
