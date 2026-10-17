# Pinning gorge

gorge-compliance measures **one gorge revision**: the commit named in
[`GORGE_PIN`](../GORGE_PIN). Every committed target in this repository is a
statement about that revision and nothing else:

| Target | Where | What it pins |
|---|---|---|
| Certification ratchet | `oracle/compliance/ratchet.json` | per-set/per-format compliance counts that may only improve |
| XMage verdicts | `oracle/compliance/verdicts/*.jsonl` | gorge's agreement with XMage per generated scenario (each row hashes the scenario gorge generated) |
| Declared sets | `oracle/compliance/declared.json` | the level (A/B) each set is claimed at |
| Hand-audit ratchets | `scenarios/oracle/*/known-divergent/`, `known-unconsumed/` | scenarios that must still fail / still leave an answer unconsumed |
| XMage-port known bugs | `known_bug` rows in `scenarios/xmageport/**`, `cmd/xmageport/known_bugs.tsv` | ported scenarios that must still fail |
| Level-B requirements | `oracle/compliance/levelb`, `oracle/compliance/printed` | what level B demands per card |

A ratchet row is stale the moment gorge fixes the behaviour it records, and a
verdict is stale when gorge's generated scenario moves, so these files are
only meaningful against the pin. The gorge corpus pin (`FORGE_REF`) is read
from the pinned gorge's Makefile, so pinning gorge pins the card corpus too.

## How the pin is expressed

- `GORGE_PIN` holds the full gorge commit SHA the targets were measured
  against.
- `go.mod` requires `github.com/adams-shaun/gorge` at the pseudo-version of
  that commit, so `go build` and CI resolve exactly the pinned code.

**Bootstrap state.** The first pin is gorge's `wt/restructure` branch (PR
#10), which is not yet merged, so it has no pushed commit to require.
Until it lands, `GORGE_PIN` reads `PENDING (wt/restructure)`, `go.mod`
carries a placeholder require plus `replace github.com/adams-shaun/gorge =>
../gorge`, and a local, gitignored `go.work` points at the gorge worktree.
The first pin bump below replaces all three.

## Bumping the pin

A pin bump is a pull request here, never a side effect:

1. Pick the gorge commit (normally gorge `main`'s head) and write its SHA to
   `GORGE_PIN`.
2. `go get github.com/adams-shaun/gorge@<sha> && go mod tidy` (drop the
   `replace` once a real version is required).
3. Refresh the corpus: in the gorge checkout at that commit,
   `make fetch-cards compile-cards`, then `make cards` here.
4. Rerun and update the targets:
   - `make test` and `make oracle-audit`: fix or re-ratchet every stale
     `known-divergent` / `known-unconsumed` / `known_bug` row (a row is
     deleted when gorge now passes; added only by triage);
   - `make compliance-pass SETS=...` on a box with the XMage build
     (`make xmage-oracle-setup`) for every set whose scenarios moved, then
     `make compliance-status` and update `ratchet.json`.
5. The PR description lists the gorge range (`old..new`), what moved, and
   every row added or deleted.

## Drift reporting

CI's nightly `drift` job builds and tests this repository against gorge
`main` instead of the pin and **reports** what would change (failing tests,
stale ratchet rows, compliance-status deltas) without failing the workflow.
Drift is the signal that a bump is due, or that gorge `main` regressed a
card; it is never a reason to edit targets without bumping the pin.

gorge itself never imports this repository and does not gate on it. gorge
keeps the oracle runner (`engine/rules/oracle_*.go`), its unit tests and a
nine-file residual scenario set as the runner's contract check
(`engine/rules/oracle_contract_test.go`); this repository's audit reads that
residual set together with its own `scenarios/oracle`.
