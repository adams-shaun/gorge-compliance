# Illumination: the countered spell's controller gains no life

Engine: XMage (magefree/mage). Upstream base: master `2a96b0ac12` (2026-11-22). Mirror branch: [`adams-shaun/mage:upstream/illumination-lki-spell`](https://github.com/adams-shaun/mage/tree/upstream/illumination-lki-spell) at `0bbdf3932c`.

## Oracle text

Illumination:

> Counter target artifact or enchantment spell. Its controller gains life equal to its mana value.

## Comprehensive Rules

> 608.2h If an effect requires information from the game (such as the number of creatures on the battlefield), the answer is determined only once, when the effect is applied. If the effect requires information from a specific object, including the source of the ability itself, the effect uses the current information of that object if it’s in the public zone it was expected to be in; if it’s no longer in that zone, or if the effect has moved it from a public zone to a hidden zone, the effect uses the object’s last known information. See rule 113.7a. If an ability states that an object does something, it’s the object as it exists—or as it most recently existed—that does it, not the ability. 

## What happens

Illumination's spell ability is `CounterTargetEffect` followed by `IlluminationEffect`
(`Mage.Sets/src/mage/cards/i/Illumination.java:37-41` at `2a96b0ac12`). When `IlluminationEffect.apply`
(`:72-90`) runs, the target spell has already been countered and is in its owner's graveyard, so
`game.getControllerId(targetId)` (`:76`) returns nothing, `controller` stays null, and no life is gained. The
effect's own `game.getStack().counter(...)` (`:78-79`) is a leftover that cannot succeed. CR 608.2h: when the object
has left the zone, the effect uses its last known information. `game.getSpellOrLKIStack` gives it.

Our farm row: Illumination countering an artifact spell (one row).

## Repro

1. Player B casts an artifact or enchantment spell. Player A casts Illumination targeting it.
2. Expected: the spell is countered and B gains life equal to its mana value. Actual: it is countered, B gains
   nothing.

`IlluminationTest` below (both tests) fails on master (`Life amounts are not equal ... expected:<21> but was:<20>`)
and passes with the fix.


## Proposed fix

```diff
diff --git a/Mage.Sets/src/mage/cards/i/Illumination.java b/Mage.Sets/src/mage/cards/i/Illumination.java
index 31280d5dbd..6c03ba85b7 100644
--- a/Mage.Sets/src/mage/cards/i/Illumination.java
+++ b/Mage.Sets/src/mage/cards/i/Illumination.java
@@ -69,25 +69,17 @@ class IlluminationEffect extends OneShotEffect {
 
     @Override
     public boolean apply(Game game, Ability source) {
-        UUID targetId = source.getFirstTarget();
-        Player controller = null;
-        boolean countered = false;
-        if (targetId != null) {
-            controller = game.getPlayer(game.getControllerId(targetId));
+        // CounterTargetEffect already countered the spell, so it is no longer on the stack.
+        // CR 608.2h: use its last known information for the mana value and the controller.
+        Spell spell = game.getSpellOrLKIStack(getTargetPointer().getFirst(game, source));
+        if (spell == null) {
+            return false;
         }
-        if (targetId != null
-                && game.getStack().counter(targetId, source, game)) {
-            countered = true;
+        Player player = game.getPlayer(spell.getControllerId());
+        if (player == null) {
+            return false;
         }
-        if (controller != null) {
-            Spell spell = game.getStack().getSpell(getTargetPointer().getFirst(game, source));
-            int cost = spell.getManaValue();
-            Player player = game.getPlayer(spell.getControllerId());
-            if (player != null) {
-                player.gainLife(cost, game, source);
-            }
-            return true;
-        }
-        return countered;
+        player.gainLife(spell.getManaValue(), game, source);
+        return true;
     }
 }
```

## Test

Added with the fix: `Mage.Tests/src/test/java/org/mage/test/cards/single/all/IlluminationTest.java`

```java
package org.mage.test.cards.single.all;

import mage.constants.PhaseStep;
import mage.constants.Zone;
import org.junit.Test;
import org.mage.test.serverside.base.CardTestPlayerBase;

/**
 * {@link mage.cards.i.Illumination}
 * Illumination {W}{W}, Instant: Counter target artifact or enchantment spell.
 * Its controller gains life equal to its mana value.
 * <p>
 * CR 608.2h: once the countered spell has left the stack, the effect uses its
 * last known information, so the controller still gains life equal to its mana value.
 */
public class IlluminationTest extends CardTestPlayerBase {

    private static final String illumination = "Illumination";

    @Test
    public void testControllerGainsManaValueOfCounteredArtifact() {
        setStrictChooseMode(true);
        addCard(Zone.HAND, playerA, "Sol Ring"); // {1}, mana value 1
        addCard(Zone.BATTLEFIELD, playerA, "Island", 1);
        addCard(Zone.HAND, playerB, illumination);
        addCard(Zone.BATTLEFIELD, playerB, "Plains", 2);

        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Sol Ring");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerB, illumination, "Sol Ring", "Sol Ring");

        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();

        assertGraveyardCount(playerA, "Sol Ring", 1);
        assertGraveyardCount(playerB, illumination, 1);
        assertLife(playerA, 21);
        assertLife(playerB, 20);
    }

    @Test
    public void testControllerGainsManaValueOfCounteredEnchantment() {
        setStrictChooseMode(true);
        addCard(Zone.HAND, playerA, "Pacifism"); // {1}{W}, mana value 2
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 2);
        addCard(Zone.BATTLEFIELD, playerB, "Grizzly Bears");
        addCard(Zone.HAND, playerB, illumination);
        addCard(Zone.BATTLEFIELD, playerB, "Plains", 2);

        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Pacifism", "Grizzly Bears");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerB, illumination, "Pacifism", "Pacifism");

        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();

        assertGraveyardCount(playerA, "Pacifism", 1);
        assertLife(playerA, 22);
    }
}
```
