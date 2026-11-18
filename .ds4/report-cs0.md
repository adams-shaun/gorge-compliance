# farm-blame-diverge-dsk-p0-exile-friendly-ghost — report

## Verdict: (c) harness

Window DSK-s130-rand `xe:RAND:4:12:w12` (evidence extracted from the pulled `raw.tar.zst`). p0 attacks with Norin, Swift Survivalist and Acrobatic Cheerleader; p1 blocks both, so Norin ("Whenever a creature you control becomes blocked, you may exile it") triggers twice. Gorge's recorded `trigger_order` pick is `[p0:Norin, p0:Norin]` and the lowered step carries those two identical picks, which name one object/ability and cannot say which blocked creature's trigger goes first. Gorge exiled Norin first, Forge exiled Acrobatic Cheerleader first; stacks are identical at steps 16-17 (two Norin abilities). Friendly Ghost is only in the window (its +2/+4 trigger targets p1's Cheerleader at step 7), not involved in the exile.

## CR
603.3b: "If multiple abilities have triggered since the last time a player received priority, each player, in APNAP order, puts triggered abilities they control on the stack in any order they choose." Either exile order is legal; neither engine is wrong. The XMage follower of the same window already classes it harness (`classify.go` `sameNameTriggerOrder`, "the driver cannot order triggers of objects named ... apart"), but that guard is gated on `Follower.TriggerOrderByName` (follower.go:129, XMage only), so Forge reads it as a diverge.

## Changes
- `oracle/xengine/diagnosed.tsv`: row `blame|diverge|p0.exile|Friendly Ghost`, verdict harness, CR 603.3b.
- `.ds4/new-tickets/xe-same-source-trigger-order-forge.md`: harness ticket (passes `.ds4/check-ticket`, rc=0) to apply the same-name trigger-order guard to Forge too.

## Commands
`make test-one PKG=./cmd/xengine RUN='^TestDiagnosed'` -> `test-one: PASS a029d67fe3+dirty ./cmd/xengine 2s`

The brief's gorge failing-first test (`TestFarmFriendlyGhost`) was not written: verdict is not (a).

## Gorge finding
None.

## Issues
- The farm box repro dir was gone; I used the pulled raw archive instead. The Forge driver's own trigger-order handling lives in the fork (not in this repo), so the exact reason Forge picked Cheerleader first was inferred from the identical picks, not read from fork source.
