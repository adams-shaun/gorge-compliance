"""Read verbs over a built carddb SQLite file (DESIGN section 6).

The file is a cache made by `carddb build` or `carddb fetch`; every verb here
opens it read-only. A verb never writes and never reports a wall-clock value
unless the caller asks (freshness, whose whole job is age), so an answer is a
function of the file it read.

  card <name|oracle_id>       every fact for one card
  search <fts>                FTS5 search with legality/status/set/label filters
  blockers --format F         legal cards that are not covered, ranked
  proven [--format F]         the certification-pinned proven view
  deck <id>                   a private deck's cards
  freshness [--tick]          writer SLAs, publish/asset age, unresolved growth, branch size
  sql '<select>'              read-only SQL
  export <view> --tsv         a deterministic TSV of a table or view
"""
import json
import os
import re
import sqlite3
import subprocess
from datetime import datetime, timezone

from .build import q, sql_name
from .norm import norm_name
from .schema import PRIVATE, PRIVATE_TABLE_NAMES, PUBLIC
from .shard import parse_writers, read_text
from .store import Store, StoreError, asset_meta_path, carddb_home, packed_mib

VERBS = ("card", "search", "blockers", "proven", "deck", "freshness", "sql", "export")

# A printing in one of these set types never makes a card tournament-legal.
# MTGJSON's set type is the only paper/digital marker the v1 schema carries;
# an unknown or missing set row is treated as paper (conservative: keep).
NON_TOURNAMENT_SET_TYPES = (
    "alchemy", "archenemy", "digital", "funny", "memorabilia", "minigame",
    "non-tournament", "planechase", "spellbook", "token", "treasure_chest",
    "vanguard",
)

LEVEL_ORDER = ("C0", "C1", "C2", "C3")


class QueryError(StoreError):
    """A read verb failed. code follows the tool's exit status convention."""

    def __init__(self, message, code=2):
        super().__init__(message)
        self.code = code


def default_db(home=None, private=False):
    return os.path.join(carddb_home(home), "carddb-private.sqlite" if private else "carddb.sqlite")


def connect(path):
    """(connection, build_meta) of a built carddb file, opened read-only."""
    if not path or not os.path.isfile(path):
        raise QueryError("no built database at %s: run `carddb build` or `carddb fetch`" % (path or "?"))
    conn = None
    try:
        conn = sqlite3.connect("file:%s?mode=ro" % os.path.abspath(path), uri=True)
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA query_only=ON")
        version = conn.execute("PRAGMA user_version").fetchone()[0]
        meta = dict(conn.execute("SELECT key, value FROM build_meta").fetchall())
    except sqlite3.DatabaseError as e:
        if conn is not None:
            conn.close()
        raise QueryError("%s: not a carddb database (%s)" % (path, e))
    if version < 1 or "scope" not in meta:
        conn.close()
        raise QueryError("%s: not a carddb database (user_version %d)" % (path, version))
    return conn, meta


def _cell(v):
    if v is None:
        return "NULL"
    return (str(v).replace("\\", "\\\\").replace("\t", "\\t")
            .replace("\n", "\\n").replace("\r", "\\r"))


def _rows(conn, sql, params=()):
    return conn.execute(sql, params).fetchall()


def _tables(conn):
    return [r[0] for r in _rows(conn, "SELECT name FROM sqlite_master WHERE type IN ('table','view') "
                                      "AND name NOT LIKE 'sqlite_%' ORDER BY name")]


def _has_table(conn, name):
    return conn.execute("SELECT 1 FROM sqlite_master WHERE type IN ('table','view') AND name=?",
                        (name,)).fetchone() is not None


def _columns(conn, table):
    return [r[1] for r in _rows(conn, "PRAGMA table_info(%s)" % q(table))]


def _emit(out, columns, rows):
    print("\t".join(columns), file=out)
    for row in rows:
        print("\t".join(_cell(v) for v in row), file=out)


def _limit(n, verb):
    if n is not None and n < 0:
        raise QueryError("%s: --limit must be at least 0" % verb)
    return n


# ---------------------------------------------------------------- card

_PREFERRED_SECTIONS = (
    "printings", "legality", "status", "evidence", "xledger", "cert",
    "labels", "curated_labels", "curated_game_changers", "curated_bracket_flags",
    "curated_exclusions", "curated_relations", "decks", "text", "tags",
    "edhrec", "prims", "values", "labels_private",
)


def _section_order(conn):
    present = [t for t in _PREFERRED_SECTIONS if _has_table(conn, t)]
    known = set(present) | {"cards", "card_names", "card_fts", "proven", "build_meta"}
    rest = [t for t in _tables(conn)
            if t not in known and not t.startswith("src_") and not t.startswith("card_fts_")
            and not t.startswith("_meta_") and "oracle_id" in _columns(conn, t)]
    return present + rest


def resolve_card(conn, ref):
    """oracle_id for an exact id, else the smallest id a norm_name maps to."""
    row = conn.execute("SELECT oracle_id FROM cards WHERE oracle_id=?", (ref,)).fetchone()
    if row:
        return row[0]
    key = norm_name(ref)
    if _has_table(conn, "card_names"):
        row = conn.execute("SELECT oracle_id FROM card_names WHERE norm_name=? ORDER BY oracle_id LIMIT 1",
                           (key,)).fetchone()
        if row:
            return row[0]
    row = conn.execute("SELECT oracle_id FROM cards WHERE lower(name)=lower(?) ORDER BY oracle_id LIMIT 1",
                       (ref,)).fetchone()
    if row:
        return row[0]
    raise QueryError("card: no card named or id %r" % ref, 1)


def run_card(conn, args, out):
    oid = resolve_card(conn, args.ref)
    row = conn.execute("SELECT * FROM cards WHERE oracle_id=?", (oid,)).fetchone()
    if row is None:
        raise QueryError("card: no card %r" % args.ref, 1)
    cols = _columns(conn, "cards")
    for name in cols:
        print("%s\t%s" % (name, _cell(row[name])), file=out)
    for table in _section_order(conn):
        tcols = _columns(conn, table)
        if "oracle_id" not in tcols:
            continue
        names = [c for c in tcols if c != "oracle_id"]
        order = ", ".join(q(c) for c in names) or "1"
        rows = _rows(conn, "SELECT %s FROM %s WHERE oracle_id=? ORDER BY %s" %
                     (", ".join(q(c) for c in names), q(table), order), (oid,))
        if not rows:
            continue
        print("-- %s" % table, file=out)
        _emit(out, names, [tuple(r[c] for c in names) for r in rows])


# -------------------------------------------------------------- search

def _status_column(conn, key):
    if _has_table(conn, "status") and key in _columns(conn, "status"):
        return key
    raise QueryError("search: status has no column %r in this build" % key)


def _status_value_column(conn):
    """The column that holds a status verdict: v1 calls it verdict, later writers may call it
    status or outcome. Every caller goes through this one name so a schema addition cannot be
    missed at one site."""
    if not _has_table(conn, "status"):
        return None
    cols = _columns(conn, "status")
    for name in ("verdict", "status", "outcome"):
        if name in cols:
            return name
    return None


def run_search(conn, args, out):
    if not _has_table(conn, "card_fts"):
        raise QueryError("search: this build has no card_fts table")
    where, params = ["card_fts MATCH ?"], [args.query]
    if args.legal:
        where.append("EXISTS (SELECT 1 FROM legality l WHERE l.oracle_id=c.oracle_id "
                     "AND l.format=? AND l.status='legal')")
        params.append(args.legal)
    for spec in args.status or ():
        if "=" not in spec:
            raise QueryError("search: --status wants key=value, got %r" % spec)
        key, value = spec.split("=", 1)
        where.append("EXISTS (SELECT 1 FROM status s WHERE s.oracle_id=c.oracle_id AND s.%s=?)" %
                     q(_status_column(conn, key)))
        params.append(value)
    if args.set:
        where.append("EXISTS (SELECT 1 FROM printings p WHERE p.oracle_id=c.oracle_id AND p.set_code=?)")
        params.append(args.set.lower())
    if args.label:
        label = args.label.split(":", 1)[-1]
        where.append("(EXISTS (SELECT 1 FROM labels x WHERE x.oracle_id=c.oracle_id AND x.label=?) "
                     "OR EXISTS (SELECT 1 FROM curated_labels x WHERE x.oracle_id=c.oracle_id AND x.label=?))")
        params += [label, label]
    if args.marked:
        scols = _columns(conn, "status") if _has_table(conn, "status") else []
        if "marked" in scols:
            where.append("EXISTS (SELECT 1 FROM status s WHERE s.oracle_id=c.oracle_id "
                         "AND s.marked IS NOT NULL AND s.marked NOT IN ('', '-'))")
        else:
            value = _status_value_column(conn)
            if value is None:
                raise QueryError("search: status has no marked or verdict column")
            where.append("EXISTS (SELECT 1 FROM status s WHERE s.oracle_id=c.oracle_id "
                         "AND lower(s.%s) IN ('refused','known-unsupported','cordon','g_missing'))" % q(value))
    sql = ("SELECT c.oracle_id, c.name, c.type_line, c.mana_value FROM cards c "
           "JOIN card_fts ON card_fts.oracle_id=c.oracle_id WHERE " + " AND ".join(where) +
           " ORDER BY c.name, c.oracle_id")
    limit = _limit(args.limit, "search")
    if limit is not None:
        sql += " LIMIT ?"
        params.append(limit)
    try:
        rows = _rows(conn, sql, params)
    except sqlite3.Error as e:
        raise QueryError("search: bad query %r: %s" % (args.query, e))
    _emit(out, ("oracle_id", "name", "type_line", "mana_value"),
          [(r["oracle_id"], r["name"], r["type_line"], r["mana_value"]) for r in rows])


# ------------------------------------------------------------ blockers

def _eligible_sql():
    marks = ", ".join("?" * len(NON_TOURNAMENT_SET_TYPES))
    return ("c.oracle_id NOT IN (SELECT p.oracle_id FROM printings p "
            "LEFT JOIN sets s ON s.set_code=p.set_code GROUP BY p.oracle_id "
            "HAVING SUM(CASE WHEN lower(COALESCE(s.set_type,'')) IN (%s) THEN 1 ELSE 0 END)=COUNT(*))" % marks)


def _level_at_least(level):
    if level not in LEVEL_ORDER:
        raise QueryError("proven: level must be one of %s, got %r" % (", ".join(LEVEL_ORDER), level))
    return LEVEL_ORDER[LEVEL_ORDER.index(level):]


def _proven_sql(level="C2"):
    levels = _level_at_least(level)
    return ("SELECT c.oracle_id, c.name, c.type_line, c.mana_value, cert.level, cert.measured "
            "FROM cards c JOIN cert ON cert.oracle_id=c.oracle_id WHERE cert.level IN (%s)" %
            ", ".join("?" * len(levels))), list(levels)


def run_blockers(conn, args, out):
    where, params = [], []
    if args.format not in ("all", "*", "paper"):
        where.append("EXISTS (SELECT 1 FROM legality l WHERE l.oracle_id=c.oracle_id "
                     "AND l.format=? AND l.status='legal')")
        params.append(args.format)
    where.append(_eligible_sql())
    params.extend(NON_TOURNAMENT_SET_TYPES)
    engine = args.engine
    if engine:
        if engine == "proven":
            where.append("EXISTS (SELECT 1 FROM cert WHERE cert.oracle_id=c.oracle_id "
                         "AND cert.level IN ('C2','C3'))")
        elif engine.startswith("cert:"):
            level = engine.split(":", 1)[1]
            where.append("EXISTS (SELECT 1 FROM cert WHERE cert.oracle_id=c.oracle_id AND cert.level=?)")
            params.append(level)
        elif engine in ("gorge", "dirac"):
            scols = _columns(conn, "status") if _has_table(conn, "status") else []
            value = _status_value_column(conn)
            if "engine" in scols:
                if value is None:
                    where.append("EXISTS (SELECT 1 FROM status s WHERE s.oracle_id=c.oracle_id AND s.engine=?)")
                    params.append(engine)
                else:
                    where.append("(NOT EXISTS (SELECT 1 FROM status s WHERE s.oracle_id=c.oracle_id "
                                 "AND s.engine=?) OR EXISTS (SELECT 1 FROM status s WHERE "
                                 "s.oracle_id=c.oracle_id AND s.engine=? AND lower(s.%s) NOT IN "
                                 "('agree','supported','pass','ok')))" % q(value))
                    params += [engine, engine]
            elif value is None:
                # no status rows to judge by: every card is an unmeasured blocker
                where.append("NOT EXISTS (SELECT 1 FROM status s WHERE s.oracle_id=c.oracle_id)")
            else:
                # v1 status has no engine column: a card with no agreeing status row is a blocker.
                where.append("(NOT EXISTS (SELECT 1 FROM status s WHERE s.oracle_id=c.oracle_id) "
                             "OR EXISTS (SELECT 1 FROM status s WHERE s.oracle_id=c.oracle_id "
                             "AND lower(s.%s) NOT IN ('agree','supported','pass','ok')))" % q(value))
        else:
            raise QueryError("blockers: --engine wants dirac, gorge, proven or cert:<level>, got %r" % engine)
    by = args.by or ("decks" if _has_table(conn, "decks") else "printings")
    if by == "decks":
        if not _has_table(conn, "decks"):
            raise QueryError("blockers: --by decks needs the private build (no decks table)", 1)
        rank = ("(SELECT COUNT(DISTINCT d.deck_id) FROM decks d WHERE d.oracle_id=c.oracle_id)")
    else:
        rank = "(SELECT COUNT(*) FROM printings p WHERE p.oracle_id=c.oracle_id)"
    sql = ("SELECT c.oracle_id, c.name, c.type_line, c.mana_value, %s AS rank FROM cards c WHERE %s "
           "ORDER BY rank DESC, c.name, c.oracle_id" % (rank, " AND ".join(where)))
    limit = _limit(args.limit, "blockers")
    if limit is not None:
        sql += " LIMIT ?"
        params.append(limit)
    rows = _rows(conn, sql, params)
    _emit(out, ("oracle_id", "name", "type_line", "mana_value", by),
          [(r["oracle_id"], r["name"], r["type_line"], r["mana_value"], r["rank"]) for r in rows])


# -------------------------------------------------------------- proven

def run_proven(conn, args, out):
    if not _has_table(conn, "cert"):
        raise QueryError("proven: this build has no cert table")
    sql, params = _proven_sql(args.level)
    if args.format:
        sql += (" AND EXISTS (SELECT 1 FROM legality l WHERE l.oracle_id=c.oracle_id "
                "AND l.format=? AND l.status='legal')")
        params.append(args.format)
    sql += " ORDER BY c.name, c.oracle_id"
    rows = _rows(conn, sql, params)
    _emit(out, ("oracle_id", "name", "type_line", "mana_value", "level", "measured"),
          [(r["oracle_id"], r["name"], r["type_line"], r["mana_value"], r["level"], r["measured"])
           for r in rows])


# ---------------------------------------------------------------- deck

def run_deck(conn, args, out):
    if not _has_table(conn, "decks"):
        raise QueryError("deck: private only; this build has no decks table", 1)
    rows = _rows(conn, "SELECT d.oracle_id, c.name, d.count FROM decks d "
                       "LEFT JOIN cards c ON c.oracle_id=d.oracle_id WHERE d.deck_id=? ORDER BY d.oracle_id",
                 (args.deck_id,))
    if not rows:
        raise QueryError("deck: no deck %r in this build" % args.deck_id, 1)
    _emit(out, ("oracle_id", "name", "count"),
          [(r["oracle_id"], r["name"], r["count"]) for r in rows])


# ----------------------------------------------------------- freshness

def _now(args):
    raw = args.now or os.environ.get("CARDDB_NOW")
    if not raw:
        return datetime.now(timezone.utc).replace(microsecond=0)
    raw = raw.strip()
    if re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}", raw):
        raw += "T00:00:00+00:00"
    try:
        dt = datetime.fromisoformat(raw.replace("Z", "+00:00"))
    except ValueError:
        raise QueryError("freshness: --now/CARDDB_NOW is not an ISO date or timestamp: %r" % raw)
    return dt.replace(tzinfo=timezone.utc) if dt.tzinfo is None else dt.astimezone(timezone.utc)


def _midnight(stamp):
    try:
        d = datetime.fromisoformat(stamp)
    except ValueError:
        raise QueryError("freshness: writer date %r is not YYYY-MM-DD" % stamp)
    return datetime(d.year, d.month, d.day, tzinfo=timezone.utc)


def _hours(now, then):
    return max(0.0, (now - then).total_seconds() / 3600.0)


def load_registry(path):
    """{writer: (sla_hours, tables, doc)} from carddb/schema/writers.tsv.

    Read as the tree validators read it and parsed by the same strict parser,
    so a file CI accepts cannot make freshness fail (or the other way round)."""
    try:
        text = read_text(path)
    except OSError as e:
        raise QueryError("freshness: cannot read writer SLA file %s: %s" % (path, e))
    registry = {}
    _writers, violations = parse_writers(path, text, registry)
    if violations:
        raise QueryError(violations[0])
    return registry


def load_writers(path):
    """{writer: sla_hours} from carddb/schema/writers.tsv."""
    return {w: v[0] for w, v in load_registry(path).items()}


_ALIAS_DOC = re.compile(r"^alias of ([^ ,;:]+)")


def _alias_groups(registry):
    """{writer: group head}. A writer whose doc starts `alias of W` (W registered)
    is another id for the same job as W: a build that records either one has run
    the job, so the group needs one _meta/writers row, not one per id."""
    head = {}
    for w in registry:
        m = _ALIAS_DOC.match(registry[w][2])
        head[w] = m.group(1) if m and m.group(1) in registry and m.group(1) != w else w
    return head


def _expected(tables, private, from_build):
    """Whether this scope's _meta/writers should hold a row for a writer that
    writes `tables`. A public build has no private table, so a writer of only
    private tables never records a row there. A private clone holds only the
    private tables; a private build also carries the public data."""
    if not tables:
        return True
    if not private:
        return any(t not in PRIVATE_TABLE_NAMES for t in tables)
    return from_build or any(t in PRIVATE_TABLE_NAMES for t in tables)


def _git(public_dir, *args):
    if not os.path.isdir(os.path.join(public_dir, ".git")):
        return None
    p = subprocess.run(["git", "-C", public_dir] + list(args), capture_output=True, text=True)
    return p.stdout if p.returncode == 0 else None


def _publish_time(public_dir):
    out = _git(public_dir, "log", "-1", "--format=%ct")
    if not out or not out.strip():
        return None
    try:
        return datetime.fromtimestamp(int(out.strip()), timezone.utc)
    except ValueError:
        return None


def _asset_meta(path):
    try:
        with open(path, encoding="utf-8") as f:
            meta = json.load(f)
    except (OSError, ValueError):
        return None
    return meta if isinstance(meta, dict) else None


def _asset_time(meta):
    if not isinstance(meta, dict):
        return None
    raw = meta.get("published_at") or meta.get("fetched_at")
    if not raw:
        return None
    try:
        dt = datetime.fromisoformat(str(raw).replace("Z", "+00:00"))
    except ValueError:
        return None
    return dt.replace(tzinfo=timezone.utc) if dt.tzinfo is None else dt.astimezone(timezone.utc)


def _writer_rows_from(conn, store):
    if conn is not None:
        return [dict(r) for r in conn.execute('SELECT writer, "table", version, ran FROM _meta_writers')]
    return [dict(r) for r in store.rows("_meta/writers")]


def _unresolved_count(conn, store):
    if conn is not None:
        return conn.execute("SELECT COUNT(*) FROM _meta_unresolved").fetchone()[0]
    return sum(1 for _ in store.rows("_meta/unresolved"))


def run_freshness(args, schema, out):
    private = args.private
    db = args.db or default_db(args.home, private)
    conn = store = None
    if os.path.isfile(db):
        conn, build_meta = connect(db)
        # The file says which scope it was built for; trust that over the flag.
        private = build_meta.get("scope", PRIVATE if private else PUBLIC) == PRIVATE
    else:
        store = Store(PRIVATE if private else PUBLIC, home=args.home, schema=schema)
        if not store.exists():
            raise QueryError("no built database at %s and no %s clone" % (db, "private" if private else "public"))
    try:
        now = _now(args)
        try:
            rows = _writer_rows_from(conn, store)
            unresolved = _unresolved_count(conn, store)
        except sqlite3.Error as e:
            raise QueryError("freshness: %s" % e)
        by_writer = {}
        for r in rows:
            by_writer[r["writer"]] = max(by_writer.get(r["writer"], r["ran"]), r["ran"])
        sla_path = args.sla or os.path.join(schema.root, "writers.tsv")
        sla_error = None
        try:
            registry = load_registry(sla_path)
            sla = {w: v[0] for w, v in registry.items()}
        except QueryError as e:
            registry, sla, sla_error = {}, {}, str(e)

        lines = []

        def line(fail, text):
            # A writer id may carry undecodable bytes (the registry is read as the
            # validators read it): print them escaped, never crash on output.
            lines.append((fail, text.encode("utf-8", "backslashreplace").decode("utf-8")))

        registered = sorted(set(sla) | set(by_writer))
        if sla_error:
            line(True, sla_error)
        group = _alias_groups(registry)
        recorded = {group.get(w, w) for w in by_writer}
        for writer in registered:
            if writer not in sla:
                line(True, "writer %s is not registered in schema/writers.tsv" % writer)
                continue
            if writer not in by_writer:
                if group[writer] in recorded:
                    continue  # another id of the same job has a row
                if group[writer] != writer:
                    continue  # an alias is reported once, under its job's name
                if not _expected(registry[writer][1], private, conn is not None):
                    continue  # this scope never holds this writer's tables
                line(True, "writer %s has no _meta/writers row" % writer)
                continue
            age = _hours(now, _midnight(by_writer[writer]))
            fail = age > sla[writer]
            line(fail, "writer %s ran %s (%.1fh, SLA %dh)" %
                 (writer, by_writer[writer], age, sla[writer]))

        # Each scope has its own release metadata file (store.asset_meta_path),
        # and the file records its scope: a private fetch can never be read as
        # the public release's age (or the other way round).
        meta_name = "carddb-private-latest" if private else "carddb-latest"
        if args.asset_meta:
            meta = _asset_meta(args.asset_meta)
        else:
            meta = _asset_meta(asset_meta_path(args.home, private))
        if meta is not None:
            want_scope = PRIVATE if private else PUBLIC
            got_scope = meta.get("scope")
            if got_scope is not None and got_scope != want_scope:
                line(True, "%s metadata is for scope %r, want %r" % (meta_name, got_scope, want_scope))
                meta = None
        baseline = None
        if args.unresolved_baseline is not None:
            baseline = args.unresolved_baseline
        elif meta is not None and isinstance(meta.get("unresolved"), int):
            baseline = meta["unresolved"]
        if baseline is None:
            line(False, "unresolved %d (no baseline)" % unresolved)
        else:
            fail = unresolved > baseline
            line(fail, "unresolved %d (baseline %d)%s" %
                 (unresolved, baseline, " -- grew" if fail else ""))

        public_dir = args.public_dir or os.path.join(carddb_home(args.home),
                                                     "private" if private else "public")
        published = _publish_time(public_dir)
        if published is None:
            line(True, "no %s clone with a publish commit" % ("private" if private else "public"))
        else:
            age = _hours(now, published)
            fail = age > 2
            line(fail, "publish %.1fh old (SLA 2h)" % age)

        if meta is None:
            line(True, "no %s metadata" % meta_name)
        else:
            asset = _asset_time(meta)
            if asset is None:
                line(True, "%s metadata has no published_at" % meta_name)
            else:
                age = _hours(now, asset)
                fail = age > 6
                line(fail, "%s %.1fh old (SLA 6h)" % (meta_name, age))

        packed = packed_mib(public_dir)
        if packed is None:
            line(True, "packed size unavailable")
        else:
            fail = packed > args.max_pack_mb
            line(fail, "data branch packed %.1f MiB (budget %.0f MiB)" % (packed, args.max_pack_mb))

        failed = 0
        for fail, text in lines:
            if fail:
                failed += 1
            if not args.tick or fail:
                print(("!! " if fail else "") + text, file=out)
        return 1 if failed else 0
    finally:
        if conn is not None:
            conn.close()


# ---------------------------------------------------------------- sql

def run_sql(conn, args, out):
    sql = args.query.strip()
    if not re.match(r"(?is)^(select|with|explain)\b", sql):
        raise QueryError("sql: only SELECT (or WITH/EXPLAIN) is allowed", 2)
    try:
        cur = conn.execute(sql)
    except sqlite3.Error as e:
        raise QueryError("sql: %s" % e, 2)
    if cur.description is None:
        raise QueryError("sql: statement returns no rows", 2)
    names = [d[0] for d in cur.description]
    _emit(out, names, cur.fetchall())


# ------------------------------------------------------------- export

def _export_table(conn, view, schema):
    present = set(_tables(conn))
    if view in present:
        return view
    if view in schema.tables:
        name = sql_name(view)
        if name in present:
            return name
    raise QueryError("export: unknown view %r (try a schema table, proven, or a table in the file)" % view)


def _export_rows(conn, table):
    info = _rows(conn, "PRAGMA table_info(%s)" % q(table))
    cols = [r[1] for r in info]
    keys = [r[1] for r in sorted((r for r in info if r[5]), key=lambda r: r[5])]
    order = keys or cols
    sql = "SELECT %s FROM %s ORDER BY %s" % (
        ", ".join(q(c) for c in cols), q(table), ", ".join(q(c) for c in order))
    return cols, _rows(conn, sql)


def run_export(conn, args, schema, meta, out):
    if not args.tsv:
        raise QueryError("export: --tsv is required", 2)
    head = "#carddb export %s v%s %s gorge_pin=%s" % (
        args.view, meta.get("schema_version", "?"), meta.get("scope", "?"),
        meta.get("gorge_pin", "unknown"))
    if args.view == "proven":
        if not _has_table(conn, "cert"):
            raise QueryError("export: this build has no cert table")
        sql, params = _proven_sql()
        rows = _rows(conn, sql + " ORDER BY c.name, c.oracle_id", params)
        print(head, file=out)
        _emit(out, ("oracle_id", "name", "type_line", "mana_value", "level", "measured"),
              [(r["oracle_id"], r["name"], r["type_line"], r["mana_value"], r["level"], r["measured"])
               for r in rows])
        return 0
    table = _export_table(conn, args.view, schema)
    cols, rows = _export_rows(conn, table)
    print(head, file=out)
    _emit(out, cols, [tuple(r[c] for c in cols) for r in rows])
    return 0


# -------------------------------------------------------------- dispatch

def run(args, conn, schema, meta, out):
    try:
        return _run(args, conn, schema, meta, out)
    except sqlite3.Error as e:
        raise QueryError("%s: %s" % (args.verb, e))


def _run(args, conn, schema, meta, out):
    if args.verb == "card":
        return run_card(conn, args, out) or 0
    if args.verb == "search":
        return run_search(conn, args, out) or 0
    if args.verb == "blockers":
        return run_blockers(conn, args, out) or 0
    if args.verb == "proven":
        return run_proven(conn, args, out) or 0
    if args.verb == "deck":
        return run_deck(conn, args, out) or 0
    if args.verb == "sql":
        return run_sql(conn, args, out) or 0
    if args.verb == "export":
        return run_export(conn, args, schema, meta, out)
    raise QueryError("%s: not a read verb" % args.verb)


# -------------------------------------------------- certification view

PROVEN_LEVELS = ("C2", "C3")

# The columns `carddb proven` prints by default: what a caller needs to act on
# the row, without the long witness text.
PROVEN_COLUMNS = ("oracle_id", "norm_name", "name", "level", "first_set", "released", "refs", "measured")

PROVEN_VIEW_SQL = ("CREATE VIEW proven AS SELECT * FROM cert WHERE level IN ('C2', 'C3') "
                   "AND (exclusion IS NULL OR exclusion = '-')")


def is_proven(row):
    """A cert row counts as proven when it reached C2 or C3 and is not excluded."""
    return row.get("level") in PROVEN_LEVELS and row.get("exclusion") in (None, "", "-")


def proven_rows(rows):
    """The proven subset of cert rows, sorted by oracle_id then norm_name."""
    return sorted((r for r in rows if is_proven(r)), key=lambda r: (r.get("oracle_id") or "",
                                                                     r.get("norm_name") or ""))


def proven_from_store(store):
    if not store.exists():
        raise StoreError("no public clone at %s: run `carddb init`" % store.dir)
    return proven_rows(store.rows("cert"))


def proven_from_db(path):
    """The built file's `proven` view as row dicts, in a stable order."""
    if not os.path.isfile(path):
        raise StoreError("no built carddb file at %s: run `carddb build`" % path)
    conn = sqlite3.connect(path)
    try:
        conn.execute("PRAGMA query_only=ON")
        cur = conn.execute("SELECT * FROM proven ORDER BY oracle_id, norm_name")
        names = [d[0] for d in cur.description]
        return [dict(zip(names, r)) for r in cur]
    except sqlite3.DatabaseError as e:
        raise StoreError("%s: %s" % (path, e))
    finally:
        conn.close()


def _proven_cell(v):
    return "-" if v is None or v == "" else str(v)


def format_proven(rows, kind="tsv"):
    """Render proven rows as tsv (default), json or count. Deterministic."""
    if kind == "count":
        return "%d\n" % len(rows)
    if kind == "json":
        return json.dumps([{c: (None if r.get(c) in (None, "", "-") else r.get(c)) for c in PROVEN_COLUMNS}
                           for r in rows], sort_keys=True) + "\n"
    lines = ["\t".join(PROVEN_COLUMNS)]
    lines += ["\t".join(_proven_cell(r.get(c)) for c in PROVEN_COLUMNS) for r in rows]
    return "\n".join(lines) + "\n"
