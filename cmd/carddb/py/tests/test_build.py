import json
import os
import sqlite3
import stat
import unittest

import carddb
from carddb import build as build_mod
from carddb import fetch as fetch_mod
from carddb import store as store_mod
from .helpers import GOLDEN, ROOT, Base, OID_A, OID_B, OID_C, git

UPDATE = os.environ.get("CARDDB_UPDATE_GOLDEN") == "1"


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


class Build(Base):
    def conn(self, path):
        c = sqlite3.connect(path)
        self.addCleanup(c.close)
        return c

    def fixture(self):
        a = self.machine("a", init=True)
        a.put("cards", [
            {"oracle_id": OID_B, "name": "Fire // Ice", "layout": "split", "mana_cost": "{1}{R} // {1}{U}", "mana_value": 4,
             "type_line": "Instant // Instant", "colors": ["R", "U"], "color_identity": ["R", "U"]},
            {"oracle_id": OID_A, "name": "Lightning Bolt", "layout": "normal", "mana_cost": "{R}", "mana_value": 1,
             "type_line": "Instant", "colors": ["R"], "color_identity": ["R"]},
            {"oracle_id": OID_C, "name": "Dáin Ironfoot", "layout": "normal", "mana_cost": "{3}{R}", "mana_value": 4,
             "type_line": "Legendary Creature — Dwarf Warrior", "colors": ["R"], "color_identity": ["R"],
             "keywords": ["Haste", "Double strike"]},
        ], writer="mtgjson")
        a.put("sets", [{"set_code": "lea", "name": "Limited Edition Alpha", "release_date": "1993-08-05",
                        "set_type": "core"}], writer="mtgjson")
        a.put("printings", [{"printing_id": "p-1", "oracle_id": OID_A, "set_code": "lea", "collector_number": "161",
                             "rarity": "common", "released": "1993-08-05"}], writer="mtgjson")
        a.put("legality", [legality for legality in (
            {"oracle_id": OID_A, "format": "modern", "status": "legal"},
            {"oracle_id": OID_B, "format": "modern", "status": "banned"})], writer="mtgjson")
        a.put("labels", [{"oracle_id": OID_A, "label": "burn", "origin": "derived"}], writer="derive")
        a.put("status", [{"oracle_id": OID_A, "level": "A", "verdict": "agree", "gorge_pin": "8f7095353a64",
                          "measured": "2026-11-19"}], writer="loop")
        src = os.path.join(self.tmp, "src")
        write(os.path.join(src, "carddb/curated/labels.tsv"),
              "#carddb curated/labels v1\noracle_id\tlabel\tnote\n%s\tstaple\t-\n" % OID_A)
        write(os.path.join(src, "carddb/curated/exclusions.tsv"),
              "#carddb curated/exclusions v1\noracle_id\treason\n%s\tante\n" % OID_C)
        for code, name in (("LEA", "Limited Edition Alpha"), ("2ED", "Unlimited Edition")):
            write(os.path.join(src, "oracle/compliance/manifests/%s.json" % code), json.dumps(
                {"code": code, "name": name, "released": "1993-08-05", "set_type": "CORE", "xmage_ref": "abc",
                 "cards": [{"name": "Lightning Bolt", "numbers": ["161", "161*"], "rarity": "COMMON"}]}) + "\n")
        write(os.path.join(src, "oracle/xengine/diagnosed.tsv"),
              "kind\tset\tcard\tfield\twrong\tcr\tevidence\tupstream\tticket\n"
              "diverge\tLEA\tLightning Bolt\tp0.hand\tharness\t307.4\twhy\t\txe-1\n")
        return a, build_mod.read_committed(directory=src)

    def build(self, **kw):
        a, committed = self.fixture()
        out = os.path.join(self.tmp, "carddb.sqlite")
        build_mod.build(out, self.schema, a.dir, None, committed, **kw)
        return out

    def golden(self, name, text):
        path = os.path.join(GOLDEN, name)
        if UPDATE:
            write(path, text)
        self.assertEqual(text, read(path), "golden %s differs (CARDDB_UPDATE_GOLDEN=1 rewrites it)" % name)

    def test_build_golden(self):
        out = self.build()
        conn = self.conn(out)
        text = build_mod.dump_text(conn)
        self.golden("build.txt", text)
        self.golden("schema.txt", text.split("-- rows ")[0])  # the DDL alone: a schema change shows here
        self.assertNotIn("Oracle text", text)

    def test_build_is_repeatable(self):
        a, committed = self.fixture()
        outs = []
        for i in range(2):
            out = os.path.join(self.tmp, "o%d.sqlite" % i)
            build_mod.build(out, self.schema, a.dir, None, committed)
            outs.append(build_mod.dump_text(self.conn(out)))
        self.assertEqual(outs[0], outs[1])

    def test_fts_and_indexes(self):
        conn = self.conn(self.build())
        self.assertEqual(conn.execute("SELECT oracle_id FROM card_fts WHERE card_fts MATCH 'lightning'").fetchall(), [(OID_A,)])
        self.assertEqual(conn.execute("SELECT oracle_id FROM card_fts WHERE card_fts MATCH 'labels:staple'").fetchall(), [(OID_A,)])
        self.assertEqual(conn.execute("SELECT oracle_id FROM card_fts WHERE card_fts MATCH 'dwarf'").fetchall(), [(OID_C,)])
        self.assertEqual(conn.execute("SELECT oracle_id FROM card_fts WHERE card_fts MATCH 'keywords:haste'").fetchall(), [(OID_C,)])
        self.assertEqual(conn.execute("SELECT oracle_id FROM card_fts WHERE card_fts MATCH 'keywords:double AND keywords:strike'").fetchall(), [(OID_C,)])
        self.assertEqual(conn.execute("SELECT oracle_id FROM card_fts WHERE card_fts MATCH 'keywords:lightning'").fetchall(), [])
        self.assertEqual(conn.execute("SELECT oracle_id FROM card_names WHERE norm_name='ice'").fetchall(), [(OID_B,)])
        cols = [r[1] for r in conn.execute("PRAGMA table_info(card_fts)")]
        self.assertEqual(cols, ["oracle_id", "name", "type_line", "keywords", "labels"])
        self.assertTrue(conn.execute("SELECT 1 FROM sqlite_master WHERE name='idx_printings_oracle_id'").fetchone())
        self.assertEqual(conn.execute("SELECT count(*) FROM src_manifest_cards").fetchone()[0], 2)
        self.assertEqual(conn.execute("PRAGMA user_version").fetchone()[0], 1)

    def test_text_goes_only_into_the_text_build(self):
        a, committed = self.fixture()
        tf = os.path.join(self.tmp, "oracle-cards.json")
        write(tf, json.dumps([{"oracle_id": OID_A, "name": "Lightning Bolt", "oracle_text": "Deals 3 damage to any target."}]))
        with self.assertRaises(store_mod.StoreError):
            build_mod.build(os.path.join(self.tmp, "carddb.sqlite"), self.schema, a.dir, None, committed, text_file=tf)
        out = os.path.join(self.tmp, "carddb-text.sqlite")
        build_mod.build(out, self.schema, a.dir, None, committed, text_file=tf)
        conn = self.conn(out)
        self.assertEqual(conn.execute("SELECT oracle_id FROM card_fts WHERE card_fts MATCH 'text:damage'").fetchall(), [(OID_A,)])

    def test_invalid_tree_refuses_to_build(self):
        a, committed = self.fixture()
        p = os.path.join(a.dir, "legality", "ab.tsv")
        write(p, read(p) + read(p).split("\n")[2] + "\n")  # a duplicate key
        with self.assertRaises(carddb.ValidationError):
            build_mod.build(os.path.join(self.tmp, "x.sqlite"), self.schema, a.dir, None, committed)
        self.assertFalse(os.path.exists(os.path.join(self.tmp, "x.sqlite")))

    def test_read_committed_from_git_ref(self):
        names = build_mod.read_committed(ref="HEAD", repo=ROOT)
        self.assertIn("carddb/curated/labels.tsv", names)
        self.assertIn("oracle/xengine/diagnosed.tsv", names)
        self.assertTrue(any(k.startswith("oracle/compliance/manifests/") for k in names))

    def test_default_ref_tolerates_a_ref_without_the_curated_tree(self):
        """A checkout whose HEAD has no curated tree: the sources it lacks are simply absent."""
        repo = os.path.join(self.tmp, "checkout")
        write(os.path.join(repo, "oracle/xengine/diagnosed.tsv"), "kind\tset\tcard\tfield\twrong\tcr\tevidence\tupstream\tticket\n")
        git("init", "--quiet", "-b", "main", repo)
        git("add", "-A", cwd=repo)
        git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "--quiet", "-m", "x", cwd=repo)
        got = build_mod.read_committed(repo=repo)  # the default ref
        self.assertEqual(sorted(got), ["oracle/xengine/diagnosed.tsv"])
        with self.assertRaises(store_mod.StoreError):
            build_mod.read_committed(repo=repo, ref="origin/nope")
        # and a whole build runs from it
        a, _ = self.fixture()
        out = os.path.join(self.tmp, "carddb.sqlite")
        build_mod.build(out, self.schema, a.dir, None, got)
        self.assertEqual(self.conn(out).execute("SELECT count(*) FROM curated_labels").fetchone()[0], 0)

    def test_default_ref_is_the_checkout_head(self):
        self.assertEqual(sorted(build_mod.read_committed(repo=ROOT)),
                         sorted(build_mod.read_committed(repo=ROOT, ref="HEAD")))

    def test_cli_build_defaults_to_the_checkout_head(self):
        a, _ = self.fixture()
        out = os.path.join(self.tmp, "cli.sqlite")
        r = self.cli(os.path.join(self.tmp, "a"), "build", "--out", out)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertTrue(os.path.isfile(out))
        # the citation columns are on this branch's HEAD; building it must not need main
        head = build_mod.read_committed(repo=ROOT, ref="HEAD")
        want = len([l for l in head["carddb/curated/game-changers.tsv"].decode("utf-8").splitlines()[2:] if l])
        self.assertEqual(self.conn(out).execute("SELECT count(*) FROM curated_game_changers").fetchone()[0], want)

    def test_a_key_in_two_shards_is_a_validation_error_not_an_integrity_error(self):
        a, committed = self.fixture()
        p = os.path.join(a.dir, "printings", "lea.tsv")
        write(os.path.join(a.dir, "printings", "neo.tsv"), read(p).replace("\tlea\t", "\tneo\t"))
        with self.assertRaises(carddb.ValidationError) as cm:
            build_mod.build(os.path.join(self.tmp, "x.sqlite"), self.schema, a.dir, None, committed)
        self.assertIn("duplicate key (p-1)", str(cm.exception))

    def test_a_hidden_shard_or_stray_schema_file_refuses_the_build(self):
        for rel, text in (("hidden.txt", "#carddb legality v1\noracle_id\tformat\tstatus\n"),
                          ("schema/v1/leak.tsv", "#carddb-schema x v1\n")):
            with self.subTest(rel):
                a, committed = self.fixture()
                write(os.path.join(a.dir, rel), text)
                with self.assertRaises(carddb.ValidationError):
                    build_mod.build(os.path.join(self.tmp, "x.sqlite"), self.schema, a.dir, None, committed)

    def test_private_build(self):
        a, committed = self.fixture()
        os.environ["CARDDB_PRIVATE_REMOTE"] = os.path.join(self.tmp, "private.git")
        self.addCleanup(os.environ.pop, "CARDDB_PRIVATE_REMOTE", None)
        git("init", "--bare", "--quiet", os.environ["CARDDB_PRIVATE_REMOTE"])
        p = store_mod.Store("private", home=os.path.join(self.tmp, "a"), schema=self.schema).init()
        p.put("tags", [{"oracle_id": OID_A, "tag": "removal", "tagger": "t"}], writer="loop")
        self.assertEqual(os.path.basename(p.dir), "private")
        self.assertEqual(carddb.put("text", [{"oracle_id": OID_A, "oracle_text": "x"}], writer="loop",
                                    home=os.path.join(self.tmp, "a")), 1)
        out = os.path.join(self.tmp, "carddb-private.sqlite")
        build_mod.build(out, self.schema, a.dir, p.dir, committed)
        conn = self.conn(out)
        self.assertEqual(conn.execute("SELECT tag FROM tags").fetchall(), [("removal",)])
        pub = self.conn(self.build())
        self.assertIsNone(pub.execute("SELECT 1 FROM sqlite_master WHERE name='tags'").fetchone())


class Resolve(Base):
    def test_resolve_and_unresolved(self):
        a = self.machine("a", init=True)
        a.put("cards", [{"oracle_id": OID_A, "name": "Lightning Bolt", "layout": "normal", "mana_value": 1,
                         "type_line": "Instant"}], writer="mtgjson")
        home = os.path.join(self.tmp, "a")
        self.assertEqual(carddb.resolve("lightning  BOLT", home=home), OID_A)
        self.assertEqual(self.snapshot(a.dir).get("_meta/unresolved.tsv"), None)
        pid = carddb.resolve("Forge Only Card", home=home)
        self.assertRegex(pid, r"^x:[0-9a-f]{16}$")
        text = read(os.path.join(a.dir, "_meta", "unresolved.tsv"))
        self.assertIn("card\tForge Only Card\t", text)
        before = self.snapshot(a.dir)
        self.assertEqual(carddb.resolve("Forge Only Card", home=home), pid)
        self.assertEqual(self.snapshot(a.dir), before)  # the same miss again: no byte changes
        self.assertEqual(carddb.put("cards", [{"oracle_id": pid, "name": "Forge Only Card", "layout": "normal",
                                               "mana_value": 0, "type_line": "Land"}], writer="forge", home=home), 1)
        self.assertTrue(os.path.isfile(os.path.join(a.dir, "cards", "x%s.tsv" % pid[2:4])))


class Fetch(Base):
    def stub_gh(self, payload, zst):
        d = os.path.join(self.tmp, "bin")
        os.makedirs(d)
        asset = os.path.join(self.tmp, "asset")
        with open(asset, "wb") as f:
            f.write(payload)
        if zst:
            from compression import zstd
            with zstd.open(asset + ".zst", "wb") as f:
                f.write(payload)
        gh = os.path.join(d, "gh")
        write(gh, '#!/bin/sh\necho "$@" > "%s/gh.args"\n'
                  'while [ $# -gt 0 ]; do case "$1" in --dir) dir="$2";; esac; shift; done\n'
                  'cp "%s"%s "$dir/carddb.sqlite%s"\n' % (self.tmp, asset, ".zst" if zst else "", ".zst" if zst else ""))
        os.chmod(gh, os.stat(gh).st_mode | stat.S_IEXEC)
        os.environ["CARDDB_GH"] = gh
        self.addCleanup(os.environ.pop, "CARDDB_GH", None)

    def test_fetch_decompresses_into_home(self):
        for zst in (True, False):
            with self.subTest(zst=zst):
                payload = b"SQLite format 3\x00 fake"
                self.stub_gh(payload, zst)
                home = os.path.join(self.tmp, "home%d" % zst)
                dest = fetch_mod.fetch(home=home)
                self.assertEqual(dest, os.path.join(home, "carddb.sqlite"))
                with open(dest, "rb") as f:
                    self.assertEqual(f.read(), payload)
                args = read(os.path.join(self.tmp, "gh.args"))
                self.assertIn("release download carddb-latest --repo adams-shaun/gorge-compliance", args)
                self.assertEqual(os.listdir(home), ["carddb.sqlite"])  # the temp dir is gone
                shutil_rm = __import__("shutil").rmtree
                shutil_rm(os.path.join(self.tmp, "bin"))

    def test_private_needs_a_repo(self):
        with self.assertRaises(store_mod.StoreError):
            fetch_mod.fetch(private=True, home=os.path.join(self.tmp, "h"))


if __name__ == "__main__":
    unittest.main()
