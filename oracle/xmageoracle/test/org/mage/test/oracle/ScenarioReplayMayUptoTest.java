package org.mage.test.oracle;

import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Predicate;
import mage.filter.StaticFilters;
import mage.target.common.TargetCardInLibrary;
import org.mage.test.player.TestPlayer;

/** Pins the two XMage shapes that refused a legal gorge line: a "you may" posed
 * as one up-to-one choose (Ambrosia Whiteheart, Spelunking) and a fixed-count
 * library search that finds fewer cards (Planar Engineering).
 * Run scripts/xmage-oracle-test-may-upto.sh. */
public final class ScenarioReplayMayUptoTest {
    private static void equal(Object want, Object got) {
        if (!java.util.Objects.equals(want, got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static List<String> q(String... items) {
        return new ArrayList<>(List.of(items));
    }

    public static void main(String[] args) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method may = adapter.getDeclaredMethod("mayPrefix", List.class, List.class, boolean.class, Predicate.class);
        may.setAccessible(true);
        Method stop = adapter.getDeclaredMethod("shortSearchStop", List.class, boolean.class, int.class, int.class);
        stop.setAccessible(true);
        Predicate<String> names = "Forest"::equals;
        Predicate<String> none = a -> false;

        // Spelunking: yes, then the pick on the choice queue itself.
        List<String> c = q("Yes", "Forest");
        List<String> t = q();
        equal(Boolean.TRUE, may.invoke(null, c, t, true, names));
        equal(q("Forest"), c);

        // Ambrosia: yes on the choice queue, the pick on the target queue.
        c = q("Yes");
        t = q("p0:Plains", "later");
        equal(Boolean.TRUE, may.invoke(null, c, t, true, none));
        equal(q("p0:Plains"), c);
        equal(q("later"), t);

        // A pick already on the choice queue stays; the target queue is untouched.
        c = q("Yes", "Forest");
        t = q("p0:Plains");
        equal(Boolean.TRUE, may.invoke(null, c, t, true, names));
        equal(q("Forest"), c);
        equal(q("p0:Plains"), t);

        // no: nothing is chosen, and the declined pair's skip goes with it.
        c = q("No", "Forest");
        t = q(TestPlayer.TARGET_SKIP, "later");
        equal(Boolean.FALSE, may.invoke(null, c, t, true, names));
        equal(q("Forest"), c);
        equal(q("later"), t);
        c = q("No");
        t = q("p0:Plains");
        equal(Boolean.FALSE, may.invoke(null, c, t, true, none));
        equal(q("p0:Plains"), t);

        // Not a may: a named pick, an empty queue, or an ask with no candidate.
        c = q("Forest");
        equal(null, may.invoke(null, c, q(), true, names));
        equal(q("Forest"), c);
        equal(null, may.invoke(null, q(), q(), true, names));
        c = q("Yes");
        equal(null, may.invoke(null, c, q(), false, names));
        equal(q("Yes"), c);

        // The library search: a skip closes a not-required, fixed-count search.
        TargetCardInLibrary basics = new TargetCardInLibrary(4, StaticFilters.FILTER_CARD_BASIC_LANDS);
        equal(false, basics.isRequired());
        equal(true, stop.invoke(null, q(TestPlayer.TARGET_SKIP), basics.isRequired(),
                basics.getMinNumberOfTargets(), basics.getMaxNumberOfTargets()));
        equal(false, stop.invoke(null, q("Forest"), false, 4, 4));
        equal(false, stop.invoke(null, q(), false, 4, 4));
        equal(false, stop.invoke(null, q(TestPlayer.TARGET_SKIP), true, 1, 1));
        equal(false, stop.invoke(null, q(TestPlayer.TARGET_SKIP), false, 0, 1));
        System.out.println("ok");
    }
}
