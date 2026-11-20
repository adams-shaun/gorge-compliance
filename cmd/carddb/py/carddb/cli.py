"""carddb command line (python3 -I cmd/carddb/main.py <verb>).

  put TABLE [--source ID] [--version V]   rows from stdin: TSV, first line the column names
  publish [--bulk] [--private]            commit what is staged, rebase, push
  merge-driver %O %A %B %P                git's merge driver (registered by the tool itself)
  build [--private] [--text FILE]         SQLite from the shards
  fetch [--private]                       gh release download carddb-latest
  init [--private]                        clone the data branch, or create it
  validate|boundary [--scope S] [--tree DIR]   the Python mirror of the Go checks
Exit status: 0 ok, 1 violations or a failed merge/publish, 2 usage or I/O error.
"""
import argparse
import os
import sys

from . import build as build_mod
from . import fetch as fetch_mod
from . import merge as merge_mod
from . import shard
from .boundary import boundary_text
from .schema import PRIVATE, PUBLIC, SchemaError, load_schema, repo_root
from .shard import ValidationError
from .store import GitError, Store, StoreError, carddb_home, gorge12, tree_shards


def _parser():
    ap = argparse.ArgumentParser(prog="carddb", description=__doc__.split("\n")[0])
    ap.add_argument("--home", help="CARDDB_HOME (default ~/.local/share/carddb)")
    ap.add_argument("--schema", help="schema directory (default: the checkout's carddb/schema)")
    sub = ap.add_subparsers(dest="verb", required=True)
    p = sub.add_parser("put")
    p.add_argument("table")
    p.add_argument("--source", help="writer id recorded in _meta/writers (default $CARDDB_WRITER or cli)")
    p.add_argument("--version", help="writer version recorded in _meta/writers")
    p = sub.add_parser("publish")
    p.add_argument("--bulk", action="store_true", help="allow more than 50000 changed lines")
    p.add_argument("--private", action="store_true")
    p = sub.add_parser("merge-driver")
    for n in ("base", "ours", "theirs", "path"):
        p.add_argument(n)
    p = sub.add_parser("build")
    p.add_argument("--private", action="store_true")
    p.add_argument("--text", metavar="FILE", help="Scryfall oracle-cards or MTGJSON AtomicCards file: adds Oracle text to FTS (never published)")
    p.add_argument("--out", help="output file")
    p.add_argument("--ref", default="origin/main", help="ref of the checkout to read curated files and committed sources from")
    p.add_argument("--src-dir", help="read curated files and committed sources from this directory instead of a git ref")
    p = sub.add_parser("fetch")
    p.add_argument("--private", action="store_true")
    p = sub.add_parser("init")
    p.add_argument("--private", action="store_true")
    for v in ("validate", "boundary"):
        p = sub.add_parser(v)
        p.add_argument("--scope", default=PUBLIC, choices=(PUBLIC, PRIVATE))
        p.add_argument("--tree", help="tree to check (default: the clone)")
    return ap


def _read_rows(stream):
    lines = stream.read().split("\n")
    if lines and lines[-1] == "":
        lines.pop()
    if lines and lines[0].startswith("#carddb "):
        lines.pop(0)
    if not lines:
        raise StoreError("put: no column header line on stdin")
    names = lines[0].split("\t")
    rows = []
    for i, l in enumerate(lines[1:], 2):
        cells = l.split("\t")
        if len(cells) != len(names):
            raise StoreError("put: stdin line %d has %d cells, want %d" % (i, len(cells), len(names)))
        rows.append(dict(zip(names, cells)))
    return rows


def main(argv=None, stdin=None, stdout=None, stderr=None):
    stdin, stdout, stderr = stdin or sys.stdin, stdout or sys.stdout, stderr or sys.stderr
    args = _parser().parse_args(argv)
    try:
        schema = load_schema(args.schema)
        kind = PRIVATE if getattr(args, "private", False) else PUBLIC
        if args.verb == "merge-driver":
            try:
                merge_mod.run_driver(schema, args.base, args.ours, args.theirs, args.path)
            except merge_mod.MergeError as e:
                print("carddb merge-driver:", e, file=stderr)
                return 1
            return 0
        if args.verb == "put":
            t = schema.tables.get(args.table)
            k = PRIVATE if t is not None and t.is_private() and not args.table.startswith("_meta/") else PUBLIC
            n = Store(k, home=args.home, schema=schema).put(args.table, _read_rows(stdin), writer=args.source,
                                                              version=args.version, raw=True)
            print("carddb put %s: %d rows changed" % (args.table, n), file=stdout)
            return 0
        if args.verb == "publish":
            r = Store(kind, home=args.home, schema=schema).publish(bulk=args.bulk)
            if r is None:
                print("carddb publish: nothing staged", file=stdout)
            else:
                print("carddb publish: %s%s" % (r["message"] or "pushed local commits",
                                                "" if r["pushed"] else " (not pushed)"), file=stdout)
            return 0
        if args.verb == "init":
            Store(kind, home=args.home, schema=schema).init()
            print("carddb init: ok (%s)" % os.path.join(carddb_home(args.home), kind), file=stdout)
            return 0
        if args.verb == "fetch":
            print(fetch_mod.fetch(private=kind == PRIVATE, home=args.home), file=stdout)
            return 0
        if args.verb == "build":
            home = carddb_home(args.home)
            pub = Store(PUBLIC, home=args.home, schema=schema)
            if not pub.exists():
                raise StoreError("no public clone at %s: run `carddb init`" % pub.dir)
            priv = None
            if args.private:
                priv = Store(PRIVATE, home=args.home, schema=schema)
                if not priv.exists():
                    raise StoreError("no private clone at %s: run `carddb init --private`" % priv.dir)
            name = "carddb-text.sqlite" if args.text else ("carddb-private.sqlite" if args.private else "carddb.sqlite")
            out = args.out or os.path.join(home, name)
            committed = build_mod.read_committed(ref=args.ref, directory=args.src_dir)
            revs = {"gorge_pin": gorge12()}
            revs["public_rev"] = pub.git("rev-parse", "HEAD").stdout.strip()[:12]
            if priv:
                revs["private_rev"] = priv.git("rev-parse", "HEAD").stdout.strip()[:12]
            build_mod.build(out, schema, pub.dir, priv.dir if priv else None, committed, args.text, revs)
            print(out, file=stdout)
            return 0
        if args.verb in ("validate", "boundary"):
            root = args.tree or Store(PUBLIC, home=args.home, schema=schema).dir
            viol = []
            for rel, text in tree_shards(root):
                if args.verb == "validate":
                    viol += shard.validate_shard(schema, args.scope, rel, text)
                else:
                    viol += boundary_text(schema, rel, text)
            for v in viol:
                print(v, file=stdout)
            if viol:
                print("carddb %s: %d violation(s)" % (args.verb, len(viol)), file=stderr)
                return 1
            print("carddb %s: ok (%s)" % (args.verb, root), file=stdout)
            return 0
    except ValidationError as e:
        print("carddb %s: invalid:\n%s" % (args.verb, e), file=stderr)
        return 1
    except (StoreError, SchemaError) as e:
        print("carddb %s: %s" % (args.verb, e), file=stderr)
        return 1 if isinstance(e, GitError) and args.verb == "publish" else 2
    return 2
