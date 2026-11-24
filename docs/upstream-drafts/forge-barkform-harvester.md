# Changeling stops being every creature type after "loses all abilities"

Engine: Forge (Card-Forge/forge). Upstream base: master `9a91120cac` (2026-11-24). Mirror branch: `adams-shaun/forge:upstream/changeling-kept-under-ability-loss` at `d0d79d623d` (local; push pending, see note).

## Oracle text

Barkform Harvester:

> Changeling (This card is every creature type.)
> Reach
> {2}: Put target card from your graveyard on the bottom of your library.

Azure Beastbinder (the effect it was under):

> Whenever Azure Beastbinder attacks, up to one target artifact, creature, or planeswalker an opponent controls loses all abilities until your next turn. If it's a creature, it also has base power and toughness 2/2 until your next turn.

## Comprehensive Rules

> 702.73a Changeling is a characteristic-defining ability. "Changeling" means "This object is every creature type." This ability works everywhere, even outside the game. See rule 604.3.

> 613.1d Layer 4: Type-changing effects are applied. ... 613.1f Layer 6: Ability-adding effects, keyword counters, ability-removing effects, and effects that say an object can't have an ability are applied.

> 613.3. Within layers 2–6, apply effects from characteristic-defining abilities first (see rule 604.3), then all other effects in timestamp order.

> 613.6. ... If an effect starts to apply in one layer and/or sublayer, it will continue to be applied to the same set of objects in each other applicable layer and/or sublayer, even if the ability generating the effect is removed during this process.

Changeling's layer-4 effect has already applied when the layer-6 ability removal takes the keyword away, so the creature stays every creature type.

## What happens

`AnimateEffectBase.java:88` applies `RemoveAllAbilities$` immediately, removing the keywords from the card's keyword cache. `Card.java:7109` rebuilds the static list only from the surviving keywords, so the Changeling static (`CardFactoryUtil.java:3795`, `CharacteristicDefining$ True | AddAllCreatureTypes$ True`) is missing when the type layer runs. Barkform Harvester under Azure Beastbinder shows only "Artifact Creature — Shapeshifter". The same holds for AnimateAll (Curious Colossus).

Found by cross-engine play (BLB); gorge and XMage keep every creature type.

## Repro

`ChangelingKeptUnderAbilityLossTest` on the mirror branch builds a Changeling creature and resolves Curious Colossus's AnimateAll line and Azure Beastbinder's Animate line at it. Before the fix 2 of 3 tests fail (`hasAllCreatureTypes()` false); after it 3/3 pass (javac + TestNG over master classes).

## Proposed fix

After an immediate ability loss, add back the characteristic-defining statics of the keyword instances that were removed: a Card method returns them, `GameAction.checkStaticAbilities` (near `GameAction.java:1130`) adds them to the layer loop, and `StaticAbility.shouldApplyContinuousAbility` (`StaticAbility.java:301`) accepts them. Fork commit: [eb765123c6](https://github.com/adams-shaun/forge/commit/eb765123c6476f478f2840298722c6f9090f9b92). The Devoid draft (`forge-emrakuls-messenger.md`) widens the same method from Changeling to every keyword with a `CharacteristicDefining` static; upstream should take the wider version.


Note: the mirror branch is built on upstream master but is not on GitHub yet: upstream master adds `.github/workflows/card-script-review.yml`, and the push token lacks the `workflow` scope. The linked commit is the same change on the fork's `gorge` branch.
