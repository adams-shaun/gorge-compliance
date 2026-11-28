"""carddb command line (python3 -I cmd/carddb/main.py <verb>).

  put TABLE [--source ID] [--version V] [--if-absent] [--refresh COLS]
                                        rows from stdin: TSV, first line the column names; --if-absent adds only
                                        rows whose key is not stored yet (--refresh COLS: the stored row still
                                        takes those columns from the input)
  ingest catalogue --mtgjson FILE         MTGJSON AllPrintings -> the catalogue tables, full snapshot
  ingest status --status-a FILE --meta FILE [--status-b FILE]
                                        oraclediff status output -> cert_sets status columns (docs site data)
  docs-status --db FILE --out DIR         built SQLite -> status-A.jsonl, status-B.jsonl, meta.json for the docs build
  docs-sets --db FILE --out DIR           built SQLite -> sets.json, writers.json, sets-carddb.md (per-set xproof snippet)
  migrate keyproof-legs                   rewrite the legacy keyproof shards of the public clone as leg rows
  publish [--bulk] [--private]            commit what is staged, rebase, push
  merge-driver %O %A %B %P                git's merge driver (registered by the tool itself)
  build [--private] [--text FILE]         SQLite from the shards
  proven [--db FILE] [--format F]         cards certification has proven (C2/C3), as tsv, json or count
  fetch [--private]                       gh release download carddb-latest
  init [--private]                        clone the data branch, or create it
  compliance [--repo DIR] [--source ID] [--version V] [--measured DATE]
                                        XMage manifests + FORGE_ORACLE_REF -> support rows
  dirac [--repo DIR] [--gorge-dir DIR] [--source ID] [--version V] [--measured DATE]
                                        gorge's dirac support lists + the gaps snapshot -> support, evidence rows
  inclusion --from FILE --sample NAME [--cards TSV] [--out PATH]
                                        a deck sample's per-card table -> carddb/curated/commander-inclusion.tsv
  size [--measured DATE]                  report the branch object store into _meta/size (D7 budget)
  release [--dry-run]                     upload carddb-latest, the weekly dated release, prune
  validate|boundary [--scope S] [--tree DIR]   the Python mirror of the Go checks
  card <name|oracle_id>                   every fact for one card
  search <fts> [--legal F] [--status K=V] [--set S] [--label L] [--marked]
  blockers --format F [--engine E] [--by printings|decks]
  proven [--format F] [--level C2] [--by cert|keys]   the certification-pinned proven view; --by keys: per-key proof
  need [--format F] [--limit N]           owed and diverged ability keys, most valuable cards first
  proof --set S                           per-set key proof report
  deck <id>                               a private deck
  freshness [--tick]                      writer SLAs, publish/asset age, unresolved, branch size
  sql '<select>'                          read-only SQL
  export <view> --tsv                     deterministic TSV of a table or view
Exit status: 0 ok, 1 violations or a failed merge/publish, 2 usage or I/O error.
"""
import argparse
import os
import sys

from . import build as build_mod
from . import catalogue as catalogue_mod
from . import compliance as compliance_mod
from . import dirac as dirac_mod
from . import docsstatus as docsstatus_mod
from . import fetch as fetch_mod
from . import inclusion as inclusion_mod
from . import keylegs as keylegs_mod
from . import merge as merge_mod
from . import query as query_mod
from . import release as release_mod
from . import shard
from .boundary import boundary_tree
from .norm import Resolver
from .schema import PRIVATE, PUBLIC, SchemaError, load_schema, repo_root
from .shard import ValidationError
from .store import GitError, Store, StoreError, carddb_home, gorge12, packed_mib


def _parser():
    ap = argparse.ArgumentParser(prog="carddb", description=__doc__.split("\n")[0])
    ap.add_argument("--home", help="CARDDB_HOME (default ~/.local/share/carddb)")
    ap.add_argument("--schema", help="schema directory (default: the checkout's carddb/schema)")
    sub = ap.add_subparsers(dest="verb", required=True)
    p = sub.add_parser("put")
    p.add_argument("table")
    p.add_argument("--source", help="writer id recorded in _meta/writers (default $CARDDB_WRITER or cli)")
    p.add_argument("--version", help="writer version recorded in _meta/writers")
    p.add_argument("--if-absent", action="store_true",
                   help="drop each input row whose key the store already holds instead of overwriting it "
                        "(the keyproof seed put); with --refresh the stored row still takes those columns")
    p.add_argument("--refresh", metavar="COLS",
                   help="with --if-absent: comma-separated column names the stored row takes from the input row")
    p = sub.add_parser("ingest")
    p.add_argument("source", choices=("catalogue", "status"))
    p.add_argument("--mtgjson", metavar="FILE", help="catalogue: MTGJSON AllPrintings.json or .json.gz")
    p.add_argument("--status-a", metavar="FILE", help="status: `oraclediff status -all -json -level A` output")
    p.add_argument("--status-b", metavar="FILE", help="status: the Level B output (optional)")
    p.add_argument("--meta", metavar="FILE",
                   help="status: meta.json {commit, gorge_pin, forge_ref, date, ratchet_failures}")
    p.add_argument("--manifests", metavar="DIR",
                   help="set manifests to cross-check (default %s; empty disables)" % catalogue_mod.MANIFEST_DIR)
    p.add_argument("--source", dest="writer",
                   help="writer id recorded in _meta/writers (default mtgjson)")
    p.add_argument("--version", help="writer version recorded in _meta/writers (default MTGJSON meta.version)")
    p = sub.add_parser("docs-status")
    p.add_argument("--db", required=True, metavar="FILE", help="built carddb.sqlite")
    p.add_argument("--out", required=True, metavar="DIR", help="directory for status-A.jsonl, status-B.jsonl, meta.json")
    p = sub.add_parser("docs-sets")
    p.add_argument("--db", required=True, metavar="FILE", help="built carddb.sqlite")
    p.add_argument("--out", required=True, metavar="DIR", help="directory for sets.json, writers.json, sets-carddb.md")
    p = sub.add_parser("migrate")
    p.add_argument("what", choices=("keyproof-legs",))
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
    p.add_argument("--ref", default="HEAD", help="ref of the checkout to read curated files and committed sources from")
    p.add_argument("--src-dir", help="read curated files and committed sources from this directory instead of a git ref")
    p = sub.add_parser("fetch")
    p.add_argument("--private", action="store_true")
    p = sub.add_parser("init")
    p.add_argument("--private", action="store_true")
    p = sub.add_parser("compliance")
    p.add_argument("--repo", help="gorge-compliance checkout holding oracle/compliance/manifests and Makefile"
                                 " (default: the checkout)")
    p.add_argument("--source", help="writer id recorded in _meta/writers (default ci)")
    p.add_argument("--version", help="writer version recorded in _meta/writers")
    p.add_argument("--measured", metavar="YYYY-MM-DD",
                   help="the support rows' measured date (default $CARDDB_NOW, else today UTC);"
                        " pass it (or CARDDB_NOW) in CI so reruns stay byte-identical")
    p = sub.add_parser("dirac")
    p.add_argument("--repo", help="gorge-compliance checkout holding GORGE_PIN and oracle/compliance/dirac-gaps.tsv"
                                 " (default: the checkout)")
    p.add_argument("--gorge-dir", help="the pinned gorge module (default $CARDDB_GORGE_DIR, else `go list -m`)")
    p.add_argument("--source", help="writer id recorded in _meta/writers (default ci)")
    p.add_argument("--version", help="writer version recorded in _meta/writers")
    p.add_argument("--measured", metavar="YYYY-MM-DD",
                   help="the support rows' measured date (default $CARDDB_NOW, else today UTC)")
    p = sub.add_parser("inclusion")
    p.add_argument("--from", dest="src", required=True, metavar="FILE",
                   help="the sample's per-card table: card decks rate rate_precon rate_casual rate_cedh rate_ranking")
    p.add_argument("--sample", required=True, metavar="NAME", help="the sample's name, e.g. decksample-v1")
    p.add_argument("--cards", metavar="TSV", help="oracle_id<TAB>name card list (default: the public clone's cards)")
    p.add_argument("--out", metavar="PATH", help="output file (default %s)" % inclusion_mod.DEFAULT_OUT)
    p = sub.add_parser("size")
    p.add_argument("--source", default="ci", help="writer id recorded in _meta/writers (default ci)")
    p.add_argument("--version", help="writer version recorded in _meta/writers")
    p.add_argument("--measured", metavar="YYYY-MM-DD",
                   help="the _meta/size measured date (default $CARDDB_NOW, else today UTC)")
    p.add_argument("--max-pack-mb", type=float, default=300.0, help="the D7 packed-size budget (default 300)")
    p = sub.add_parser("release")
    p.add_argument("--repo", help="owner/name of the release repo (default $GITHUB_REPOSITORY or the public repo)")
    p.add_argument("--db", help="built SQLite file (default $CARDDB_HOME/carddb.sqlite)")
    p.add_argument("--asset", action="append", metavar="FILE",
                   help="asset to upload; repeatable (default <db>.zst when it exists, else <db>)")
    p.add_argument("--now", metavar="YYYY-MM-DD", help="release week and prune date (default $CARDDB_NOW)")
    p.add_argument("--gh", help="gh executable (default $CARDDB_GH or gh)")
    p.add_argument("--dry-run", action="store_true", help="build the plan and print mutating gh commands; upload nothing")
    p.add_argument("--no-weekly", action="store_true", help="do not create the dated weekly release")
    p.add_argument("--keep-weeks", type=int, default=12, help="weeks of dated releases to keep (default 12)")
    p.add_argument("--max-pack-mb", type=float, default=300.0,
                   help="the D7 packed-size budget to report (default 300)")
    for v in ("validate", "boundary"):
        p = sub.add_parser(v)
        p.add_argument("--scope", default=PUBLIC, choices=(PUBLIC, PRIVATE))
        p.add_argument("--tree", help="tree to check (default: the clone)")
    read = (
        ("card", (("ref", {}),)),
        ("search", (("query", {}),)),
        ("blockers", ()),
        ("proven", ()),
        ("need", ()),
        ("proof", ()),
        ("deck", (("deck_id", {}),)),
        ("freshness", ()),
        ("sql", (("query", {}),)),
        ("export", (("view", {}),)),
    )
    for name, positionals in read:
        p = sub.add_parser(name, help=query_mod.__doc__.split("\n")[0])
        for pname, kw in positionals:
            p.add_argument(pname, **kw)
        p.add_argument("--db", help="built SQLite file (default $CARDDB_HOME/carddb[-private].sqlite)")
        p.add_argument("--private", action="store_true", help="use the private build")
    p = sub.choices["search"]
    p.add_argument("--legal", metavar="FORMAT")
    p.add_argument("--status", action="append", metavar="COL=VALUE")
    p.add_argument("--set", metavar="SET")
    p.add_argument("--label", metavar="LABEL")
    p.add_argument("--marked", action="store_true")
    p.add_argument("--limit", type=int)
    p = sub.choices["blockers"]
    p.add_argument("--format", required=True, metavar="FORMAT")
    p.add_argument("--engine", help="dirac, gorge, proven or cert:<level>")
    p.add_argument("--by", choices=("printings", "decks"), default=None,
                   help="rank by printings (public default) or real decks (private default)")
    p.add_argument("--limit", type=int, default=50)
    p = sub.choices["proven"]
    p.add_argument("--format", metavar="FORMAT")
    p.add_argument("--level", default="C2", choices=query_mod.LEVEL_ORDER)
    p.add_argument("--by", default="cert", choices=("cert", "keys"),
                   help="cert (default): the cert table's proven view; keys: every ability key proven")
    p = sub.choices["need"]
    p.add_argument("--format", metavar="FORMAT")
    p.add_argument("--limit", type=int, default=50)
    p.add_argument("--recredit", metavar="REFS",
                   help="list agreed keys whose refs (less +P) are exactly REFS, e.g. X, in place of the "
                        "owed and diverged keys: the keys to replay for another engine's credit")
    p = sub.choices["proof"]
    p.add_argument("--set", required=True, metavar="SET")
    p = sub.choices["freshness"]
    p.add_argument("--tick", action="store_true", help="print only `!!` lines")
    p.add_argument("--public-dir", help="the data branch clone (default $CARDDB_HOME/public|private)")
    p.add_argument("--asset-meta", help="release metadata JSON (default $CARDDB_HOME/carddb[-private]-latest.json)")
    p.add_argument("--sla", help="writer SLA file (default the schema directory's writers.tsv)")
    p.add_argument("--now", help="ISO date or timestamp to judge ages at (default $CARDDB_NOW or now)")
    p.add_argument("--max-pack-mb", type=float, default=300.0)
    p.add_argument("--unresolved-baseline", type=int)
    p = sub.choices["export"]
    p.add_argument("--tsv", action="store_true")
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
        if args.verb == "proven" and args.by == "cert" and args.format in (None, "tsv", "json", "count"):
            # The certification view over the clone (or a built file's `proven`
            # view); any other --format is a legality filter over the built cards.
            rows = (query_mod.proven_from_db(args.db) if args.db
                    else query_mod.proven_from_store(Store(PUBLIC, home=args.home, schema=schema)))
            print(query_mod.format_proven(rows, args.format), end="", file=stdout)
            return 0
        if args.verb in query_mod.VERBS:
            if args.verb == "freshness":
                return query_mod.run_freshness(args, schema, stdout)
            db = args.db or query_mod.default_db(args.home, args.private)
            conn, meta = query_mod.connect(db)
            try:
                return query_mod.run(args, conn, schema, meta, stdout)
            finally:
                conn.close()
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
            refresh = tuple(c.strip() for c in args.refresh.split(",") if c.strip()) if args.refresh else ()
            n = Store(k, home=args.home, schema=schema).put(args.table, _read_rows(stdin), writer=args.source,
                                                              version=args.version, raw=True,
                                                              if_absent=args.if_absent, refresh=refresh)
            print("carddb put %s: %d rows changed" % (args.table, n), file=stdout)
            return 0
        if args.verb == "migrate":
            pub = Store(PUBLIC, home=args.home, schema=schema)
            if not pub.exists():
                raise StoreError("no public clone at %s: run `carddb init`" % pub.dir)
            n = keylegs_mod.migrate(pub)
            print("carddb migrate %s: %d rows written" % (args.what, n), file=stdout)
            return 0
        if args.verb == "docs-status":
            na, nb = docsstatus_mod.export(args.db, args.out)
            print("carddb docs-status: %d Level A set(s), %d Level B set(s) -> %s" % (na, nb, args.out), file=stdout)
            return 0
        if args.verb == "docs-sets":
            n = docsstatus_mod.export_sets(args.db, args.out)
            print("carddb docs-sets: %d set(s) with xproof rows -> %s" % (n, args.out), file=stdout)
            return 0
        if args.verb == "ingest" and args.source == "status":
            if not args.status_a or not args.meta:
                raise StoreError("ingest status needs --status-a and --meta")
            pub = Store(PUBLIC, home=args.home, schema=schema)
            if not pub.exists():
                raise StoreError("no public clone at %s: run `carddb init`" % pub.dir)
            changed, n = docsstatus_mod.run(pub, args.status_a, args.status_b, args.meta,
                                            writer=args.writer or "ci", version=args.version)
            print("carddb ingest status: %d set(s) folded into cert_sets (%d rows changed)" % (n, changed), file=stdout)
            return 0
        if args.verb == "ingest":
            if not args.mtgjson:
                raise StoreError("ingest catalogue needs --mtgjson")
            manifests = args.manifests
            if manifests is None:
                manifests = os.path.join(repo_root(), catalogue_mod.MANIFEST_DIR)
            elif manifests == "":
                manifests = None
            summary = catalogue_mod.ingest(
                args.mtgjson, Store(PUBLIC, home=args.home, schema=schema), manifests,
                writer=args.writer or "mtgjson", version=args.version)
            print("carddb ingest catalogue: %d cards, %d printings, %d sets, %d legality rows, %d faces "
                  "(%d rows changed)" % (
                      summary.tables["cards"], summary.tables["printings"], summary.tables["sets"],
                      summary.tables["legality"], summary.tables["faces"], summary.changed), file=stdout)
            if manifests is None:
                print("carddb ingest catalogue: cross-check skipped (no manifest directory)", file=stdout)
            else:
                print("carddb ingest catalogue: cross-check %d manifest set(s): %d mismatch(es); "
                      "manifests unchanged" % (summary.checked, len(summary.mismatches)), file=stdout)
                for line in summary.mismatches:
                    print(line, file=stdout)
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
        if args.verb == "compliance":
            pub = Store(PUBLIC, home=args.home, schema=schema)
            if not pub.exists():
                raise StoreError("no public clone at %s: run `carddb init`" % pub.dir)
            changed, counts = compliance_mod.run(pub, repo=args.repo, writer=args.source or "ci",
                                                 version=args.version, ran=args.measured)
            parts = " ".join("%s:%s=%d" % (e, s, n) for (e, s), n in sorted(counts.items()))
            print("carddb compliance: %d rows changed; %s" % (changed, parts), file=stdout)
            return 0
        if args.verb == "dirac":
            pub = Store(PUBLIC, home=args.home, schema=schema)
            if not pub.exists():
                raise StoreError("no public clone at %s: run `carddb init`" % pub.dir)
            try:
                r = dirac_mod.run_report(pub, repo=args.repo, gorge=args.gorge_dir,
                                         writer=args.source or "ci", version=args.version,
                                         ran=args.measured)
            except dirac_mod.Unavailable as e:
                print("::warning::carddb dirac skipped, the last dirac rows are kept: %s" % e, file=stderr)
                print("carddb dirac: skipped (%s); dirac rows unchanged" % type(e).__name__, file=stdout)
                return 0
            if r.stale:
                print("dirac: snapshot at %s, GORGE_PIN %s: %d rows from the snapshot may be stale"
                      % (r.snapshot_pin, r.pin, r.stale_rows), file=stdout)
            parts = " ".join("%s=%d" % kv for kv in sorted(r.counts.items()))
            print("carddb dirac: %d rows changed; %s; %d evidence rows" % (r.changed, parts, r.evidence),
                  file=stdout)
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
            packed = packed_mib(pub.dir)
            revs["packed_budget_mib"] = str(int(release_mod.BUDGET_MIB))
            if packed is not None:
                revs["packed_mib"] = "%.1f" % packed
            build_mod.build(out, schema, pub.dir, priv.dir if priv else None, committed, args.text, revs)
            print(out, file=stdout)
            return 0
        if args.verb == "inclusion":
            inputs = inclusion_mod.read_input(args.src)
            if args.cards:
                cards = inclusion_mod.read_cards(args.cards)
            else:
                pub = Store(PUBLIC, home=args.home, schema=schema)
                if not pub.exists():
                    raise StoreError("no public clone at %s: run `carddb init`, or pass --cards" % pub.dir)
                cards = pub.cards()
            res = inclusion_mod.rows(inputs, Resolver(cards), args.sample)
            inclusion_mod.write(args.out or inclusion_mod.default_out(), res.rows, schema)
            print(res.summary(args.sample), file=stdout)
            return 0
        if args.verb == "size":
            pub = Store(PUBLIC, home=args.home, schema=schema)
            if not pub.exists():
                raise StoreError("no public clone at %s: run `carddb init`" % pub.dir)
            packed, rows = release_mod.size_rows(pub, args.measured, args.max_pack_mb)
            # The measurement date size_rows chose (--measured, CARDDB_NOW or the
            # data tip's date) is also the writers row's date: one deterministic
            # date per run, not a `measured` from the tip and a `ran` from the
            # wall clock.
            changed = pub.put("_meta/size", rows, writer=args.source, version=args.version,
                              ran=rows[0]["measured"], replace=True)
            print("carddb size: data branch packed %.1f MiB (budget %d MiB); %d row(s) changed" % (
                packed, int(args.max_pack_mb), changed), file=stdout)
            if packed > args.max_pack_mb:
                print("carddb size: packed %.1f MiB exceeds the D7 budget of %d MiB; "
                      "refusing to publish" % (packed, int(args.max_pack_mb)), file=stderr)
                return 1
            return 0
        if args.verb == "release":
            pub = Store(PUBLIC, home=args.home, schema=schema)
            db = args.db or os.path.join(carddb_home(args.home), "carddb.sqlite")
            release_mod.run(pub, db, assets=args.asset, repo=args.repo, gh=args.gh,
                            now=args.now, dry_run=args.dry_run, keep_weeks=args.keep_weeks,
                            budget_mib=args.max_pack_mb, weekly=not args.no_weekly, out=stdout)
            return 0
        if args.verb in ("validate", "boundary"):
            root = args.tree or Store(PUBLIC, home=args.home, schema=schema).dir
            files = list(shard.tree_files(root))
            viol = (shard.validate_tree(schema, args.scope, files) if args.verb == "validate"
                    else boundary_tree(schema, files))
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
    except query_mod.QueryError as e:
        print("carddb %s: %s" % (args.verb, e), file=stderr)
        return e.code
    except (StoreError, SchemaError) as e:
        print("carddb %s: %s" % (args.verb, e), file=stderr)
        return 1 if isinstance(e, GitError) and args.verb == "publish" else 2
    return 2
