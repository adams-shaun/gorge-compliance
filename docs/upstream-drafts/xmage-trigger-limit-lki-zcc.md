# "This ability triggers only once each turn" resets when the source dies in the triggering event

Engine: XMage (magefree/mage). Upstream base: master `2a96b0ac12` (2026-11-22). Mirror branch: [`adams-shaun/mage:upstream/trigger-limit-lki-zcc`](https://github.com/adams-shaun/mage/tree/upstream/trigger-limit-lki-zcc) at `9502c50322`.

## Oracle text

Grand Nagus Zek:

> Grand Nagus Zek gets +1/+1 for each Treasure you control.
> Whenever another creature dies, create a Treasure token. This ability triggers only once each turn. (It's an artifact with "{T}, Sacrifice this token: Add one mana of any color.")

## Comprehensive Rules

> 603.2. Whenever a game event or game state matches a triggered ability’s trigger event, that ability automatically triggers. The ability doesn’t do anything at this point. 

## What happens

`Mage/src/main/java/mage/abilities/TriggeredAbilityImpl.java` (at `2a96b0ac12`), `getKeyLastTurnTriggered`,
`getKeyLastTurnTriggeredCount` and `getKeyGameTriggeredCount`, key the trigger limit with
`CardUtil.getCardZoneString(..., getSourceId(), game)`, so with the card's current zone-change counter. A
leaves-the-battlefield trigger looks back in time (CR 603.10a). When its source dies at the same time as another
creature, the ability triggers as the battlefield ability, but its limit is then read under the graveyard
object's new counter. A limit already used up this turn reads as unused, and the ability triggers a second time.

The fix keys a leaves-the-battlefield trigger's limit by the zone-change counter of the permanent's last known
information. Our farm row: Grand Nagus Zek (one row). Battle-Scarred Survivalist and Morbid Opportunist show the
same thing.

## Repro

1. Player A controls Grand Nagus Zek, Grizzly Bears and Runeclaw Bear. Player B has Lightning Bolt, Fiery Cannonade
   and four Mountains.
2. B bolts Grizzly Bears: Zek triggers, one Treasure (the once-each-turn limit is now used up).
3. B casts Fiery Cannonade: Zek and Runeclaw Bear die together.
4. Expected: Zek sees Runeclaw Bear die but has already triggered this turn, so one Treasure total.
   Actual: it triggers again, two Treasures.

The three tests below fail on master (`Treasure Token ... expected:<1> but was:<2>`, and the Survivalist and
Opportunist equivalents) and pass with the fix.


## Proposed fix

```diff
diff --git a/Mage/src/main/java/mage/abilities/TriggeredAbilityImpl.java b/Mage/src/main/java/mage/abilities/TriggeredAbilityImpl.java
index fcef0db4a2..f937f69678 100644
--- a/Mage/src/main/java/mage/abilities/TriggeredAbilityImpl.java
+++ b/Mage/src/main/java/mage/abilities/TriggeredAbilityImpl.java
@@ -11,6 +11,7 @@ import mage.game.Game;
 import mage.game.events.BatchEvent;
 import mage.game.events.GameEvent;
 import mage.game.events.ZoneChangeEvent;
+import mage.game.permanent.Permanent;
 import mage.game.permanent.PermanentToken;
 import mage.players.Player;
 import mage.util.CardUtil;
@@ -80,23 +81,36 @@ public abstract class TriggeredAbilityImpl extends AbilityImpl implements Trigge
 
     // Used for triggers with a per-turn limit.
     private String getKeyLastTurnTriggered(Game game) {
-        return CardUtil.getCardZoneString(
-                "lastTurnTriggered|" + getOriginalId(), getSourceId(), game
-        );
+        return getTriggerLimitKey("lastTurnTriggered|" + getOriginalId(), game);
     }
 
     // Used for triggers with a per-turn limit.
     private String getKeyLastTurnTriggeredCount(Game game) {
-        return CardUtil.getCardZoneString(
-                "lastTurnTriggeredCount|" + getOriginalId(), getSourceId(), game
-        );
+        return getTriggerLimitKey("lastTurnTriggeredCount|" + getOriginalId(), game);
     }
 
     // Used for triggers with a per-game limit.
     private String getKeyGameTriggeredCount(Game game) {
-        return CardUtil.getCardZoneString(
-                "gameTriggeredCount|" + getOriginalId(), getSourceId(), game
-        );
+        return getTriggerLimitKey("gameTriggeredCount|" + getOriginalId(), game);
+    }
+
+    /**
+     * Key for a trigger limit counter. A leaves-the-battlefield trigger looks
+     * back in time (CR 603.10a), so the ability whose limit is checked is the
+     * one the source had immediately before it left the battlefield. Using the
+     * card's current zone-change counter would give the trigger a fresh counter
+     * for the zone it moved to and let "triggers only once each turn" fire
+     * again from the graveyard.
+     */
+    private String getTriggerLimitKey(String text, Game game) {
+        if (isLeavesTheBattlefieldTrigger()) {
+            Permanent source = game.getPermanentOrLKIBattlefield(getSourceId());
+            if (source != null) {
+                return CardUtil.getObjectZoneString(text, source.getId(), game,
+                        source.getZoneChangeCounter(game), false);
+            }
+        }
+        return CardUtil.getCardZoneString(text, getSourceId(), game);
     }
 
     private void updateTurnCount(Game game) {
```

## Test

Added with the fix: `Mage.Tests/src/test/java/org/mage/test/cards/single/trk/BattleScarredSurvivalistTest.java`

```java
package org.mage.test.cards.single.trk;

import mage.constants.PhaseStep;
import mage.constants.Zone;
import org.junit.Test;
import org.mage.test.serverside.base.CardTestPlayerBase;

/**
 * "Whenever another creature dies, put a +1/+1 counter on this creature.
 * This ability triggers only once each turn."
 */
public class BattleScarredSurvivalistTest extends CardTestPlayerBase {

    @Test
    public void testOnceEachTurnSurvivesItsOwnDeath() {
        addCard(Zone.BATTLEFIELD, playerA, "Battle-Scarred Survivalist");
        addCard(Zone.BATTLEFIELD, playerA, "Memnite"); // dies first, uses the once-per-turn trigger
        addCard(Zone.BATTLEFIELD, playerB, "Elite Vanguard"); // dies with the Survivalist
        addCard(Zone.HAND, playerA, "Doom Blade");
        addCard(Zone.HAND, playerA, "Wrath of God");
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 4);

        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Doom Blade", "Memnite", true); // uses the trigger
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Wrath of God");
        waitStackResolved(1, PhaseStep.PRECOMBAT_MAIN, playerA, true); // resolve only the sweeper
        checkStackSize("no second Survivalist trigger", 1, PhaseStep.PRECOMBAT_MAIN, playerA, 0);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
    }
}
```

Added with the fix: `Mage.Tests/src/test/java/org/mage/test/cards/single/trk/GrandNagusZekTest.java`

```java
package org.mage.test.cards.single.trk;

import mage.constants.PhaseStep;
import mage.constants.Zone;
import org.junit.Test;
import org.mage.test.serverside.base.CardTestPlayerBase;

/**
 * Grand Nagus Zek (TRK): "Whenever another creature dies, create a Treasure
 * token. This ability triggers only once each turn."
 *
 * CR 603.10a: leaves-the-battlefield abilities look back in time, so Zek
 * triggers for creatures that die at the same time as it does. CR 603.2: the
 * once-each-turn limit counts the triggerings of this one ability, and Zek
 * dying does not make it a different ability.
 */
public class GrandNagusZekTest extends CardTestPlayerBase {

    @Test
    public void testDiesWithAnotherCreatureAfterTriggeringDoesNotTriggerAgain() {
        setStrictChooseMode(true);
        addCard(Zone.BATTLEFIELD, playerA, "Grand Nagus Zek"); // 1/1, +1/+1 per Treasure
        addCard(Zone.BATTLEFIELD, playerA, "Grizzly Bears");
        addCard(Zone.BATTLEFIELD, playerA, "Runeclaw Bear");
        addCard(Zone.BATTLEFIELD, playerB, "Mountain", 4);
        addCard(Zone.HAND, playerB, "Lightning Bolt");
        addCard(Zone.HAND, playerB, "Fiery Cannonade");

        // the Bears die: Zek triggers (first time this turn), one Treasure, Zek is 2/2
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerB, "Lightning Bolt", "Grizzly Bears");
        waitStackResolved(1, PhaseStep.PRECOMBAT_MAIN);
        // 2 damage to each non-Pirate creature: Zek (2/2) and Runeclaw Bear die together
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerB, "Fiery Cannonade");

        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();

        assertGraveyardCount(playerA, "Grand Nagus Zek", 1);
        assertGraveyardCount(playerA, "Runeclaw Bear", 1);
        // the ability triggered only once this turn
        assertPermanentCount(playerA, "Treasure Token", 1);
    }
}
```

Added with the fix: `Mage.Tests/src/test/java/org/mage/test/cards/single/trk/MorbidOpportunistTest.java`

```java
package org.mage.test.cards.single.trk;

import mage.constants.PhaseStep;
import mage.constants.Zone;
import org.junit.Test;
import org.mage.test.serverside.base.CardTestPlayerBase;

/**
 * Shared regression for the per-turn trigger limit: the counter must survive
 * the source leaving the battlefield for a leaves-the-battlefield trigger.
 *
 * Morbid Opportunist: "Whenever one or more other creatures die, draw a card.
 * This ability triggers only once each turn."
 */
public class MorbidOpportunistTest extends CardTestPlayerBase {

    @Test
    public void testOnceEachTurnSurvivesItsOwnDeath() {
        addCard(Zone.BATTLEFIELD, playerA, "Morbid Opportunist");
        addCard(Zone.BATTLEFIELD, playerA, "Memnite"); // dies first, uses the once-per-turn trigger
        addCard(Zone.BATTLEFIELD, playerB, "Elite Vanguard"); // dies with the Opportunist
        addCard(Zone.HAND, playerA, "Doom Blade");
        addCard(Zone.HAND, playerA, "Wrath of God");
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 4);

        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Doom Blade", "Memnite", true); // draws one card
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Wrath of God"); // must not draw a second card
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();

        assertHandCount(playerA, 1);
    }
}
```
