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

/** Executable regression for a mandatory library search with many legal
 * cards and no scripted pick: Gladiolus Amicitia's ETB ("search your library
 * for a land card, put it onto the battlefield tapped, then shuffle"). The
 * item's cut ends before the search, so no xmage_answers entry names a land;
 * XMage still poses the 1..1 TargetCardInLibrary ask. The search is in the
 * trailing play (after the last scenario checkpoint), so the driver gathers
 * the first eligible card instead of stopping the play with "Missing TARGET
 * def", and the strict run reaches every checkpoint with no trailing miss.
 *
 * The same item with one extra checkpoint appended moves the search INSIDE
 * the compared window. There the driver must not invent the card: the strict
 * run keeps its "Missing TARGET def" as a strict_miss and reruns loose. The
 * two rows pin both sides of {@link ScenarioReplay#pastLastCheckpoint()}.
 *
 * Run scripts/xmage-oracle-test-gladiolus-search.sh. */
public final class ScenarioReplayGladiolusSearchTest {
    private static final String ID = "xs:XP:gladiolus-amicitia:f0t0:s2";

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
        check(args.length == 1, "usage: ScenarioReplayGladiolusSearchTest <fixture.jsonl>");
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
        ScenarioReplay driver = new ScenarioReplay();

        JsonObject row = replay(driver, line);
        equal(ID, row.get("id").getAsString());
        check(!row.has("harness"), "the Gladiolus item failed its replay: " + row.get("harness"));
        // The pre-fix driver kept a trailing_miss (and reran loose when the
        // ask came earlier): the strict run must now finish the play.
        check(!row.has("trailing_miss"), "the search is still a trailing miss: " + row.get("trailing_miss"));
        check(!row.has("strict_miss"), "the search is still a strict miss: " + row.get("strict_miss"));
        check(row.get("strict").getAsBoolean(), "the row is not the strict run");
        equal(steps + 1, row.getAsJsonArray("snapshots").size());
        // The search is past the last checkpoint, so no snapshot compares the
        // land it gathered: assert the funnel picked the name-sorted first
        // eligible card rather than merely declining the ask.
        List<String> storedPicks = new ArrayList<>(driver.searchPicks());
        equal(java.util.Collections.singletonList("Forest"), storedPicks);

        // One resolving pass after the item's cut puts the search INSIDE the
        // compared window (the next checkpoint sees the land enter). There
        // the pick is a real gorge decision the driver cannot infer: it must
        // stay a strict miss and rerun loose, never an invented board. The
        // appended pass carries the position it reached, like the item's
        // recorded passes, and resolves the ETB trigger whose search is the
        // ask under test.
        JsonObject extra = new JsonObject();
        extra.addProperty("op", "pass");
        extra.addProperty("seat", 1);
        fixture.getAsJsonArray("steps").add(extra);
        JsonArray positions = fixture.getAsJsonArray("xmage_pos");
        while (positions.size() < steps) {
            positions.add(com.google.gson.JsonNull.INSTANCE);
        }
        JsonObject extraPosition = new JsonObject();
        extraPosition.addProperty("turn", 1);
        extraPosition.addProperty("step", "main1");
        extraPosition.addProperty("active", 0);
        extraPosition.addProperty("resolves", true);
        positions.add(extraPosition);
        JsonObject inWindow = replay(driver, fixture.toString());
        equal(ID, inWindow.get("id").getAsString());
        check(!inWindow.has("harness"), "the in-window item failed its replay: " + inWindow.get("harness"));
        check(!inWindow.get("strict").getAsBoolean(),
                "an in-window search was answered by the driver as a strict run: " + inWindow);
        check(inWindow.has("strict_miss")
                        && inWindow.get("strict_miss").getAsString().contains("Missing TARGET def"),
                "an in-window search did not keep its strict target miss: " + inWindow);
        check(!inWindow.has("trailing_miss"),
                "an in-window search was classified as trailing: " + inWindow);
        equal(steps + 2, inWindow.getAsJsonArray("snapshots").size());
        check(driver.searchPicks().isEmpty(),
                "the in-window search was guessed by the auto-pick: " + driver.searchPicks());

        System.out.println("PASS Gladiolus Amicitia's unscripted " + steps
                + "-step library search replays clean in the trailing play (picked " + storedPicks
                + "); an in-window search keeps its strict miss");
    }
}
