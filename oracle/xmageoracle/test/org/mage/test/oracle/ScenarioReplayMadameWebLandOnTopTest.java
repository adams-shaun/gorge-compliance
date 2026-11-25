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

/** Madame Web, Clairvoyant ("You may cast Spider spells and noncreature spells
 * from the top of your library", xproof xs:XP:madame-web-clairvoyant:static0.1:s1)
 * does not let you play a land from the top: lands are played, not cast
 * (CR 305.1, 601.1). With an Island on top after Canopy Spider is cast, the
 * unfixed card (a plain FilterCard that matches the noncreature land) offered
 * p0:Island|play| at step 4. Needs the out-of-tree build at an XMAGE_REF that
 * carries the fix, and its card database, like a full replay. */
public final class ScenarioReplayMadameWebLandOnTopTest {
    private static final String FIXTURE = "madame-web-land-on-top.jsonl";
    private static final String STEP4 = "step 4 (pass)";

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
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
        JsonObject step4 = null;
        for (JsonElement e : res.getAsJsonArray("snapshots")) {
            JsonObject snap = e.getAsJsonObject();
            if (STEP4.equals(snap.get("checkpoint").getAsString())) {
                step4 = snap;
            }
        }
        if (step4 == null) {
            throw new AssertionError("no snapshot at checkpoint \"" + STEP4 + "\" in " + FIXTURE);
        }
        JsonArray offered = step4.has("offered") && step4.get("offered").isJsonArray()
                ? step4.getAsJsonArray("offered") : new JsonArray();
        for (JsonElement e : offered) {
            if ("play".equals(e.getAsJsonObject().get("kind").getAsString())) {
                throw new AssertionError("a land on top of the library is offered as a play at " + STEP4 + ": " + offered);
            }
        }
        System.out.println("PASS Madame Web does not offer to play the land on top of the library");
    }
}
