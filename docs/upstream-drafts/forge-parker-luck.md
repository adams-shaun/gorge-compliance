# PeekAndReveal: an empty library stops later players from revealing (Parker Luck)

Engine: Forge (Card-Forge/forge). Upstream base: master `9a91120cac` (2026-11-24). Mirror branch: `adams-shaun/forge:upstream/peekandreveal-clamp` at `4bef25798e` (local; push pending, see note).

## Oracle text

Parker Luck:

> At the beginning of your end step, two target players each reveal the top card of their library. They each lose life equal to the mana value of the card revealed by the other player. Then they each put the card they revealed into their hand.

## Comprehensive Rules

> 608.2c The controller of the spell or ability follows its instructions in the order written.

> 701.20a To reveal a card, show that card to all players for a brief time.

An empty library means that player reveals nothing; the other player still reveals their top card.

## What happens

`forge-game/src/main/java/forge/game/ability/effects/PeekAndRevealEffect.java:66` overwrites the shared peek count with the current player's library size inside the per-player loop. When an earlier player in the list has an empty (or shorter) library, every later player peeks and reveals fewer cards, here none, so nobody loses life. The same applies to every PeekAndReveal line over more than one player (Defined$ Player, two targets).

Found by cross-engine play (SPM, attributed to Sinister Hideout, the card whose Surveil emptied the library): gorge and XMage take p0 from 5 to 1 life; Forge leaves 5.

## Repro

1. Player A (life 5) controls Parker Luck and has an empty library. Player B's top card has mana value 4.
2. A's end step: Parker Luck targets A then B.
3. Expected: B reveals the card; A loses 4 (to 1); B loses 0. Actual: B reveals nothing and no one loses life.

## Proposed fix

Clamp per player: use a local count inside the loop and leave the shared one unchanged. Fork commit: [bf545e6710](https://github.com/adams-shaun/forge/commit/bf545e6710f30b763f2e445638a217e686a581a9). Untested (compiles against master).


Note: the mirror branch is built on upstream master but is not on GitHub yet: upstream master adds `.github/workflows/card-script-review.yml`, and the push token lacks the `workflow` scope. The linked commit is the same change on the fork's `gorge` branch.
