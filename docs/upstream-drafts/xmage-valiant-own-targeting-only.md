# Valiant does not trigger once an opponent has targeted the creature that turn

Engine: XMage (magefree/mage). Upstream base: master `2a96b0ac12` (2026-11-22). Mirror branch: [`adams-shaun/mage:upstream/valiant-own-targeting-only`](https://github.com/adams-shaun/mage/tree/upstream/valiant-own-targeting-only) at `edaada6675`.

## Oracle text

Nettle Guard:

> Valiant — Whenever Nettle Guard becomes the target of a spell or ability you control for the first time each turn, it gets +0/+2 until end of turn.
> {1}, Sacrifice Nettle Guard: Destroy target artifact or enchantment.

## Comprehensive Rules

> 603.2. Whenever a game event or game state matches a triggered ability’s trigger event, that ability automatically triggers. The ability doesn’t do anything at this point. 

## What happens

`Mage/src/main/java/mage/abilities/common/BecomesTargetSourceFirstTimeTriggeredAbility.java:36` (at `2a96b0ac12`)
refuses to trigger when `NumberOfTimesPermanentTargetedATurnWatcher.numTimesTargetedThisTurn(permanent, game) > 1`.
The watcher counts every spell and ability that targeted the permanent, whoever controls it.
`ValiantTriggeredAbility` extends this class unchanged. Valiant reads "becomes the target of a spell or ability
**you control** for the first time each turn", so only its controller's targeting counts: an opponent's spell
that targets the creature first must not use up the trigger.

Cards in our farm rows: Repel Calamity, High Stride and Crumb and Get It targeting a Valiant creature after an
opponent had (three rows). The same applies to every user of `ValiantTriggeredAbility`.

## Repro

1. Player A controls Nettle Guard (3/1). Both players have High Stride in hand and a Forest.
2. In A's upkeep, B casts High Stride targeting Nettle Guard (+1/+3): 4/4.
3. In A's main phase, A casts High Stride targeting Nettle Guard.
4. Expected: Valiant triggers (first time this turn a spell A controls targets it): 5/7 + 0/2 = 5/9.
   Actual: no trigger, 5/7.

`ValiantFirstTimeTest.testOpponentTargetingFirstDoesNotUseUpValiant` below fails on master ("found similar ...
power=5 toughness=7") and passes with the fix; `testSecondOwnTargetingDoesNotTriggerValiant` passes both ways.


## Proposed fix

```diff
diff --git a/Mage/src/main/java/mage/abilities/common/BecomesTargetSourceFirstTimeTriggeredAbility.java b/Mage/src/main/java/mage/abilities/common/BecomesTargetSourceFirstTimeTriggeredAbility.java
index bf895161c7..407b73f8c1 100644
--- a/Mage/src/main/java/mage/abilities/common/BecomesTargetSourceFirstTimeTriggeredAbility.java
+++ b/Mage/src/main/java/mage/abilities/common/BecomesTargetSourceFirstTimeTriggeredAbility.java
@@ -33,9 +33,17 @@ public class BecomesTargetSourceFirstTimeTriggeredAbility extends BecomesTargetS
     public boolean checkTrigger(GameEvent event, Game game) {
         Permanent permanent = game.getPermanent(event.getTargetId());
         NumberOfTimesPermanentTargetedATurnWatcher watcher = game.getState().getWatcher(NumberOfTimesPermanentTargetedATurnWatcher.class);
-        if (permanent == null || watcher == null || watcher.numTimesTargetedThisTurn(permanent, game) > 1) {
+        if (permanent == null || watcher == null || numTimesTargetedThisTurn(watcher, permanent, game) > 1) {
             return false;
         }
         return super.checkTrigger(event, game);
     }
+
+    /**
+     * How often the permanent was targeted this turn by the objects the
+     * trigger's "first time" counts: every spell and ability by default.
+     */
+    protected int numTimesTargetedThisTurn(NumberOfTimesPermanentTargetedATurnWatcher watcher, Permanent permanent, Game game) {
+        return watcher.numTimesTargetedThisTurn(permanent, game);
+    }
 }
diff --git a/Mage/src/main/java/mage/abilities/common/ValiantTriggeredAbility.java b/Mage/src/main/java/mage/abilities/common/ValiantTriggeredAbility.java
index fdf3befd50..3a1691b992 100644
--- a/Mage/src/main/java/mage/abilities/common/ValiantTriggeredAbility.java
+++ b/Mage/src/main/java/mage/abilities/common/ValiantTriggeredAbility.java
@@ -5,6 +5,9 @@ import mage.constants.AbilityWord;
 import mage.constants.SetTargetPointer;
 import mage.constants.TargetController;
 import mage.filter.FilterStackObject;
+import mage.game.Game;
+import mage.game.permanent.Permanent;
+import mage.watchers.common.NumberOfTimesPermanentTargetedATurnWatcher;
 
 /**
  * @author xenohedron, TheElk801
@@ -32,6 +35,16 @@ public class ValiantTriggeredAbility extends BecomesTargetSourceFirstTimeTrigger
         super(ability);
     }
 
+    /**
+     * Valiant's "first time each turn" counts only spells and abilities its
+     * controller controls: an opponent targeting the creature first does not
+     * use it up.
+     */
+    @Override
+    protected int numTimesTargetedThisTurn(NumberOfTimesPermanentTargetedATurnWatcher watcher, Permanent permanent, Game game) {
+        return watcher.numTimesTargetedThisTurnBy(permanent, getControllerId(), game);
+    }
+
     @Override
     public ValiantTriggeredAbility copy() {
         return new ValiantTriggeredAbility(this);
diff --git a/Mage/src/main/java/mage/watchers/common/NumberOfTimesPermanentTargetedATurnWatcher.java b/Mage/src/main/java/mage/watchers/common/NumberOfTimesPermanentTargetedATurnWatcher.java
index 08e5276cac..5ee3e679f4 100644
--- a/Mage/src/main/java/mage/watchers/common/NumberOfTimesPermanentTargetedATurnWatcher.java
+++ b/Mage/src/main/java/mage/watchers/common/NumberOfTimesPermanentTargetedATurnWatcher.java
@@ -8,8 +8,11 @@ import mage.game.permanent.Permanent;
 import mage.game.stack.StackObject;
 import mage.watchers.Watcher;
 
+import java.util.ArrayList;
 import java.util.HashMap;
+import java.util.List;
 import java.util.Map;
+import java.util.UUID;
 
 /**
  *
@@ -17,7 +20,7 @@ import java.util.Map;
  */
 public class NumberOfTimesPermanentTargetedATurnWatcher extends Watcher {
 
-    private final Map<MageObjectReference, Integer> permanentsTargeted = new HashMap<>();
+    private final Map<MageObjectReference, List<UUID>> permanentsTargeted = new HashMap<>();
 
     public NumberOfTimesPermanentTargetedATurnWatcher() {
         super(WatcherScope.GAME);
@@ -35,13 +38,24 @@ public class NumberOfTimesPermanentTargetedATurnWatcher extends Watcher {
         Permanent permanent = game.getPermanent(event.getTargetId());
         if (permanent != null) {
             MageObjectReference mor = new MageObjectReference(permanent, game);
-            int nTimes = permanentsTargeted.getOrDefault(mor, 0);
-            permanentsTargeted.put(mor, nTimes + 1);
+            permanentsTargeted.computeIfAbsent(mor, k -> new ArrayList<>()).add(targetingObject.getControllerId());
         }
     }
 
     public int numTimesTargetedThisTurn(Permanent permanent, Game game) {
-        return permanentsTargeted.getOrDefault(new MageObjectReference(permanent, game), 0);
+        return permanentsTargeted.getOrDefault(new MageObjectReference(permanent, game), new ArrayList<>()).size();
+    }
+
+    /**
+     * Number of times the permanent was targeted this turn by spells and
+     * abilities controlled by the given player (CR 603.2: "spell or ability
+     * you control" counts only that player's own targeting).
+     */
+    public int numTimesTargetedThisTurnBy(Permanent permanent, UUID controllerId, Game game) {
+        return (int) permanentsTargeted.getOrDefault(new MageObjectReference(permanent, game), new ArrayList<>())
+                .stream()
+                .filter(controllerId::equals)
+                .count();
     }
 
     @Override
```

## Test

Added with the fix: `Mage.Tests/src/test/java/org/mage/test/cards/single/blb/ValiantFirstTimeTest.java`

```java
package org.mage.test.cards.single.blb;

import mage.constants.PhaseStep;
import mage.constants.Zone;
import org.junit.Test;
import org.mage.test.serverside.base.CardTestPlayerBase;

/**
 * Valiant (BLB), Nettle Guard: "Whenever Nettle Guard becomes the target of a
 * spell or ability you control for the first time each turn, it gets +0/+2
 * until end of turn."
 *
 * The count is of spells and abilities its controller controls: a targeting by
 * the opponent's spell earlier in the turn is not a "spell or ability you
 * control" and does not use up the first time (Bloomburrow release notes,
 * Valiant rulings). Forge counts per targeting player (Card.isValiant).
 */
public class ValiantFirstTimeTest extends CardTestPlayerBase {

    // High Stride: target creature gets +1/+3 and gains reach until end of turn. Untap it.
    @Test
    public void testOpponentTargetingFirstDoesNotUseUpValiant() {
        setStrictChooseMode(true);
        addCard(Zone.BATTLEFIELD, playerA, "Forest", 1);
        addCard(Zone.BATTLEFIELD, playerB, "Forest", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Nettle Guard"); // 3/1
        addCard(Zone.HAND, playerA, "High Stride");
        addCard(Zone.HAND, playerB, "High Stride");

        castSpell(1, PhaseStep.UPKEEP, playerB, "High Stride", "Nettle Guard");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "High Stride", "Nettle Guard");

        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        setStrictChooseMode(true);
        execute();

        // 3/1 +1/+3 +1/+3, valiant +0/+2 for playerA's own targeting only
        assertPowerToughness(playerA, "Nettle Guard", 5, 9);
    }

    @Test
    public void testSecondOwnTargetingDoesNotTriggerValiant() {
        setStrictChooseMode(true);
        addCard(Zone.BATTLEFIELD, playerA, "Forest", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Nettle Guard"); // 3/1
        addCard(Zone.HAND, playerA, "High Stride", 2);

        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "High Stride", "Nettle Guard");
        waitStackResolved(1, PhaseStep.PRECOMBAT_MAIN);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "High Stride", "Nettle Guard");

        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();

        // 3/1 +1/+3 +1/+3, valiant +0/+2 once
        assertPowerToughness(playerA, "Nettle Guard", 5, 9);
    }
}
```
