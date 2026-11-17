# Report cs0 — xmage driver elects adventure_alt / omen_alt

## Built
- `ScenarioReplay.castModeSupported` now admits `adventure_alt` and `omen_alt`. No change to the cast arm was needed:
  gorge's cast step already names the half (`card: "p0:Stomp"`, setup deals the whole card; see
  `spellCastAdventureCause` / adventure_faces_test.go), and the arm casts `castSpelling(refName(card))` = "Stomp",
  which is exactly XMage's `castSpell(..., "Stomp")` form. Snapshot stack source reads "Stomp" / "Absorb Essence",
  so `gorgeSpellingRule` needs no change (neither name contains " // ").
- `ScenarioReplayCastModeTest`: the old rejection of adventure_alt is replaced by acceptance checks, plus two real one-step
  replays (`replayOnce`, real H2 card DB): Bonecrusher Giant / Stomp (adventure_alt) and Purging Stormbrood / Absorb Essence
  (omen_alt, TDM), each asserting the half is the stack spell after the cast step.
- `scripts/xmage-oracle-test-cast-mode.sh` now sets up the scratch-cwd H2 copy (as the driver-contract script does) so the replays run.
- `ScenarioReplayDriverContractTest`: positive adventure_alt/omen_alt checks; the "unwired mode rejected" check now uses `warped`.

## Commands / output
XMAGE_ORACLE_DIR=/home/sadams/.cache/xmage-oracle-4142596 (the only XMage build on this box; none under the worktree)
- `bash scripts/xmage-oracle-test-cast-mode.sh .coverage/xmage-driver/cast-mode`:
  PASS mayplay ... / PASS plot_cast stays rejected (kicked too) / PASS adventure_alt and omen_alt are accepted /
  PASS adventure_alt casts Stomp by its own name; it is on the stack / PASS omen_alt casts Absorb Essence by its own name; it is on the stack /
  PASS an absent cast_mode is the ordinary cast
- `bash scripts/xmage-oracle-test-driver-contract.sh .coverage/xmage-driver/contract`: all PASS lines, exit 0 (castModeSupported adventure_alt/omen_alt "true").
- `make test-one PKG=./oracle/compliance/oraclegen RUN='^TestDriverPlotCast$'`: `test-one: PASS 88c18e7bdb+dirty ./oracle/compliance/oraclegen 2s`

## Concerns
- Repro sweep NOT verified. `XENGINE_FOLLOWERS=xmage scripts/xengine-sweep.sh --run adv --sets WOE --chunks 1 --games 10 --seed 111`
  failed in the driver constructor with `org.h2.jdbc.JdbcSQLException: Feature not supported: "autoServerMode && (readOnly ...` (card DB
  open against the shared build dir on this box; environmental, before any step runs). Also, that sample's 3 adventure_alt items were all
  tape items (`xmage_window_only` refusals), so it would not have exercised the arm anyway. The real replays above cover the cast path.
- `follower.go` XMage entry still has no `CastModes` (nil = follows everything), which is now correct for adventure_alt/omen_alt.
