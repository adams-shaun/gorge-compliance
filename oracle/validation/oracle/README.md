# Refreshing the oracle results

This runbook is for someone who changed gorge's behaviour, the generator or a driver and now
has stale verdicts. It assumes the builds from
[docs/oracles.md](../../../docs/oracles.md#what-the-xmage-and-forge-oracles-need). CI never runs
XMage or Forge, so a person refreshes these results and commits them.

| Script | What it does |
|---|---|
| `scripts/compliance-pass.sh [SET...]` | The usual pass: replays only stale scenarios, using a result cache keyed by `XMAGE_REF` and the driver's sha |
| `full-replay.sh OUT` | Regenerates and replays every printed set, writes verdicts and applies triage; use it after a driver or generator-wide change |
| `std-write.sh WORKTREE OLDRUN NEWRUN SET...` | Per set: regenerates, replays only the sets whose scenario file changed (or those in `FORCE_XMAGE`), writes verdicts |
| `std-batch.sh OUTROOT SET...` | A read-only survey: generate, replay and diff, writing no verdicts |
| `verdict-compare.py BASE NEW` | Compares row statuses between two checkouts; exits 1 if a row went from `agree` to anything else |
| `scripts/forge-pass.sh [SET...]` | The [Forge pass](#the-forge-pass) |

## After a change that moves a card's result

A gorge change that moves a card leaves `TestCertificationRatchet`
(`oracle/compliance/adopt`) or `TestDeclaredSetsCompliantChunkNN` (`oracle/compliance/gate`) red.
Those tests log a `GORGE-CERT-RESULT` line saying whether every problem is a stale verdict.

1. Run the pass in the checkout whose verdicts should change. It writes
   `oracle/compliance/verdicts` and `oracle/compliance/triage`:

   ```sh
   COMPLIANCE_PASS_OUT=/some/run/dir LEVEL=B scripts/compliance-pass.sh FDN
   ```

   `xmage-oracle-run.sh` starts its own systemd scope, so don't wrap it in another one; the
   nested scope fails with "Unit ... already loaded".

2. Compare every row with the base before trusting the result:

   ```sh
   oracle/validation/oracle/verdict-compare.py <base checkout> .
   ```

   A fix that is right per the rules can still regress other cards through the harness.

3. A row whose scenario sha changed loses its ruling and goes back to `diverge`. Reapply one only
   if its citation still holds:

   ```sh
   go run ./cmd/oraclediff rule -card "X" -status xmage_wrong -ruling "<CR or Oracle citation>"
   ```

   Don't rule a divergence the harness caused (an ambiguous target name, a choice the script
   didn't make) as `xmage_wrong`; leave it outstanding. XMage answers an unexpected choice with its
   AI, so replay a single scenario twice before believing a flip.

4. When a set's outstanding count fell, record it. The command writes the named sets to
   `oracle/compliance/ratchet.json` as they stand:

   ```sh
   go run ./cmd/oraclediff status -all -sets FDN -write-ratchet
   ```

   A count may only fall, so if a set's count grew, fix the cause; don't record it.

5. Run the touched packages: `go test ./oracle/compliance/...`.

## After a driver or generator-wide change

`compliance-pass.sh` replays a scenario only when its sha or `XMAGE_REF` changed. A change to
`oracle/xmageoracle` changes the cache key but not that test, so rows written by the old driver
stay in place and look fresh. After any driver change, force a replay instead, and compare every
row as above:

```sh
oracle/validation/oracle/full-replay.sh /some/run/dir
```

`FULL_REPLAY_JOBS` sets how many JVMs run at once (default 6), each with its own copy of XMage's
card database. Then reconfirm any sampled automatic ruling the rewrite reset, with
`oraclediff rule -card X -confirm`.

## The Forge pass

Forge is a corroborating reference and never enters the CI gate. Run its pass after the XMage pass,
at the same level, because it adjudicates against XMage's result cache and skips any scenario the
cache doesn't hold:

```sh
LEVEL=B scripts/compliance-pass.sh
scripts/forge-pass.sh            # every printed set
scripts/forge-pass.sh FDN OTJ    # a survey of named sets
```

Per set it runs `oraclediff gen`, `forge-export`, a replay of the requests the Forge cache doesn't
hold and `forge-diff`, then one `adjudicate` over every set. It writes the ledger to
`oracle/compliance/adjudication/<a-z>.jsonl`: card, template, scenario sha, Forge ref, pattern and
first-difference field. Those are gorge's own vocabulary, with no Forge-produced value or script text.
Forge's raw rows and caches stay under `$FORGE_ORACLE_DIR`.

The ledger writer replaces whole shards, so a pass over named sets leaves a partial ledger. The
pass never writes verdicts; marking a contradiction for review is a separate step,
`oraclediff adjudicate -mark-review`.

The cache is keyed by request sha, and its directory is named by the driver's source sha and
`FORGE_ORACLE_REF`, so a warm cache replays nothing. A new driver source starts cold, but a
`FORGE_ORACLE_REF` bump with the same driver source doesn't, so add `--force` when you need every
row to be current. `ops/scripts/tests/forge_pass_smoke.sh` pins that behaviour with stubs.

After a `FORGE_REF` bump in gorge, merge the fork's `gorge` branch into `oracle-harness`, bump
`FORGE_ORACLE_REF` in the Makefile and rerun `make forge-oracle-setup`. The setup refuses to go on
unless `FORGE_REF` is an ancestor of the driver ref with identical card and token script trees,
because a script difference would look like an engine disagreement.
