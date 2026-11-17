package org.mage.test.oracle;

import java.util.ArrayList;
import java.util.List;
import org.mage.test.player.TestPlayer;

/** Pins the target-shaped generator answer routing used by surveil's library chooser.
 * Run scripts/xmage-oracle-test-surveil-skip.sh; full XMage replay remains a real replay. */
public final class ScenarioReplaySurveilSkipTest {
    private static void equal(Object want, Object got) {
        if (!java.util.Objects.equals(want, got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    public static void main(String[] args) throws Exception {
        List<String> choices = new ArrayList<>();
        List<String> targets = new ArrayList<>(List.of(TestPlayer.TARGET_SKIP));

        // Preconditions: the source answer is specifically a target skip and
        // there is no competing choice answer; otherwise this would not test
        // the surveil/scry chooser's mismatched queue.
        equal(true, choices.isEmpty());
        equal(TestPlayer.TARGET_SKIP, targets.get(0));
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        java.lang.reflect.Method route = adapter.getDeclaredMethod("librarySelectionQueue",
                List.class, List.class, java.util.function.Predicate.class);
        route.setAccessible(true);
        @SuppressWarnings("unchecked")
        List<String> selected = (List<String>) route.invoke(null, choices, targets,
                (java.util.function.Predicate<String>) answer -> false);
        equal(choices, selected);
        equal(List.of(TestPlayer.CHOICE_SKIP), choices);
        equal(List.of(), targets);

        // A real card answer already on the choice queue takes precedence and
        // leaves a later target skip for its own target ask.
        List<String> cardChoice = new ArrayList<>(List.of("Wastes"));
        List<String> otherTarget = new ArrayList<>(List.of(TestPlayer.TARGET_SKIP));
        equal(cardChoice, route.invoke(null, cardChoice, otherTarget,
                (java.util.function.Predicate<String>) "Wastes"::equals));
        equal(List.of(TestPlayer.TARGET_SKIP), otherTarget);
        // The kept-card order phase after a partial arrange's skip consumes
        // exactly the kept labels. binned (Island, Mountain) > kept (Forest):
        // a same-step answer naming the binned Island stays queued.
        java.lang.reflect.Method order = adapter.getDeclaredMethod("keptOrderLabels", List.class, List.class);
        order.setAccessible(true);
        List<String> q1 = new ArrayList<>(List.of("Forest", "Island"));
        equal(List.of("Forest"), order.invoke(null, q1, new ArrayList<>(List.of("Forest"))));
        equal(List.of("Island"), q1);
        // kept (Mountain, Forest) > binned (Island): both labels are consumed,
        // in queue (PickIdx) order, and the next decision's answer is left.
        List<String> q2 = new ArrayList<>(List.of("Mountain", "Forest", "Mountain"));
        equal(List.of("Mountain", "Forest"), order.invoke(null, q2, new ArrayList<>(List.of("Forest", "Mountain"))));
        equal(List.of("Mountain"), q2);
        // An all-kept arrange emits no order labels: a label naming a kept
        // card is not consumed when no kept card remains to order.
        List<String> q3 = new ArrayList<>(List.of("Forest"));
        equal(List.of(), order.invoke(null, q3, new ArrayList<>()));
        equal(List.of("Forest"), q3);
        // The first label naming no remaining kept card ends the phase.
        List<String> q4 = new ArrayList<>(List.of("Forest", "Island", "Mountain"));
        equal(List.of("Forest"), order.invoke(null, q4, new ArrayList<>(List.of("Forest", "Mountain"))));
        equal(List.of("Island", "Mountain"), q4);
        System.out.println("PASS surveil library skip moves from target queue to choice queue");
    }
}
