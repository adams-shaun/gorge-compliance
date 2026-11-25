package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.FileReader;
import java.io.PrintWriter;
import java.io.StringReader;
import java.io.StringWriter;
import java.util.ArrayList;
import java.util.List;

/** Executable regression for a snapshot that must not list a phased-out
 * permanent. Talon Gates of Madara's ETB ("up to one target creature phases
 * out") resolves against p1's Ornithopter; CR 702.26b says the phased-out
 * permanent is treated as though it does not exist, so its resolve checkpoint
 * must not list the Ornithopter, while every checkpoint before the trigger
 * resolves still does. gorge's oracle snapshot and Forge's battlefield omit
 * it too; XMage keeps it on the battlefield so phasing can return it, which is
 * why the driver's perms loop walks the active list.
 *
 * Run scripts/xmage-oracle-test-phased-out.sh. */
public final class ScenarioReplayPhasedOutTest {
    private static final String ID = "xs:XP:talon-gates-of-madara:f0a2:s1";
    private static final String PHASED = "Ornithopter";
    private static final String SOURCE = "Talon Gates of Madara";

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

    private static List<String> permanentNames(JsonObject snapshot) {
        List<String> names = new ArrayList<>();
        for (JsonElement e : snapshot.getAsJsonArray("permanents")) {
            names.add(e.getAsJsonObject().get("name").getAsString());
        }
        return names;
    }

    public static void main(String[] args) throws Exception {
        check(args.length == 1, "usage: ScenarioReplayPhasedOutTest <fixture.jsonl>");
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
        equal(ID, fixture.get("id").getAsString());
        equal(SOURCE, fixture.get("card").getAsString());
        equal("xproof-start", fixture.get("template").getAsString());
        equal(7, steps);

        ScenarioReplay driver = new ScenarioReplay();
        JsonObject row = replay(driver, line);
        equal(ID, row.get("id").getAsString());
        check(!row.has("harness"), "the xproof start failed its replay: " + row.get("harness"));
        check(!row.has("strict_miss"), "the replay is a strict miss: " + row.get("strict_miss"));
        JsonArray snaps = row.getAsJsonArray("snapshots");
        equal(steps + 1, snaps.size());

        // The Ornithopter is on the battlefield in every checkpoint before the
        // trigger resolves, and in none at or after it. The resolve checkpoint
        // still names Talon Gates, so the board is not simply empty.
        boolean sawOrnithopter = false;
        JsonObject resolve = null;
        for (JsonElement e : snaps) {
            JsonObject snap = e.getAsJsonObject();
            String checkpoint = snap.get("checkpoint").getAsString();
            List<String> names = permanentNames(snap);
            if (names.contains(PHASED)) {
                sawOrnithopter = true;
                check(!checkpoint.contains("resolve"),
                        "the " + checkpoint + " snapshot lists the phased-out " + PHASED + ": " + names);
            }
            if (checkpoint.contains("resolve")) {
                resolve = snap;
            }
        }
        check(sawOrnithopter, "precondition: " + PHASED + " never appears, so the fixture did not sit on the battlefield");
        check(resolve != null, "no resolve checkpoint in " + snaps);
        List<String> resolveNames = permanentNames(resolve);
        check(!resolveNames.contains(PHASED),
                "the resolve snapshot lists the phased-out " + PHASED + ": " + resolveNames);
        check(resolveNames.contains(SOURCE),
                "precondition: " + SOURCE + " is not on the resolve battlefield: " + resolveNames);
        System.out.println("PASS " + SOURCE + "'s ETB phases out " + PHASED
                + " and the resolve checkpoint omits it (" + resolveNames + ")");
    }
}
