# xp-xmage-choosepile-scripted

## What
- `ScenarioReplay.ScriptedChoicePlayer.choosePile` override (driver, not the fork): a leading `Yes`/`No` on the choice queue is consumed as the pile pick (`Yes` = pile1 = the separator's chosen cards = gorge pile A, matching `RevealAndSeparatePilesEffect`'s `choice ? HAND : targetZone` and `twoPilesPileAnswer`); empty/other head falls back to TestPlayer (AI). Helper `scriptedPileAnswer`.
- New driver test `ScenarioReplayTwoPilesChoosePileTest` + `scripts/xmage-oracle-test-two-piles-choose-pile.sh` (auto-picked by `make xmage-driver-tests`): `Yes`->true, `No`->false, queue empty after; only head consumed.
- gen.go: comment only (the pile answer also serves choosePile). No generator behaviour change. No fork patch needed.

## Evidence
- Repro: scenario.json from the find compacted to input.jsonl. Driver at HEAD (before): `Choices of PlayerB) Count are not equal (found [Yes]) ...`. With the change: no such error, no error field (grep empty). Run via a manual java invocation with a private db copy because the only XMage build (fork-sync worktree's, at b34a6018) is read-only here, so `xmage-oracle-run.sh` couldn't take its driver.lock.
- `make xmage-driver-tests XMAGE_ORACLE_DIR=<fork-sync build>`: every xmage-oracle-test-* passed, including `PASS choosePile follows the scripted Yes/No and leaves no choice queued`. It then stops at `forge-oracle-test-suspend` (no Forge build in this worktree; environmental, not related).
- `make test-one PKG=./oracle/compliance/oraclegen RUN='TwoPiles'` matches no test (no such name); the pile mapping tests live in `TestXAnswersQueueRoutingShapes` (`^TestXAnswersQueueRoutingShapes$`): PASS.
- `make boundary`: blocked by "raw go test is blocked in seats" wrapper message in this env; not run.

## Issues
- Build used is at b34a6018, not this Makefile's XMAGE_REF (7aeb547a); choosePile is unchanged between them as far as the TestPlayer source at 7aeb547a shows.
