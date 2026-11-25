package org.mage.test.oracle;

import java.lang.reflect.Method;
import java.util.List;
import java.util.UUID;
import java.util.function.BiPredicate;

/** Pins which hand asks take gorge's CR 401.4 placement order. Sawtooth Loon
 * draws into an empty hand and puts two cards from it on the bottom of the
 * library, one chooseTarget call for a 2..2 hand target. gorge poses no
 * selection (both cards are forced) and logs only the library_placement order;
 * the generator queues it as choices naming the cards, and a queued target
 * answer for the OTHER Loon trigger sits in front of them. The helper must take
 * every forced card in the choice order, and nothing else: a head that does not
 * name the candidates exactly once, a spare candidate, or a target answer
 * naming a candidate stay with the base. Run
 * scripts/xmage-oracle-test-hand-library-placement.sh. */
public final class ScenarioReplayHandPlacementTest {
    private static void equal(Object want, Object got) {
        if (!java.util.Objects.equals(want, got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    public static void main(String[] args) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method order = adapter.getDeclaredMethod("forcedHandPlacementOrder", List.class, List.class, List.class,
                int.class, BiPredicate.class);
        order.setAccessible(true);
        UUID island = new UUID(0, 1);
        UUID plains = new UUID(0, 2);
        UUID forest = new UUID(0, 3);
        BiPredicate<UUID, String> names = (id, answer) ->
                answer.equals(id.equals(island) ? "Island" : id.equals(plains) ? "Plains" : "Forest");
        List<UUID> two = List.of(island, plains);

        // The Loon shape: another trigger's target answer is queued first.
        equal(List.of(island, plains), order.invoke(null, List.of("Sawtooth Loon"), List.of("Island", "Plains"), two, 2, names));
        // gorge's order, not the hand's: the first pick is placed first.
        equal(List.of(plains, island), order.invoke(null, List.of(), List.of("Plains", "Island"), two, 2, names));
        // Choices beyond the count belong to later asks and stay.
        equal(List.of(island, plains), order.invoke(null, List.of(), List.of("Island", "Plains", "Forest"), two, 2, names));
        // One card is no arrangement (gorge poses none), and the existing forced pick owns it.
        equal(null, order.invoke(null, List.of(), List.of("Island"), List.of(island), 1, names));
        // A spare candidate means a real selection, answered by the ordinary path.
        equal(null, order.invoke(null, List.of(), List.of("Island", "Plains"), List.of(island, plains, forest), 2, names));
        // A head naming a card twice, a non-candidate, or a skip is not this order.
        equal(null, order.invoke(null, List.of(), List.of("Island", "Island"), two, 2, names));
        equal(null, order.invoke(null, List.of(), List.of("Island", "Forest"), two, 2, names));
        equal(null, order.invoke(null, List.of(), List.of("Island", "[choice_skip]"), two, 2, names));
        // A queued target answer naming a candidate (or a skip) is this ask's own.
        equal(null, order.invoke(null, List.of("Island"), List.of("Island", "Plains"), two, 2, names));
        equal(null, order.invoke(null, List.of("[target_skip]"), List.of("Island", "Plains"), two, 2, names));
        System.out.println("PASS a forced multi-card hand pick takes gorge's placement order");
    }
}
