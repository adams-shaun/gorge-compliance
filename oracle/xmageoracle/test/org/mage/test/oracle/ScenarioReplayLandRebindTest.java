package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.FileReader;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.util.ArrayList;
import java.util.List;

/** Executable check that a land ref names the permanent gorge means after the
 * land changed zone mid-window.
 *
 * XMage answers an effect's by-name pick (a hand land put onto the battlefield,
 * a land returned to hand) with any same-named card, so it can move the sibling
 * of the card gorge moved. The land play that follows then enters one card
 * while the ref's alias still names the permanent that was already on the
 * battlefield, and the later tap of the ref found it tapped (harness row
 * "has no mana ability"). test/fixtures/land-rebind.jsonl forces the mis-pick
 * with an "@ref" answer so the failure does not depend on XMage's hash order.
 *
 * Run scripts/xmage-oracle-test-land-rebind.sh. */
public final class ScenarioReplayLandRebindTest {
    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static JsonObject snapshot(JsonObject row, int at) {
        JsonArray snaps = row.getAsJsonArray("snapshots");
        check(snaps != null && snaps.size() > at, row.get("id").getAsString() + " has no snapshot " + at);
        return snaps.get(at).getAsJsonObject();
    }

    private static List<JsonObject> permanents(JsonObject snap, String name) {
        List<JsonObject> out = new ArrayList<>();
        for (JsonElement e : snap.getAsJsonArray("permanents")) {
            JsonObject perm = e.getAsJsonObject();
            if (perm.get("name").getAsString().equals(name)) {
                out.add(perm);
            }
        }
        return out;
    }

    private static int tappedCount(List<JsonObject> perms) {
        int n = 0;
        for (JsonObject perm : perms) {
            if (perm.get("tapped").getAsBoolean()) {
                n++;
            }
        }
        return n;
    }

    private static JsonObject row(List<JsonObject> rows, String id) {
        for (JsonObject row : rows) {
            if (row.get("id").getAsString().equals(id)) {
                return row;
            }
        }
        throw new AssertionError("no row " + id);
    }

    /** A setup Forest returned to hand (the mis-pick bounces Forest#2, not the
     * Forest the later step plays), played again and tapped by its ref. */
    static void bouncedAndReplayedLandTapsByRef(List<JsonObject> rows) {
        JsonObject r = row(rows, "landrebind-bounced-replayed");
        check(!r.has("harness"), "bounced-replayed failed its replay: " + r.get("harness"));
        JsonObject played = snapshot(r, 8);
        equal("step 7 (play)", played.get("checkpoint").getAsString());
        equal(2, permanents(played, "Forest").size());
        equal(1, tappedCount(permanents(played, "Forest")));
        JsonObject tapped = snapshot(r, 9);
        equal("step 8 (activate)", tapped.get("checkpoint").getAsString());
        equal(2, permanents(tapped, "Forest").size());
        equal(2, tappedCount(permanents(tapped, "Forest")));
    }

    /** An effect puts a drawn Forest onto the battlefield tapped and the mis-pick
     * moves the card Forest#6 names; the played Forest#6 is still the
     * permanent a later tap reaches. */
    static void effectPlacedLandKeepsSiblingRef(List<JsonObject> rows) {
        JsonObject r = row(rows, "landrebind-effect-placed");
        check(!r.has("harness"), "effect-placed failed its replay: " + r.get("harness"));
        JsonObject played = snapshot(r, 9);
        equal("step 8 (play)", played.get("checkpoint").getAsString());
        equal(6, permanents(played, "Forest").size());
        equal(5, tappedCount(permanents(played, "Forest")));
        JsonObject tapped = snapshot(r, 14);
        equal("step 13 (activate)", tapped.get("checkpoint").getAsString());
        equal(6, permanents(tapped, "Forest").size());
        equal(6, tappedCount(permanents(tapped, "Forest")));
    }

    public static void main(String[] args) throws Exception {
        check(args.length == 1, "usage: ScenarioReplayLandRebindTest <fixture.jsonl>");
        List<JsonObject> rows = new ArrayList<>();
        try (BufferedReader in = new BufferedReader(new FileReader(args[0]));
             StringWriter text = new StringWriter();
             PrintWriter out = new PrintWriter(text)) {
            int n = ScenarioReplay.replayLines(new ScenarioReplay(), in, out);
            out.flush();
            for (String line : text.toString().split("\\R")) {
                if (!line.trim().isEmpty()) {
                    rows.add(JsonParser.parseString(line).getAsJsonObject());
                }
            }
            equal(2, n);
        }
        equal(2, rows.size());
        bouncedAndReplayedLandTapsByRef(rows);
        effectPlacedLandKeepsSiblingRef(rows);
        System.out.println("PASS a land that changed zone mid-window is tapped by its ref "
                + "(bounced and replayed Forest; effect-placed sibling of the played Forest#6)");
    }
}
