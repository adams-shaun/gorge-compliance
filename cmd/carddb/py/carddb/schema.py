"""Schema loader: a port of internal/carddb/schema.go (keep the two in step).

carddb/schema/VERSION holds N; carddb/schema/vN/<table>.tsv declares one table
each (`#carddb-schema <table> vN`, the column header, one row per column).
"""
import os
import re

PUBLIC = "public"
PRIVATE = "private"
EMPTY = "-"

COLUMN_TYPES = ("string", "int", "bool", "date", "list")
SCHEMA_HEADER = ["name", "type", "required", "key", "visibility", "doc"]

_IDENT = re.compile(r"^[a-z][a-z0-9_]*$")
_FORBIDDEN_FIELD = re.compile(r"^(oracle_text|price.*|prims|tagger|edhrec.*|value.*)$", re.I)

# Tables that are never published, named here as well as in the schema so the
# boundary holds against a schema edited to call one public (boundary.go).
PRIVATE_TABLE_NAMES = frozenset(
    ["text", "tags", "edhrec", "prims", "values", "labels_private", "decks",
     "cert_events", "cert_runs", "rulings"])


class SchemaError(Exception):
    pass


def forbidden_field(name):
    return _FORBIDDEN_FIELD.match(name) is not None


class Column:
    __slots__ = ("name", "type", "required", "key", "visibility", "doc")

    def __init__(self, name, type_, required, key, visibility, doc):
        self.name, self.type, self.required = name, type_, required
        self.key, self.visibility, self.doc = key, visibility, doc


class Table:
    def __init__(self, name, columns):
        self.name = name
        self.columns = columns

    def is_private(self):
        return all(c.visibility != PUBLIC for c in self.columns)

    def scope_columns(self, scope):
        if scope == PRIVATE:
            return list(self.columns)
        return [c for c in self.columns if c.visibility == PUBLIC]

    def key_names(self, scope=PRIVATE):
        return [c.name for c in self.scope_columns(scope) if c.key]


SCHEMA_DIR = "schema"
_SCHEMA_DECL_PATH = re.compile(r"^schema/v([0-9]+)/(.+)\.tsv$")


def is_schema_path(rel):
    return rel == SCHEMA_DIR or rel.startswith(SCHEMA_DIR + "/")


def decl_text(table, version):
    """A table's schema declaration file, without the final newline (the form _parse_schema_file reads)."""
    yn = lambda b: "yes" if b else "no"
    lines = ["#carddb-schema %s v%d" % (table.name, version), "\t".join(SCHEMA_HEADER)]
    for c in table.columns:
        lines.append("\t".join([c.name, c.type, yn(c.required), yn(c.key), c.visibility, c.doc]))
    return "\n".join(lines)


def is_schema_decl(schema, rel, text):
    """internal/carddb IsSchemaDecl: is the file at rel, whose whole content is text, a declaration of schema?
    schema/VERSION holding exactly the version, or schema/v<N>/<table>.tsv equal to the schema's own declaration."""
    if rel == SCHEMA_DIR + "/VERSION":
        return re.fullmatch(r"[0-9]+\n?", text) is not None and int(text.rstrip("\n")) == schema.version
    m = _SCHEMA_DECL_PATH.match(rel)
    if m is None or m.group(1) != str(schema.version) or m.group(2) not in schema.tables:
        return False
    body = text[:-1] if text.endswith("\n") else text
    return body == decl_text(schema.tables[m.group(2)], schema.version)


def is_schema_aux(rel):
    """internal/carddb IsSchemaAux: schema/writers.tsv, the writer SLA registry. The one file under
    schema/ that is neither VERSION nor a table declaration; freshness reads it, the validators allow it."""
    return rel == SCHEMA_DIR + "/writers.tsv"


class Schema:
    def __init__(self, version, tables, root):
        self.version = version
        self.tables = tables  # name -> Table
        self.root = root

    def table_names(self):
        return sorted(self.tables)


def default_schema_dir():
    """carddb/schema of the compliance checkout this tool lives in."""
    env = os.environ.get("CARDDB_SCHEMA")
    if env:
        return env
    return os.path.join(repo_root(), "carddb", "schema")


def repo_root():
    """The compliance checkout this tool lives in (cmd/carddb/py/carddb -> root)."""
    here = os.path.dirname(os.path.abspath(__file__))
    return os.path.normpath(os.path.join(here, "..", "..", "..", ".."))


def load_schema(directory=None):
    directory = directory or default_schema_dir()
    try:
        with open(os.path.join(directory, "VERSION"), encoding="utf-8") as f:
            raw = f.read().strip()
    except OSError as e:
        raise SchemaError("cannot read the schema: %s" % e)
    if not re.fullmatch(r"[0-9]+", raw) or int(raw) < 1:
        raise SchemaError("%s/VERSION: %r is not a schema version" % (directory, raw))
    version = int(raw)
    root = os.path.join(directory, "v%d" % version)
    files = []
    for dp, _dn, fns in os.walk(root):
        for fn in fns:
            if fn.endswith(".tsv"):
                files.append(os.path.join(dp, fn))
    files.sort()
    tables = {}
    for p in files:
        rel = os.path.relpath(p, root).replace(os.sep, "/")
        name = rel[: -len(".tsv")]
        tables[name] = _parse_schema_file(p, name, version)
    if not tables:
        raise SchemaError("%s: no tables" % root)
    return Schema(version, tables, directory)


def _parse_schema_file(path, name, version):
    def fail(line, msg):
        raise SchemaError("%s:%d: %s" % (path, line, msg))

    with open(path, encoding="utf-8") as f:
        text = f.read()
    if text.endswith("\n"):
        text = text[:-1]
    lines = text.split("\n")
    want = "#carddb-schema %s v%d" % (name, version)
    if lines[0] != want:
        fail(1, "first line is %r, want %r" % (lines[0], want))
    if len(lines) < 3 or lines[1] != "\t".join(SCHEMA_HEADER):
        fail(2, "column header must be %r" % "\t".join(SCHEMA_HEADER))
    cols, seen, any_key = [], set(), False
    for i, l in enumerate(lines[2:]):
        ln = i + 3
        f_ = l.split("\t")
        if len(f_) != len(SCHEMA_HEADER):
            fail(ln, "%d fields, want %d" % (len(f_), len(SCHEMA_HEADER)))
        cname, ctype, req, key, vis, doc = f_
        if not _IDENT.match(cname):
            fail(ln, "bad column name %r" % cname)
        if cname in seen:
            fail(ln, "duplicate column %r" % cname)
        seen.add(cname)
        if ctype not in COLUMN_TYPES:
            fail(ln, "unknown type %r" % ctype)
        if req not in ("yes", "no") or key not in ("yes", "no"):
            fail(ln, "required and key must be yes or no")
        required, is_key = req == "yes", key == "yes"
        if is_key and not required:
            fail(ln, "key column %r must be required" % cname)
        if vis not in (PUBLIC, PRIVATE):
            fail(ln, "visibility must be public or private, got %r" % vis)
        if vis == PUBLIC and forbidden_field(cname):
            fail(ln, "public column %r has a name the public boundary forbids" % cname)
        if doc == "":
            fail(ln, "column %r has no doc" % cname)
        any_key = any_key or is_key
        cols.append(Column(cname, ctype, required, is_key, vis, doc))
    if not any_key:
        fail(1, "table %r declares no key column" % name)
    return Table(name, cols)
