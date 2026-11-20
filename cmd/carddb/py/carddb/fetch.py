"""fetch: gh release download carddb-latest into CARDDB_HOME."""
import os
import subprocess
import tempfile

from .store import StoreError, carddb_home

TAG = "carddb-latest"
PUBLIC_REPO = "adams-shaun/gorge-compliance"


def _unzstd(src, dst):
    try:
        from compression import zstd  # Python 3.14+
    except ImportError:
        p = subprocess.run(["zstd", "-d", "-q", "-f", "-o", dst, src], capture_output=True, text=True)
        if p.returncode != 0:
            raise StoreError("zstd: " + p.stderr.strip())
        return
    with zstd.open(src, "rb") as r, open(dst, "wb") as w:
        while True:
            b = r.read(1 << 20)
            if not b:
                break
            w.write(b)


def fetch(private=False, home=None, repo=None, tag=TAG):
    """Download the release asset (carddb.sqlite or carddb-private.sqlite, .zst or plain)
    into CARDDB_HOME, replacing the old file atomically. -> path of the sqlite file."""
    home = carddb_home(home)
    name = "carddb-private.sqlite" if private else "carddb.sqlite"
    if private:
        repo = repo or os.environ.get("CARDDB_PRIVATE_REPO")
        if not repo:
            raise StoreError("no private repo: set CARDDB_PRIVATE_REPO (owner/name)")
    else:
        repo = repo or os.environ.get("CARDDB_PUBLIC_REPO", PUBLIC_REPO)
    gh = os.environ.get("CARDDB_GH", "gh")
    os.makedirs(home, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".fetch-", dir=home) as tmp:
        p = subprocess.run([gh, "release", "download", tag, "--repo", repo, "--dir", tmp,
                            "--pattern", name + "*", "--clobber"], capture_output=True, text=True)
        if p.returncode != 0:
            raise StoreError("gh release download %s: %s" % (tag, (p.stderr or p.stdout).strip()))
        zst, plain = os.path.join(tmp, name + ".zst"), os.path.join(tmp, name)
        out = os.path.join(tmp, "out.sqlite")
        if os.path.isfile(zst):
            _unzstd(zst, out)
        elif os.path.isfile(plain):
            os.replace(plain, out)
        else:
            raise StoreError("release %s of %s has no asset %s[.zst]" % (tag, repo, name))
        dest = os.path.join(home, name)
        os.replace(out, dest)
    return dest
