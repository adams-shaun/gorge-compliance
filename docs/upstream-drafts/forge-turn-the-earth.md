# Turn the Earth with no target shuffles the caster's library

Engine: Forge (Card-Forge/forge). Upstream base: master `9a91120cac` (2026-11-24). Mirror branch: `adams-shaun/forge:upstream/changezone-shuffle-no-cards` at `6c10997556` (local; push pending, see note).

## Oracle text

Turn the Earth:

> Choose up to three target cards in graveyards. The owners of those cards shuffle them into their libraries. You gain 2 life.
> Flashback {1}{G}

Gatherer ruling (2021-09-24): "You may choose no targets for Turn the Earth. In that case, no libraries will be shuffled and you'll just gain 2 life."

## Comprehensive Rules

> 701.24c If an effect would cause a player to shuffle one or more specific objects into a library, that library is shuffled even if none of those objects are in the zone they're expected to be in or an effect causes all of those objects to be moved to another zone or remain in their current zone.

> 608.2c The controller of the spell or ability follows its instructions in the order written.

With zero targets the effect names no object and no owner, so 701.24c does not apply and nothing is shuffled.

## What happens

`forge-game/src/main/java/forge/game/ability/effects/ChangeZoneEffect.java:903-921` (the block commented "for things like Gaea's Blessing"): with `Destination$ Library` and `Shuffle$ True`, when there is no `TargetsWithDefinedController$` it collects the owners of the moved cards and, if that list is empty, adds the activator (`:915`), then shuffles. Turn the Earth cast with no target shuffles the caster's library.

Scope: the `TargetsWithDefinedController$` path is correct as it is. Gaea's Blessing, Loaming Shaman, Krosan Reclamation and Dwell on the Past read "target player shuffles up to N target cards ...", and their rulings say the target player shuffles even with no card chosen (Gaea's Blessing 2018-04-27: "If Gaea's Blessing has no legal target cards in a graveyard, either because its targets became illegal or because you didn't choose any, the target player shuffles their library."; Loaming Shaman 2019-07-12: "... that player just shuffles their library.").

## Repro

1. Player A has a library of known order and casts Turn the Earth choosing no targets.
2. Expected: A gains 2 life; no library changes order. Actual: A gains 2 life and A's library is shuffled.

## Proposed fix

Keep the activator fallback only for abilities that do not target: guard the `pl.add(activator)` line with "the ability does not use targeting". A targeted ability that chose no card then shuffles nothing; the target-player path is unchanged. Fork commit: [2700297e43](https://github.com/adams-shaun/forge/commit/2700297e43f61bf6fffc094a596171ad1ee2869e). Untested: a unit test needs a PlayerController (the resolve path asks it about ordering), which forge-game's tests do not provide.


Note: the mirror branch is built on upstream master but is not on GitHub yet: upstream master adds `.github/workflows/card-script-review.yml`, and the push token lacks the `workflow` scope. The linked commit is the same change on the fork's `gorge` branch.
