package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonParser;
import mage.constants.EmptyNames;
import org.mage.test.player.TestPlayer;

import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/** No database or game: pins the wave-4 driver asks (the lb4w2-driver
 * branch). Each block names the Level B row it clears. Run
 * scripts/xmage-oracle-test-wave4-driver.sh; the replay itself is a host gate. */
public final class ScenarioReplayWave4DriverTest {
    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static JsonArray steps(String json) {
        return JsonParser.parseString(json).getAsJsonArray();
    }

    private static boolean throwsIae(Runnable r) {
        try {
            r.run();
            return false;
        } catch (IllegalArgumentException expected) {
            return true;
        }
    }

    private static Object adapter(String name, Class<?>[] types, Object... args) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod(name, types);
        method.setAccessible(true);
        return method.invoke(null, args);
    }

    public static void main(String[] args) throws Exception {
        activeSeatOnePair();
        battlefieldMove();
        gameOverLeftovers();
        mixedMustAttack();
        faceDownAttacker();
        declinedDig();
        alternativeCast();
        promisedGift();
        System.out.println("PASS wave-4 driver asks: active p1 pair, battlefield move, game over, mixed must-attack,"
                + " face-down attacker, declined dig, alternative cast, promised gift");
    }

    /** (a) FIN Zidane / TLA Wan Shi Tong trigger#0.1: seat 1 casts on its
     * own turn and the p1 -> p0 pass pair resolves the spell on seat 1's
     * queue. A pair whose next step answers a mid-resolution choose (Wan Shi
     * Tong's opponent tutor) defers the resolution to that pass_to. */
    private static void activeSeatOnePair() {
        JsonArray zidane = steps("[{op:'pass_to',seat:0,step:'main1',active:'p1'},"
                + "{op:'cast',seat:1,card:'p1:Threaten',targets:['p0:Llanowar Elves']},"
                + "{op:'pass',seat:1},{op:'pass',seat:0},{op:'resolve',seat:0}]");
        check(ScenarioReplay.isActiveP1Pair(zidane, 2, 1) && ScenarioReplay.isActiveP1Pair(zidane, 3, 1),
                "both passes of seat 1's own pair must classify as the active p1 pair");
        check(ScenarioReplay.passCommands(zidane, 2, 1).isEmpty(), "the pair's first pass queues nothing");
        check(ScenarioReplay.passCommands(zidane, 3, 1).equals(Arrays.asList(
                ScenarioReplay.CMD_REQUIRE_STACK, ScenarioReplay.CMD_WAIT_RESOLVE_ONE)),
                "the pair's second pass resolves one object: " + ScenarioReplay.passCommands(zidane, 3, 1));
        check(ScenarioReplay.passCommands(zidane, 4, 1).isEmpty(), "the resolve after the pair queues nothing");
        // Precondition: the same pair with seat 0 active is NOT the active
        // seat 1's pair (it is the existing p1-first instant-speed shape).
        check(!ScenarioReplay.isActiveP1Pair(zidane, 3, 0), "seat 0 active: not the active p1 pair");
        // A p0-first pair on seat 1's turn is still refused.
        JsonArray p0first = steps("[{op:'pass_to',seat:0,step:'main1',active:'p1'},"
                + "{op:'cast',seat:1,card:'p1:Shock'},{op:'pass',seat:0},{op:'pass',seat:1}]");
        check(throwsIae(() -> ScenarioReplay.passCommands(p0first, 3, 1)),
                "a p0-first pair on seat 1's turn must still be rejected");
        // A p1 pair with no seat-1 cast before it is not this shape.
        JsonArray bare = steps("[{op:'pass_to',seat:0,step:'main1',active:'p1'},{op:'pass',seat:1},{op:'pass',seat:0}]");
        check(!ScenarioReplay.isActiveP1Pair(bare, 2, 1), "a bare p1 pair is not the cast-then-pass shape");

        JsonArray wanShiTong = steps("[{op:'pass_to',seat:0,step:'main1',active:'p1'},"
                + "{op:'cast',seat:1,card:'p1:Demonic Tutor'},{op:'pass',seat:1},{op:'pass',seat:0},"
                + "{op:'pass_to',seat:0,decision:'priority',answers:[{kind:'choose',pick:['Wastes']}]},"
                + "{op:'resolve',seat:0}]");
        check(ScenarioReplay.pairEndsMidResolution(wanShiTong, 3), "the tutor's pair stops mid-resolution");
        check(ScenarioReplay.passCommands(wanShiTong, 3, 1).equals(Collections.singletonList(ScenarioReplay.CMD_REQUIRE_STACK)),
                "a mid-resolution pair leaves the spell on the stack: " + ScenarioReplay.passCommands(wanShiTong, 3, 1));
        check(ScenarioReplay.passToResolvesTop(wanShiTong, 4), "the answering pass_to resolves the deferred top object");
        check(!ScenarioReplay.pairEndsMidResolution(zidane, 3), "a pair followed by a resolve is not mid-resolution");
        // A pass_to priority with no choose answer after a pair stays a plain
        // checkpoint (Twinflame Travelers' static rows).
        JsonArray plain = steps("[{op:'cast',seat:0,card:'p0:Shock'},{op:'pass',seat:0},{op:'pass',seat:1},"
                + "{op:'pass_to',seat:0,decision:'priority'}]");
        check(!ScenarioReplay.pairEndsMidResolution(plain, 2) && !ScenarioReplay.passToResolvesTop(plain, 3),
                "an unanswered pass_to after a pair must not resolve anything");
        check(ScenarioReplay.passCommands(plain, 2, 0).equals(Arrays.asList(
                ScenarioReplay.CMD_REQUIRE_STACK, ScenarioReplay.CMD_WAIT_RESOLVE_ONE)), "seat 0's pair is unchanged");
    }

    /** (b) FIN The Masamune static#0.1: a move of a battlefield ref (a
     * dies-equivalent move to the graveyard). Card zones stay first. */
    private static void battlefieldMove() {
        check("hand card".equals(ScenarioReplay.moveSource("hand card", () -> "permanent")),
                "a card found in a card zone is moved, not the permanent");
        check("permanent".equals(ScenarioReplay.<String>moveSource(null, () -> "permanent")),
                "a ref in no card zone falls back to its battlefield permanent");
        check(ScenarioReplay.<String>moveSource(null, () -> null) == null, "a ref nowhere stays a failure");
    }

    /** (c) DSK Marina Vendrell's Grimoire trigger#0.2, LCI The Millennium
     * Calendar trigger#0.1: at game over only a tail of the unreached
     * steps' queue may be left over. */
    private static void gameOverLeftovers() {
        List<List<String>> queued = Arrays.asList(Arrays.asList("cast", "cp0"), Arrays.asList("wait", "cp1"));
        check(ScenarioReplay.isQueuedSuffix(Arrays.asList("wait", "cp1"), queued, 1), "the unreached step's whole queue");
        check(ScenarioReplay.isQueuedSuffix(Collections.singletonList("cp1"), queued, 1), "a step the game ended inside");
        check(ScenarioReplay.isQueuedSuffix(Collections.emptyList(), queued, 1), "nothing left over");
        check(!ScenarioReplay.isQueuedSuffix(Arrays.asList("cp0", "wait", "cp1"), queued, 1),
                "a completed step's leftover must still fail the row");
        check(!ScenarioReplay.isQueuedSuffix(Collections.singletonList("wait"), queued, 1),
                "a leftover that is not a tail (a later ask consumed out of order) must fail");
        check(ScenarioReplay.isLeftoverAssertion("Player PlayerA must have 0 actions but found 2"), "action leftover");
        check(ScenarioReplay.isLeftoverAssertion("(Choices of PlayerA) Count are not equal (found [Yes])"), "choice leftover");
        check(!ScenarioReplay.isLeftoverAssertion("Can't find ability to activate command: Cast Shock"),
                "any other failure is not a leftover");
    }

    /** (d) MSH Ares, God of War trigger#0.0: XMage declares the must-attack
     * creature itself, so only the others get an attack command. */
    private static void mixedMustAttack() {
        List<String> left = ScenarioReplay.withoutMustAttack(Arrays.asList("Ares, God of War", "Grizzly Bears"),
                n -> n.equals("Ares, God of War"));
        check(left.equals(Collections.singletonList("Grizzly Bears")), "must-attack name dropped: " + left);
        List<String> dup = ScenarioReplay.withoutMustAttack(Arrays.asList("Goblin Token:1", "Goblin Token", "Ares, God of War"),
                n -> n.equals("Ares, God of War"));
        check(dup.equals(Arrays.asList("Goblin Token:1", "Goblin Token")), "others keep order and indices: " + dup);
        check(ScenarioReplay.withoutMustAttack(Collections.singletonList("Goblin Token:1"), n -> n.equals("Goblin Token")).isEmpty(),
                "an indexed duplicate is matched by its base name");
    }

    /** (e) MKM Yarus trigger#0.0: a face-down attacker has no name, so the
     * combat command is TestPlayer's face-down test command. */
    private static void faceDownAttacker() {
        String name = ScenarioReplay.faceDownCombatName();
        check(name.equals("[face_down_creature]"), "face-down combat name: " + name);
        check(EmptyNames.replaceTestCommandByObjectName(name).isEmpty(),
                "precondition: TestPlayer maps the command back to the empty face-down name");
        check(ScenarioReplay.turnsFaceUp("Turn face up") && ScenarioReplay.turnsFaceUp("Megamorph {1}{G}: turn this face up")
                && !ScenarioReplay.turnsFaceUp("{T}: Add {G}."), "only a turn-face-up activation clears the face-down name");
    }

    /** (f) FIN Choco trigger#0.0: the generator's decline pair ("No" +
     * "[target_skip]") becomes one [choice_skip] for an optional
     * choose(Cards) dig; everything else is left alone. */
    private static void declinedDig() throws Exception {
        Class<?>[] types = {int.class, List.class, List.class};
        List<String> choices = new ArrayList<>(Arrays.asList("No", "No"));
        List<String> targets = new ArrayList<>(Arrays.asList(TestPlayer.TARGET_SKIP, TestPlayer.TARGET_SKIP));
        check((Boolean) adapter("declinePairToChoiceSkip", types, 0, choices, targets), "the decline pair is rewritten");
        check(choices.equals(Arrays.asList(TestPlayer.CHOICE_SKIP, "No")) && targets.equals(Collections.singletonList(TestPlayer.TARGET_SKIP)),
                "only the front pair is rewritten: " + choices + " " + targets);
        List<String> required = new ArrayList<>(Collections.singletonList("No"));
        List<String> skip = new ArrayList<>(Collections.singletonList(TestPlayer.TARGET_SKIP));
        check(!(Boolean) adapter("declinePairToChoiceSkip", types, 1, required, skip) && required.get(0).equals("No"),
                "a required dialog is never declined");
        List<String> named = new ArrayList<>(Collections.singletonList("Forest"));
        check(!(Boolean) adapter("declinePairToChoiceSkip", types, 0, named, new ArrayList<>(skip)), "a named pick is kept");
        List<String> loneNo = new ArrayList<>(Collections.singletonList("No"));
        check(!(Boolean) adapter("declinePairToChoiceSkip", types, 0, loneNo, new ArrayList<String>()),
                "a lone No (Oko's chooseUse) is kept");
    }

    /** (g) DSK Leyline of Mutation static#0.0: cast_mode "alternative" is a
     * plain cast whose leading Yes picks XMage's first alternative cost. */
    private static void alternativeCast() throws Exception {
        check(ScenarioReplay.castModeSupported("alternative"), "alternative cast_mode must be elected");
        check(!ScenarioReplay.castModeSupported("kicked"), "an unwired cast_mode is still rejected");
        Class<?>[] types = {String.class, List.class};
        List<String> choices = new ArrayList<>(Collections.singletonList("Yes"));
        check((Boolean) adapter("alternativeCostKey", types, "You may choose an alternative cost", choices)
                && choices.get(0).equals("1"), "Yes -> key 1: " + choices);
        List<String> other = new ArrayList<>(Collections.singletonList("Yes"));
        check(!(Boolean) adapter("alternativeCostKey", types, "Choose a creature type", other) && other.get(0).equals("Yes"),
                "another key menu is untouched");
    }

    /** (i) BLB Jolly Gerbils trigger#0.0: a promised Gift names the lone
     * opponent unless the scenario scripted one. */
    private static void promisedGift() throws Exception {
        Class<?>[] types = {boolean.class, List.class, List.class};
        check("PlayerB".equals(adapter("giftOpponentChoice", types, true, Collections.singletonList("PlayerB"), new ArrayList<String>())),
                "the lone opponent is chosen");
        check(adapter("giftOpponentChoice", types, true, Collections.singletonList("PlayerB"),
                new ArrayList<>(Collections.singletonList("PlayerB"))) == null, "a scripted opponent is kept");
        check(adapter("giftOpponentChoice", types, false, Collections.singletonList("PlayerB"), new ArrayList<String>()) == null,
                "not a gift ask");
        check(adapter("giftOpponentChoice", types, true, Arrays.asList("PlayerB", "PlayerC"), new ArrayList<String>()) == null,
                "two opponents is a real choice");
    }
}
