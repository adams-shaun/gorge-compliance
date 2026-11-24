# `Shuffle$ False` shuffles anyway in ChangeZoneAll, DigUntil and Manifest

Engine: Forge (Card-Forge/forge). Upstream base: master `9a91120cac` (2026-11-24). Mirror branch: `adams-shaun/forge:upstream/changezoneall-shuffle-false` at `2f9531d5be` (local; push pending, see note).

## Oracle text

Wakanda Forever!:

> Reveal the top six cards of your library. You may put a permanent card from among them onto the battlefield with an indestructible counter on it. You may put a permanent card from among them into your hand. Put the rest into your graveyard.

## Comprehensive Rules

> 401.2. Each library must be kept in a single face-down pile. Players can't look at or change the order of cards in a library.

> 608.2c The controller of the spell or ability follows its instructions in the order written.

The card says nothing about shuffling.

## What happens

The "put the rest into your graveyard" step is a `ChangeZoneAll` from library with an explicit `Shuffle$ False` (`forge-gui/res/cardsfolder/w/wakanda_forever.txt:7`). `ChangeZoneAllEffect.java:109` and `:217` test whether `Shuffle` is present, not its value, so the spell shuffles the library, and, because the ability has no defined or targeted player, every player's library (in the farm row the opponent's untouched library was shuffled too). `DigUntilEffect.java:141` and `ManifestBaseEffect.java:72` test the parameter the same way. Values used for `Shuffle$` in the card corpus are only `True` and `False`. The one other `ChangeZoneAll` with `Shuffle$ False` (Elixir of Immortality) then shuffles once, through its own Shuffle step, instead of twice.

Found by cross-engine play: four farm rows (SUP); gorge and XMage keep library order.

## Repro

1. Player A has a library of known order; player B's library is also of known order.
2. A casts Wakanda Forever!, puts one permanent onto the battlefield, one into hand.
3. Expected: the remaining library cards keep their order, and so does B's library. Actual: both libraries are shuffled.

## Proposed fix

Test the parameter's value: shuffle only when `Shuffle` is `True` in ChangeZoneAll and Manifest, and skip it when it is `False` in DigUntil. Fork commit: [4df37cb4cf](https://github.com/adams-shaun/forge/commit/4df37cb4cf867ddf7a8ef9ee478944c0f855c4db). Untested (compiles against master).


Note: the mirror branch is built on upstream master but is not on GitHub yet: upstream master adds `.github/workflows/card-script-review.yml`, and the push token lacks the `workflow` scope. The linked commit is the same change on the fork's `gorge` branch.
