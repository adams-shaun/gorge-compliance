package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.FileReader;
import java.io.PrintWriter;
import java.io.StringReader;
import java.io.StringWriter;
import java.util.ArrayList;
import java.util.List;

/** Executable regression for a lowered xproof start whose one differing card
 * carries both spellings: xprep stamps "card" (the corpus spelling) and
 * "xmage_name" (XMage's) on the item when exactly one card XMage spells
 * differently is in it. The item here is Bartolomé del Presidio (XMage
 * "Bartolome del Presidio"): the driver must add and cast the card under the
 * XMage spelling, play the window clean, and report every snapshot under the
 * corpus spelling, so the compare sees the same name gorge does.
 *
 * Run scripts/xmage-oracle-test-xproof-spelling.sh. */
public final class ScenarioReplayXproofSpellingTest {
    private static final String ID = "xs:XP:bartolom-del-presidio:cast:s1";
    private static final String CORPUS = "Bartolomé del Presidio";
    private static final String XMAGE = "Bartolome del Presidio";

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

    /** Replay one scenario line through the real batch entry point. */
    private static JsonObject replay(ScenarioReplay driver, String line) throws Exception {
        List<JsonObject> rows = new ArrayList<>();
        try (BufferedReader in = new BufferedReader(new StringReader(line));
             StringWriter text = new StringWriter();
             PrintWriter out = new PrintWriter(text)) {
            equal(1, ScenarioReplay.replayLines(driver, in, out));
            out.flush();
            for (String rowLine : text.toString().split("\\R")) {
                if (!rowLine.trim().isEmpty()) {
                    rows.add(JsonParser.parseString(rowLine).getAsJsonObject());
                }
            }
        }
        equal(1, rows.size());
        return rows.get(0);
    }

    public static void main(String[] args) throws Exception {
        check(args.length == 1, "usage: ScenarioReplayXproofSpellingTest <fixture.jsonl>");
        String line;
        JsonObject fixture;
        int steps;
        try (BufferedReader in = new BufferedReader(new FileReader(args[0]))) {
            line = in.readLine();
            fixture = JsonParser.parseString(line).getAsJsonObject();
            JsonArray array = fixture.getAsJsonArray("steps");
            check(array != null, "the fixture has no steps");
            steps = array.size();
        }
        equal(CORPUS, fixture.get("card").getAsString());
        equal(XMAGE, fixture.get("xmage_name").getAsString());
        equal("xproof-start", fixture.get("template").getAsString());

        ScenarioReplay driver = new ScenarioReplay();
        JsonObject row = replay(driver, line);
        equal(ID, row.get("id").getAsString());
        check(!row.has("harness"), "the xproof start failed its replay: " + row.get("harness"));
        check(!row.has("strict_miss"), "the replay is a strict miss: " + row.get("strict_miss"));
        equal(steps + 1, row.getAsJsonArray("snapshots").size());
        // The cast resolved: the snapshots name the card the way the scenario
        // does, never under XMage's spelling.
        String snaps = row.getAsJsonArray("snapshots").toString();
        check(snaps.contains(CORPUS), "no snapshot names " + CORPUS + ": " + snaps);
        check(!snaps.contains(XMAGE), "a snapshot leaks XMage's spelling " + XMAGE + ": " + snaps);

        System.out.println("PASS an xproof start carrying card+xmage_name replays " + steps
                + " steps clean and reports " + CORPUS + " under the corpus spelling");
    }
}
