package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import mage.util.RandomUtil;

/** Replays the shape where a flash spell is queued for a later step of the
 * same turn and an earlier p0/p1 pass pair must not consume that cast: a
 * seat-0 spell resolves after the pair and seat 1's flash cast waits for its
 * own step. The committed fixture is the full ECL-s106 item
 * xe:RAND:2:5:w7 (xitems.jsonl line 40), whose cut input.jsonl changed the
 * timing and hid the defect. Run scripts/xmage-oracle-test-flash-held.sh. */
public final class ScenarioReplayFlashHeldTest {

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    /** Deal the scenario the same RNG seed replayLines gives it, then fail on
     * the harness row TestPlayer writes instead of an exception. */
    private static JsonObject replay(String name, JsonObject sc) {
        RandomUtil.setSeed(ScenarioReplay.scenarioSeed(sc));
        JsonObject res = new ScenarioReplay().replay(sc);
        if (res.has("harness")) {
            throw new AssertionError(name + ": " + res.get("harness").getAsString());
        }
        return res;
    }

    private static JsonObject snapshot(JsonObject res, int at) {
        JsonArray snaps = res.getAsJsonArray("snapshots");
        if (snaps == null || snaps.size() <= at) {
            throw new AssertionError("no snapshot " + at + " (have " + (snaps == null ? 0 : snaps.size()) + ")");
        }
        return snaps.get(at).getAsJsonObject();
    }

    private static JsonObject player(JsonObject res, int at, int seat) {
        return snapshot(res, at).getAsJsonArray("players").get(seat).getAsJsonObject();
    }

    /** The stack's source names, top first. */
    private static List<String> stack(JsonObject res, int at) {
        JsonArray stack = snapshot(res, at).has("stack")
                ? snapshot(res, at).getAsJsonArray("stack") : new JsonArray();
        List<String> names = new ArrayList<>();
        for (int i = 0; i < stack.size(); i++) {
            names.add(stack.get(i).getAsJsonObject().get("source").getAsString());
        }
        return names;
    }

    private static List<String> hand(JsonObject res, int at, int seat) {
        JsonArray cards = player(res, at, seat).getAsJsonArray("hand");
        List<String> names = new ArrayList<>();
        for (int i = 0; i < cards.size(); i++) {
            names.add(cards.get(i).getAsString());
        }
        return names;
    }

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    /** The reported item: p0 casts Impolite Entrance, both seats pass (it
     * resolves and p0 draws Flamekin Gildweaver), p0 casts Sting-Slinger,
     * then p1's queued Glamermite (flash) must wait for its own steps 10-13
     * instead of firing at the step-4 pass pair. */
    private static void recordedItem(String path) throws Exception {
        List<String> lines = Files.readAllLines(Paths.get(path));
        check(!lines.isEmpty(), "fixture is empty: " + path);
        JsonObject sc = JsonParser.parseString(lines.get(0)).getAsJsonObject();
        equal("xe:RAND:2:5:w7", sc.get("id").getAsString(), "fixture id");
        JsonObject res = replay("recorded item", sc);
        JsonArray snaps = res.getAsJsonArray("snapshots");
        equal(16, snaps.size(), "setup plus every one of the 15 steps is checkpointed");

        // Step 3 (pass, p0) still sees Impolite Entrance; step 4 (pass, p1)
        // resolves it and p0 draws, as gorge's snapshot does.
        equal(Arrays.asList("Impolite Entrance"), stack(res, 4), "step 3 stack before the resolving pass");
        equal(Arrays.asList(), stack(res, 5), "step 4 stack after the resolving pass");
        equal(Arrays.asList("Flamekin Gildweaver", "Glamermite", "Island", "Mountain", "Mountain", "Sting-Slinger"),
                hand(res, 5, 0), "step 4 p0 hand after Impolite Entrance resolves");
        equal(28, player(res, 5, 0).get("library_count").getAsInt(), "step 4 p0 library after the draw");

        // p1's Glamermite is queued for step 13 but must not fire at the
        // step-4 pass pair: it stays in hand through the p1 mana steps and is
        // on the stack only at its own cast step. The p1 mana steps keep their
        // own checkpoints (pool U/UU/UUU) instead of the whole queue leaking
        // into step 10.
        for (int at = 6; at <= 13; at++) {
            check(!stack(res, at).contains("Glamermite"),
                    "Glamermite must not be on the stack at snapshot " + at + ": " + stack(res, at));
        }
        equal("U", player(res, 11, 1).get("pool").getAsString(), "step 10 p1 pool");
        equal("UU", player(res, 12, 1).get("pool").getAsString(), "step 11 p1 pool");
        equal("UUU", player(res, 13, 1).get("pool").getAsString(), "step 12 p1 pool");
        check(hand(res, 13, 1).contains("Glamermite"), "Glamermite leaves p1's hand only at its cast step");
        equal(Arrays.asList("Glamermite", "Sting-Slinger"), stack(res, 14), "step 13 p1 flash cast");
        equal(6, hand(res, 14, 1).size(), "step 13 p1 hand after casting Glamermite");
        equal(Arrays.asList("Glamermite", "Sting-Slinger"), stack(res, 15), "step 14 stack is unchanged");
    }

    /** The minimal shape with cards that do not depend on the ECL set: p0
     * casts Divination, p0 and p1 pass (it resolves and draws two), then p1
     * casts Ambush Viper at instant speed in the same step. The pass pair must
     * resolve Divination before p1's queued flash cast is allowed to fire. */
    private static void syntheticShape() {
        JsonObject sc = JsonParser.parseString("{"
                + "id:'test:flash-held-after-pass',"
                + "name:'flash held after pass',turn:1,"
                + "setup:{"
                + "p0:{battlefield:['Island','Island','Island'],hand:['Divination'],"
                + "library_top:['Forest','Forest','Forest','Forest','Forest'],life:20},"
                + "p1:{battlefield:['Forest','Forest'],hand:['Ambush Viper'],"
                + "library_top:['Mountain','Mountain','Mountain','Mountain','Mountain'],life:20}},"
                + "steps:["
                + "{op:'cast',seat:0,card:'p0:Divination'},"
                + "{op:'pass',seat:0},{op:'pass',seat:1},{op:'cast',seat:1,card:'p1:Ambush Viper'},"
                + "{op:'pass',seat:1}],"
                + "xmage_pos:[null,{turn:1,step:'main1',active:0},"
                + "{turn:1,step:'main1',active:0,resolves:true},null,{turn:1,step:'main1',active:0}]}")
                .getAsJsonObject();
        JsonObject res = replay("synthetic shape", sc);
        equal(Arrays.asList("Divination"), stack(res, 2), "step 1 stack before the resolving pass");
        equal(Arrays.asList(), stack(res, 3), "step 2 stack after the resolving pass");
        equal(2, hand(res, 3, 0).size(), "step 2 p0 hand after Divination draws two");
        int libraryBefore = player(res, 2, 0).get("library_count").getAsInt();
        equal(libraryBefore - 2, player(res, 3, 0).get("library_count").getAsInt(),
                "step 2 p0 library after the draw");
        equal(Arrays.asList("Ambush Viper"), stack(res, 4), "step 3 p1 flash cast is on the stack");
        equal(2, hand(res, 4, 0).size(), "step 3 p0 hand is unchanged by the flash cast");
        equal(Arrays.asList("Ambush Viper"), stack(res, 5), "step 4 stack is unchanged");
    }

    public static void main(String[] args) throws Exception {
        if (args.length != 1) {
            throw new IllegalArgumentException("usage: ScenarioReplayFlashHeldTest <xitems.jsonl line>");
        }
        recordedItem(args[0]);
        syntheticShape();
        System.out.println("PASS flash cast held for its own step after an earlier pass pair resolves the top object");
    }
}
