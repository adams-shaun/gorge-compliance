"""Read verbs over a built carddb SQLite file (DESIGN section 6).

The file is a cache made by `carddb build` or `carddb fetch`; every verb here
opens it read-only. A verb never writes and never reports a wall-clock value
unless the caller asks (freshness, whose whole job is age), so an answer is a
function of the file it read.

  card <name|oracle_id>       every fact for one card
  search <fts>                FTS5 search with legality/status/set/label filters
  blockers --format F         legal cards that are not covered, ranked
  proven [--format F]         the certification-pinned proven view
  deck <id> [--history]       a private deck's cards
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
from .schema import PRIVATE, PUBLIC
from .store import Store, StoreError, carddb_home

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
    known = set(present) | {"cards", "card_names", "card_fts", "build_meta"}
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
    return ("SELECT c.oracle_id, c.name, c.type_line, c.mana_value, cert.level, cert.declared "
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
    _emit(out, ("oracle_id", "name", "type_line", "mana_value", "level", "declared"),
          [(r["oracle_id"], r["name"], r["type_line"], r["mana_value"], r["level"], r["declared"])
           for r in rows])


# ---------------------------------------------------------------- deck

def run_deck(conn, args, out):
    if not _has_table(conn, "decks"):
        raise QueryError("deck: private only; this build has no decks table", 1)
    if args.history:
        raise QueryError("deck: this build has no deck history table", 1)
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


def load_writers(path):
    """{writer: sla_hours} from carddb/schema/writers.tsv."""
    try:
        with open(path, encoding="utf-8") as f:
            text = f.read()
    except OSError as e:
        raise QueryError("freshness: cannot read writer SLA file %s: %s" % (path, e))
    lines = text.rstrip("\n").split("\n")
    if len(lines) < 2 or not lines[0].startswith("#carddb-writers "):
        raise QueryError("%s: first line must be `#carddb-writers v1`" % path)
    header = lines[1].split("\t")
    if "writer" not in header or "sla_hours" not in header:
        raise QueryError("%s: columns need writer and sla_hours" % path)
    out = {}
    for ln, line in enumerate(lines[2:], 3):
        cells = line.split("\t")
        if len(cells) != len(header):
            raise QueryError("%s:%d: %d cells, want %d" % (path, ln, len(cells), len(header)))
        row = dict(zip(header, cells))
        writer = row["writer"]
        if not writer:
            raise QueryError("%s:%d: empty writer" % (path, ln))
        if writer in out:
            raise QueryError("%s:%d: duplicate writer %r" % (path, ln, writer))
        try:
            hours = int(row["sla_hours"])
        except ValueError:
            raise QueryError("%s:%d: sla_hours %r is not an int" % (path, ln, row["sla_hours"]))
        if hours < 1:
            raise QueryError("%s:%d: sla_hours must be at least 1" % (path, ln))
        out[writer] = hours
    return out


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


def _packed_mib(public_dir):
    out = _git(public_dir, "count-objects", "-v")
    if out is None:
        return None
    for line in out.splitlines():
        if line.startswith("size-pack:"):
            try:
                return int(line.split(":", 1)[1].strip()) / 1024.0
            except ValueError:
                return None
    return None


def _asset_meta(path):
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def _asset_time(meta):
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
        conn, _meta = connect(db)
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
            sla = load_writers(sla_path)
        except QueryError as e:
            sla, sla_error = {}, str(e)

        lines = []

        def line(fail, text):
            lines.append((fail, text))

        registered = sorted(set(sla) | set(by_writer))
        if sla_error:
            line(True, sla_error)
        for writer in registered:
            if writer not in sla:
                line(True, "writer %s is not registered in schema/writers.tsv" % writer)
                continue
            if writer not in by_writer:
                line(True, "writer %s has no _meta/writers row" % writer)
                continue
            age = _hours(now, _midnight(by_writer[writer]))
            fail = age > sla[writer]
            line(fail, "writer %s ran %s (%.1fh, SLA %dh)" %
                 (writer, by_writer[writer], age, sla[writer]))

        meta = _asset_meta(args.asset_meta) if args.asset_meta else _asset_meta(
            os.path.join(carddb_home(args.home), "carddb-latest.json"))
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
            line(True, "no carddb-latest metadata")
        else:
            asset = _asset_time(meta)
            if asset is None:
                line(True, "carddb-latest metadata has no published_at")
            else:
                age = _hours(now, asset)
                fail = age > 6
                line(fail, "carddb-latest %.1fh old (SLA 6h)" % age)

        packed = _packed_mib(public_dir)
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
        _emit(out, ("oracle_id", "name", "type_line", "mana_value", "level", "declared"),
              [(r["oracle_id"], r["name"], r["type_line"], r["mana_value"], r["level"], r["declared"])
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
