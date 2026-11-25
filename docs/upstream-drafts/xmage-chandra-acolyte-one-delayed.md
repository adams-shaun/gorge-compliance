# Chandra, Acolyte of Flame: the 0 makes one end-step sacrifice trigger per token

Engine: XMage (magefree/mage). Written against our fork pin `27b8bfe661` (the card file is unchanged since); not yet re-checked against upstream master, and no mirror branch off upstream HEAD exists yet. Fork fix: `adams-shaun/mage:fix/chandra-acolyte-one-delayed` at `98052067d4`.

## Oracle text

Chandra, Acolyte of Flame, second 0:

> Create two 1/1 red Elemental creature tokens. They gain haste. Sacrifice them at the beginning of the next end step.

## Comprehensive Rules

> 603.7a Delayed triggered abilities are created by the resolution of a spell or ability. ... A delayed triggered ability that refers to a particular object still affects it even if the object changes characteristics, but not if it has left the zone it was in. 

One resolving instruction ("Sacrifice them ...") creates one delayed triggered ability, which sacrifices both tokens when it resolves.

## What happens

`ChandraAcolyteOfFlameEffect.apply` (`Mage.Sets/src/mage/cards/c/ChandraAcolyteOfFlame.java`) calls
`game.addDelayedTriggeredAbility(new AtTheBeginOfNextEndStepDelayedTriggeredAbility(effect2), source)` inside its
`token.getLastAddedTokenIds()` loop, once per token, each with a `FixedTarget` of its own. At the beginning of the
end step two triggers go on the stack and the controller is asked to order them. `CreateTokenEffect.removeTokensCreatedAt`
already builds the right shape: one `SacrificeTargetEffect` over `FixedTargets` of every created token.

Same pattern, other cards (a `getLastAddedTokenIds` loop that registers a delayed trigger per token for an instruction
that created several): Firecat Blitz (X tokens), Force of Rage (two, at your next upkeep), Thatcher Revolt (three),
Waylay (three, next cleanup step). Not fixed here.

## Repro

1. Player A controls Chandra, Acolyte of Flame and activates the second 0.
2. Go to the end step.
3. Expected: one delayed trigger on the stack. Actual: two.

`ChandraAcolyteOfFlameTest.testOneDelayedSacrificeForBothTokens` fails before
(`Missing CHOICE def for turn 1, step END_TURN, PlayerA / Triggered list (total 2)`) and passes after.

## Proposed fix and test

```diff
diff --git a/Mage.Sets/src/mage/cards/c/ChandraAcolyteOfFlame.java b/Mage.Sets/src/mage/cards/c/ChandraAcolyteOfFlame.java
index 96c88da3de7..7c9819ceb8b 100644
--- a/Mage.Sets/src/mage/cards/c/ChandraAcolyteOfFlame.java
+++ b/Mage.Sets/src/mage/cards/c/ChandraAcolyteOfFlame.java
@@ -29,7 +29,10 @@ import mage.game.permanent.token.RedElementalToken;
 import mage.game.permanent.token.Token;
 import mage.target.common.TargetCardInYourGraveyard;
 import mage.target.targetpointer.FixedTarget;
+import mage.target.targetpointer.FixedTargets;
 
+import java.util.ArrayList;
+import java.util.List;
 import java.util.UUID;
 
 /**
@@ -98,23 +101,26 @@ class ChandraAcolyteOfFlameEffect extends OneShotEffect {
         Token token = new RedElementalToken();
         token.putOntoBattlefield(2, game, source, source.getControllerId());
 
-        token.getLastAddedTokenIds().stream().forEach(permId -> {
+        List<Permanent> permanents = new ArrayList<>();
+        for (UUID permId : token.getLastAddedTokenIds()) {
             Permanent permanent = game.getPermanent(permId);
             if (permanent == null) {
-                return;
+                continue;
             }
+            permanents.add(permanent);
 
             ContinuousEffect effect = new GainAbilityTargetEffect(HasteAbility.getInstance(), Duration.Custom);
             effect.setTargetPointer(new FixedTarget(permId, game));
             game.addEffect(effect, source);
 
-            Effect effect2 = new SacrificeTargetEffect();
-            effect2.setTargetPointer(new FixedTarget(permId, game));
-            game.addDelayedTriggeredAbility(new AtTheBeginOfNextEndStepDelayedTriggeredAbility(effect2), source);
-
             // extra info
             InfoEffect.addInfoToPermanent(game, source, permanent, "<i><b>Warning</b>: It will be sacrificed at the beginning of the next end step</i>");
-        });
+        }
+
+        // "Sacrifice them" is one instruction, so it is one delayed triggered ability for both tokens (CR 603.7a)
+        Effect sacrificeEffect = new SacrificeTargetEffect("sacrifice them", source.getControllerId());
+        sacrificeEffect.setTargetPointer(new FixedTargets(permanents, game));
+        game.addDelayedTriggeredAbility(new AtTheBeginOfNextEndStepDelayedTriggeredAbility(sacrificeEffect), source);
 
         return true;
     }
diff --git a/Mage.Tests/src/test/java/org/mage/test/cards/single/m20/ChandraAcolyteOfFlameTest.java b/Mage.Tests/src/test/java/org/mage/test/cards/single/m20/ChandraAcolyteOfFlameTest.java
new file mode 100644
index 00000000000..2133203d94e
--- /dev/null
+++ b/Mage.Tests/src/test/java/org/mage/test/cards/single/m20/ChandraAcolyteOfFlameTest.java
@@ -0,0 +1,32 @@
+package org.mage.test.cards.single.m20;
+
+import mage.constants.PhaseStep;
+import mage.constants.Zone;
+import org.junit.Test;
+import org.mage.test.serverside.base.CardTestPlayerBase;
+
+/**
+ * Chandra, Acolyte of Flame: "0: Create two 1/1 red Elemental creature tokens.
+ * They gain haste. Sacrifice them at the beginning of the next end step."
+ * <p>
+ * One instruction makes one delayed triggered ability (CR 603.7a) that
+ * sacrifices both tokens, so the end step puts one trigger on the stack, not
+ * one per token.
+ */
+public class ChandraAcolyteOfFlameTest extends CardTestPlayerBase {
+
+    @Test
+    public void testOneDelayedSacrificeForBothTokens() {
+        addCard(Zone.BATTLEFIELD, playerA, "Chandra, Acolyte of Flame", 1);
+
+        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "0: Create two 1/1 red Elemental");
+        checkPermanentCount("two tokens before the end step", 1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Elemental Token", 2);
+        checkStackSize("one delayed trigger for both tokens", 1, PhaseStep.END_TURN, playerA, 1);
+
+        setStrictChooseMode(true);
+        setStopAt(1, PhaseStep.CLEANUP);
+        execute();
+
+        assertPermanentCount(playerA, "Elemental Token", 0);
+    }
+}
```
