package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonParser;
import mage.abilities.Ability;
import mage.constants.EmptyNames;
import mage.constants.Zone;
import mage.game.Game;
import mage.game.permanent.Permanent;
import org.mage.test.player.TestPlayer;

import java.lang.reflect.Constructor;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;
import java.util.UUID;

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
        blackCatLookExileChoice();
        alternativeCast();
        promisedGift();
        replacementOrder();
        loneForcedTarget();
        dividedAmountUnscripted();
        multiCandidateSearch();
        phyrexianLife();
        wishDeclined();
        emptyOptionalAnimate();
        optionalMultiCandidateTarget();
        mandatoryChoosePickOnTargetQueue();
        cardOrderPick();
        System.out.println("PASS driver asks: active p1 pair, battlefield move, game over, mixed must-attack,"
                + " face-down attacker, manifested face-down target, declined dig, look-at/exile same-name choice,"
                + " alternative cast, promised gift, replacement order,"
                + " lone forced target, divided amount, multi-candidate search, phyrexian life, wish decline,"
                + " empty optional animate, optional multi-candidate target, mandatory choose pick on the target queue,"
                + " same-name card order");
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

    /** Disruptive Stormbrood: an optional "up to one target artifact or
     * enchantment" with two legal candidates. The generator scripts the
     * decision gorge's own replay made (a decline's [target_skip] or a pick's
     * name); the driver must not guess between them. An explicit skip and an
     * explicit pick are both this ask's own answer and stay at the queue front
     * for TestPlayer, an unscripted ask is not declared unscriptable (it stays
     * the base's strict miss), and a skip queued for a LATER target object is
     * hidden from this ask. */
    private static void optionalMultiCandidateTarget() throws Exception {
        Class<?>[] empty = {int.class, int.class, boolean.class};
        check(!(Boolean) adapter("unscriptableEmptyAsk", empty, 0, 2, false),
                "an optional ask with two candidates and no answer must not be auto-declined");
        check(!(Boolean) adapter("unscriptableEmptyAsk", empty, 0, 2, true),
                "an optional ask with two candidates and its own skip is the base's");
        Class<?>[] hide = {List.class};
        List<String> skip = new ArrayList<>(Collections.singletonList(TestPlayer.TARGET_SKIP));
        check(((List<?>) adapter("hideAfterNextSkip", hide, skip)).isEmpty()
                        && skip.equals(Collections.singletonList(TestPlayer.TARGET_SKIP)),
                "an explicit skip stays at the front for this ask");
        List<String> pick = new ArrayList<>(Collections.singletonList("Seal of Removal"));
        check(((List<?>) adapter("hideAfterNextSkip", hide, pick)).isEmpty()
                        && pick.equals(Collections.singletonList("Seal of Removal")),
                "an explicit pick stays at the front for this ask");
        List<String> laterSkip = new ArrayList<>(Arrays.asList("Seal of Removal", TestPlayer.TARGET_SKIP));
        check(((List<?>) adapter("hideAfterNextSkip", hide, laterSkip))
                        .equals(Collections.singletonList(TestPlayer.TARGET_SKIP)),
                "a skip queued for a later target object is hidden from this ask");
        check(laterSkip.equals(Collections.singletonList("Seal of Removal")),
                "the explicit pick remains this ask's head answer");
    }

    /** Wayward Guide-Beast: a mandatory non-targeting choose of a controlled land whose pick the
     * generator scripted on the TARGET queue moves to the CHOICE queue. */
    private static void mandatoryChoosePickOnTargetQueue() throws Exception {
        Class<?>[] types = {List.class, List.class, boolean.class, java.util.function.Predicate.class};
        java.util.function.Predicate<String> lands = a -> a.equals("@p0:Mountain") || a.equals("@p0:Forest");
        List<String> choices = new ArrayList<>();
        List<String> targets = new ArrayList<>(Arrays.asList("@p0:Mountain", "Other"));
        check((Boolean) adapter("movePickToChoices", types, choices, targets, true, lands),
                "a target-queue pick naming a candidate moves");
        check(choices.equals(Collections.singletonList("@p0:Mountain")) && targets.equals(Collections.singletonList("Other")),
                "the pick leads the choice queue and leaves the target queue: " + choices + " " + targets);
        choices = new ArrayList<>(Arrays.asList("@p0:Forest"));
        targets = new ArrayList<>(Arrays.asList("@p0:Mountain"));
        check(!(Boolean) adapter("movePickToChoices", types, choices, targets, true, lands),
                "a choice head that names a candidate is this ask's own answer");
        choices = new ArrayList<>();
        targets = new ArrayList<>(Arrays.asList("Other"));
        check(!(Boolean) adapter("movePickToChoices", types, choices, targets, true, lands),
                "a target head naming no candidate stays");
        targets = new ArrayList<>(Arrays.asList("@p0:Mountain"));
        check(!(Boolean) adapter("movePickToChoices", types, choices, targets, false, lands),
                "an ask with no candidates takes nothing");
        choices = new ArrayList<>(Arrays.asList(TestPlayer.CHOICE_SKIP));
        check(!(Boolean) adapter("movePickToChoices", types, choices, targets, true, lands),
                "a choice skip is this ask's own answer");
        // The helper alone is not the fix: pin that the choose(Outcome, Target...)
        // override calls it, after the min-0 may handling and before forcedChoose,
        // behind the role guards (a TargetPermanent ask that is not one of the
        // source's own targets). Dropping the call or a guard would leave every
        // check above passing while the strict replay fails again.
        String src = System.getProperty("scenarioReplay.source");
        check(src != null, "scenarioReplay.source must name ScenarioReplay.java (run the driver-asks script)");
        String text = Files.readString(Path.of(src), StandardCharsets.UTF_8);
        int override = text.indexOf("public boolean choose(Outcome outcome, mage.target.Target target, Ability source, Game game)");
        check(override >= 0, "ScriptedChoicePlayer must override choose(Outcome, Target, Ability, Game)");
        int forced = text.indexOf("forcedChoose(target, source, game)", override);
        int call = text.indexOf("movePickToChoices(", override);
        check(call >= 0 && forced >= 0 && call < forced,
                "ScriptedChoicePlayer.choose must call movePickToChoices before forcedChoose");
        String guard = text.substring(Math.max(override, call - 1200), call);
        check(guard.contains("getMinNumberOfTargets() > 0")
                        && guard.contains("instanceof mage.target.TargetPermanent")
                        && guard.contains("source.getTargets().contains(target)")
                        && guard.contains("source.getTargets().contains(target.getOriginalTarget())"),
                "the move must stay behind the mandatory / TargetPermanent / not-the-source's-own-target guards");
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

    /** Sita Varma, Masked Racer's Exhaust: with no other creature, "you may
     * have the base power and toughness of each other creature you control
     * become equal to Sita Varma's power" changes nothing either way (CR
     * 608.2d), so gorge logs no ask while XMage asks anyway. The driver
     * declines an unscripted ask, keeps it when another controlled creature
     * exists, and leaves a queued Yes/No for TestPlayer. */
    private static void emptyOptionalAnimate() throws Exception {
        String ask = "Have the base power and toughness of each other creature you control"
                + " become equal to Sita Varma's power until end of turn?";
        Class<?>[] types = {String.class, boolean.class, List.class};
        check((Boolean) adapter("unanswerableEmptyAnimateAsk", types, ask, false, new ArrayList<String>()),
                "an unscripted empty animate ask must be declined");
        check(!(Boolean) adapter("unanswerableEmptyAnimateAsk", types, ask, true, new ArrayList<String>()),
                "another controlled creature keeps the ask");
        check(!(Boolean) adapter("unanswerableEmptyAnimateAsk", types, ask, false,
                        new ArrayList<>(Collections.singletonList("No"))),
                "a queued no is the generator's answer and stays queued");
        check(!(Boolean) adapter("unanswerableEmptyAnimateAsk", types, ask, false,
                        new ArrayList<>(Collections.singletonList("Yes"))),
                "a queued yes is the generator's answer and stays queued");
        check(!(Boolean) adapter("unanswerableEmptyAnimateAsk", types, "Draw a card?", false, new ArrayList<String>()),
                "another chooseUse is untouched");

        // The battlefield scan counts only the controller's other creatures:
        // Sita herself, a land, and an opponent's creature are not "each other
        // creature you control", while p0's second creature is.
        UUID p0 = UUID.fromString("00000000-0000-0000-0000-0000000000a0");
        UUID p1 = UUID.fromString("00000000-0000-0000-0000-0000000000a1");
        UUID sita = UUID.fromString("00000000-0000-0000-0000-0000000000b0");
        UUID land = UUID.fromString("00000000-0000-0000-0000-0000000000b1");
        UUID bears = UUID.fromString("00000000-0000-0000-0000-0000000000b2");
        Game game = (Game) Proxy.newProxyInstance(Game.class.getClassLoader(),
                new Class<?>[]{Game.class}, (proxy, method, args) -> null);
        Ability ability = (Ability) Proxy.newProxyInstance(Ability.class.getClassLoader(),
                new Class<?>[]{Ability.class}, (proxy, method, args) -> {
                    if (method.getName().equals("getControllerId")) {
                        return p0;
                    }
                    if (method.getName().equals("getSourceId")) {
                        return sita;
                    }
                    return null;
                });
        Class<?>[] fieldTypes = {Iterable.class, Game.class, Ability.class};
        check(!(Boolean) adapter("hasOtherControlledCreature", fieldTypes,
                        Arrays.asList(permanent(sita, p0, true), permanent(land, p0, false)), game, ability),
                "Sita Varma herself and a land are not another creature");
        check((Boolean) adapter("hasOtherControlledCreature", fieldTypes,
                        Arrays.asList(permanent(sita, p0, true), permanent(bears, p0, true)), game, ability),
                "a second creature controlled by the same player keeps the ask");
        check(!(Boolean) adapter("hasOtherControlledCreature", fieldTypes,
                        Arrays.asList(permanent(sita, p0, true), permanent(bears, p1, true)), game, ability),
                "an opponent's creature is not one you control");
        check(!(Boolean) adapter("hasOtherControlledCreature", fieldTypes,
                        Collections.singletonList(permanent(sita, p0, true)), game, ability),
                "the source permanent alone is no other creature");

        // The helper alone is not the fix: pin that chooseUse calls it with
        // the battlefield scan. Dropping the call would leave every check
        // above passing while the strict replay fails again.
        String src = System.getProperty("scenarioReplay.source");
        check(src != null, "scenarioReplay.source must name ScenarioReplay.java (run the driver-asks script)");
        String text = Files.readString(Path.of(src), StandardCharsets.UTF_8);
        int override = text.indexOf("public boolean chooseUse(Outcome outcome");
        check(override >= 0, "ScriptedChoicePlayer must override chooseUse");
        int call = text.indexOf("unanswerableEmptyAnimateAsk(message,", override);
        int scan = text.indexOf("hasOtherControlledCreature(game.getBattlefield().getAllActivePermanents(), game, source)", override);
        check(call >= 0 && call < override + 900 && scan > call,
                "ScriptedChoicePlayer.chooseUse must call unanswerableEmptyAnimateAsk with"
                        + " hasOtherControlledCreature over the active battlefield");
    }

    private static Permanent permanent(UUID id, UUID controller, boolean creature) {
        return (Permanent) Proxy.newProxyInstance(Permanent.class.getClassLoader(),
                new Class<?>[]{Permanent.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getId":
                            return id;
                        case "getControllerId":
                            return controller;
                        case "isCreature":
                            return creature;
                        case "hashCode":
                            return System.identityHashCode(proxy);
                        case "equals":
                            return proxy == args[0];
                        case "toString":
                            return "permanent " + id;
                        default:
                            throw new UnsupportedOperationException(method.getName());
                    }
                });
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

    /** (f2) SPM Black Cat, Cunning Thief trigger#0.0: XMage's look-at-the-top
     * choose(Cards, TargetCard) asks for TWO cards from nine, and the item cut
     * ends before gorge's decision, so the choice queue is empty. Every
     * offered card is a Plains (gorge's decision moves two same-named objects,
     * so any subset is the same named game state): the driver takes the first
     * two the dialog offers. The shape gates keep it from becoming a default
     * pick: distinct names, an open count, a non-library zone, a non-required
     * ask and an ask inside the compared window (before the last checkpoint)
     * all decline. A caret-joined multi-pick choice answer ("Plains^Plains")
     * names its candidates through its segments and stays for the base player,
     * never the fall-through. The same shape reaches the chooseTarget(Cards)
     * funnel (Dimir Charm's "put one back"), so both funnels must call the
     * helper. */
    private static void blackCatLookExileChoice() throws Exception {
        Class<?>[] types = {boolean.class, boolean.class, int.class, int.class, Zone.class, int.class, List.class,
                List.class, java.util.function.Function.class};
        java.util.UUID a = java.util.UUID.randomUUID();
        java.util.UUID b = java.util.UUID.randomUUID();
        java.util.UUID c = java.util.UUID.randomUUID();
        java.util.UUID later = java.util.UUID.randomUUID();
        List<java.util.UUID> offer = Arrays.asList(a, b, c);
        List<java.util.UUID> candidates = Arrays.asList(a, b, c);
        java.util.function.Function<java.util.UUID, String> plains = id -> "Plains";
        List<?> picked = (List<?>) adapter("interchangeableLibraryPick", types,
                true, true, 2, 2, Zone.LIBRARY, 2, offer, candidates, plains);
        check(picked != null && picked.equals(Arrays.asList(a, b)),
                "two of nine same-named looked-at cards are the first two offered: " + picked);
        // The offered order, not the candidate order, decides which ids are
        // taken: the same library window listed another way picks those ids.
        check(((List<?>) adapter("interchangeableLibraryPick", types,
                true, true, 2, 2, Zone.LIBRARY, 2, Arrays.asList(later, b, a), candidates, plains))
                .equals(Arrays.asList(b, a)), "the offered order names the taken cards");
        java.util.Map<java.util.UUID, String> mixed = new java.util.HashMap<>();
        mixed.put(a, "Plains");
        mixed.put(b, "Plains");
        mixed.put(c, "Island");
        java.util.function.Function<java.util.UUID, String> nameOf = mixed::get;
        check(adapter("interchangeableLibraryPick", types,
                true, true, 2, 2, Zone.LIBRARY, 2, offer, candidates, nameOf) == null,
                "a distinct card is gorge's real choice and stays unscripted");
        check(adapter("interchangeableLibraryPick", types,
                true, true, 0, 2, Zone.LIBRARY, 1, offer, candidates, plains) == null,
                "an open count is never a default pick");
        check(adapter("interchangeableLibraryPick", types,
                true, true, 2, 2, Zone.BATTLEFIELD, 2, offer, candidates, plains) == null,
                "a same-named permanent can differ by attached state, so only a library window is safe");
        check(adapter("interchangeableLibraryPick", types,
                true, true, 2, 2, Zone.LIBRARY, 2, offer, Arrays.asList(a, b), plains) == null,
                "every candidate needed is the forced-pick path, not this rule");
        check(adapter("interchangeableLibraryPick", types,
                true, true, 2, 2, Zone.LIBRARY, 2, Collections.singletonList(a), candidates, plains) == null,
                "the offer must actually hold the picks");
        check(adapter("interchangeableLibraryPick", types,
                true, false, 2, 2, Zone.LIBRARY, 2, offer, candidates, plains) == null,
                "a non-required ask may take none, so it stays a decision");
        check(adapter("interchangeableLibraryPick", types,
                false, true, 2, 2, Zone.LIBRARY, 2, offer, candidates, plains) == null,
                "a pick inside the compared window is gorge's real decision, not a substitute");
        // A caret-joined multi-pick choice answer ("Plains^Plains", the
        // generator's spelling for a multi-card pick) names its candidates
        // through its segments. The whole-string check missed it in
        // forcedChooseCards, left the scripted answer at the queue front and
        // took the cards, so the replay ended with a leftover choice.
        Class<?>[] headTypes = {String.class, java.util.UUID.class, java.util.function.BiPredicate.class};
        java.util.function.BiPredicate<java.util.UUID, String> names =
                (id, part) -> id.equals(a) ? "Plains".equals(part) : "Island".equals(part);
        check((Boolean) adapter("answerNamesCandidate", headTypes, "Plains^Plains", a, names),
                "a caret-joined answer naming a candidate is this ask's own and stays for the base");
        check(!(Boolean) adapter("answerNamesCandidate", headTypes, "Island^Island", a, names),
                "a caret-joined answer naming no candidate is not this ask's own");
        // The helper alone is not the fix: pin that BOTH Cards funnels call
        // it. Dropping either call would leave the checks above passing while
        // the strict replay fails again on that card's effect API.
        String src = System.getProperty("scenarioReplay.source");
        check(src != null, "scenarioReplay.source must name ScenarioReplay.java (run the driver-asks script)");
        String text = Files.readString(Path.of(src), StandardCharsets.UTF_8);
        int choiceFunnel = text.indexOf("public boolean choose(Outcome outcome, Cards cards");
        check(choiceFunnel >= 0, "ScriptedChoicePlayer must override choose(Cards)");
        int forced = text.indexOf("forcedChooseCards(cards, target, source, game)", choiceFunnel);
        check(forced >= 0 && forced < choiceFunnel + 500,
                "choose(Cards) must call forcedChooseCards");
        // The head check must split a multi-pick answer. A whole-string
        // comparison leaves the scripted caret answer queued (leftover).
        int headCheck = text.indexOf("boolean headNamesCandidate", forced);
        int headMatch = text.indexOf("answerNamesCandidate(game, id, head)", headCheck);
        check(headCheck >= 0 && headMatch >= 0 && headMatch < headCheck + 800,
                "forcedChooseCards head detection must use the segment-aware answerNamesCandidate");
        check(text.indexOf("hasObjectTargetNameOrAlias(o, head)", choiceFunnel) < 0,
                "forcedChooseCards must not compare the whole caret-joined choice head string");
        int choiceCall = text.indexOf("interchangeableLibraryPick(", forced);
        check(choiceCall >= 0 && choiceCall < forced + 4000,
                "forcedChooseCards must call interchangeableLibraryPick");
        int targetFunnel = text.indexOf("public boolean chooseTarget(Outcome outcome, Cards cards");
        check(targetFunnel >= 0, "ScriptedChoicePlayer must override chooseTarget(Cards)");
        int targetCall = text.indexOf("interchangeableLibraryPick(", targetFunnel);
        int targetSuper = text.indexOf("return super.chooseTarget(outcome, cards", targetFunnel);
        check(targetCall >= 0 && (targetSuper < 0 || targetCall < targetSuper),
                "chooseTarget(Cards) must call interchangeableLibraryPick before the base");
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
     * search with many legal cards and no scripted pick. In the trailing play
     * -- after every checkpoint was captured, which is where the stored item
     * poses it -- the driver gathers the first eligible card in the order
     * XMage presents the search (name-sorted) instead of strict-missing
     * "Missing TARGET def". The same ask INSIDE the compared window is a real
     * decision the driver cannot infer, so it must not be guessed: the helper
     * returns null and the strict miss is kept for the loose rerun. A queued
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
        Class<?>[] types = {List.class, List.class, boolean.class, boolean.class, boolean.class,
            java.util.function.BiPredicate.class};
        List<java.util.UUID> many = new ArrayList<>(Arrays.asList(forest, mountain, swamp));

        check(forest.equals(adapter("searchPickChoice", types,
                        new ArrayList<String>(), many, true, false, true, byName)),
                "an unscripted multi-candidate single-pick search did not gather the first presented card");
        check(adapter("searchPickChoice", types,
                        new ArrayList<String>(), many, true, false, false, byName) == null,
                "a search inside the compared window was guessed instead of keeping its strict miss");
        check(forest.equals(adapter("searchPickChoice", types,
                        new ArrayList<>(Collections.singletonList("Shock")), many, true, false, true, byName)),
                "a later ask's queued answer stopped the search pick");
        check(adapter("searchPickChoice", types,
                        new ArrayList<>(Collections.singletonList("Mountain")), many, true, false, true, byName) == null,
                "a queued answer naming a candidate was overridden");
        check(adapter("searchPickChoice", types,
                        new ArrayList<>(Arrays.asList("Shock", "Mountain")), many, true, false, true, byName) == null,
                "a queued answer naming a candidate behind another ask's answer (index 1) was"
                        + " overridden: TestPlayer scans the whole queue");
        List<String> skip = new ArrayList<>(Collections.singletonList(TestPlayer.TARGET_SKIP));
        check(adapter("searchPickChoice", types, skip, many, true, true, true, byName) == null,
                "a front skip that can close the search was overridden");
        check(forest.equals(adapter("searchPickChoice", types, skip, many, true, false, true, byName))
                        && skip.equals(Collections.singletonList(TestPlayer.TARGET_SKIP)),
                "a fixed search did not take its pick and leave the later ask's skip queued");
        check(adapter("searchPickChoice", types, new ArrayList<String>(), many, false, false, true, byName) == null,
                "a multi-pick search (count is its own decision) was guessed");
        check(adapter("searchPickChoice", types, new ArrayList<String>(),
                        new ArrayList<java.util.UUID>(), true, false, true, byName) == null,
                "an empty search produced a pick");
        // The helper alone is not the fix: a merge that drops the call from
        // the TargetCardInLibrary funnel, or drops the trailing gate from the
        // call, leaves every check above passing while the strict replay
        // either fails again or compares an invented in-window board. Pin
        // both the call site and the trailing predicate it passes.
        String src = System.getProperty("scenarioReplay.source");
        check(src != null, "scenarioReplay.source must name ScenarioReplay.java (run the driver-asks script)");
        String text = Files.readString(Path.of(src), StandardCharsets.UTF_8);
        int call = text.indexOf("= searchPickChoice(");
        check(call >= 0, "ScriptedChoicePlayer.chooseTarget(Cards, TargetCard) must call searchPickChoice"
                + " for a TargetCardInLibrary ask");
        check(text.indexOf("owner.pastLastCheckpoint()", call) > call,
                "the search funnel must gate the pick on owner.pastLastCheckpoint(): an in-window"
                        + " search is a real decision, not one to invent");
        check(text.indexOf("boolean pastLastCheckpoint()") >= 0,
                "ScenarioReplay.pastLastCheckpoint must exist to describe the trailing play");
        // A front skip that closes a short search is the ask's own answer even
        // when only one card is legal: the funnel must close the search before
        // the lone-target rule, or it takes the card and leaves the skip
        // queued (CR 701.23b).
        int funnel = text.indexOf(
                "public boolean chooseTarget(Outcome outcome, Cards cards, mage.target.TargetCard target");
        int closeSkip = text.indexOf("if (shortSearchStop(", funnel);
        int loneCall = text.indexOf("UUID lone = unscriptedLoneTarget(", funnel);
        check(funnel >= 0 && closeSkip > funnel && loneCall > closeSkip,
                "the TargetCardInLibrary funnel must handle shortSearchStop before the lone-target"
                        + " rule: a skip it can close is not overridden");
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

    /** (l) Ureni, the Song Unending's ETB: X damage divided among any number
     * of target creatures and/or planeswalkers with two legal candidates and
     * no scripted "^X=" answer. gorge settles that without a decision, so the
     * driver deals the points evenly over every recipient the ask allows
     * (the lone-candidate rule would stop at one) instead of stopping at
     * "Missing TARGET def". The production chooseTargetAmount is driven over a
     * synthetic TargetAmount so a merge that drops or reorders the shares
     * fails here. A queued "name^X=n" answer at the head and a front skip that
     * closes the ask stay the base's; a later ask's skip stays queued; a
     * one-recipient ask with several candidates is a real target choice and
     * is not guessed. */
    private static void dividedAmountUnscripted() throws Exception {
        Class<?>[] sharesTypes = {int.class, int.class};
        check(Arrays.equals(new int[]{4, 4}, (int[]) adapter("evenShares", sharesTypes, 8, 2)),
                "8 points over two candidates is not the even 4+4 split");
        check(Arrays.equals(new int[]{4, 3}, (int[]) adapter("evenShares", sharesTypes, 7, 2)),
                "the remainder must go to the earlier candidate");
        check(Arrays.equals(new int[]{1, 1, 1}, (int[]) adapter("evenShares", sharesTypes, 3, 3)),
                "3 points over three candidates is not one each");
        check(Arrays.equals(new int[]{1, 1}, (int[]) adapter("evenShares", sharesTypes, 2, 5)),
                "a 2-point ask cannot give five recipients one each");
        check(((int[]) adapter("evenShares", sharesTypes, 0, 2)).length == 0,
                "a zero amount must not ask for a recipient");
        UUID a = UUID.randomUUID();
        UUID b = UUID.randomUUID();
        java.util.function.BiPredicate<UUID, String> amountName = (id, answer) -> {
            int at = answer == null ? -1 : answer.indexOf("^X=");
            return (id.equals(a) ? "Ajani Resolute" : "Ornithopter").equals(at >= 0 ? answer.substring(0, at) : answer);
        };
        Class<?>[] choiceTypes = {List.class, List.class, boolean.class, java.util.function.BiPredicate.class};
        List<UUID> two = new ArrayList<>(Arrays.asList(a, b));
        check((Boolean) adapter("unscriptedChoice", choiceTypes, new ArrayList<String>(), two, true, amountName),
                "two candidates with no scripted amount must be answered");
        check(!(Boolean) adapter("unscriptedChoice", choiceTypes,
                        new ArrayList<>(Collections.singletonList("Ajani Resolute^X=2")), two, true, amountName),
                "a scripted ^X= answer must be left for the base");
        List<String> skip = new ArrayList<>(Collections.singletonList(TestPlayer.TARGET_SKIP));
        check(!(Boolean) adapter("unscriptedChoice", choiceTypes, skip, two, true, amountName),
                "a front skip that can close the ask must be left for the base");
        check((Boolean) adapter("unscriptedChoice", choiceTypes, skip, two, false, amountName),
                "a later ask's skip must not stop the inferred split");

        // Production path. Two legal permanents (passed in reverse creation
        // order), eight points, two slots.
        Class<?> scripted = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        ScenarioReplay owner = (ScenarioReplay) unsafe().allocateInstance(ScenarioReplay.class);
        Constructor<?> ctor = scripted.getDeclaredConstructor(
                org.mage.test.player.TestComputerPlayer.class, ScenarioReplay.class);
        ctor.setAccessible(true);
        TestPlayer player = (TestPlayer) ctor.newInstance(
                new org.mage.test.player.TestComputerPlayer("PlayerA", mage.constants.RangeOfInfluence.ALL), owner);
        seat(owner, "playerA", player);
        UUID own = player.getId();
        Permanent earlier = amountPermanent(own, 1, "Ajani Resolute");
        Permanent later = amountPermanent(own, 2, "Ornithopter");
        Game game = amountGame(earlier, later);
        List<UUID> reversed = Arrays.asList(later.getId(), earlier.getId());

        // (1) Nothing scripted: 4+4, earlier-created first, queue untouched.
        AmountTarget amount = new AmountTarget(8, 0, 2, reversed);
        check(player.chooseTargetAmount(mage.constants.Outcome.Benefit, amount, null, game),
                "chooseTargetAmount declined an unscripted multi-candidate divided ask");
        check(amount.getTargets().size() == 2,
                "the divided ask chose " + amount.getTargets().size() + " recipients, not 2");
        check(amount.getTargets().get(0).equals(earlier.getId()),
                "the earlier-created permanent must take the first share (stable order)");
        check(amount.getTargetAmount(earlier.getId()) == 4 && amount.getTargetAmount(later.getId()) == 4,
                "8 points must be dealt 4+4, got " + amount.getTargetAmount(earlier.getId())
                        + "+" + amount.getTargetAmount(later.getId()));
        check(player.getTargets().isEmpty(), "the inferred split left an answer queued");

        // (2) A scripted "name^X=n" answer at the head is the base's: it
        // takes the whole amount for the named candidate and consumes the entry.
        player.getTargets().clear();
        player.getTargets().add("Ornithopter^X=8");
        amount = new AmountTarget(8, 0, 2, reversed);
        check(player.chooseTargetAmount(mage.constants.Outcome.Benefit, amount, null, game),
                "a scripted amount answer did not finish the ask");
        check(amount.getTargets().size() == 1 && amount.getTargetAmount(later.getId()) == 8,
                "the scripted 'Ornithopter^X=8' was overridden by an inferred split");
        check(player.getTargets().isEmpty(), "the scripted amount answer was not consumed");

        // (3) A front skip on an "up to" ask is the base's: it closes the ask
        // with nothing chosen and is consumed.
        player.getTargets().clear();
        player.getTargets().add(TestPlayer.TARGET_SKIP);
        amount = new AmountTarget(8, 0, 2, reversed);
        check(!player.chooseTargetAmount(mage.constants.Outcome.Benefit, amount, null, game),
                "a front skip on an up-to ask did not close it");
        check(amount.getTargets().isEmpty() && player.getTargets().isEmpty(),
                "the closing skip was overridden or left queued");

        // (4) On a mandatory ask (min 1) a front skip belongs to a LATER ask:
        // the split is inferred and the skip stays queued.
        player.getTargets().clear();
        player.getTargets().add(TestPlayer.TARGET_SKIP);
        amount = new AmountTarget(8, 1, 2, reversed);
        check(player.chooseTargetAmount(mage.constants.Outcome.Benefit, amount, null, game),
                "a mandatory divided ask declined");
        check(amount.getTargets().size() == 2 && player.getTargets().equals(Collections.singletonList(TestPlayer.TARGET_SKIP)),
                "a later ask's skip was consumed or the split not inferred");

        // (5) A ONE-recipient ask (max 1) with several candidates is a target
        // choice, not a division: it must not silently take the first.
        player.getTargets().clear();
        amount = new AmountTarget(8, 1, 1, reversed);
        boolean guessed;
        try {
            guessed = player.chooseTargetAmount(mage.constants.Outcome.Benefit, amount, null, game)
                    || !amount.getTargets().isEmpty();
        } catch (AssertionError strict) {
            guessed = false;
        }
        check(!guessed && amount.getTargets().isEmpty(),
                "a max-1 ask with two candidates and no answer silently took a target");
    }

    /** A divided-amount target with the Ureni shape: an amount, a
     * minimum and maximum, and a fixed legal set. */
    private static final class AmountTarget extends mage.target.TargetAmount {
        private final Set<UUID> possible;

        AmountTarget(int amount, int minTargets, int maxTargets, List<UUID> candidates) {
            super(mage.abilities.dynamicvalue.common.StaticValue.get(amount), minTargets, maxTargets);
            possible = new LinkedHashSet<>(candidates);
            this.targetName = "creature";
        }

        @Override
        public AmountTarget copy() {
            return this;
        }

        @Override
        public mage.filter.Filter getFilter() {
            return mage.filter.StaticFilters.FILTER_PERMANENT;
        }

        @Override
        public String getTargetedName(Game game) {
            return "";
        }

        @Override
        public boolean canChoose(UUID playerId, Ability source, Game game) {
            return true;
        }

        @Override
        public Set<UUID> possibleTargets(UUID controllerId, Ability source, Game game) {
            return possible;
        }

        @Override
        public boolean canTarget(UUID id, Ability source, Game game) {
            return true;
        }

        @Override
        public boolean canTarget(UUID playerId, UUID id, Ability source, Game game) {
            return true;
        }
    }

    private static Permanent amountPermanent(UUID controller, int order, String name) {
        UUID id = UUID.randomUUID();
        return (Permanent) Proxy.newProxyInstance(Permanent.class.getClassLoader(),
                new Class<?>[]{Permanent.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getId": return id;
                        case "getControllerId": return controller;
                        case "getName": return name;
                        case "getCreateOrder": return order;
                        case "isToken": return false;
                        case "isPhasedIn": return true;
                        case "isCopy": return false;
                        case "isFaceDown": return false;
                        case "isControlledBy": return controller.equals(args[0]);
                        case "getCardType": return List.of(mage.constants.CardType.CREATURE);
                        case "isCreature": return true;
                        case "isTapped": return false;
                        case "getExpansionSetCode": return "TST";
                        default: throw new AssertionError("unexpected permanent call " + method);
                    }
                });
    }

    private static Game amountGame(Permanent first, Permanent second) {
        return (Game) Proxy.newProxyInstance(Game.class.getClassLoader(),
                new Class<?>[]{Game.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getPermanent":
                            return first.getId().equals(args[0]) ? first
                                    : second.getId().equals(args[0]) ? second : null;
                        case "getObject":
                            return first.getId().equals(args[0]) ? first
                                    : second.getId().equals(args[0]) ? second : null;
                        case "getPlayer": return null;
                        case "getCard": return null;
                        case "getPlayers": return new LinkedHashMap<UUID, mage.players.Player>();
                        case "isSimulation": return true;
                        case "getRangeOfInfluence": return mage.constants.RangeOfInfluence.ALL;
                        default: throw new AssertionError("unexpected game call " + method);
                    }
                });
    }

    /** Fathom Trawl, PutCards.BOTTOM_ANY (and every any-order path): XMage
     * asks the order one card at a time through a "card ORDER to put ..."
     * TargetCard. When every card in the window has the same name the order
     * changes no state the replay compares, and gorge records no decision for
     * it, so the driver takes the first offered card per ask (the window is
     * CardsImpl, the looked-at library order). ZonesHandler.chooseOrder's
     * simultaneous-zone-change asks and a card's own order loop (Proteus
     * Staff) are the same role and are covered too. A distinct-name order is
     * gorge's own CR 401.4 choice and stays the base's strict miss; a pick-one
     * (Booster Tutor, Psychotic Episode, Agonizing Memories' hand pick) is not
     * an order and must not be guessed. */
    private static void cardOrderPick() throws Exception {
        Class<?>[] ask = {mage.constants.Zone.class, int.class, int.class, String.class};
        check((Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.ALL, 1, 1,
                        "card ORDER to put on the BOTTOM of your library (last one chosen will be bottommost)"),
                "PutCards.BOTTOM_ANY's TargetCard is not recognised as an order ask");
        check((Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.ALL, 1, 1,
                        "card ORDER to put on the TOP of your library (last one chosen will be topmost)"),
                "putCardsOnTopOfLibrary's order ask is not recognised");
        check((Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.ALL, 1, 1,
                        "card ORDER to put seventh from the TOP of your library (last one chosen will be topmost)"),
                "putCardOnTopXOfLibrary's order ask is not recognised");
        check((Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.ALL, 1, 1,
                        "order to put on bottom of library (last chosen will be bottommost)"),
                "ZonesHandler.chooseOrder's library order ask is not recognised");
        check((Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.ALL, 1, 1,
                        "order to put in graveyard (last chosen will be on top)"),
                "ZonesHandler.chooseOrder's graveyard order ask is not recognised");
        check((Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.GRAVEYARD, 1, 1,
                        "card to put on the top of your graveyard (last one chosen will be topmost)"),
                "PlayerImpl's simultaneous-graveyard order ask is not recognised");
        check((Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.LIBRARY, 1, 1,
                        "card to put on bottom of your library (last chosen will be on bottom)"),
                "Proteus Staff's own library order loop is not recognised");
        check(!(Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.ALL, 1, 1, "card"),
                "a plain pick-one from a Zone.ALL window was misread as an order ask");
        check(!(Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.ALL, 1, 1, "creature card"),
                "Booster Tutor's Zone.ALL pick-one was misread as an order ask");
        check(!(Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.HAND, 1, 1,
                        "card to put on the top of library (last chosen will be on top)"),
                "Agonizing Memories' two hand picks are a sequence of choices, not an order");
        check(!(Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.LIBRARY, 1, 1,
                        "card to put back on top of your library"),
                "a plain look-at pick is not an order ask");
        check(!(Boolean) adapter("isCardOrderAsk", ask, mage.constants.Zone.ALL, 0, 1,
                        "card ORDER to put on the BOTTOM of your library (last one chosen will be bottommost)"),
                "an up-to order ask is not the fixed one-at-a-time dialog");
        java.util.UUID first = UUID.randomUUID();
        java.util.UUID second = UUID.randomUUID();
        java.util.UUID third = UUID.randomUUID();
        java.util.function.Function<UUID, String> same = id -> "Island";
        java.util.function.Function<UUID, String> mixed =
                id -> id.equals(third) ? "Forest" : "Island";
        Class<?>[] types = {boolean.class, List.class, java.util.function.Function.class};
        List<UUID> offered = new ArrayList<>(Arrays.asList(first, second, third));
        check(first.equals(adapter("interchangeableCardOrderPick", types, true, offered, same)),
                "a same-named card order did not take the first offered card");
        check(adapter("interchangeableCardOrderPick", types, true, offered, mixed) == null,
                "a distinct-name card order was guessed (gorge's CR 401.4 choice)");
        check(adapter("interchangeableCardOrderPick", types, false, offered, same) == null,
                "a non-order ask was answered by the order rule");
        check(adapter("interchangeableCardOrderPick", types, true,
                        new ArrayList<>(Collections.singletonList(first)), same) == null,
                "a single remaining card is the caller's last move, not an order ask");
        // The helper alone is not the fix: a merge that drops a funnel's call
        // leaves every check above passing while the order ask strict-misses
        // again. Pin both Cards funnels to the one shared decision, and that
        // decision to the role gate and the same-name rule.
        String src = System.getProperty("scenarioReplay.source");
        check(src != null, "scenarioReplay.source must name ScenarioReplay.java (run the driver-asks script)");
        String text = Files.readString(Path.of(src), StandardCharsets.UTF_8);
        int forced = text.indexOf("private boolean forcedChooseCards(");
        int helper = text.indexOf("UUID unscriptedCardOrderPick(", forced);
        int forcedCall = text.indexOf("unscriptedCardOrderPick(", forced);
        check(forced >= 0 && forcedCall > forced && forcedCall < helper,
                "forcedChooseCards (choose(Cards, TargetCard)) must call unscriptedCardOrderPick");
        int funnel = text.indexOf(
                "public boolean chooseTarget(Outcome outcome, Cards cards, mage.target.TargetCard target");
        int funnelCall = text.indexOf("unscriptedCardOrderPick(", funnel);
        int funnelFallback = text.indexOf("return super.chooseTarget(", funnel);
        check(funnel >= 0 && funnelCall > funnel && funnelCall < funnelFallback,
                "chooseTarget(Cards, TargetCard) must call the same unscriptedCardOrderPick");
        int gate = text.indexOf("isCardOrderAsk(", helper);
        int pick = text.indexOf("interchangeableCardOrderPick(", helper);
        check(gate > helper && pick > gate,
                "unscriptedCardOrderPick must gate on isCardOrderAsk before the same-name rule");
    }

    private static void seat(ScenarioReplay owner, String field, TestPlayer player) throws Exception {
        for (Class<?> c = ScenarioReplay.class; c != null; c = c.getSuperclass()) {
            try {
                java.lang.reflect.Field f = c.getDeclaredField(field);
                f.setAccessible(true);
                f.set(owner, player);
                return;
            } catch (NoSuchFieldException absent) { }
        }
        throw new AssertionError("no " + field + " field");
    }

    private static sun.misc.Unsafe unsafe() throws Exception {
        java.lang.reflect.Field f = sun.misc.Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        return (sun.misc.Unsafe) f.get(null);
    }
}
