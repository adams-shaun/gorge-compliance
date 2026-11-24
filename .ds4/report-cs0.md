# Report cs0: Empty the Pits / Silent Attendant / Tymaret Calls the Dead (p0.graveyard, SUP)

## CR
CR 702.66a (checked against .ds4/MagicCompRules-20260807.txt:8773): "Delve is a static ability that functions while the spell with delve is on the stack. 'Delve' means 'For each generic mana in this spell's total cost, you may exile a card from your graveyard rather than pay that mana.'"
Empty the Pits is `K:Delve`, cost X X B B B B. Window `SUP-s293-supp xe:RAND:2:16:w16`, step 5 (cast): gorge p0.graveyard `[]`, xmage `[Grave Strength]`; the Forge follower says `harness: ... p0:Empty the Pits was not put on the stack`.
Engine sources:
- gorge poses the exile as a KChoose of kind "exile" (engine/decision/decision.go:74 in the pinned module) and subtracts it from the generic cost in engine/rules/cast_paymentplan.go:39-45.
- XMage models Delve as a special mana-payment action (mage-fork `DelveAbility.addSpecialAction`).
- The XMage replay driver lowers only the Improvise and Convoke special actions (oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java:483-490). `grep -rli delve` over oracle/xmageoracle, oracle/xengine/lower, cmd/oraclediff and scripts finds nothing.
- So gorge's exile pick is never replayed: XMage leaves the card in the graveyard and Forge cannot pay the cast.

## Verdict
(c) harness. Both engines implement Delve; the lowering drops the exile pick. No gorge test applies, so `TestFarmEmptythePits` was not written. Gorge follows CR 702.66a.
- Row appended to `oracle/xengine/diagnosed.tsv`: `blame|diverge|p0.graveyard|Empty the Pits`, wrong=harness, cr=702.66a, ticket `xe-delve-special-action-lowering`.
- Follow-up harness ticket: `.ds4/new-tickets/xe-delve-special-action-lowering.md.filed`. The orchestrator renamed it to `.filed` after my first write, and my edits to the citations went into that renamed file. The text lints clean with `.ds4/check-ticket`.

## Commands
- `go run ./cmd/xengine diagnosed -check`: exit 0, no output.
- `.ds4/check-ticket /tmp/xe-delve.md` (a copy of the ticket): rc=0.
- No replay: the farm repro directories under /scratch no longer exist, and the pulled copies (/mnt/sata/gorge-training/fleet/remote/pulled/5.187.38.38/xengine/SUP-s*-supp/...) hold only REPRO.md and verdict.json, with no input.jsonl. The verdict rests on the driver and engine source reads above, not on a re-run.

## Issues (not fixed)
- Silent Attendant (SUP-s276-supp xe:RAND:4:12:w27) and Tymaret Calls the Dead (SUP-s277-supp xe:RAND:4:4:w28) are NOT diagnosed, and I recorded no row for them.
  - Neither card's text touches the graveyard fields. The cards look like weak attributions.
  - w27: gorge has a trailing Mountain in the graveyard that xmage lacks, then the driver stops with `harness: activate step 8 on Mountain has no mana ability`. That points at object identity, but I could not confirm it.
  - w28: xmage has extra Umbral Juke and Treacherous Vampire, then `Player PlayerA must have 0 actions but found 3`.
  - Without the inputs they cannot be replayed. Their signatures will keep re-filing until someone replays them from a fresh farm run.
