package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import mage.constants.PhaseStep;
import mage.game.Game;

/** No database or game: pins how the driver follows the runs of priority
 * passes of a recorded game (an xengine tape or window), where the steps carry
 * the position gorge reached after each pass (xmage_pos). Run
 * scripts/xmage-oracle-test-pass-runs.sh; the replay itself is a host gate. */
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

    public static void main(String[] args) {
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
        check(ScenarioReplay.phaseOfGorgeStep("combat-damage") == PhaseStep.COMBAT_DAMAGE, "combat-damage");
        check(ScenarioReplay.phaseOfGorgeStep("end") == PhaseStep.END_TURN, "end");
        check(throwsIae(() -> ScenarioReplay.phaseOfGorgeStep("cleanup")), "a step with no priority is refused");

        // A pass that finds the stack non-empty resolves its top object, on
        // the active seat, as a legacy pair's second pass does.
        JsonObject res = item("{steps:[{op:'cast',seat:0,card:'p0:Shock'},{op:'pass',seat:0},{op:'pass',seat:1}],"
                + "xmage_pos:[null,{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0,resolves:true}]}");
        JsonArray rs = ScenarioReplay.steps(res);
        check(ScenarioReplay.passCommands(rs, 1, 0).isEmpty(), "the first pass of a pair queues nothing");
        List<String> resolve = ScenarioReplay.passCommands(rs, 2, 0);
        check(resolve.equals(Arrays.asList(ScenarioReplay.CMD_REQUIRE_STACK, ScenarioReplay.CMD_WAIT_RESOLVE_ONE)),
                "the resolving pass queues the one-object resolution: " + resolve);
        check(ScenarioReplay.passCommands(rs, 2, 1).equals(resolve), "the resolution queues the same on seat 1's turn");

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

        // Legacy scenarios carry no position and keep their shapes.
        JsonArray pair = JsonParser.parseString("[{op:'pass',seat:0},{op:'pass',seat:1}]").getAsJsonArray();
        check(ScenarioReplay.passAction(pair, 0) == ScenarioReplay.PASS_PAIR_FIRST, "legacy pair first");
        check(ScenarioReplay.passAction(pair, 1) == ScenarioReplay.PASS_PAIR_SECOND, "legacy pair second");
        declTiming();
        emptyStackPair();
        declarationStepEnd();
        System.out.println("ScenarioReplayPassRunsTest ok");
    }
}
