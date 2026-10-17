package org.mage.test.oracle;

import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import mage.constants.PhaseStep;
import org.mage.test.player.PlayerAction;
import org.mage.test.player.TestPlayer;

/** No database or game: lb-xharness2's two driver shapes.
 *
 * (1) The CantBeCast/CantBeActivated opponent-priority probes open with a
 * p0/p1 pass pair on an EMPTY stack (MSH Jennifer Walters static#0.0/1.0,
 * Grand Abolisher, Kutzil, Voice of Victory). Gorge moves to the next step
 * there; the driver used to demand a stack object and threw "pass pair needs
 * a stack object".
 *
 * (2) A pass pair at the scenario's start with one object on the stack: the
 * pair is a prelude to the steps after it (Shadow of the Goblin trigger#0.3,
 * whose setupResolvePrelude pair clears a beginning-of-main1 trigger), so it
 * resolves the top object and keeps its checkpoint in main1. It must NOT be
 * treated as an empty-stack pair just because it opens the scenario.
 *
 * (3) Two same-name attackers ("p0:token:Goblin Token", "...#2", Okoye):
 * TestPlayer finds "Goblin Token:1" among UNTAPPED creatures only, and the
 * first declaration taps its token, so "Goblin Token:1" was left over. */
public final class ScenarioReplayOpeningPassAttackOrderTest {

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static JsonArray steps(String json) {
        return JsonParser.parseString(json).getAsJsonArray();
    }

    private static Field field(String name) throws Exception {
        for (Class<?> c = ScenarioReplay.class; c != null; c = c.getSuperclass()) {
            try {
                Field f = c.getDeclaredField(name);
                f.setAccessible(true);
                return f;
            } catch (NoSuchFieldException next) {
                // keep climbing
            }
        }
        throw new AssertionError("no field " + name);
    }

    private static void invoke(ScenarioReplay r, String method, Object... args) throws Exception {
        for (Method m : ScenarioReplay.class.getDeclaredMethods()) {
            if (m.getName().equals(method) && m.getParameterCount() == args.length) {
                m.setAccessible(true);
                m.invoke(r, args);
                return;
            }
        }
        throw new AssertionError("no method " + method);
    }

    private static TestPlayer bare(String name) {
        return new TestPlayer(new org.mage.test.player.TestComputerPlayer(name, mage.constants.RangeOfInfluence.ALL));
    }

    /** The replay loop for one step (pass commands, step, checkpoint at the
     * driver's CURRENT phase), as ScenarioReplay's loop runs it. */
    private static void loopStep(ScenarioReplay r, TestPlayer a, JsonArray st, int i) throws Exception {
        String op = st.get(i).getAsJsonObject().get("op").getAsString();
        invoke(r, "queuePassCommands", i);
        invoke(r, "step", st.get(i).getAsJsonObject(), op, i);
        PhaseStep phase = (PhaseStep) field("phase").get(r);
        r.runCode("step " + i + " (" + op + ")", 1, phase, a, (info, p, g) -> { });
    }

    private static void openingPair() throws Exception {
        JsonArray pair = steps("[{op:'pass',seat:0,expect:[{offered:{seat:1,kind:'cast',card:'p1:Gut Shot'},want:false}]},"
                + "{op:'pass',seat:1}]");
        check(ScenarioReplay.passAction(pair, 1) == ScenarioReplay.PASS_PAIR_SECOND, "precondition: step 1 closes a pair");
        check(ScenarioReplay.passCommands(pair, 1, 0).equals(Arrays.asList(
                        ScenarioReplay.CMD_REQUIRE_EMPTY_STACK, ScenarioReplay.CMD_ADVANCE_STEP)),
                "an opening pair must expect an empty stack and advance the step, not wait for a stack object; got "
                        + ScenarioReplay.passCommands(pair, 1, 0));

        TestPlayer a = bare("A");
        ScenarioReplay r = new ScenarioReplay();
        JsonObject sc = new JsonObject();
        sc.add("steps", pair);
        field("playerA").set(r, a);
        field("playerB").set(r, bare("B"));
        field("sc0").set(r, sc);
        check(field("phase").get(r) == PhaseStep.PRECOMBAT_MAIN, "precondition: a scenario starts in main1");
        loopStep(r, a, pair, 0);
        loopStep(r, a, pair, 1);
        List<String> names = new ArrayList<>();
        List<PhaseStep> at = new ArrayList<>();
        for (PlayerAction act : a.getActions()) {
            names.add(act.getActionName());
            at.add(act.getStep());
        }
        check(names.equals(Arrays.asList("step 0 (pass)", ScenarioReplay.CMD_REQUIRE_EMPTY_STACK, "step 1 (pass)")),
                "opening pair queue order; got " + names);
        check(at.equals(Arrays.asList(PhaseStep.PRECOMBAT_MAIN, PhaseStep.PRECOMBAT_MAIN, PhaseStep.BEGIN_COMBAT)),
                "the first pass and the empty-stack check stay in main1, the second pass's checkpoint is begin combat "
                        + "(gorge: step 1 (pass) at begin-combat); got " + at);
        check(!names.contains(ScenarioReplay.CMD_REQUIRE_STACK) && !names.contains(ScenarioReplay.CMD_WAIT_RESOLVE_ONE),
                "an opening pair must not wait for a stack object; got " + names);

        // A pair after a cast is unchanged: it resolves the top object.
        JsonArray cast = steps("[{op:'cast',seat:0},{op:'pass',seat:0},{op:'pass',seat:1}]");
        check(ScenarioReplay.passCommands(cast, 2, 0).equals(Arrays.asList(
                        ScenarioReplay.CMD_REQUIRE_STACK, ScenarioReplay.CMD_WAIT_RESOLVE_ONE)),
                "a pair after a cast still resolves one object; got " + ScenarioReplay.passCommands(cast, 2, 0));
        boolean threw = false;
        try {
            ScenarioReplay.stepAfterEmptyPass(PhaseStep.END_TURN);
        } catch (IllegalArgumentException expected) {
            threw = true;
        }
        check(threw, "an opening pair outside main1 must fail loudly");
        System.out.println("PASS opening pass pair (empty stack, checkpoint at begin combat; cast pair unchanged)");
    }

    /** The same opening pair shape, but with one object on the stack: the pair
     * is a prelude to the steps after it (Shadow of the Goblin trigger#0.3:
     * setupResolvePrelude emits {pass p0},{pass p1} before the cast that fires
     * the trigger, and the beginning-of-main1 trigger sits on the stack at the
     * pair). It must resolve the top object and keep its checkpoint in main1,
     * not demand an empty stack and advance the step. */
    private static void openingPairWithStackObject() throws Exception {
        JsonArray steps = steps("[{op:'pass',seat:0},{op:'pass',seat:1},"
                + "{op:'cast',seat:0,card:'p0:Misthollow Griffin'},{op:'resolve',seat:0}]");
        check(ScenarioReplay.passAction(steps, 1) == ScenarioReplay.PASS_PAIR_SECOND,
                "precondition: step 1 still closes the opening pair");
        check(ScenarioReplay.passCommands(steps, 1, 0).equals(Arrays.asList(
                        ScenarioReplay.CMD_REQUIRE_STACK, ScenarioReplay.CMD_WAIT_RESOLVE_ONE)),
                "a pair with a step after it must resolve the top object (the trigger on the stack), not demand an "
                        + "empty stack; got " + ScenarioReplay.passCommands(steps, 1, 0));

        TestPlayer a = bare("A");
        ScenarioReplay r = new ScenarioReplay();
        JsonObject sc = new JsonObject();
        sc.add("steps", steps);
        field("playerA").set(r, a);
        field("playerB").set(r, bare("B"));
        field("sc0").set(r, sc);
        check(field("phase").get(r) == PhaseStep.PRECOMBAT_MAIN, "precondition: a scenario starts in main1");
        loopStep(r, a, steps, 0);
        loopStep(r, a, steps, 1);
        List<String> labels = new ArrayList<>();
        List<PhaseStep> at = new ArrayList<>();
        for (PlayerAction act : a.getActions()) {
            // waitStackResolved queues a PlayerAction whose actionName is empty
            // (its command lives in getAction()); normalise to the command.
            labels.add(act.getActionName().isEmpty() ? act.getAction() : act.getActionName());
            at.add(act.getStep());
        }
        check(labels.equals(Arrays.asList("step 0 (pass)", ScenarioReplay.CMD_REQUIRE_STACK,
                        ScenarioReplay.CMD_WAIT_RESOLVE_ONE, "step 1 (pass)")),
                "the pair must check for a stack object and resolve it before the second pass's checkpoint; got " + labels);
        check(at.equals(Arrays.asList(PhaseStep.PRECOMBAT_MAIN, PhaseStep.PRECOMBAT_MAIN,
                        PhaseStep.PRECOMBAT_MAIN, PhaseStep.PRECOMBAT_MAIN)),
                "a pair that resolves the stack keeps every checkpoint in main1 (the step does not advance); got " + at);
        check(!labels.contains(ScenarioReplay.CMD_REQUIRE_EMPTY_STACK) && !labels.contains(ScenarioReplay.CMD_ADVANCE_STEP),
                "a pair with an object on the stack must not demand an empty stack or advance the step; got " + labels);
        System.out.println("PASS opening pass pair with a stack object (resolves the trigger, checkpoint stays in main1)");
    }

    /** TestPlayer.findPermanent with FilterCreatureForCombat, modelled: a
     * "Name:k" command takes the k-th untapped creature of that name in
     * battlefield order, and declaring it taps it. Returns the creature
     * indices declared, or null when a command is left over. */
    private static List<Integer> consume(List<String> battlefield, List<String> commands) {
        boolean[] tapped = new boolean[battlefield.size()];
        List<Integer> declared = new ArrayList<>();
        for (String cmd : commands) {
            String name = ScenarioReplay.combatBaseName(cmd);
            int k = ScenarioReplay.combatIndex(cmd);
            int seen = 0;
            int hit = -1;
            for (int i = 0; i < battlefield.size(); i++) {
                if (battlefield.get(i).equals(name) && !tapped[i] && seen++ == k) {
                    hit = i;
                    break;
                }
            }
            if (hit < 0) {
                return null;
            }
            tapped[hit] = true;
            declared.add(hit);
        }
        return declared;
    }

    private static void attackOrder() {
        // Okoye / Bone-Cairn Butcher / Starry-Eyed Skyrider: Bears plus two
        // Dragon Fodder Goblins. combatName spells "#2" as ":1".
        List<String> field = Arrays.asList("Grizzly Bears", "Goblin Token", "Goblin Token");
        List<String> scripted = Arrays.asList("Grizzly Bears", "Goblin Token", "Goblin Token:1");
        check(consume(field, scripted) == null,
                "precondition: in scenario order the second Goblin Token command finds no untapped Goblin");
        List<String> ordered = ScenarioReplay.attackCommandOrder(scripted);
        check(ordered.equals(Arrays.asList("Grizzly Bears", "Goblin Token:1", "Goblin Token")),
                "same-name attackers must be declared highest index first; got " + ordered);
        List<Integer> declared = consume(field, ordered);
        check(declared != null && declared.size() == 3 && declared.containsAll(Arrays.asList(0, 1, 2)),
                "every attacker must be declared exactly once; got " + declared);

        // Deepway Navigator / Case of the Gateway Express: three of a name.
        List<String> three = Arrays.asList("Coral Merfolk", "Coral Merfolk", "Coral Merfolk");
        List<String> merfolk = Arrays.asList("Coral Merfolk", "Coral Merfolk:1", "Coral Merfolk:2");
        check(consume(three, merfolk) == null, "precondition: scenario order leaves a merfolk command over");
        List<Integer> m = consume(three, ScenarioReplay.attackCommandOrder(merfolk));
        check(m != null && m.containsAll(Arrays.asList(0, 1, 2)), "three same-name attackers; got " + m);

        // Distinct names and a colon inside a card name keep their order.
        List<String> plain = Arrays.asList("Summon: Bahamut", "Grizzly Bears", "Shivan Dragon");
        check(ScenarioReplay.attackCommandOrder(plain).equals(plain), "distinct names must keep scenario order");
        System.out.println("PASS attack command order (same-name duplicates highest index first)");
    }

    public static void main(String[] args) throws Exception {
        openingPair();
        openingPairWithStackObject();
        attackOrder();
    }
}
