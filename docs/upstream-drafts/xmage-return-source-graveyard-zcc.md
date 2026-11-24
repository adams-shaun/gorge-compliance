# Reassembling Skeleton and kin: a stale return ability brings back the new object

Engine: XMage (magefree/mage). Upstream base: master `2a96b0ac12` (2026-11-22). Mirror branch: [`adams-shaun/mage:upstream/return-source-graveyard-zcc`](https://github.com/adams-shaun/mage/tree/upstream/return-source-graveyard-zcc) at `4a60541464`.

## Oracle text

Reassembling Skeleton:

> {1}{B}: Return Reassembling Skeleton from your graveyard to the battlefield tapped.

## Comprehensive Rules

> 400.7. An object that moves from one zone to another becomes a new object with no memory of, or relation to, its previous existence. This rule has the following exceptions. 

## What happens

`Mage/src/main/java/mage/abilities/effects/common/ReturnSourceFromGraveyardToBattlefieldEffect.java:82` (at
`2a96b0ac12`) checks only `game.getState().getZone(source.getSourceId()) == Zone.GRAVEYARD`. If the card left the
graveyard after the ability was put on the stack and came back to the graveyard, it is a new object (CR 400.7), and
the ability on the stack should do nothing. XMage still moves it, because the card id is the same. The fix also
requires `source.getSourceObjectIfItStillExists(game) != null`, the zone-change-counter check XMage already uses
elsewhere.

69 card classes in Mage.Sets use this effect. Our farm rows: Hungry Ghoul (FDN) sacrificing a Reassembling
Skeleton that had returned while a second return activation waited on the stack (two rows).

## Repro

1. Player A controls Hungry Ghoul and six Swamps; Reassembling Skeleton is in A's graveyard.
2. A activates the Skeleton's return ability twice (both on the stack).
3. The top one resolves: the Skeleton returns tapped. With the second still on the stack, A sacrifices the Skeleton
   to Hungry Ghoul; it goes back to the graveyard as a new object.
4. Expected: the second return ability does nothing; the Skeleton stays in the graveyard.
   Actual: the Skeleton returns to the battlefield.

`ReassemblingSkeletonStaleReturnTest.testStaleReturnAbilityDoesNothing` below fails on master
(`(Battlefield) Permanents counts for PlayerA are not equal (Reassembling Skeleton) expected:<0> but was:<1>`) and
passes with the fix; `testReturnFromGraveyard` passes both ways.


## Proposed fix

```diff
diff --git a/Mage/src/main/java/mage/abilities/effects/common/ReturnSourceFromGraveyardToBattlefieldEffect.java b/Mage/src/main/java/mage/abilities/effects/common/ReturnSourceFromGraveyardToBattlefieldEffect.java
index c9914a98ae..aaf300618a 100644
--- a/Mage/src/main/java/mage/abilities/effects/common/ReturnSourceFromGraveyardToBattlefieldEffect.java
+++ b/Mage/src/main/java/mage/abilities/effects/common/ReturnSourceFromGraveyardToBattlefieldEffect.java
@@ -79,7 +79,8 @@ public class ReturnSourceFromGraveyardToBattlefieldEffect extends OneShotEffect
         if (player == null) {
             return false;
         }
-        if (game.getState().getZone(source.getSourceId()) == Zone.GRAVEYARD) {
+        if (game.getState().getZone(source.getSourceId()) == Zone.GRAVEYARD
+                && source.getSourceObjectIfItStillExists(game) != null) { // CR 400.7: not a new object
             player.moveCards(card, Zone.BATTLEFIELD, source, game, tapped, false, true, null);
             if (haste) {
                 Permanent permanent = CardUtil.getPermanentFromCardPutToBattlefield(card, game);
```

## Test

Added with the fix: `Mage.Tests/src/test/java/org/mage/test/cards/single/ReassemblingSkeletonStaleReturnTest.java`

```java
package org.mage.test.cards.single;

import mage.constants.PhaseStep;
import mage.constants.Zone;
import mage.counters.CounterType;
import org.junit.Test;
import org.mage.test.serverside.base.CardTestPlayerBase;

/**
 * Reassembling Skeleton: {1}{B}: Return Reassembling Skeleton from your graveyard to the battlefield tapped.
 * Hungry Ghoul: {1}, Sacrifice another creature: Put a +1/+1 counter on Hungry Ghoul.
 * <p>
 * CR 400.7: an object that moves from one zone to another becomes a new object with no memory of,
 * or relation to, its previous existence.
 *
 * @author adams-shaun
 */
public class ReassemblingSkeletonStaleReturnTest extends CardTestPlayerBase {

    private static final String skeleton = "Reassembling Skeleton";
    private static final String ghoul = "Hungry Ghoul";

    /**
     * Two return abilities (A1, A2) are put on the stack for the graveyard card. A2 resolves and the Skeleton
     * returns; it is sacrificed to the Ghoul and is a new object in the graveyard. A1 was put on the stack for the
     * old object, so it must do nothing.
     */
    @Test
    public void testStaleReturnAbilityDoesNothing() {
        addCard(Zone.BATTLEFIELD, playerA, ghoul);
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 6);
        addCard(Zone.GRAVEYARD, playerA, skeleton);

        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "{1}{B}: Return");
        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "{1}{B}: Return");
        checkStackSize("two return abilities", 1, PhaseStep.PRECOMBAT_MAIN, playerA, 2);
        waitStackResolved(1, PhaseStep.PRECOMBAT_MAIN, true); // A2 resolves, the Skeleton is back (tapped)
        checkPermanentCount("skeleton returned", 1, PhaseStep.PRECOMBAT_MAIN, playerA, skeleton, 1);

        setChoice(playerA, skeleton); // the creature sacrificed to Hungry Ghoul
        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "{1}, Sacrifice another creature");
        waitStackResolved(1, PhaseStep.PRECOMBAT_MAIN, true); // the Ghoul ability resolves, A1 still on the stack
        checkStackSize("stale return ability left", 1, PhaseStep.PRECOMBAT_MAIN, playerA, 1);
        checkGraveyardCount("skeleton sacrificed", 1, PhaseStep.PRECOMBAT_MAIN, playerA, skeleton, 1);

        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        setStrictChooseMode(true);
        execute();

        assertPermanentCount(playerA, skeleton, 0);
        assertGraveyardCount(playerA, skeleton, 1);
        assertCounterCount(playerA, ghoul, CounterType.P1P1, 1);
    }

    /**
     * A single return ability still returns the card: the check must not break the ordinary case.
     */
    @Test
    public void testReturnFromGraveyard() {
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 2);
        addCard(Zone.GRAVEYARD, playerA, skeleton);

        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "{1}{B}: Return");

        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        setStrictChooseMode(true);
        execute();

        assertPermanentCount(playerA, skeleton, 1);
        assertTapped(skeleton, true);
        assertGraveyardCount(playerA, skeleton, 0);
    }
}
```
