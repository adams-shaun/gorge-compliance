# Myriad with no token still creates the end-of-combat exile trigger

Engine: Forge (Card-Forge/forge). Upstream base: master `9a91120cac` (2026-11-24). Mirror branch: `adams-shaun/forge:upstream/myriad-empty-end-of-combat` at `6760169653` (local; push pending, see note).

## Oracle text

Broodbirth Viper (the same holds for every Myriad carrier: Dalek Squadron, Cybermen Squadron, and creatures that gain Myriad from Cybermen Squadron, such as Containment Construct, Metalwork Colossus and Mister Gutsy):

> Myriad (Whenever this creature attacks, for each opponent other than defending player, you may create a token copy that's tapped and attacking that player or a planeswalker they control. Exile the tokens at end of combat.)
> Whenever Broodbirth Viper deals combat damage to a player, you may draw a card.

## Comprehensive Rules

> 702.116a Myriad is a triggered ability that may also create a delayed triggered ability. "Myriad" means "Whenever this creature attacks, for each opponent other than defending player, you may create a token that's a copy of this creature that's tapped and attacking that player or a planeswalker they control. If one or more tokens are created this way, exile the tokens at end of combat."

## What happens

In a two-player game the only opponent is the defending player, so Myriad creates no token. Forge still creates the "exile the tokens" delayed trigger, and at end of combat an empty ability goes on the stack and players get priority over it.

- `forge-game/src/main/java/forge/game/card/CardFactoryUtil.java:1516`: the Myriad copy ability uses `ForEach$ OppNonDefendingPlayer` with `AtEOT$ ExileCombat`.
- `forge-game/src/main/java/forge/game/ability/effects/TokenEffectBase.java:223`: `registerDelayedTrigger` is called with the token list whether or not it is empty.
- `forge-game/src/main/java/forge/game/ability/SpellAbilityEffect.java:650`: `registerDelayedTrigger` builds and registers the trigger with no check for an empty card set.

Found by cross-engine play (gorge vs Forge vs XMage): 12 farm windows across six cards; gorge and XMage leave the stack empty after combat.

## Repro

1. Two-player game. Player A controls Broodbirth Viper. Player B has no creatures.
2. A attacks B with the Viper. The Myriad trigger resolves; it creates no token (no opponent other than B).
3. Go to end of combat.
4. Expected: nothing on the stack. Actual: an ability of Broodbirth Viper (the exile-at-end-of-combat delayed trigger) is put on the stack.

`MyriadNoTokensCreatesNoDelayedTriggerTest` on the mirror branch resolves Myriad's own ability, fires the end-combat trigger, and checks the delayed-trigger list and the stack. It fails on master (`expected [false] but found [true]`) and passes with the fix (javac + TestNG over master classes).

## Proposed fix

In `SpellAbilityEffect.registerDelayedTrigger`, return early when the card set is empty. That covers every `AtEOT` caller (Token, CopyPermanent, Animate, Pump, Effect, ChangeZone, ChangeZoneAll), not just Myriad. Fork commit: [f30d448731](https://github.com/adams-shaun/forge/commit/f30d4487312965e2b3b24c459b3576731ddc4781).


Note: the mirror branch is built on upstream master but is not on GitHub yet: upstream master adds `.github/workflows/card-script-review.yml`, and the push token lacks the `workflow` scope. The linked commit is the same change on the fork's `gorge` branch.
