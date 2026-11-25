# Extraordinary Journey: X = 0 throws IndexOutOfBoundsException when the enters trigger resolves

Engine: XMage (magefree/mage). Written against our fork pin `98052067d4` (the card file is unchanged since magefree/mage at that revision); not yet re-checked against upstream master, and no mirror branch off upstream HEAD exists yet. Fork fix: `adams-shaun/mage:fix/extraordinary-journey-x-zero` at `8ddb51df0c`.

## Oracle text

Extraordinary Journey ({X}{X}{U}{U}, Enchantment):

> When Extraordinary Journey enters the battlefield, exile up to X target creatures. For each of those cards, its owner may play it for as long as it remains exiled.

## Comprehensive Rules

> 601.2c ... If the spell has a variable number of targets, the player announces how many targets they will choose before they announce those targets. In some cases, the number of targets will be defined by the spell's text. Once the number of targets the spell has is determined, that number doesn't change, even if the information used to determine the number of targets does.

> 115.6. A spell or ability that requires targets may allow zero targets to be chosen. Such a spell or ability is still said to require targets, but that spell or ability is targeted only if one or more targets have been chosen for it.

"Up to X target creatures" with X = 0 is up to zero targets: legal, and the effect does nothing.

## What happens

The ability's blueprint target is `new TargetCreaturePermanent(0, 1)` with `XTargetsCountAdjuster`.
`TargetsCountAdjuster.adjustTargets` clears the targets and adds none when X <= 0, so `source.getTargets()` is an empty
list. `ExtraordinaryJourneyEffect.apply` (`Mage.Sets/src/mage/cards/e/ExtraordinaryJourney.java`) starts with
`source.getTargets().get(0).getTargets()`, which throws `IndexOutOfBoundsException: Index 0 out of bounds for length 0`
as the trigger resolves.

A grep of the 81 card classes that use `XTargetsCountAdjuster` for `getTargets().get(0)` finds only this one.

## Fix

Return true at the top of `apply` when `source.getTargets().isEmpty()` (nothing to exile, no card to play).

## Repro

1. Player A casts Extraordinary Journey with X = 0.
2. Let the enters trigger resolve.
3. Expected: it resolves and does nothing. Actual: `IndexOutOfBoundsException`.

`ExtraordinaryJourneyTest.testXZeroResolvesWithNoTargets` (WOE) fails before with `Index 0 out of bounds for length 0` and
passes after; `testXOneStillExilesATarget` pins that X = 1 still exiles its target.
