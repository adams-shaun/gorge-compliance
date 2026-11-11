# gorge-compliance

Evidence for how far to trust [gorge](https://github.com/adams-shaun/gorge), the
pure-Go *Magic: The Gathering* rules engine. This repository checks gorge, card by
card, against independent references, and holds the results to committed targets for
one pinned gorge commit. Read it to judge gorge's rules, to add a scenario, to run an
oracle or to check one card.

gorge never imports this repository and nothing here changes how gorge plays. If you
only want to embed or play gorge, you don't need it.

## How much to trust gorge

gorge makes a claim per card set, at one of two levels:

| Level | What the claim says | What it leaves out |
|---|---|---|
| **A** | Every card in the set is supported, and one generated scenario (cast and resolve, play a land, or counter a spell) agrees with XMage, or, where XMage has not compared it, with Forge (a Forge-only verdict), or a hand scenario passes, or the disagreement has a ruling citing the rules | Activated abilities, triggers, statics, attacks and blocks |
| **B** | Level A, plus one generated scenario per activated ability, trigger, static and attack or block, each compared with XMage | Anything no scenario template can express yet, which is reported as a gap, not passed. Interactions between cards |

A Forge-only verdict checks gorge's reading of Forge's own card script, not the script, so a wrong
script both engines read alike passes; `oraclediff status` reports XMage-verified and Forge-only cards
separately, and an XMage comparison replaces a Forge-only verdict.

Neither level means gorge is right about a whole game. The claims rest on short
scripted scenarios, and two engines can share a bug. [docs/oracles.md](docs/oracles.md)
says what each check sees and what it can't.

`oracle/compliance/declared.json` holds the claims. Measured on 2026-11-02 at gorge
`5224a59f2` with `make compliance-status`: 20 sets are declared, 17 at level B and 3 at
level A. Those are the sets of the Standard target, whose 5331 entries have no
outstanding problem at level A. Across every set XMage knows, 59672 of 73159 entries are
outstanding, nearly all because no scenario has been replayed for them yet, so a card
outside the declared sets usually has no XMage verdict.

The current numbers per set are on
[gorge's support page](https://adams-shaun.github.io/gorge/support/sets/), built from
this repository's nightly CI.

## Setup

You need Go (the version in `go.mod`), git and make, and about 200 MB for the corpus.

```sh
git clone https://github.com/adams-shaun/gorge.git
git clone https://github.com/adams-shaun/gorge-compliance.git
git -C gorge checkout "$(cat gorge-compliance/GORGE_PIN)"
make -C gorge fetch-cards compile-cards    # the Forge card scripts, into gorge/.cards
cd gorge-compliance
make cards                                 # links ../gorge/.cards here as .cards
make boundary oracle-audit ORACLE_RUN=Fatal_Push
```

The last line should end with `ok` for both packages.

`make cards` looks for a gorge checkout at `../gorge`. For a checkout elsewhere, run
`make cards GORGE_ROOT=/path/to/gorge`. It fails, saying what to do, when there is no
checkout or the checkout has no corpus, and warns when the corpus isn't at the commit the
pinned gorge expects. Go gets gorge's code from `go.mod`, not from this checkout.

Without `.cards`, every corpus-dependent test skips and the package still prints `ok` in
milliseconds. CI refuses to run that way, and so should you.

To build against another gorge checkout, write a gitignored `go.work`:

```
go 1.25.8

use (
	.
	../gorge
)
```

## Check a card

```sh
# Is the card's set declared, and is anything outstanding?
go run ./cmd/oraclediff status -set FDN -level B

# XMage's verdict on each of the card's scenarios (files are by first letter).
grep '"card":"Llanowar Elves"' oracle/compliance/verdicts/l.jsonl

# A hand scenario, if the card has one, and running it.
ls scenarios/oracle/*/fatal-push.json
make oracle-audit ORACLE_RUN=Fatal_Push

# gorge's compiled definition beside mtgish's (needs make mtgish-fetch).
go run ./cmd/mtgishxc -card "Fatal Push"

# What gorge does on each generated scenario of a set, step by step.
go run ./cmd/oraclediff gen -level B -manifest oracle/compliance/manifests/BIG.json -out /tmp/big.jsonl
go run ./cmd/oraclediff show -scenarios /tmp/big.jsonl -card "Ancient Cornucopia"
```

A verdict's `status` is one of `agree`, `gorge_wrong`, `xmage_wrong` (with a `ruling`
citing the rules), `diverge` (not triaged yet), `harness` (a driver couldn't express the
scenario) or `xmage_lacks`. Only `agree` and `xmage_wrong` count toward a claim, and a hand
scenario stands in for a card XMage doesn't have.

To add a scenario, see [scenarios/README.md](scenarios/README.md).

## Commands

| Command | What it does | Needs |
|---|---|---|
| `make test` | The whole suite, `go test ./...`, as CI runs it with a 30-minute limit | corpus, and mtgish data for its ledger |
| `make boundary` | The licence guards | |
| `make oracle-audit [ORACLE_RUN=Card_Name]` | Both scenario corpora, verbose | corpus |
| `make smoke` | The script smokes, with stubs for Java | |
| `make compliance-status` | Per-set and per-format status in `oracle/compliance/status.md`, and the ratchet check; about 3 minutes | corpus |
| `make compliance-sets` | Both levels over every set with a printed list, in `.coverage/compliance-sets.md` | corpus |
| `make mtgish-fetch`, `make mtgish-xc` | Fetch the pinned mtgish data; run the definition cross-check | corpus |
| `make xmage-oracle-setup`, `make forge-oracle-setup` | Build XMage or Forge out of tree | [docs/oracles.md](docs/oracles.md#what-the-xmage-and-forge-oracles-need) |
| `make xmage-driver-tests` | The Java driver's unit tests | XMage build |
| `make compliance-pass SETS='FRA BLB'` | Replay stale scenarios in XMage, update verdicts | XMage build |
| `make xengine-sweep SETS=FDN CHUNKS=1 GAMES=2` | Full-game differential sweep | both builds |
| `make pr DRY=1` | Print the pull request `make pr` would open | `gh` |

## Layout

| Path | What |
|---|---|
| `audit/` | Test drivers for the two scenario corpora |
| `scenarios/` | The hand scenarios and XMage test ports ([README](scenarios/README.md)) |
| `oracle/compliance/` | Per-set manifests, verdicts, declared levels, the gate, certification, scenario generation, shape rulings and triage |
| `oracle/xmageoracle/` | The Java XMage replay driver and its unit tests |
| `oracle/xengine/`, `cmd/xengine` | The full-game differential harness |
| `oracle/mtgish/`, `cmd/mtgishxc` | The card-definition cross-check |
| `oracle/validation/oracle/` | [Runbook](oracle/validation/oracle/README.md) for refreshing verdicts |
| `cmd/` | `oraclediff` (generate, diff, triage, status), `compliance` (manifests), `xmageport`, `oraclepacket` (the printed card, for scenario authors) |
| `scripts/`, `ops/scripts/tests/` | Oracle setup and run scripts; their smokes |
| `internal/` | `gorgedir` (locates the gorge module), `boundary` (the licence guards) |
| `docs/` | [The oracles](docs/oracles.md), [the pin](docs/PINNING.md), [the bug hunter](docs/hunt.md), [capacity stalls](docs/capacity.md), [old designs](docs/design/README.md) |

## Relationship to gorge

This repository uses gorge's public packages only, chiefly the oracle runner `engine/rules`
exports (`RunOracleScenarioJSON`, `RunOracleAuditScenarioJSON`,
`CheckOracleScenarioJSON`). gorge keeps the runner, its unit tests and a few residual
scenario files as the runner's contract check; the audit here reads those files together
with `scenarios/oracle`. A gorge change that moves a card's behaviour shows up here as a
stale row at the next [pin bump](docs/PINNING.md), or earlier in the nightly drift report.

## Licence

Apache-2.0 (`LICENSE`). Forge's card scripts, token scripts and other artifacts are
GPL-3.0 and are never committed here: the corpus stays in gorge's gitignored `.cards/`,
and `internal/boundary` fails the build if any of it is tracked or one `git add` away.
Oracle text isn't committed either; scenarios name cards only. `NOTICE` credits XMage (MIT),
whose set classes and tests the manifests and ports derive from, and mtgish (MIT), whose
data is read at run time from the gitignored `.mtgish/`.
