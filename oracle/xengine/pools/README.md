# xengine deck pools

Card-name lists for `xengine play -pool FILE` (and `XENGINE_POOL` in
`scripts/xengine-sweep.sh`). A pool replaces the deck source: the decks are
drawn from its cards alone, through the same filter as a set (gorge fully
supports the card, basics left out; with `-xmage`, no card XMage spells other
than the corpus) and the same 23-spell, 17-basic, two-colour pair builder,
weighted against the ledger as `-set` is. `-set` still names the run in the
item ids. `-pool` cannot be combined with `-random`, `-forge-only` or `-dirac`
(each picks the cards another way; a dirac table holds one set, a pool is not
one), and play says so. `-pool-filter FILE` drops the names it lists (cards a
follower refuses); a filter name the corpus does not know is reported, not an
error, since one filter may serve several pools.

Each run writes `<out>/pool.json`: the pool's and filter's path and sha256,
and how many names each step left out.

## Format

One card name per line, in the corpus (Forge) spelling gorge uses, the front
face of a multi-face card. Everything from `#` on is a comment; blank lines are
skipped. A name the corpus does not know is an error that lists every such
name: a pool is committed, and a misspelling must not shrink it in silence.
Names only: never Oracle text.

## Pools

| File | Source | Names | After the gorge filter (corpus pin of this commit) |
|---|---|---|---|
| `fdn.txt` | the FDN manifest (`oracle/compliance/manifests/FDN.json`), minus unfinished: exactly what `-set FDN` draws | 517 | 512 (the 5 basics) |
| `fdn-commons.txt` | `fdn.txt` ∩ MTGJSON `legalities.pauper == "Legal"` | 196 | 191 (the 5 basics) |

`TestPoolFDNMatchesSet` checks `-pool fdn.txt` deals the same candidates and
decks as `-set FDN`, with and without `-xmage`; `TestPoolFiles` that `fdn.txt`
is current against the manifest.

### Regenerate

```
go run ./cmd/xengine pool -set FDN -out oracle/xengine/pools/fdn.txt
curl -sSfLO https://mtgjson.com/api/v5/FDN.json.gz     # outside the repo: it carries Oracle text
go run ./cmd/xengine pool -set FDN -legal pauper -mtgjson FDN.json.gz -out oracle/xengine/pools/fdn-commons.txt
```

`fdn-commons.txt` was cut from MTGJSON 5.3.0+20261110 (`FDN.json.gz`, sha256
in the file's header). The repository holds no legality data of its own (the
manifests' rarity is the FDN printing's, and the union of every manifest's
commons, intersected with FDN, gives 191 names, not 196: a set's XMage class
is not Pauper's list of sanctioned printings), so the MTGJSON set file is the
source. It is downloaded, never committed.

### Deferred: `pauper.txt`

The whole Pauper-legal pool (~11k names before the gorge filter) is deferred:
it needs MTGJSON's AtomicCards (or every set file) rather than one set file,
and a generator mode over it. `xengine pool` reads one set's manifest today.
