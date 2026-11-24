# Biorganic Carapace: the granted draw trigger fires on combat damage to a creature

Engine: XMage (magefree/mage). Upstream base: master `2a96b0ac12` (2026-11-22). Mirror branch: [`adams-shaun/mage:upstream/biorganic-carapace`](https://github.com/adams-shaun/mage/tree/upstream/biorganic-carapace) at `095c41cee6`.

## Oracle text

Biorganic Carapace:

> When this Equipment enters, attach it to target creature you control.
> Equipped creature gets +2/+2 and has "Whenever this creature deals combat damage to a player, draw a card for each modified creature you control." (Equipment, Auras you control, and counters are modifications.)
> Equip {2}

## Comprehensive Rules

> 603.2. Whenever a game event or game state matches a triggered ability’s trigger event, that ability automatically triggers. The ability doesn’t do anything at this point. 

> 510.1c A blocked creature assigns its combat damage to the creatures blocking it. If no creatures are currently blocking it (if, for example, they were destroyed or removed from combat), it assigns no combat damage. If exactly one creature is blocking it, it assigns all its combat damage to that creature. If two or more creatures are blocking it, it assigns its combat damage to those creatures divided as its controller chooses among them. Example: An attacking Elvish Regrower (a 4/3 creature) is blocked by Vampire Spawn (a 2/3 creature) and Helpful Hunter (a 1/1 creature). Elvish Regrower’s controller can assign all 4 damage to the Hunter, 1 damage to the Spawn and 3 damage to the Hunter, 2 damage to each creature, 3 damage to the Spawn and 1 damage to the Hunter, or all 4 damage to the Spawn. 

## What happens

`Mage.Sets/src/mage/cards/b/BiorganicCarapace.java:42` (at `2a96b0ac12`) builds the granted ability as
`DealsCombatDamageTriggeredAbility`, which triggers on combat damage to any object. The Oracle text says "deals
combat damage to a player". So an equipped creature that is blocked, and deals its damage only to the blocker,
still draws cards. The class `DealsCombatDamageToAPlayerTriggeredAbility` is the one that matches the printed text.

Found by cross-engine play (gorge vs Forge vs XMage). Nine farm rows across the SPM cards that share a game with
the Carapace (Spider-Bot, Wraith, Venomized Cat, SP//dr, Oscorp Research Team, Living Brain, Doc Ock's Henchmen,
Daily Bugle Reporters) trace to this one ability.

## Repro

1. Player A controls Biorganic Carapace attached to Grizzly Bears. Player B controls Grizzly Bears.
2. A attacks with the equipped Bears; B blocks with its Bears.
3. Combat damage: A's Bears deal 4 to B's Bears, none to B.
4. Expected: no trigger, A's hand stays at 0. Actual: the trigger resolves and A draws 1.

`BiorganicCarapaceTest.testBlockedEquippedCreatureDoesNotDraw` below fails on master
(`(Hand PlayerA) Card counts are not equal expected:<0> but was:<1>`) and passes with the fix; the unblocked case
passes both ways.


## Proposed fix

```diff
diff --git a/Mage.Sets/src/mage/cards/b/BiorganicCarapace.java b/Mage.Sets/src/mage/cards/b/BiorganicCarapace.java
index b27b1cd90f..843b220140 100644
--- a/Mage.Sets/src/mage/cards/b/BiorganicCarapace.java
+++ b/Mage.Sets/src/mage/cards/b/BiorganicCarapace.java
@@ -1,7 +1,7 @@
 package mage.cards.b;
 
 import mage.abilities.Ability;
-import mage.abilities.common.DealsCombatDamageTriggeredAbility;
+import mage.abilities.common.DealsCombatDamageToAPlayerTriggeredAbility;
 import mage.abilities.common.EntersBattlefieldAttachToTarget;
 import mage.abilities.common.SimpleStaticAbility;
 import mage.abilities.dynamicvalue.common.PermanentsOnBattlefieldCount;
@@ -39,7 +39,7 @@ public final class BiorganicCarapace extends CardImpl {
         this.addAbility(new EntersBattlefieldAttachToTarget());
 
         // Equipped creature gets +2/+2 and has "Whenever this creature deals combat damage to a player, draw a card for each modified creature you control."
-        Ability gainedAbility = new DealsCombatDamageTriggeredAbility(new DrawCardSourceControllerEffect(new PermanentsOnBattlefieldCount(filter)), false);
+        Ability gainedAbility = new DealsCombatDamageToAPlayerTriggeredAbility(new DrawCardSourceControllerEffect(new PermanentsOnBattlefieldCount(filter)), false);
         Ability equipAbility = new SimpleStaticAbility(new BoostEquippedEffect(2, 2));
         equipAbility.addEffect(new GainAbilityAttachedEffect(gainedAbility, null)
                 .concatBy("and"));
```

## Test

Added with the fix: `Mage.Tests/src/test/java/org/mage/test/cards/single/spm/BiorganicCarapaceTest.java`

```java
package org.mage.test.cards.single.spm;

import mage.constants.PhaseStep;
import mage.constants.Zone;
import org.junit.Test;
import org.mage.test.serverside.base.CardTestPlayerBase;

/**
 * Biorganic Carapace (SPM): "Equipped creature gets +2/+2 and has 'Whenever
 * this creature deals combat damage to a player, draw a card for each
 * modified creature you control.'"
 *
 * CR 120.2a: combat damage is dealt by attacking and blocking creatures.
 * CR 510.1c: a blocked creature assigns its combat damage to the creatures
 * blocking it, so that damage is not dealt to a player.
 * CR 603.2: a triggered ability triggers only when its trigger event occurs;
 * "deals combat damage to a player" does not match damage dealt to a creature.
 */
public class BiorganicCarapaceTest extends CardTestPlayerBase {

    private void setUpEquippedBears() {
        addCard(Zone.BATTLEFIELD, playerA, "Forest", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Biorganic Carapace");
        addCard(Zone.BATTLEFIELD, playerA, "Grizzly Bears");
        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Equip", "Grizzly Bears");
    }

    @Test
    public void testBlockedEquippedCreatureDoesNotDraw() {
        setStrictChooseMode(true);
        setUpEquippedBears();
        addCard(Zone.BATTLEFIELD, playerB, "Grizzly Bears");

        attack(1, playerA, "Grizzly Bears");
        block(1, playerB, "Grizzly Bears", "Grizzly Bears");

        setStopAt(1, PhaseStep.END_TURN);
        execute();

        assertHandCount(playerA, 0);
    }

    @Test
    public void testUnblockedEquippedCreatureDraws() {
        setStrictChooseMode(true);
        setUpEquippedBears();

        attack(1, playerA, "Grizzly Bears");

        setStopAt(1, PhaseStep.END_TURN);
        execute();

        assertHandCount(playerA, 1);
    }
}
```
