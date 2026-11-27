# xengine deck pools

Card-name lists for `xengine play -pool FILE` (and `XENGINE_POOL` in
`scripts/xengine-sweep.sh`). A pool replaces the deck source: the decks are
drawn from its cards alone, through the same filter as a set (gorge fully
supports the card, basics left out; with `-xmage`, no card XMage spells other
than the corpus) and the same 23-spell, 17-basic builder, weighted against the
ledger as `-set` is. Without an identity header the builder rotates the 10
colour pairs (and the triples a three-colour candidate needs), as `-set` does;
with one (below) every deck is that identity alone. `-set` still names the run
in the item ids. `-pool` cannot be combined with `-random`, `-forge-only` or
`-dirac` (each picks the cards another way; a dirac table holds one set, a
pool is not one), and play says so. `-pool-filter FILE` drops the names it
lists (cards a follower refuses); a filter name the corpus does not know is
reported, not an error, since one filter may serve several pools.

Each run writes `<out>/pool.json`: the pool's and filter's path and sha256,
the pool's identity when it has one, and how many names each step left out.

## Format

One card name per line, in the corpus (Forge) spelling gorge uses, the front
face of a multi-face card. Everything from `#` on is a comment; blank lines are
skipped. A name the corpus does not know is an error that lists every such
name: a pool is committed, and a misspelling must not shrink it in silence.
Names only: never Oracle text.

A pool may carry suppq.py's identity header:

    # supp <IDENTITY> tier=<t> targets=<n>

`<IDENTITY>` is one to five distinct WUBRG letters in WUBRG order (`WU`,
`WUB`, ..., `WUBRG`). Every deck is then that identity: its spells fit the
identity and its 17 basics are its colours (three or more colours split
evenly). A pool name whose gorge colour identity does not fit the header is
left out and counted in `pool.json`, not dealt: suppq derives the header from
the Forge script by approximation and gorge computes its own. A pool with no
`# supp` header keeps the set builder's rotation.

## Pools

| File | Source | Names | After the gorge filter (corpus pin of this commit) |
|---|---|---|---|
| `fdn.txt` | the FDN manifest (`oracle/compliance/manifests/FDN.json`), minus unfinished: exactly what `-set FDN` draws | 517 | 512 (the 5 basics) |
| `fdn-commons.txt` | `fdn.txt` ∩ MTGJSON `legalities.pauper == "Legal"` | 196 | 191 (the 5 basics) |
| `mana-abilities.txt` | the nonland mana sources follower-check must activate (Torgal FIN, Sol Talisman MH2, Riven Turnbull/Princess Lucrezia LEG, The Alright Henge no committed set) | 5 | 5 |

`TestPoolFDNMatchesSet` checks `-pool fdn.txt` deals the same candidates and
decks as `-set FDN`, with and without `-xmage`; `TestPoolFiles` that `fdn.txt`
is current against the manifest.

### Regenerate

```
go run ./cmd/xengine pool -set FDN -out oracle/xengine/pools/fdn.txt
curl -sSfLO https://mtgjson.com/api/v5/FDN.json.gz     # outside the repo: it carries Oracle text
go run ./cmd/xengine pool -set FDN -legal pauper -mtgjson FDN.json.gz -out oracle/xengine/pools/fdn-commons.txt
```

`mana-abilities.txt` is the follower-check mana gate's pool: its first line, `#!require-mana`, is a comment to every other reader and puts `scripts/follower-check.sh ENGINE --pool oracle/xengine/pools/mana-abilities.txt` in mana-gate mode (the same as `--require-mana` on any pool). It reads the pool without `--set`, builds a card source from the names, and appends each card's level-A mana-ability activation item. The Alright Henge is in no committed set manifest, which is why the pool (not `--set`) is the card source. In that mode the gate refuses (exit 2) and names every pool name whose `activate-mana` item was not generated (a misspelling, a mana ability the activate family cannot build) or not sent (the follower's preflight refused it, or `--limit` cut it), so none of them can shrink the gate in silence. A basic land is not a valid pool name here: its intrinsic mana ability is engine-provided and has no Forge requirement to activate. A pool without the directive (`fdn-commons.txt`) is a plain card filter: `--pool` keeps the items of the named cards and nothing else changes.

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
