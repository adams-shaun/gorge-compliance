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
    """The Python validator and boundary agree with internal/carddb on malformed shards."""

    CASES = {
        "ok": ("legality/ab.tsv", "#carddb legality v1\noracle_id\tformat\tstatus\nab1\tmodern\tlegal\nab2\tmodern\tlegal\n"),
        "unsorted": ("legality/ab.tsv", "#carddb legality v1\noracle_id\tformat\tstatus\nab2\tmodern\tlegal\nab1\tmodern\tlegal\n"),
        "dup": ("legality/ab.tsv", "#carddb legality v1\noracle_id\tformat\tstatus\nab1\tmodern\tlegal\nab1\tmodern\tlegal\n"),
        "empty cell": ("legality/ab.tsv", "#carddb legality v1\noracle_id\tformat\tstatus\nab1\tmodern\t\n"),
        "bad int": ("cards/ab.tsv", "#carddb cards v1\noracle_id\tname\tlayout\tmana_cost\tmana_value\ttype_line\tcolors\tcolor_identity\nab1\tn\tnormal\t-\tx\tt\t-\t-\n"),
        "wrong path": ("elsewhere.tsv", "#carddb legality v1\noracle_id\tformat\tstatus\nab1\tmodern\tlegal\n"),
        "private table": ("text/ab.tsv", "#carddb text v1\noracle_id\n"),
        "extra column": ("legality/ab.tsv", "#carddb legality v1\noracle_id\tformat\tstatus\tprice\nab1\tmodern\tlegal\t1\n"),
        "forge line": ("xledger/ab.tsv", "#carddb xledger v1\noracle_id\ttier\tstate\tnote\nab1\tT1\tx\tSVar:X$ Y\n"),
        "no newline": ("legality/ab.tsv", "#carddb legality v1\noracle_id\tformat\tstatus\nab1\tmodern\tlegal"),
        "bad date": ("sets.tsv", "#carddb sets v1\nset_code\tname\trelease_date\tset_type\tparent_code\nlea\tn\t1993-13-45\tcore\t-\n"),
    }

    def test_same_verdict(self):
        go = shutil.which("go")
        if go is None:
            self.skipTest("no go")
        from carddb import shard
        from carddb.boundary import boundary_text
        for name, (rel, text) in sorted(self.CASES.items()):
            with self.subTest(name):
                tree = os.path.join(self.tmp, name.replace(" ", "_"))
                write(os.path.join(tree, rel), text)
                py = bool(shard.validate_shard(self.schema, "public", rel, text)) or bool(boundary_text(self.schema, rel, text))
                gos = []
                for verb in ("validate", "boundary"):
                    p = subprocess.run([go, "run", "./cmd/carddb", verb, "-schema", "carddb/schema", "-tree", tree],
                                       cwd=ROOT, capture_output=True, text=True, env=dict(os.environ, GOWORK="off"))
                    self.assertIn(p.returncode, (0, 1), p.stderr)
                    gos.append(p.returncode == 1)
                self.assertEqual(py, any(gos), "%s: python %s, go validate/boundary %s" % (name, py, gos))
                self.assertEqual(py, name != "ok")
