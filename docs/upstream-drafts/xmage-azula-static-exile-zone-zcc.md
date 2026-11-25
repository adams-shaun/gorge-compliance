# Azula, Cunning Usurper: the cast permission reads the zcc-0 exile zone (DRAFT, UNVERIFIED)

Engine: XMage (magefree/mage). Written against fork pin `98052067d470` (`Mage.Sets/src/mage/cards/a/AzulaCunningUsurper.java`,
blob `46aab3bb2672dd2c6c93b8684cf17d188888d31d`, identical at `4142596fb72b`). No fork branch exists yet. This text was drafted from a
read-only checkout: the patch and the test below have NOT been compiled or run.

## Oracle text

> When Azula enters, target opponent exiles a nontoken creature they control, then they exile a nonland card from their graveyard.
> During your turn, you may cast cards exiled with Azula and you may cast them as though they had flash. Mana of any type can be
> spent to cast those spells.

## What happens

`AzulaCunningUsurperExileEffect` (the ETB) exiles into `CardUtil.getExileZoneId(game, source)`; the resolving trigger's
`getStackMomentSourceZCC()` is the entering permanent's zcc. `AzulaCunningUsurperCastEffect`, `...FlashEffect` and `...ManaEffect`
call the same method from the *static* ability, which XMage registers at game load (`GameState.addAbility` ->
`ContinuousEffects.addEffect`) with zcc 0 (the `CardUtil.getExileZoneId` javadoc warns about exactly this). They look in an empty
zone, so `applies` is never true and the exiled card cannot be cast. Real-game bug, not a harness artefact.

Compliance repro: xproof find `xs_XP_azula-cunning-usurper_cast_s1`, step 8, `Can't find ability to activate command: Cast
@p1:Shield Sphere`. The driver currently masks it with `ScenarioReplay.repairStaticSourceZCC`.

## Proposed fix

One helper in the card, used by the three static effects (the ETB keeps `getExileZoneId(game, source)`, the trigger's own zcc):

```java
// Static abilities register with zcc 0, so key the zone on the source permanent's current counter.
static UUID azulaExileZoneId(Game game, Ability source) {
    return CardUtil.getExileZoneId(game, source.getSourceId(), game.getState().getZoneChangeCounter(source.getSourceId()));
}
```

and replace `CardUtil.getExileZoneId(game, source)` with `azulaExileZoneId(game, source)` in the three `applies` methods.

Test (`Mage.Tests/src/test/java/org/mage/test/cards/single/tla/AzulaCunningUsurperTest.java`), to fail before and pass after:

```java
@Test
public void testCastOpponentOwnedExiledCard() {
    addCard(Zone.HAND, playerA, "Azula, Cunning Usurper");
    addCard(Zone.BATTLEFIELD, playerA, "Island", 1);
    addCard(Zone.BATTLEFIELD, playerA, "Swamp", 6);
    addCard(Zone.BATTLEFIELD, playerB, "Grizzly Bears");

    castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Azula, Cunning Usurper");
    addTarget(playerA, playerB); // the ETB's TargetOpponent; strict choose mode does not pick it
    setChoice(playerB, "Grizzly Bears");
    waitStackResolved(1, PhaseStep.PRECOMBAT_MAIN);
    castSpell(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Grizzly Bears");

    setStopAt(1, PhaseStep.END_TURN);
    setStrictChooseMode(true);
    execute();
    assertAllCommandsUsed();

    assertPermanentCount(playerA, "Grizzly Bears", 1);
    assertPermanentCount(playerB, "Grizzly Bears", 0);
}
```

Land budget: Azula costs {2}{U}{B}{B} (5 mana), the Bears {1}{G} (2), so the test supplies 7 lands (1 Island, 6 Swamps). The Bears
are paid with Swamp mana on purpose: there is no green source, so the cast only succeeds if the Mana effect's `applies` also
sees the exile zone, which makes the test cover the Cast and Mana `applies` changes (the Flash effect is not exercised: the cast is at sorcery timing). Azula does not add mana; it only lets any type pay.

Needs a check that the choice prompt order matches (nontoken creature first; the graveyard is empty so no second prompt).

## After it lands

Bump `XMAGE_REF` in the compliance Makefile, then delete `ScenarioReplay.repairStaticSourceZCC` and its `mayplay` call site, or keep it
with a comment naming the fixed fork commit. Re-run `scripts/xmage-oracle-run.sh` on the xproof find.
