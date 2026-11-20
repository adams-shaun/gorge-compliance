"""Key-aware merge of two edits of one shard (the git merge driver).

Rows merge per key against the common ancestor. A key both sides changed
differently resolves by, in order: the newer timestamp column (measured_at,
else measured, declared, ran, retrieved: the first the table has), the writer
priority (ci > cert > loop, from the table's `writer` column when it has one),
then the greater row bytes. The last step makes the merge commutative and
idempotent, so two clones that merge in either order converge on one file.
A modify beats a delete.
"""
from . import shard
from .schema import PRIVATE, PUBLIC

TIMESTAMP_COLUMNS = ("measured_at", "measured", "declared", "ran", "retrieved")
WRITER_PRIORITY = {"ci": 3, "cert": 2, "loop": 1}


class MergeError(Exception):
    pass


def _rank(cols, row):
    names = [c.name for c in cols]
    ts = ""
    for tc in TIMESTAMP_COLUMNS:
        if tc in names:
            v = row[names.index(tc)]
            ts = "" if v == "-" else v
            break
    prio = 0
    if "writer" in names:
        prio = WRITER_PRIORITY.get(row[names.index("writer")], 0)
    return (ts, prio, "\t".join(row))


def merge_rows(cols, base, ours, theirs):
    """Three-way merge of row lists (cells in column order). -> sorted row list."""
    kidx = shard.key_indexes(cols)
    key = lambda r: shard.row_key(r, kidx)
    b = {key(r): r for r in base}
    o = {key(r): r for r in ours}
    t = {key(r): r for r in theirs}
    out = []
    for k in sorted(set(b) | set(o) | set(t)):
        bv, ov, tv = b.get(k), o.get(k), t.get(k)
        if ov == tv:
            pick = ov
        elif ov == bv:
            pick = tv
        elif tv == bv:
            pick = ov
        elif ov is None:
            pick = tv
        elif tv is None:
            pick = ov
        else:
            pick = max(ov, tv, key=lambda r: _rank(cols, r))
        if pick is not None:
            out.append(pick)
    return out


def merge_texts(schema, rel, base, ours, theirs):
    """Merged shard text of three shard texts (base may be empty: an add/add).
    Raises MergeError when the sides are not mergeable shards of one table."""
    try:
        oh, ocols, orows = shard.parse_text(ours)
        th, tcols, trows = shard.parse_text(theirs)
    except ValueError as e:
        raise MergeError("%s: %s" % (rel, e))
    if oh != th or ocols != tcols:
        raise MergeError("%s: the two sides disagree on header or columns" % rel)
    hdr = shard.parse_shard_header(oh)
    if hdr is None or hdr[0] not in schema.tables:
        raise MergeError("%s: not a shard of a declared table: %r" % (rel, oh))
    table = schema.tables[hdr[0]]
    scope = PRIVATE if table.is_private() else PUBLIC
    cols = table.scope_columns(scope)
    if ocols != [c.name for c in cols]:
        raise MergeError("%s: columns differ from the schema" % rel)
    if base.strip() == "":
        brows = []
    else:
        try:
            bh, bcols, brows = shard.parse_text(base)
        except ValueError as e:
            raise MergeError("%s: ancestor: %s" % (rel, e))
        if bh != oh or bcols != ocols:
            raise MergeError("%s: the ancestor disagrees on header or columns" % rel)
    for name, rows in (("ours", orows), ("theirs", trows), ("base", brows)):
        for r in rows:
            if len(r) != len(cols):
                raise MergeError("%s: %s has a row of %d cells, want %d" % (rel, name, len(r), len(cols)))
    merged = merge_rows(cols, brows, orows, trows)
    text = shard.render(hdr[0], hdr[1], ocols, merged)
    from .boundary import boundary_text
    viol = shard.validate_shard(schema, scope, rel, text)
    if scope == PUBLIC:
        viol += boundary_text(schema, rel, text)
    if viol:
        raise MergeError("merged %s fails validation:\n%s" % (rel, "\n".join(viol[:10])))
    return text


def run_driver(schema, base_path, ours_path, theirs_path, rel):
    """git's merge-driver protocol: write the merge into ours_path; raise on conflict."""
    def read(p):
        with open(p, encoding="utf-8", newline="") as f:
            return f.read()
    text = merge_texts(schema, rel, read(base_path), read(ours_path), read(theirs_path))
    with open(ours_path, "w", encoding="utf-8", newline="") as f:
        f.write(text)
