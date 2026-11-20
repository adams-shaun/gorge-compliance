"""The public boundary (a port of internal/carddb/boundary.go for data files).

Nothing private, nothing with a banned field name, no Forge-script-shaped
line may reach a public tree. Schema declarations are not data and live on
develop, never on the data branch, so any file under schema/ is refused here.
"""
import re

from .schema import PRIVATE, PRIVATE_TABLE_NAMES, forbidden_field
from .shard import parse_shard_header

_FORGE_LINE = re.compile(r"^A:|^SVar:|^T:Mode\$|^S:Mode\$|\$ [A-Z]")
_JSON_FIELD = re.compile(r'(?i)"(oracle_text|price[a-z0-9_]*|prims|tagger|edhrec[a-z0-9_]*|value[a-z0-9_]*)"\s*:')
_SHARD_PREFIX = "#carddb "


def boundary_text(schema, rel, text):
    """Violations of the public boundary in one file's text (rel is slash-separated)."""
    out = []

    def add(line, msg):
        out.append("%s:%d: %s" % (rel, line, msg) if line > 0 else "%s: %s" % (rel, msg))

    if rel == "schema" or rel.startswith("schema/"):
        add(0, "the data branch holds no schema: only data shards (the schema lives on develop)")
    for comp in rel.split("/"):
        stem = comp.rsplit(".", 1)[0] if "." in comp.lstrip(".") else comp
        for n in (stem, comp):
            if n in PRIVATE_TABLE_NAMES:
                add(0, "path names private table %r" % n)
                break
            if forbidden_field(n):
                add(0, "path names forbidden field %r" % n)
                break
    is_tsv = rel.endswith(".tsv")
    is_json = rel.endswith(".json") or rel.endswith(".jsonl")
    table, cols_at = "", 0
    for ln, line in enumerate(text.split("\n"), 1):
        line = line.rstrip("\r")
        if _FORGE_LINE.search(line):
            add(ln, "Forge-script-shaped line (pattern %s)" % _FORGE_LINE.pattern)
        if (is_tsv and ln == 1) or (not is_tsv and line.startswith(_SHARD_PREFIX)):
            table, cols_at = _header_table(schema, ln, line, add), ln + 1
        elif ln == cols_at:
            _column_violations(schema, table, line, lambda m, ln=ln: add(ln, m))
        if is_json:
            m = _JSON_FIELD.search(line)
            if m:
                add(ln, "field %r is forbidden in a public tree" % m.group(1))
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
        for c in t.columns:
            if c.name == name and c.visibility == PRIVATE:
                add("column %r of table %r is private" % (name, table))
