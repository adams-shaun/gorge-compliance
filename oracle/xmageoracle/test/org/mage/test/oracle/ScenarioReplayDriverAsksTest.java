package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonParser;
import mage.constants.EmptyNames;
import org.mage.test.player.TestPlayer;

import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/** No database or game: pins the driver asks. Each block names the Level B row it clears. Run
 * scripts/xmage-oracle-test-driver-asks.sh; the replay itself is a real replay. */
public final class ScenarioReplayDriverAsksTest {
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
        manifestedFaceDownTarget();
        declinedDig();
        alternativeCast();
        promisedGift();
        replacementOrder();
        loneForcedTarget();
        multiCandidateSearch();
        phyrexianLife();
        wishDeclined();
        System.out.println("PASS driver asks: active p1 pair, battlefield move, game over, mixed must-attack,"
                + " face-down attacker, manifested face-down target, declined dig, alternative cast, promised gift, replacement order,"
                + " lone forced target, multi-candidate search, phyrexian life, wish decline");
    }

    /** (a) FIN Zidane / TLA Wan Shi Tong trigger#0.1: seat 1 casts on its
     * own turn and the p1 -> p0 pass pair resolves the spell on seat 1's
     * queue. A pair whose next step answers a mid-resolution choose (Wan Shi
     * Tong's opponent tutor) defers the resolution to that pass_to. */
    /** Edgewall Inn / Zhao + Deserted Beach: XMage asks the controller to order
     * simultaneous replacement effects, which gorge never poses. The driver
     * answers an unscripted ask itself and leaves a scripted head alone: a
     * queued colour for the LATER as-enters ask is not a replacement text. */
    private static void replacementOrder() throws Exception {
        // The helper alone is not the fix: a merge that drops the override
        // leaves every check below passing while the strict replay fails
        // again. Pin that ScriptedChoicePlayer DECLARES the override (it
        // would otherwise inherit TestPlayer's and consume the queue head).
        Class<?> scripted = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        boolean declares = false;
        for (Method method : scripted.getDeclaredMethods()) {
            if (method.getName().equals("chooseReplacementEffect")) {
                declares = true;
            }
        }
        check(declares, "ScriptedChoicePlayer must override chooseReplacementEffect to answer an unscripted"
                + " multi-effect order ask without consuming the queue head (Edgewall Inn strict miss)");
        List<String> texts = Arrays.asList("Edgewall Inn enters tapped.", "As Edgewall Inn enters, choose a color.");
        Class<?>[] types = {List.class, java.util.Collection.class};
        check(!(Boolean) adapter("replacementScripted", types, new ArrayList<String>(), texts),
                "an empty queue scripts no replacement order");
        check(!(Boolean) adapter("replacementScripted", types, new ArrayList<>(Arrays.asList("White")), texts),
                "a queued colour is not a replacement text: the ordering ask must not consume or fail on it");
        check((Boolean) adapter("replacementScripted", types, new ArrayList<>(Arrays.asList("As Edgewall Inn enters")), texts),
                "a head naming an effect's text is a scripted order");
        check((Boolean) adapter("replacementScripted", types, new ArrayList<>(Arrays.asList("Edgewall Inn*choose a color")), texts),
                "TestPlayer's object*ability form is a scripted order");
    }

    /** North Wind Avatar: WishEffect asks "Put a card you own from outside the
     * game into your hand?" before it checks the sideboard. With none outside
     * the game it has no candidate, so the driver declines an unscripted ask
     * instead of strict-missing; a queued boolean or a non-empty sideboard is
     * left to TestPlayer. The filter form with alsoFromExile can still have an
     * exile candidate, so its wording is never declined. */
    private static void wishDeclined() throws Exception {
        String wish = "Put a card you own from outside the game into your hand?";
        String coax = "Reveal an Eldrazi card you own from outside the game or choose a face-up Eldrazi card"
                + " you own in exile. Put that card into your hand?";
        Class<?>[] types = {String.class, boolean.class, List.class};
        check((Boolean) adapter("unanswerableWishAsk", types, wish, true, new ArrayList<String>()),
                "an empty sideboard's unscripted wish must be declined");
        check(!(Boolean) adapter("unanswerableWishAsk", types, wish, false, new ArrayList<String>()),
                "a non-empty sideboard still asks");
        check(!(Boolean) adapter("unanswerableWishAsk", types, wish, true, new ArrayList<>(Arrays.asList("Yes"))),
                "a queued yes is the generator's answer");
        check(!(Boolean) adapter("unanswerableWishAsk", types, coax, true, new ArrayList<String>()),
                "the alsoFromExile wording can still have an exile candidate and must ask");
        String filtered = "Reveal a multicolored card you own from outside the game and put it into your hand?";
        check((Boolean) adapter("unanswerableWishAsk", types, filtered, true, new ArrayList<String>()),
                "an empty sideboard's filtered wish (Glittering Wish) must be declined");
        check(!(Boolean) adapter("unanswerableWishAsk", types, filtered, false, new ArrayList<String>()),
                "a filtered wish over a non-empty sideboard still asks");
        check(!(Boolean) adapter("unanswerableWishAsk", types, "Draw a card?", true, new ArrayList<String>()),
                "another chooseUse is untouched");
        // Elvish Piper and its siblings: the same candidate-free ask over an
        // empty hand.
        String piper = "Put a creature card from your hand onto the battlefield?";
        check((Boolean) adapter("unanswerableHandPutAsk", types, piper, true, new ArrayList<String>()),
                "an empty hand's unscripted put-from-hand ask must be declined");
        check(!(Boolean) adapter("unanswerableHandPutAsk", types, piper, false, new ArrayList<String>()),
                "a non-empty hand still asks");
        check(!(Boolean) adapter("unanswerableHandPutAsk", types, piper, true, new ArrayList<>(Arrays.asList("No"))),
                "a queued no is the generator's answer");
        check(!(Boolean) adapter("unanswerableHandPutAsk", types, "Draw a card?", true, new ArrayList<String>()),
                "another chooseUse is untouched");
        // The helper alone is not the fix: pin that the chooseUse override
        // actually calls it. Dropping the call would leave every check above
        // passing while the strict replay fails again.
        String src = System.getProperty("scenarioReplay.source");
        check(src != null, "scenarioReplay.source must name ScenarioReplay.java (run the driver-asks script)");
        String text = Files.readString(Path.of(src), StandardCharsets.UTF_8);
        int override = text.indexOf("public boolean chooseUse(Outcome outcome");
        check(override >= 0, "ScriptedChoicePlayer must override chooseUse");
        int call = text.indexOf("unanswerableWishAsk(", override);
        check(call >= 0 && call < override + 600,
                "ScriptedChoicePlayer.chooseUse must call unanswerableWishAsk");
    }

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

    /** DSK Conductive Machete / Dissection Tools: a manifest-dread creature
     * (CR 708.2a: no name) is made mid-window, so its ref, "Mountain", cannot
     * be queued as the face-down test command. TestPlayer's name search
     * (equip target, sacrifice-cost pick) compares the queued value to the
     * empty face-down name and never matches, so the driver's override matches
     * the face-down permanent by the card that sits under it. */
    private static void manifestedFaceDownTarget() throws Exception {
        Class<?>[] types = {boolean.class, String.class, String.class, boolean.class};
        check((Boolean) adapter("namesFaceDownCard", types, true, "Mountain", "Mountain", false),
                "the face-down Mountain is the ref's permanent");
        check(!(Boolean) adapter("namesFaceDownCard", types, true, "Mountain", "Forest", false),
                "another card's name does not match the face-down permanent");
        check(!(Boolean) adapter("namesFaceDownCard", types, false, "Mountain", "Mountain", false),
                "a face-up permanent is answered by the base name comparison, not here");
        check(!(Boolean) adapter("namesFaceDownCard", types, true, "Mountain", "@p0:Mountain#2", false)
                        && !(Boolean) adapter("namesFaceDownCard", types, true, "Mountain", ScenarioReplay.faceDownCombatName(), false)
                        && !(Boolean) adapter("namesFaceDownCard", types, true, null, "Mountain", false),
                "an alias, a test command and an unknown card are left to the base");
        check(!(Boolean) adapter("namesFaceDownCard", types, true, "Mountain", "Mountain", true),
                "a face-up Mountain keeps the ref, so the candidate order never decides");
        // The twin scan is scoped to the ask's legal candidates: a face-up
        // Mountain LAND is no candidate of an equip / "sacrifice a creature"
        // ask, so it must not stop the face-down Mountain from matching.
        java.util.UUID faceDown = java.util.UUID.randomUUID();
        java.util.UUID land = java.util.UUID.randomUUID();
        java.util.UUID creatureTwin = java.util.UUID.randomUUID();
        java.util.Map<java.util.UUID, String> names = new java.util.HashMap<>();
        names.put(land, "Mountain");
        names.put(creatureTwin, "Mountain");
        Class<?>[] twinTypes = {java.util.Set.class, java.util.UUID.class, java.util.function.Function.class,
                Iterable.class, String.class};
        java.util.function.Function<java.util.UUID, String> nameOf = names::get;
        java.util.Set<java.util.UUID> creaturesOnly = new java.util.HashSet<>(Arrays.asList(faceDown));
        check(!(Boolean) adapter("faceUpTwinAmong", twinTypes, creaturesOnly, faceDown, nameOf,
                        new ArrayList<Object>(), "Mountain"),
                "a face-up Mountain land outside the ask's candidates is no twin");
        java.util.Set<java.util.UUID> withTwin = new java.util.HashSet<>(Arrays.asList(faceDown, creatureTwin));
        check((Boolean) adapter("faceUpTwinAmong", twinTypes, withTwin, faceDown, nameOf,
                        new ArrayList<Object>(), "Mountain"),
                "a face-up candidate of the same name is a twin");
        check(!(Boolean) adapter("faceUpTwinAmong", twinTypes, withTwin, creatureTwin, nameOf,
                        new ArrayList<Object>(), "Forest"),
                "another name is no twin");
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

    /** (g) DSK Leyline of Mutation static#0.0, Land Grant's own cost and Mine
     * Collapse's sacrifice cost: cast_mode "alternative" is the election, so
     * the first alternative is key 1 whether the generator queued a leading
     * "Yes" or nothing at all (Land Grant's cost is on the card, not a gorge
     * decision). A real pick queued for the elected cost (Mine Collapse's
     * Mountain) must stay behind the key: the key is inserted ahead of it, not
     * written over it. */
    private static void alternativeCast() throws Exception {
        check(ScenarioReplay.castModeSupported("alternative"), "alternative cast_mode must be elected");
        check(!ScenarioReplay.castModeSupported("kickedboth"), "an unwired cast_mode is still rejected");
        Class<?>[] types = {String.class, List.class};
        List<String> choices = new ArrayList<>(Collections.singletonList("Yes"));
        check((Boolean) adapter("alternativeCostKey", types, "You may choose an alternative cost", choices)
                && choices.equals(Collections.singletonList("1")), "Yes -> key 1: " + choices);
        List<String> queuedNone = new ArrayList<>();
        check((Boolean) adapter("alternativeCostKey", types, "Choose an alternative cost", queuedNone)
                && queuedNone.equals(Collections.singletonList("1")), "no answer -> key 1: " + queuedNone);
        List<String> sacrificePick = new ArrayList<>(Collections.singletonList("@p0:Mountain"));
        check((Boolean) adapter("alternativeCostKey", types, "You may choose an alternative cost", sacrificePick)
                && sacrificePick.equals(Arrays.asList("1", "@p0:Mountain")),
                "a real pick stays behind key 1: " + sacrificePick);
        List<String> decline = new ArrayList<>(Collections.singletonList("Cast with no alternative cost"));
        check((Boolean) adapter("alternativeCostKey", types, "You may choose an alternative cost", decline)
                && decline.equals(Collections.singletonList("1")),
                "the plain-cast decline is replaced by key 1: " + decline);
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

    /** (k) Black Cat, Cunning Thief and Tishana's Tidebinder: a target ask
     * whose only legal candidate gorge settled without a decision. Nothing is
     * scripted for that ask, so the driver takes the lone candidate; a queued
     * answer naming the candidate, or two candidates, keeps the base's strict
     * reading. A front skip closes only an "up to" ask whose minimum is met:
     * on a mandatory ask it belongs to a later ask and must stay queued while
     * the lone candidate is taken. The override declarations are pinned here;
     * scripts/xmage-oracle-test-driver-asks.sh additionally guards the funnel
     * call sites, which reflection over a declared method cannot see. */
    private static void loneForcedTarget() throws Exception {
        Class<?> scripted = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        boolean declares = false;
        for (Method method : scripted.getDeclaredMethods()) {
            if (method.getName().equals("chooseTarget") && method.getParameterCount() == 4
                    && method.getParameterTypes()[1].getName().equals("mage.target.Target")) {
                declares = true;
            }
        }
        check(declares, "ScriptedChoicePlayer must override chooseTarget to take a lone candidate"
                + " (Black Cat, Cunning Thief's only opponent; Tishana's Tidebinder's only ability)");
        Class<?>[] targetTypes = {mage.target.Target.class};
        check(!(Boolean) adapter("skipCanCloseAsk", targetTypes, new mage.target.TargetPlayer(1, 1, false)),
                "a mandatory 1/1 ask cannot be closed by a skip");
        check((Boolean) adapter("skipCanCloseAsk", targetTypes, new mage.target.TargetPlayer(0, 1, false)),
                "an optional 0/1 ask can be closed by a skip");
        java.util.UUID bears = java.util.UUID.randomUUID();
        java.util.UUID shock = java.util.UUID.randomUUID();
        java.util.function.BiPredicate<java.util.UUID, String> byName =
                (id, name) -> name.equals(id.equals(bears) ? "Grizzly Bears" : "Shock");
        Class<?>[] types = {List.class, List.class, boolean.class, java.util.function.BiPredicate.class};
        List<java.util.UUID> one = new ArrayList<>(Collections.singletonList(bears));
        check(bears.equals(adapter("loneTargetChoice", types, new ArrayList<String>(), one, true, byName)),
                "a lone candidate with nothing scripted was not taken");
        check(bears.equals(adapter("loneTargetChoice", types,
                        new ArrayList<>(Collections.singletonList("Island")), one, true, byName)),
                "a lone candidate was lost to a later ask's queued answer");
        check(adapter("loneTargetChoice", types,
                        new ArrayList<>(Collections.singletonList("Grizzly Bears")), one, true, byName) == null,
                "a queued answer naming the candidate was overridden");
        check(adapter("loneTargetChoice", types,
                        new ArrayList<>(Arrays.asList("Island", "Grizzly Bears")), one, true, byName) == null,
                "a queued answer naming the candidate behind another ask's answer (index 1) was left"
                        + " unconsumed: TestPlayer scans the whole queue, so the base must answer");
        List<String> skip = new ArrayList<>(Collections.singletonList(TestPlayer.TARGET_SKIP));
        check(adapter("loneTargetChoice", types, skip, one, true, byName) == null,
                "a front skip was overridden on an ask it can close");
        check(bears.equals(adapter("loneTargetChoice", types, skip, one, false, byName))
                        && skip.equals(Collections.singletonList(TestPlayer.TARGET_SKIP)),
                "a mandatory ask did not take its lone candidate and leave the later ask's skip queued");
        check(adapter("loneTargetChoice", types, new ArrayList<String>(),
                        Arrays.asList(bears, shock), true, byName) == null, "a two-candidate ask was guessed");
        check(adapter("loneTargetChoice", types, new ArrayList<String>(),
                        new ArrayList<java.util.UUID>(), true, byName) == null, "an empty candidate set produced a pick");
    }

    /** (l) Gladiolus Amicitia's ETB search: a mandatory single-pick library
     * search with many legal cards and no scripted pick. The driver gathers
     * the first eligible card in the order XMage presents the search
     * (name-sorted) instead of strict-missing "Missing TARGET def". A queued
     * answer naming a candidate, at any index (the base scans the whole
     * queue), is the ask's own; a front skip that can close it is too. An
     * answer for a later ask does not stop the pick, and a skip on a fixed
     * min == max ask stays queued. A multi-pick search's count is a decision
     * of its own, so it is never guessed. */
    private static void multiCandidateSearch() throws Exception {
        java.util.UUID forest = java.util.UUID.randomUUID();
        java.util.UUID mountain = java.util.UUID.randomUUID();
        java.util.UUID swamp = java.util.UUID.randomUUID();
        java.util.function.BiPredicate<java.util.UUID, String> byName =
                (id, name) -> name.equals(id.equals(forest) ? "Forest" : id.equals(mountain) ? "Mountain" : "Swamp");
        Class<?>[] types = {List.class, List.class, boolean.class, boolean.class, java.util.function.BiPredicate.class};
        List<java.util.UUID> many = new ArrayList<>(Arrays.asList(forest, mountain, swamp));

        check(forest.equals(adapter("searchPickChoice", types,
                        new ArrayList<String>(), many, true, false, byName)),
                "an unscripted multi-candidate single-pick search did not gather the first presented card");
        check(forest.equals(adapter("searchPickChoice", types,
                        new ArrayList<>(Collections.singletonList("Shock")), many, true, false, byName)),
                "a later ask's queued answer stopped the search pick");
        check(adapter("searchPickChoice", types,
                        new ArrayList<>(Collections.singletonList("Mountain")), many, true, false, byName) == null,
                "a queued answer naming a candidate was overridden");
        check(adapter("searchPickChoice", types,
                        new ArrayList<>(Arrays.asList("Shock", "Mountain")), many, true, false, byName) == null,
                "a queued answer naming a candidate behind another ask's answer (index 1) was"
                        + " overridden: TestPlayer scans the whole queue");
        List<String> skip = new ArrayList<>(Collections.singletonList(TestPlayer.TARGET_SKIP));
        check(adapter("searchPickChoice", types, skip, many, true, true, byName) == null,
                "a front skip that can close the search was overridden");
        check(forest.equals(adapter("searchPickChoice", types, skip, many, true, false, byName))
                        && skip.equals(Collections.singletonList(TestPlayer.TARGET_SKIP)),
                "a fixed search did not take its pick and leave the later ask's skip queued");
        check(adapter("searchPickChoice", types, new ArrayList<String>(), many, false, false, byName) == null,
                "a multi-pick search (count is its own decision) was guessed");
        check(adapter("searchPickChoice", types, new ArrayList<String>(),
                        new ArrayList<java.util.UUID>(), true, false, byName) == null,
                "an empty search produced a pick");
        // The helper alone is not the fix: a merge that drops the call from
        // the TargetCardInLibrary funnel leaves every check above passing
        // while the strict replay fails again. Pin the call site.
        String src = System.getProperty("scenarioReplay.source");
        check(src != null, "scenarioReplay.source must name ScenarioReplay.java (run the driver-asks script)");
        String text = Files.readString(Path.of(src), StandardCharsets.UTF_8);
        int call = text.indexOf("= searchPickChoice(");
        check(call >= 0, "ScriptedChoicePlayer.chooseTarget(Cards, TargetCard) must call searchPickChoice"
                + " for a TargetCardInLibrary ask");
    }

    /** (j) NPH Blinding Souleater's {W/P} activation: XMage asks "Pay 2 life
     * instead of {W} (phyrexian cost)?" at CR 601.2b. A recorded life face
     * scripts one "Yes", which TestPlayer consumes (the helper must leave it
     * alone). A colour-face pick scripts nothing, so the ask must be answered
     * no WITHOUT consuming the step's next answer -- TestPlayer would
     * otherwise stop the replay at "Missing CHOICE def" before the pool pays
     * the pip. The other Phyrexian wording (ManaCostsImpl's "?" and K'rrik's
     * "(pay life cost)?") is the same ask. */
    private static void phyrexianLife() throws Exception {
        String[] asks = {
            "Pay 2 life instead of {W} (phyrexian cost)?",
            "Pay 2 life instead of {B} (pay life cost)?",
            "Pay 2 life instead of {U}?"
        };
        Class<?>[] one = {String.class};
        for (String ask : asks) {
            check((Boolean) adapter("phyrexianAsk", one, ask), "not recognised as a Phyrexian ask: " + ask);
        }
        for (String other : new String[] {"Pay 1 life.", "Pay {W}?", "Choose a color", null}) {
            check(!(Boolean) adapter("phyrexianAsk", one, other), "misread as a Phyrexian ask: " + other);
        }
        Class<?>[] two = {String.class, List.class};
        String ask = asks[0];
        check(!(Boolean) adapter("phyrexianStarved", two, ask, new ArrayList<>(Collections.singletonList("Yes"))),
                "a queued life-face Yes is left for TestPlayer to consume");
        check(!(Boolean) adapter("phyrexianStarved", two, ask, new ArrayList<>(Collections.singletonList("No"))),
                "a queued No is left for TestPlayer to consume");
        check((Boolean) adapter("phyrexianStarved", two, ask, new ArrayList<String>()),
                "no queued answer: a colour-face pick is answered no, not Missing CHOICE def");
        check((Boolean) adapter("phyrexianStarved", two, ask,
                new ArrayList<>(Collections.singletonList(TestPlayer.CHOICE_SKIP))),
                "the step's next non-boolean answer is not consumed");
        check(!(Boolean) adapter("phyrexianStarved", two, "Choose a color", new ArrayList<String>()),
                "another ask is untouched");
        // Mixed faces: the generator scripts one boolean per pip in cost
        // order (mana pip first, life pip second), so the queue holds
        // [No, Yes]. The positional guarantee is the generator's, pinned
        // end-to-end by the Go test TestXAnswersPhyrexianMixedFaces (reverting
        // its wiring reproduces the swap). This block only pins the driver
        // side: the starvation check must leave the pair intact, so TestPlayer
        // consumes the mana pip's No then the life pip's Yes.
        List<String> pair = new ArrayList<>(Arrays.asList("No", "Yes"));
        check(!(Boolean) adapter("phyrexianStarved", two, ask, pair),
                "the mana pip's queued No must not starve the first ask");
        check("No".equals(pair.get(0)), "the starvation check leaves the first ask's answer at the head");
        List<String> rest = new ArrayList<>(pair.subList(1, pair.size()));
        check(!(Boolean) adapter("phyrexianStarved", two, ask, rest),
                "the life pip's queued Yes must not starve the second ask");
        check("Yes".equals(rest.get(0)), "the second ask still sees the life pip's Yes");
        check(pair.size() == 2 && rest.size() == 1,
                "the starvation check never consumes or reorders the queue");
    }
}
