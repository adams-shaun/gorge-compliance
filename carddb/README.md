# carddb

The card data set gorge-compliance publishes (on the `carddb` branch) and the
curated inputs it is built from.

- `schema/VERSION`, `schema/v<N>/<table>.tsv`: the schema. One file per table,
  columns `name type required key visibility doc`. Types: `string int bool date
  list`. Tables whose every column is `private` (text, tags, edhrec, prims,
  values, labels_private, decks) are declared so the boundary can name them and
  are never published.
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
