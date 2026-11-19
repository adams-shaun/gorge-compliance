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

/** Replays the two SUP windows whose recorded cast is a convoke spell and
 * whose recorded tap pays a coloured pip. Gorge and Forge spend the plan's
 * units on the other pips and the tap on the pip of its own colour, keeping
 * the one pool unit the driver used to spend; the replay must leave the same
 * pool at the cast checkpoint. The pre-fix driver leaves U for Calamity of
 * Cinders and R for Scatter the Seeds (expected R and G). The fixtures are the
 * cut input.jsonl lines from those windows, one scenario each. Run
 * scripts/xmage-oracle-test-convoke-pip.sh; it needs the out-of-tree build's
 * card database and deck files, like a full replay. */
public final class ScenarioReplayConvokePipTest {
    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static void replay(ScenarioReplay replay, Path fixture, String card,
            String wantPool) throws Exception {
        String line = new String(Files.readAllBytes(fixture), StandardCharsets.UTF_8).trim();
        JsonObject sc = JsonParser.parseString(line).getAsJsonObject();
        JsonArray steps = sc.getAsJsonArray("steps");
        JsonObject castStep = steps.get(steps.size() - 1).getAsJsonObject();
        equal("cast", castStep.get("op").getAsString());
        equal(card, castStep.get("card").getAsString());

        StringWriter out = new StringWriter();
        int n;
        try (BufferedReader in = new BufferedReader(new StringReader(line));
             PrintWriter writer = new PrintWriter(out)) {
            n = ScenarioReplay.replayLines(replay, in, writer);
        }
        equal(1, n);
        JsonObject res = JsonParser.parseString(out.toString().trim()).getAsJsonObject();
        if (res.has("harness") && !res.get("harness").isJsonNull()) {
            throw new AssertionError(fixture.getFileName() + " harness: "
                    + res.get("harness").getAsString());
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        equal(steps.size() + 1, snaps.size());
        JsonObject cast = snaps.get(snaps.size() - 1).getAsJsonObject();
        equal("step " + (steps.size() - 1) + " (cast)",
                cast.get("checkpoint").getAsString());
        JsonArray players = cast.getAsJsonArray("players");
        equal(wantPool, players.get(0).getAsJsonObject().get("pool").getAsString());
        System.out.println("PASS " + fixture.getFileName() + " leaves " + wantPool
                + " after the recorded convoke tap pays the coloured pip");
    }

    public static void main(String[] args) throws Exception {
        Path dir = Paths.get(args.length > 0 ? args[0]
                : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures");
        ScenarioReplay replay = new ScenarioReplay();
        // Calamity of Cinders: pool UUUUURR, plan UUUUUR, tap Anax for R.
        replay(replay, dir.resolve("convoke-pip-calamity.jsonl"),
                "p0:Calamity of Cinders", "R");
        // Scatter the Seeds: pool RRRGG, plan RRRG, tap Nessian Wanderer for G.
        replay(replay, dir.resolve("convoke-pip-scatter.jsonl"),
                "p0:Scatter the Seeds", "G");
    }
}
