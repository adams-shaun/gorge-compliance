# Report cs0: Forge differs on Chomping Changeling (permanents), ECL xe:RAND:0:6:w16

## Verdict: (b) Forge fork fix. Fork write is out of reach for this seat, so the fix is filed as a ticket.

## CR
- CR 613.1d: "Layer 4: Type-changing effects are applied." CR 613.1f: "Layer 6: Ability-adding effects, keyword counters, ability-removing effects, and effects that say an object can't have an ability are applied."
- CR 702.73a: Changeling is a characteristic-defining ability meaning "This object is every creature type"; CR 613.3 applies CDAs first within layers 2-6.
- Layer 4 settles before layer 6 removes the ability, so a Changeling hit by "loses all abilities, becomes a Coward in addition to its other types" stays every creature type. Gorge pins it in engine/rules/changeling_layer_order_test.go `TestChangelingTypeCDASettlesBeforeAbilityRemoval` (pin 1b7ffec895); XMage agrees with gorge.

## Measurement
Rebuilt from `raw.tar.zst` (the farm repro dir is pruned): window items.jsonl shows p0's Curious Colossus resolving onto p1's Chomping Changeling. Its trigger ("each creature target opponent controls loses all abilities, becomes a Coward in addition to its other types, and has base power and toughness 1/1") yields gorge `[allcreaturetypes creature] 1/1` vs Forge `[coward creature shapeshifter] 1/1`.

## Forge source line (read, not run)
- forge-game/src/main/java/forge/game/card/CardFactoryUtil.java:3791-3795: Changeling = keyword-attached static, `AddAllCreatureTypes$ True`.
- forge-game/src/main/java/forge/game/ability/effects/AnimateEffectBase.java:80-82, 108-113: `RemoveAllAbilities` becomes `addChangedCardKeywords(removeAllKeywords=true)`, removing the keyword and its static, so the layer-4 type effect is gone.
I did not rerun Forge; this is source reading plus the farm row.

## Changes
- `oracle/xengine/diagnosed.tsv`: new row `GX|F ECL Chomping Changeling permanents F 613.1d` (ticket `xe-forge-changeling-survives-ability-removal`).
- `.ds4/new-tickets/xe-forge-changeling-survives-ability-removal.md`: fork fix ticket (passes `.ds4/check-ticket`, rc=0).

## Commands
- `go run ./cmd/xengine diagnosed -check` -> rc=0
- `make test-one PKG=./cmd/xengine RUN='^TestDiagnosed'` -> `test-one: PASS 0604eb5c87+dirty ./cmd/xengine 0s`
- `make boundary` is blocked in seats (raw go test); not a Done-means gate.

## Concerns
Done means says the fork fix lands with the row; ~/projects/forge-oracle is a read-only filesystem here, so only the row and the ticket landed. The row stays until a fixed driver build reruns the window.

## Contract changes
None. No gorge change, no failing-first gorge test (verdict is not (a)).

## Gorge finding
None.

## Issues
Fork fix not landed (above).
