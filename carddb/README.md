# carddb

The card data set gorge-compliance publishes (on the `carddb` branch) and the
curated inputs it is built from.

- `schema/VERSION`, `schema/v<N>/<table>.tsv`: the schema. One file per table,
  columns `name type required key visibility doc`. Types: `string int bool date
  list`. Tables whose every column is `private` (text, tags, edhrec, prims,
  values, labels_private, decks, cert_events, cert_runs, rulings) are declared
  so the boundary can name them and are never published. `rulings` holds the
  ruling text (`comment`); its public mirror `ruling_refs` keeps only
  `oracle_id`, `ruling_id`, `published`, `source` and `retrieved`, so a
  diagnosed row can cite `ruling:<ruling_id>` without a word of the ruling
  reaching the public tree.
- `curated/*.tsv`: human-maintained inputs. `game-changers` and `bracket-flags` are
  seeded from the published WotC lists with a `source` URL and `cited` date per row;
  a human reviews a change to them.
- `schema/writers.tsv`: the registered writer ids and their SLA hours (columns `writer
  sla_hours tables doc`). `carddb freshness` compares it with `_meta/writers`; it is
  the one non-declaration the validators allow under `schema/`, and its shape is checked.
- `_meta/size.tsv`: the branch's object-store measurement (`branch_packed_mib`:
  loose objects plus packfiles, so a fresh publish is counted in the run that
  makes it) and the D7 budget (`budget_mib`), written before each publish. The
  same measurement is what the release reports and `freshness` checks.

A shard is `#carddb <table> v<N>`, a column-header line, then rows sorted
bytewise by the key columns with unique keys. A cell is never empty (`-` is
the empty value) and never holds a tab, newline or CR. A table lives at
`<table>.tsv` or under `<table>/`.

```
go run ./cmd/carddb validate -tree carddb       # shards match the schema
go run ./cmd/carddb boundary -tree carddb       # nothing private or Forge-shaped
```

`validate -scope private` accepts the private tables and columns too (a tree
that is never published). Exit status 0 clean, 1 violations, 2 usage/I-O error.
CI (`.github/workflows/carddb.yml`) runs both on PRs touching `carddb/**`, on
pushes to the `carddb` branch, nightly and on dispatch.

## Write path and releases

A push to `carddb` waits out a two-hour quiet period (measured from the push
event; one concurrency group, so a later push cancels the wait), then CI runs
the catalogue ingest (CDB-03) and the compliance status writer (CDB-16) from
`main`, records the branch's object-store size in `_meta/size`, and
`carddb publish` commits the result. The same path runs nightly at 04:23 UTC and
on `workflow_dispatch`; the dispatch input `dry_run` builds and packs the SQLite
but neither publishes the data branch nor touches a release. A schedule or
dispatch dates its rows from the data tip, never the wall clock, so rerunning
the same inputs on another day is byte-identical. A branch over the 300 MiB D7
budget fails the `size` step before `publish`.

The release has two shapes: `carddb-latest` keeps the floating
`carddb.sqlite.zst`, replaced in place, and `carddb-YYYY-MM-DD` keeps one
snapshot per ISO week. Dated releases older than 12 weeks are pruned except the
newest release of each quarter. `carddb-latest.json` travels beside the asset
with the data revision, gorge pin, packed size and sha256; only the latest
release carries it, so a weekly asset cannot masquerade as the latest. Both
releases also carry `carddb-parquet.tar.gz`: one `.parquet` per ordinary table
of the built file (the schema tables, `card_names`, `build_meta`, the `src_*`
sources and the `proven` view; FTS5 and sqlite's own objects are excluded),
plus a `MANIFEST.json` naming each object's rows, columns and sha256. Columns
are typed from the declared SQLite types (INTEGER int64, REAL float64, TEXT
string, BLOB binary) and rows are written in key order, so rerunning the export
on the same build gives the same bytes.

`cmd/carddb/py/parquet_export.py` is the writer. pyarrow is a CI-only
dependency: it is pinned (`pyarrow==21.0.0`) and installed in a throwaway venv
by the workflow step that runs the export, and the exporter — standalone, it
never imports the `carddb` package and is never imported by it — fails loudly
when pyarrow is missing. The carddb tool itself stays stdlib only.

**Branch protection (operator setting).** Protect `carddb` with require linear
history and fast-forward only; the only pushers are the GitHub Actions token
and the publisher deploy key. The workflow refreshes the branch's own copy of
`.github/workflows/carddb.yml` from `main` on every publish, so that copy
cannot drift.

The data derives from MTGJSON (MIT); see NOTICE.

## The tool (Python, stdlib only)

`cmd/carddb/main.py` is the write path and the builder; `go run ./cmd/carddb` stays
the validate/boundary checker CI runs. Run it as `python3 -I cmd/carddb/main.py <verb>`;
the Python API is `import carddb` with `cmd/carddb/py` on the path.

| verb | does |
|---|---|
| `init [--private]` | clone `$CARDDB_HOME/public` (branch `carddb`) or `private` (`carddb-private`), or create the orphan branch when the remote has none |
| `put TABLE [--source ID] [--version V] [--if-absent] [--refresh COLS] < rows.tsv` | flock, merge rows by key into the touched shards, validate + boundary, update `_meta/writers`, `git add`. stdin: a column-name line then rows. A rerun changes no byte. `carddb.put(table, rows, writer=...)` is the API; `replace=True` writes a full snapshot (keys the batch does not name are deleted); `if_absent=True` (CLI `--if-absent`) adds only rows whose key is not stored yet, and `refresh=("col",...)` (`--refresh col,col`) is the exception: a stored row still takes those columns from the input row |
| `ingest catalogue --mtgjson FILE` | MTGJSON AllPrintings (plain or `.json.gz`) -> the `cards`, `printings`, `sets`, `legality` and `faces` tables as a full snapshot, then cross-check every `oracle/compliance/manifests/*.json` set card list (reported, never overwritten). No Oracle text is written: `cards.oracle_sha` is the full sha256 of the normalised text. `--manifests DIR` (empty disables) and `--source ID` override the defaults |
| `publish [--bulk] [--private]` | one commit `carddb: <writers> <n> rows @<gorge12>`, rebase onto the remote branch, push fast-forward; refuses over 50000 changed lines without `--bulk`; no-op when nothing is staged |
| `merge-driver %O %A %B %P` | the git driver `*.tsv merge=carddb` (registered in each clone's config) |
| `build [--private] [--text FILE]` | SQLite from the shards + curated files + committed sources read from this checkout's `HEAD` (`--ref`, or `--src-dir`) |
| `proven [--db FILE] [--format tsv\|json\|count]` | the cards certification has proven (cert level C2 or C3, not excluded), from the clone or a built file's `proven` view |
| `fetch [--private]` | `gh release download carddb-latest` into `$CARDDB_HOME`, and record `carddb-latest.json` (public) or `carddb-private-latest.json` (`--private`) (`published_at`, `unresolved`) for `freshness` |
| `compliance [--repo DIR] [--source ID] [--measured DATE]` | the compliance status writer: XMage manifests (`oracle/compliance/manifests/*.json`) + `FORGE_ORACLE_REF` -> `support` rows (xmage listed/unfinished/lacks_card; forge listed per corpus card), writer `ci` by default; existing `failing` rows (the farm override, CDB-14) are preserved; pass `--measured` (or set `CARDDB_NOW`) in CI for byte-identical reruns |
| `size [--measured DATE] [--max-pack-mb 300]` | record the branch's object store (loose objects + packfiles) in `_meta/size` (`branch_packed_mib`, `budget_mib`) for the D7 budget; prints the same number `freshness` checks, and exits non-zero before `publish` when it exceeds the budget |
| `release [--dry-run] [--asset FILE ...] [--now DATE]` | upload the built asset to `carddb-latest` (`--clobber`) with `carddb-latest.json`, create/refresh this ISO week's `carddb-YYYY-MM-DD` release with the asset only, prune dated releases older than 12 weeks except the newest per quarter; refuses to upload over the D7 budget and fails loudly if it cannot list the releases; `--dry-run` prints every mutating `gh` command and uploads nothing |
| `validate`, `boundary` | the Python mirror of the Go checks (a parity test keeps them in step) |
| `card <name\|oracle_id>` | every fact for one card: identity, printings, legality, status/evidence, cert, labels and relations (+ the private overlay's tables when the build has them) |
| `search <fts> [--legal F] [--status K=V] [--set S] [--label L] [--marked]` | FTS5 over the built file, with legality/status/set/label filters |
| `blockers --format F [--engine E] [--by printings\|decks]` | legal cards that are not covered, ranked by printings (public default) or real-deck counts (private default); digital and non-tournament-only cards are skipped |
| `proven [--format F] [--level C2]` | cards whose `cert` level is at least the requested level (the cert-pinned proven view) |
| `deck <id>` | a private deck's cards |
| `freshness [--tick]` | writer SLAs from `schema/writers.tsv`, publish age, `carddb-latest` age, unresolved growth and the data branch's object-store size; `--tick` prints only `!!` lines |
| `sql '<select>'` | read-only SQL (the file is opened `mode=ro` + `query_only`; only SELECT/WITH/EXPLAIN is accepted) |
| `export <view> --tsv` | deterministic TSV naming the view and asset version (schema + gorge pin) |

Read verbs take `--db FILE` (default `$CARDDB_HOME/carddb.sqlite`, or
`carddb-private.sqlite` with `--private`) and open the file read-only. `freshness`
also accepts `--asset-meta FILE` (default `$CARDDB_HOME/carddb-latest.json`, or `carddb-private-latest.json` with `--private`; keys
`published_at` and optional `unresolved`), `--sla FILE`, `--now` and
`--public-dir`.

Catalogue rows: `cards` holds one row per `oracle_id` and the newest printing's
mana cost, type line, colours and keywords; `legality` is one row per
(oracle_id, format) except `not_legal`; `faces` holds the multi-face
cost/type/P-T only (no Oracle text); `printings` holds one row per physical
printing (the front-face MTGJSON uuid); `sets` carries the release date used by
`first_released`/`first_set`, and `paper` says whether any printing's
`availability` includes `paper`. A full ingest removes a stale row when a
format turns `not_legal` or a printing disappears. A meld set is three cards, not
two two-faced ones: each component and the meld result (the `side: b` entry, e.g.
Brisela, Voice of Nightmares) is its own `cards` row and its own printing, and none
of them has a `faces` row; a transform or MDFC back face is a face, never a card.

`carddb/curated/game-changers.tsv` and `bracket-flags.tsv` carry the WotC URL
and date per row (`source`, `cited`) and are seeded from the published list;
a human reviews a change to them before it merges. The citation is data, not
a live fetch: bump it with the list.

Env: `CARDDB_HOME` (default `~/.local/share/carddb`), `CARDDB_PUBLIC_REMOTE` (default the
GitHub repo), `CARDDB_PRIVATE_REMOTE` (four-yay, required for the private store),
`CARDDB_WRITER`, `CARDDB_WRITER_VERSION`. Tests: `make carddb-test`.

Shard paths: a table keyed on `oracle_id` shards on its first two hex digits (`x:<sha1>`
pseudo ids on `x` + two digits), `printings` by set, every other table is one file. A key lives in
one shard only: a `put` that re-homes a printing (another `set_code`) removes the old copy and deletes
a shard it empties, and `put`, `publish` and `build` all refuse a tree whose table holds a key in two
shards (the Go validator's rule, checked across the whole table, not per file).

`status`, `cert`, `cert_sets` and `evidence` carry a `writer` column: `put` stamps every row that has
none with its `--source`/`writer=`, and a rerun by a lower-priority writer does not overwrite a row a
higher one wrote with the same data.

Merge rule (a conflict never reaches a human): per key against the common ancestor; a key both
sides changed differently resolves by the newer timestamp column (`measured_at`, else
`measured`, `declared`, `ran`, `retrieved`: the first the table has), then the writer priority
(`ci` > `cert` > `loop`, from the row's `writer` column), then the greater row
bytes, so both clones converge. A modify beats a delete. The merged file is re-validated.

`cert` is the certification job's per-card current row (one row per `oracle_id`, named by
`norm_name`): the level C0..C3, any exclusion, the agreeing `refs`, the `keys_*` and
`triggers_unmeasured` counts, `scen_level`, `oracle_audit`, `open_deltas`,
`diagnosed_deltas`, `tickets`, `why` and `witness` (`k=v;` entries). `cert_sets` is the
job's per-set row. Both carry the `gorge_sha` and `corpus_pin` they were measured at and a
`measured` date; git history is the pin history. `cert_sets` also carries the docs site's per-set status (CDB-37), written by the `ci`
writer's `carddb ingest status` from `oraclediff status -all -json` output: `printed`, the Level A
columns `a_*` and Level B columns `b_*` (counts, `bucket=n;` problem buckets, declared claims) and the
`status_*` stamps (date, compliance commit, gorge pin, FORGE_REF, `ratchet_failures`). A set the
certification job has not written gets a minimal `queued` row (`paper`/`playable` from the status; the
`new_paper`, `reprints`, `excluded`, `c0`..`c3` and `open` counts are empty); a certified set keeps its
certification columns and gains only the status ones. `put` replaces a whole row, so the certification
writer must read the row back and carry the `a_*`, `b_*`, `printed` and `status_*` columns it did not
measure. A set a later measurement no longer covers has its status columns cleared (and a `queued` row only the status writer made is
deleted), so the export is a snapshot of the newest measurement. `carddb docs-status --db carddb.sqlite --out DIR` turns the built asset back into the
`status-A.jsonl`, `status-B.jsonl` and `meta.json` shapes gorge's `scripts/docs_sets.py` reads; this
replaces the force-pushed `docs-data` branch, which CI keeps writing beside the fold until a gorge docs build has gone green on the
asset (CDB-37b), then drops. `cert_events` and `cert_runs` are
private-only (`--private`) partitions for the job's exact event and run counts.

`validate` and `boundary` (this tool's Python verbs, and the Go ones) walk every file but `.git`:
a shard behind another extension is refused, `schema/` may hold only the schema's own declarations
plus the fixed-shape `schema/writers.tsv` config.

The SQLite file: one table per schema table (`/` and `-` become `_`), `card_names`
(norm_name -> oracle_id), `card_fts` (FTS5 over name, type_line, keywords, labels; plus Oracle
text with `--text`, which writes `carddb-text.sqlite`, never the published file),
`src_manifest_*` and `src_diagnosed` (the committed sources), `proven` (the `cert` rows at
C2/C3 with no exclusion) and `build_meta`. The FTS `keywords`
column is `cards.keywords`; a `--ref` that lacks one of the committed source paths
just has none of those inputs.
