# Bugs in the other engines

What the oracles have found in XMage and Forge, by root cause. A pattern from the harness (`GF|X`, `GX|F`) is a
suspect and doesn't belong here. A row goes in once we've confirmed it ourselves:

1. The printed card or the Comprehensive Rules says what should happen, and the row cites it.
2. The cause is read in that engine's source, at its current upstream branch and not only at our pin.
3. It isn't our driver: a replay reproduces it with the harness ruled out.

For XMage, the verdict store (`oracle/compliance/verdicts`, status `xmage_wrong`) holds the ruling per scenario; this
list says whether upstream knows. For Forge, a script that disagrees with the printed card is also fixed on our fork.

## XMage, confirmed and not filed

| Card or class | What XMage does | Rule | Source | Upstream |
|---|---|---|---|---|
| March of the World Ooze over a Vehicle animated after it entered; the Lifecraft Engine family | `MarchOfTheWorldOoze.java:36-41` registers the layer-4 `AddCardSubtypeAllEffect` (Ooze) with a `null` dependency and the layer-7b `SetBasePowerToughnessAllEffect` as separate effects; `AddCardSubtypeAllEffect.java:21-27` therefore never calls `addDependedToType(BecomeCreature)`, so the layer-4 dependency hook (`ContinuousEffects.java:1176-1185`, `ContinuousEffectImpl.isDependentTo:404-419`) never delays March. Layer 4 stays in timestamp order (`:215-216`, `:239-256`, `:1014`): March's older Ooze add runs first, its `FILTER_CONTROLLED_CREATURES` is evaluated while the Vehicle is still a non-creature, and the later animation adds Artifact+Creature but not Ooze. Crew provides `BecomeCreature` through `AddCardTypeSourceEffect.java:32-38` and Midnight Mangler's self-animation uses the same effect (`MidnightMangler.java:29-33`); Guidelight Matrix's external animation is `AddCardTypeTargetEffect` (`GuidelightMatrix.java:48-53`), whose `CREATURE` branch is missing the `BecomeCreature` dependency that `AddCardTypeSourceEffect` has (`AddCardTypeTargetEffect.java:30-39`), so neither side of that dependency is declared either. Layer 7b re-filters dynamically (`SetBasePowerToughnessAllEffect:91`), so the Vehicle is 6/6 without Ooze. 30 DFT random rows: 29 in-window animations (28 March, 1 Lifecraft Engine) and 1 setup divergence (Midnight Mangler, whose self-animation and March are both on the battlefield when the window opens) | CR 613.8a | Mage/src/main/java/mage/abilities/effects/ContinuousEffects.java:1014, 1176-1185; Mage.Sets/src/mage/cards/m/MarchOfTheWorldOoze.java:36-41; Mage.Sets/src/mage/cards/g/GuidelightMatrix.java:48-53; Mage.Sets/src/mage/cards/m/MidnightMangler.java:29-33 at master be0d2048 (2026-11-07; the add-type effect and two card files read at fe075157, 2026-11-02) | not reported |

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

Source read at Card-Forge/forge master 7c34365c74 (2026-11-03). "Our fork" is adams-shaun/forge, branch `gorge` (e59046060a); every file named below is byte-identical there to master, so none of these is fixed on the fork unless the row says so. The March of the World Ooze row was read later, at master 54353f6f (2026-11-07); its files are not checked against the fork.

### Filed 2026-11-03

All filed after an independent recheck; texts in the operator's triage store, none reproduced in a running game beyond the harness rows named.

| Upstream | Card or class | What Forge does | Rule | Our fork |
|---|---|---|---|---|
| Card-Forge/forge#12588 | Adrenaline Jockey | `Defined$ TriggeredPlayer` on a SpellCast trigger, which sets only the activator | Oracle text | not fixed |
| Card-Forge/forge#12589 | Bucolic Ranch, Cabaretti Ascendancy, Kenessos, Hurkyl, Loyal Inventor | `ChangeZoneEffect` shuffles on Library to Library unless the script says `NoShuffle$`; the `Defined$` branch never clears it | CR 701.24, Oracle text | not fixed |
| Card-Forge/forge#12590 | Combat whose attackers all left | `PhaseHandler.isSkippingPhase` (COMBAT_DECLARE_BLOCKERS) skips declare blockers and both damage steps when `combat.getAttackers()` is empty, and `removeFromCombat` empties the live set; the rule skips them only when no creature was declared. 50 random2 rows | CR 506.1, 508.8, 117.3a | not fixed |
| Card-Forge/forge#12591 | Hollow Marauder | `AbilityUtils.doXMath` splits its operand on every `.`, so `Minus.Remembered$Valid Card.cmcGE4` reads 0 | Oracle text | not fixed |
| Card-Forge/forge#12592 | Primo, the Indivisible's token (via Zimone, All-Questioning) | `tokenscripts/primo_the_indivisible.txt` has `Colors:green, blue`; `ColorSet.fromNames` does not trim, so the token is green only. The only token script with a space after the comma. Harness row xe:RAND:59:1:w10 | CR 105.2, printed card | not fixed |
| Card-Forge/forge#12593 | Time Stop and kin | `onStackResolved` gives priority after `endTurnByEffect` has entered cleanup | CR 724.1f, 514.3 | not fixed |
| Card-Forge/forge PR comments: #12466, #12409, #12487 | Open Trek script PRs: Enterprise NX-01 (`ValidgTgtsDesc$` typo), Erica Ortegas (`SVarDBDelTrig` missing colon, so the delayed return never exists), Seven of Nine (`DBimmediateTrig` vs `DBImmediateTrig`, SVars are case-sensitive), U.S.S. Enterprise-C (`Execute$ TrigToken` but only `TrigDiscard` exists), Infinite Diversity (`YoupOwn` for `YouOwn`) | script typos, read at each PR head on 2026-11-02 | Oracle text | fixed: branch `gorge-trek2` (66757752dc) carries the five scripts without the typos |

### Confirmed, not filed

| Card or class | What Forge does | Rule | Source | Our fork |
|---|---|---|---|---|
| Combat damage with nothing assigned after first strike | `PhaseHandler.onPhaseBegin` COMBAT_DAMAGE sets `givePriorityToPlayer = false` when `assignCombatDamage(false)` assigns nothing, so a lone first striker gets no priority in the second combat damage step. 12 random3 rows (`forge_skips_damage_step`); gorge gives the round | CR 510.4, 117.3a | forge-game/.../phase/PhaseHandler.java:342-351 at master 6d4a175d9e (rechecked 2026-11-06; the pin ae39d91530 has the same block); also the same shape for a 0-power attacker (CR 510.3). Upstream already has it as part 2 of Card-Forge/forge#12590, filed from source reading; no separate issue | fix on branch `fix-second-combat-damage-step-priority` of adams-shaun/forge (b16a076d04, regular damage step only; PR Card-Forge/forge#12722) |
| Parting Gust and any ChangeZone with `WithCountersType$` | `ChangeZoneEffect` passes `sa.getActivatingPlayer()` as the player who puts the entry counters (lines 610-615, 750-754, 1337-1340, 1640-1647, unless the script sets `WithCountersPlacer$`); with no placer the rule says the object's controller does, so a counter-doubling effect of the wrong player applies | CR 122.6a | forge-game/.../ability/effects/ChangeZoneEffect.java; seen on blb Parting Gust (fl-nstack2); the effect on a game was not replayed here | not fixed |
| Any player at 0 life when a state-based check starts | `GameAction.checkStateEffects` calls `checkGameOverCondition` first and returns when the game is over, before the loop performs the other state-based actions of the same check; The Darkness Crystal's exile-and-gain is lost. Final board and life only, and the early return is deliberate; held back from filing | CR 704.3, 704.5a | forge-game/.../GameAction.java:1424-1429 | not fixed |
| March of the World Ooze over a Vehicle animated after it entered; the Lifecraft Engine family | Forge does add March's layer-4 `AddType$ Ooze` record to the Vehicle, but the record carries March's ETB timestamp and `CardType.getTypeWithChanges` folds the changed-type records in timestamp order (`forge-core/src/main/java/forge/card/CardType.java:646-659`). `CardType.addAll` runs `sanisfySubtypes` after each record (`:203-213`, `:663-704`), and the Ooze creature subtype is dropped while the intermediate type is still a non-creature artifact; the animation's later record adds `Creature` but cannot restore Ooze. The P/T still lands because the affected set is computed from the final merged type and cached across layers (CR 613.6, `GameAction.java:1160-1173`). Crew and Guidelight Matrix both animate through `AB$ Animate` (`guidelight_matrix.txt`; `CardFactoryUtil.java:3702-3717`, `AnimateEffect.java:57`, `AnimateEffectBase.java:116`), which writes `CardChangedType` directly and so never enters Forge's 613.8a graph (`GameAction.findStaticAbilityToApply:1297-1409`, TYPE is dependency-enabled). Midnight Mangler's self-animation is a `S:Mode$ Continuous` static with `AffectedDefined$ Self` and `AddType$ Artifact & Creature` (`midnight_mangler.txt`) and does enter the graph, but the type-record fold above is still timestamp-ordered, so the older March record loses Ooze before the animation adds Creature. 30 DFT random rows (`crew_type_dependency`: 29 in-window animations — 28 March, 1 Lifecraft Engine — and 1 setup divergence, Midnight Mangler); gorge follows CR 613.8a | CR 613.8a | forge-core/src/main/java/forge/card/CardType.java:203-213, 646-659, 663-704; forge-game/.../GameAction.java:1160-1173, 1297-1409; forge-game/.../staticability/StaticAbilityContinuous.java:104-108, 395-397, 906-909 at master 54353f6f (2026-11-07) | not fixed |

Lifecraft Engine after an animation is the same shape: Forge's older Card-Forge/forge#7128 and XMage's magefree/mage#16484 (our comment on #13565). The rows above now carry the source trace.

### Reading the rules and not traced in the source

- Unwilling Vessel and Erratic Apparition: the "enchantment enters" trigger fires in Forge and XMage when the entering Aura strips the ability (CR 603.6a, 603.10; gorge doesn't fire). Erratic Apparition with Unable to Scream enchanting itself is harness row xe:RAND:426:11:w10. Neither engine's trigger collection was read line by line (Forge: ChangesZone trigger before the static re-layer; XMage: `ErraticApparition.java` via the EntersBattlefield event), so this stays a suspect.

### Looked at and dropped

- Convoke followed by a sacrifice: `CostAdjustment.adjustCostByConvokeOrImprovise` taps the convoke creatures while the cost is adjusted, before mana abilities run, and nothing in the sacrifice-candidate filter reads `SpellAbility.getTappedForConvoke` (its only readers are the AI and trigger code), so Forge lets a creature tapped for convoke be sacrificed to a mana ability in the same payment. gorge refuses it (CR 601.2g-h order). The Comprehensive Rules do not plainly forbid Forge's order (601.2h allows any order), so this is a divergence, not a confirmed Forge bug, and it has no row.

### Reported and open

| Upstream | Card | What it does |
|---|---|---|
| Card-Forge/forge#12581, PR #12582 | Romulan Bird-of-Prey | `Affected$ Self.nonCreature` matches nothing in Forge's `isValid`, so it never gets its printed hexproof (CR 702.11b) |
| Card-Forge/forge#12724, PR #12725 | Homesickness, Mind Spiral, Sygg's Command | the stun `PutCounter` has `Defined$ ParentTarget`, which `getDefinedPlayers` also resolves to the player targeted earlier in the chain, so that player gets a stun counter (Oracle text: only the tapped creature). Fix `Defined$ TargetedCard`, branch `fix/homesickness-mind-spiral-stun` (b2f70dde46, off master 6d4a175d9e); not on `gorge` yet. Harness row SOS-s104 xe:RAND:3:17:w14 |

Our fork, adams-shaun/forge, branch `gorge`, carries that fix.
