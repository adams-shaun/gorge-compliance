package org.mage.test.oracle;

import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import mage.target.Target;
import mage.target.common.TargetPermanentOrPlayer;

/** Pins where hideLaterPlayerTargets' withheld "targetPlayer=" answers go back
 * after the ask: at their original place among the answers still queued, not
 * at the queue end. Boulder Dash ("any target ... any other target") with the
 * queue [creature, targetPlayer=PlayerB, a later step's creature answer]
 * otherwise reaches its second ask with the later step's answer in front of
 * its own player answer and kills the caster's own creature. No game, no H2.
 * Run scripts/xmage-oracle-test-later-player-restore.sh. */
public final class ScenarioReplayLaterPlayerRestoreTest {
    private static final String P = "targetPlayer=PlayerB";

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static Class<?> adapter() throws Exception {
        return Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
    }

    @SuppressWarnings("unchecked")
    private static List<String> askAndRestore(int consumed, String... queueAnswers) throws Exception {
        Class<?> c = adapter();
        Method indices = c.getDeclaredMethod("laterPlayerIndices", Target.class, List.class);
        Method hide = c.getDeclaredMethod("hideLaterPlayerTargets", Target.class, List.class);
        Method restore = c.getDeclaredMethod("restoreLaterPlayerTargets", List.class, List.class, List.class,
                int.class);
        indices.setAccessible(true);
        hide.setAccessible(true);
        restore.setAccessible(true);
        Target anyTarget = new TargetPermanentOrPlayer();
        List<String> queue = new ArrayList<>(Arrays.asList(queueAnswers));
        List<Integer> slots = (List<Integer>) indices.invoke(null, anyTarget, queue);
        List<String> hidden = (List<String>) hide.invoke(null, anyTarget, queue);
        int visibleBefore = queue.size();
        for (int i = 0; i < consumed; i++) {
            queue.remove(0);
        }
        restore.invoke(null, queue, hidden, slots, visibleBefore);
        return queue;
    }

    public static void main(String[] args) throws Exception {
        // The Boulder Dash queue: the first ask takes the creature; the
        // player answer must stay ahead of the later step's answer.
        check(askAndRestore(1, "@p1:Virulent Emissary", P, "@p0:Virulent Emissary")
                .equals(Arrays.asList(P, "@p0:Virulent Emissary")), "player answer restored behind the later step's answer");
        // A queue that ends at the player answer restores to the front.
        check(askAndRestore(1, "@p1:Virulent Emissary", P).equals(Arrays.asList(P)), "trailing player answer");
        // Two withheld answers keep their order and their spots.
        check(askAndRestore(1, "@a", P, "@b", P, "@c")
                .equals(Arrays.asList(P, "@b", P, "@c")), "two withheld answers reordered");
        // An ask that consumed nothing leaves the queue as it was.
        check(askAndRestore(0, "@a", P, "@b").equals(Arrays.asList("@a", P, "@b")), "no-consume restore moved an answer");
        // The answer standing behind two visible answers stays behind the
        // one that remains.
        check(askAndRestore(1, "@a", "@b", P, "@c")
                .equals(Arrays.asList("@b", P, "@c")), "answer behind two visible entries misplaced");
        // A front player answer is the ask's own: nothing withheld, nothing restored.
        check(askAndRestore(1, P, "@a").equals(Arrays.asList("@a")), "front player answer withheld");
        System.out.println("PASS later player answers restored at their original queue position");
    }
}
