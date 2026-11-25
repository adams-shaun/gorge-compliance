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
import java.util.List;

/** Replays the live Whip Vine untap cut (cycle 11251221's xproof find
 * xpt-4782660c, find xs:XP:whip-vine:f0a0:s1): a pass ends turn 1 with Whip
 * Vine's optional untap choice (CR 502.3) pending in turn 2's untap step, so
 * gorge's cut position is (2, untap). phaseOfGorgeStep had no untap case, so
 * the follower threw "pass position in unknown step untap" before replaying
 * anything. The untap step grants no priority (CR 500.4), so the cut's
 * checkpoint, queued at (turn, UNTAP), can only fire from the ask itself:
 * chooseUse's untap-election branch flushes the queued runCode actions,
 * answers leave-tapped (gorge left the choice pending, the permanent still
 * tapped) and pauses the game, the row complete. The fixture's second and
 * third rows bind the election (gorge recorded the ask's answer as its own
 * "Untap" / "Keep tapped" label): the branch consumes the label, answers it
 * and the game rolls on into turn 2's first main phase, so the cut stands
 * there with the vine untapped (tapped, for Keep tapped).
 * Run scripts/xmage-oracle-test-untap-election.sh; it needs the out-of-tree
 * build's card database and deck files, like a full replay. */
public final class ScenarioReplayUntapElectionTest {
    private static final String FIXTURE = "whip-vine-untap-cut.jsonl";

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
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

    private static boolean whipVineTapped(JsonObject cut, boolean want) {
        boolean tapped = false;
        for (JsonElement e : cut.getAsJsonArray("permanents")) {
            JsonObject p = e.getAsJsonObject();
            if ("Whip Vine".equals(p.get("name").getAsString())) {
                equal(1, p.get("controller").getAsInt(), "Whip Vine controller");
                tapped = p.get("tapped").getAsBoolean();
                List<String> keywords = new java.util.ArrayList<>();
                for (JsonElement k : p.getAsJsonArray("keywords")) {
                    keywords.add(k.getAsString());
                }
                if (!keywords.contains("defender") || !keywords.contains("reach")) {
                    throw new AssertionError("Whip Vine keywords: " + keywords);
                }
            }
        }
        return tapped == want;
    }

    public static void main(String[] args) throws Exception {
        equal("UNTAP", ScenarioReplay.phaseOfGorgeStep("untap").name(), "untap step mapping");
        equal(true, ScenarioReplay.isUntapElectionAsk("Untap Whip Vine?"), "untap ask shape");
        equal(false, ScenarioReplay.isUntapElectionAsk(null), null);

        Path dir = Paths.get(args.length > 0 ? args[0]
                : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures");
        List<JsonObject> rows = new java.util.ArrayList<>();
        for (String line : Files.readAllLines(dir.resolve(FIXTURE), StandardCharsets.UTF_8)) {
            if (!line.trim().isEmpty()) {
                rows.add(JsonParser.parseString(line).getAsJsonObject());
            }
        }
        equal(3, rows.size(), "fixture rows");

        // Row 0, the live find: gorge left the choice pending, so the
        // unbound branch answers leave-tapped and takes the cut at the ask.
        JsonObject row = replay(rows.get(0));
        if (row.has("harness") && !row.get("harness").isJsonNull()) {
            throw new AssertionError("harness row: " + row.get("harness"));
        }
        JsonArray snaps = row.getAsJsonArray("snapshots");
        equal(23, snaps.size(), "snapshots (setup + steps 0..21)");
        JsonObject cut = snaps.get(22).getAsJsonObject();
        equal("step 21 (pass)", cut.get("checkpoint").getAsString(), "cut checkpoint");
        equal(2, cut.get("turn").getAsInt(), "cut turn");
        equal("UNTAP", cut.get("step").getAsString(), "cut step");
        equal(1, cut.get("active").getAsInt(), "cut active seat");
        equal(0, cut.get("priority").getAsInt(), "cut priority seat");
        equal(false, cut.get("over").getAsBoolean(), "cut over");
        if (!whipVineTapped(cut, true)) {
            throw new AssertionError("Whip Vine must stay tapped at the pending choice (CR 502.3)");
        }

        // Rows 1 and 2: the election bound. gorge's game rolled through the
        // answered election into turn 2's first main phase, so the pass
        // ending turn 1 checkpoints there. The branch consumes the "Untap" /
        // "Keep tapped" label, answers it and the game continues, so the cut
        // stands at turn 2 main with the vine untapped (tapped, for Keep
        // tapped).
        for (int i = 1; i <= 2; i++) {
            String label = i == 1 ? "Untap" : "Keep tapped";
            boolean wantTapped = i == 2;
            row = replay(rows.get(i));
            if (row.has("harness") && !row.get("harness").isJsonNull()) {
                throw new AssertionError("bound " + label + " harness row: " + row.get("harness"));
            }
            snaps = row.getAsJsonArray("snapshots");
            equal(23, snaps.size(), "bound " + label + " snapshots (setup + steps 0..21)");
            cut = snaps.get(22).getAsJsonObject();
            equal("step 21 (pass)", cut.get("checkpoint").getAsString(), "bound " + label + " cut checkpoint");
            equal(2, cut.get("turn").getAsInt(), "bound " + label + " cut turn");
            equal("PRECOMBAT_MAIN", cut.get("step").getAsString(), "bound " + label + " cut step");
            equal(1, cut.get("active").getAsInt(), "bound " + label + " cut active seat");
            equal(1, cut.get("priority").getAsInt(), "bound " + label + " cut priority seat");
            equal(false, cut.get("over").getAsBoolean(), "bound " + label + " cut over");
            if (!whipVineTapped(cut, wantTapped)) {
                throw new AssertionError("bound " + label + ": Whip Vine tapped state wrong at the cut");
            }
        }
        System.out.println("PASS Whip Vine untap cut: pending choice cut at turn 2 untap (Whip Vine tapped);"
                + " bound Untap/Keep tapped elections consumed, the cut a pass later at turn 2 main (untapped / tapped)");
    }
}
