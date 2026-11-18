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

/** Executable check of the driver's card-scoped Plot special action (CR 702.170a).
 *
 * Aloe Alchemist and Beastbond Outcaster both print "Plot {1}{G}", so the
 * bare rule text "Plot {1}{G}" matches both hand cards and TestPlayer takes
 * the first it lists. The fixture windows in test/fixtures/plot-scope.jsonl
 * hold both cards in p0's hand and replay through the production
 * ScenarioReplay with a plot step naming one card each: the named card must
 * be in exile and the other still in hand, whichever XMage lists first.
 *
 * Run scripts/xmage-oracle-test-plot-scope.sh. */
public final class ScenarioReplayPlotScopedToCardTest {
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

    private static List<String> zone(JsonObject snap, String name) {
        List<String> out = new ArrayList<>();
        for (JsonElement e : snap.getAsJsonArray("players").get(0).getAsJsonObject().getAsJsonArray(name)) {
            out.add(e.getAsString());
        }
        return out;
    }

    private static void plots(JsonObject row, String id, String plotted, String kept) {
        equal(id, row.get("id").getAsString());
        JsonObject before = snapshot(row, 0);
        check(zone(before, "hand").contains(plotted) && zone(before, "hand").contains(kept),
                "precondition: both Plot cards start in hand");
        equal(0, zone(before, "exile").size());

        JsonObject after = snapshot(row, 1);
        List<String> exile = zone(after, "exile");
        List<String> hand = zone(after, "hand");
        check(exile.contains(plotted), plotted + " was not plotted (exile " + exile + ", hand " + hand + ")");
        check(!exile.contains(kept), kept + " was plotted instead of " + plotted + " (exile " + exile + ")");
        check(hand.contains(kept), kept + " left the hand (hand " + hand + ")");
        check(!hand.contains(plotted), plotted + " is still in hand (hand " + hand + ")");
        equal(1, exile.size());
    }

    public static void main(String[] args) throws Exception {
        check(args.length == 1, "usage: ScenarioReplayPlotScopedToCardTest <fixture.jsonl>");
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
        for (JsonObject row : rows) {
            check(!row.has("harness"), row.get("id").getAsString() + " failed its replay: " + row.get("harness"));
        }

        // One row names each card, so the check holds whichever of the two
        // XMage lists first: an unscoped "Plot {1}{G}" fails one of them.
        plots(rows.get(0), "plotscope-second", "Beastbond Outcaster", "Aloe Alchemist");
        plots(rows.get(1), "plotscope-first", "Aloe Alchemist", "Beastbond Outcaster");

        System.out.println("PASS plot is scoped to the named card "
                + "(Beastbond Outcaster or Aloe Alchemist plotted alone, the same-cost twin stays in hand)");
    }
}
