"""fetch: gh release download carddb-latest into CARDDB_HOME."""
import json
import os
import sqlite3
import subprocess
import tempfile

from .store import StoreError, carddb_home

TAG = "carddb-latest"
PUBLIC_REPO = "adams-shaun/gorge-compliance"
META = "carddb-latest.json"


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


def _published_at(gh, repo, tag):
    """The release's publishedAt, or None when gh cannot answer (fetch still succeeds)."""
    p = subprocess.run([gh, "release", "view", tag, "--repo", repo, "--json", "publishedAt"],
                       capture_output=True, text=True)
    if p.returncode != 0:
        return None
    try:
        return json.loads(p.stdout).get("publishedAt") or None
    except ValueError:
        return None


def _unresolved(path):
    try:
        conn = sqlite3.connect("file:%s?mode=ro" % os.path.abspath(path), uri=True)
        try:
            return conn.execute("SELECT COUNT(*) FROM _meta_unresolved").fetchone()[0]
        finally:
            conn.close()
    except (OSError, sqlite3.Error):
        return None


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
    published = _published_at(gh, repo, tag)
    if published:
        meta = {"tag": tag, "repo": repo, "published_at": published}
        unresolved = _unresolved(dest)
        if unresolved is not None:
            meta["unresolved"] = unresolved
        path = os.path.join(home, META)
        tmp = path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(meta, f, sort_keys=True)
            f.write("\n")
        os.replace(tmp, path)
    return dest
