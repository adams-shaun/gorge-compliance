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

/** Thought Collapse ("Counter target spell. Its controller mills three cards",
 * xproof xs:XP:thought-collapse:cast:s1) resolves in printed order (CR 608.2c):
 * the countered Reach Through Mists is in p1's graveyard first (CR 701.6a),
 * then the three milled cards go on top of it. The unfixed card
 * (docs/upstream-patches/xmage-thought-collapse-counter-first.patch) milled
 * before it countered, so step 10's p1.graveyard ended with Reach Through
 * Mists. Needs the out-of-tree build at an XMAGE_REF that carries the fix, and
 * its card database, like a full replay. */
public final class ScenarioReplayThoughtCollapseOrderTest {
    private static final String FIXTURE = "thought-collapse-counter-first.jsonl";
    private static final String STEP10 = "step 10 (pass)";
    private static final String COUNTERED = "[\"Reach Through Mists\",\"Plains\",\"Island\",\"Plains\"]";

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    private static JsonObject graveyard(JsonObject snap, int seat) {
        return snap.getAsJsonArray("players").get(seat).getAsJsonObject();
    }

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args.length > 0 ? args[0]
                : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures");
        String line = new String(Files.readAllBytes(dir.resolve(FIXTURE)), StandardCharsets.UTF_8).trim();
        StringWriter out = new StringWriter();
        int n;
        try (BufferedReader in = new BufferedReader(new StringReader(line));
             PrintWriter writer = new PrintWriter(out)) {
            n = ScenarioReplay.replayLines(new ScenarioReplay(), in, writer);
        }
        equal(1, n, "scenario rows");
        JsonObject res = JsonParser.parseString(out.toString().trim()).getAsJsonObject();
        for (String key : new String[]{"harness", "error", "strict_miss"}) {
            if (res.has(key) && !res.get(key).isJsonNull()) {
                throw new AssertionError(FIXTURE + " " + key + ": " + res.get(key).getAsString());
            }
        }
        equal(true, res.get("strict").getAsBoolean(), "strict");
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject step10 = null;
        for (JsonElement e : snaps) {
            JsonObject snap = e.getAsJsonObject();
            if (STEP10.equals(snap.get("checkpoint").getAsString())) {
                step10 = snap;
            }
        }
        if (step10 == null) {
            throw new AssertionError("no snapshot at checkpoint \"" + STEP10 + "\" in " + FIXTURE);
        }
        equal(COUNTERED, graveyard(step10, 1).getAsJsonArray("graveyard").toString(),
                "p1 graveyard after Thought Collapse resolves");
        equal("[\"Thought Collapse\"]", graveyard(step10, 0).getAsJsonArray("graveyard").toString(),
                "p0 graveyard after Thought Collapse resolves");
        System.out.println("PASS Thought Collapse put Reach Through Mists in the graveyard before the three milled cards");
    }
}
