"""A working clone of a data branch: put (stage), publish (commit, rebase, push).

CARDDB_HOME (default ~/.local/share/carddb) holds
  public/   clone of the compliance `carddb` branch
  private/  clone of the four-yay `carddb-private` branch (when configured)
Both are caches, never canonical. A put takes the clone's flock, rewrites only
the shards its rows touch, and `git add`s them; one publish per machine commits
what is staged and pushes it fast-forward.
"""
import contextlib
import datetime
import fcntl
import os
import re
import shlex
import shutil
import subprocess
import sys

from . import keylegs, shard
from .boundary import boundary_text
from .merge import WRITER_PRIORITY
from .schema import PRIVATE, PUBLIC, SchemaError, load_schema, repo_root
from .shard import ValidationError

DEFAULT_PUBLIC_REMOTE = "git@github.com:adams-shaun/gorge-compliance.git"
PUBLIC_BRANCH = "carddb"
PRIVATE_BRANCH = "carddb-private"
MAX_PUBLISH_LINES = 50000
GITATTRIBUTES = "*.tsv merge=carddb\n"
LEDGER = "carddb-pending.tsv"

_SAFE_STEM = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
_WRITER = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]*$")


class StoreError(Exception):
    pass


class GitError(StoreError):
    pass


def carddb_home(home=None):
    return os.path.abspath(home or os.environ.get("CARDDB_HOME")
                           or os.path.join(os.path.expanduser("~"), ".local", "share", "carddb"))


# The release metadata is per scope: a private fetch must not overwrite the
# public carddb-latest.json (freshness would report the other release's age).
ASSET_META = "carddb-latest.json"
PRIVATE_ASSET_META = "carddb-private-latest.json"


def asset_meta_path(home=None, private=False):
    return os.path.join(carddb_home(home), PRIVATE_ASSET_META if private else ASSET_META)


def today(ran=None):
    ran = ran or os.environ.get("CARDDB_NOW")
    if ran:
        return ran
    return datetime.datetime.now(datetime.timezone.utc).date().isoformat()


def packed_mib(directory):
    """The object-store size of a git clone in MiB (`count-objects` size + size-pack), or None.

    The one measurement `freshness`, `size` and `release` report. A data branch's
    D7 budget covers everything a clone of it downloads, not just the last
    packfile: a fresh CI clone holds the published history in a pack while the
    writers' new blobs stay loose until a fetch or repack. Counting only
    `size-pack` reported 0 on the first publish and lagged one run behind
    afterwards. Measuring both is deterministic and needs no repack, so it is
    safe to call before and after a publish."""
    p = subprocess.run(["git", "-C", directory, "count-objects", "-v"], capture_output=True, text=True)
    if p.returncode != 0:
        return None
    total_kib, found = 0, False
    for line in p.stdout.splitlines():
        if line.startswith(("size:", "size-pack:")):
            try:
                total_kib += int(line.split(":", 1)[1].strip())
            except ValueError:
                return None
            found = True
    return total_kib / 1024.0 if found else None


def gorge12(root=None):
    """First 12 hex digits of the pinned gorge commit (GORGE_PIN)."""
    env = os.environ.get("CARDDB_GORGE_PIN")
    if env:
        return env.strip()[:12]
    try:
        with open(os.path.join(root or repo_root(), "GORGE_PIN"), encoding="utf-8") as f:
            return f.read().strip()[:12] or "unknown"
    except OSError:
        return "unknown"


def _shard_column(table):
    """The column a table's rows are sharded on, None for a one-file table."""
    names = {c.name for c in table.columns}
    if table.name == "printings" and "set_code" in names:
        return "set_code"
    kcols = [c for c in table.columns if c.key]
    if kcols and kcols[0].name == "oracle_id":
        return "oracle_id"
    return None


def placement_follows_key(table):
    """True when a row's shard is a function of its key, so one key can live in one shard only.
    False for a table sharded on a column outside its key (printings by set_code): there a put
    can re-home a key, and uniqueness has to be checked across all the table's shards."""
    col = _shard_column(table)
    return col is None or col in {c.name for c in table.columns if c.key}


def shard_rel(table, cells):
    """Path, relative to the clone, of the shard that holds a row of table.

    A table whose first key is oracle_id shards on the first two hex digits of
    the id (x:<sha1> pseudo ids on `x` + two digits); printings shard by set;
    every other table is one file."""
    col = _shard_column(table)
    if col is None:
        return table.name + ".tsv"
    by_name = {c.name: i for i, c in enumerate(table.columns)}
    if col == "set_code":
        stem = cells[by_name["set_code"]]
    else:
        oid = cells[by_name["oracle_id"]]
        if oid.startswith("x:"):
            stem = "x" + oid[2:4]
        elif re.fullmatch(r"[0-9a-f]{2}", oid[:2]):
            stem = oid[:2]
        else:
            stem = "other"
    if not _SAFE_STEM.match(stem):
        raise ValidationError(["%s: %r cannot name a shard file" % (table.name, stem)])
    return "%s/%s.tsv" % (table.name, stem)


def tree_shards(root):
    """(rel, text) of every data shard under root: .tsv files outside .git, .github, schema."""
    for dp, dn, fns in os.walk(root):
        dn[:] = sorted(d for d in dn if d not in (".git", ".github", "schema"))
        for fn in sorted(fns):
            if fn.endswith(".tsv"):
                p = os.path.join(dp, fn)
                with open(p, encoding="utf-8", newline="") as f:
                    yield os.path.relpath(p, root).replace(os.sep, "/"), f.read()


def _atomic_write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp = "%s.tmp.%d" % (path, os.getpid())
    with open(tmp, "w", encoding="utf-8", newline="") as f:
        f.write(text)
    os.replace(tmp, path)


class Store:
    def __init__(self, kind=PUBLIC, home=None, schema=None, remote=None):
        if kind not in (PUBLIC, PRIVATE):
            raise StoreError("store kind must be public or private")
        self.kind = kind
        self.home = carddb_home(home)
        self.dir = os.path.join(self.home, kind)
        self.branch = PUBLIC_BRANCH if kind == PUBLIC else PRIVATE_BRANCH
        if remote is None:
            if kind == PUBLIC:
                remote = os.environ.get("CARDDB_PUBLIC_REMOTE", DEFAULT_PUBLIC_REMOTE)
            else:
                remote = os.environ.get("CARDDB_PRIVATE_REMOTE")
        self.remote = remote
        self._schema = schema

    @property
    def schema(self):
        if self._schema is None:
            self._schema = load_schema()
        return self._schema

    # ---- git ----------------------------------------------------------

    def git(self, *args, check=True, input=None, cwd=None):
        cmd = ["git", "-C", cwd or self.dir] + list(self._identity()) + list(args)
        env = dict(os.environ, GIT_TERMINAL_PROMPT="0", LC_ALL="C")
        p = subprocess.run(cmd, input=input, capture_output=True, text=True, env=env)
        if check and p.returncode != 0:
            raise GitError("git %s: %s" % (" ".join(args[:3]), (p.stderr or p.stdout).strip()))
        return p

    def _identity(self):
        if getattr(self, "_ident", None) is None:
            have = subprocess.run(["git", "-C", self.dir if os.path.isdir(self.dir) else ".", "config", "user.email"],
                                  capture_output=True, text=True).stdout.strip()
            self._ident = [] if have else ["-c", "user.name=carddb", "-c", "user.email=carddb@localhost"]
        return self._ident

    def _driver_command(self):
        main = os.path.join(repo_root(), "cmd", "carddb", "main.py")
        return "%s -I %s --schema %s merge-driver %%O %%A %%B %%P" % (
            shlex.quote(sys.executable), shlex.quote(main), shlex.quote(os.path.abspath(self.schema.root)))

    def configure(self):
        """Register the key-aware merge driver (.gitattributes `*.tsv merge=carddb`) in this clone."""
        self.git("config", "merge.carddb.name", "carddb key-aware shard merge")
        self.git("config", "merge.carddb.driver", self._driver_command())
        self.git("config", "core.autocrlf", "false")

    # ---- clone management --------------------------------------------

    def exists(self):
        return os.path.isdir(os.path.join(self.dir, ".git"))

    def _need_remote(self):
        if not self.remote:
            raise StoreError("no remote for the %s store: set CARDDB_%s_REMOTE" % (self.kind, self.kind.upper()))

    def ensure(self):
        """The clone, cloning the data branch when it is not there yet."""
        if not self.exists():
            self._need_remote()
            os.makedirs(self.home, exist_ok=True)
            p = subprocess.run(["git", "ls-remote", "--heads", self.remote, self.branch],
                               capture_output=True, text=True)
            if p.returncode != 0:
                raise GitError("git ls-remote %s: %s" % (self.remote, p.stderr.strip()))
            if not p.stdout.strip():
                raise StoreError("%s has no branch %s: run `carddb init` first" % (self.remote, self.branch))
            self.git("clone", "--quiet", "--origin", "origin", "--branch", self.branch,
                     "--single-branch", self.remote, self.dir, cwd=self.home)
            self._ident = None
        self.configure()
        return self

    def init(self):
        """Clone the data branch, or create it (an orphan holding .gitattributes) when the remote has none."""
        if self.exists():
            return self.ensure()
        self._need_remote()
        os.makedirs(self.home, exist_ok=True)
        p = subprocess.run(["git", "ls-remote", "--heads", self.remote, self.branch],
                           capture_output=True, text=True)
        if p.returncode != 0:
            raise GitError("git ls-remote %s: %s" % (self.remote, p.stderr.strip()))
        if p.stdout.strip():
            return self.ensure()
        self.git("init", "--quiet", "-b", self.branch, self.dir, cwd=self.home)
        self._ident = None
        self.git("remote", "add", "origin", self.remote)
        self.configure()
        _atomic_write(os.path.join(self.dir, ".gitattributes"), GITATTRIBUTES)
        paths = [".gitattributes"]
        wf = os.path.join(repo_root(), ".github", "workflows", "carddb.yml")
        if self.kind == PUBLIC and os.path.isfile(wf):
            os.makedirs(os.path.join(self.dir, ".github", "workflows"), exist_ok=True)
            shutil.copyfile(wf, os.path.join(self.dir, ".github", "workflows", "carddb.yml"))
            paths.append(".github/workflows/carddb.yml")
        self._check_paths(paths)
        self.git("add", "--", *paths)
        self.git("commit", "--quiet", "-m", "carddb: init")
        self.git("push", "--quiet", "origin", "HEAD:" + self.branch)
        return self

    @contextlib.contextmanager
    def lock(self):
        os.makedirs(os.path.join(self.dir, ".git"), exist_ok=True)
        fd = os.open(os.path.join(self.dir, ".git", "carddb.lock"), os.O_CREAT | os.O_RDWR, 0o644)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX)
            yield
        finally:
            os.close(fd)  # releases the flock

    # ---- validation ---------------------------------------------------

    def _check_paths(self, paths):
        """Validate (and, for a public store, boundary-check) files of the clone, and the keys the touched
        tables' shards hold across files. Raises ValidationError."""
        viol, tables = [], set()
        for rel in sorted(paths):
            p = os.path.join(self.dir, rel)
            if not os.path.isfile(p):
                continue
            with open(p, encoding="utf-8", errors="surrogateescape", newline="") as f:
                text = f.read()
            viol += self._check_text(rel, text)
            if shard.is_data_file(rel):
                hdr = shard.parse_shard_header(text.split("\n", 1)[0])
                if hdr:
                    tables.add(hdr[0])
        if tables:
            files = [(rel, text) for rel, text in tree_shards(self.dir)
                     if (shard.parse_shard_header(text.split("\n", 1)[0]) or ("",))[0] in tables]
            viol += shard.cross_shard_duplicates(self.schema, self.kind, files)
        if viol:
            raise ValidationError(viol)

    def _check_text(self, rel, text):
        viol = shard.validate_file(self.schema, self.kind, rel, text)
        if self.kind == PUBLIC:
            viol += boundary_text(self.schema, rel, text)
        return viol

    # ---- put ----------------------------------------------------------

    def _table(self, name):
        t = self.schema.tables.get(name)
        if t is None:
            raise StoreError("table %r is not declared in the schema" % name)
        if name.startswith("curated/"):
            raise StoreError("table %r is curated: hand-maintained files on develop, never written by put" % name)
        if not name.startswith("_meta/") and t.is_private() != (self.kind == PRIVATE):
            raise StoreError("table %r is %s: it belongs in the %s store" % (
                name, "private" if t.is_private() else "public", "private" if t.is_private() else "public"))
        return t

    def _cells(self, table, rows, raw, writer):
        cols = table.scope_columns(self.kind)
        names = [c.name for c in cols]
        out = []
        for r in rows:
            if isinstance(r, dict):
                extra = sorted(set(r) - set(names))
                if extra:
                    raise ValidationError(["%s: columns [%s] are not in the %s schema of the table" % (
                        table.name, " ".join(extra), self.kind)])
                vals = [r.get(n) for n in names]
            else:
                vals = list(r)
                if len(vals) != len(names):
                    raise ValidationError(["%s: row has %d cells, want %d (%s)" % (
                        table.name, len(vals), len(names), " ".join(names))])
            if "writer" in names:  # a row carries who wrote it: the merge driver ranks writers on it
                w = names.index("writer")
                if vals[w] in (None, "", "-"):
                    vals[w] = writer
            if raw:
                out.append(["-" if v is None or v == "" else str(v) for v in vals])
            else:
                out.append([shard.escape_cell(v) for v in vals])
        return cols, out

    def _table_shards(self, table):
        """Paths (relative to the clone) of the shard files that exist for table."""
        out = []
        flat = table.name + ".tsv"
        if os.path.isfile(os.path.join(self.dir, flat)):
            out.append(flat)
        root = os.path.join(self.dir, table.name)
        for dp, dn, fns in os.walk(root):
            dn.sort()
            out += [os.path.relpath(os.path.join(dp, fn), self.dir).replace(os.sep, "/")
                    for fn in sorted(fns) if fn.endswith(".tsv")]
        return out

    def _read_shard(self, table, cols, kidx, rel):
        """(text or None, {key: row}) of one shard of table as it is on disk."""
        path = os.path.join(self.dir, rel)
        if not os.path.isfile(path):
            return None, {}
        with open(path, encoding="utf-8", newline="") as f:
            text = f.read()
        try:
            hdr, head, rows = shard.parse_text(text)
        except ValueError as e:
            raise ValidationError(["%s: %s" % (rel, e)])
        if hdr != shard.shard_header(table.name, self.schema.version) or head != [c.name for c in cols]:
            raise ValidationError(["%s: existing shard header does not match the schema (%r)" % (rel, hdr)])
        out = {}
        for r in rows:
            if len(r) != len(cols):
                raise ValidationError(["%s: existing row has %d cells, want %d" % (rel, len(r), len(cols))])
            out[shard.row_key(r, kidx)] = r
        return text, out

    @staticmethod
    def _writer_tie(cols, old, new):
        """True when new differs from old in the writer column only and old's writer ranks at least as
        high: a lower-priority rerun must not flip the row back and forth."""
        names = [c.name for c in cols]
        if "writer" not in names:
            return False
        w = names.index("writer")
        if old[:w] + old[w + 1:] != new[:w] + new[w + 1:]:
            return False
        return WRITER_PRIORITY.get(old[w], 0) >= WRITER_PRIORITY.get(new[w], 0)

    def _stale_legs(self, table, cols, cells, legacy):
        """[(rel, key)] of the stored xmage/forge/cite rows of the legacy keys in legacy (see
        keylegs.split_batch) that the batch does not rewrite: a legacy row states the whole key, so a leg it
        no longer holds (FX narrowed to X, diagnosed turned agreed) must not linger and outvote it."""
        kidx = shard.key_indexes(cols)
        by_key = {}
        for r in cells:
            k = shard.row_key(r, kidx)
            if (k[0], k[1]) in legacy:
                by_key.setdefault((k[0], k[1]), r)
        kept = {shard.row_key(r, kidx) for r in cells}
        seen, out = {}, []
        for ok, r in sorted(by_key.items()):
            rel = self._shard_rel_scoped(table, cols, r)
            if rel not in seen:
                seen[rel] = self._read_shard(table, cols, kidx, rel)[1]
            for k in sorted(seen[rel]):
                if (k[0], k[1]) == ok and k[2] in keylegs.LEGACY_LEGS and k not in kept:
                    out.append((rel, k))
        return out

    def _only_absent(self, table, cols, cells, refresh):
        """The surviving rows of an --if-absent put: each row whose key no stored row
        holds passes through; a row whose key exists is dropped, except that the
        stored row takes the input's value for every column named in refresh (an
        update of those cells, not a replace)."""
        names = [c.name for c in cols]
        ri = []
        for col in refresh:
            if col not in names:
                raise StoreError("put: refresh column %r is not a column of %s" % (col, table.name))
            ri.append(names.index(col))
        kidx = shard.key_indexes(cols)
        shards = {}

        def stored(rel):
            if rel not in shards:
                shards[rel] = self._read_shard(table, cols, kidx, rel)[1]
            return shards[rel]

        out = []
        for r in cells:
            d = stored(self._shard_rel_scoped(table, cols, r))
            old = d.get(shard.row_key(r, kidx))
            if old is None:
                out.append(r)
            elif ri:
                merged = list(old)
                for i in ri:
                    merged[i] = r[i]
                if merged != old:
                    out.append(merged)
        return out

    def _stored_rows(self, table, cols):
        """[(rel, key, {column: cell})] of every stored row of table, in shard then key order."""
        names = [c.name for c in cols]
        kidx = shard.key_indexes(cols)
        return [(rel, k, dict(zip(names, r)))
                for rel in self._table_shards(table)
                for k, r in sorted(self._read_shard(table, cols, kidx, rel)[1].items())]

    def _check_seed_batch(self, table, cols, cells):
        """-> the keys of a --prune-seed batch. The prune deletes by absence, so it refuses (StoreError) a
        batch that is not the seed's own, or one naming fewer than half of the cards that have a stored
        seed row: a partial seed file must not delete the rest of the seed."""
        names = [c.name for c in cols]
        w, oid = names.index("writer"), names.index("oracle_id")
        others = sorted({r[w] for r in cells} - {keylegs.SEED_WRITER})
        if others:
            raise StoreError("put: prune_seed takes seed rows only, the batch has rows of writer %s; "
                             "nothing written" % ", ".join(others))
        seeded = {row["oracle_id"] for _, _, row in self._stored_rows(table, cols)
                  if row["writer"] == keylegs.SEED_WRITER}
        named = seeded & {r[oid] for r in cells}
        if 2 * len(named) < len(seeded):
            raise StoreError("put: prune_seed refuses a partial seed: the batch names %d of the %d cards with "
                             "a stored seed row (want at least half); nothing written" % (len(named), len(seeded)))
        kidx = shard.key_indexes(cols)
        return {shard.row_key(r, kidx) for r in cells}

    def _keyproof_multiplayer(self, table, cols, cells):
        """The one keyproof multiplayer rule, for every writer but the seed: the flag
        belongs to the key, and only the seed -- which reads the card's script -- may
        set it. Every non-seed row's `multiplayer` is rewritten before planning: a row
        replacing a stored row keeps the stored value; a new row takes the value of the
        first (by key order) stored row with the same (oracle_id, key), else false."""
        names = [c.name for c in cols]
        if "multiplayer" not in names or "leg" not in names:
            return cells  # a pre-leg keyproof shape: split_batch has set false, leave it
        mi = names.index("multiplayer")
        kidx = shard.key_indexes(cols)
        shards = {}
        out = []
        for r in cells:
            rel = self._shard_rel_scoped(table, cols, r)
            if rel not in shards:
                shards[rel] = self._read_shard(table, cols, kidx, rel)[1]
            d = shards[rel]
            k = shard.row_key(r, kidx)
            old = d.get(k)
            if old is not None:
                v = old[mi]
            else:
                v = "false"
                for ok in sorted(d):
                    if ok[0] == k[0] and ok[1] == k[1]:
                        v = d[ok][mi]
                        break
            if r[mi] != v:
                r = list(r)
                r[mi] = v
            out.append(r)
        return out

    def _plan(self, table, cols, rows, replace=False, drop=()):
        """(plan {rel: text, or None to delete the file}, changed row count) of merging rows (cells) into
        table's shards. A key keeps one home: when a row lands in a different shard than the one that holds
        its key (a table sharded outside its key), the old copy goes. replace drops every existing key of the
        table that the batch does not name, so a full catalogue ingest cannot leave a stale row behind."""
        kidx = shard.key_indexes(cols)
        incoming = {}  # key -> (rel, row); the last row of a key in the batch wins
        for r in rows:
            incoming[shard.row_key(r, kidx)] = (self._shard_rel_scoped(table, cols, r), r)
        state = {}  # rel -> [old text, {key: row}]

        def load(rel):
            if rel not in state:
                state[rel] = list(self._read_shard(table, cols, kidx, rel))
            return state[rel][1]

        changed, touched = 0, {rel for rel, _ in incoming.values()}
        homes = None  # key -> shards holding it; a table whose shard is not a function of its key, or a replace
        if replace or not placement_follows_key(table):
            homes = {}
            for rel in self._table_shards(table):
                for k in load(rel):
                    homes.setdefault(k, set()).add(rel)
        if replace:
            for k in sorted(set(homes) - set(incoming)):
                for rel in sorted(homes[k]):
                    del state[rel][1][k]
                    touched.add(rel)
                changed += 1
        for rel, k in drop:  # stored rows the batch retires (a legacy put narrowing a key)
                load(rel).pop(k, None)
                touched.add(rel)
                changed += 1
        for k in sorted(incoming):
            rel, r = incoming[k]
            if homes is not None:
                for other in sorted(homes.get(k, ())):
                    if other != rel:  # the key leaves its old shard
                        del state[other][1][k]
                        touched.add(other)
                homes[k] = {rel}
            d = load(rel)
            old = d.get(k)
            if old is not None and self._writer_tie(cols, old, r):
                continue
            if old != r:
                changed += 1
            d[k] = r
        plan = {}
        for rel in sorted(touched):
            old_text, d = state[rel] if rel in state else (None, {})
            if not d:
                if old_text is not None:
                    plan[rel] = None
                continue
            text = shard.render(table.name, self.schema.version, [c.name for c in cols], [d[k] for k in sorted(d)])
            if text != old_text:
                plan[rel] = text
        return plan, changed

    def _cross_shard_check(self, table, plan):
        """Violations of key uniqueness across all of table's shards as they would be after plan."""
        files = {}
        for rel in self._table_shards(table):
            with open(os.path.join(self.dir, rel), encoding="utf-8", errors="surrogateescape", newline="") as f:
                files[rel] = f.read()
        for rel, text in plan.items():
            if text is None:
                files.pop(rel, None)
            elif rel.startswith(table.name + "/") or rel == table.name + ".tsv":
                files[rel] = text
        return shard.cross_shard_duplicates(self.schema, self.kind, files.items())

    def _shard_rel_scoped(self, table, cols, cells):
        # a table with columns hidden in this scope: place by the visible cells
        full = [shard.EMPTY] * len(table.columns)
        idx = {c.name: i for i, c in enumerate(table.columns)}
        for c, v in zip(cols, cells):
            full[idx[c.name]] = v
        return shard_rel(table, full)

    def apply_plan(self, table, plan, writer, changed):
        """Validate plan (rel -> text, None deletes), write it, `git add` it and ledger it. The caller holds
        the lock. Raises ValidationError, writing nothing."""
        viol = []
        for rel, text in sorted(plan.items()):
            if text is not None:
                viol += self._check_text(rel, text)
        if not placement_follows_key(table):
            viol += self._cross_shard_check(table, plan)
        if viol:
            raise ValidationError(viol)
        for rel, text in plan.items():
            if text is None:
                with contextlib.suppress(FileNotFoundError):
                    os.remove(os.path.join(self.dir, rel))
            else:
                _atomic_write(os.path.join(self.dir, rel), text)
        if plan:
            self.git("add", "-A", "--", *sorted(plan))
            with open(os.path.join(self.dir, ".git", LEDGER), "a", encoding="utf-8") as f:
                f.write("%s\t%s\t%d\n" % (writer, table.name, changed))

    def put(self, table_name, rows, writer=None, version=None, raw=False, ran=None, replace=False,
            if_absent=False, refresh=(), prune_seed=False):
        """Merge rows into table_name's shards, validate, write, `git add`. -> rows changed.

        rows: dicts (column -> value) or sequences in column order. A rerun with the
        same rows changes no byte, writers.tsv included. replace also deletes every
        existing key of the table that rows does not name (a full snapshot writer).

        if_absent drops every input row whose key a stored row already holds instead
        of overwriting it (the keyproof seed put: absent work becomes owed, measured
        work stays). refresh (column names, only meaningful with if_absent) excepts
        them: the stored row takes the input's value for each named column. For every
        writer but the seed, a keyproof row never takes `multiplayer` from the input
        (see _keyproof_multiplayer).

        prune_seed (keyproof only; rows is the whole seed file) also deletes the
        untried seed rows whose key rows does not name (keylegs.seed_prune) and
        leaves the counts in last_pruned and last_orphans."""
        self.last_pruned = self.last_orphans = 0
        writer = writer or os.environ.get("CARDDB_WRITER") or "cli"
        if not _WRITER.match(writer):
            raise StoreError("writer %r is not a valid writer name" % writer)
        if refresh and not if_absent:
            raise StoreError("put: refresh needs if_absent")
        if prune_seed and table_name != keylegs.TABLE:
            raise StoreError("put: prune_seed applies to the %s table only" % keylegs.TABLE)
        table = self._table(table_name)
        rows, legacy = list(rows), {}
        if table_name == keylegs.TABLE:
            rows, legacy = keylegs.split_batch(rows)  # the proof loop's one-row-per-key shape -> leg rows
        self.ensure()
        cols, cells = self._cells(table, rows, raw, writer)
        # the keys of the whole input: if_absent must not make a key the seed still names look absent
        batch_keys = self._check_seed_batch(table, cols, cells) if prune_seed else None
        with self.lock():
            if if_absent:
                cells = self._only_absent(table, cols, cells, tuple(refresh))
            if table_name == keylegs.TABLE and writer != "seed":
                cells = self._keyproof_multiplayer(table, cols, cells)
            drop = self._stale_legs(table, cols, cells, legacy) if legacy else []
            pruned, orphans = [], 0
            if prune_seed:
                pruned, orphans = keylegs.seed_prune(self._stored_rows(table, cols), batch_keys)
                drop = list(dict.fromkeys(drop + pruned))  # a row both lists retire counts once
            plan, changed = self._plan(table, cols, cells, replace=replace, drop=drop)
            if plan and table_name != "_meta/writers":
                wt = self.schema.tables["_meta/writers"]
                wcols = wt.scope_columns(self.kind)
                wrow = [writer, table_name, version or os.environ.get("CARDDB_WRITER_VERSION") or "unversioned",
                        today(ran)]
                wplan, _ = self._plan(wt, wcols, [wrow])
                plan.update(wplan)
            self.apply_plan(table, plan, writer, changed)
        self.last_pruned, self.last_orphans = len(pruned), orphans
        return changed

    # ---- reading ------------------------------------------------------

    def rows(self, table_name):
        """All rows of a table as dicts (cells as stored, `-` for empty)."""
        t = self.schema.tables[table_name]
        names = [c.name for c in t.scope_columns(self.kind)]
        for rel, text in tree_shards(self.dir):
            if rel == table_name + ".tsv" or rel.startswith(table_name + "/"):
                _h, _c, rows = shard.parse_text(text)
                for r in rows:
                    yield dict(zip(names, r))

    def cards(self):
        return [(r["oracle_id"], r["name"]) for r in self.rows("cards")]

    def flush_unresolved(self, resolver, writer="resolve"):
        """Record a Resolver's misses in _meta/unresolved. -> rows changed."""
        rows = [{"kind": "card", "ref": name, "reason": "no card of this name; pseudo id " + pid}
                for name, pid in sorted(resolver.misses.items())]
        if not rows:
            return 0
        return self.put("_meta/unresolved", rows, writer=writer)

    # ---- publish ------------------------------------------------------

    def _staged(self):
        return [l for l in self.git("diff", "--cached", "--name-only").stdout.split("\n") if l]

    def _ahead(self):
        p = self.git("rev-list", "--count", "origin/%s..HEAD" % self.branch, check=False)
        if p.returncode == 0:
            return int(p.stdout.strip() or 0)
        return int(self.git("rev-list", "--count", "HEAD").stdout.strip() or 0)

    def _ledger(self):
        path = os.path.join(self.dir, ".git", LEDGER)
        writers, rows = set(), 0
        if os.path.isfile(path):
            with open(path, encoding="utf-8") as f:
                for l in f:
                    w, _t, n = l.rstrip("\n").split("\t")
                    writers.add(w)
                    rows += int(n)
        return sorted(writers), rows

    def publish(self, bulk=False, pin=None, tries=6):
        """Commit what is staged (one commit), rebase onto the remote branch, push fast-forward.

        -> dict(committed, pushed, rows, message), or None when nothing is staged or unpushed."""
        self.ensure()
        with self.lock():
            staged = self._staged()
            if not staged and self._ahead() == 0:
                return None
            result = {"committed": False, "pushed": False, "rows": 0, "message": ""}
            if staged:
                self._check_paths(staged)
                lines = 0
                for l in self.git("diff", "--cached", "--numstat").stdout.splitlines():
                    a, d = l.split("\t")[:2]
                    lines += (int(a) if a.isdigit() else 0) + (int(d) if d.isdigit() else 0)
                if lines > MAX_PUBLISH_LINES and not bulk:
                    raise StoreError("%d changed lines exceed %d: pass --bulk to publish them" % (lines, MAX_PUBLISH_LINES))
                writers, rows = self._ledger()
                if not writers:
                    writers, rows = ["unknown"], lines
                msg = "carddb: %s %d rows @%s" % (",".join(writers), rows, pin or gorge12())
                self.git("commit", "--quiet", "-m", msg)
                with contextlib.suppress(FileNotFoundError):
                    os.remove(os.path.join(self.dir, ".git", LEDGER))
                result.update(committed=True, rows=rows, message=msg)
            last = None
            for _ in range(tries):
                self._rebase()
                self._check_paths(self._changed_vs_remote())
                p = self.git("push", "--quiet", "origin", "HEAD:" + self.branch, check=False)
                if p.returncode == 0:
                    result["pushed"] = True
                    return result
                last = p.stderr.strip()
            raise GitError("push failed after %d tries: %s" % (tries, last))

    def _remote_has_branch(self):
        p = self.git("ls-remote", "--heads", "origin", self.branch)
        return bool(p.stdout.strip())

    def _rebase(self):
        if not self._remote_has_branch():
            return
        self.git("fetch", "--quiet", "origin", self.branch)
        ref = "origin/" + self.branch
        if self.git("merge-base", "--is-ancestor", ref, "HEAD", check=False).returncode == 0:
            return
        p = self.git("rebase", ref, check=False)
        if p.returncode != 0:
            self.git("rebase", "--abort", check=False)
            raise GitError("rebase onto %s failed (local commit kept, retry publish): %s" % (
                ref, (p.stdout + p.stderr).strip()[:2000]))

    def _changed_vs_remote(self):
        ref = "origin/" + self.branch
        if self.git("rev-parse", "--verify", "--quiet", ref, check=False).returncode == 0:
            out = self.git("diff", "--name-only", "--diff-filter=ACMR", ref, "HEAD").stdout
        else:
            out = self.git("ls-files").stdout
        return [l for l in out.split("\n") if l]
