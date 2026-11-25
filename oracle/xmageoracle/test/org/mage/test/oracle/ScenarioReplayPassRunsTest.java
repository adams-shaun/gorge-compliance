package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.PrintWriter;
import java.io.StringReader;
import java.io.StringWriter;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import mage.constants.PhaseStep;
import mage.game.Game;

/** Pins how the driver follows the runs of priority passes of a recorded game
 * (an xengine tape or window), where the steps carry the position gorge
 * reached after each pass (xmage_pos). The unit checks need no database; with
 * the fixture directory argument, the Rebuff the Wicked s2 row is also replayed
 * end to end. Run scripts/xmage-oracle-test-pass-runs.sh. */
public final class ScenarioReplayPassRunsTest {
    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static JsonObject item(String json) {
        JsonObject sc = JsonParser.parseString(json).getAsJsonObject();
        ScenarioReplay.mergePassPositions(sc);
        return sc;
    }

    private static boolean throwsIae(Runnable r) {
        try {
            r.run();
            return false;
        } catch (IllegalArgumentException expected) {
            return true;
        }
    }

    /** A pass that moves into a declaration step the next step declares in
     * is checkpointed before the declaration (XMage declares in beginStep). */
    private static void declTiming() {
        JsonObject atk = item("{steps:[{op:'pass',seat:0},{op:'pass',seat:1},{op:'attack',seat:0,attackers:['p0:Bear'],defender:'p1'},{op:'pass',seat:0}],"
                + "xmage_pos:[{turn:1,step:'begin-combat',active:0},{turn:1,step:'declare-attackers',active:0,moved:true},null,"
                + "{turn:1,step:'declare-attackers',active:0}]}");
        JsonArray as = ScenarioReplay.steps(atk);
        check(ScenarioReplay.declarationAhead(as, 0) == null, "a pass that stays in its step has no declaration ahead");
        check(ScenarioReplay.declarationAhead(as, 1) == PhaseStep.DECLARE_ATTACKERS, "the pass into declare-attackers stops before the attack");
        check(ScenarioReplay.declarationAhead(as, 3) == null, "the pass after the declaration has none ahead");
        check(ScenarioReplay.beforeDeclaration(PhaseStep.DECLARE_ATTACKERS) == PhaseStep.BEGIN_COMBAT,
                "attackers are declared after begin-combat's last priority");
        check(ScenarioReplay.beforeDeclaration(PhaseStep.DECLARE_BLOCKERS) == PhaseStep.DECLARE_ATTACKERS,
                "blockers are declared after declare-attackers' last priority");
        check(throwsIae(() -> ScenarioReplay.beforeDeclaration(PhaseStep.PRECOMBAT_MAIN)), "main has no declaration");

        JsonObject blk = item("{steps:[{op:'pass',seat:1},{op:'block',seat:1,blocks:[['p1:Bear','p0:Bear']]}],"
                + "xmage_pos:[{turn:1,step:'declare-blockers',active:0,moved:true},null]}");
        check(ScenarioReplay.declarationAhead(ScenarioReplay.steps(blk), 0) == PhaseStep.DECLARE_BLOCKERS,
                "the pass into declare-blockers stops before a block");

        // An empty declaration (a no-op step) and a pass not followed by one stay put.
        JsonObject none = item("{steps:[{op:'pass',seat:1},{op:'block',seat:1}],"
                + "xmage_pos:[{turn:1,step:'declare-blockers',active:0,moved:true},null]}");
        check(ScenarioReplay.declarationAhead(ScenarioReplay.steps(none), 0) == null, "an empty block declares nothing");
        JsonObject move = item("{steps:[{op:'pass',seat:1},{op:'pass',seat:0}],"
                + "xmage_pos:[{turn:1,step:'declare-attackers',active:0,moved:true},{turn:1,step:'declare-attackers',active:0}]}");
        check(ScenarioReplay.declarationAhead(ScenarioReplay.steps(move), 0) == null, "no attack follows, so nothing is pending");
        // A legacy scenario carries no position.
        JsonArray legacy = JsonParser.parseString("[{op:'pass',seat:0},{op:'attack',seat:0,attackers:['p0:Bear'],defender:'p1'}]").getAsJsonArray();
        check(ScenarioReplay.declarationAhead(legacy, 0) == null, "legacy scenarios keep their checkpoints");
    }

    /** A recorded game's resolving pass finds XMage's stack empty (gorge
     * resolved an object XMage never put there): not a harness failure. The
     * legacy pairs still require the stack object. */
    private static void emptyStackPair() {
        JsonObject res = item("{steps:[{op:'cast',seat:0,card:'p0:Shock'},{op:'pass',seat:0},{op:'pass',seat:1}],"
                + "xmage_pos:[null,{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0,resolves:true}]}");
        JsonArray rs = ScenarioReplay.steps(res);
        check(ScenarioReplay.passCommands(rs, 2, 0).contains(ScenarioReplay.CMD_REQUIRE_STACK), "the command list is unchanged");
        check(!ScenarioReplay.requireStackIsStrict(rs, 2), "a recorded game's pass does not demand a stack object");
        JsonArray pair = JsonParser.parseString("[{op:'cast',seat:0,card:'p0:Shock'},{op:'pass',seat:0},{op:'pass',seat:1}]").getAsJsonArray();
        check(ScenarioReplay.requireStackIsStrict(pair, 2), "a legacy pair demands the stack object it resolves");
    }

    /** The Rebuff the Wicked s2 shape: the active seat passes, the other
     * seat taps a land for mana and passes, then the active seat passes. The
     * mana ability is an action between the two passes, so they are not in
     * succession (CR 117.4): the other seat's pass only hands priority back
     * and the game is still in main1. Its position must not count as a new
     * occurrence of main1 (which hides the checkpoint at a Step XMage never
     * starts); the active seat's next pass is the one that ends main1 and
     * begins combat. Both the corrected xmage_pos (no moved on the split
     * pass) and an older item's (moved=true) are pinned. */
    private static void passPairSplitByOpponentManaAbilityAdvancesStep() {
        String steps = "{steps:[{op:'pass',seat:0},{op:'activate',seat:1,card:'p1:Plains'},"
                + "{op:'pass',seat:1},{op:'pass',seat:0}],";
        JsonObject stale = item(steps + "xmage_pos:[{turn:1,step:'main1',active:0},"
                + "{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0,moved:true},"
                + "{turn:1,step:'begin-combat',active:0,moved:true}]}");
        JsonArray staleSteps = ScenarioReplay.steps(stale);
        check(!ScenarioReplay.passEntersNewStep(staleSteps, 2),
                "a split pass after the opponent's mana ability stays in the same main1 instance");
        check(ScenarioReplay.passEntersNewStep(staleSteps, 3),
                "the active seat's next pass ends main1 and begins combat");
        check(ScenarioReplay.declarationAhead(staleSteps, 2) == null,
                "the split pass enters no declaration step");
        for (int i = 0; i < staleSteps.size(); i++) {
            if ("pass".equals(staleSteps.get(i).getAsJsonObject().get("op").getAsString())) {
                check(ScenarioReplay.passAction(staleSteps, i) == ScenarioReplay.PASS_RUN,
                        "pass " + i + " of the split pair is PASS_RUN");
                check(ScenarioReplay.passCommands(staleSteps, i, 0).isEmpty(),
                        "pass " + i + " of the split pair queues nothing: "
                                + ScenarioReplay.passCommands(staleSteps, i, 0));
            }
        }

        JsonObject fixed = item(steps + "xmage_pos:[{turn:1,step:'main1',active:0},"
                + "{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0},"
                + "{turn:1,step:'begin-combat',active:0,moved:true}]}");
        JsonArray fixedSteps = ScenarioReplay.steps(fixed);
        check(!ScenarioReplay.passEntersNewStep(fixedSteps, 2),
                "the corrected position also stays in main1");
        check(ScenarioReplay.passEntersNewStep(fixedSteps, 3),
                "the corrected position advances to begin-combat at the active seat's pass");

        // A plain pair and an additional same-named step still advance: their
        // two passes are in succession, with no action in between.
        JsonObject plain = item("{steps:[{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:0}],"
                + "xmage_pos:[{turn:1,step:'main1',active:0},{turn:1,step:'begin-combat',active:0,moved:true},"
                + "{turn:1,step:'begin-combat',active:0}]}");
        check(ScenarioReplay.passEntersNewStep(ScenarioReplay.steps(plain), 1),
                "a plain pair still begins combat");
        JsonObject extra = item("{steps:[{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:0}],"
                + "xmage_pos:[{turn:1,step:'end',active:0},{turn:1,step:'end',active:0,moved:true},"
                + "{turn:1,step:'end',active:0}]}");
        check(ScenarioReplay.passEntersNewStep(ScenarioReplay.steps(extra), 1),
                "an additional same-named step still begins at a round of passes");
    }

    /** The active seat's closing pass of a split additional end step is the
     * round's last (CR 117.4/500.9) and begins the next end step even when a
     * stale item recorded no move there. The split handoff before it does not
     * begin an instance. */
    private static void splitAdditionalEndStepAdvancesOnClosingSeatPass() {
        String steps = "{steps:[{op:'pass',seat:0},{op:'activate',seat:1,card:'p1:Plains'},"
                + "{op:'pass',seat:1},{op:'pass',seat:0}],";
        JsonArray stale = ScenarioReplay.steps(item(steps + "xmage_pos:["
                + "{turn:1,step:'end',active:0},{turn:1,step:'end',active:0},"
                + "{turn:1,step:'end',active:0,moved:true},"
                + "{turn:1,step:'end',active:0}]}"));
        check(!ScenarioReplay.passEntersNewStep(stale, 2),
                "the split handoff in an additional end step stays in the same instance");
        check(ScenarioReplay.passEntersNewStep(stale, 3),
                "the active seat's closing pass begins the next end step");
        JsonArray fixed = ScenarioReplay.steps(item(steps + "xmage_pos:["
                + "{turn:1,step:'end',active:0},{turn:1,step:'end',active:0},"
                + "{turn:1,step:'end',active:0},"
                + "{turn:1,step:'end',active:0,moved:true}]}"));
        check(!ScenarioReplay.passEntersNewStep(fixed, 2),
                "the corrected position keeps the split handoff in its instance");
        check(ScenarioReplay.passEntersNewStep(fixed, 3),
                "the corrected position advances at the closing pass");
    }

    /** A first-strike combat whose round is split by the opponent's mana
     * ability: the active seat's closing pass ends the first damage step even
     * though the active seat takes it, and the next pass stays in the regular
     * damage step. */
    private static void splitFirstStrikeClosesAtRoundEnd() {
        JsonArray steps = ScenarioReplay.steps(item("{steps:["
                + "{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:0},"
                + "{op:'activate',seat:1,card:'p1:Plains'},{op:'pass',seat:1},{op:'pass',seat:0},"
                + "{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:0},{op:'pass',seat:1}],"
                + "xmage_pos:[{turn:1,step:'first-strike-damage',active:0,moved:true},"
                + "{turn:1,step:'first-strike-damage',active:0,resolves:true},"
                + "{turn:1,step:'first-strike-damage',active:0},null,"
                + "{turn:1,step:'first-strike-damage',active:0},"
                + "{turn:1,step:'combat-damage',active:0,moved:true},"
                + "{turn:1,step:'combat-damage',active:0},"
                + "{turn:1,step:'end-combat',active:0,moved:true},"
                + "{turn:1,step:'end-combat',active:0},"
                + "{turn:2,step:'upkeep',active:1,moved:true}]}"));
        check(!ScenarioReplay.passEntersNewStep(steps, 1),
                "the pass that resolves the trigger does not begin a step");
        check(!ScenarioReplay.passEntersNewStep(steps, 2),
                "the first pass after the resolution is not the round's last");
        check(!ScenarioReplay.passEntersNewStep(steps, 4),
                "the split handoff stays in the first damage step");
        check(ScenarioReplay.passEntersNewStep(steps, 5),
                "the active seat's closing pass ends the first damage step");
        check(!ScenarioReplay.passEntersNewStep(steps, 6),
                "the next pass stays in the regular damage step");
        check(ScenarioReplay.phaseOfGorgeStep("combat-damage") == PhaseStep.COMBAT_DAMAGE,
                "the regular damage step is a distinct XMage step");
    }

    /** The round size comes from the game's seats, not from the seats the
     * steps happen to name: a two-pass run in a three-seat game is not a full
     * round, so the handoff after it still yields priority. */
    private static void roundSizeUsesResolvedSeats() {
        JsonArray steps = ScenarioReplay.steps(item("{steps:["
                + "{op:'pass',seat:1},{op:'pass',seat:0},{op:'activate',seat:1,card:'p1:Plains'}],"
                + "xmage_pos:[{turn:1,step:'end',active:0},"
                + "{turn:1,step:'end',active:0,moved:true},null]}"));
        check(!ScenarioReplay.passEntersNewStep(steps, 1, 3),
                "two passes are not a full round with three seats");
        check(ScenarioReplay.passEntersNewStep(steps, 1, 2),
                "two passes are a full round with two seats");
        check(ScenarioReplay.passCommands(steps, 2, 0, 3)
                .contains(ScenarioReplay.CMD_YIELD_PRIORITY),
                "the three-seat handoff still yields priority");
        check(!ScenarioReplay.passCommands(steps, 2, 0, 2)
                .contains(ScenarioReplay.CMD_YIELD_PRIORITY),
                "a closed round has no handoff to yield");
    }

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    private static void noHarness(JsonObject res, String what) {
        if (res.has("harness") && !res.get("harness").isJsonNull()) {
            throw new AssertionError(what + " harness: " + res.get("harness").getAsString());
        }
        if (res.has("error") && !res.get("error").isJsonNull()) {
            throw new AssertionError(what + " error: " + res.get("error").getAsString());
        }
    }

    private static JsonObject replay(JsonObject sc) throws Exception {
        StringWriter out = new StringWriter();
        int n;
        try (BufferedReader in = new BufferedReader(new StringReader(sc.toString()));
             PrintWriter writer = new PrintWriter(out)) {
            n = ScenarioReplay.replayLines(new ScenarioReplay(), in, writer);
        }
        equal(1, n, "scenario rows");
        return JsonParser.parseString(out.toString().trim()).getAsJsonObject();
    }

    private static List<String> snapshotSteps(JsonObject res) {
        List<String> steps = new ArrayList<>();
        JsonArray snaps = res.getAsJsonArray("snapshots");
        for (int i = 0; i < snaps.size(); i++) {
            steps.add(snaps.get(i).getAsJsonObject().get("step").getAsString());
        }
        return steps;
    }

    /** The Rebuff the Wicked s2 row end to end: the split pass stays in main 1
     * (so its checkpoint is consumed, not left over), and the active seat's
     * next pass reaches begin-combat. Both the corrected xmage_pos and an
     * older item's stale moved:true on the split pass replay clean. */
    private static void replayPassPairSplitByOpponentManaAbility(Path dir) throws Exception {
        String line = new String(Files.readAllBytes(
                dir.resolve("rebuff-pass-pair-mana-split.jsonl")), StandardCharsets.UTF_8).trim();
        JsonObject corrected = JsonParser.parseString(line).getAsJsonObject();
        JsonObject stale = corrected.deepCopy();
        JsonArray stalePos = stale.getAsJsonArray("xmage_pos").deepCopy();
        stalePos.get(2).getAsJsonObject().addProperty("moved", true);
        stale.add("xmage_pos", stalePos);

        List<String> want = Arrays.asList("PRECOMBAT_MAIN", "PRECOMBAT_MAIN", "PRECOMBAT_MAIN",
                "PRECOMBAT_MAIN", "BEGIN_COMBAT");
        for (JsonObject sc : Arrays.asList(stale, corrected)) {
            JsonObject res = replay(sc);
            noHarness(res, "Rebuff pass pair split by the opponent's mana ability");
            equal(want.size(), res.getAsJsonArray("snapshots").size(), "snapshot count");
            equal(want, snapshotSteps(res), "snapshot steps");
            JsonArray snaps = res.getAsJsonArray("snapshots");
            // The split pass does not end main 1: p1's mana stays in the pool
            // until the active seat's next pass ends the step.
            equal("PRECOMBAT_MAIN", snaps.get(3).getAsJsonObject().get("step").getAsString(),
                    "split pass step");
            equal("W", snaps.get(3).getAsJsonObject().getAsJsonArray("players").get(1)
                    .getAsJsonObject().get("pool").getAsString(), "split pass pool");
            equal("", snaps.get(4).getAsJsonObject().getAsJsonArray("players").get(1)
                    .getAsJsonObject().get("pool").getAsString(), "step end pool");
        }
    }

    /** No More Lies over Reach Through Mists, replayed end to end: every
     * recorded step is checkpointed exactly once, in step order, and p1's
     * later Consider (step 9) is checkpointed after p0's answer was made and
     * passed (steps 4-7), not at the priority p1's step-3 pass handed on. */
    private static void replayRespondingOpponentKeepsStepOrder(Path dir) throws Exception {
        String line = new String(Files.readAllBytes(
                dir.resolve("responding-opponent-handoff-pass-no-more-lies.jsonl")), StandardCharsets.UTF_8).trim();
        JsonObject res = replay(JsonParser.parseString(line).getAsJsonObject());
        noHarness(res, "responding opponent handoff pass");
        List<String> order = new ArrayList<>();
        JsonArray snaps = res.getAsJsonArray("snapshots");
        for (int i = 0; i < snaps.size(); i++) {
            String cp = snaps.get(i).getAsJsonObject().get("checkpoint").getAsString();
            if (cp.equals("setup")) {
                continue;
            }
            order.add(cp);
        }
        List<String> want = new ArrayList<>();
        for (int i = 0; i < 26; i++) {
            want.add("step " + i + " (" + Arrays.asList("pass", "activate", "cast", "pass", "activate",
                    "activate", "cast", "pass", "activate", "cast", "pass", "pass", "pass", "pass", "pass",
                    "pass", "pass", "pass", "pass", "pass", "pass", "pass", "pass", "pass", "pass", "pass")
                    .get(i) + ")");
        }
        equal(want, order, "one snapshot per step, in step order");
    }

    /** No More Lies: p1 taps a land, casts Reach Through Mists and passes
     * (the stack keeps the spell), p0 answers by tapping mana and casting the
     * counterspell, and only then p1 acts again in the same main phase (its
     * later Consider). p1's pass must really pass: the opponent's next step
     * is a response, not a pass, but it is still the seat that takes priority
     * after the pass (CR 117.3d), and a virtual pass would let XMage drain
     * p1's queue at one priority, firing the Consider before p0's answer. */
    private static void respondingOpponentMakesHandoffPassReal() {
        JsonObject run = item("{steps:[{op:'pass',seat:0},{op:'activate',seat:1,card:'p1:Island'},"
                + "{op:'cast',seat:1,card:'p1:Reach Through Mists'},{op:'pass',seat:1},"
                + "{op:'activate',seat:0,card:'p0:Island'},{op:'cast',seat:0,card:'p0:No More Lies'},"
                + "{op:'pass',seat:0},{op:'cast',seat:1,card:'p1:Consider'}],"
                + "xmage_pos:[null,null,null,{turn:1,step:'main1',active:0},null,null,null,null]}");
        JsonArray ts = ScenarioReplay.steps(run);
        List<Boolean> real = new ArrayList<>();
        for (int i = 0; i < ts.size(); i++) {
            real.add(ScenarioReplay.passHandsOffBeforeLaterAction(ts, i, 0, 2));
        }
        equal(Arrays.asList(false, false, false, true, false, false, false, false), real,
                "only p1's pass before p0's response and p1's later cast is a real pass");

        // The same shape with nothing later of the passing seat's in the step:
        // the virtual pass is still enough, a response alone does not make it real.
        JsonObject last = item("{steps:[{op:'pass',seat:0},{op:'activate',seat:1,card:'p1:Island'},"
                + "{op:'cast',seat:1,card:'p1:Reach Through Mists'},{op:'pass',seat:1},"
                + "{op:'activate',seat:0,card:'p0:Island'},{op:'cast',seat:0,card:'p0:No More Lies'}],"
                + "xmage_pos:[null,null,null,{turn:1,step:'main1',active:0},null,null]}");
        JsonArray lastSteps = ScenarioReplay.steps(last);
        check(!ScenarioReplay.passHandsOffBeforeLaterAction(lastSteps, 3, 0, 2),
                "a pass with no later action of its seat stays virtual before a response");

        // The exclusions keep holding before a responding opponent: a pass
        // that resolves the top object is its own real pass by another rule,
        // a pass that moved the step hands priority inside the new step, and
        // the seat acting again right after its own pass is not a handoff.
        JsonObject res = item("{steps:[{op:'pass',seat:0},{op:'activate',seat:1,card:'p1:Island'},"
                + "{op:'cast',seat:1,card:'p1:Reach Through Mists'},{op:'pass',seat:1},"
                + "{op:'activate',seat:0,card:'p0:Island'},{op:'cast',seat:1,card:'p1:Consider'}],"
                + "xmage_pos:[null,null,null,{turn:1,step:'main1',active:0,resolves:true},null,null]}");
        check(!ScenarioReplay.passHandsOffBeforeLaterAction(ScenarioReplay.steps(res), 3, 0, 2),
                "a resolving pass is real by CMD_PASS_OWN_PRIORITY, not this rule");
        JsonObject moved = item("{steps:[{op:'pass',seat:0},{op:'activate',seat:1,card:'p1:Island'},"
                + "{op:'cast',seat:1,card:'p1:Reach Through Mists'},{op:'pass',seat:1},"
                + "{op:'activate',seat:0,card:'p0:Island'},{op:'cast',seat:1,card:'p1:Consider'}],"
                + "xmage_pos:[null,null,null,{turn:2,step:'main1',active:0,moved:true},null,null]}");
        check(!ScenarioReplay.passHandsOffBeforeLaterAction(ScenarioReplay.steps(moved), 3, 0, 2),
                "a pass that moved the step has no later action in its instance");
        JsonObject own = item("{steps:[{op:'pass',seat:0},{op:'activate',seat:1,card:'p1:Island'},"
                + "{op:'cast',seat:1,card:'p1:Reach Through Mists'},{op:'pass',seat:1},"
                + "{op:'cast',seat:1,card:'p1:Consider'}],"
                + "xmage_pos:[null,null,null,{turn:1,step:'main1',active:0},null]}");
        check(!ScenarioReplay.passHandsOffBeforeLaterAction(ScenarioReplay.steps(own), 3, 0, 2),
                "the seat's own action right after its pass is no handoff to make real");
    }

    /** Helix Pinnacle / Saproling Burst: the active seat 1's opponent (seat 0)
     * activates twice, passes, seat 1 resolves the top ability and passes
     * again (CR 117.3b), then seat 0 activates with the first ability still
     * on the stack. Seat 0's pass must really pass: otherwise its queued third
     * activation fires at the same priority and seat 1's two checkpoints run
     * after it. */
    private static void stackedOtherSeatActivationsKeepResolvingPassCheckpoint(Path dir) throws Exception {
        JsonObject two = item("{steps:[{op:'pass',seat:1},{op:'activate',seat:0,card:'p0:X'},{op:'activate',seat:0,card:'p0:X'},"
                + "{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:1},{op:'activate',seat:0,card:'p0:X'}],"
                + "xmage_pos:[{turn:2,step:'main1',active:1},null,null,{turn:2,step:'main1',active:1},"
                + "{turn:2,step:'main1',active:1,resolves:true},{turn:2,step:'main1',active:1},null]}");
        JsonArray ts = ScenarioReplay.steps(two);
        List<Boolean> real = new ArrayList<>();
        for (int i = 0; i < ts.size(); i++) {
            real.add(ScenarioReplay.passHandsOffBeforeLaterAction(ts, i, 1, 2));
        }
        equal(Arrays.asList(false, false, false, true, false, false, false), real,
                "only seat 0's pass before its later activation is a real pass");
        // No later action of seat 0 in the step: its virtual pass is enough.
        JsonObject single = item("{steps:[{op:'activate',seat:0,card:'p0:X'},{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:1}],"
                + "xmage_pos:[null,{turn:2,step:'main1',active:1},{turn:2,step:'main1',active:1,resolves:true},"
                + "{turn:2,step:'main1',active:1}]}");
        for (int i = 0; i < 4; i++) {
            check(!ScenarioReplay.passHandsOffBeforeLaterAction(ScenarioReplay.steps(single), i, 1, 2),
                    "a single activation keeps its virtual pass (step " + i + ")");
        }
        // The seat's later action is in a later step: the pass between enters it.
        JsonObject later = item("{steps:[{op:'activate',seat:0,card:'p0:X'},{op:'pass',seat:0},{op:'pass',seat:1},{op:'activate',seat:0,card:'p0:X'}],"
                + "xmage_pos:[null,{turn:2,step:'main1',active:1},{turn:2,step:'begin-combat',active:1,moved:true},null]}");
        check(!ScenarioReplay.passHandsOffBeforeLaterAction(ScenarioReplay.steps(later), 1, 1, 2),
                "an action after the step moved on is not in this step's priority");

        String line = new String(Files.readAllBytes(
                dir.resolve("stacked-other-seat-activations-helix-pinnacle.jsonl")), StandardCharsets.UTF_8).trim();
        JsonObject res = replay(JsonParser.parseString(line).getAsJsonObject());
        noHarness(res, "stacked other-seat activations");
        JsonArray snaps = res.getAsJsonArray("snapshots");
        List<String> order = new ArrayList<>();
        List<Integer> stack = new ArrayList<>();
        for (int i = 0; i < snaps.size(); i++) {
            JsonObject snap = snaps.get(i).getAsJsonObject();
            String cp = snap.get("checkpoint").getAsString();
            if (cp.equals("setup")) {
                continue;
            }
            order.add(cp);
            stack.add(snap.has("stack") ? snap.getAsJsonArray("stack").size() : 0);
        }
        equal(Arrays.asList("step 0 (pass)", "step 1 (activate)", "step 2 (activate)", "step 3 (pass)",
                "step 4 (pass)", "step 5 (pass)", "step 6 (activate)"), order, "one snapshot per step, in step order");
        equal(Arrays.asList(0, 1, 2, 2, 1, 1, 2), stack,
                "the resolving pass sees one ability left, and the third activation stacks on it");
    }

    /** A declaration-ahead checkpoint stands after a step end, which empties
     * every mana pool through XMage's own emptyManaPools; an ordinary
     * checkpoint keeps the pool XMage shows. */
    private static void declarationStepEnd() {
        final int[] calls = {0};
        Game fake = (Game) java.lang.reflect.Proxy.newProxyInstance(
                ScenarioReplayPassRunsTest.class.getClassLoader(),
                new Class<?>[]{Game.class},
                (proxy, method, args) -> {
                    if (method.getName().equals("emptyManaPools")) {
                        calls[0]++;
                        check(args.length == 1 && args[0] == null, "the step end has no source ability");
                        return null;
                    }
                    throw new AssertionError("unexpected Game call " + method.getName());
                });
        ScenarioReplay.applyDeclarationStepEnd(PhaseStep.DECLARE_ATTACKERS, fake);
        check(calls[0] == 1, "the attacker declaration checkpoint applies the step end");
        ScenarioReplay.applyDeclarationStepEnd(PhaseStep.DECLARE_BLOCKERS, fake);
        check(calls[0] == 2, "the blocker declaration checkpoint applies the step end");
        ScenarioReplay.applyDeclarationStepEnd(null, fake);
        ScenarioReplay.applyDeclarationStepEnd(PhaseStep.BEGIN_COMBAT, fake);
        check(calls[0] == 2, "an ordinary checkpoint keeps the pool XMage shows");
    }

    public static void main(String[] args) throws Exception {
        // Three passes in a row: p0 passes (priority goes to p1, same step),
        // p1 passes (both passed on an empty stack: begin-combat), p0 passes
        // again. The legacy classifier throws "unsupported pass sequence".
        JsonArray bare = JsonParser.parseString("[{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:0}]").getAsJsonArray();
        check(throwsIae(() -> ScenarioReplay.passAction(bare, 1)),
                "without positions a three-pass run keeps failing loudly");
        JsonObject run = item("{steps:[{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:0}],"
                + "xmage_pos:[{turn:1,step:'main1',active:0},{turn:1,step:'begin-combat',active:0,moved:true},"
                + "{turn:1,step:'begin-combat',active:0}]}");
        JsonArray steps = ScenarioReplay.steps(run);
        for (int i = 0; i < steps.size(); i++) {
            check(ScenarioReplay.passAction(steps, i) == ScenarioReplay.PASS_RUN, "pass " + i + " of a run is PASS_RUN");
            check(ScenarioReplay.passAction(steps, i, 0) == ScenarioReplay.PASS_RUN, "pass " + i + " of a run is PASS_RUN (active seat)");
            check(ScenarioReplay.passCommands(steps, i, 0).isEmpty(),
                    "a pass that resolves nothing queues nothing: " + ScenarioReplay.passCommands(steps, i, 0));
        }
        // The same passes while seat 1 is active are no unsupported shape either.
        check(ScenarioReplay.passAction(steps, 0, 1) == ScenarioReplay.PASS_RUN, "a run is PASS_RUN on seat 1's turn");

        // A pass through an empty step moves the game on: the position names
        // the step the next steps are queued in.
        check(ScenarioReplay.phaseOfGorgeStep("begin-combat") == PhaseStep.BEGIN_COMBAT, "begin-combat");
        check(ScenarioReplay.phaseOfGorgeStep("main1") == PhaseStep.PRECOMBAT_MAIN, "main1");
        check(ScenarioReplay.phaseOfGorgeStep("first-strike-damage") == PhaseStep.FIRST_COMBAT_DAMAGE, "first-strike-damage");
        check(ScenarioReplay.phaseOfGorgeStep("combat-damage") == PhaseStep.COMBAT_DAMAGE, "combat-damage");
        check(ScenarioReplay.phaseOfGorgeStep("end") == PhaseStep.END_TURN, "end");
        // Cleanup normally gives no priority, but a trigger there does (CR 514.3a),
        // so gorge records passes at it (see ScenarioReplayCleanupPriorityTest).
        check(ScenarioReplay.phaseOfGorgeStep("cleanup") == PhaseStep.CLEANUP, "cleanup");
        check(throwsIae(() -> ScenarioReplay.phaseOfGorgeStep("no-such-step")), "an unknown step is refused");

        // A pass that finds the stack non-empty resolves its top object, on
        // the active seat, as a legacy pair's second pass does.
        JsonObject res = item("{steps:[{op:'cast',seat:0,card:'p0:Shock'},{op:'pass',seat:0},{op:'pass',seat:1}],"
                + "xmage_pos:[null,{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0,resolves:true}]}");
        JsonArray rs = ScenarioReplay.steps(res);
        check(ScenarioReplay.passCommands(rs, 1, 0).isEmpty(), "the first pass of a pair queues nothing");
        List<String> resolve = ScenarioReplay.passCommands(rs, 2, 0);
        check(resolve.equals(Arrays.asList(ScenarioReplay.CMD_PASS_OWN_PRIORITY, ScenarioReplay.CMD_REQUIRE_STACK,
                ScenarioReplay.CMD_WAIT_RESOLVE_ONE)),
                "seat 1's resolving pass on seat 0's turn queues its own pass, then the one-object resolution: " + resolve);
        check(ScenarioReplay.passCommands(rs, 2, 1).equals(Arrays.asList(ScenarioReplay.CMD_REQUIRE_STACK,
                ScenarioReplay.CMD_WAIT_RESOLVE_ONE)), "the active seat's own resolving pass queues no extra pass");
        // The Lunar Insight shape: seat 1's resolving pass sits between seat 0's
        // passes and seat 1's own later cast on the same step. The pass seat 1
        // makes at its first priority must be queued before that cast.
        JsonObject lunar = item("{steps:[{op:'cast',seat:0,card:'p0:Lunar Insight'},{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:0},"
                + "{op:'activate',seat:1,card:'p1:Island'},{op:'cast',seat:1,card:'p1:Voracious Greatshark'}],"
                + "xmage_pos:[null,{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0,resolves:true},{turn:1,step:'main1',active:0},null,null]}");
        JsonArray ls = ScenarioReplay.steps(lunar);
        List<String> lres = ScenarioReplay.passCommands(ls, 2, 0);
        check(lres.get(0).equals(ScenarioReplay.CMD_PASS_OWN_PRIORITY), "seat 1's pass is queued first: " + lres);
        check(ScenarioReplay.passCommands(ls, 3, 0).isEmpty(), "seat 0's later pass queues nothing");
        check(ScenarioReplay.passCommands(ls, 4, 0).equals(Collections.singletonList(ScenarioReplay.CMD_YIELD_PRIORITY)),
                "seat 0's pass on the empty stack hands priority to seat 1's activation");

        // The active seat passes within a step and the other seat acts next:
        // yield priority first. After a pass that moved the step, or one that
        // resolved, there is nothing to yield.
        JsonObject hand = item("{steps:[{op:'pass',seat:0},{op:'cast',seat:1,card:'p1:Shock'},{op:'pass',seat:1},{op:'pass',seat:0},{op:'play',seat:1,card:'p1:Swamp'}],"
                + "xmage_pos:[{turn:1,step:'main1',active:0},null,{turn:1,step:'main1',active:0},"
                + "{turn:1,step:'begin-combat',active:0,moved:true},null]}");
        JsonArray hs = ScenarioReplay.steps(hand);
        check(ScenarioReplay.passCommands(hs, 1, 0).equals(Collections.singletonList(ScenarioReplay.CMD_YIELD_PRIORITY)),
                "the other seat's cast after the active seat's pass starts by yielding");
        check(ScenarioReplay.passCommands(hs, 4, 0).isEmpty(), "no yield after a pass that moved the step");

        // The checkpoints of seat 1's actions in that game run on seat 1's queue,
        // each right after its action; no other shape moves its checkpoint.
        check(ScenarioReplay.opensOnOtherSeat(ls, 4, 0) && ScenarioReplay.opensOnOtherSeat(ls, 5, 0),
                "a recorded game's non-active seat actions checkpoint on their own seat");
        check(!ScenarioReplay.opensOnOtherSeat(ls, 0, 0), "the active seat's action keeps its checkpoint seat");
        check(!ScenarioReplay.opensOnOtherSeat(ls, 4, 1), "the active seat's own action keeps its checkpoint seat");
        JsonArray legacy = JsonParser.parseString("[{op:'cast',seat:1,card:'p1:Shock'},{op:'pass',seat:0}]").getAsJsonArray();
        check(!ScenarioReplay.opensOnOtherSeat(legacy, 0, 0), "a scenario with no positioned pass keeps its checkpoint seat");

        // Legacy scenarios carry no position and keep their shapes.
        JsonArray pair = JsonParser.parseString("[{op:'pass',seat:0},{op:'pass',seat:1}]").getAsJsonArray();
        check(ScenarioReplay.passAction(pair, 0) == ScenarioReplay.PASS_PAIR_FIRST, "legacy pair first");
        check(ScenarioReplay.passAction(pair, 1) == ScenarioReplay.PASS_PAIR_SECOND, "legacy pair second");
        passPairSplitByOpponentManaAbilityAdvancesStep();
        splitAdditionalEndStepAdvancesOnClosingSeatPass();
        splitFirstStrikeClosesAtRoundEnd();
        roundSizeUsesResolvedSeats();
        respondingOpponentMakesHandoffPassReal();
        declTiming();
        emptyStackPair();
        declarationStepEnd();
        if (args.length > 0) {
            replayPassPairSplitByOpponentManaAbility(Paths.get(args[0]));
            stackedOtherSeatActivationsKeepResolvingPassCheckpoint(Paths.get(args[0]));
            replayRespondingOpponentKeepsStepOrder(Paths.get(args[0]));
        }
        System.out.println("ScenarioReplayPassRunsTest ok");
    }
}
