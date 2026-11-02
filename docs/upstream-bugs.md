# Bugs in the other engines

What the oracles have found in XMage and Forge, by root cause. A pattern from the harness (`GF|X`, `GX|F`) is a
suspect and doesn't belong here. A row goes in once we've confirmed it ourselves:

1. The printed card or the Comprehensive Rules says what should happen, and the row cites it.
2. The cause is read in that engine's source, at its current upstream branch and not only at our pin.
3. It isn't our driver: a replay reproduces it with the harness ruled out.

For XMage, the verdict store (`oracle/compliance/verdicts`, status `xmage_wrong`) holds the ruling per scenario; this
list says whether upstream knows. For Forge, a script that disagrees with the printed card is also fixed on our fork.

## XMage, confirmed and not filed

Each was ruled from a scenario at our pin, `XMAGE_REF` (2026-10-02). Check step 2 again before filing: any of these may
be fixed upstream by now.

| Card | What XMage does | Rule |
|---|---|---|
| Bolg's Company | Builds its mana ability as a plain activated ability, so it uses the stack | CR 605.1a |
| Destined Confrontation | Passes a card id as a player id, so under range of influence one nothing is sacrificed | CR 701.21a |
| Driftgloom Coyote | Checks "had power 2 or less" against the exiled card rather than last known information | CR 608.2h |
| Jennifer Walters | She-Hulk's trigger damages the damaged creature, not the chosen target | CR 603.3d |
| Kitesail Larcenist | Becoming a Treasure also clears the permanent's colour | CR 613.1e, 105.2 |
| Ojer Pakpatiq, Deepest Epoch | Returns Temple of Cyclical Time without its three time counters | CR 122.6 |
| Pain for All | Misses the trigger when the Aura and the creature leave in the same state-based action | CR 603.10a |
| Sage of the Skies | Counts Sage itself for "if you've cast another spell this turn" | CR 603.4 |
| Sanar, Innovative First-Year | Exiles every revealed card, not only those sharing a colour with your permanents | Oracle text |
| Serah Farron | Applies the back face's cost reduction while the front face is up | CR 711.4, 712.8d |

## XMage, reported

| Upstream | Cards | State |
|---|---|---|
| magefree/mage#16452 (our PR) | Sarkhan, Dragon Ascendant; Emergent Haunting; Sugar Coat | merged 2026-10-17 |
| magefree/mage#16453 | Living Brain, Mechanical Marvel | closed 2026-10-10 |
| magefree/mage#16454 | Firdoch Core | closed 2026-10-17 |
| magefree/mage#16455 | Secret Identity | closed 2026-10-10 |
| magefree/mage#16476 | Rooms: Roaring Furnace, Smoky Lounge, Restricted Office | open |
| magefree/mage#16477 | Loot, the Pathfinder | open |
| magefree/mage#16478 | Beast, Erudite Aerialist | closed 2026-10-21 |
| magefree/mage#16479 | Choreographed Sparks, Display of Power, See Double | closed 2026-10-21 |
| magefree/mage#16480 | Spider-Slayer, Hatred Honed | closed 2026-10-17 |
| magefree/mage#16481 | Deepway Navigator | closed 2026-10-17 |
| magefree/mage#16482 | Veteran Survivor | closed 2026-10-17 |
| magefree/mage#13565 (our comment) | Lifecraft Engine | closed 2026-10-17 |

`XMAGE_REF` predates all of these closures, so the harness still reports the fixed cards as `GF|X` until the pin moves.

## Forge

27 issues and 22 merged PRs on Card-Forge/forge, 2026-10-06 to 2026-10-28, all closed: `gh search issues --repo Card-Forge/forge --author adams-shaun`.

Suspected, not yet confirmed to the standard above:

| Card | What we saw |
|---|---|
| Romulan Bird-of-Prey | `Affected$ Self.nonCreature` matches nothing in Forge's `isValid`, so it never gets its printed hexproof |
