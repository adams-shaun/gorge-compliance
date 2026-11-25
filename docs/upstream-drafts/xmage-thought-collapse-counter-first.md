# Thought Collapse, Didn't Say Please: the three cards are milled before the countered spell reaches the graveyard

Engine: XMage (magefree/mage). Written against our fork pin base `11940683b90` (both card files are unchanged since `8ddb51df0c`); not yet re-checked against upstream master, and no mirror branch off upstream HEAD exists yet. Fork fix: `adams-shaun/mage:fix/thought-collapse-counter-first` at `5cc61aaba0b`.

## Oracle text

Thought Collapse ({3}{U}{U}, Instant) and Didn't Say Please ({1}{U}{U}, Instant):

> Counter target spell. Its controller mills three cards.

## Comprehensive Rules

> 608.2c ... the controller of the spell or ability follows its instructions in the order written.

> 701.6a To counter a spell or ability means to cancel it, removing it from the stack. It doesn't resolve and none of its effects occur. A countered spell is put into its owner's graveyard.

Countering and milling are two separate events, one after the other, not a simultaneous batch, so the owner has no CR 404.3
choice of graveyard order: the countered spell is in the graveyard first and the three milled cards go on top of it.

## What happens

`ThoughtCollapseEffect.apply` (`Mage.Sets/src/mage/cards/t/ThoughtCollapse.java`) and `DidntSayPleaseEffect.apply`
(`Mage.Sets/src/mage/cards/d/DidntSayPlease.java`) each call `player.millCards(3, source, game)` and only then
`CounterTargetEffect.apply`, so the milled cards enter the graveyard before the countered spell and it ends up on top.

Observed in an xproof replay (`xs:XP:thought-collapse:cast:s1`), the opponent's Reach Through Mists countered: expected
graveyard `[Reach Through Mists, Plains, Island, Plains]` (gorge and Forge agree), XMage gives
`[Plains, Island, Plains, Reach Through Mists]`.

Census: 268 card classes use `new CounterTargetEffect`; four also mill. Thought Collapse and Didn't Say Please are the
two wrong ones; InduceParanoia counters first, and Sokka's Haiku uses separate effects in printed order. PsychicStrike,
Countermand and TibaltsTrickery (counter then mill) also counter first, and BrokenAmbitions uses separate effects in
printed order.

## Fix

A shared `CounterTargetThenMillEffect(int millAmount, String text)`
(`Mage/src/main/java/mage/abilities/effects/common/`) reads the target's controller before the counter (the stack object
is gone afterwards), applies `CounterTargetEffect`, then mills. Both cards use it, so the order lives in one place.

## Repro

1. Player A casts Thought Collapse (or Didn't Say Please) targeting Player B's spell, with at least three cards in B's library.
2. Let it resolve.
3. Expected: B's graveyard holds the countered spell first, then the three milled cards on top. Actual: the three milled cards, then the countered spell on top.

`CounterThenMillOrderTest` covers both cards with a known top three (`Plains`, `Island`, `Plains`) and a countered
Lightning Bolt: on the unpatched classes it fails with expected `[Lightning Bolt, Plains, Island, Plains]` but was
`[Plains, Island, Plains, Lightning Bolt]`, and it passes after.
