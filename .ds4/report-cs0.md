# Report cs0: Attentive Sunscribe (stack, LCI-s122 xe:RAND:0:10:w19)

## Verdict: (b) XMage fix on the fork (fork is read-only in this seat, so the patch is a filed follow-up)
No gorge finding, no Forge finding, not a harness problem. The farm row also has a 3way `GF|X` twin: Forge agrees with gorge.

## Measurement
Extracted the pulled raw.tar.zst (LCI-s122/RAND/0) and compared the three engines' snapshots.
- p1 controls Dauntless Dismantler ("Artifacts your opponents control enter the battlefield tapped.").
- p0 casts Attentive Sunscribe at step 17. At step 19 (pass) it resolves and is a tapped 2/2 in gorge, Forge and XMage.
- gorge and Forge: stack [] at step 19. XMage: stack [ability:Attentive Sunscribe@p0] at steps 19 and 20, gone at 21.

## CR
CR 701.26a: "To tap a permanent, turn it sideways from an upright position. Only untapped permanents can be tapped." A permanent that enters tapped (CR 614.12, replacement effects that modify how a permanent enters) is never tapped as an action, so "Whenever Attentive Sunscribe becomes tapped, scry 1" does not trigger.
Agreeing engines: gorge engine/rules/event_trigger_test.go `TestTapsTriggerCityOfBrassDoesNotFireForEntryTapped` (entry-tapped Tap events queue no Taps triggers); Forge's Mode$ Taps trigger did not fire (step-19 snapshot stack []).

## Cause in XMage (farm ref 4142596)
`Mage/src/main/java/mage/abilities/effects/common/PermanentsEnterBattlefieldTappedEffect.java` replaceEvent calls `target.tap(source, game)`, which fires TAPPED (`PermanentImpl.java:648-653`); `BecomesTappedSourceTriggeredAbility` (checkEventType TAPPED) then triggers. XMage's own self-entry path is right: `TapSourceEffect(true)` uses `permanent.setTapped(true)` (TapSourceEffect.java:44). The shared effect is used by 25 cards; the fix (`target.setTapped(true)`) is in the effect, so every one of them is covered.

## What changed
- `oracle/xengine/diagnosed.tsv`: one row `diverge LCI Attentive Sunscribe stack X 701.26a`, evidence starts `blame|diverge|stack|Attentive Sunscribe`.
- Follow-up ticket `xe-xmage-enter-tapped-no-trigger` (fork patch + Mage.Tests test), written to `.ds4/new-tickets/`.

## Commands
`go run -trimpath ./cmd/xengine diagnosed -check -file oracle/xengine/diagnosed.tsv -cards .cards` -> no output, rc=0 (register valid).
No gorge test was run: verdict is not (a), so the `TestFarmAttentiveSunscribe` gorge test does not apply.

## Issues
- The fork checkout (/mnt/sata/gorge-training/xmageoracle/mage and ~/projects/ref/xmage) is a read-only mount here, so the fork fix and its Mage.Tests test are NOT done; the upstream column of the row is empty until the follow-up lands.
- The orchestrator picked up the follow-up ticket before I had fixed two `done_no_command` lint findings in its Done-means list, so it was filed in the lint-failing form and a triage rewrite is likely. I did not file a duplicate.
- The farm repro dir /scratch/xengine/LCI-s122 is pruned; I used the pulled raw.tar.zst instead.
