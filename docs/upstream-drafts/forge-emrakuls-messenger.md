# Devoid creature gets its colour back after "loses all abilities"

Engine: Forge (Card-Forge/forge). Upstream base: master `9a91120cac` (2026-11-24). Mirror branch: `adams-shaun/forge:upstream/devoid-colour-kept-under-ability-loss` at `de40e66342` (local; push pending, see note). (it builds on the Changeling mirror branch).

## Oracle text

Emrakul's Messenger:

> Devoid (This card has no color.)
> Flying
> Whenever you draw your second card each turn, create a 0/1 colorless Eldrazi Spawn creature token with "Sacrifice this creature: Add {C}."

Humble: "Until end of turn, target creature loses all abilities and has base power and toughness 0/1."

## Comprehensive Rules

> 702.114a Devoid is a characteristic-defining ability. "Devoid" means "This object is colorless." This ability functions everywhere, even outside the game. See rule 604.3.

> 613.1e Layer 5: Color-changing effects are applied. 613.1f Layer 6: Ability-adding effects, keyword counters, ability-removing effects, and effects that say an object can't have an ability are applied.

> 613.6. ... If an effect starts to apply in one layer and/or sublayer, it will continue to be applied to the same set of objects in each other applicable layer and/or sublayer, even if the ability generating the effect is removed during this process.

## What happens

Same cause as the Changeling report: `AnimateEffectBase.java:88` removes the keywords immediately, `Card.java:7109` drops their statics, and Devoid's `CharacteristicDefining$ True | SetColor$ Colorless` static (`CardFactoryUtil.java:3881`) is gone before the colour layer. Emrakul's Messenger under Humble reports blue.

Found by cross-engine play (SUP): gorge reports the Messenger colourless, Forge `{U}`.

## Repro

`DevoidColourAfterAbilityLossTest` on the mirror branch builds a Devoid blue creature and resolves Humble's own Animate line at it: before the fix the colourless assertion fails (blue); after it both tests pass (the control, a blue non-Devoid creature, stays blue). `ChangelingKeptUnderAbilityLossTest` still passes 3/3.

## Proposed fix

Widen the Changeling fix to every keyword characteristic-defining static: select the lost keywords' statics by the `CharacteristicDefining` parameter instead of by the Changeling keyword. Only Changeling and Devoid build such statics today (`CardFactoryUtil.java:3795,3881`). Fork commit (Changeling fix and this widening, on the fork's `gorge` branch): [b967b5b4b6](https://github.com/adams-shaun/forge/commit/b967b5b4b626b88a9c6e86ae816a7561b9f31d6b).


Note: the mirror branch is built on upstream master but is not on GitHub yet: upstream master adds `.github/workflows/card-script-review.yml`, and the push token lacks the `workflow` scope. The linked commit is the same change on the fork's `gorge` branch.
