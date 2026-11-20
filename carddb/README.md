# carddb

The card data set gorge-compliance publishes (on the `carddb` branch) and the
curated inputs it is built from.

- `schema/VERSION`, `schema/v<N>/<table>.tsv`: the schema. One file per table,
  columns `name type required key visibility doc`. Types: `string int bool date
  list`. Tables whose every column is `private` (text, tags, edhrec, prims,
  values, labels_private, decks) are declared so the boundary can name them and
  are never published.
- `curated/*.tsv`: human-maintained inputs. `game-changers` and `bracket-flags` are
  seeded from the published WotC lists with a `source` URL and `cited` date per row;
  a human reviews a change to them.

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
| `put TABLE [--source ID] [--version V] < rows.tsv` | flock, merge rows by key into the touched shards, validate + boundary, update `_meta/writers`, `git add`. stdin: a column-name line then rows. A rerun changes no byte. `carddb.put(table, rows, writer=...)` is the API; `replace=True` writes a full snapshot (keys the batch does not name are deleted) |
| `ingest catalogue --mtgjson FILE` | MTGJSON AllPrintings (plain or `.json.gz`) -> the `cards`, `printings`, `sets`, `legality` and `faces` tables as a full snapshot, then cross-check every `oracle/compliance/manifests/*.json` set card list (reported, never overwritten). No Oracle text is written: `cards.oracle_sha` is the full sha256 of the normalised text. `--manifests DIR` (empty disables) and `--source ID` override the defaults |
| `publish [--bulk] [--private]` | one commit `carddb: <writers> <n> rows @<gorge12>`, rebase onto the remote branch, push fast-forward; refuses over 50000 changed lines without `--bulk`; no-op when nothing is staged |
| `merge-driver %O %A %B %P` | the git driver `*.tsv merge=carddb` (registered in each clone's config) |
| `build [--private] [--text FILE]` | SQLite from the shards + curated files + committed sources read from this checkout's `HEAD` (`--ref`, or `--src-dir`) |
| `fetch [--private]` | `gh release download carddb-latest` into `$CARDDB_HOME` |
| `validate`, `boundary` | the Python mirror of the Go checks (a parity test keeps them in step) |

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

`validate` and `boundary` (this tool's Python verbs, and the Go ones) walk every file but `.git`:
a shard behind another extension is refused, `schema/` may hold only the schema's own declarations.

The SQLite file: one table per schema table (`/` and `-` become `_`), `card_names`
(norm_name -> oracle_id), `card_fts` (FTS5 over name, type_line, keywords, labels; plus Oracle
text with `--text`, which writes `carddb-text.sqlite`, never the published file),
`src_manifest_*` and `src_diagnosed` (the committed sources) and `build_meta`. The FTS `keywords`
column is `cards.keywords`; a `--ref` that lacks one of the committed source paths
just has none of those inputs.
