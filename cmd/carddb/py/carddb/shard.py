"""Shards: parse, render, validate (a port of internal/carddb/validate.go).

A shard is `#carddb <table> v<N>`, a column-header line, then rows sorted
bytewise by the key columns, keys unique. A cell is never empty (`-` is the
empty value) and never holds a tab, newline or CR.
"""
import datetime
import os
import re

from .schema import (EMPTY, PRIVATE, PRIVATE_TABLE_NAMES, PUBLIC, forbidden_field,
                     is_schema_aux, is_schema_decl, is_schema_path)

_INT = re.compile(r"^-?(0|[1-9][0-9]*)$")
_DATE = re.compile(r"^[0-9]{4}-[0-9]{2}-[0-9]{2}$")


class ValidationError(Exception):
    """Raised when rows or shards break the schema or the public boundary."""

    def __init__(self, violations):
        self.violations = list(violations)
        super().__init__("\n".join(self.violations[:20]) +
                         ("\n... %d more" % (len(self.violations) - 20) if len(self.violations) > 20 else ""))


def shard_header(table, version):
    return "#carddb %s v%d" % (table, version)


def parse_shard_header(line):
    """-> (table, version) or None for a line that is not `#carddb <t> v<N>`."""
    f = line.split(" ")
    if len(f) != 3 or f[0] != "#carddb" or f[1] == "" or not f[2].startswith("v"):
        return None
    digits = f[2][1:]
    if not re.fullmatch(r"[1-9][0-9]*", digits):
        return None
    return f[1], int(digits)


def path_holds_table(rel, table):
    return rel == table + ".tsv" or rel.startswith(table + "/")


def escape_cell(v):
    """A python value as one shard cell (the API form; the CLI passes cells through)."""
    if v is None:
        return EMPTY
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, (list, tuple)):
        v = ",".join(escape_cell(x) for x in v)
        return v if v else EMPTY
    v = str(v)
    if v == "":
        return EMPTY
    return (v.replace("\\", "\\\\").replace("\t", "\\t")
             .replace("\n", "\\n").replace("\r", "\\r"))


def parse_text(text):
    """Split shard text into (header_line, columns, rows); rows are lists of cells.
    Raises ValueError on a shard without header and column lines."""
    if text.endswith("\n"):
        text = text[:-1]
    lines = text.split("\n")
    if len(lines) < 2:
        raise ValueError("no column header line")
    return lines[0], lines[1].split("\t"), [l.split("\t") for l in lines[2:]]


def render(table, version, columns, rows):
    """Shard text for rows (lists of cells in column order), sorted by key."""
    out = [shard_header(table, version), "\t".join(columns)]
    out.extend("\t".join(r) for r in rows)
    return "\n".join(out) + "\n"


def key_indexes(cols):
    return [i for i, c in enumerate(cols) if c.key]


def row_key(row, kidx):
    return tuple(row[i] for i in kidx)


def sort_rows(rows, kidx):
    return sorted(rows, key=lambda r: row_key(r, kidx))


def check_cell(col, cell):
    """'' when cell is a legal value of col, else the reason."""
    if cell == "":
        return "empty cell (write - for no value)"
    try:
        cell.encode("utf-8")
    except UnicodeEncodeError:
        return "not valid UTF-8"
    if cell == EMPTY:
        return "required column has no value" if col.required else ""
    t = col.type
    if t == "int" and not _INT.match(cell):
        return "%r is not an int" % _clip(cell)
    if t == "bool" and cell not in ("true", "false"):
        return "%r is not true or false" % _clip(cell)
    if t == "date":
        ok = _DATE.match(cell) is not None
        if ok:
            try:
                datetime.date.fromisoformat(cell)
            except ValueError:
                ok = False
        if not ok:
            return "%r is not a YYYY-MM-DD date" % _clip(cell)
    if t == "list":
        for it in cell.split(","):
            if it == "" or it == EMPTY:
                return "%r is not a comma-separated list of non-empty items" % _clip(cell)
    return ""


def _clip(s):
    return s[:60] + "..." if len(s) > 60 else s


SHARD_PREFIX = "#carddb "
NOT_DECL_MSG = ("not a schema declaration: only schema/VERSION, schema/v<N>/<table>.tsv (identical to the "
                "trusted schema's declaration of that table) and schema/writers.tsv may live at or under schema/")

# The one fixed shape of schema/writers.tsv. The header is compared exactly,
# so the one non-declaration under schema/ cannot grow a column (or a column
# named oracle_text / a private table) to park data the boundary refuses.
WRITERS_HEADER = ("writer", "sla_hours", "tables", "doc")
WRITERS_MAX_SLA_HOURS = (1 << 63) - 1
_SLA_HOURS = re.compile(r"^[1-9][0-9]*$")
_WRITERS_HEADER_RE = re.compile(r"#carddb-writers v[1-9][0-9]*")


def parse_writers(rel, text, registry=None):
    """(writers {writer: sla_hours}, violations) of schema/writers.tsv.

    One parser for the validators (validate_tree) and the consumer
    (query.load_writers), so a file one of them accepts cannot make the other
    fail: the strict header, the strict positive-int SLA and the duplicate-writer
    rule are enforced in exactly one place. When registry is a dict it also gets
    {writer: (sla_hours, (table, ...), doc)} for each accepted row."""
    out, writers, first_line = [], {}, {}

    def add(line, msg):
        out.append("%s:%d: %s" % (rel, line, msg) if line > 0 else "%s: %s" % (rel, msg))

    if text == "":
        add(0, "empty file")
        return writers, out
    if not text.endswith("\n"):
        add(0, "does not end with a newline")
    lines = (text[:-1] if text.endswith("\n") else text).split("\n")
    if not _WRITERS_HEADER_RE.fullmatch(lines[0]):
        add(1, "first line %r is not `#carddb-writers v<N>`" % _clip(lines[0]))
        return writers, out
    if len(lines) < 2:
        add(0, "no column header line")
        return writers, out
    header = lines[1].split("\t")
    for name in header:
        if name == "":
            add(2, "empty column name")
        elif name in PRIVATE_TABLE_NAMES:
            add(2, "column %r names a private table" % name)
        elif forbidden_field(name):
            add(2, "column %r is a forbidden field" % name)
    if tuple(header) != WRITERS_HEADER:
        add(2, "columns must be exactly %s" % ", ".join(WRITERS_HEADER))
        return writers, out
    w, h = header.index("writer"), header.index("sla_hours")
    for i, line in enumerate(lines[2:], 3):
        cells = line.split("\t")
        if len(cells) != len(header):
            add(i, "%d cells, want %d" % (len(cells), len(header)))
            continue
        writer = cells[w]
        if writer == "":
            add(i, "empty writer")
            continue
        if writer in first_line:
            add(i, "duplicate writer %r (first at line %d)" % (writer, first_line[writer]))
            continue
        first_line[writer] = i
        cell = cells[h]
        if not _SLA_HOURS.fullmatch(cell):
            add(i, "sla_hours %r is not a strict positive int" % _clip(cell))
            continue
        # Bound the digit count before int(): a huge cell would hit Python's
        # int-conversion limit (a ValueError), where Go's ParseInt just says
        # out of range. No leading zeros, so more digits than the max is over.
        if len(cell) > len(str(WRITERS_MAX_SLA_HOURS)) or int(cell) > WRITERS_MAX_SLA_HOURS:
            add(i, "sla_hours %r is out of range" % _clip(cell))
            continue
        writers[writer] = int(cell)
        if registry is not None:
            registry[writer] = (writers[writer],
                                tuple(t for t in cells[header.index("tables")].split(",") if t),
                                cells[header.index("doc")])
    return writers, out


def writers_violations(rel, text):
    """Violations of the schema's writer SLA file. The one non-declaration under schema/ is a fixed
    shape too, so it cannot become a place to park arbitrary (private) data."""
    return parse_writers(rel, text)[1]


def is_data_file(rel):
    """A data shard is a .tsv outside schema/ (internal/carddb Validate)."""
    return rel.endswith(".tsv") and not is_schema_path(rel)


def read_text(path):
    """A file's text exactly as the tree validators read it (UTF-8 with undecodable
    bytes kept, no newline translation), so every consumer of one file sees one text."""
    with open(path, encoding="utf-8", errors="surrogateescape", newline="") as f:
        return f.read()


def tree_files(root):
    """(rel, text) of every regular file under root but .git, in lexical path order (Go's treeFiles)."""
    for dp, dn, fns in os.walk(root):
        dn[:] = sorted(d for d in dn if d != ".git")
        for fn in sorted(fns):
            p = os.path.join(dp, fn)
            if os.path.isfile(p) and not os.path.islink(p):
                yield os.path.relpath(p, root).replace(os.sep, "/"), read_text(p)


def validate_file(schema, scope, rel, text):
    """Violations of one file of a tree, as Go's Validate reports them for a file that stands alone:
    a schema/ file must be a declaration (writers.tsv is the one config exception), a non-.tsv file
    must not claim to be a shard, a .tsv is validated."""
    if is_schema_path(rel):
        if is_schema_aux(rel):
            return writers_violations(rel, text)
        return [] if is_schema_decl(schema, rel, text) else ["%s: %s" % (rel, NOT_DECL_MSG)]
    if not rel.endswith(".tsv"):
        return ["%s:1: carddb shard must be a .tsv file" % rel] if text.startswith(SHARD_PREFIX) else []
    return validate_shard(schema, scope, rel, text)


def cross_shard_duplicates(schema, scope, files):
    """Violations for a key that two different shards of one table both hold (validate_shard sees one
    file at a time; Go's Validate keeps one key map per table over the whole tree).

    files: iterable of (rel, text), any order; only data shards of a table the scope may hold count.
    A duplicate inside one file is validate_shard's to report."""
    out, seen = [], {}
    for rel, text in sorted(files):
        if not is_data_file(rel) or not text.endswith("\n"):
            continue
        lines = text[:-1].split("\n")
        hdr = parse_shard_header(lines[0])
        t = schema.tables.get(hdr[0]) if hdr else None
        if t is None or (scope != PRIVATE and t.is_private()) or len(lines) < 2:
            continue
        cols = t.scope_columns(scope)
        if lines[1].split("\t") != [c.name for c in cols]:
            continue
        kidx = key_indexes(cols)
        keys = seen.setdefault(t.name, {})
        for i, l in enumerate(lines[2:]):
            cells = l.split("\t")
            if "\r" in l or len(cells) != len(cols) or any(check_cell(c, v) for c, v in zip(cols, cells)):
                continue  # validate_shard reports it; it registers no key
            key = tuple(cells[j] for j in kidx)
            at = keys.get(key)
            if at is None:
                keys[key] = (rel, i + 3)
            elif at[0] != rel:
                out.append("%s:%d: duplicate key (%s), first at %s:%d" % (rel, i + 3, ", ".join(key), at[0], at[1]))
    return out


def validate_tree(schema, scope, files):
    """All violations of a tree's files (rel, text): per file, then keys duplicated across shards."""
    files = sorted(files)
    out = []
    for rel, text in files:
        out += validate_file(schema, scope, rel, text)
    return out + cross_shard_duplicates(schema, scope, files)


# The closed value sets of the keyproof ledger. One table-specific hook rather
# than per-column vocabularies: the schema's string columns are open by design,
# but a keyproof row can only mean one of these.
KEYPROOF_LEGS = ("xmage", "forge", "lockstep", "scenario", "cite")
KEYPROOF_STATES = ("agreed", "diagnosed", "diverged", "owed", "stale", "excluded")
KEYPROOF_CITE_STATES = ("diagnosed", "owed", "excluded")
KEYPROOF_SEATS = ("0", "2", "3", "4")


def keyproof_row_rules(rel, head, row):
    """Violations of a keyproof row that cannot mean anything, as `rel: key: rule` messages.

    Runs after the generic cell checks and looks every column up by name, so a
    reordered header does not change the verdict. The row key is the schema's
    key columns in order: oracle_id, key, leg."""
    cell = {name: row[i] for i, name in enumerate(head) if i < len(row)}
    leg, state, cite, seats = cell.get("leg", ""), cell.get("state", ""), cell.get("cite", ""), cell.get("seats", "")
    key = " ".join((cell.get("oracle_id", ""), cell.get("key", ""), leg))
    out = []

    def add(msg):
        out.append("%s: %s: %s" % (rel, key, msg))

    if leg not in KEYPROOF_LEGS:
        add("leg '%s' is not xmage, forge, lockstep, scenario or cite" % leg)
    if state not in KEYPROOF_STATES:
        add("state '%s' is not agreed, diagnosed, diverged, owed, stale or excluded" % state)
    if state == "diagnosed" and cite == EMPTY:
        add("diagnosed without a cite")
    if leg == "cite" and state not in KEYPROOF_CITE_STATES:
        add("cite leg must be diagnosed, owed or excluded, not '%s'" % state)
    if seats not in KEYPROOF_SEATS:
        add("seats '%s' is not 0, 2, 3 or 4" % seats)
    if state == "agreed" and seats == "0":
        add("agreed with seats 0")
    return out


def validate_shard(schema, scope, rel, text):
    """Violations (strings `path:line: msg`) of one shard, as Go's validateShard reports them for a shard
    that stands alone. Keys that two shards of a table share are cross_shard_duplicates'."""
    out = []

    def add(line, msg):
        out.append("%s:%d: %s" % (rel, line, msg) if line > 0 else "%s: %s" % (rel, msg))

    if text == "":
        add(0, "empty file")
        return out
    if not text.endswith("\n"):
        add(0, "does not end with a newline")
    lines = (text[:-1] if text.endswith("\n") else text).split("\n")
    hdr = parse_shard_header(lines[0])
    if hdr is None:
        add(1, "first line %r is not `#carddb <table> v<N>`" % _clip(lines[0]))
        return out
    table, ver = hdr
    if ver != schema.version:
        add(1, "shard is v%d, schema is v%d" % (ver, schema.version))
    t = schema.tables.get(table)
    if t is None:
        add(1, "table %r is not declared in the schema" % table)
        return out
    if not path_holds_table(rel, table):
        add(1, "table %r must live at %s.tsv or under %s/" % (table, table, table))
    if scope != PRIVATE and t.is_private():
        add(1, "table %r is private and not allowed in a public tree" % table)
        return out
    cols = t.scope_columns(scope)
    if len(lines) < 2:
        add(0, "no column header line")
        return out
    head = lines[1].split("\t")
    if head != [c.name for c in cols]:
        add(2, _column_diff(head, cols))
        return out
    kidx = key_indexes(cols)
    prev, seen = None, {}
    for i, l in enumerate(lines[2:]):
        ln = i + 3
        if "\r" in l:
            add(ln, "carriage return in row")
            continue
        cells = l.split("\t")
        if len(cells) != len(cols):
            add(ln, "%d cells, want %d" % (len(cells), len(cols)))
            continue
        bad = False
        for c, cell in zip(cols, cells):
            msg = check_cell(c, cell)
            if msg:
                add(ln, "%s: %s" % (c.name, msg))
                bad = True
        if bad:
            continue
        if table == "keyproof":
            out += keyproof_row_rules(rel, head, cells)
        key = tuple(cells[j] for j in kidx)
        if key in seen:
            add(ln, "duplicate key (%s), first at %s:%d" % (", ".join(key), rel, seen[key]))
        else:
            seen[key] = ln
            if prev is not None and prev > key:
                add(ln, "rows not sorted: key (%s) follows (%s)" % (", ".join(key), ", ".join(prev)))
        prev = key
    return out


def _column_diff(head, cols):
    want = [c.name for c in cols]
    extra = sorted(h for h in head if h not in want)
    missing = [n for n in want if n not in head]
    if extra or missing:
        return "columns differ from the schema: extra [%s], missing [%s]" % (" ".join(extra), " ".join(missing))
    return "columns are in the wrong order: want %s" % "\t".join(want)
