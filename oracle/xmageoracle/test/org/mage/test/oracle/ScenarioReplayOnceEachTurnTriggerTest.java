package org.mage.test.oracle;

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

/** Planetarium of Wan Shi Tong ("Whenever you scry or surveil, look at the top
 * card of your library. You may cast that card without paying its mana cost. Do
 * this only once each turn."). XMage's setDoOnlyOnceEachTurn makes the trigger
 * optional, so a resolving trigger poses TWO chooseUse asks: the trigger's own
 * "use this ability?" and the effect's "Cast spell without paying its mana
 * cost?". gorge records one decision (the cast), so the driver answers the
 * synthetic first ask itself and leaves each step's scripted Yes for the cast
 * ask. The fixture casts Opt (scry trigger, cast), flickers the Planetarium
 * with Ghostly Flicker (a new object, CR 400.7: the once-each-turn limit is
 * unused) and casts Preordain: the trigger casts the top card again, so the
 * library ends at 31, not 32. Needs the out-of-tree build and its card
 * database, like a full replay. */
public final class ScenarioReplayOnceEachTurnTriggerTest {
    private static final String FIXTURE = "planetarium-once-each-turn.jsonl";

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    private static int libraryCount(JsonObject res, String checkpoint) {
        for (JsonElement e : res.getAsJsonArray("snapshots")) {
            JsonObject snap = e.getAsJsonObject();
            if (checkpoint.equals(snap.get("checkpoint").getAsString())) {
                return snap.getAsJsonArray("players").get(0).getAsJsonObject().get("library_count").getAsInt();
            }
        }
        throw new AssertionError("no snapshot at checkpoint \"" + checkpoint + "\" in " + FIXTURE);
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
        for (String key : new String[]{"harness", "error", "strict_miss", "leftover"}) {
            if (res.has(key) && !res.get(key).isJsonNull()) {
                throw new AssertionError(FIXTURE + " " + key + ": " + res.get(key).getAsString());
            }
        }
        equal(true, res.get("strict").getAsBoolean(), "strict");
        // Opt's scry trigger casts the top Bears and Opt draws: 35 -> 33.
        equal(33, libraryCount(res, "step 1 (resolve)"), "library after Opt");
        // The flickered Planetarium casts again off Preordain's scry, and
        // Preordain draws: 33 -> 31.
        equal(31, libraryCount(res, "step 5 (resolve)"), "library after Preordain");
        System.out.println("PASS a once-each-turn trigger's synthetic use ask leaves the scripted cast answer for the effect");
    }
}
