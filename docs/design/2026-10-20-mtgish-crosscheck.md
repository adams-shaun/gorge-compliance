# mtgish card-definition cross-check

Status: implemented on branch `wt/mtgish-xc`, 2026-10-20. Code: `oracle/mtgish/`,
`cmd/mtgishxc/`. Ledgers: `oracle/mtgish/known/`.

## What it is

The three game-level oracles (hand Oracle-text scenarios, XMage, Forge) find a
wrong card script only when a scenario exercises it. This check works at the
**definition** level instead and needs no game: it compares gorge's compiled
card IR (Forge scripts compiled by gorge's `engine/cards`) with
[mtgish](https://github.com/i5jb/mtgish) (MIT), an independent typed encoding of
the same cards parsed from Oracle text. Wherever the two encodings of a card
disagree, one of them, or the comparison, is wrong. That makes it a fourth
independent source, and a cheap one: the whole corpus compares in about two
seconds.

The first consumers are the sets the dirty-dirac kernel plays, FDN and BLB; they
carry a ledger. The other sets are reported with `-all` but not ratcheted.

## How

Both sides are normalised to one `Summary` per card face, made only of short
canonical tokens (never card or script text), in four tiers:

| Tier | Field(s) | Tokens |
|---|---|---|
| T1 characteristics | `mana_cost`, `types`, `pt`, `loyalty`, `keywords` | `{2}{W}{G/W}`; `creature drake`; `2/2`, `*/4`; `flying ward:{2} enchant:creature` (cheap parameters only: costs, counts) |
| T2 ability shape | `abilities` | one per printed non-keyword ability: `static`, `activated`, `spell`, `replacement`, `pregame`, `triggered:<event>` (`etb`, `dies`, `attacks`, `upkeep`, `combat-damage-player`, ...) |
| T3 targets | `targets` | per targeting ability, its slots: kind class (`creature`, `permanent`, `player`, `spell`, `any`, `card-graveyard`, alternatives joined by `\|`) and count (`x0..1`, `x2`; exactly one has no suffix) |
| T4 effects | `effects` | effect verbs: `damage:3`, `destroy`, `move:battlefield>exile`, `draw:1`, `counters:+1/+1:2`, `pump:+2/+0`, `grant:flying`, `token:1`, `search:hand`, ... |

Fields compare as multisets, so ability order never matters. Amounts are
compared only where both sides give a literal: a non-literal amount (`*`, an X,
a count) matches any amount of the same verb, and a non-literal target count is
not compared. Alternatives of which exactly one runs (modes, kicked or not, gift
promised or not, if/else) merge branch-max on both sides; a gift or kicker
spell's script is summarised once per branch (`Count$PromisedGift.A.B`,
`Count$Kicked.A.B` and the `PromisedGift`/`Kicked` conditions resolved) and
merged the same way.

The mtgish side decodes `mtgish.lines.json` generically (`oracle/mtgish/node.go`):
mtgish's JSON is tagged unions (`{"_Action":"DrawACard"}`) over about 7,300
variants, and only the frequent ones are named in the normaliser. The gorge side
reads the compiled `cards.Face`: printed keywords, abilities, triggers, statics
and replacements, skipping keyword expansions (tagged `KeywordLine`/`Keyword`)
except the ones that are printed abilities (Class levels, "as this enters"
choices and counters, prevention shields).

Anything either normaliser cannot map becomes an explicit `unmapped:<name>`
token in the field it would have filled. A row involving one is a **normaliser
gap**, counted apart from disagreements and never dropped.

## Classes

Every difference in a ledgered set is triaged into one class:

| Class | Meaning |
|---|---|
| `forge-bug` | the Forge script does not do what the printed card says |
| `gorge-compile` | the script is right and gorge's compile of it is wrong |
| `mtgish-bug` | mtgish's encoding does not match the printed card |
| `normalizer` | a mapping debt in this check (fix it rather than ledger it) |
| `equivalent` | both encodings are right and the normalisers cannot see it (a cost on one side is an effect on the other, one static per type against one per sentence, ...) |

## The pin

`MTGISH_REF` in the Makefile and `oracle/mtgish/MTGISH.lock` (ref plus the
sha256 of each file) pin the data. `make mtgish-fetch` downloads
`data/mtgish.lines.json` and `LICENSE` at that ref from
raw.githubusercontent.com into `.mtgish/<ref>/` and verifies them. The data is
derived from Oracle text (Wizards' copyright), so it is gitignored and never
committed; `internal/boundary` fails on a `.mtgish/` path, an `mtgish.lines.*`
file or a copied card record. NOTICE carries mtgish's MIT licence. Bumping the
pin is a change to both the Makefile and the lock, then a ledger refresh.

The gorge side is the `GORGE_PIN` revision like every other target here.

## The ratchet

`oracle/mtgish/known/<SET>.tsv` (FDN, BLB) lists each live difference: card,
tier, field, the gorge-only and the mtgish-only tokens, class, note.
`TestMtgishLedger` holds the live diff equal to the ledger in both directions:
a new difference fails (fix the normaliser or triage it), a ledger row that no
longer differs fails as stale (delete it). It is the shape of gorge's
`TestEveryRepoDeckIsFullySupported`. Without `.cards` or the fetched data it
skips with a message saying the ratchet did not run.

```sh
make mtgish-fetch                               # once per pin
make mtgish-xc                                  # FDN+BLB report + the ratchet
go run ./cmd/mtgishxc -card "Prayer of Binding" # both summaries, side by side
go run ./cmd/mtgishxc -set BLB -tier T3         # one tier's rows
go run ./cmd/mtgishxc -set FDN -write-ledger    # refresh a ledger, keeping triage
go run ./cmd/mtgishxc -all                      # the whole corpus, report only
```

Reports go to the gitignored `.coverage/mtgish/`.

## First results (2026-10-20, gorge `be95bda94`, mtgish `153451e4`)

- FDN, 517 cards: T1 100%, T2 99.8%, T3 99.4%, T4 98.6% of faces agree. 11 rows:
  1 `forge-bug` (Prayer of Binding: the card's exile target is optional, the
  script's is required), 3 `mtgish-bug`, 7 `equivalent`.
- BLB, 278 cards: T1 100%, T2 100%, T3 99.3%, T4 97.1%. 10 rows: 4 `mtgish-bug`,
  6 `equivalent`.
- mtgish's most common error in these sets reuses an earlier number in the
  sentence ("deals 3 damage ... you gain 2 life" read as gain 3), and drops an
  "up to" on a second target.
- The whole corpus (`-all`, 33,442 cards): T1 96.6%, T2 93.1%, T3 98.4%, T4 76.4%.
  Most T4 differences there are normaliser gaps on the long tail of mtgish
  variants. The same "up to one target" scan finds four more cards whose script
  requires a target the card makes optional.
