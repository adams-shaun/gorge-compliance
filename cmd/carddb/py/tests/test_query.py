import json
import os
import shutil
import sqlite3
import unittest

from carddb import build as build_mod
from carddb import keylegs
from carddb import query as query_mod
from carddb import schema as schema_mod
from carddb import shard
from carddb.boundary import boundary_tree
from carddb.store import Store

import parquet_export

from .helpers import Base, GOLDEN, OID_A, OID_B, OID_C, ROOT, git
from .test_cert import cert_row

UPDATE = os.environ.get("CARDDB_UPDATE_GOLDEN") == "1"

OID_D = "de" + "3" * 30
OID_E = "ef" + "4" * 30
OID_F = "f0" + "5" * 30
OID_G = "ab" + "6" * 30

GORGE = "8f7095353a64"


def keyproof_row(oid, key, state, leg="xmage", **kw):
    """One leg row of the keyproof ledger (the new column set)."""
    row = {
        "oracle_id": oid, "key": key, "leg": leg, "desc": "-", "state": state, "reason": "-", "refs": "-",
        "source": "-", "agree_items": 0, "diff_items": 0, "witness": "-", "sig": "-", "cite": "-",
        "ticket": "-", "seats": 2, "multiplayer": "false", "gorge_sha": GORGE, "corpus_pin": "-",
        "script_sha": "-", "xmage_ref": "-", "forge_ref": "-", "measured": "2026-11-23", "replayed": "-",
        "replayed_sha": "-", "writer": "xproof",
    }
    row.update(kw)
    return row


def keyproof_rows(oid, key, state, **kw):
    """The leg rows of one key as the proof loop's per-key row would split: agreed -> an agreed row per
    engine in refs (FX when none is given), diagnosed -> a cite row, any other state -> xmage and forge."""
    refs = kw.pop("refs", "FX")
    if state == "agreed":
        return [keyproof_row(oid, key, state, leg=leg, refs=r, **kw)
                for leg, r in (("xmage", "X"), ("forge", "F")) if r in refs]
    if state == "diagnosed":
        return [keyproof_row(oid, key, state, leg="cite", **kw)]
    return [keyproof_row(oid, key, state, leg=leg, **kw) for leg in ("xmage", "forge")]


def flat(*groups):
    return [r for g in groups for r in g]


CURATED = {
    "labels": [{"oracle_id": OID_A, "label": "staple", "note": "format fixture"}],
    "game-changers": [{"oracle_id": OID_A, "added": "2023-01-01", "source": "https://example.test/wotc",
                       "cited": "2025-02-11"}],
    "bracket-flags": [{"oracle_id": OID_A, "flag": "mass-land-denial", "source": "https://example.test/wotc",
                       "cited": "2025-02-11"}],
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


class QueryFixture(Base):
    """One fixture build (the leg-row ledger); Query and TestLegacyNumbersHold read it."""

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
        ] + [
            # Fire // Ice is legal somewhere (so tier 4, not tier 5) but banned in modern.
            {"oracle_id": OID_B, "format": "legacy", "status": "legal"},
        ], writer="catalogue")
        self.a.put("status", [
            {"oracle_id": OID_A, "level": "A", "verdict": "agree", "gorge_pin": "8f7095353a64",
             "measured": "2026-11-19", "writer": "loop"},
            {"oracle_id": OID_B, "level": "B", "verdict": "diverge", "gorge_pin": "8f7095353a64",
             "measured": "2026-11-19", "writer": "loop"},
        ], writer="loop")
        self.a.put("cert", [
            cert_row(OID_A, "Lightning Bolt", "C2", measured="2026-11-19"),
            cert_row(OID_B, "Fire // Ice", "C1", measured="2026-11-19"),
        ], writer="certification")
        self.load_keyproof()
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
        write(self.sla, "#carddb-writers v1\nwriter\tsla_hours\ttables\tdoc\n" + "".join(
            "%s\t%d\tcards\tfixture\n" % (w, 24 if w == "catalogue" else 1000)
            for w in ("catalogue", "certification", "ci", "farm", "labels", "loop", "resolve", "xproof")))
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

    def load_keyproof(self):
        self.a.put("keyproof", flat(
            keyproof_rows(OID_A, "cast", "agreed", desc="Mana", refs="FX", source="scen", agree_items=2),
            keyproof_rows(OID_A, "f0/a0", "agreed", desc="Damage", refs="X", source="start", agree_items=1,
                          witness="xs:LEA:lightning-bolt:f0a0:s1"),
            keyproof_rows(OID_B, "cast", "agreed", desc="Mana", refs="FX", source="scen", agree_items=2),
            keyproof_rows(OID_B, "f1/a0", "owed", desc="Forage", reason="no-generator"),
            keyproof_rows(OID_C, "cast", "diverged", desc="Mana", diff_items=3, sig="a1b2c3d4e5f6",
                          ticket="xp-a1b2c3d4e5f6"),
            keyproof_rows(OID_C, "combat#0.attack", "diagnosed", sig="a1b2c3d4e5f6", cite="CR 702.4b"),
            keyproof_rows(OID_E, "cast", "owed", desc="Mana", reason="no-generator"),
        ), writer="xproof")

    def run_cli(self, *args, private=False):
        db = self.priv if private else self.pub
        argv = list(args) + ["--db", db] + (["--private"] if private else [])
        return self.cli(self.home, *argv)

    def golden(self, name, text):
        path = os.path.join(GOLDEN, name)
        if UPDATE:
            write(path, text)
        self.assertEqual(text, read(path), "golden %s differs (CARDDB_UPDATE_GOLDEN=1 rewrites it)" % name)

class Query(QueryFixture):
    """Every read verb against the fixture build."""

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

    def test_need(self):
        r = self.run_cli("need")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_need.txt", r.stdout)
        self.assertIn("Dáin Ironfoot", r.stdout)
        self.assertIn("Fire // Ice", r.stdout)
        self.assertNotIn("Fun Card", r.stdout)  # curated exclusion
        self.assertNotIn("Lightning Bolt", r.stdout)  # every key agreed
        r = self.run_cli("need", "--format", "modern")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_need_modern.txt", r.stdout)
        self.assertNotIn("Fire // Ice", r.stdout)  # banned in modern
        r = self.run_cli("need", "--limit", "1")
        self.assertEqual(len(r.stdout.splitlines()), 2, r.stdout)  # header and one row
        r = self.run_cli("need", private=True)  # the edhrec tier-3 path over a private build
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout, self.run_cli("need").stdout)

    def test_need_recredit_lists_only_keys_one_engine_backs(self):
        r = self.run_cli("need", "--recredit", "X")
        self.assertEqual(r.returncode, 0, r.stderr)
        lines = r.stdout.splitlines()
        self.assertEqual(lines[0], self.run_cli("need").stdout.splitlines()[0])  # same columns as need
        rows = [l.split("\t") for l in lines[1:]]
        # Bolt's f0/a0 is agreed by XMage alone; its FX cast and the owed/diverged keys are not listed
        self.assertEqual([(x[2], x[3], x[5]) for x in rows], [("Lightning Bolt", "f0/a0", "agreed")], r.stdout)
        r = self.run_cli("need", "--recredit", "FX", "--format", "modern")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(sorted((x.split("\t")[2], x.split("\t")[3]) for x in r.stdout.splitlines()[1:]),
                         [("Lightning Bolt", "cast")], r.stdout)  # Fire // Ice is banned in modern
        r = self.run_cli("need", "--recredit", "X+P")
        self.assertNotEqual(r.returncode, 0)
        # a legacy refs X+P (a P witness beside XMage) counts as XMage alone: split_legacy strips the +P
        # into a lone xmage leg (TestKeyLegs.test_split_table), and keyproof_keys derives refs from the legs

    def test_per_key_verbs_ask_to_rebuild_a_build_without_keyproof_keys(self):
        # a SQLite built before keyproof had legs has the table but not the rollup view
        db = os.path.join(self.tmp, "old-build.sqlite")
        shutil.copy(self.pub, db)
        conn = sqlite3.connect(db)
        conn.execute("DROP VIEW keyproof_keys")
        conn.commit()
        conn.close()
        for argv in (("need",), ("proof", "--set", "lea")):
            r = self.cli(self.home, *argv, "--db", db)
            self.assertEqual(r.returncode, 2, r.stdout)  # QueryError's default, as "has no keyproof table"
            self.assertIn("carddb %s: %s: this build predates the keyproof leg rows" % (argv[0], argv[0]), r.stderr)
            self.assertIn("rebuild it with carddb build", r.stderr)
        r = self.cli(self.home, "docs-sets", "--db", db, "--out", os.path.join(self.tmp, "docs-sets"))
        self.assertNotEqual(r.returncode, 0, r.stdout)
        self.assertIn("predates the keyproof leg rows", r.stderr)
        self.assertNotIn("Traceback", r.stderr)

    def test_need_edhrec_rank_only_orders_tier_three(self):
        # Bolt (tier 2, edhrec rank 42, released first) and Dain (tier 2, no edhrec, released later)
        # both owe a key: the edhrec rank must not push Bolt behind Dain.
        db = os.path.join(self.tmp, "need-rank.sqlite")
        shutil.copy(self.priv, db)
        conn = sqlite3.connect(db)
        cols = [r[1] for r in conn.execute("PRAGMA table_info(keyproof)")]
        rows = keyproof_rows(OID_A, "f0/a1", "owed", desc="Forage", reason="no-generator")
        conn.executemany("INSERT INTO keyproof (%s) VALUES (%s)" % (
            ",".join('"%s"' % c for c in cols), ",".join("?" * len(cols))), [[row[c] for c in cols] for row in rows])
        conn.execute("UPDATE cards SET first_released=? WHERE oracle_id=?", ("1993-08-05", OID_A))
        conn.execute("UPDATE cards SET first_released=? WHERE oracle_id=?", ("2000-01-01", OID_C))
        conn.commit()
        conn.close()
        r = self.cli(self.home, "need", "--db", db, "--private")
        self.assertEqual(r.returncode, 0, r.stderr)
        rows = [l.split("\t") for l in r.stdout.splitlines()[1:]]
        self.assertEqual([(x[0], x[2]) for x in rows],
                         [("2", "Lightning Bolt"), ("2", "Dáin Ironfoot"), ("4", "Fire // Ice")], r.stdout)

    def test_need_legal_nowhere_is_tier_five(self):
        # Mise is printed in a funny set and a promo set, so the all-non-tournament rule does not
        # reach it; no legality row says legal in any format, so it cannot be tier 1-4. Fire // Ice
        # is legal in legacy (tier 4), and Mise must rank after it.
        db = os.path.join(self.tmp, "need-legal-nowhere.sqlite")
        shutil.copy(self.priv, db)
        conn = sqlite3.connect(db)
        conn.execute("INSERT INTO cards (oracle_id, name, layout, mana_value, type_line) "
                     "VALUES (?,?,?,?,?)", (OID_G, "Mise", "normal", 1, "Instant"))
        conn.execute("INSERT INTO sets (set_code, name, release_date, set_type) "
                     "VALUES (?,?,?,?)", ("promo", "Promo", "2020-01-01", "promo"))
        conn.executemany("INSERT INTO printings (printing_id, oracle_id, set_code, collector_number, "
                         "rarity, released) VALUES (?,?,?,?,?,?)", [
                             ("p-g1", OID_G, "funny", "2", "rare", "2020-01-01"),
                             ("p-g2", OID_G, "promo", "1", "rare", "2020-01-01"),
                         ])
        cols = [r[1] for r in conn.execute("PRAGMA table_info(keyproof)")]
        rows = keyproof_rows(OID_G, "cast", "owed", desc="Mana", reason="no-generator")
        conn.executemany("INSERT INTO keyproof (%s) VALUES (%s)" % (
            ",".join('"%s"' % c for c in cols), ",".join("?" * len(cols))), [[row[c] for c in cols] for row in rows])
        conn.commit()
        conn.close()
        r = self.cli(self.home, "need", "--db", db, "--private")
        self.assertEqual(r.returncode, 0, r.stderr)
        rows = [l.split("\t") for l in r.stdout.splitlines()[1:]]
        self.assertEqual([(x[0], x[2]) for x in rows],
                         [("2", "Dáin Ironfoot"), ("4", "Fire // Ice"), ("5", "Mise")], r.stdout)

    def test_need_restricted_only_is_legal_somewhere(self):
        # Vintage and Old School report "restricted" for tournament-legal cards with a copy limit
        # (the real catalogue has Black Lotus as vintage/oldschool restricted, no legal row), so a
        # restricted-only card must stay tier 4, not join the legal-nowhere tier 5.
        db = os.path.join(self.tmp, "need-restricted.sqlite")
        shutil.copy(self.priv, db)
        conn = sqlite3.connect(db)
        conn.execute("DELETE FROM legality WHERE oracle_id=?", (OID_B,))
        conn.execute("INSERT INTO legality (oracle_id, format, status) VALUES (?,?,?)",
                     (OID_B, "vintage", "restricted"))
        conn.commit()
        conn.close()
        r = self.cli(self.home, "need", "--db", db, "--private")
        self.assertEqual(r.returncode, 0, r.stderr)
        rows = [l.split("\t") for l in r.stdout.splitlines()[1:]]
        self.assertEqual([(x[0], x[2]) for x in rows],
                         [("2", "Dáin Ironfoot"), ("4", "Fire // Ice")], r.stdout)
        # The same shared predicate drives the format filters: a restricted card is selectable.
        r = self.cli(self.home, "need", "--db", db, "--private", "--format", "vintage")
        self.assertEqual(r.returncode, 0, r.stderr)
        rows = [l.split("\t") for l in r.stdout.splitlines()[1:]]
        self.assertEqual([(x[0], x[2]) for x in rows], [("4", "Fire // Ice")], r.stdout)

    def test_need_empty_legality_table_does_not_demote(self):
        # A partial build with no legality shard still creates the table, but with no rows. That
        # must not make every card "legal nowhere": without rows there is nothing proving it.
        db = os.path.join(self.tmp, "need-empty-legality.sqlite")
        shutil.copy(self.priv, db)
        conn = sqlite3.connect(db)
        conn.execute("DELETE FROM legality")
        conn.commit()
        conn.close()
        r = self.cli(self.home, "need", "--db", db, "--private")
        self.assertEqual(r.returncode, 0, r.stderr)
        rows = {x[2]: x[0] for x in (l.split("\t") for l in r.stdout.splitlines()[1:])}
        self.assertEqual(rows, {"Dáin Ironfoot": "4", "Fire // Ice": "4"}, r.stdout)

    def test_proven_by_keys_rejects_output_formats(self):
        for fmt in ("tsv", "json", "count"):
            r = self.run_cli("proven", "--by", "keys", "--format", fmt)
            self.assertNotEqual(r.returncode, 0, fmt)
            self.assertIn("legality format", r.stderr)

    def test_proof(self):
        r = self.run_cli("proof", "--set", "lea")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_proof.txt", r.stdout)
        r = self.run_cli("proof", "--set", "LEA")
        self.assertEqual(r.stdout, self.run_cli("proof", "--set", "lea").stdout)
        r = self.run_cli("proven", "--by", "keys")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.golden("query_proven_keys.txt", r.stdout)
        # the fixture ledger has no lockstep leg, so the approved rule proves no card (the old
        # rule's count survives only as the proof section's legacy_proven)
        self.assertNotIn("Lightning Bolt", r.stdout)
        self.assertNotIn("Dáin Ironfoot", r.stdout)
        self.assertNotIn("Fun Card", r.stdout)

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
        good = ("#carddb-writers v1\nwriter\tsla_hours\ttables\tdoc\n"
                "catalogue\t24\tcards\tfixture\n")
        files = [("schema/writers.tsv", good)]
        self.assertEqual(shard.validate_tree(self.schema, "public", files), [])
        self.assertEqual(boundary_tree(self.schema, files), [])
        real = list(shard.tree_files(os.path.join(ROOT, "carddb")))
        self.assertEqual(shard.validate_tree(self.schema, "public", real), [])
        self.assertEqual(boundary_tree(self.schema, real), [])
        bad = [("schema/writers.tsv", "#carddb-writers v1\nwriter\tsla_hours\ttables\tdoc\n"
                "catalogue\tzero\tcards\tfixture\n")]
        self.assertTrue(shard.validate_tree(self.schema, "public", bad))
        self.assertTrue(boundary_tree(self.schema, bad))

    def test_writers_shape_is_fixed(self):
        # The one non-declaration under schema/ cannot grow a column, so it
        # cannot park oracle_text (or a private table) under schema/.
        header = "#carddb-writers v1\nwriter\tsla_hours\ttables\tdoc\n"
        bad = {
            "extra-forbidden-column": header.replace("\tdoc\n", "\toracle_text\n")
            + "catalogue\t24\tDraw a card.\tfixture\n",
            "extra-private-table-column": header.replace("\tdoc\n", "\ttext\n")
            + "catalogue\t24\tprivate text\tfixture\n",
            "over-wide-header": header.replace("\tdoc\n", "\tdoc\textra\n")
            + "catalogue\t24\tcards\tfixture\tmore\n",
            "missing-doc": "#carddb-writers v1\nwriter\tsla_hours\ttables\ncatalogue\t24\tcards\n",
            "duplicate-writer": header + "catalogue\t24\tcards\ta\ncatalogue\t36\tcards\tb\n",
            "plus-sla": header + "catalogue\t+24\tcards\tfixture\n",
            "leading-zero-sla": header + "catalogue\t01\tcards\tfixture\n",
            "huge-sla": header + "catalogue\t999999999999999999999\tcards\tfixture\n",
        }
        for name, text in bad.items():
            with self.subTest(name):
                files = [("schema/writers.tsv", text)]
                self.assertTrue(shard.validate_tree(self.schema, "public", files), name)
                self.assertTrue(boundary_tree(self.schema, files), name)
                path = os.path.join(self.tmp, name + ".tsv")
                write(path, text)
                with self.assertRaises(query_mod.QueryError):
                    query_mod.load_writers(path)

    def test_writer_sla_parser(self):
        self.assertEqual(query_mod.load_writers(self.sla)["catalogue"], 24)
        committed = query_mod.load_writers(os.path.join(ROOT, "carddb", "schema", "writers.tsv"))
        # ids the schema declarations and merge driver actually write
        self.assertTrue({"ci", "cert", "loop"} <= set(committed), sorted(committed))
        bad = os.path.join(self.tmp, "bad-writers.tsv")
        write(bad, "#carddb-writers v1\nwriter\tsla_hours\ttables\tdoc\ncatalogue\tzero\tcards\tfixture\n")
        with self.assertRaises(query_mod.QueryError):
            query_mod.load_writers(bad)

    def test_malformed_inputs_are_clean_errors(self):
        # A valid-JSON non-object asset metadata is "no metadata", not an
        # AttributeError traceback.
        bad_meta = os.path.join(self.tmp, "list-asset.json")
        write(bad_meta, "[]\n")
        r = self.run_cli("freshness", "--public-dir", os.path.join(self.home, "public"),
                         "--asset-meta", bad_meta, "--sla", self.sla,
                         "--now", "2026-11-19T00:00:00Z")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertNotIn("Traceback", r.stderr)
        self.assertIn("no carddb-latest metadata", r.stdout)

        # A missing SLA file is the tool's clean error.
        r = self.run_cli("freshness", "--public-dir", os.path.join(self.home, "public"),
                         "--asset-meta", self.asset, "--sla", os.path.join(self.tmp, "nope.tsv"),
                         "--now", "2026-11-19T00:00:00Z")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertNotIn("Traceback", r.stderr)
        self.assertIn("cannot read writer SLA file", r.stdout)

        # A cell past Python's int-conversion limit is a violation, not a traceback.
        huge = os.path.join(self.tmp, "huge-sla.tsv")
        write(huge, "#carddb-writers v1\nwriter\tsla_hours\ttables\tdoc\ncatalogue\t1" + "0" * 5000
              + "\tcards\tfixture\n")
        files = [("schema/writers.tsv", shard.read_text(huge))]
        self.assertTrue(shard.validate_tree(self.schema, "public", files))
        r = self.run_cli("freshness", "--public-dir", os.path.join(self.home, "public"),
                         "--asset-meta", self.asset, "--sla", huge, "--now", "2026-11-19T00:00:00Z")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertNotIn("Traceback", r.stderr)
        self.assertIn("out of range", r.stdout)

    def test_writers_file_is_read_like_the_validators_read_it(self):
        # One text for validate_tree and load_writers: whatever the validators
        # accept the consumer accepts, and the other way round.
        header = "#carddb-writers v1\nwriter\tsla_hours\ttables\tdoc\n"
        cases = {
            "lone-cr-in-cell": (header + "catalogue\t24\tcards\ta\rb\n").encode(),
            "invalid-utf8": (header + "catalogue\t24\tcards\t").encode() + b"\xff\n",
            "crlf": (header + "catalogue\t24\tcards\tfixture\n").replace("\n", "\r\n").encode(),
        }
        for name, raw in cases.items():
            with self.subTest(name):
                path = os.path.join(self.tmp, name + ".tsv")
                with open(path, "wb") as f:
                    f.write(raw)
                rel = [("schema/writers.tsv", shard.read_text(path))]
                validators_ok = not shard.validate_tree(self.schema, "public", rel)
                self.assertEqual(validators_ok, not boundary_tree(self.schema, rel), name)
                try:
                    query_mod.load_writers(path)
                    consumer_ok = True
                except query_mod.QueryError:
                    consumer_ok = False
                self.assertEqual(validators_ok, consumer_ok, name)
                if consumer_ok:
                    r = self.run_cli("freshness", "--public-dir", os.path.join(self.home, "public"),
                                     "--asset-meta", self.asset, "--sla", path,
                                     "--now", "2026-11-19T00:00:00Z")
                    self.assertNotIn("Traceback", r.stderr)

        # Metadata for the other scope is refused rather than used as this
        # scope's age/baseline.
        other_meta = os.path.join(self.tmp, "private-asset.json")
        write(other_meta, json.dumps({"scope": "private",
                                      "published_at": "2026-11-19T00:00:00Z"}) + "\n")
        r = self.run_cli("freshness", "--public-dir", os.path.join(self.home, "public"),
                         "--asset-meta", other_meta, "--sla", self.sla,
                         "--now", "2026-11-19T00:00:00Z")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("metadata is for scope 'private', want 'public'", r.stdout)
        self.assertIn("no carddb-latest metadata", r.stdout)

    def test_freshness_public_build_needs_only_public_writers(self):
        # A realistic public store: a row for every public-capable writer, one id
        # of each alias pair, nothing from the private-only writers.
        reg = query_mod.load_registry(os.path.join(ROOT, "carddb", "schema", "writers.tsv"))
        self.assertTrue({"scryfall", "values", "decklib"} <= set(reg))
        aliases = query_mod._alias_groups(reg)
        self.assertEqual({w: h for w, h in aliases.items() if w != h},
                         {"ci": "compliance", "cert": "certification", "loop": "evloop"})
        home = os.path.join(self.tmp, "public-only")
        os.makedirs(home)
        pub = Store("public", home=home, schema=self.schema)
        pub.init()
        public_writers = [w for w in sorted(reg)
                          if w not in aliases or aliases[w] == w
                          if any(t not in query_mod.PRIVATE_TABLE_NAMES for t in reg[w][1])]
        self.assertNotIn("scryfall", public_writers)
        pub.put("_meta/writers", [{"writer": w, "table": "_meta/writers", "version": "t",
                                   "ran": "2026-11-19"} for w in public_writers])
        write(os.path.join(home, "carddb-latest.json"),
              json.dumps({"scope": "public", "published_at": "2026-11-19T00:00:00Z",
                          "unresolved": 1}) + "\n")
        args = ("freshness", "--public-dir", pub.dir, "--now", "2026-11-19T00:00:00Z")
        r = self.cli(home, *args)
        self.assertNotIn("no _meta/writers row", r.stdout)
        self.assertNotIn("Traceback", r.stderr)
        # the one alias id recorded in place of its job name still satisfies the job
        for dropped, alias in (("compliance", "ci"), ("certification", "cert"), ("evloop", "loop")):
            rows = [{"writer": alias if w == dropped else w, "table": "_meta/writers",
                     "version": "t", "ran": "2026-11-19"} for w in public_writers]
            h2 = os.path.join(self.tmp, "alias-" + alias)
            os.makedirs(h2)
            p2 = Store("public", home=h2, schema=self.schema)
            p2.init()
            p2.put("_meta/writers", rows)
            write(os.path.join(h2, "carddb-latest.json"),
                  json.dumps({"scope": "public", "published_at": "2026-11-19T00:00:00Z"}) + "\n")
            r = self.cli(h2, "freshness", "--public-dir", p2.dir, "--now", "2026-11-19T00:00:00Z")
            self.assertNotIn("no _meta/writers row", r.stdout, alias)
        # a public writer that never ran is still flagged
        rows = [{"writer": w, "table": "_meta/writers", "version": "t", "ran": "2026-11-19"}
                for w in public_writers if w != "catalogue"]
        h3 = os.path.join(self.tmp, "missing")
        os.makedirs(h3)
        p3 = Store("public", home=h3, schema=self.schema)
        p3.init()
        p3.put("_meta/writers", rows)
        r = self.cli(h3, "freshness", "--public-dir", p3.dir, "--now", "2026-11-19T00:00:00Z")
        self.assertIn("!! writer catalogue has no _meta/writers row", r.stdout)
        self.assertNotIn("writer scryfall has no", r.stdout)

    def test_freshness_uses_the_committed_registry_and_scope_metadata(self):
        # No --sla and no --asset-meta: the default path loads the committed
        # registry and each scope's own metadata file.
        sla = query_mod.load_writers(os.path.join(ROOT, "carddb", "schema", "writers.tsv"))
        home = os.path.join(self.tmp, "registry")
        os.makedirs(home)
        priv_remote = os.path.join(self.tmp, "private-registry.git")
        git("init", "--bare", "--quiet", priv_remote)
        old = os.environ.get("CARDDB_PRIVATE_REMOTE")
        os.environ["CARDDB_PRIVATE_REMOTE"] = priv_remote

        def restore():
            if old is None:
                os.environ.pop("CARDDB_PRIVATE_REMOTE", None)
            else:
                os.environ["CARDDB_PRIVATE_REMOTE"] = old
        self.addCleanup(restore)
        pub = Store("public", home=home, schema=self.schema)
        pub.init()
        priv = Store("private", home=home, schema=self.schema)
        priv.init()
        rows = [{"writer": w, "table": "_meta/writers", "version": "test", "ran": "2026-11-19"}
                for w in sorted(sla)]
        pub.put("_meta/writers", rows)
        priv.put("_meta/writers", rows)
        write(os.path.join(home, "carddb-latest.json"),
              json.dumps({"scope": "public", "published_at": "2026-11-18T00:00:00Z",
                          "unresolved": 1}) + "\n")
        write(os.path.join(home, "carddb-private-latest.json"),
              json.dumps({"scope": "private", "published_at": "2026-11-19T00:00:00Z",
                          "unresolved": 0}) + "\n")

        public = self.cli(home, "freshness", "--public-dir", pub.dir, "--now", "2026-11-19T00:00:00Z")
        self.assertNotIn("not registered", public.stdout)
        self.assertNotIn("no _meta/writers row", public.stdout)
        self.assertIn("writer ci ran 2026-11-19", public.stdout)
        self.assertIn("carddb-latest 24.0h old (SLA 6h)", public.stdout)

        private = self.cli(home, "freshness", "--private", "--public-dir", priv.dir,
                           "--now", "2026-11-19T00:00:00Z")
        self.assertNotIn("not registered", private.stdout)
        self.assertIn("carddb-private-latest 0.0h old (SLA 6h)", private.stdout)


def legacy_row(oid, key, state, **kw):
    """A row of the old one-row-per-key keyproof.tsv (the 20 legacy columns)."""
    row = {
        "oracle_id": oid, "key": key, "desc": "-", "state": state, "reason": "-", "refs": "-",
        "source": "-", "agree_items": 0, "diff_items": 0, "witness": "-", "sig": "-", "cite": "-",
        "ticket": "-", "gorge_sha": GORGE, "corpus_pin": "-", "script_sha": "-", "xmage_ref": "-",
        "forge_ref": "-", "measured": "2026-11-23", "writer": "xproof",
    }
    row.update(kw)
    return row


def write_legacy_shards(root, rows):
    """Write rows (legacy_row dicts) as legacy-shaped keyproof shards under a clone; -> shard paths."""
    by = {}
    for r in rows:
        by.setdefault(r["oracle_id"][:2], []).append(r)
    paths = []
    for stem, rs in sorted(by.items()):
        cells = [["-" if r[c] in (None, "") else str(r[c]) for c in keylegs.LEGACY_COLUMNS]
                 for r in sorted(rs, key=lambda r: (r["oracle_id"], r["key"]))]
        path = os.path.join(root, "keyproof", stem + ".tsv")
        write(path, shard.render("keyproof", 1, list(keylegs.LEGACY_COLUMNS), cells))
        paths.append(path)
    return paths


class TestLegacyNumbersHold(QueryFixture):
    """The fixture ledger written in the legacy shape and migrated: need, proof and proven --by keys
    print the same bytes the goldens recorded before the ledger had legs."""

    def load_keyproof(self):
        write_legacy_shards(self.a.dir, [
            legacy_row(OID_A, "cast", "agreed", desc="Mana", refs="FX", source="scen", agree_items=2),
            legacy_row(OID_A, "f0/a0", "agreed", desc="Damage", refs="X", source="start", agree_items=1,
                       witness="xs:LEA:lightning-bolt:f0a0:s1"),
            legacy_row(OID_B, "cast", "agreed", desc="Mana", refs="FX", source="scen", agree_items=2),
            legacy_row(OID_B, "f1/a0", "owed", desc="Forage", reason="no-generator"),
            legacy_row(OID_C, "cast", "diverged", desc="Mana", diff_items=3, sig="a1b2c3d4e5f6",
                       ticket="xp-a1b2c3d4e5f6"),
            # the real ledger's shape: every diagnosed row has cite '-' (a cite leg owed diagnosed-no-cite),
            # and the key must still roll up as diagnosed, not owed
            legacy_row(OID_C, "combat#0.attack", "diagnosed", sig="a1b2c3d4e5f6"),
            legacy_row(OID_E, "cast", "owed", desc="Mana", reason="no-generator"),
        ])
        self.assertEqual(keylegs.migrate(self.a), 12)

    def check(self, name, text):
        self.assertEqual(text, read(os.path.join(GOLDEN, name)), "golden %s differs after the migration" % name)

    def test_need(self):
        self.check("query_need.txt", self.run_cli("need").stdout)
        self.check("query_need_modern.txt", self.run_cli("need", "--format", "modern").stdout)

    def test_proof(self):
        self.check("query_proof.txt", self.run_cli("proof", "--set", "lea").stdout)

    def test_proven_keys(self):
        self.check("query_proven_keys.txt", self.run_cli("proven", "--by", "keys").stdout)


PROVEN_NEW = "2026-11-23"  # every fresh row is measured this day: the table's newest date
PROVEN_OLD = "2026-11-03"  # 20 days before the newest row, well outside the 7-day window


class TestProvenRule(Base):
    """The approved proven rule (lockstep plus one independent leg fresh at need_seats) over one
    fixture card per case, asserted against the key_proven and card_proven views."""

    def setUp(self):
        super().setUp()
        self.home = os.path.join(self.tmp, "proven")
        self.a = self.machine("proven", init=True)
        self.oid = {}
        cards, rows = [], []

        def card(name, i):
            self.oid[name] = oid = "%02x" % i + "9" * 30
            cards.append({"oracle_id": oid, "name": name, "layout": "normal", "mana_value": 1,
                          "type_line": "Instant"})
            return oid

        def key(name, keyname, *legs):
            for leg, state, kw in legs:
                rows.append(keyproof_row(self.oid[name], keyname, state, leg=leg, **kw))

        card("Proven Witnessed", 0x10)  # xmage witness + forge + lockstep, all fresh
        key("Proven Witnessed", "cast",
            ("xmage", "agreed", {"refs": "X", "witness": "xs:w1"}),
            ("forge", "agreed", {"refs": "F", "witness": "fo:w2"}),
            ("lockstep", "agreed", {}))
        card("Forge Only", 0x11)  # no independent leg: forge plus lockstep
        key("Forge Only", "cast",
            ("forge", "agreed", {"refs": "F", "witness": "fo:w3"}),
            ("lockstep", "agreed", {}))
        card("Unwitnessed", 0x12)  # agreed xmage with an empty witness, plus lockstep
        key("Unwitnessed", "cast",
            ("xmage", "agreed", {"refs": "X", "witness": "-"}),
            ("lockstep", "agreed", {}))
        card("Stale Xmage", 0x13)  # xmage measured 20 days old, never replayed
        key("Stale Xmage", "cast",
            ("xmage", "agreed", {"refs": "X", "witness": "xs:w4", "measured": PROVEN_OLD}),
            ("lockstep", "agreed", {}))
        card("Replayed Fresh", 0x14)  # the same rows, but replayed on the newest date
        key("Replayed Fresh", "cast",
            ("xmage", "agreed", {"refs": "X", "witness": "xs:w5", "measured": PROVEN_OLD,
                                 "replayed": PROVEN_NEW}),
            ("lockstep", "agreed", {}))
        card("Two Seat Only", 0x15)  # multiplayer key proven at two seats only
        key("Two Seat Only", "cast",
            ("lockstep", "agreed", {"seats": 4, "multiplayer": "true"}),
            ("xmage", "agreed", {"refs": "X", "witness": "xs:w6", "seats": 2, "multiplayer": "true"}))
        card("Multiplayer Proven", 0x16)  # multiplayer key at four seats everywhere
        key("Multiplayer Proven", "cast",
            ("lockstep", "agreed", {"seats": 4, "multiplayer": "true"}),
            ("xmage", "agreed", {"refs": "X", "witness": "xs:w7", "seats": 4, "multiplayer": "true"}))
        card("Cite Proven", 0x17)  # a diagnosed cite is the independent leg; cites do not age
        key("Cite Proven", "cast",
            ("cite", "diagnosed", {"cite": "CR 702.75a"}),
            ("lockstep", "agreed", {}))
        card("Diverged Forge", 0x18)  # one diverged leg holds the whole key up
        key("Diverged Forge", "cast",
            ("xmage", "agreed", {"refs": "X", "witness": "xs:w8"}),
            ("forge", "diverged", {}),
            ("lockstep", "agreed", {}))
        card("Excluded Key", 0x19)  # an all-excluded key counts neither way; the card is still proven
        key("Excluded Key", "cast",
            ("xmage", "excluded", {}), ("forge", "excluded", {}), ("lockstep", "excluded", {}))
        key("Excluded Key", "land",
            ("xmage", "agreed", {"refs": "X", "witness": "xs:w9"}),
            ("lockstep", "agreed", {}))
        card("Curated Out", 0x1a)  # proven keys, but curated-excluded: the card is not proven
        key("Curated Out", "cast",
            ("xmage", "agreed", {"refs": "X", "witness": "xs:w10"}),
            ("lockstep", "agreed", {}))
        card("Stale Forge Leg", 0x1b)  # proven, but a stale forge leg: need must still list the key
        key("Stale Forge Leg", "cast",
            ("xmage", "agreed", {"refs": "X", "witness": "xs:w11"}),
            ("forge", "stale", {}),
            ("lockstep", "agreed", {}))
        # every row is a seed row (seats 0, not excluded): the rollup says `diagnosed` (a seats-0
        # cite), so only the seed clause makes need list it; no lockstep game, so never proven
        card("Seed Only", 0x1c)
        key("Seed Only", "cast",
            ("cite", "diagnosed", {"cite": "CR 702.75a", "seats": 0}),
            ("lockstep", "owed", {"seats": 0}))
        card("Excluded Seed", 0x1d)  # every row seats 0 but excluded: not seed rows, need skips it
        key("Excluded Seed", "cast",
            ("xmage", "excluded", {"seats": 0}), ("forge", "excluded", {"seats": 0}))
        key("Excluded Seed", "land",
            ("xmage", "agreed", {"refs": "X", "witness": "xs:w12"}),
            ("lockstep", "agreed", {}))

        self.a.put("cards", cards, writer="catalogue")
        # Only the seed writer may set a key's multiplayer flag (store._keyproof_multiplayer, plan pf-ledger-04);
        # a later xproof row replacing a seeded one keeps it. So the multiplayer keys are seeded first.
        self.a.put("keyproof", [r for r in rows if r.get("multiplayer") == "true"], writer="seed")
        self.a.put("keyproof", rows, writer="xproof")
        src = os.path.join(self.tmp, "proven-src")
        write(os.path.join(src, "carddb", "curated", "exclusions.tsv"),
              "#carddb curated/exclusions v1\noracle_id\treason\n%s\tante\n" % self.oid["Curated Out"])
        db = self.db = os.path.join(self.tmp, "proven.sqlite")
        build_mod.build(db, self.schema, self.a.dir, None, build_mod.read_committed(directory=src))
        conn = sqlite3.connect(db)
        conn.row_factory = sqlite3.Row
        self.conn = conn
        self.addCleanup(conn.close)

    def key_row(self, name, keyname="cast"):
        row = self.conn.execute('SELECT * FROM key_proven WHERE oracle_id=? AND "key"=?',
                                (self.oid[name], keyname)).fetchone()
        self.assertIsNotNone(row, "no key_proven row for %s %s" % (name, keyname))
        return dict(row)

    def card_row(self, name):
        row = self.conn.execute("SELECT * FROM card_proven WHERE oracle_id=?", (self.oid[name],)).fetchone()
        self.assertIsNotNone(row, "no card_proven row for %s" % name)
        return dict(row)

    def test_witnessed_xmage_forge_and_lockstep_is_proven(self):
        r = self.key_row("Proven Witnessed")
        self.assertEqual(r["proven"], 1, r)
        self.assertEqual((r["lockstep_seats"], r["xmage_seats"], r["outside_seats"]), (2, 2, 2), r)
        self.assertEqual((r["forge_only"], r["unwitnessed"], r["two_seat_only"]), (0, 0, 0), r)
        c = self.card_row("Proven Witnessed")
        self.assertEqual(c["proven"], 1, c)
        self.assertEqual((c["keys"], c["live_keys"], c["proven_keys"]), (1, 1, 1), c)

    def test_forge_and_lockstep_only_is_forge_only(self):
        r = self.key_row("Forge Only")
        self.assertEqual(r["proven"], 0, r)
        self.assertEqual(r["forge_only"], 1, r)
        self.assertEqual((r["unwitnessed"], r["two_seat_only"]), (0, 0), r)
        self.assertEqual(self.card_row("Forge Only")["forge_only_keys"], 1)

    def test_agreed_xmage_without_a_witness_is_unwitnessed(self):
        r = self.key_row("Unwitnessed")
        self.assertEqual(r["proven"], 0, r)
        self.assertEqual(r["unwitnessed"], 1, r)
        self.assertEqual(r["unwitnessed_seats"], 2, r)
        self.assertEqual(self.card_row("Unwitnessed")["unwitnessed_keys"], 1)

    def test_xmage_older_than_the_window_is_a_stale_leg(self):
        r = self.key_row("Stale Xmage")
        self.assertEqual(r["proven"], 0, r)
        self.assertEqual(r["stale_legs"], 1, r)
        self.assertEqual(r["xmage_seats"], 0, r)
        c = self.card_row("Stale Xmage")
        self.assertEqual((c["proven"], c["stale_keys"]), (0, 1), c)

    def test_a_row_replayed_on_the_newest_date_is_fresh(self):
        r = self.key_row("Replayed Fresh")
        self.assertEqual(r["proven"], 1, r)
        self.assertEqual(r["stale_legs"], 0, r)
        self.assertEqual(self.card_row("Replayed Fresh")["proven"], 1)

    def test_multiplayer_key_proven_at_two_seats_is_two_seat_only(self):
        r = self.key_row("Two Seat Only")
        self.assertEqual((r["multiplayer"], r["need_seats"]), (1, 4), r)
        self.assertEqual(r["proven"], 0, r)
        self.assertEqual(r["two_seat_only"], 1, r)
        self.assertEqual((r["forge_only"], r["unwitnessed"]), (0, 0), r)
        c = self.card_row("Two Seat Only")
        self.assertEqual((c["proven"], c["two_seat_only_keys"]), (0, 1), c)

    def test_multiplayer_key_proven_at_four_seats_is_proven(self):
        r = self.key_row("Multiplayer Proven")
        self.assertEqual((r["multiplayer"], r["need_seats"]), (1, 4), r)
        self.assertEqual(r["proven"], 1, r)
        self.assertEqual(r["two_seat_only"], 0, r)
        self.assertEqual(self.card_row("Multiplayer Proven")["proven"], 1)

    def test_diagnosed_cite_with_a_rule_number_is_the_independent_leg(self):
        r = self.key_row("Cite Proven")
        self.assertEqual(r["cite_seats"], 4, r)
        self.assertEqual(r["proven"], 1, r)
        self.assertEqual(self.card_row("Cite Proven")["proven"], 1)

    def test_one_diverged_leg_holds_the_key_up(self):
        r = self.key_row("Diverged Forge")
        self.assertEqual(r["diverged_legs"], 1, r)
        self.assertEqual(r["proven"], 0, r)
        self.assertEqual((r["forge_only"], r["unwitnessed"], r["two_seat_only"]), (0, 0, 0), r)
        self.assertEqual(self.card_row("Diverged Forge")["proven"], 0)

    def test_an_all_excluded_key_counts_neither_way(self):
        r = self.key_row("Excluded Key", "cast")
        self.assertIsNone(r["proven"], r)
        self.assertEqual((r["legs"], r["excluded_legs"]), (3, 3), r)
        self.assertIsNone(r["forge_only"], r)
        self.assertIsNone(r["unwitnessed"], r)
        self.assertIsNone(r["two_seat_only"], r)
        c = self.card_row("Excluded Key")  # the live key still proves the card
        self.assertEqual((c["keys"], c["live_keys"], c["proven_keys"], c["proven"]), (2, 1, 1, 1), c)

    def test_a_curated_excluded_card_is_never_proven(self):
        self.assertEqual(self.key_row("Curated Out")["proven"], 1)
        self.assertEqual(self.card_row("Curated Out")["proven"], 0)

    def test_need_lists_stale_legs_and_seed_only_keys(self):
        # need goes below the rollup: a key with one stale leg (rollup agreed) and a key whose
        # every row is a seed row (seats 0) are owed work the rollup cannot see
        r = self.cli(self.home, "need", "--db", self.db)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        listed = {(l.split("\t")[2], l.split("\t")[3]) for l in r.stdout.splitlines()[1:]}
        self.assertIn(("Stale Forge Leg", "cast"), listed, r.stdout)
        self.assertIn(("Seed Only", "cast"), listed, r.stdout)
        self.assertNotIn(("Proven Witnessed", "cast"), listed, r.stdout)
        self.assertNotIn(("Multiplayer Proven", "cast"), listed, r.stdout)
        # the stale forge leg does not hold the key up under the proven rule; a seed row cannot prove it
        self.assertEqual(self.key_row("Stale Forge Leg")["proven"], 1)
        self.assertEqual(self.key_row("Seed Only")["proven"], 0)
        self.assertEqual(self.card_row("Seed Only")["proven"], 0)
        # an all-excluded key written at seats 0 is a decision, not owed work
        self.assertNotIn(("Excluded Seed", "cast"), listed, r.stdout)
        self.assertIsNone(self.key_row("Excluded Seed")["proven"])
        self.assertEqual(self.card_row("Excluded Seed")["proven"], 1)

    def test_proven_by_keys_lists_the_proven_cards_with_their_counts(self):
        r = self.cli(self.home, "proven", "--by", "keys", "--db", self.db)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        lines = r.stdout.splitlines()
        head = lines[0].split("\t")
        self.assertEqual(head, ["oracle_id", "name", "type_line", "mana_value", "keys", "proven_keys",
                                "forge_only_keys", "unwitnessed_keys"])
        got = {f[1]: dict(zip(head, f)) for f in (l.split("\t") for l in lines[1:])}
        self.assertEqual(sorted(got), sorted([
            "Proven Witnessed", "Replayed Fresh", "Multiplayer Proven", "Cite Proven",
            "Excluded Key", "Stale Forge Leg", "Excluded Seed"]), r.stdout)
        self.assertEqual((got["Proven Witnessed"]["keys"], got["Proven Witnessed"]["proven_keys"],
                          got["Proven Witnessed"]["forge_only_keys"],
                          got["Proven Witnessed"]["unwitnessed_keys"]), ("1", "1", "0", "0"))
        self.assertEqual((got["Excluded Key"]["keys"], got["Excluded Key"]["proven_keys"]), ("2", "1"))
        for absent in ("Forge Only", "Unwitnessed", "Stale Xmage", "Two Seat Only", "Diverged Forge",
                       "Curated Out", "Seed Only"):
            self.assertNotIn(absent, got, r.stdout)

    def test_every_view_column_has_a_parquet_type(self):
        # parquet_export exports every view and types columns from declarations only; an
        # aggregate/CASE column without a CAST has no declared type. Checked without pyarrow.
        objs = parquet_export.data_objects(self.conn)
        views = [name for kind, name in objs if kind == "view"]
        for v in ("key_legs", "key_proven", "card_proven", "proven_keys", "keyproof_cards"):
            self.assertIn(v, views)
        bad = []
        for _kind, name in objs:
            for col, declared, _pk in parquet_export.columns(self.conn, name):
                try:
                    parquet_export.parquet_type(declared)
                except parquet_export.ExportError as e:  # report every untyped column at once
                    bad.append("%s.%s: %s" % (name, col, e))
        self.assertEqual(bad, [])


class TestScopeCards(Base):
    """The proof denominator: Commander-legal paper cards minus curated exclusions."""

    def scope_build(self, exclusions=(), schema=None):
        a = self.machine("scope", init=True)
        a.put("cards", [
            {"oracle_id": OID_A, "name": "In Scope", "layout": "normal", "mana_value": 1,
             "type_line": "Instant", "paper": True},
            {"oracle_id": OID_B, "name": "Digital Only", "layout": "normal", "mana_value": 1,
             "type_line": "Instant", "paper": False},
            {"oracle_id": OID_C, "name": "Modern Only", "layout": "normal", "mana_value": 1,
             "type_line": "Instant", "paper": True},
            {"oracle_id": OID_D, "name": "Curated Out", "layout": "normal", "mana_value": 1,
             "type_line": "Sorcery", "paper": True},
        ], writer="catalogue")
        if schema is None or "legality" in schema.tables:
            a.put("legality", [
                {"oracle_id": OID_A, "format": "commander", "status": "legal"},
                {"oracle_id": OID_B, "format": "commander", "status": "legal"},
                {"oracle_id": OID_C, "format": "modern", "status": "legal"},
                {"oracle_id": OID_D, "format": "commander", "status": "legal"},
            ], writer="catalogue")
        src = os.path.join(self.tmp, "scope-src")
        write(os.path.join(src, "carddb/curated/exclusions.tsv"),
              "#carddb curated/exclusions v1\noracle_id\treason\n" +
              "".join("%s\t%s\n" % (r["oracle_id"], r["reason"]) for r in exclusions))
        out = os.path.join(self.tmp, "scope.sqlite")
        build_mod.build(out, schema or self.schema, a.dir, None,
                        build_mod.read_committed(directory=src))
        return out

    def test_scope_view_in_scope_is_paper_commander_legal_and_not_excluded(self):
        db = self.scope_build(exclusions=[{"oracle_id": OID_D, "reason": "ante"}])
        conn = sqlite3.connect(db)
        self.addCleanup(conn.close)
        self.assertEqual([r[1] for r in conn.execute("PRAGMA table_info(scope_cards)")],
                         ["oracle_id", "name"])
        self.assertEqual(conn.execute("SELECT oracle_id, name FROM scope_cards").fetchall(),
                         [(OID_A, "In Scope")])
        r = self.cli(self.tmp, "export", "scope", "--tsv", "--db", db)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout.splitlines()[1:], ["oracle_id\tname", "%s\tIn Scope" % OID_A])

    def test_build_without_legality_has_no_scope_view(self):
        schema = schema_mod.Schema(
            self.schema.version,
            {k: v for k, v in self.schema.tables.items() if k != "legality"},
            self.schema.root)
        db = self.scope_build(schema=schema)
        conn = sqlite3.connect(db)
        self.addCleanup(conn.close)
        self.assertIsNone(conn.execute(
            "SELECT 1 FROM sqlite_master WHERE type='view' AND name='scope_cards'").fetchone())
        r = self.cli(self.tmp, "export", "scope", "--tsv", "--db", db)
        self.assertEqual(r.returncode, 2, r.stderr)
        self.assertIn("no scope_cards view", r.stderr)


class TestHeadline(Base):
    """`carddb headline`: the held-out deck share proven at four seats, plus its supporting numbers."""

    def setUp(self):
        super().setUp()
        self.home = os.path.join(self.tmp, "headline")
        a = self.machine("headline", init=True)
        names = {OID_A: "Proven One", OID_B: "Proven Two", OID_C: "Unproven", OID_D: "No Row"}
        a.put("cards", [{"oracle_id": oid, "name": n, "layout": "normal", "mana_value": 1,
                         "type_line": "Instant", "paper": True} for oid, n in names.items()],
              writer="catalogue")
        a.put("legality", [{"oracle_id": oid, "format": "commander", "status": "legal"} for oid in names],
              writer="catalogue")
        rows = []
        for oid in (OID_A, OID_B):  # proven: a witnessed xmage leg plus lockstep, both fresh
            rows.append(keyproof_row(oid, "cast", "agreed", leg="xmage", refs="X", witness="xs:w",
                                     measured="2026-11-20"))
            rows.append(keyproof_row(oid, "cast", "agreed", leg="lockstep", measured="2026-11-23"))
        rows.append(keyproof_row(OID_C, "cast", "owed", leg="xmage", reason="no-generator"))
        a.put("keyproof", rows, writer="xproof")
        src = os.path.join(self.tmp, "headline-src")
        write(os.path.join(src, "carddb/curated/exclusions.tsv"),
              "#carddb curated/exclusions v1\noracle_id\treason\n")
        committed = build_mod.read_committed(directory=src)
        self.pub = os.path.join(self.tmp, "carddb.sqlite")
        build_mod.build(self.pub, self.schema, a.dir, None, committed)
        os.environ["CARDDB_PRIVATE_REMOTE"] = os.path.join(self.tmp, "private.git")
        self.addCleanup(os.environ.pop, "CARDDB_PRIVATE_REMOTE", None)
        git("init", "--bare", "--quiet", os.environ["CARDDB_PRIVATE_REMOTE"])
        p = Store("private", home=self.home, schema=self.schema).init()
        p.put("decks", [
            {"deck_id": "h1", "oracle_id": OID_A, "count": 1},  # all proven, seats four
            {"deck_id": "h1", "oracle_id": OID_B, "count": 1},
            {"deck_id": "h2", "oracle_id": OID_A, "count": 1},  # all proven, not four-seat
            {"deck_id": "h3", "oracle_id": OID_A, "count": 1},  # one unproven card
            {"deck_id": "h3", "oracle_id": OID_C, "count": 1},
            {"deck_id": "r1", "oracle_id": OID_A, "count": 1},  # ranking half: never counted
        ], writer="decklib")
        self.priv = os.path.join(self.tmp, "carddb-private.sqlite")
        build_mod.build(self.priv, self.schema, a.dir, p.dir, committed)
        # deck_sample is declared by the deck-support work; the fixture creates it by hand.
        conn = sqlite3.connect(self.priv)
        conn.execute("CREATE TABLE deck_sample (deck_id TEXT, half TEXT, bracket TEXT, dirac_four_seat INTEGER)")
        conn.executemany("INSERT INTO deck_sample VALUES (?,?,?,?)", [
            ("h1", "holdout", "cedh", 1), ("h2", "holdout", "casual", 0),
            ("h3", "holdout", "cedh", 1), ("r1", "rank", "precon", 1)])
        conn.commit()
        conn.close()

    def headline(self, private=False):
        db = self.priv if private else self.pub
        r = self.cli(self.home, "headline", "--db", db, "--now", "2026-12-01",
                     *(["--private"] if private else []))
        self.assertEqual(r.returncode, 0, r.stderr)
        rows = [l.split("\t") for l in r.stdout.splitlines()]
        self.assertEqual(rows[0], ["metric", "n", "of", "pct"])
        return r.stdout, {row[0]: row[1:] for row in rows[1:]}

    def golden(self, name, text):
        path = os.path.join(GOLDEN, name)
        if UPDATE:
            write(path, text)
        self.assertEqual(text, read(path), "golden %s differs (CARDDB_UPDATE_GOLDEN=1 rewrites it)" % name)

    def test_private_decks_proven_counts_holdout_four_seat_fully_proven(self):
        text, m = self.headline(private=True)
        self.assertEqual(m["decks_proven:all"], ["1", "3", "33.3"])
        self.assertEqual(m["decks_proven:cedh"], ["1", "2", "50.0"])
        self.assertEqual(m["decks_proven:casual"], ["0", "1", "0.0"])
        self.assertNotIn("decks_proven:precon", m)  # the ranking-half deck is not counted
        metrics = [l.split("\t")[0] for l in text.splitlines()[1:]]
        self.assertEqual(metrics[:4], ["decks_proven:all", "decks_proven:casual", "decks_proven:cedh",
                                       "median_deck_share_pct"])
        self.golden("query_headline_private.txt", text)

    def test_median_over_holdout_shares(self):
        # shares 100, 100, 50 -> median 100
        _text, m = self.headline(private=True)
        self.assertEqual(m["median_deck_share_pct"], ["100.0", "3", "-"])

    def test_public_prints_dash_row_and_key_and_leg_rows(self):
        text, m = self.headline()
        self.assertEqual(m["decks_proven:all"], ["-", "-", "-"])
        self.assertNotIn("median_deck_share_pct", m)
        self.assertEqual(m["keys_proven"], ["2", "3", "-"])
        self.assertEqual(m["keys_forge_only"], ["0", "3", "-"])
        self.assertEqual(m["keys_unwitnessed"], ["0", "3", "-"])
        self.assertEqual(m["keys_two_seat_only"], ["0", "3", "-"])
        # oldest leg first: xmage last agreed 2026-11-20, lockstep 2026-11-23
        self.assertEqual(m["leg_age_hours:xmage"], ["264", "2", "-"])
        self.assertEqual(m["leg_age_hours:lockstep"], ["192", "2", "-"])
        legs = [l.split("\t")[0] for l in text.splitlines() if l.startswith("leg_age_hours:")]
        self.assertEqual(legs, ["leg_age_hours:xmage", "leg_age_hours:lockstep"])
        self.golden("query_headline.txt", text)

    def test_cards_without_a_row_counts_scope_card_with_no_keyproof(self):
        _text, m = self.headline()
        self.assertEqual(m["cards_without_a_row"], ["1", "4", "-"])

    def test_deck_proven_view(self):
        conn = sqlite3.connect(self.priv)
        self.addCleanup(conn.close)
        self.assertEqual(conn.execute("SELECT deck_id, cards, proven_cards, proven FROM deck_proven "
                                      "ORDER BY deck_id").fetchall(),
                         [("h1", 2, 2, 1), ("h2", 1, 1, 1), ("h3", 2, 1, 0), ("r1", 1, 1, 1)])
