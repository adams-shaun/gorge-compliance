"""Shards: parse, render, validate (a port of internal/carddb/validate.go).

A shard is `#carddb <table> v<N>`, a column-header line, then rows sorted
bytewise by the key columns, keys unique. A cell is never empty (`-` is the
empty value) and never holds a tab, newline or CR.
"""
import datetime
import re

from .schema import EMPTY, PRIVATE, PUBLIC

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


def validate_shard(schema, scope, rel, text):
    """Violations (strings `path:line: msg`) of one shard, as Go's validateShard
    reports them for a shard that stands alone (cross-shard duplicate keys cannot
    occur: the shard path is a function of the key)."""
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
