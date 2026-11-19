package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.util.ArrayList;
import java.util.List;

/**
 * A forced resolution inside a passing seat's action must settle the
 * state-based actions (and triggers) that a real priority grant would run
 * before the checkpoint is taken. CR 704.3: "the game checks for any of the
 * listed conditions for state-based actions, then performs all applicable
 * state-based actions simultaneously as a single event"; CR 704.5m: "If an
 * Aura is attached to an illegal object or player, or is not attached to an
 * object or player, that Aura is put into its owner's graveyard."
 *
 * <p>Bleed Dry ("Target creature gets -13/-13 until end of turn. If that
 * creature would die this turn, exile it instead.") exiles the enchanted
 * Blood Burglar. The Aura then leaves in the state-based check, so the
 * resolving pass checkpoint must show it in p0's graveyard. Before the driver
 * settled the forced resolution, the checkpoint ran before the game loop's
 * next checkStateAndTriggered: the Aura was still on the battlefield with a
 * dangling attached_to, matching the SUP-s159-supp xe:RAND:4:10:w27 farm row.
 *
 * <p>Run scripts/xmage-oracle-test-resolve-settle.sh. Needs the out-of-tree
 * XMage build (the constructor opens the card DB).
 */
public final class ScenarioReplayResolveSettleTest {
    private static final String SCENARIO = """
            {
              "id": "test:bleed-dry-aura",
              "card": "Bleed Dry",
              "template": "xengine-window",
              "name": "Bleed Dry exiles an enchanted creature; the Aura settles before the checkpoint",
              "turn": 1,
              "why": "CR 704.3/704.5m regression: a resolving pass must settle state-based actions before its snapshot",
              "setup": {
                "p0": {
                  "battlefield": ["Swamp", "Swamp", "Swamp", "Swamp", "Binding Agony"],
                  "hand": ["Bleed Dry"],
                  "graveyard": [],
                  "exile": [],
                  "library_top": ["Swamp", "Swamp", "Swamp", "Swamp", "Swamp"],
                  "attached": [{"card": "Binding Agony", "to": "p1:Blood Burglar"}],
                  "life": 20
                },
                "p1": {
                  "battlefield": ["Blood Burglar"],
                  "hand": [],
                  "graveyard": [],
                  "exile": [],
                  "library_top": ["Swamp", "Swamp", "Swamp", "Swamp", "Swamp"],
                  "life": 20
                }
              },
              "steps": [
                {"op": "cast", "seat": 0, "card": "p0:Bleed Dry", "targets": ["p1:Blood Burglar"]},
                {"op": "pass", "seat": 0},
                {"op": "pass", "seat": 1}
              ],
              "xmage_answers": [null, null, null],
              "xmage_ability": ["", "", ""],
              "xmage_pos": [
                null,
                {"turn": 1, "step": "main1", "active": 0},
                {"turn": 1, "step": "main1", "active": 0, "resolves": true}
              ],
              "payment": ["BBBB", "", ""]
            }
            """;

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static List<String> names(JsonArray array) {
        List<String> out = new ArrayList<>();
        for (JsonElement e : array) {
            out.add(e.getAsString());
        }
        return out;
    }

    public static void main(String[] args) {
        JsonObject scenario = JsonParser.parseString(SCENARIO).getAsJsonObject();
        JsonObject result = new ScenarioReplay().replay(scenario);
        check(result.get("harness") == null, "harness row: " + result.get("harness"));

        JsonArray snaps = result.getAsJsonArray("snapshots");
        check(snaps != null && snaps.size() == 4, "snapshots: " + (snaps == null ? "null" : snaps.size()));
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        check(last.get("checkpoint").getAsString().equals("step 2 (pass)"),
                "last checkpoint is " + last.get("checkpoint") + ", want the resolving pass");

        JsonArray players = last.getAsJsonArray("players");
        List<String> p0Graveyard = names(players.get(0).getAsJsonObject().getAsJsonArray("graveyard"));
        check(p0Graveyard.contains("Bleed Dry"), "Bleed Dry did not resolve: " + p0Graveyard);
        check(p0Graveyard.contains("Binding Agony"),
                "CR 704.5m: the Aura is not in its owner's graveyard at the resolving pass: " + p0Graveyard);
        List<String> p1Exile = names(players.get(1).getAsJsonObject().getAsJsonArray("exile"));
        check(p1Exile.contains("Blood Burglar"), "the enchanted creature was not exiled: " + p1Exile);

        for (JsonElement e : last.getAsJsonArray("permanents")) {
            JsonObject perm = e.getAsJsonObject();
            check(!perm.get("name").getAsString().equals("Binding Agony"),
                    "the Aura is still on the battlefield at the resolving pass: " + perm);
        }

        System.out.println("PASS resolving pass settles the unattached Aura into its owner's graveyard before the checkpoint");
    }
}
