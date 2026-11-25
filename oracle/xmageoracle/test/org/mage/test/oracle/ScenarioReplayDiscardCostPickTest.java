package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.PrintWriter;
import java.io.StringReader;
import java.io.StringWriter;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;

/** A "Discard<1/Land>" activation discards the card its step scripted
 * (Borborygmos Enraged, xproof cycles 11250348 and 11250405). The replay's
 * answers share one FIFO choice queue built before any step runs, and gorge
 * scripts nothing for a one-card hand's forced discard, so the first
 * activation (the lone Mountain) took the head, a Mountain scripted for a LATER
 * step; that step's own ask then found the queue holding the wrong card (the
 * Forest went first) or empty ("Missing CHOICE def ... Select a land card").
 * The fixtures are the finds' scenarios. Run
 * scripts/xmage-oracle-test-discard-cost-pick.sh; it needs the out-of-tree
 * build's card database, like a full replay. */
public final class ScenarioReplayDiscardCostPickTest {
    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static boolean lone(boolean discardCost, int needed, int candidates) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method m = adapter.getDeclaredMethod("loneDiscardCostCandidate", boolean.class, int.class, int.class);
        m.setAccessible(true);
        return (Boolean) m.invoke(null, discardCost, needed, candidates);
    }

    private static JsonObject replay(Path fixture) throws Exception {
        String line = new String(Files.readAllBytes(fixture), StandardCharsets.UTF_8).trim();
        StringWriter out = new StringWriter();
        try (BufferedReader in = new BufferedReader(new StringReader(line));
             PrintWriter writer = new PrintWriter(out)) {
            check(ScenarioReplay.replayLines(new ScenarioReplay(), in, writer) == 1, fixture + ": one row");
        }
        JsonObject row = JsonParser.parseString(out.toString().trim()).getAsJsonObject();
        for (String key : new String[] {"strict_miss", "harness", "leftover", "trailing_miss"}) {
            check(!row.has(key), fixture.getFileName() + ": " + key + " = " + row.get(key));
        }
        return row;
    }

    private static List<String> zone(JsonObject row, String checkpoint, String zone) {
        for (JsonElement s : row.getAsJsonArray("snapshots")) {
            JsonObject snap = s.getAsJsonObject();
            if (snap.get("checkpoint").getAsString().equals(checkpoint)) {
                JsonArray cards = snap.getAsJsonArray("players").get(0).getAsJsonObject().getAsJsonArray(zone);
                List<String> names = new ArrayList<>();
                for (JsonElement c : cards) {
                    names.add(c.getAsString());
                }
                return names;
            }
        }
        throw new AssertionError("no checkpoint " + checkpoint);
    }

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args.length > 0 ? args[0]
                : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures");
        // The rule: only a discard cost with exactly one legal card still to find is forced.
        check(lone(true, 1, 1), "a discard cost's lone card is forced");
        check(!lone(true, 1, 2), "two candidates: a real pick");
        check(!lone(true, 0, 1), "nothing left to discard");
        check(!lone(false, 1, 1), "any other ask keeps its head");

        // Mountain, Forest and Mountain in hand: step 18 discards the scripted
        // Mountain (not the Forest the AI would take), step 19 the Forest.
        JsonObject forest = replay(dir.resolve("discard-cost-borborygmos-forest.jsonl"));
        check(zone(forest, "step 18 (activate)", "hand").equals(List.of("Forest", "Mountain")),
                "step 18 kept the Forest: " + zone(forest, "step 18 (activate)", "hand"));
        check(zone(forest, "step 19 (activate)", "hand").equals(List.of("Mountain")),
                "step 19 discarded the Forest: " + zone(forest, "step 19 (activate)", "hand"));
        // Two Mountains in hand: the one scripted pick reaches the second activation.
        JsonObject mountains = replay(dir.resolve("discard-cost-borborygmos-mountains.jsonl"));
        check(zone(mountains, "step 19 (activate)", "hand").equals(List.of("Mountain")),
                "step 19 discarded a Mountain: " + zone(mountains, "step 19 (activate)", "hand"));
        check(zone(mountains, "step 20 (activate)", "hand").isEmpty(),
                "step 20 discarded the last Mountain: " + zone(mountains, "step 20 (activate)", "hand"));
        System.out.println("PASS a Discard<1/Land> activation discards the scripted card");
    }
}
