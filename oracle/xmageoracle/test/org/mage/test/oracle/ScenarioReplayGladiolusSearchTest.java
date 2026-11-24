package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.FileReader;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.util.ArrayList;
import java.util.List;

/** Executable regression for a mandatory library search with many legal
 * cards and no scripted pick: Gladiolus Amicitia's ETB ("search your library
 * for a land card, put it onto the battlefield tapped, then shuffle"). The
 * item's cut ends before the search, so no xmage_answers entry names a land;
 * XMage still poses the 1..1 TargetCardInLibrary ask. The driver gathers the
 * first eligible card instead of stopping the play with "Missing TARGET def",
 * so the strict run reaches every checkpoint and leaves no trailing miss.
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

    public static void main(String[] args) throws Exception {
        check(args.length == 1, "usage: ScenarioReplayGladiolusSearchTest <fixture.jsonl>");
        int steps;
        try (BufferedReader in = new BufferedReader(new FileReader(args[0]))) {
            JsonObject fixture = JsonParser.parseString(in.readLine()).getAsJsonObject();
            JsonArray array = fixture.getAsJsonArray("steps");
            check(array != null, "the fixture has no steps");
            steps = array.size();
        }
        List<JsonObject> rows = new ArrayList<>();
        try (BufferedReader in = new BufferedReader(new FileReader(args[0]));
             StringWriter text = new StringWriter();
             PrintWriter out = new PrintWriter(text)) {
            equal(1, ScenarioReplay.replayLines(new ScenarioReplay(), in, out));
            out.flush();
            for (String line : text.toString().split("\\R")) {
                if (!line.trim().isEmpty()) {
                    rows.add(JsonParser.parseString(line).getAsJsonObject());
                }
            }
        }
        equal(1, rows.size());
        JsonObject row = rows.get(0);
        equal(ID, row.get("id").getAsString());
        check(!row.has("harness"), "the Gladiolus item failed its replay: " + row.get("harness"));
        // The pre-fix driver kept a trailing_miss (and reran loose when the
        // ask came earlier): the strict run must now finish the play.
        check(!row.has("trailing_miss"), "the search is still a trailing miss: " + row.get("trailing_miss"));
        check(!row.has("strict_miss"), "the search is still a strict miss: " + row.get("strict_miss"));
        check(row.get("strict").getAsBoolean(), "the row is not the strict run");
        equal(steps + 1, row.getAsJsonArray("snapshots").size());
        System.out.println("PASS Gladiolus Amicitia's unscripted " + steps
                + "-step library search replays clean (strict, no trailing miss)");
    }
}
