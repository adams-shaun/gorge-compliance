"""The public boundary (a port of internal/carddb/boundary.go, every file kind).

Nothing private, nothing with a banned field name, no undeclared column, no
Forge-script-shaped line may reach a public tree. Only a real schema declaration
(it names private columns, so it is exempt from the field checks) and the
fixed-shape schema/writers.tsv config may live under schema/.
"""
import re

from .schema import (PRIVATE, PRIVATE_TABLE_NAMES, forbidden_field, is_schema_aux,
                     is_schema_decl, is_schema_path)
from .shard import NOT_DECL_MSG, parse_shard_header, writers_violations

_FORGE_LINE = re.compile(r"^A:|^SVar:|^T:Mode\$|^S:Mode\$|\$ [A-Z]")
_JSON_FIELD = re.compile(r'(?i)"(oracle_text|price[a-z0-9_]*|prims|tagger|edhrec[a-z0-9_]*|value[a-z0-9_]*)"\s*:')
_SHARD_PREFIX = "#carddb "


def boundary_text(schema, rel, text):
    """Violations of the public boundary in one file's text (rel is slash-separated).

    A real schema declaration (is_schema_decl) names private tables and columns, so it is exempt
    from the table and field checks (not from the Forge-line scan); any other file at or under
    schema/ is refused, the data branch holds no schema."""
    out = []

    def add(line, msg):
        out.append("%s:%d: %s" % (rel, line, msg) if line > 0 else "%s: %s" % (rel, msg))

    decl = False
    aux = is_schema_aux(rel)
    if aux:
        out += writers_violations(rel, text)
    if is_schema_path(rel) and not aux:
        decl = is_schema_decl(schema, rel, text)
        if not decl:
            add(0, NOT_DECL_MSG)
    data = not decl
    is_tsv = rel.endswith(".tsv")
    is_json = rel.endswith(".json") or rel.endswith(".jsonl")
    if data:
        for comp in rel.split("/"):
            stem = comp.rsplit(".", 1)[0] if "." in comp.lstrip(".") else comp
            for n in (stem, comp):
                if n in PRIVATE_TABLE_NAMES:
                    add(0, "path names private table %r" % n)
                    break
                if forbidden_field(n):
                    add(0, "path names forbidden field %r" % n)
                    break
    table, cols_at = "", 0
    lines = text.split("\n")
    if lines and lines[-1] == "":
        lines.pop()  # the empty piece after the final newline is not a line
    for ln, line in enumerate(lines, 1):
        line = line.rstrip("\r")
        if _FORGE_LINE.search(line):
            add(ln, "Forge-script-shaped line (pattern %s)" % _FORGE_LINE.pattern)
        if decl and rel == "schema/VERSION" and (ln > 1 or not re.fullmatch(r"[0-9]+", line)):
            add(ln, "schema/VERSION must hold only the version number")
        if data and not aux and ((is_tsv and ln == 1) or (not is_tsv and line.startswith(_SHARD_PREFIX))):
            table, cols_at = _header_table(schema, ln, line, add), ln + 1
        elif data and ln == cols_at:
            _column_violations(schema, table, line, lambda m, ln=ln: add(ln, m))
        if data and is_json:
            m = _JSON_FIELD.search(line)
            if m:
                add(ln, "field %r is forbidden in a public tree" % m.group(1))
    return out


def boundary_tree(schema, files):
    """Violations of the public boundary over a tree's files (rel, text)."""
    out = []
    for rel, text in sorted(files):
        out += boundary_text(schema, rel, text)
    return out


def _header_table(schema, ln, line, add):
    hdr = parse_shard_header(line)
    if hdr is None:
        add(ln, "not a carddb shard: first line %r is not `#carddb <table> v<N>`" % line[:60])
        return ""
    table = hdr[0]
    t = schema.tables.get(table)
    if table in PRIVATE_TABLE_NAMES or (t is not None and t.is_private()):
        add(ln, "private table %r" % table)
    return table


def _column_violations(schema, table, line, add):
    t = schema.tables.get(table) if table else None
    for name in line.split("\t"):
        if forbidden_field(name):
            add("field %r is forbidden in a public tree" % name)
            continue
        if t is None:
            continue
        col = next((c for c in t.columns if c.name == name), None)
        if col is not None and col.visibility == PRIVATE:
            add("column %r of table %r is private" % (name, table))
        elif col is None:
            # A public shard may not grow a column (such as `comment` on
            # ruling_refs) to carry the private data of its table.
            add("column %r is not declared public in table %r" % (name, table))
