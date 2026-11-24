# Nihil Spellbomb and Declaration in Stone shuffle a library they never touch

Engine: Forge (Card-Forge/forge). Upstream base: master `9a91120cac` (2026-11-24). Mirror branch: `adams-shaun/forge:upstream/nihil-spellbomb-stray-shuffle` at `dc8661b125` (local; push pending, see note).

## Oracle text

Nihil Spellbomb:

> {T}, Sacrifice Nihil Spellbomb: Exile all cards from target player's graveyard.
> When Nihil Spellbomb is put into a graveyard from the battlefield, you may pay {B}. If you do, draw a card.

Declaration in Stone:

> Exile target creature and all other creatures its controller controls with the same name as that creature. That player investigates for each nontoken creature exiled this way.

## Comprehensive Rules

> 608.2c The controller of the spell or ability follows its instructions in the order written.

Neither card says to shuffle.

## What happens

Both scripts set `Shuffle$ True` on a `ChangeZoneAll` that does not involve a library: `forge-gui/res/cardsfolder/n/nihil_spellbomb.txt:4` (graveyard to exile) and `forge-gui/res/cardsfolder/d/declaration_in_stone.txt:5` (battlefield to exile). `ChangeZoneAllEffect.java:217` shuffles every affected player whenever `Shuffle` is present, so the targeted player's library is randomized. A grep of every `ChangeZoneAll` with `Shuffle$ True` and no library in Origin or Destination finds only these two scripts.

Found by cross-engine play: 25 farm rows (Nihil Spellbomb, the target's `library_top` field); gorge and XMage keep the library order.

## Repro

1. Player B has a library of known order and a non-empty graveyard.
2. Player A activates Nihil Spellbomb targeting B.
3. Expected: B's graveyard is exiled; B's library order is unchanged. Actual: B's library is shuffled.

## Proposed fix

Drop the `Shuffle$ True` parameter from both scripts. Fork commit: [ef11f0eef0](https://github.com/adams-shaun/forge/commit/ef11f0eef08ff375f779112dea9b67ef784c435b). Script-only, untested.


Note: the mirror branch is built on upstream master but is not on GitHub yet: upstream master adds `.github/workflows/card-script-review.yml`, and the push token lacks the `workflow` scope. The linked commit is the same change on the fork's `gorge` branch.
