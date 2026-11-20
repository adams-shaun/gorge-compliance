# carddb

The card data set gorge-compliance publishes (on the `carddb` branch) and the
curated inputs it is built from.

- `schema/VERSION`, `schema/v<N>/<table>.tsv`: the schema. One file per table,
  columns `name type required key visibility doc`. Types: `string int bool date
  list`. Tables whose every column is `private` (text, tags, edhrec, prims,
  values, labels_private, decks, cert_events, cert_runs) are declared so the
  boundary can name them and are never published.
- `curated/*.tsv`: human-maintained inputs (header only until populated).

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
CI (`.github/workflows/carddb.yml`) runs both on PRs touching `carddb/**` and on
pushes to the `carddb` branch.

The data derives from MTGJSON (MIT); see NOTICE.

## The tool (Python, stdlib only)

`cmd/carddb/main.py` is the write path and the builder; `go run ./cmd/carddb` stays
the validate/boundary checker CI runs. Run it as `python3 -I cmd/carddb/main.py <verb>`;
the Python API is `import carddb` with `cmd/carddb/py` on the path.

| verb | does |
|---|---|
| `init [--private]` | clone `$CARDDB_HOME/public` (branch `carddb`) or `private` (`carddb-private`), or create the orphan branch when the remote has none |
| `put TABLE [--source ID] [--version V] < rows.tsv` | flock, merge rows by key into the touched shards, validate + boundary, update `_meta/writers`, `git add`. stdin: a column-name line then rows. A rerun changes no byte. `carddb.put(table, rows, writer=...)` is the API |
| `publish [--bulk] [--private]` | one commit `carddb: <writers> <n> rows @<gorge12>`, rebase onto the remote branch, push fast-forward; refuses over 50000 changed lines without `--bulk`; no-op when nothing is staged |
| `merge-driver %O %A %B %P` | the git driver `*.tsv merge=carddb` (registered in each clone's config) |
| `build [--private] [--text FILE]` | SQLite from the shards + curated files + committed sources read from `origin/main` (`--ref`, or `--src-dir`) |
| `proven [--db FILE] [--format tsv\|json\|count]` | the cards certification has proven (cert level C2 or C3, not excluded), from the clone or a built file's `proven` view |
| `fetch [--private]` | `gh release download carddb-latest` into `$CARDDB_HOME` |
| `compliance [--repo DIR] [--source ID]` | the compliance status writer: XMage manifests (`oracle/compliance/manifests/*.json`) + `FORGE_ORACLE_REF` -> `support` rows (xmage listed/unfinished/lacks_card; forge listed per corpus card), writer `ci` by default |
| `validate`, `boundary` | the Python mirror of the Go checks (a parity test keeps them in step) |

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
`measured` date; git history is the pin history. `cert_events` and `cert_runs` are
private-only (`--private`) partitions for the job's exact event and run counts.

`validate` and `boundary` (this tool's Python verbs, and the Go ones) walk every file but `.git`:
a shard behind another extension is refused, `schema/` may hold only the schema's own declarations.

The SQLite file: one table per schema table (`/` and `-` become `_`), `card_names`
(norm_name -> oracle_id), `card_fts` (FTS5 over name, type_line, keywords, labels; plus Oracle
text with `--text`, which writes `carddb-text.sqlite`, never the published file),
`src_manifest_*` and `src_diagnosed` (the committed sources), `proven` (the `cert` rows at
C2/C3 with no exclusion) and `build_meta`. The FTS `keywords`
column is `cards.keywords`; a `--ref` that lacks one of the committed source paths (origin/main
before the curated tree was promoted) just has none of those inputs.
