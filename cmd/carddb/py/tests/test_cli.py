import os
import shutil
import sqlite3
import subprocess
import unittest

from .helpers import Base, OID_A, ROOT, git
from .test_build import write


class Cli(Base):
    def test_init_put_publish_build_roundtrip(self):
        home = os.path.join(self.tmp, "m1")
        for args, stdin in [(("init",), ""),
                            (("put", "legality", "--source", "loop"), "oracle_id\tformat\tstatus\n%s\tmodern\tlegal\n" % OID_A),
                            (("put", "cards", "--source", "mtgjson"),
                             "oracle_id\tname\tlayout\tmana_value\ttype_line\n%s\tBolt\tnormal\t1\tInstant\n" % OID_A),
                            (("publish",), "")]:
            r = self.cli(home, *args, stdin=stdin)
            self.assertEqual(r.returncode, 0, "%s: %s%s" % (args, r.stdout, r.stderr))
        self.assertIn("carddb: loop,mtgjson 2 rows @8f7095353a64", r.stdout)
        r = self.cli(home, "publish")
        self.assertIn("nothing staged", r.stdout)
        src = os.path.join(self.tmp, "src")
        write(os.path.join(src, "carddb/curated/labels.tsv"), "#carddb curated/labels v1\noracle_id\tlabel\tnote\n")
        out = os.path.join(self.tmp, "o.sqlite")
        r = self.cli(home, "build", "--src-dir", src, "--out", out)
        self.assertEqual(r.returncode, 0, r.stderr)
        c = sqlite3.connect(out)
        self.addCleanup(c.close)
        self.assertEqual(c.execute("SELECT name FROM cards").fetchall(), [("Bolt",)])
        self.assertEqual(self.cli(home, "validate").returncode, 0)
        self.assertEqual(self.cli(home, "boundary").returncode, 0)
        # a second machine clones what the first published
        r = self.cli(os.path.join(self.tmp, "m2"), "put", "legality", stdin="oracle_id\tformat\tstatus\n%s\tmodern\tlegal\n" % OID_A)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("0 rows changed", r.stdout)

    def test_bad_put_exits_nonzero_and_writes_nothing(self):
        home = os.path.join(self.tmp, "m1")
        self.assertEqual(self.cli(home, "init").returncode, 0)
        r = self.cli(home, "put", "legality", stdin="oracle_id\tformat\tstatus\n%s\tmodern\t-\n" % OID_A)
        self.assertEqual(r.returncode, 1, r.stdout)
        self.assertIn("required column has no value", r.stderr)
        self.assertEqual(git("status", "--porcelain", cwd=os.path.join(home, "public")), "")

    def test_unknown_table_and_missing_remote(self):
        home = os.path.join(self.tmp, "m1")
        self.assertEqual(self.cli(home, "init").returncode, 0)
        self.assertEqual(self.cli(home, "put", "nope", stdin="a\n1\n").returncode, 2)
        r = self.cli(os.path.join(self.tmp, "m3"), "init", "--private")
        self.assertEqual(r.returncode, 2)
        self.assertIn("CARDDB_PRIVATE_REMOTE", r.stderr)


class GoParity(Base):
    """The Python validator and boundary agree with internal/carddb (the oracle) on trees: each case is a
    list of (path, text) files; a case is clean ("ok", "real schema decl") or both sides refuse it."""

    HDR = "#carddb printings v1\nprinting_id\toracle_id\tset_code\tcollector_number\trarity\treleased\n"
    LEG = "#carddb legality v1\noracle_id\tformat\tstatus\n"
    CLEAN = ("ok", "real schema decl", "ruling refs ok")

    @classmethod
    def cases(cls):
        def one(rel, text):
            return [(rel, text)]
        def decl(rel):
            with open(os.path.join(ROOT, "carddb", rel), encoding="utf-8") as f:
                return rel, f.read()
        return {
            "ok": one("legality/ab.tsv", cls.LEG + "ab1\tmodern\tlegal\nab2\tmodern\tlegal\n"),
            "unsorted": one("legality/ab.tsv", cls.LEG + "ab2\tmodern\tlegal\nab1\tmodern\tlegal\n"),
            "dup": one("legality/ab.tsv", cls.LEG + "ab1\tmodern\tlegal\nab1\tmodern\tlegal\n"),
            "empty cell": one("legality/ab.tsv", cls.LEG + "ab1\tmodern\t\n"),
            "bad int": one("cards/ab.tsv", "#carddb cards v1\noracle_id\tname\tlayout\tmana_cost\tmana_value\ttype_line\tcolors\tcolor_identity\tkeywords\toracle_sha\tfirst_released\tfirst_set\tpaper\nab1\tn\tnormal\t-\tx\tt\t-\t-\t-\t-\t-\t-\tfalse\n"),
            "wrong path": one("elsewhere.tsv", cls.LEG + "ab1\tmodern\tlegal\n"),
            "private table": one("text/ab.tsv", "#carddb text v1\noracle_id\n"),
            "extra column": one("legality/ab.tsv", cls.LEG.rstrip("\n") + "\tprice\nab1\tmodern\tlegal\t1\n"),
            "forge line": one("xledger/ab.tsv", "#carddb xledger v1\noracle_id\ttier\tstate\tnote\nab1\tT1\tx\tSVar:X$ Y\n"),
            "no newline": one("legality/ab.tsv", cls.LEG + "ab1\tmodern\tlegal"),
            "bad date": one("sets.tsv", "#carddb sets v1\nset_code\tname\trelease_date\tset_type\tparent_code\nlea\tn\t1993-13-45\tcore\t-\n"),
            # a key two shards both hold (printings shard by set, keyed by printing id)
            "dup across shards": [("printings/lea.tsv", cls.HDR + "p1\tab1\tlea\t1\tcommon\t-\n"),
                                  ("printings/neo.tsv", cls.HDR + "p1\tab1\tneo\t1\tcommon\t-\n")],
            "dup across case-variant shards": [("printings/lea.tsv", cls.HDR + "p1\tab1\tlea\t1\tcommon\t-\n"),
                                               ("printings/LEA.tsv", cls.HDR + "p1\tab1\tLEA\t1\tcommon\t-\n")],
            "distinct keys across shards": [("printings/lea.tsv", cls.HDR + "p1\tab1\tlea\t1\tcommon\t-\n"),
                                            ("printings/neo.tsv", cls.HDR + "p2\tab1\tneo\t1\tcommon\t-\n")],
            # a shard behind another extension, a forbidden key in a json file
            "hidden shard": one("hidden.txt", cls.LEG + "ab1\tmodern\tlegal\n"),
            "forbidden json key": one("notes.json", '{\n  "oracle_text": "x"\n}\n'),
            "forge line in a txt": one("notes.txt", "A:AB$ Draw | Cost$ 1\n"),
            # schema/: the real declarations are exempt, anything else is refused
            "real schema decl": [decl("schema/VERSION"), decl("schema/v1/cards.tsv"), decl("schema/v1/tags.tsv")],
            "stray schema file": one("schema/v1/leak.tsv", "#carddb-schema cards v1\n"),
            "altered schema decl": [("schema/v1/cards.tsv", decl("schema/v1/cards.tsv")[1] + "extra\tstring\tno\tno\tpublic\tx\n")],
            "schema dir holds a shard": one("schema/legality.tsv", cls.LEG + "ab1\tmodern\tlegal\n"),
            # rulings: the text is a private table; its public mirror carries ids only
            "ruling refs ok": one("ruling_refs/ab.tsv",
                                  "#carddb ruling_refs v1\noracle_id\truling_id\tpublished\tsource\tretrieved\n"
                                  "ab1\tcd162149f976\t2026-01-02\twotc\t2026-01-03\n"),
            "rulings private": one("rulings/ab.tsv",
                                   "#carddb rulings v1\noracle_id\truling_id\tpublished\tsource\tcomment\tretrieved\n"
                                   "ab1\tcd162149f976\t2026-01-02\twotc\tAn invented sentence.\t2026-01-03\n"),
            "ruling refs comment": one("ruling_refs/ab.tsv",
                                       "#carddb ruling_refs v1\noracle_id\truling_id\tpublished\tsource\tretrieved\tcomment\n"
                                       "ab1\tcd162149f976\t2026-01-02\twotc\t2026-01-03\tAn invented sentence.\n"),
        }

    def test_same_verdict(self):
        go = shutil.which("go")
        if go is None:
            self.skipTest("no go")
        from carddb import shard
        from carddb.boundary import boundary_tree
        for name, files in sorted(self.cases().items()):
            with self.subTest(name):
                tree = os.path.join(self.tmp, name.replace(" ", "_"))
                for rel, text in files:
                    write(os.path.join(tree, rel), text)
                on_disk = list(shard.tree_files(tree))
                self.assertEqual(sorted(rel for rel, _ in on_disk), sorted(rel for rel, _ in files))
                py = bool(shard.validate_tree(self.schema, "public", on_disk)) or bool(boundary_tree(self.schema, on_disk))
                gos = []
                for verb in ("validate", "boundary"):
                    p = subprocess.run([go, "run", "./cmd/carddb", verb, "-schema", "carddb/schema", "-tree", tree],
                                       cwd=ROOT, capture_output=True, text=True, env=dict(os.environ, GOWORK="off"))
                    self.assertIn(p.returncode, (0, 1), p.stderr)
                    gos.append(p.returncode == 1)
                self.assertEqual(py, any(gos), "%s: python %s, go validate/boundary %s" % (name, py, gos))
                self.assertEqual(py, name not in self.CLEAN + ("distinct keys across shards",))
                # the python verbs a human runs give the Go exit status per verb
                for verb, want in zip(("validate", "boundary"), gos):
                    r = self.cli(os.path.join(self.tmp, "h"), verb, "--tree", tree)
                    self.assertEqual(r.returncode == 1, want, "%s: carddb %s: %s%s" % (name, verb, r.stdout, r.stderr))
