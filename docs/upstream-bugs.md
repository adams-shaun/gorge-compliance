# Bugs in the other engines

What the oracles have found in XMage and Forge, by root cause. A pattern from the harness (`GF|X`, `GX|F`) is a
suspect and doesn't belong here. A row goes in once we've confirmed it ourselves:

1. The printed card or the Comprehensive Rules says what should happen, and the row cites it.
2. The cause is read in that engine's source, at its current upstream branch and not only at our pin.
3. It isn't our driver: a replay reproduces it with the harness ruled out.

For XMage, the verdict store (`oracle/compliance/verdicts`, status `xmage_wrong`) holds the ruling per scenario; this
list says whether upstream knows. For Forge, a script that disagrees with the printed card is also fixed on our fork.

## XMage, confirmed and not filed

None today.

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
| magefree/mage#16686, PR #16687 | Bolg's Company: its mana ability is built as a plain activated ability, so it uses the stack (CR 605.1a, 605.3b) | open |
| magefree/mage#16688, PR #16689 | Destined Confrontation: passes a source id as a player id, so a limited range of influence sacrifices nothing (CR 701.21a, 801.2) | open |
| magefree/mage#16690, PR #16691 | Jennifer Walters: She-Hulk's trigger damages the damaged creature, not the chosen target (CR 603.3d) | open |
| magefree/mage#16692, PR #16693 | Driftgloom Coyote: a permanent filter is matched against the exiled card (CR 608.2h) | open |
| magefree/mage#16694, PR #16695 | Pain for All: the trigger fires, then the effect finds no creature once it has died (CR 603.2, 608.2h) | open |
| magefree/mage#16696, PR #16697 | Sanar, Innovative First-Year: the colour check never runs for a non-target choice (Oracle text) | open |
| magefree/mage#16698, PR #16699 | Serah Farron: a copied ability shares its effect id, so the reduction applies once per ability (CR 712.8d, 712.8e) | open |
| magefree/mage#16700, PR #16701 | Sage of the Skies: counts itself for "if you've cast another spell this turn" (CR 603.4) | open |
| magefree/mage#16702, PR #16703 | Ojer Pakpatiq, and Unbreakable Bond: a card entering as another half loses its counters (CR 122.6a, 712.14a) | open |

Kitesail Larcenist needed no report: upstream fixed it in PR #16464 (75f4f1ae3d, 2026-10-10), after our pin.

Our fork, adams-shaun/mage, branch `gorge`, is magefree master plus the open fixes.

`XMAGE_REF` predates all of these closures, so the harness still reports the fixed cards as `GF|X` until the pin moves.

## Forge

Earlier: 27 issues and 22 merged PRs on Card-Forge/forge, 2026-10-06 to 2026-10-28, all closed: `gh search issues --repo Card-Forge/forge --author adams-shaun`.

Reported and open:

| Upstream | Card | What it does |
|---|---|---|
| Card-Forge/forge#12581, PR #12582 | Romulan Bird-of-Prey | `Affected$ Self.nonCreature` matches nothing in Forge's `isValid`, so it never gets its printed hexproof (CR 702.11b) |

Our fork, adams-shaun/forge, branch `gorge`, carries that fix.
