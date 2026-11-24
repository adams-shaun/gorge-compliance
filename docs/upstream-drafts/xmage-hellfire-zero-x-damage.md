# Hellfire deals no damage to you when no creature died

Engine: XMage (magefree/mage). Upstream base: master `2a96b0ac12` (2026-11-22). Mirror branch: [`adams-shaun/mage:upstream/hellfire-zero-x-damage`](https://github.com/adams-shaun/mage/tree/upstream/hellfire-zero-x-damage) at `a0a013e175`.

## Oracle text

Hellfire:

> Destroy all nonblack creatures. Hellfire deals X plus 3 damage to you, where X is the number of creatures that died this way.

## Comprehensive Rules

> 608.2c The controller of the spell or ability follows its instructions in the order written. However, replacement effects may modify these actions. In some cases, later text on the card may modify the meaning of earlier text (for example, “Destroy target creature. It can’t be regenerated” or “Counter target spell. If that spell is countered this way, put it on top of its owner’s library instead of into its owner’s graveyard.”) Don’t just apply effects step by step without thinking in these cases—read the whole text and apply the rules of English to the text. 

## What happens

`Mage.Sets/src/mage/cards/h/Hellfire.java:67` (at `2a96b0ac12`) wraps the damage in
`if (destroyedCreature > 0)`. The Oracle text deals "X plus 3 damage to you": with X = 0 it still deals 3.

Our farm row: Hellfire cast with no nonblack creature on the battlefield (one row).

## Repro

1. Player A controls five Swamps and no creatures; no player controls a nonblack creature. A casts Hellfire.
2. Expected: A takes 3 damage (20 to 17). Actual: A stays at 20.

`HellfireTest.test_NoCreatureDies_StillDealsThree` below fails on master
(`Life amounts are not equal for player PlayerA expected:<17> but was:<20>`) and passes with the fix;
`test_CreaturesDie_DealsXPlusThree` passes both ways.


## Proposed fix

```diff
diff --git a/Mage.Sets/src/mage/cards/h/Hellfire.java b/Mage.Sets/src/mage/cards/h/Hellfire.java
index 45ac442928..e1c35fb8db 100644
--- a/Mage.Sets/src/mage/cards/h/Hellfire.java
+++ b/Mage.Sets/src/mage/cards/h/Hellfire.java
@@ -64,9 +64,8 @@ class HellfireEffect extends OneShotEffect {
                     destroyedCreature++;
                 }
             }
-            if (destroyedCreature > 0) {
-                new DamageControllerEffect(destroyedCreature + 3).apply(game, source);
-            }
+            // X may be 0 (nothing died), it still deals 3 damage (CR 608.2c)
+            new DamageControllerEffect(destroyedCreature + 3).apply(game, source);
             return true;
         }
         return false;
```

## Test

Added with the fix: `Mage.Tests/src/test/java/org/mage/test/cards/single/leg/HellfireTest.java`

```java
package org.mage.test.cards.single.leg;

import mage.constants.PhaseStep;
import mage.constants.Zone;
import org.junit.Test;
import org.mage.test.serverside.base.CardTestPlayerBase;

/**
 * @author adams-shaun
 */
public class HellfireTest extends CardTestPlayerBase {

    /**
     * {@link mage.cards.h.Hellfire Hellfire} {2}{B}{B}{B}
     * Sorcery
     * Destroy all nonblack creatures. Hellfire deals X plus 3 damage to you,
     * where X is the number of creatures that died this way.
     */
    private static final String hellfire = "Hellfire";

    @Test
    public void test_NoCreatureDies_StillDealsThree() {
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 5);
        addCard(Zone.HAND, playerA, hellfire);

        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, hellfire);

        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();

        assertGraveyardCount(playerA, hellfire, 1);
        assertLife(playerA, 20 - 3);
    }

    @Test
    public void test_CreaturesDie_DealsXPlusThree() {
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 5);
        addCard(Zone.BATTLEFIELD, playerA, "Hill Giant"); // nonblack, dies
        addCard(Zone.BATTLEFIELD, playerB, "Grizzly Bears"); // nonblack, dies
        addCard(Zone.BATTLEFIELD, playerB, "Vampire Bats"); // black, survives
        addCard(Zone.HAND, playerA, hellfire);

        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, hellfire);

        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();

        assertPermanentCount(playerA, "Hill Giant", 0);
        assertPermanentCount(playerB, "Grizzly Bears", 0);
        assertPermanentCount(playerB, "Vampire Bats", 1);
        assertLife(playerA, 20 - 5);
    }
}
```
