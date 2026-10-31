# Pinning gorge

This page explains why the results here hold for one gorge commit, and how to move to
another. gorge-compliance measures the commit named in [`GORGE_PIN`](../GORGE_PIN). Every
committed target in this repository is a statement about that commit and nothing else:

| Target | Where | What it pins |
|---|---|---|
| Certification ratchet | `oracle/compliance/ratchet.json` | per-set/per-format compliance counts that may only improve |
| XMage verdicts | `oracle/compliance/verdicts/*.jsonl` | gorge's agreement with XMage per generated scenario (each row hashes the scenario gorge generated) |
| Declared sets | `oracle/compliance/declared.json` | the level (A/B) each set is claimed at |
| Hand-audit ratchets | `scenarios/oracle/*/known-divergent/`, `known-unconsumed/` | scenarios that must still fail / still leave an answer unconsumed |
| XMage-port known bugs | `known_bug` rows in `scenarios/xmageport/**`, `cmd/xmageport/known_bugs.tsv` | ported scenarios that must still fail |
| Level-B requirements | `oracle/compliance/levelb`, `oracle/compliance/printed` | what level B demands per card |

A ratchet row is stale the moment gorge fixes the behaviour it records, and a verdict is
stale when gorge's generated scenario moves, so these files mean something only against the
pin. gorge's Makefile names its corpus commit (`FORGE_REF`), so pinning gorge pins the card
corpus too.

## How the pin is expressed

- `GORGE_PIN` holds the full gorge commit SHA the targets were measured
  against.
- `go.mod` requires `github.com/adams-shaun/gorge` at the pseudo-version of
  that commit, so `go build` and CI resolve exactly the pinned code.

## Bumping the pin

A pin bump is a pull request here, never a side effect:

1. Pick the gorge commit and write its SHA to `GORGE_PIN`.
2. `go get github.com/adams-shaun/gorge@<sha> && go mod tidy`.
3. Refresh the corpus: in a gorge checkout at that commit, run
   `make fetch-cards compile-cards`, then `make cards` here. `make cards` warns if the
   corpus isn't at the `FORGE_REF` of the gorge go now resolves.
4. Rerun and update the targets:
   - `make test` and `make oracle-audit`: fix or re-ratchet every stale `known-divergent`,
     `known-unconsumed` or `known_bug` row. A row is deleted when gorge now passes and added
     only by triage.
   - `make compliance-pass SETS=...` with the XMage build (`make xmage-oracle-setup`) for every
     set whose scenarios moved, as the [runbook](../oracle/validation/oracle/README.md) describes.
     Then run `make compliance-status` and update `ratchet.json`.
5. The PR description lists the gorge range (`old..new`), what moved, and
   every row added or deleted.

## Drift reporting

CI's nightly `drift` job builds and tests this repository against gorge
`main` instead of the pin and **reports** what would change (failing tests,
stale ratchet rows, compliance-status deltas) without failing the workflow.
Drift is the signal that a bump is due, or that gorge `main` regressed a
card; it is never a reason to edit targets without bumping the pin.

gorge itself never imports this repository and doesn't gate on it. gorge keeps the oracle
runner (`engine/rules/oracle_*.go`), its unit tests and a few residual scenario files as the
runner's contract check (`engine/rules/oracle_contract_test.go`); the audit here reads those
files together with `scenarios/oracle`.
