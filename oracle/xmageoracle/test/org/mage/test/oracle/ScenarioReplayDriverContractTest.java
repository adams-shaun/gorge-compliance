package org.mage.test.oracle;

import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.UUID;
import java.util.function.BiPredicate;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import mage.abilities.keyword.SpreeAbility;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.cards.a.AresGodOfWar;
import mage.cards.g.GrizzlyBears;
import mage.cards.l.LightningBolt;
import mage.cards.o.OneLastJob;
import mage.cards.r.RedHerring;
import mage.constants.Rarity;
import mage.constants.Zone;
import org.mage.test.player.TestPlayer;

/** No database or AI: executable contract checks on the driver helpers that
 * answer an XMage attach ask, map a move step's zone and detect Spree. Actual
 * chooser/strict replay still belongs to the host replay gate. */
public final class ScenarioReplayDriverContractTest {
    private static final String SKIP = TestPlayer.TARGET_SKIP;

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static UUID id(int n) {
        return new UUID(0, n);
    }

    /** The driver's attach-ask decision, reached the same way
     * OptionalTargetSkipsTest reaches hideAfterNextSkip: reflection into the
     * private ScriptedChoicePlayer so the production method is what runs. */
    @SuppressWarnings("unchecked")
    private static UUID attachmentChoice(List<String> queue, List<UUID> candidates,
            BiPredicate<UUID, String> matches) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("attachmentChoice", List.class, List.class, BiPredicate.class);
        method.setAccessible(true);
        return (UUID) method.invoke(null, queue, candidates, matches);
    }

    private static boolean isAttachmentChoice(mage.target.Target target) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("isAttachmentChoice", mage.target.Target.class);
        method.setAccessible(true);
        return (Boolean) method.invoke(null, target);
    }

    /** The attach STEP's idempotence: a bearer that already lists the
     * attachment (its ETB attach trigger attached the same pair during the
     * resolve step) makes the step a no-op instead of the refusal
     * Card.addAttachment would throw. */
    private static boolean attachAlreadySatisfied(List<UUID> bearerAttachments, UUID attachmentId)
            throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay");
        Method method = adapter.getDeclaredMethod("attachAlreadySatisfied", List.class, UUID.class);
        method.setAccessible(true);
        return (Boolean) method.invoke(null, bearerAttachments, attachmentId);
    }

    private static void attachments() throws Exception {
        mage.target.TargetPermanent ordinary = new mage.target.TargetPermanent();
        mage.target.TargetPermanent unrelatedChoice = new mage.target.TargetPermanent();
        unrelatedChoice.withNotTarget(true);
        mage.target.TargetPermanent attachAsk = new mage.target.TargetPermanent(
                new mage.filter.FilterPermanent("a creature you control that Abduction can be attached to"));
        attachAsk.withNotTarget(true);
        check(!isAttachmentChoice(ordinary),
                "ordinary targeted permanent ask was classified as attachment choice");
        check(!isAttachmentChoice(unrelatedChoice),
                "unrelated non-targeting permanent ask was classified as attachment choice");
        check(isAttachmentChoice(attachAsk),
                "unhinted non-targeting permanent attach ask was not recognized");

        UUID bears = id(1), elves = id(2);
        BiPredicate<UUID, String> byId = (candidate, answer) -> answer.equals(candidate.toString());
        List<UUID> both = Arrays.asList(bears, elves);

        // A scripted answer that names the non-first candidate is honored and
        // consumed, not overridden by an alphabetical or first-candidate pick.
        List<String> queue = new ArrayList<>(Arrays.asList(elves.toString()));
        check(elves.equals(attachmentChoice(queue, both, byId)),
                "scripted second candidate was not honored");
        check(queue.isEmpty(), "scripted attachment answer was not consumed");
        // Precondition: the two candidates really are distinct answers.
        check(!bears.equals(elves), "candidates must differ");

        // Successive asks: the first consumes the scripted answer, the second
        // (now unscripted and uniquely determined) auto-selects without a
        // stale answer resurfacing.
        List<String> successive = new ArrayList<>(Arrays.asList(elves.toString()));
        check(elves.equals(attachmentChoice(successive, both, byId)), "first of successive asks failed");
        check(bears.equals(attachmentChoice(successive, Arrays.asList(bears), byId)),
                "second successive ask did not auto-select its unique candidate");

        // Unscripted, uniquely determined: automatic selection.
        check(bears.equals(attachmentChoice(new ArrayList<>(), Arrays.asList(bears), byId)),
                "unscripted unique candidate was not auto-selected");

        // Unscripted, ambiguous: the driver must not guess; the base player
        // answers (and its strict unused-command check still applies).
        check(attachmentChoice(new ArrayList<>(), both, byId) == null,
                "ambiguous unscripted attach ask was auto-selected");

        // A scripted answer that names no candidate is left queued so the
        // strict replay reports the scenario error rather than hiding it.
        List<String> unmatched = new ArrayList<>(Arrays.asList("@not-a-candidate"));
        check(attachmentChoice(unmatched, both, byId) == null, "unmatched answer accepted");
        check(unmatched.equals(Arrays.asList("@not-a-candidate")),
                "unmatched scripted answer was consumed or altered");

        // A leading skip is the base's optional-skip semantics; fall through.
        List<String> skipped = new ArrayList<>(Arrays.asList(SKIP, bears.toString()));
        check(attachmentChoice(skipped, Arrays.asList(bears), byId) == null,
                "a leading skip must fall through, not auto-select");
        check(skipped.equals(Arrays.asList(SKIP, bears.toString())),
                "a leading skip was consumed by the attach branch");
        // The attach STEP is idempotent: a bearer that already lists the
        // attachment is a satisfied no-op (the ETB attach trigger attached
        // this pair during the resolve step), not a refusal.
        check(attachAlreadySatisfied(Arrays.asList(bears), bears),
                "an already-listed attachment was not treated as satisfied");
        check(!attachAlreadySatisfied(Arrays.asList(elves), bears),
                "an unrelated attachment was treated as satisfied");
        check(!attachAlreadySatisfied(null, bears), "a null attachment list was satisfied");
        System.out.println("PASS attach-ask choice (scripted first/non-first, consume, successive, "
                + "unique auto-select, ambiguous, unmatched, skip, idempotent step)");
    }

    private static void zones() {
        check(ScenarioReplay.moveDestination("exile") == Zone.EXILED,
                "gorge's lowercase exile did not map to XMage's EXILED");
        check(ScenarioReplay.moveDestination("graveyard") == Zone.GRAVEYARD, "graveyard mapping");
        check(ScenarioReplay.moveDestination("hand") == Zone.HAND, "hand mapping");
        check(ScenarioReplay.moveDestination("battlefield") == Zone.BATTLEFIELD, "battlefield mapping");
        // Precondition: EXILED is not EXILE (the enum member whose absence the
        // mapping exists for); a valueOf("exile") would in fact throw.
        boolean threw = false;
        try {
            Zone.valueOf("exile");
        } catch (IllegalArgumentException expected) {
            threw = true;
        }
        check(threw, "precondition: Zone.valueOf(\"exile\") should not resolve");

        // The `move` op must find its source card in any Card-bearing zone, not
        // only the hand: the next generator shape that moves a card already in
        // a graveyard/library/exile must not throw. Hand stays first so today's
        // generated hand -> zone move is unchanged, and a battlefield permanent
        // (a Permanent, not a Card) is deliberately absent.
        List<Zone> sources = ScenarioReplay.moveSourceZones();
        check(sources.get(0) == Zone.HAND, "move must search the hand first; got " + sources);
        for (Zone expected : Arrays.asList(Zone.HAND, Zone.GRAVEYARD, Zone.LIBRARY, Zone.EXILED)) {
            check(sources.contains(expected), "move source zones omit " + expected + ": " + sources);
        }
        check(!sources.contains(Zone.BATTLEFIELD),
                "a battlefield permanent is not a Card move source; got " + sources);
        System.out.println("PASS move destination mapping (exile->EXILED) and multi-zone source search");
    }

    private static JsonArray steps(String json) {
        return JsonParser.parseString(json).getAsJsonArray();
    }

    private static boolean rejects(JsonArray steps, int index) {
        try {
            ScenarioReplay.passAction(steps, index);
            return false;
        } catch (IllegalArgumentException expected) {
            return true;
        }
    }

    private static void setField(Object target, String name, Object value) throws Exception {
        for (Class<?> c = target.getClass(); c != null; c = c.getSuperclass()) {
            try {
                java.lang.reflect.Field f = c.getDeclaredField(name);
                f.setAccessible(true);
                f.set(target, value);
                return;
            } catch (NoSuchFieldException next) {
                // keep climbing
            }
        }
        throw new AssertionError("no field " + name);
    }

    /** A driver with two bare players: no game, so only queueing runs. */
    private static ScenarioReplay queueingDriver(JsonArray steps, TestPlayer a, TestPlayer b) throws Exception {
        ScenarioReplay r = new ScenarioReplay();
        JsonObject sc = new JsonObject();
        sc.add("steps", steps);
        setField(r, "playerA", a);
        setField(r, "playerB", b);
        setField(r, "sc0", sc);
        return r;
    }

    private static TestPlayer bare(String name) {
        return new TestPlayer(new org.mage.test.player.TestComputerPlayer(name, mage.constants.RangeOfInfluence.ALL));
    }

    private static List<String> names(TestPlayer p) {
        List<String> out = new ArrayList<>();
        for (org.mage.test.player.PlayerAction a : p.getActions()) {
            out.add(a.getAction().startsWith("waitStackResolved") ? a.getAction() : a.getActionName());
        }
        return out;
    }

    /** What the replay loop does for one step: its pass commands, the step
     * itself, then its checkpoint (the loop's order is pinned by
     * driver_levelb_pass_test.go). */
    private static void loopStep(ScenarioReplay r, TestPlayer a, JsonArray steps, int i) throws Exception {
        String op = steps.get(i).getAsJsonObject().get("op").getAsString();
        queue(r, "queuePassCommands", i);
        if (op.equals("pass")) {
            queue(r, "step", steps.get(i).getAsJsonObject(), op, i);
        }
        r.runCode("step " + i + " (" + op + ")", 1, mage.constants.PhaseStep.PRECOMBAT_MAIN, a, (info, p, g) -> { });
    }

    private static void queue(ScenarioReplay r, String method, Object... args) throws Exception {
        for (Method m : ScenarioReplay.class.getDeclaredMethods()) {
            if (m.getName().equals(method) && m.getParameterCount() == args.length) {
                m.setAccessible(true);
                m.invoke(r, args);
                return;
            }
        }
        throw new AssertionError("no method " + method);
    }

    private static Object getField(Object target, String name) throws Exception {
        for (Class<?> c = target.getClass(); c != null; c = c.getSuperclass()) {
            try {
                java.lang.reflect.Field f = c.getDeclaredField(name);
                f.setAccessible(true);
                return f.get(target);
            } catch (NoSuchFieldException next) {
                // keep climbing
            }
        }
        throw new AssertionError("no field " + name);
    }

    /** A pass_to with no step is a checkpoint where the scenario already is:
     * it must keep the tracked phase, not reset it to MAIN. */
    private static void statelessPassToKeepsPhase() throws Exception {
        JsonArray s = steps("[{op:'pass_to',seat:0,decision:'priority',step:'end'},"
                + "{op:'pass_to',seat:0,decision:'priority'},"
                + "{op:'pass_to',seat:0,decision:'priority',step:'main1'},"
                + "{op:'pass_to',seat:0,decision:'priority'}]");
        ScenarioReplay r = queueingDriver(s, bare("A"), bare("B"));
        queue(r, "step", s.get(0).getAsJsonObject(), "pass_to", 0);
        check(getField(r, "phase") == mage.constants.PhaseStep.END_TURN, "step:'end' must set END_TURN");
        check(mage.constants.PhaseStep.END_TURN != mage.constants.PhaseStep.PRECOMBAT_MAIN, "precondition");
        queue(r, "step", s.get(1).getAsJsonObject(), "pass_to", 1);
        check(getField(r, "phase") == mage.constants.PhaseStep.END_TURN,
                "a stepless pass_to must keep END_TURN; got " + getField(r, "phase"));
        queue(r, "step", s.get(2).getAsJsonObject(), "pass_to", 2);
        check(getField(r, "phase") == mage.constants.PhaseStep.PRECOMBAT_MAIN, "step:'main1' must set MAIN");
        queue(r, "step", s.get(3).getAsJsonObject(), "pass_to", 3);
        check(getField(r, "phase") == mage.constants.PhaseStep.PRECOMBAT_MAIN, "a stepless pass_to from MAIN stays MAIN");
        System.out.println("PASS stepless pass_to keeps the tracked phase");
    }

    private static void passes() throws Exception {
        JsonArray pair = steps("[{op:'pass',seat:0},{op:'pass',seat:1}]");
        check(ScenarioReplay.passAction(pair, 0) == ScenarioReplay.PASS_PAIR_FIRST, "first pass of a pair");
        check(ScenarioReplay.passAction(pair, 1) == ScenarioReplay.PASS_PAIR_SECOND, "second pass of a pair");
        JsonArray handoff = steps("[{op:'pass',seat:0},{op:'cast',seat:1,card:'p1:Lightning Bolt'}]");
        check(ScenarioReplay.passAction(handoff, 0) == ScenarioReplay.PASS_HANDOFF, "lone p0 pass before a p1 cast");

        // p0 hands priority off, p1 casts at instant speed and passes (CR
        // 117.3c), p0 passes to resolve (CR 117.4). The pair is p1-first: its
        // first pass queues nothing (the checkpoint still sees the spell) and
        // its second queues the one-object resolution.
        JsonArray p1Pair = steps("[{op:'pass',seat:0},{op:'cast',seat:1,card:'p1:King Cheetah'},"
                + "{op:'pass',seat:1},{op:'pass',seat:0},{op:'resolve',seat:0}]");
        check(ScenarioReplay.passAction(p1Pair, 0) == ScenarioReplay.PASS_HANDOFF,
                "step 0 is the lone p0 handoff pass");
        check(ScenarioReplay.passAction(p1Pair, 2) == ScenarioReplay.PASS_P1_PAIR_FIRST,
                "step 2 is the first pass of a p1-first pair");
        check(ScenarioReplay.passAction(p1Pair, 3) == ScenarioReplay.PASS_PAIR_SECOND,
                "step 3 is the second pass of a p1-first pair");
        TestPlayer ap = bare("A");
        TestPlayer bp = bare("B");
        ScenarioReplay rp = queueingDriver(p1Pair, ap, bp);
        loopStep(rp, ap, p1Pair, 0);
        check(names(ap).equals(Arrays.asList("step 0 (pass)")),
                "the handoff pass queues only its checkpoint; got " + names(ap));
        queue(rp, "queuePassCommands", 1);
        check(names(ap).equals(Arrays.asList("step 0 (pass)", ScenarioReplay.CMD_YIELD_PRIORITY)),
                "the p1 cast step yields priority before its checkpoint; got " + names(ap));
        loopStep(rp, ap, p1Pair, 2);
        check(names(ap).equals(Arrays.asList("step 0 (pass)", ScenarioReplay.CMD_YIELD_PRIORITY, "step 2 (pass)")),
                "the p1-first pair's first pass queues only its checkpoint; got " + names(ap));
        loopStep(rp, ap, p1Pair, 3);
        check(names(ap).equals(Arrays.asList("step 0 (pass)", ScenarioReplay.CMD_YIELD_PRIORITY, "step 2 (pass)",
                        ScenarioReplay.CMD_REQUIRE_STACK, ScenarioReplay.CMD_WAIT_RESOLVE_ONE, "step 3 (pass)")),
                "the p1-first pair's second pass queues the one-object resolution before its checkpoint; got " + names(ap));
        check(names(bp).isEmpty(), "the p1-first pair queues nothing for the opponent; got " + names(bp));

        // The pair can be followed by a pass_to (Entropic Battlecruiser's
        // static probe answers at the next priority): classification and queue
        // order must not depend on what follows.
        JsonArray p1PairTail = steps("[{op:'pass',seat:0},{op:'cast',seat:1,card:'p1:Occult Epiphany'},"
                + "{op:'pass',seat:1},{op:'pass',seat:0},{op:'pass_to',decision:'priority'},{op:'resolve',seat:0}]");
        check(ScenarioReplay.passAction(p1PairTail, 2) == ScenarioReplay.PASS_P1_PAIR_FIRST,
                "the first pass of a pass_to-suffixed p1-first pair");
        check(ScenarioReplay.passAction(p1PairTail, 3) == ScenarioReplay.PASS_PAIR_SECOND,
                "the second pass of a pass_to-suffixed p1-first pair");
        TestPlayer at = bare("A");
        ScenarioReplay rt = queueingDriver(p1PairTail, at, bare("B"));
        loopStep(rt, at, p1PairTail, 2);
        check(names(at).equals(Arrays.asList("step 2 (pass)")),
                "a pass_to-suffixed p1-first pair's first pass queues only its checkpoint; got " + names(at));
        loopStep(rt, at, p1PairTail, 3);
        check(names(at).equals(Arrays.asList("step 2 (pass)", ScenarioReplay.CMD_REQUIRE_STACK,
                        ScenarioReplay.CMD_WAIT_RESOLVE_ONE, "step 3 (pass)")),
                "a pass_to-suffixed p1-first pair's second pass queues the one-object resolution before its checkpoint; got " + names(at));

        // Every other shape fails loudly rather than being guessed at.
        check(rejects(steps("[{op:'pass',seat:0}]"), 0), "a lone trailing pass must be rejected");
        check(rejects(steps("[{op:'pass',seat:0},{op:'resolve'}]"), 0), "a pass before a non-cast must be rejected");
        check(rejects(steps("[{op:'pass',seat:1},{op:'pass',seat:0}]"), 0),
                "a bare p1-first pair with no preceding seat-1 cast must be rejected");
        check(rejects(steps("[{op:'pass',seat:0},{op:'pass',seat:0}]"), 0), "a same-seat pair must be rejected");
        check(rejects(steps("[{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:0}]"), 1),
                "a three-pass run must be rejected");

        // At active seat 1 the p1-first pair after a seat-1 cast is the
        // active player's OWN pair (Zidane, Wan Shi Tong): its first pass
        // queues nothing and its second resolves one object on seat 1's
        // queue (ScenarioReplayWave4DriverTest pins the commands).
        check(ScenarioReplay.passCommands(p1Pair, 2, 1).isEmpty(),
                "the active seat 1's own p1-first pair queues nothing at its first pass");

        // Queue order on the active player (the checkpoint owner). Cast and
        // pass pair: the spell is on the stack at the first-pass checkpoint,
        // and the one-object resolution sits after it, before the second.
        JsonArray cast = steps("[{op:'cast',seat:0},{op:'pass',seat:0},{op:'pass',seat:1}]");
        TestPlayer a = bare("A");
        TestPlayer b = bare("B");
        ScenarioReplay r = queueingDriver(cast, a, b);
        queue(r, "queuePassCommands", 0);
        check(names(a).isEmpty(), "a cast step queues no pass command; got " + names(a));
        loopStep(r, a, cast, 1);
        check(names(a).equals(Arrays.asList("step 1 (pass)")),
                "first pass must queue only its checkpoint (no early resolution); got " + names(a));
        loopStep(r, a, cast, 2);
        check(names(a).equals(Arrays.asList("step 1 (pass)", ScenarioReplay.CMD_REQUIRE_STACK,
                        ScenarioReplay.CMD_WAIT_RESOLVE_ONE, "step 2 (pass)")),
                "second pass must queue the one-object resolution before its checkpoint; got " + names(a));
        check(names(b).isEmpty(), "the pair queues nothing for the opponent; got " + names(b));
        check(a.getActions().get(2).getAction().equals("waitStackResolved:1"),
                "the resolution must wait for exactly one stack object");

        // The draw and event recipes append the pair before a pass_to (the
        // scry/surveil answer), so the pair is not the scenario's tail. Its
        // classification and queue order must not depend on what follows it.
        JsonArray pairTail = steps("[{op:'cast',seat:0},{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass_to',decision:'priority'}]");
        check(ScenarioReplay.passAction(pairTail, 1) == ScenarioReplay.PASS_PAIR_FIRST,
                "the first pass of a pass_to-suffixed pair");
        check(ScenarioReplay.passAction(pairTail, 2) == ScenarioReplay.PASS_PAIR_SECOND,
                "the second pass of a pass_to-suffixed pair");
        TestPlayer a3 = bare("A");
        ScenarioReplay r3 = queueingDriver(pairTail, a3, bare("B"));
        loopStep(r3, a3, pairTail, 1);
        check(names(a3).equals(Arrays.asList("step 1 (pass)")),
                "a pass_to-suffixed pair's first pass must queue only its checkpoint; got " + names(a3));
        loopStep(r3, a3, pairTail, 2);
        check(names(a3).equals(Arrays.asList("step 1 (pass)", ScenarioReplay.CMD_REQUIRE_STACK,
                        ScenarioReplay.CMD_WAIT_RESOLVE_ONE, "step 2 (pass)")),
                "a pass_to-suffixed pair's second pass must queue the one-object resolution before its checkpoint; got " + names(a3));

        // Lone p0 pass then p1 cast: p0 yields priority between the pass
        // checkpoint and the opponent's cast checkpoint.
        TestPlayer a2 = bare("A");
        ScenarioReplay r2 = queueingDriver(handoff, a2, bare("B"));
        loopStep(r2, a2, handoff, 0);
        check(names(a2).equals(Arrays.asList("step 0 (pass)")), "the handoff pass queues only its checkpoint; got " + names(a2));
        queue(r2, "queuePassCommands", 1); // the cast step's own pre-step hook
        check(names(a2).equals(Arrays.asList("step 0 (pass)", ScenarioReplay.CMD_YIELD_PRIORITY)),
                "priority must be yielded before the opponent's cast checkpoint; got " + names(a2));

        // pass_to choose then pass_to priority: the second resolves the top
        // object (a mid-resolution ask gorge answers there); nothing else does.
        JsonArray mid = steps("[{op:'cast',seat:0},{op:'pass_to',decision:'choose',seat:0},"
                + "{op:'pass_to',decision:'priority',seat:0},{op:'pass_to',decision:'priority',seat:0}]");
        check(!ScenarioReplay.passToResolvesTop(mid, 0), "a cast step must not resolve");
        check(!ScenarioReplay.passToResolvesTop(mid, 1), "the choose pass_to must not resolve");
        check(ScenarioReplay.passToResolvesTop(mid, 2), "a priority pass_to after a choose pass_to resolves the top object");
        check(!ScenarioReplay.passToResolvesTop(mid, 3), "a priority pass_to after a priority pass_to must not resolve");

        // Passes need seat 0 to be the active player.
        JsonArray turn2 = steps("[{op:'pass',seat:0},{op:'pass',seat:1}]");
        boolean threw = false;
        try {
            ScenarioReplay.passCommands(turn2, 1, 1);
        } catch (IllegalArgumentException expected) {
            threw = true;
        }
        check(threw, "a pass on seat 1's turn must be rejected");

        // The empty-stack failure names the turn, step, priority holder and the
        // objects that left the stack since the last checkpoint, so a pair that
        // met an empty stack (an end-step trigger that resolved early, or a
        // pair run at the wrong step) is self-diagnosing without a re-run.
        String diag = ScenarioReplay.emptyStackDiagnostic(
                ScenarioReplay.CMD_REQUIRE_STACK, 1, "END_TURN", "A",
                java.util.Arrays.asList("Ultimecia, Time Sorceress"),
                java.util.Collections.<String>emptyList());
        check(diag.startsWith(ScenarioReplay.CMD_REQUIRE_STACK),
                "the empty-stack diagnostic must keep the command prefix; got " + diag);
        check(diag.contains("turn 1"), "the empty-stack diagnostic must name the turn; got " + diag);
        check(diag.contains("END_TURN"), "the empty-stack diagnostic must name the step; got " + diag);
        check(diag.contains("priority A"), "the empty-stack diagnostic must name the priority holder; got " + diag);
        check(diag.contains("[Ultimecia, Time Sorceress]"),
                "the empty-stack diagnostic must name the recent stack departures; got " + diag);
        String stillThere = ScenarioReplay.emptyStackDiagnostic(
                ScenarioReplay.CMD_REQUIRE_STACK, 1, "END_TURN", "A",
                java.util.Arrays.asList("A", "B"), java.util.Arrays.asList("B"));
        check(stillThere.contains("[A]") && !stillThere.contains("[A, B]"),
                "the empty-stack diagnostic must report only what departed; got " + stillThere);

        System.out.println("PASS pass mapping and queue order (pair resolves after its second pass; "
                + "lone p0 pass yields before the p1 cast checkpoint; p1-first pair resolves after its "
                + "second pass; other shapes rejected; empty-stack failure self-diagnosing)");
    }

    private static void mustAttack() {
        // The structural check the attack step uses must be exact on the
        // actual XMage card models, not a name list: a card that carries
        // AttacksEachCombatStaticAbility is declared and tapped by XMage's
        // own checkAttackRequirements, so its attack() command is never
        // consumed. Ares, God of War and Red Herring carry it; a plain
        // creature does not.
        Card ares = new AresGodOfWar(UUID.randomUUID(), info("Ares, God of War"));
        Card herring = new RedHerring(UUID.randomUUID(), info("Red Herring"));
        Card bears = new GrizzlyBears(UUID.randomUUID(), info("Grizzly Bears"));
        check(ScenarioReplay.isMustAttackCard(ares), "Ares, God of War was not detected as a must-attack card");
        check(ScenarioReplay.isMustAttackCard(herring), "Red Herring was not detected as a must-attack card");
        check(!ScenarioReplay.isMustAttackCard(bears), "Grizzly Bears was detected as a must-attack card");
        check(!ScenarioReplay.isMustAttackCard(null), "a null card was detected as must-attack");
        System.out.println("PASS must-attack detection (AttacksEachCombatStaticAbility on the card, not a name list)");
    }

    private static CardSetInfo info(String name) {
        return new CardSetInfo(name, "TEST", "1", Rarity.RARE);
    }

    private static void spree() {
        // The structural check the cast step uses must be exact on the actual
        // XMage card models, not a name list.
        Card spree = new OneLastJob(UUID.randomUUID(), info("One Last Job"));
        Card plain = new LightningBolt(UUID.randomUUID(), info("Lightning Bolt"));
        check(spree.getAbilities().containsClass(SpreeAbility.class),
                "precondition: OneLastJob does not carry a SpreeAbility");
        check(!plain.getAbilities().containsClass(SpreeAbility.class),
                "precondition: LightningBolt carries a SpreeAbility");
        check(ScenarioReplay.isSpreeCard(spree), "a Spree card was not detected structurally");
        check(!ScenarioReplay.isSpreeCard(plain), "a non-Spree card was detected as Spree");
        check(!ScenarioReplay.isSpreeCard(null), "a null card was detected as Spree");
        System.out.println("PASS Spree detection (SpreeAbility on the card, not a name list)");
    }

    private static String castModeSupported(String mode) throws Exception {
        Class<?> replay = ScenarioReplay.class;
        java.lang.reflect.Method m = replay.getDeclaredMethod("castModeSupported", String.class);
        m.setAccessible(true);
        return String.valueOf(m.invoke(null, mode));
    }

    @SuppressWarnings("unchecked")
    private static boolean isSacrificeChoice(mage.target.Target target) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("isSacrificeChoice", mage.target.Target.class);
        method.setAccessible(true);
        return (Boolean) method.invoke(null, target);
    }

    @SuppressWarnings("unchecked")
    private static Object optionalAdditionalCostAnswer(boolean bargained, boolean optionalCost, List<String> choices) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("optionalAdditionalCostAnswer", boolean.class, boolean.class, List.class);
        method.setAccessible(true);
        return method.invoke(null, bargained, optionalCost, choices);
    }

    @SuppressWarnings("unchecked")
    private static UUID costPickChoice(List<String> picks, List<UUID> candidates,
            BiPredicate<UUID, String> matches) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("costPickChoice", List.class, List.class, BiPredicate.class);
        method.setAccessible(true);
        return (UUID) method.invoke(null, picks, candidates, matches);
    }

    private static boolean isCraftMaterialsAsk(mage.target.Target target) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("isCraftMaterialsAsk", mage.target.Target.class);
        method.setAccessible(true);
        return (Boolean) method.invoke(null, target);
    }

    private static boolean craftMaterialsNeedFill(int chosen, int min, boolean queueNonEmpty) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("craftMaterialsNeedFill", int.class, int.class, boolean.class);
        method.setAccessible(true);
        return (Boolean) method.invoke(null, chosen, min, queueNonEmpty);
    }

    private static void bargain() throws Exception {
        // cast_mode acceptance: the driver elects Bargain and main's
        // "optionalcost" cast, an absent mode is the ordinary cast, and
        // anything else is rejected loudly (previously EVERY cast_mode was
        // rejected, before its cost was paid). The "optionalcost" check is
        // the regression guard: the reference commit's first cut dropped it.
        check(castModeSupported("").equals("true"), "an absent cast_mode must be the ordinary cast");
        check(castModeSupported("bargained").equals("true"), "bargained cast_mode was not accepted");
        check(castModeSupported("optionalcost").equals("true"),
                "optionalcost cast_mode was not accepted");
        check(castModeSupported("plot").equals("true"), "plot cast_mode was not accepted");
        check(castModeSupported("plot_cast").equals("false"), "plot_cast cast_mode must stay unsupported");
        check(castModeSupported("kicked").equals("false"), "kicked cast_mode must stay unsupported");
        check(castModeSupported("adventure_alt").equals("false"),
                "an unwired cast_mode must be rejected, not cast at face value");

        // The sacrifice ask is recognized structurally by its "to sacrifice"
        // hint (what TargetSacrifice carries), not by a card name.
        mage.target.TargetPermanent sacrifice = new mage.target.common.TargetSacrifice(
                new mage.filter.common.FilterControlledPermanent("an artifact, enchantment, or token"));
        mage.target.TargetPermanent ordinary = new mage.target.TargetPermanent();
        mage.target.TargetPermanent attach = new mage.target.TargetPermanent();
        attach.withChooseHint("to attach to a creature you control");
        check(isSacrificeChoice(sacrifice), "a TargetSacrifice ask was not recognized");
        check(!isSacrificeChoice(ordinary), "an ordinary permanent ask was classified as a sacrifice");
        check(!isSacrificeChoice(attach), "an attach ask was classified as a sacrifice");
        check(!isSacrificeChoice(null), "a null ask was classified as a sacrifice");

        // The optional-additional-cost yes/no: a bargained cast pays (the ask
        // is exactly what kicker/offspring/waterbend pose), a scripted no is
        // consumed so strict mode sees it used, a scripted yes is consumed
        // (and so pays) only on an "optionalcost" cast where gorge elected
        // the cost, anything else declines.
        Object pay = optionalAdditionalCostAnswer(true, false, new ArrayList<>());
        Object consume = optionalAdditionalCostAnswer(false, false, new ArrayList<>(Arrays.asList("No")));
        Object consumeYes = optionalAdditionalCostAnswer(false, true, new ArrayList<>(Arrays.asList("Yes")));
        Object decline = optionalAdditionalCostAnswer(false, false, new ArrayList<>());
        // On a plain cast a leading Yes belongs to a later ask: leave it.
        check(optionalAdditionalCostAnswer(false, false, new ArrayList<>(Arrays.asList("Yes"))).toString().equals("DECLINE"),
                "a plain cast consumed a leading Yes meant for a later ask");
        check(pay.toString().equals("PAY"), "a bargained cast did not PAY the optional additional cost");
        check(consume.toString().equals("CONSUME"), "a scripted No was not consumed");
        check(consumeYes.toString().equals("CONSUME"), "a scripted Yes was not consumed");
        check(decline.toString().equals("DECLINE"), "an unscripted ask was not declined");
        // Preconditions: the three outcomes really differ, and the elected
        // mode wins over a stale scripted No.
        check(!pay.toString().equals(consume.toString()) && !consume.toString().equals(decline.toString()),
                "the three optional-cost outcomes must differ");
        check(!consumeYes.toString().equals(decline.toString()),
                "a scripted Yes must be consumed, not declined");
        check(optionalAdditionalCostAnswer(true, false, new ArrayList<>(Arrays.asList("No"))).toString().equals("PAY"),
                "the elected mode must win over a scripted No");

        // The sacrifice object comes from the cast's recorded choose picks.
        UUID opter = id(11), relic = id(12);
        BiPredicate<UUID, String> byId = (candidate, answer) -> answer.equals(candidate.toString());
        List<UUID> both = Arrays.asList(opter, relic);
        List<String> picks = new ArrayList<>(Arrays.asList(relic.toString()));
        check(relic.equals(costPickChoice(picks, both, byId)), "the recorded sacrifice pick was not honored");
        check(costPickChoice(new ArrayList<>(), both, byId) == null,
                "a missing pick must not be guessed at");
        List<String> unmatched = new ArrayList<>(Arrays.asList("@nope"));
        check(costPickChoice(unmatched, both, byId) == null, "an unmatched pick was accepted");
        System.out.println("PASS Bargain election (cast_mode acceptance, sacrifice recognition, "
                + "pay/consume/decline, recorded sacrifice pick)");
    }

    private static Object staticCall(String cls, String name, Class<?>[] types, Object... args) throws Exception {
        Method m = Class.forName(cls).getDeclaredMethod(name, types);
        m.setAccessible(true);
        return m.invoke(null, args);
    }

    /** The wave-5 (lb5-driver) ops: the copy-cost cast modes and the keyword
     * each elects, the prepared copy cast, the divided activation's scripted
     * "^X=" shares, the teamwork tap's recorded picks and the gained
     * tap-for-mana text a printed card does not carry. */
    private static void wave5() throws Exception {
        final String replay = "org.mage.test.oracle.ScenarioReplay";
        final String player = "org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer";
        for (String mode : new String[]{"prepared_copy", "conspired", "casualty", "teamworked"}) {
            check(castModeSupported(mode).equals("true"), mode + " cast_mode was not accepted");
        }
        Class<?>[] str = {String.class};
        check(mage.abilities.keyword.ConspireAbility.class.equals(staticCall(replay, "copyCostKeyword", str, "conspired")),
                "conspired must elect ConspireAbility's cost");
        check(mage.abilities.keyword.CasualtyAbility.class.equals(staticCall(replay, "copyCostKeyword", str, "casualty")),
                "casualty must elect CasualtyAbility's cost");
        check(mage.abilities.keyword.TeamworkAbility.class.equals(staticCall(replay, "copyCostKeyword", str, "teamworked")),
                "teamworked must elect TeamworkAbility's cost");
        for (String mode : new String[]{"", "bargained", "prepared_copy", "optionalcost"}) {
            check(staticCall(replay, "copyCostKeyword", str, mode) == null, mode + " must elect no copy cost");
        }

        // A divided activation (The Earth Crystal) skips its bare step
        // targets only when its own step scripts a "^X=" share.
        JsonObject sc = JsonParser.parseString("{\"xmage_answers\":[[{\"seat\":0,\"kind\":\"target\","
                + "\"value\":\"p0:Grizzly Bears^X=2\"}],null,[{\"seat\":0,\"kind\":\"target\",\"value\":\"p1:Grizzly Bears\"}]]}")
                .getAsJsonObject();
        Class<?>[] scIdx = {JsonObject.class, int.class};
        check((Boolean) staticCall(replay, "stepScriptsTargetAmount", scIdx, sc, 0), "a scripted ^X= share was not seen");
        check(!(Boolean) staticCall(replay, "stepScriptsTargetAmount", scIdx, sc, 1), "a null step scripted a share");
        check(!(Boolean) staticCall(replay, "stepScriptsTargetAmount", scIdx, sc, 2), "a bare target was read as a share");
        check(!(Boolean) staticCall(replay, "stepScriptsTargetAmount", scIdx, sc, 5), "an out-of-range step scripted a share");
        check(!(Boolean) staticCall(replay, "stepScriptsTargetAmount", scIdx, new JsonObject(), 0),
                "a scenario with no answers scripted a share");

        // The teamwork tap names the recorded creatures, then closes the
        // open-ended ask only when another candidate is left.
        UUID hill = id(21), bears = id(22);
        BiPredicate<UUID, String> byId = (candidate, answer) -> answer.equals(candidate.toString());
        Class<?>[] tw = {List.class, List.class, BiPredicate.class};
        Object only = staticCall(player, "teamworkTapChoices", tw,
                new ArrayList<>(Arrays.asList(hill.toString())), Arrays.asList(hill), byId);
        check(only.equals(Arrays.asList(hill.toString())), "a lone candidate's tap must not be followed by a skip: " + only);
        Object more = staticCall(player, "teamworkTapChoices", tw,
                new ArrayList<>(Arrays.asList(hill.toString())), Arrays.asList(hill, bears), byId);
        check(more.equals(Arrays.asList(hill.toString(), TestPlayer.CHOICE_SKIP)),
                "a tap leaving a candidate untapped must close the ask: " + more);
        Object none = staticCall(player, "teamworkTapChoices", tw,
                new ArrayList<>(Arrays.asList("@nope")), Arrays.asList(hill, bears), byId);
        check(((List<?>) none).isEmpty(), "an unmatched pick must queue nothing (strict mode names the miss)");

        // Locus of Enlightenment's gained "{T}: Add {G}" is a mana ability
        // by its text; a cost or a non-mana effect is not.
        check((Boolean) staticCall(replay, "isTapManaText", str, "{T}: Add {G}"), "{T}: Add {G} is a tap-mana text");
        check((Boolean) staticCall(replay, "isTapManaText", str, "{T}: Add {C}{C}."), "{T}: Add {C}{C}. is a tap-mana text");
        check(!(Boolean) staticCall(replay, "isTapManaText", str, "{1}, {T}: Add {G}"), "a mana-costed ability is not the shape");
        check(!(Boolean) staticCall(replay, "isTapManaText", str, "{T}: Draw a card."), "a draw is not a mana ability");
        System.out.println("PASS wave-5 ops (copy-cost modes, prepared copy, divided activation shares, "
                + "teamwork tap, gained tap-mana text)");
    }

    /** The craft material ask and the driver's fill loop. CraftCost.pay calls
     * chooseTarget exactly once, so the driver must keep asking until the
     * ask's minimum is chosen; the recognition must be structural (the target
     * class plus a min of two or more), because the same target class serves
     * single-material asks (Endless Detour, Daretti). */
    private static void craft() throws Exception {
        mage.filter.common.FilterCreatureCard cards = new mage.filter.common.FilterCreatureCard();
        mage.filter.common.FilterControlledCreaturePermanent permanents =
                new mage.filter.common.FilterControlledCreaturePermanent();
        mage.target.Target craft = new mage.target.common.TargetCardInGraveyardBattlefieldOrStack(
                2, 2, cards, permanents);
        mage.target.Target single = new mage.target.common.TargetCardInGraveyardBattlefieldOrStack(
                1, 1, cards, permanents);
        mage.target.Target ordinary = new mage.target.TargetPermanent();
        check(isCraftMaterialsAsk(craft), "a two-material graveyard/battlefield ask was not recognized as craft");
        check(!isCraftMaterialsAsk(single), "a single-material graveyard/battlefield ask was classified as craft");
        check(!isCraftMaterialsAsk(ordinary), "an ordinary permanent ask was classified as craft");
        check(!isCraftMaterialsAsk(null), "a null ask was classified as craft");

        // The fill loop's decision: fill only while below the minimum AND an
        // answer is queued; a satisfied ask or an empty queue must stop it
        // (an empty queue would fall into the base's strict-mode AI pick).
        check(craftMaterialsNeedFill(1, 2, true), "a half-filled craft ask with a queued answer must fill");
        check(!craftMaterialsNeedFill(2, 2, true), "a satisfied craft ask must not fill");
        check(!craftMaterialsNeedFill(1, 2, false), "an empty queue must stop the fill loop");
        System.out.println("PASS craft material fill (structural recognition, fill decision)");
    }

    /** The split/Room spellings the contract pins, both engines' sides of
     * one DSK table: gorge spells every card object by its front face and
     * casts a face by its half's ability name; XMage stores the whole
     * "A // B" card object and its back-half spell for a face-1 cast. */
    private static void spellings() {
        String front = "Dazzling Theater", whole = "Dazzling Theater // Prop Room";
        check(ScenarioReplay.frontHalf(whole).equals(front), "front half of a split name");
        check(ScenarioReplay.backHalf(whole).equals("Prop Room"), "back half of a split name");
        check(ScenarioReplay.frontHalf(front).equals(front) && ScenarioReplay.backHalf(front).equals(front),
                "a non-split name is its own halves (precondition)");

        // A whole-name cast of a probe Room (not the card under test, so
        // xmageName is empty) must reach XMage's front-half "Cast <half>"
        // ability, not the whole name that found no ability.
        check(ScenarioReplay.castSpellingRule(front, "", whole).equals(front),
                "probe whole-name cast did not map to the front half");
        // The card under test keeps its two existing spellings.
        check(ScenarioReplay.castSpellingRule(front, whole, front).equals(front),
                "under-test front-name cast lost its spelling");
        check(ScenarioReplay.castSpellingRule(front, whole, whole).equals(front),
                "under-test whole-name cast did not map to the front half");
        // An ordinary name falls through to xmageSpelling (null).
        check(ScenarioReplay.castSpellingRule("", "", "Grizzly Bears") == null,
                "an ordinary cast name was consumed by the split rule");

        // Snapshot values: XMage's whole object name for a probe (xmageName
        // empty) and for the under-test card both read as the front; the
        // under-test card's back half -- the face-1 cast's spell name --
        // reads as the front too when the card is not known to be a Room
        // (the three-argument form); ordinary names pass through. A Room's
        // door spell and a fully unlocked Room permanent keep their CR 709.5
        // names: ScenarioReplayRoomSpellingTest pins those.
        check(ScenarioReplay.gorgeSpellingRule(front, "", whole).equals(front),
                "probe whole object name did not rewrite to the front half");
        check(ScenarioReplay.gorgeSpellingRule(front, whole, whole).equals(front),
                "under-test whole object name did not rewrite to the front");
        check(ScenarioReplay.gorgeSpellingRule(front, whole, "Prop Room").equals(front),
                "face-1 cast spell name (back half) did not rewrite to the front");
        check(ScenarioReplay.gorgeSpellingRule(front, whole, "Grizzly Bears").equals("Grizzly Bears"),
                "an ordinary snapshot name was rewritten");
        // A non-spell snapshot string (a Room permanent whose back door is
        // unlocked, CR 709.5) keeps the back half's name; the whole object
        // name still reads as the front half.
        check(ScenarioReplay.gorgeObjectSpellingRule(front, whole, "Prop Room").equals("Prop Room"),
                "an unlocked back-door permanent name was rewritten to the front");
        check(ScenarioReplay.gorgeObjectSpellingRule(front, whole, whole).equals(front),
                "whole object name did not rewrite to the front half");
        // Precondition: the back half and the front really are distinct names.
        check(!front.equals("Prop Room"), "halves must differ for the alias to mean anything");
        System.out.println("PASS split/Room spellings (probe whole-name cast, back-half spell alias, "
                + "whole object name rewrite, pass-through)");
    }

    /** The scenario's seat count: absent and 2 are the two-seat duel, 3 the
     * FreeForAll, anything else fails (gorge's oracleSeatCount). */
    private static void seatCounts() {
        check(ScenarioReplay.scenarioPlayers(JsonParser.parseString("{}").getAsJsonObject()) == 2, "absent players is not 2");
        check(ScenarioReplay.scenarioPlayers(JsonParser.parseString("{\"players\":2}").getAsJsonObject()) == 2, "players 2 is not 2");
        check(ScenarioReplay.scenarioPlayers(JsonParser.parseString("{\"players\":3}").getAsJsonObject()) == 3, "players 3 is not 3");
        for (String bad : new String[]{"1", "4"}) {
            boolean threw = false;
            try {
                ScenarioReplay.scenarioPlayers(JsonParser.parseString("{\"players\":" + bad + "}").getAsJsonObject());
            } catch (IllegalArgumentException e) {
                threw = true;
            }
            check(threw, "players " + bad + " was accepted");
        }
        System.out.println("PASS seat counts (absent/2 duel, 3 free-for-all, others refused)");
    }

    public static void main(String[] args) throws Exception {
        attachments();
        zones();
        passes();
        statelessPassToKeepsPhase();
        spree();
        mustAttack();
        bargain();
        craft();
        wave5();
        spellings();
        seatCounts();
    }
}
