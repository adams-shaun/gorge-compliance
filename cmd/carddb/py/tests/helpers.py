import os
import shutil
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
PY = os.path.dirname(HERE)
MAIN = os.path.join(os.path.dirname(PY), "main.py")
ROOT = os.path.normpath(os.path.join(HERE, "..", "..", "..", ".."))
GOLDEN = os.path.join(HERE, "golden")

from carddb import schema as schema_mod  # noqa: E402
from carddb.store import Store  # noqa: E402

OID_A = "ab" + "0" * 30
OID_B = "ab" + "1" * 30
OID_C = "cd" + "2" * 30


def git(*args, cwd=None):
    p = subprocess.run(["git"] + list(args), cwd=cwd, capture_output=True, text=True)
    if p.returncode != 0:
        raise AssertionError("git %s: %s" % (" ".join(args), p.stderr))
    return p.stdout


class Base(unittest.TestCase):
    """A temp dir, a bare remote, and per-machine CARDDB_HOMEs."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="carddb-test-")
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.remote = os.path.join(self.tmp, "remote.git")
        git("init", "--bare", "--quiet", self.remote)
        self.schema = schema_mod.load_schema()
        self.env = {"CARDDB_PUBLIC_REMOTE": self.remote, "CARDDB_NOW": "2026-11-19",
                    "CARDDB_GORGE_PIN": "8f7095353a64deadbeef"}
        for k, v in self.env.items():
            old = os.environ.get(k)
            os.environ[k] = v
            self.addCleanup(lambda k=k, old=old: os.environ.pop(k, None) if old is None else os.environ.__setitem__(k, old))

    def machine(self, name, init=False):
        home = os.path.join(self.tmp, name)
        s = Store("public", home=home, schema=self.schema)
        if init:
            s.init()
        else:
            s.ensure()
        return s

    def cli(self, home, *args, stdin=""):
        env = dict(os.environ, CARDDB_HOME=home)
        return subprocess.run([sys.executable, "-I", MAIN] + list(args), input=stdin, capture_output=True,
                              text=True, env=env)

    @staticmethod
    def snapshot(root):
        out = {}
        for dp, dn, fns in os.walk(root):
            dn[:] = [d for d in dn if d != ".git"]
            for fn in fns:
                p = os.path.join(dp, fn)
                with open(p, "rb") as f:
                    out[os.path.relpath(p, root)] = f.read()
        return out
