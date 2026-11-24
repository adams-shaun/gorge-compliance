# Vivien Reid, Sigarda, Sarkhan's Dragonfire: "the rest on the bottom in a random order" asks the player to order them

Engine: Forge (Card-Forge/forge). Upstream base: master `9a91120cac` (2026-11-24). Mirror branch: `adams-shaun/forge:upstream/dig-rest-random-order` at `26c24e6972` (local; push pending, see note).

## Oracle text

Vivien Reid, +1:

> Look at the top four cards of your library. You may reveal a creature or land card from among them and put it into your hand. Put the rest on the bottom of your library in a random order.

Sigarda, Champion of Light and Sarkhan's Dragonfire carry the same last sentence.

## Comprehensive Rules

> 401.4. If an effect puts two or more cards in a specific position in a library at the same time, the owner of those cards may arrange them in any order.

> 608.2c The controller of the spell or ability follows its instructions in the order written.

401.4's choice applies only when the effect leaves the order open; "in a random order" sets it.

## What happens

`DigEffect.java:454` randomizes the rest only when `RestRandomOrder$` is set; otherwise the player orders them. The Dig lines at `forge-gui/res/cardsfolder/v/vivien_reid.txt:5`, `s/sigarda_champion_of_light.txt:9` and `s/sarkhans_dragonfire.txt:5` lack it. A grep for Oracle text "... bottom of ... library in a random order" over Dig scripts with no `RandomOrder` finds only these three.

Found by gorge's independent card compiler (Oracle-based), cross-checked against Forge's script.

## Repro

1. Player A controls Vivien Reid with four or more cards in library.
2. Activate +1; take one creature card.
3. Expected: the other three go to the bottom in a random order with no prompt. Actual: Forge asks A to order them.

## Proposed fix

Add `RestRandomOrder$ True` to the three Dig lines. Fork commit: [ef600f3f67](https://github.com/adams-shaun/forge/commit/ef600f3f6701a6c881414cccb191d095472575fb). Script-only, untested.


Note: the mirror branch is built on upstream master but is not on GitHub yet: upstream master adds `.github/workflows/card-script-review.yml`, and the push token lacks the `workflow` scope. The linked commit is the same change on the fork's `gorge` branch.
