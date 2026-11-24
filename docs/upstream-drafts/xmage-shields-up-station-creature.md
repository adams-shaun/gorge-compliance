# Shields Up!: a stationed Spacecraft gets no +1/+1 counter

Engine: XMage (magefree/mage). Upstream base: master `2a96b0ac12` (2026-11-22). Mirror branch: [`adams-shaun/mage:upstream/shields-up-station-creature`](https://github.com/adams-shaun/mage/tree/upstream/shields-up-station-creature) at `2a29998dc6`.

## Oracle text

Shields Up!:

> Target artifact or creature you control gains hexproof and indestructible until end of turn. If it's a creature, put a +1/+1 counter on it. (It can't be the target of spells or abilities your opponents control. Damage and effects that say "destroy" don't destroy it.)

## Comprehensive Rules

> 721.2b “{N+}[abilities][P/T]” means “As long as this permanent has N or more charge counters on it, it has [abilities] and is a creature with base power and toughness [P/T] in addition to its other types.” 

> 608.2c The controller of the spell or ability follows its instructions in the order written. However, replacement effects may modify these actions. In some cases, later text on the card may modify the meaning of earlier text (for example, “Destroy target creature. It can’t be regenerated” or “Counter target spell. If that spell is countered this way, put it on top of its owner’s library instead of into its owner’s graveyard.”) Don’t just apply effects step by step without thinking in these cases—read the whole text and apply the rules of English to the text. 

## What happens

`Mage.Sets/src/mage/cards/s/ShieldsUp.java:80` (at `2a96b0ac12`) filters the target with `permanent.isCreature()`,
the no-game overload, which reads the card's own types and misses the creature type a station ability grants
(CR 721.2b). A Spacecraft with enough charge counters is a creature when Shields Up! resolves, so "If it's a
creature, put a +1/+1 counter on it" should apply. `isCreature(game)` reads the current types.

Our farm rows: Romulan Bird-of-Prey with 8+ charge counters targeted by Shields Up! (two rows). The same no-game
`isCreature()` call is in `TeyoDiamondbladeMage.java:75` and `TeyoLightshieldExpert.java:75`; we have not checked
those.

## Repro

1. Player A controls Romulan Bird-of-Prey with 8 charge counters (STATION 8+: a 4/4 flier) and two Plains.
2. A casts Shields Up! targeting it.
3. Expected: it gets a +1/+1 counter (5/5). Actual: no counter (4/4).

`ShieldsUpStationTest.testStationedSpacecraftGetsCounter` below fails on master
(`Counter counts are not equal (Romulan Bird-of-Prey:+1/+1) expected:<1> but was:<0>`) and passes with the fix;
the unstationed case passes both ways.


## Proposed fix

```diff
diff --git a/Mage.Sets/src/mage/cards/s/ShieldsUp.java b/Mage.Sets/src/mage/cards/s/ShieldsUp.java
index 4c3c44bdf1..455074dff2 100644
--- a/Mage.Sets/src/mage/cards/s/ShieldsUp.java
+++ b/Mage.Sets/src/mage/cards/s/ShieldsUp.java
@@ -77,7 +77,7 @@ class ShieldsUpEffect extends OneShotEffect {
         return Optional
                 .ofNullable(getTargetPointer().getFirst(game, source))
                 .map(game::getPermanent)
-                .filter(permanent -> permanent.isCreature())
+                .filter(permanent -> permanent.isCreature(game))
                 .filter(permanent -> permanent.addCounters(CounterType.P1P1.createInstance(), source, game))
                 .isPresent();
     }
```

## Test

Added with the fix: `Mage.Tests/src/test/java/org/mage/test/cards/single/trk/ShieldsUpStationTest.java`

```java
package org.mage.test.cards.single.trk;

import mage.constants.PhaseStep;
import mage.constants.Zone;
import mage.counters.CounterType;
import org.junit.Test;
import org.mage.test.serverside.base.CardTestPlayerBase;

/**
 * Shields Up! (TRK): "Target artifact or creature you control gains hexproof and
 * indestructible until end of turn. If it's a creature, put a +1/+1 counter on it."
 *
 * CR 721.2b: a station permanent with N or more charge counters "is a creature
 * with base power and toughness [P/T] in addition to its other types", so a
 * Spacecraft with enough charge counters is a creature on resolution and gets the counter.
 * CR 608.2c: the controller follows the instructions in order; "if it's a creature"
 * is read from the permanent as it is now, layer effects included.
 */
public class ShieldsUpStationTest extends CardTestPlayerBase {

    @Test
    public void testStationedSpacecraftGetsCounter() {
        setStrictChooseMode(true);
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Romulan Bird-of-Prey");
        addCounters(1, PhaseStep.UPKEEP, playerA, "Romulan Bird-of-Prey", CounterType.CHARGE, 8);
        addCard(Zone.HAND, playerA, "Shields Up!");

        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Shields Up!", "Romulan Bird-of-Prey");

        setStopAt(1, PhaseStep.END_TURN);
        execute();

        assertCounterCount(playerA, "Romulan Bird-of-Prey", CounterType.P1P1, 1);
        assertPowerToughness(playerA, "Romulan Bird-of-Prey", 5, 5);
    }

    @Test
    public void testUnstationedSpacecraftGetsNoCounter() {
        setStrictChooseMode(true);
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Romulan Bird-of-Prey");
        addCard(Zone.HAND, playerA, "Shields Up!");

        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Shields Up!", "Romulan Bird-of-Prey");

        setStopAt(1, PhaseStep.END_TURN);
        execute();

        assertCounterCount(playerA, "Romulan Bird-of-Prey", CounterType.P1P1, 0);
    }
}
```
