# Fight deals only lethal damage, so a lifelink fighter gains too little life

Engine: XMage (magefree/mage). Upstream base: master `2a96b0ac12` (2026-11-22). Mirror branch: [`adams-shaun/mage:upstream/fight-full-power-damage`](https://github.com/adams-shaun/mage/tree/upstream/fight-full-power-damage) at `8f442a23e2`.

## Oracle text

Plow Through:

> Choose one —
> • Target creature you control fights target creature an opponent controls. (Each deals damage equal to its power to the other.)
> • Destroy target Vehicle.

## Comprehensive Rules

> 701.14a A spell or ability may instruct a creature to fight another creature or it may instruct two creatures to fight each other. Each of those creatures deals damage equal to its power to the other creature. 

> 702.15b Damage dealt by a source with lifelink causes that source’s controller, or its owner if it has no controller, to gain that much life (in addition to any other results that damage causes). See rule 120.3. 

## What happens

`Mage/src/main/java/mage/game/permanent/PermanentImpl.java:2338` (at `2a96b0ac12`), in `fightWithExcess`, has the
fighting creature deal its damage through `fightTarget.damageWithExcess(getPower().getValue(), ...)`.
`Permanent.damageWithExcess` (`Permanent.java:220-226`) deals `Math.min(lethal, damage)` and returns the rest, which
is right for its other callers ("excess damage is dealt to that creature's controller instead": Flame Spill and
kin), but a fight is not one of them. CR 701.14a says each creature deals damage equal to its power. With
`fightWithExcess` used by every `fight(...)` call, all fights deal at most lethal damage, so:

- a lifelink creature gains life equal to the lethal damage, not its power (CR 702.15b);
- with deathtouch, lethal damage is 1, so a lifelink-deathtouch fighter gains 1;
- "whenever this deals damage" effects see the truncated amount.

Cards in our farm rows: Plow Through, Kapow!, Chelonian Tackle (`FightTargetsEffect`) and Pitiless Fists
(`FightEnchantedTargetEffect`), each with a lifelink fighter: eight rows.

## Repro

1. Player A controls Baneslayer Angel (5/5 lifelink). Player B controls Llanowar Elves (1/1). A casts Plow Through,
   first mode, Baneslayer fights Elves.
2. Expected: Baneslayer deals 5, A goes to 25. Actual: Baneslayer deals 1 (lethal for a 1/1), A goes to 21.

`PlowThroughTest` below: the two fight tests fail on the unpatched class (`expected:<25> but was:<21>`,
`expected:<22> but was:<21>`) and pass with the fix; the two Flame Spill controls pass both ways, so the
excess-instead callers keep truncating. (Run red/green by compiling the patched `PermanentImpl` and the test over a
1.4.63 build at our fork's pin `33704ee4fe`; the patched hunk is byte-identical at master `2a96b0ac12`.)


## Proposed fix

```diff
diff --git a/Mage/src/main/java/mage/game/permanent/PermanentImpl.java b/Mage/src/main/java/mage/game/permanent/PermanentImpl.java
index b5f16b8736..dc709b659c 100644
--- a/Mage/src/main/java/mage/game/permanent/PermanentImpl.java
+++ b/Mage/src/main/java/mage/game/permanent/PermanentImpl.java
@@ -2335,7 +2335,13 @@ public abstract class PermanentImpl extends CardImpl implements Permanent {
         game.fireEvent(GameEvent.getEvent(GameEvent.EventType.FIGHTED_PERMANENT, fightTarget.getId(), source, source.getControllerId()));
         game.fireEvent(GameEvent.getEvent(GameEvent.EventType.FIGHTED_PERMANENT, getId(), source, source.getControllerId()));
         damage(fightTarget.getPower().getValue(), fightTarget.getId(), source, game);
-        int excess = fightTarget.damageWithExcess(getPower().getValue(), getId(), source, game);
+        // CR 701.14a: each creature deals damage equal to its whole power, so lifelink and damage-dealt
+        // effects count all of it. damageWithExcess would deal only min(lethal, power) because its other
+        // callers send the excess elsewhere instead; a fight only measures the excess, so deal the full
+        // amount here and compute the excess from the lethal damage taken before the damage.
+        int fightPower = getPower().getValue();
+        int excess = Math.max(CardUtil.overflowDec(fightPower, fightTarget.getLethalDamage(getId(), game)), 0);
+        fightTarget.damage(fightPower, getId(), source, game);
 
         if (batchTrigger) {
             Set<MageObjectReference> morSet = new HashSet<>();
```

## Test

Added with the fix: `Mage.Tests/src/test/java/org/mage/test/cards/single/dft/PlowThroughTest.java`

```java
package org.mage.test.cards.single.dft;

import mage.constants.PhaseStep;
import mage.constants.Zone;
import org.junit.Test;
import org.mage.test.serverside.base.CardTestPlayerBase;

/**
 * Plow Through, mode 1: "Target creature you control fights target creature an opponent controls."
 * <p>
 * CR 701.14a: when two creatures fight each other, each deals damage equal to its power to the other.
 * CR 702.15b: damage dealt by a source with lifelink causes its controller to gain that much life.
 * So a lifelink fighter gains its whole power, not only the part that is lethal to the other creature.
 * <p>
 * The excess-damage helper (Permanent.damageWithExcess) deals only min(lethal, damage) because its
 * "excess damage is dealt to that creature's controller instead" callers (Flame Spill and the like)
 * must not deal the excess twice; a fight must not borrow that truncation.
 */
public class PlowThroughTest extends CardTestPlayerBase {

    private static final String plow = "Plow Through";
    private static final String nighthawk = "Vampire Nighthawk"; // 2/3 flying, deathtouch, lifelink
    private static final String crawWurm = "Craw Wurm"; // 6/4
    private static final String baneslayer = "Baneslayer Angel"; // 5/5 flying, first strike, lifelink
    private static final String elves = "Llanowar Elves"; // 1/1
    private static final String spill = "Flame Spill"; // 4 damage to target creature, excess to its controller instead
    private static final String bears = "Grizzly Bears"; // 2/2
    private static final String reckoner = "Boros Reckoner"; // 3/3, whenever dealt damage, deals that much damage to any target

    @Test
    public void testLifelinkDeathtouchFightGainsWholePower() {
        addCard(Zone.BATTLEFIELD, playerA, "Forest", 1);
        addCard(Zone.BATTLEFIELD, playerA, nighthawk);
        addCard(Zone.BATTLEFIELD, playerB, crawWurm);
        addCard(Zone.HAND, playerA, plow);

        setModeChoice(playerA, "1");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, plow, nighthawk + "^" + crawWurm);

        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();

        // the 6/4 Craw Wurm deals 6 to the Nighthawk (dies); the Nighthawk deals 2 with deathtouch (dies) and lifelink
        assertGraveyardCount(playerA, nighthawk, 1);
        assertGraveyardCount(playerB, crawWurm, 1);
        assertLife(playerA, 20 + 2);
        assertLife(playerB, 20);
    }

    @Test
    public void testLifelinkFightGainsWholePowerNotToughness() {
        addCard(Zone.BATTLEFIELD, playerA, "Forest", 1);
        addCard(Zone.BATTLEFIELD, playerA, baneslayer);
        addCard(Zone.BATTLEFIELD, playerB, elves);
        addCard(Zone.HAND, playerA, plow);

        setModeChoice(playerA, "1");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, plow, baneslayer + "^" + elves);

        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();

        // Baneslayer deals 5 (not just the 1 that is lethal to the Elves) and gains 5
        assertGraveyardCount(playerB, elves, 1);
        assertPermanentCount(playerA, baneslayer, 1);
        assertDamageReceived(playerA, baneslayer, 1);
        assertLife(playerA, 20 + 5);
        assertLife(playerB, 20);
    }

    /**
     * Control, must pass before and after the fight fix: Flame Spill's "excess damage is dealt to that
     * creature's controller instead" still sends only the excess to the controller, and the creature
     * is still dealt only its lethal part. Boros Reckoner reflects the damage it is dealt, so a
     * damage-dealt count of 4 instead of 3 would show.
     */
    @Test
    public void testFlameSpillExcessInsteadStillTruncated() {
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 3);
        addCard(Zone.HAND, playerA, spill);
        addCard(Zone.BATTLEFIELD, playerB, reckoner);

        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, spill, reckoner);
        addTarget(playerB, playerA); // the Reckoner's reflected damage

        setStopAt(1, PhaseStep.END_TURN);
        execute();

        // 4 damage at a 3/3: 3 to the Reckoner (it reflects 3 to playerA), the excess 1 to playerB
        assertGraveyardCount(playerB, reckoner, 1);
        assertLife(playerB, 20 - 1);
        assertLife(playerA, 20 - 3);
    }

    /** Control: the plain Flame Spill case from ExcessDamageTest, a 2/2 takes 2 and its controller 2. */
    @Test
    public void testFlameSpillOnBears() {
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 3);
        addCard(Zone.HAND, playerA, spill);
        addCard(Zone.BATTLEFIELD, playerB, bears);

        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, spill, bears);

        setStopAt(1, PhaseStep.END_TURN);
        execute();

        assertGraveyardCount(playerB, bears, 1);
        assertLife(playerB, 20 - 2);
        assertLife(playerA, 20);
    }
}
```
