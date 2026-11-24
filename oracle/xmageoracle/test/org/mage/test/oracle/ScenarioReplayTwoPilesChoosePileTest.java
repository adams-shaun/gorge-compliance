package org.mage.test.oracle;

import java.lang.reflect.Constructor;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.List;
import mage.constants.Outcome;
import mage.constants.RangeOfInfluence;
import org.mage.test.player.TestComputerPlayer;
import org.mage.test.player.TestPlayer;

/** Pins that a RevealAndSeparatePilesEffect pile pick (Steam Augury, Fact or
 * Fiction: Player.choosePile, not chooseUse) follows the scripted Yes/No and
 * leaves no choice queued. Run scripts/xmage-oracle-test-two-piles-choose-pile.sh. */
public final class ScenarioReplayTwoPilesChoosePileTest {
    private static void equal(Object want, Object got) {
        if (!java.util.Objects.equals(want, got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    public static void main(String[] args) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Constructor<?> ctor = adapter.getDeclaredConstructor(TestComputerPlayer.class, ScenarioReplay.class);
        ctor.setAccessible(true);
        // choosePile never reads the owner replay or the game when it is scripted.
        TestPlayer player = (TestPlayer) ctor.newInstance(
                new TestComputerPlayer("PlayerB", RangeOfInfluence.ALL), null);

        player.addChoice("Yes");
        equal(true, player.choosePile(Outcome.Benefit, "Choose a pile to put into hand.", List.of(), List.of(), null));
        equal(List.of(), player.getChoices());

        player.addChoice("No");
        equal(false, player.choosePile(Outcome.Benefit, "Choose a pile to put into hand.", List.of(), List.of(), null));
        equal(List.of(), player.getChoices());

        // Only the head is consumed: the next decision's answer stays queued.
        player.addChoice("No");
        player.addChoice("Island");
        equal(false, player.choosePile(Outcome.Benefit, "Choose a pile to put into hand.", List.of(), List.of(), null));
        equal(List.of("Island"), player.getChoices());

        // A non-boolean head is not a pile answer: untouched (the AI chooses).
        Method pick = adapter.getDeclaredMethod("scriptedPileAnswer", List.class);
        pick.setAccessible(true);
        List<String> other = new ArrayList<>(List.of("Island", "Yes"));
        equal(null, pick.invoke(null, other));
        equal(List.of("Island", "Yes"), other);
        equal(null, pick.invoke(null, new ArrayList<String>()));
        System.out.println("PASS choosePile follows the scripted Yes/No and leaves no choice queued");
    }
}
