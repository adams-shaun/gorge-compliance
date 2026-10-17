# gorge-compliance

Compliance oracles and scenario audits for
[gorge](https://github.com/adams-shaun/gorge), the pure-Go Magic: The
Gathering rules engine.

This repository exists to give confidence in gorge's rules engine. It is of
no functional use to someone embedding gorge: **gorge never imports it**, and
nothing here changes how gorge plays. What it does is check gorge, card by
card, against independent references, and hold the results to coverage
targets for one **pinned** gorge revision.

It also takes the compliance test load out of gorge: gorge's own suite keeps
the oracle runner and its unit tests, and this repository carries the large
scenario corpora, the generated XMage/Forge pipelines and their gates.

## The three-way oracle

For a card gorge claims to support, three independent sources say what it
does:

| Source | What it is | Where |
|---|---|---|
| **Hand-authored Oracle-text scenarios** | Written from the card's printed (Oracle) text and the Comprehensive Rules by an author who never read gorge's card script; they assert observable state (zones, life, P/T, counters, the stack, what is offered) | `scenarios/oracle/` (+ gorge's residual `engine/rules/testdata/oracle/`), run by `audit/` |
| **XMage** | An independent Java engine (MIT). Scenarios are generated per card (`oraclediff gen`), replayed in XMage by a driver (`oracle/xmageoracle/`), and the two engines' checkpoint snapshots are diffed into per-scenario verdicts | `oracle/compliance/` (manifests, verdicts, gate, ratchet), `scripts/xmage-oracle-*`, `scripts/compliance-pass.sh` |
| **Forge** | The engine gorge's card scripts come from (GPL-3.0), replayed through an out-of-tree driver and adjudicated against the other two | `cmd/oraclediff` (`forge-*`, `adjudicate`), `scripts/forge-*` |

Plus `scenarios/xmageport/`: XMage's own Mage.Tests interaction tests,
translated by `cmd/xmageport` into gorge's scenario schema and run through
gorge's runner.

Where the sources disagree, the triage step classifies the row (gorge wrong,
XMage wrong, a ruled divergence) and the result is recorded, never silently
accepted. See `docs/design/` for the designs:
[XMage oracle](docs/design/2026-10-02-xmage-compliance-oracle-design.md),
[level B](docs/design/2026-10-05-compliance-level-b.md),
[XMage scenario port](docs/design/2026-10-10-xmage-scenario-port-design.md).

## Coverage targets and the pin

Every committed target here (the certification ratchet
`oracle/compliance/ratchet.json`, the XMage verdicts, the declared set
levels, the hand-audit `known-divergent`/`known-unconsumed` rows, the
`known_bug` rows of the XMage ports) is a statement about **the gorge commit
in [`GORGE_PIN`](GORGE_PIN)**. Bumping the pin is a pull request that changes
`GORGE_PIN` and `go.mod`, reruns everything and updates the targets. A nightly
CI job runs against gorge `main` and **reports** drift without failing.
[docs/PINNING.md](docs/PINNING.md) has the procedure.

## Layout

| Path | What |
|---|---|
| `audit/` | the scenario corpus drivers (`TestOracleAudit`, `TestOracleScenarioFilesWellFormed`, `TestXMagePortScenarios`), external tests over gorge's exported runner API |
| `scenarios/oracle/` | hand-authored Oracle-text scenarios, one file per card per family, with their per-card `known-divergent/` and `known-unconsumed/` ratchets |
| `scenarios/xmageport/` | XMage Mage.Tests translations (MIT; `NOTICE` inside) |
| `oracle/compliance/` | per-set manifests (XMage-derived), verdicts, declared levels, the gate (`gate`), certification (`adopt`), level B (`levelb`), scenario generation (`oraclegen`), shape rulings (`shape`, `rulings/`), triage |
| `oracle/xmageoracle/` | the Java XMage replay driver and its unit tests |
| `oracle/validation/oracle/` | the verdict-refresh runbook and scripts |
| `cmd/` | `oraclediff` (the pipeline's gorge side: gen, diff, triage, status, Forge export/adjudicate), `compliance` (manifest maintenance), `xmageport`, `oraclepacket` (printed-card view for audit authors) |
| `scripts/` | XMage and Forge oracle drivers, the compliance pass, the driver unit-test runners (`xmage-oracle-test-*.sh`) |
| `ops/scripts/` | `driver_replay_batch.sh` (parked: written for the maintainers' agent pipeline) and the script smokes |
| `internal/` | `gorgedir` (locates the gorge module) and `boundary` (the licence guards) |

## Setup

```sh
git clone git@github.com:adams-shaun/gorge.git
git clone git@github.com:adams-shaun/gorge-compliance.git
cd gorge && make fetch-cards compile-cards     # Forge corpus into gorge/.cards (gitignored)
cd ../gorge-compliance && make cards           # link gorge's .cards here
```

`go.mod` resolves gorge as described in [docs/PINNING.md](docs/PINNING.md).
To build against a different gorge checkout, write a local `go.work`
(gitignored, never committed):

```
go 1.25.8

use (
	.
	../gorge/.worktrees/<branch>
)
```

Without `.cards`, every corpus-dependent test SKIPS and the package still
prints `ok` in milliseconds. CI refuses to run without the corpus; do the
same locally.

## Common commands

```sh
make test                  # go test ./... (capped)
make oracle-audit          # both scenario corpora, -v (ORACLE_RUN='Fatal_Push' filters the audit)
make boundary              # the licence guards
make smoke                 # script smokes (stubs; no Java, no network)
make compliance-status     # per-set/per-format status (oracle/compliance/status.md, never committed) + ratchet check
make compliance-sets       # both levels over every printed set -> .coverage/compliance-sets.md
make xmage-oracle-setup    # build XMage at XMAGE_REF out of tree (heavy)
make xmage-driver-tests    # the Java driver's unit tests (needs the XMage build)
make compliance-pass SETS='FRA BLB'   # XMage pass over the sets; reruns only stale verdicts
make forge-oracle-setup    # build the Forge driver at FORGE_ORACLE_REF out of tree (heavy)
```

The XMage and Forge passes are heavy (a JVM per batch). On a shared box set
`HEAVY_LOCK` to the box's heavy-job lock wrapper (`scripts/heavy-lock.sh`
documents the interface); unset, they run unlocked.

## Writing a hand scenario

One JSON file per card at `scenarios/oracle/<family>/<card-slug>.json`,
derived from the printed card text and the Comprehensive Rules only (use
`go run ./cmd/oraclepacket "<Card>"` to see the printed view; write its output
to a gitignored path). `go test ./audit -run TestOracleScenarioFilesWellFormed`
checks the schema without the corpus; `make oracle-audit ORACLE_RUN=<Card_Name>`
runs it. The schema and the runner are gorge's (`engine/rules/oracle_run.go`);
the authoring rules are in gorge's
[oracle-text card audit design](https://github.com/adams-shaun/gorge/blob/main/docs/design/2026-09-27-oracle-text-card-audit.md).

## Relationship to gorge

- gorge's engine never imports this repository; this repository imports
  gorge's public packages only (`engine/rules`'s exported oracle runner API:
  `RunOracleScenarioJSON`, `RunOracleAuditScenarioJSON`,
  `CheckOracleScenarioJSON` and the `Oracle*` result types).
- gorge keeps the runner, its unit tests and nine residual hand-scenario
  files its own card tests load, run by `engine/rules/oracle_contract_test.go`
  as the runner's contract check. This repository's audit and gate read those
  together with `scenarios/oracle`; a scenario file lives in exactly one of
  the two.
- A gorge change that moves a card's behaviour shows up here as a stale
  ratchet row or a moved verdict at the next pin bump (or in the nightly
  drift report before it).

## Licence

Apache-2.0 (`LICENSE`). **Forge card scripts, token scripts and every Forge
artifact are GPL-3.0 and are never committed here.** The corpus lives only in
gorge's gitignored `.cards/`, reached through the gitignored `.cards` link;
`internal/boundary` fails the build if a script or Forge artifact is tracked,
or is one `git add` away from it. Card rules (Oracle) text is not committed
either; scenarios name cards only.

`NOTICE` credits XMage (MIT): the set manifests are derived from XMage's set
classes, and `scenarios/xmageport/` translates XMage's tests.
