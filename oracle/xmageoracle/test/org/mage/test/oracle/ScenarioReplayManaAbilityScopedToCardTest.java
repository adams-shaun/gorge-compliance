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

/** Executable check of the driver's card-scoped mana-ability activation.
 *
 * TestPlayer's manaActivate: command takes the first available mana producer
 * whose rule text starts with the command, so a bare "{T}" (Hidden Grotto)
 * lands on any untapped land. The fixture windows in test/fixtures/
 * mana-scope.jsonl replay through the production ScenarioReplay and pin the
 * named permanent: Hidden Grotto is tapped and the Island is not, pool C;
 * with two Swamps, Swamp#2 is tapped and Swamp#1 is not; and a Plains#2
 * played from hand resolves by its setup alias although it is the only
 * Plains on the battlefield.
 *
 * Run scripts/xmage-oracle-test-mana-scope.sh. */
public final class ScenarioReplayManaAbilityScopedToCardTest {
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

    private static JsonObject player(JsonObject snap, int seat) {
        return snap.getAsJsonArray("players").get(seat).getAsJsonObject();
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

    private static JsonObject one(JsonObject snap, String name) {
        List<JsonObject> found = permanents(snap, name);
        equal(1, found.size());
        return found.get(0);
    }

    private static boolean tapped(JsonObject perm) {
        return perm.get("tapped").getAsBoolean();
    }

    private static int tappedCount(JsonObject snap) {
        int n = 0;
        for (JsonElement e : snap.getAsJsonArray("permanents")) {
            if (tapped(e.getAsJsonObject())) {
                n++;
            }
        }
        return n;
    }

    public static void main(String[] args) throws Exception {
        check(args.length == 1, "usage: ScenarioReplayManaAbilityScopedToCardTest <fixture.jsonl>");
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
            equal(3, n);
        }
        equal(3, rows.size());
        for (JsonObject row : rows) {
            check(!row.has("harness"), row.get("id").getAsString() + " failed its replay: " + row.get("harness"));
        }

        // Hidden Grotto prints {T}: Add {C}; the untapped Island also starts
        // with "{T}". Activating Grotto ability 0 must tap Grotto, leave the
        // Island untapped, and add C -- the old global scan tapped the Island
        // and added U.
        JsonObject grottoRow = rows.get(0);
        equal("manascope-grotto", grottoRow.get("id").getAsString());
        JsonObject before = snapshot(grottoRow, 0);
        check(!tapped(one(before, "Hidden Grotto")), "precondition: Hidden Grotto starts untapped");
        check(!tapped(one(before, "Island")), "precondition: Island starts untapped");
        equal("", player(before, 0).get("pool").getAsString());

        JsonObject after = snapshot(grottoRow, 1);
        check(tapped(one(after, "Hidden Grotto")), "Hidden Grotto was not tapped");
        check(!tapped(one(after, "Island")), "the Island was tapped instead of Hidden Grotto");
        equal(1, tappedCount(after));
        equal("C", player(after, 0).get("pool").getAsString());

        // Two Swamps: the step names p0:Swamp#2, so only the second one may
        // be tapped. The snapshot lists permanents in battlefield insertion
        // order, which is the setup order build() used.
        JsonObject swampRow = rows.get(1);
        equal("manascope-swamp2", swampRow.get("id").getAsString());
        List<JsonObject> swampsBefore = permanents(snapshot(swampRow, 0), "Swamp");
        equal(2, swampsBefore.size());
        check(!tapped(swampsBefore.get(0)) && !tapped(swampsBefore.get(1)),
                "precondition: both Swamps start untapped");

        JsonObject swampAfter = snapshot(swampRow, 1);
        List<JsonObject> swamps = permanents(swampAfter, "Swamp");
        equal(2, swamps.size());
        check(!tapped(swamps.get(0)), "Swamp#1 was tapped instead of Swamp#2");
        check(tapped(swamps.get(1)), "Swamp#2 was not tapped");
        equal(1, tappedCount(swampAfter));
        equal("B", player(swampAfter, 0).get("pool").getAsString());

        // A second Plains played from hand keeps the setup ref "Plains#2",
        // but it is the only Plains on the battlefield. Its identity alias
        // resolves it; a battlefield-ordinal-only lookup would demand a
        // second Plains and fail the window.
        JsonObject landRow = rows.get(2);
        equal("manascope-played-plains2", landRow.get("id").getAsString());
        equal(1, permanents(snapshot(landRow, 1), "Plains").size());
        JsonObject landAfter = snapshot(landRow, 2);
        List<JsonObject> plains = permanents(landAfter, "Plains");
        equal(1, plains.size());
        check(tapped(plains.get(0)), "the played Plains was not tapped");
        equal(1, tappedCount(landAfter));
        equal("W", player(landAfter, 0).get("pool").getAsString());

        System.out.println("PASS mana ability is scoped to the named permanent "
                + "(Hidden Grotto not Island, pool C; Swamp#2 not Swamp#1, pool B; "
                + "a played Plains#2 is still the named permanent)");
    }
}
