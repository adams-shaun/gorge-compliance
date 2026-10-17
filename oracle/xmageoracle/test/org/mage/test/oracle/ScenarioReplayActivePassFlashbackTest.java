package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonParser;
import java.util.List;

/** No database or game: pins how the driver classifies the active seat-1
 * player's own pass and which cast_mode values it elects. Run
 * scripts/xmage-oracle-test-active-pass.sh; the replay itself is a host gate. */
public final class ScenarioReplayActivePassFlashbackTest {
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

    public static void main(String[] args) {
        // HOB Radagast shape: pass_to active p1, seat 1's pass, seat 0 casts.
        JsonArray own = steps("[{op:'pass_to',seat:0,step:'main1',active:'p1'},{op:'pass',seat:1},"
                + "{op:'cast',seat:0,card:'p0:Grizzly Bears'},{op:'resolve',seat:0}]");
        check(ScenarioReplay.passAction(own, 1, 1) == ScenarioReplay.PASS_ACTIVE_OWN,
                "seat 1's lone pass on its own turn must be the active player's own pass");
        List<String> cmds = ScenarioReplay.passCommands(own, 1, 1);
        check(cmds.isEmpty(), "the active player's own pass queues nothing; got " + cmds);
        // The cast step that follows is not a handoff and queues nothing either.
        check(ScenarioReplay.passCommands(own, 2, 1).isEmpty(), "the cast after an own pass queues nothing");

        // Preconditions: with seat 0 active the same pass is NOT an own pass,
        // so the rule must not swallow a non-active seat's pass.
        check(throwsIae(() -> ScenarioReplay.passAction(own, 1, 0)),
                "seat 1's lone pass while seat 0 is active must still be rejected");
        check(throwsIae(() -> ScenarioReplay.passCommands(own, 1, 0)),
                "seat 1's lone pass while seat 0 is active must still be rejected (commands)");
        // A pair on seat 1's turn is still refused (the pair's checkpoint
        // seat is seat 0).
        JsonArray pair = steps("[{op:'pass',seat:0},{op:'pass',seat:1}]");
        check(throwsIae(() -> ScenarioReplay.passCommands(pair, 1, 1)),
                "a pass pair on seat 1's turn must still be rejected");
        // Seat 0's pass while seat 1 is active is not the active player's own.
        JsonArray p0 = steps("[{op:'pass',seat:0},{op:'cast',seat:1,card:'p1:Shock'}]");
        check(throwsIae(() -> ScenarioReplay.passCommands(p0, 0, 1)),
                "seat 0's pass on seat 1's turn must still be rejected");

        check(ScenarioReplay.castModeSupported("flashback"), "flashback cast_mode must be elected");
        check(!ScenarioReplay.castModeSupported("escape"), "an unwired cast_mode must still be rejected");
        // TestPlayer ends an "@ref ability" ref at its first space, so the
        // command must carry the space-free twin, and setup must bind it.
        String fb = ScenarioReplay.flashbackCommand("@p0:Chatter of the Squirrel");
        check(fb.equals("@p0:Chatter_of_the_Squirrel Flashback"), "flashback command scopes to the card alias: " + fb);
        check(fb.indexOf(' ') == fb.indexOf(" Flashback"), "alias part of the flashback command holds a space: " + fb);
        java.util.List<String> bound = new java.util.ArrayList<>();
        ScenarioReplay.registerObjectAliases(0, "Chatter of the Squirrel", "p0:Chatter of the Squirrel",
                "p0:Chatter of the Squirrel", 0, java.util.UUID.randomUUID(), new java.util.LinkedHashMap<>(),
                (ref, objId) -> bound.add(ref));
        check(bound.contains("p0:Chatter of the Squirrel"), "scenario ref not bound: " + bound);
        check(bound.contains("p0:Chatter_of_the_Squirrel"), "space-free twin not bound: " + bound);
        check(ScenarioReplay.flashbackCommand(null).equals("Flashback"), "unaliased flashback command");
        System.out.println("PASS active seat 1 own pass is a no-op; flashback cast_mode elected via alias-scoped ability");
    }
}
