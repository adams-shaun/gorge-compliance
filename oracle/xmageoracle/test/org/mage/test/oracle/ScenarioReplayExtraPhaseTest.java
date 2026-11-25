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

/** Replays All-Out Assault's additional combat phase followed by an
 * additional main phase (CR 500.8): the extra BEGIN_COMBAT, DECLARE_ATTACKERS,
 * END_COMBAT and POSTCOMBAT_MAIN occurrences must each run their own queued
 * passes, not the regular phase's. Before the occurrence keying the driver
 * queued every command at (turn, PhaseStep), so the extra phase's passes ran
 * in the first occurrence and its six checkpoints collapsed onto the regular
 * phase's. Run scripts/xmage-oracle-test-extra-phase.sh; it needs the
 * out-of-tree build's card database and deck files, like a full replay. */
public final class ScenarioReplayExtraPhaseTest {
    private static final String FIXTURE = "extra-phase-all-out-assault.jsonl";
    private static final String UPKEEP_FIXTURE = "extra-upkeep-start-turn-obeka.jsonl";

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

    private static int count(List<String> steps, String step) {
        int n = 0;
        for (String s : steps) {
            n += step.equals(s) ? 1 : 0;
        }
        return n;
    }

    /** A copy cut to the first n steps, like xprep's CutSteps: the repro's
     * window is a prefix of the full game, and its per-step side tables are
     * parallel. */
    private static JsonObject cut(JsonObject sc, int n) {
        JsonObject out = sc.deepCopy();
        for (String key : new String[]{"steps", "xmage_answers", "xmage_ability", "xmage_pos", "payment"}) {
            if (!out.has(key) || !out.get(key).isJsonArray()) {
                continue;
            }
            JsonArray original = out.getAsJsonArray(key);
            JsonArray prefix = new JsonArray();
            for (int i = 0; i < n && i < original.size(); i++) {
                prefix.add(original.get(i));
            }
            out.add(key, prefix);
        }
        return out;
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
        JsonArray snaps = res.getAsJsonArray("snapshots");
        List<String> steps = new ArrayList<>();
        for (JsonElement e : snaps) {
            steps.add(e.getAsJsonObject().get("step").getAsString());
        }
        return steps;
    }

    private static void noHarness(JsonObject res, String what) {
        if (res.has("harness") && !res.get("harness").isJsonNull()) {
            throw new AssertionError(what + " harness: " + res.get("harness").getAsString());
        }
        if (res.has("error") && !res.get("error").isJsonNull()) {
            throw new AssertionError(what + " error: " + res.get("error").getAsString());
        }
    }

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args.length > 0 ? args[0]
                : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures");
        String line = new String(Files.readAllBytes(dir.resolve(FIXTURE)), StandardCharsets.UTF_8).trim();
        JsonObject sc = JsonParser.parseString(line).getAsJsonObject();
        List<String> expected = expectedSteps(sc);
        // The fixture really has an extra combat and main: each of those steps
        // occurs twice, so the test cannot pass on a collapsed phase list.
        equal(4, count(expected, "BEGIN_COMBAT"), "expected begin-combat checkpoints");
        equal(4, count(expected, "DECLARE_ATTACKERS"), "expected declare-attackers checkpoints");
        equal(4, count(expected, "END_COMBAT"), "expected end-combat checkpoints");
        equal(4, count(expected, "POSTCOMBAT_MAIN"), "expected postcombat-main checkpoints");

        JsonObject res = replay(sc);
        noHarness(res, FIXTURE);
        equal(expected.size(), res.getAsJsonArray("snapshots").size(), "snapshot count");
        List<String> actual = snapshotSteps(res);
        for (int i = 0; i < expected.size(); i++) {
            equal(expected.get(i), actual.get(i), "snapshot " + i + " step");
        }
        System.out.println("PASS All-Out Assault extra combat and extra main: "
                + expected.size() + " checkpoints step for step ("
                + count(actual, "BEGIN_COMBAT") + " begin-combat, "
                + count(actual, "DECLARE_ATTACKERS") + " declare-attackers, "
                + count(actual, "END_COMBAT") + " end-combat, "
                + count(actual, "POSTCOMBAT_MAIN") + " postcombat-main)");

        // A window cut at the second (extra) POSTCOMBAT_MAIN must stop there,
        // not at the first main 2: setStopAt names a PhaseStep and would pause
        // at the regular phase before the extra one.
        JsonObject cut = cut(sc, 27);
        List<String> cutExpected = expectedSteps(cut);
        JsonObject cutRes = replay(cut);
        noHarness(cutRes, FIXTURE + " cut at the extra main");
        equal(cutExpected.size(), cutRes.getAsJsonArray("snapshots").size(), "cut snapshot count");
        List<String> cutActual = snapshotSteps(cutRes);
        for (int i = 0; i < cutExpected.size(); i++) {
            equal(cutExpected.get(i), cutActual.get(i), "cut snapshot " + i + " step");
        }
        equal("POSTCOMBAT_MAIN", cutActual.get(cutActual.size() - 1), "cut window's last step");
        equal(4, count(cutActual, "POSTCOMBAT_MAIN"), "cut window's main-2 checkpoints");
        System.out.println("PASS a window cut at the extra main stops at that occurrence: "
                + cutActual.size() + " checkpoints, last " + cutActual.get(cutActual.size() - 1));

        additionalUpkeepAfterCombatOnStartTurn(dir);
    }

    /** Obeka, Splitter of Seconds: a replay from turn 1 main 1 whose combat
     * adds two upkeep steps after end of combat (CR 500.8). The queued side
     * once never counted turn 1's natural upkeep, so the extra upkeeps were
     * queued as occurrence 1 and ran in the natural upkeep, before the setup
     * snapshot. The setup snapshot must come first and every upkeep
     * checkpoint must sit after end of combat. */
    private static void additionalUpkeepAfterCombatOnStartTurn(Path dir) throws Exception {
        String line = new String(Files.readAllBytes(dir.resolve(UPKEEP_FIXTURE)), StandardCharsets.UTF_8).trim();
        JsonObject sc = JsonParser.parseString(line).getAsJsonObject();
        List<String> expected = expectedSteps(sc);
        equal("PRECOMBAT_MAIN", expected.get(0), "expected first checkpoint");
        // Four upkeep checkpoints in turn 1 (two extra upkeeps, a pass pair
        // each) plus the turn 2 natural upkeep the window ends at.
        equal(5, count(expected, "UPKEEP"), "expected upkeep checkpoints");

        JsonObject res = replay(sc);
        noHarness(res, UPKEEP_FIXTURE);
        equal(expected.size(), res.getAsJsonArray("snapshots").size(), "upkeep snapshot count");
        List<String> actual = snapshotSteps(res);
        for (int i = 0; i < expected.size(); i++) {
            equal(expected.get(i), actual.get(i), "upkeep snapshot " + i + " step");
        }
        equal("PRECOMBAT_MAIN", actual.get(0), "first snapshot");
        if (actual.indexOf("UPKEEP") < actual.indexOf("END_COMBAT")) {
            throw new AssertionError("an upkeep checkpoint precedes end of combat: " + actual);
        }
        System.out.println("PASS additional upkeeps after combat on the start turn: "
                + actual.size() + " checkpoints, setup first, " + count(actual, "UPKEEP")
                + " upkeep checkpoints after end of combat");
    }
}
