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

/** Executable check of the driver's activity on a mana ability that XMage
 * splits into several abilities or grants from another card.
 *
 * XMage represents Haunted Screen's "{T}, Pay 1 life: Add {G}, {U}, or {R}"
 * as three separate abilities, so the gorge ability_index does not name one;
 * the scripted colour answer must pick the Green one. After a craft the setup
 * alias still names the OLD half, so the driver must resolve the LIVE
 * transformed permanent (Sunbird Effigy, Locus of Enlightenment) and activate
 * its gained/back-face mana ability: a duplicated granted "{T}: Add {G}" is
 * the same activation whichever copy is chosen.
 *
 * Run scripts/xmage-oracle-test-mana-split.sh. */
public final class ScenarioReplayManaSplitTest {
    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static void equal(Object want, Object got) {
        if (!java.util.Objects.equals(want, got)) {
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

    private static JsonObject onePermanent(JsonObject snap, String name) {
        List<JsonObject> found = new ArrayList<>();
        for (JsonElement e : snap.getAsJsonArray("permanents")) {
            JsonObject perm = e.getAsJsonObject();
            if (perm.get("name").getAsString().equals(name)) {
                found.add(perm);
            }
        }
        equal(1, found.size());
        return found.get(0);
    }

    public static void main(String[] args) throws Exception {
        check(args.length == 1, "usage: ScenarioReplayManaSplitTest <fixture.jsonl>");
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

        // Haunted Screen: the three "{T}, Pay 1 life" abilities are split by
        // colour; the scripted Green answer selects GreenManaAbility, taps the
        // source and adds G (not one of the other two).
        JsonObject haunted = rows.get(0);
        JsonObject hauntedAfter = snapshot(haunted, 1);
        check(onePermanent(hauntedAfter, "Haunted Screen").get("tapped").getAsBoolean(),
                "Haunted Screen was not tapped");
        equal("G", player(hauntedAfter, 0).get("pool").getAsString());

        // Sunbird Standard crafted into Sunbird Effigy: the old front-half
        // alias must not shadow the live permanent, whose "{T}" mana ability
        // adds one mana of the exiled material's colour.
        JsonObject sunbird = rows.get(1);
        JsonObject sunbirdAfter = snapshot(sunbird, 3);
        check(onePermanent(sunbirdAfter, "Sunbird Effigy").get("tapped").getAsBoolean(),
                "the transformed Sunbird Effigy was not tapped");
        equal("G", player(sunbirdAfter, 0).get("pool").getAsString());

        // The Enigma Jewel crafted into Locus of Enlightenment, which gains
        // both Llanowar Elves' and Elvish Mystic's identical "{T}: Add {G}".
        JsonObject jewel = rows.get(2);
        JsonObject jewelAfter = snapshot(jewel, 3);
        check(onePermanent(jewelAfter, "Locus of Enlightenment").get("tapped").getAsBoolean(),
                "the crafted Locus of Enlightenment was not tapped");
        equal("G", player(jewelAfter, 0).get("pool").getAsString());

        System.out.println("PASS a split/gained mana ability resolves on the named source "
                + "(Haunted Screen pays 1 life for G; Sunbird Effigy and Locus of Enlightenment "
                + "are the live transformed permanents and add G)");
    }
}
