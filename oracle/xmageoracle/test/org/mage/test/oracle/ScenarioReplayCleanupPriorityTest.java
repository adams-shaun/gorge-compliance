package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
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
import java.util.List;

/** Replays Stolen Uniform through its cleanup-step trigger: the control effect
 * ends in the cleanup step (CR 514.2) and "when you lose control of that
 * Equipment this turn" triggers there, so the players receive priority in
 * cleanup (CR 514.3a) and gorge records passes at step "cleanup", including a
 * trigger resolution and the second cleanup step that follows. phaseOfGorgeStep
 * had no cleanup case, so the follower threw "pass position in unknown step
 * cleanup" (xproof triage xpt-8171bc0b). Run
 * scripts/xmage-oracle-test-cleanup-priority.sh; it needs the out-of-tree
 * build's card database and deck files, like a full replay. */
public final class ScenarioReplayCleanupPriorityTest {
    private static final String FIXTURE = "cleanup-priority-stolen-uniform.jsonl";

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    /** The step each checkpoint must report: the setup snapshot is main 1,
     * then each step's snapshot is the position its pass reached (xmage_pos),
     * or the step the scenario was already in for an action step. */
    private static List<String> expectedSteps(JsonObject sc) {
        List<String> expected = new ArrayList<>();
        expected.add("PRECOMBAT_MAIN");
        JsonArray steps = sc.getAsJsonArray("steps");
        JsonArray positions = sc.getAsJsonArray("xmage_pos");
        String current = "PRECOMBAT_MAIN";
        for (int i = 0; i < steps.size(); i++) {
            if (positions != null && i < positions.size()
                    && positions.get(i) != null && positions.get(i).isJsonObject()) {
                JsonObject pos = positions.get(i).getAsJsonObject();
                current = ScenarioReplay.phaseOfGorgeStep(pos.get("step").getAsString()).name();
            }
            expected.add(current);
        }
        return expected;
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

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args.length > 0 ? args[0]
                : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures");
        String line = new String(Files.readAllBytes(dir.resolve(FIXTURE)), StandardCharsets.UTF_8).trim();
        JsonObject sc = JsonParser.parseString(line).getAsJsonObject();

        equal("CLEANUP", ScenarioReplay.phaseOfGorgeStep("cleanup").name(), "cleanup step mapping");
        List<String> expected = expectedSteps(sc);
        int cleanups = 0;
        for (String s : expected) {
            cleanups += "CLEANUP".equals(s) ? 1 : 0;
        }
        // The fixture really holds a cleanup priority window and the second
        // cleanup round after the trigger resolves (CR 514.3a).
        equal(4, cleanups, "expected cleanup checkpoints");

        JsonObject res = replay(sc);
        if (res.has("harness") && !res.get("harness").isJsonNull()) {
            throw new AssertionError(FIXTURE + " harness: " + res.get("harness").getAsString());
        }
        if (res.has("error") && !res.get("error").isJsonNull()) {
            throw new AssertionError(FIXTURE + " error: " + res.get("error").getAsString());
        }
        List<String> actual = new ArrayList<>();
        for (JsonElement e : res.getAsJsonArray("snapshots")) {
            actual.add(e.getAsJsonObject().get("step").getAsString());
        }
        equal(expected, actual, "snapshot steps");
        System.out.println("PASS Stolen Uniform cleanup priority: " + actual.size()
                + " checkpoints step for step, " + cleanups + " at CLEANUP");
    }
}
