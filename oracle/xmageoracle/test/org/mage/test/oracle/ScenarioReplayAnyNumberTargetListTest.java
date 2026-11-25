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

/** Riverchurn Monument's "{1}, {T}: Any number of target players each mill
 * two cards" (DFT level-B row activate#0.0, CR 602.2/601.2c): the scenario
 * targets p1 only. XMage's TargetPlayer(0, MAX) re-asks after the scripted
 * pick with p0 as the ask's lone remaining candidate, and the driver's
 * unscripted lone-candidate rule must not take it: gorge poses ONE decision
 * for the whole selection, so the re-ask closes with what the script chose
 * (closeScriptedTargetList). P1 mills two, p0 mills none. Needs the
 * out-of-tree build and its card database, like a full replay. */
public final class ScenarioReplayAnyNumberTargetListTest {
    private static final String FIXTURE = "any-number-target-list-riverchurn.jsonl";

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
        for (String key : new String[]{"harness", "error", "strict_miss", "trailing_miss"}) {
            if (res.has(key) && !res.get(key).isJsonNull()) {
                throw new AssertionError(FIXTURE + " " + key + ": " + res.get(key).getAsString());
            }
        }
        equal(true, res.get("strict").getAsBoolean(), "strict");
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        equal("[\"Wastes\",\"Wastes\"]",
                last.getAsJsonArray("players").get(1).getAsJsonObject().getAsJsonArray("graveyard").toString(),
                "p1 graveyard (the scripted target mills two)");
        equal("[]",
                last.getAsJsonArray("players").get(0).getAsJsonObject().getAsJsonArray("graveyard").toString(),
                "p0 graveyard (the controller is not an unscripted extra target)");
        System.out.println("PASS Riverchurn Monument milled only the scripted target (any-number list closed)");
    }
}
