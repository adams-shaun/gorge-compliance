"""build: shards + curated files + committed sources -> one SQLite file.

Inputs
  the public clone's shards                          ($CARDDB_HOME/public)
  with --private, the private clone's shards         ($CARDDB_HOME/private)
  curated files and committed sources, read from this checkout's `HEAD`
  (carddb/curated/*, oracle/compliance/manifests/*.json,
  oracle/xengine/diagnosed.tsv), or from a directory laid out the same way

Output (one table per schema table; `/` and `-` in a name become `_`):
  <table>          the schema's columns, `-` -> NULL, PRIMARY KEY on the key columns
  card_names       norm_name -> oracle_id for every card name and back face
  card_fts         FTS5 over name, type_line, keywords, labels (+ text with --text)
  src_manifest_sets, src_manifest_cards, src_diagnosed    the committed sources
  build_meta       what the file was built from
No Oracle text is in the file unless --text is given, and such a file is never
the published one (it is named carddb-text.sqlite, never carddb.sqlite).
"""
import io
import json
import os
import sqlite3
import subprocess
import tarfile

from . import shard
from .boundary import boundary_tree
from .norm import name_keys
from .schema import PRIVATE, PUBLIC, repo_root
from .shard import ValidationError
from .store import StoreError, carddb_home

CURATED_PREFIX = "carddb/curated/"
MANIFEST_PREFIX = "oracle/compliance/manifests/"
DIAGNOSED = "oracle/xengine/diagnosed.tsv"
SOURCE_PATHS = [CURATED_PREFIX.rstrip("/"), MANIFEST_PREFIX.rstrip("/"), DIAGNOSED]

_SQL_TYPES = {"string": "TEXT", "list": "TEXT", "date": "TEXT", "int": "INTEGER", "bool": "INTEGER"}
_INDEXED = ("oracle_id", "norm_name", "set_code", "name", "format", "level", "status", "label", "tier", "source")

SOURCE_DDL = [
    "CREATE TABLE src_manifest_sets (code TEXT NOT NULL, name TEXT, released TEXT, set_type TEXT, "
    "xmage_ref TEXT, PRIMARY KEY (code)) WITHOUT ROWID",
    "CREATE TABLE src_manifest_cards (set_code TEXT NOT NULL, name TEXT NOT NULL, numbers TEXT, rarity TEXT)",
    "CREATE INDEX idx_src_manifest_cards_set_code ON src_manifest_cards (set_code)",
    "CREATE INDEX idx_src_manifest_cards_name ON src_manifest_cards (name)",
    "CREATE TABLE src_diagnosed (kind TEXT, set_code TEXT, card TEXT, field TEXT, wrong TEXT, cr TEXT, "
    "evidence TEXT, upstream TEXT, ticket TEXT)",
    "CREATE INDEX idx_src_diagnosed_card ON src_diagnosed (card)",
    "CREATE TABLE build_meta (key TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY (key)) WITHOUT ROWID",
]


def sql_name(table):
    return table.replace("/", "_").replace("-", "_")


def q(ident):
    """A quoted SQL identifier (`table`, `values`, `text` are all column or table names)."""
    return '"%s"' % ident


def read_committed(repo=None, ref="HEAD", directory=None):
    """{repo-relative path: bytes} of the curated files and committed sources.

    The default ref is the checkout's own HEAD, the same tree the schema is read
    from: a branch that adds a curated column must be able to build itself before
    it is promoted.  Pass --ref <ref> (or --src-dir) to build from another tree; that tree
    must carry the curated schema this checkout expects (a ref from before the
    citation columns fails validation, e.g. origin/main until this branch lands)."""
    out = {}
    if directory:
        for p in SOURCE_PATHS:
            full = os.path.join(directory, p)
            if os.path.isfile(full):
                with open(full, "rb") as f:
                    out[p] = f.read()
            elif os.path.isdir(full):
                for fn in sorted(os.listdir(full)):
                    with open(os.path.join(full, fn), "rb") as f:
                        out[p + "/" + fn] = f.read()
        return out
    repo = repo or repo_root()
    git = lambda *a: subprocess.run(["git", "-C", repo] + list(a), capture_output=True)
    if git("rev-parse", "--verify", "--quiet", ref + "^{commit}").returncode != 0:
        raise StoreError("%s: no such ref in %s (git fetch it, or pass --ref / --src-dir)" % (ref, repo))
    for path in SOURCE_PATHS:  # one archive per path: a ref that lacks one just has no such inputs
        if git("cat-file", "-e", "%s:%s" % (ref, path)).returncode != 0:
            continue
        proc = git("archive", "--format=tar", ref, "--", path)
        if proc.returncode != 0:
            raise StoreError("git archive %s %s: %s" % (ref, path, proc.stderr.decode("utf-8", "replace").strip()))
        with tarfile.open(fileobj=io.BytesIO(proc.stdout), mode="r:") as tf:
            for m in tf.getmembers():
                if m.isfile():
                    out[m.name] = tf.extractfile(m).read()
    return out


def _decode(rel, b):
    try:
        return b.decode("utf-8")
    except UnicodeDecodeError:
        raise ValidationError(["%s: not valid UTF-8" % rel])


def _load_shards(schema, scope, files, public_boundary):
    """files: iterable of (rel, text) of a whole tree -> {table: [row dicts]}.

    Every file is validated as Go's Validate does (a hidden shard, a stray schema/ file), keys are checked
    across a table's shards, and a public tree is boundary-checked; any violation refuses the build."""
    files = sorted(files)
    viol = shard.validate_tree(schema, scope, files)
    if public_boundary:
        viol += boundary_tree(schema, files)
    if viol:
        raise ValidationError(viol)
    tables = {}
    for rel, text in files:
        if shard.is_data_file(rel):
            hdr, head, rows = shard.parse_text(text)
            name = shard.parse_shard_header(hdr)[0]
            tables.setdefault(name, []).extend(dict(zip(head, r)) for r in rows)
    return tables


def _value(col, cell):
    if cell == "-":
        return None
    if col.type == "int":
        return int(cell)
    if col.type == "bool":
        return 1 if cell == "true" else 0
    return cell


def _create_table(conn, table, cols):
    parts = ["%s %s%s" % (q(c.name), _SQL_TYPES[c.type], " NOT NULL" if c.required else "") for c in cols]
    keys = [c.name for c in cols if c.key]
    t = q(sql_name(table.name))
    conn.execute("CREATE TABLE %s (%s, PRIMARY KEY (%s)) WITHOUT ROWID" % (
        t, ", ".join(parts), ", ".join(q(k) for k in keys)))
    for c in cols:
        if c.name in _INDEXED and not (keys and c.name == keys[0]):
            conn.execute("CREATE INDEX %s ON %s (%s)" % (q("idx_%s_%s" % (sql_name(table.name), c.name)), t, q(c.name)))


def load_text(path):
    """{oracle_id or 'name:<name>': oracle text} from a Scryfall oracle-cards list or an MTGJSON AtomicCards file."""
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    out = {}
    if isinstance(data, dict) and "data" in data:  # MTGJSON AtomicCards
        for name, faces in sorted(data["data"].items()):
            texts = [x.get("text") for x in faces if x.get("text")]
            oid = next((x.get("identifiers", {}).get("scryfallOracleId") for x in faces
                        if x.get("identifiers", {}).get("scryfallOracleId")), None)
            out[oid or "name:" + name] = "\n//\n".join(texts)
    elif isinstance(data, list):  # Scryfall
        for c in data:
            t = c.get("oracle_text")
            if t is None and c.get("card_faces"):
                t = "\n//\n".join(f.get("oracle_text", "") for f in c["card_faces"])
            out[c.get("oracle_id") or "name:" + c.get("name", "")] = t or ""
    else:
        raise StoreError("%s: neither Scryfall oracle-cards nor MTGJSON AtomicCards" % path)
    return out


def _sources(conn, committed):
    for rel in sorted(k for k in committed if k.startswith(MANIFEST_PREFIX) and k.endswith(".json")):
        m = json.loads(_decode(rel, committed[rel]))
        code = m["code"]
        conn.execute("INSERT INTO src_manifest_sets VALUES (?,?,?,?,?)",
                     (code, m.get("name"), m.get("released"), m.get("set_type"), m.get("xmage_ref")))
        for c in m.get("cards", []):
            conn.execute("INSERT INTO src_manifest_cards VALUES (?,?,?,?)",
                         (code, c["name"], ",".join(c.get("numbers", [])) or None, c.get("rarity")))
    if DIAGNOSED in committed:
        lines = _decode(DIAGNOSED, committed[DIAGNOSED]).split("\n")
        head = lines[0].split("\t")
        want = ["kind", "set", "card", "field", "wrong", "cr", "evidence", "upstream", "ticket"]
        if head != want:
            raise ValidationError(["%s: columns %r, want %r" % (DIAGNOSED, head, want)])
        for l in lines[1:]:
            if l:
                cells = l.split("\t")
                cells += [""] * (len(want) - len(cells))
                conn.execute("INSERT INTO src_diagnosed VALUES (?,?,?,?,?,?,?,?,?)",
                             [c or None for c in cells[:len(want)]])


def build(out, schema, public_dir, private_dir=None, committed=None, text_file=None, revs=None):
    """Build the SQLite file at out. committed: read_committed()'s dict. revs: {key: value} for build_meta."""
    committed = committed or {}
    private = private_dir is not None
    if text_file and os.path.basename(out) == "carddb.sqlite":
        raise StoreError("a build with Oracle text is never the published file: name it carddb-text.sqlite")
    pub = _load_shards(schema, PUBLIC, shard.tree_files(public_dir), True)
    over = _load_shards(schema, PRIVATE, shard.tree_files(private_dir), False) if private else {}
    cur_shards = [(k[len("carddb/"):], _decode(k, v)) for k, v in sorted(committed.items())
                  if k.startswith(CURATED_PREFIX) and k.endswith(".tsv")]
    cur = _load_shards(schema, PUBLIC, cur_shards, True)
    texts = load_text(text_file) if text_file else None

    tmp = out + ".tmp.%d" % os.getpid()
    if os.path.exists(tmp):
        os.remove(tmp)
    conn = sqlite3.connect(tmp)
    try:
        conn.execute("PRAGMA journal_mode=OFF")
        conn.execute("PRAGMA synchronous=OFF")
        for name in schema.table_names():
            t = schema.tables[name]
            if t.is_private() and not private and not name.startswith("_meta/"):
                continue
            cols = t.columns if private else t.scope_columns(PUBLIC)
            _create_table(conn, t, cols)
            rows = pub.get(name, []) + over.get(name, []) + cur.get(name, [])
            kn = [c.name for c in cols if c.key]
            rows.sort(key=lambda r: tuple(r[k] for k in kn))
            for r in rows:
                conn.execute("INSERT INTO %s VALUES (%s)" % (q(sql_name(name)), ",".join("?" * len(cols))),
                             [_value(c, r[c.name]) for c in cols])
        for ddl in SOURCE_DDL:
            conn.execute(ddl)
        _sources(conn, committed)
        _derived(conn, schema, texts)
        meta = {"schema_version": str(schema.version), "scope": "private" if private else "public",
                "text": "yes" if texts is not None else "no"}
        meta.update(revs or {})
        conn.executemany("INSERT INTO build_meta VALUES (?,?)", sorted(meta.items()))
        conn.execute("PRAGMA user_version=%d" % schema.version)
        conn.commit()
        conn.execute("VACUUM")
    except BaseException:
        conn.close()
        if os.path.exists(tmp):
            os.remove(tmp)
        raise
    conn.close()
    os.replace(tmp, out)
    return out


def _derived(conn, schema, texts):
    conn.execute("CREATE TABLE card_names (norm_name TEXT NOT NULL, oracle_id TEXT NOT NULL, name TEXT NOT NULL, "
                 "PRIMARY KEY (norm_name, oracle_id)) WITHOUT ROWID")
    cards = conn.execute("SELECT oracle_id, name, type_line FROM cards ORDER BY oracle_id").fetchall()
    has_kw = any(c.name == "keywords" for c in schema.tables["cards"].columns)
    kw = dict(conn.execute("SELECT oracle_id, keywords FROM cards").fetchall()) if has_kw else {}
    labels = {}
    for tbl in ("labels", "curated_labels"):
        for oid, lab in conn.execute("SELECT oracle_id, label FROM %s" % q(tbl)):
            labels.setdefault(oid, set()).add(lab)
    norm_to_oid = {}
    for oid, name, _tl in cards:
        for k in name_keys(name):
            conn.execute("INSERT OR IGNORE INTO card_names VALUES (?,?,?)", (k, oid, name))
            norm_to_oid.setdefault(k, oid)
    cols = "oracle_id UNINDEXED, name, type_line, keywords, labels" + (", text" if texts is not None else "")
    conn.execute("CREATE VIRTUAL TABLE card_fts USING fts5(%s, tokenize='porter unicode61')" % cols)
    by_oid = {}
    if texts is not None:
        for k, v in texts.items():
            if k.startswith("name:"):
                oid = norm_to_oid.get(next(iter(name_keys(k[5:])), ""))
                if oid:
                    by_oid.setdefault(oid, v)
            else:
                by_oid[k] = v
    for oid, name, tl in cards:
        row = [oid, name, tl, (kw.get(oid) or "").replace(",", " "), " ".join(sorted(labels.get(oid, ())))]
        if texts is not None:
            row.append(by_oid.get(oid, ""))
        conn.execute("INSERT INTO card_fts VALUES (%s)" % ",".join("?" * len(row)), row)
    conn.execute("CREATE INDEX idx_card_names_oracle_id ON card_names (oracle_id)")
    # Imported here: query imports this module for `q`/`sql_name`, so a
    # module-level import would be circular.
    from .query import (CARD_PROVEN_VIEW_SQL, DECK_PROVEN_VIEW_SQL, KEY_LEGS_VIEW_SQL, KEY_PROVEN_VIEW_SQL,
                        KEYPROOF_CARDS_VIEW_SQL, KEYPROOF_KEYS_VIEW_SQL, PROVEN_KEYS_VIEW_SQL,
                        PROVEN_VIEW_SQL, SCOPE_CARDS_VIEW_SQL)
    conn.execute(PROVEN_VIEW_SQL)
    conn.execute(KEYPROOF_KEYS_VIEW_SQL)
    conn.execute(KEYPROOF_CARDS_VIEW_SQL)
    conn.execute(KEY_LEGS_VIEW_SQL)
    conn.execute(KEY_PROVEN_VIEW_SQL)
    conn.execute(CARD_PROVEN_VIEW_SQL)
    conn.execute(PROVEN_KEYS_VIEW_SQL)
    # Scope needs the legality table. A partial build without it has no
    # denominator; readers report the missing view instead of guessing.
    if conn.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name='legality'").fetchone():
        conn.execute(SCOPE_CARDS_VIEW_SQL)
    if conn.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name='decks'").fetchone():
        conn.execute(DECK_PROVEN_VIEW_SQL)


def dump_text(conn):
    """A deterministic text form of a built file (the golden): schema, user_version, rows, FTS rows."""
    out = ["-- user_version %d" % conn.execute("PRAGMA user_version").fetchone()[0]]
    objs = conn.execute("SELECT type, name, sql FROM sqlite_master WHERE sql IS NOT NULL "
                        "AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'card_fts_%' ORDER BY type, name").fetchall()
    out.append("-- schema")
    out.extend("%s;" % sql for _t, _n, sql in objs)
    for typ, name, _sql in objs:
        if typ != "table":
            continue
        out.append("-- rows %s" % name)
        if name == "card_fts":
            sel = "SELECT * FROM card_fts ORDER BY oracle_id"
        else:
            sel = "SELECT * FROM %s ORDER BY 1,2,3,4" % name if name.startswith("src_") else "SELECT * FROM %s ORDER BY 1,2" % q(name)
        for r in conn.execute(sel):
            out.append("\t".join("NULL" if v is None else str(v) for v in r))
    return "\n".join(out) + "\n"
