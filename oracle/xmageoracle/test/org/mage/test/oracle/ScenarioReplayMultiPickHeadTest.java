package org.mage.test.oracle;

import java.lang.reflect.Method;
import java.util.List;

/** Pins that a "^"-joined multi-pick answer ("Ornithopter^Sol Ring") is read
 * as its segments, in order, so a forced choose ask whose queue head names its
 * candidates leaves the pick to the base player instead of taking every
 * candidate in XMage's possibleTargets (HashSet) order. Rottenmouth Viper's
 * two sacrificed permanents reached the graveyard in a random order, and the
 * scripted pick was left unused. Run scripts/xmage-oracle-test-multi-pick-head.sh. */
public final class ScenarioReplayMultiPickHeadTest {
    private static void equal(Object want, Object got) {
        if (!java.util.Objects.equals(want, got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    public static void main(String[] args) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method segments = adapter.getDeclaredMethod("answerSegments", String.class);
        segments.setAccessible(true);
        equal(List.of("Ornithopter", "Sol Ring"), segments.invoke(null, "Ornithopter^Sol Ring"));
        equal(List.of("Sol Ring", "Ornithopter"), segments.invoke(null, "Sol Ring^Ornithopter"));
        equal(List.of("@p0:Wastes#27", "@p0:Wastes#39"), segments.invoke(null, "@p0:Wastes#27^@p0:Wastes#39"));
        equal(List.of("Island"), segments.invoke(null, "Island"));

        // A head that names a candidate is the ask's own answer: never forced.
        Method forced = adapter.getDeclaredMethod("forcedPick", int.class, int.class, String.class, boolean.class);
        forced.setAccessible(true);
        equal(false, forced.invoke(null, 2, 2, "Ornithopter^Sol Ring", true));
        equal(true, forced.invoke(null, 2, 2, null, false));
        System.out.println("PASS a multi-pick head names its candidates in order");
    }
}
