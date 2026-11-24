package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.util.ArrayList;
import java.util.List;

/**
 * A token ref names the token by the name it was created with, so a layer-3
 * name change must not hide it from the driver. Witness Protection gives the
 * enchanted creature a new name ("named Legitimate Businessperson") in the
 * layer system (CR 613.1c), which changes the token's live name but not which
 * object the scenario's "p0:token:Goblin Token" ref means.
 *
 * <p>Witness Protection enchants p0's Goblin token, then Obliterating Bolt
 * ("deals 4 damage to target creature or planeswalker. If that creature or
 * planeswalker would die this turn, exile it instead.") names the same ref as
 * its target. The token is exiled (and ceases to exist, CR 111.7), the Aura
 * is unattached and goes to its owner's graveyard (CR 704.5m). Before the
 * driver matched a token's printed name, the live name "Legitimate
 * Businessperson" made tokenChoice find nothing and the cast never found its
 * target, matching the FDN-s387-rand xe:RAND:2:15:w6 farm row.
 *
 * <p>Run scripts/xmage-oracle-test-renamed-token.sh. Needs the out-of-tree
 * XMage build (the constructor opens the card DB).
 */
public final class ScenarioReplayRenamedTokenTest {
    private static final String SCENARIO = """
            {
              "id": "test:witness-protection-token-bolt",
              "card": "Obliterating Bolt",
              "template": "xengine-window",
              "name": "Bolt names a Goblin token that Witness Protection renamed",
              "turn": 1,
              "why": "CR 613.1: a name-changing Aura must not hide a token from its scenario ref",
              "setup": {
                "p0": {
                  "battlefield": ["Island", "Mountain", "Mountain"],
                  "hand": ["Witness Protection", "Obliterating Bolt"],
                  "graveyard": [],
                  "exile": [],
                  "tokens": [{"script": "r_1_1_goblin"}],
                  "library_top": ["Swamp", "Swamp", "Swamp", "Swamp", "Swamp"],
                  "life": 20
                },
                "p1": {
                  "battlefield": [],
                  "hand": [],
                  "graveyard": [],
                  "exile": [],
                  "library_top": ["Swamp", "Swamp", "Swamp", "Swamp", "Swamp"],
                  "life": 20
                }
              },
              "steps": [
                {"op": "cast", "seat": 0, "card": "p0:Witness Protection", "targets": ["p0:token:Goblin Token"]},
                {"op": "pass", "seat": 0},
                {"op": "pass", "seat": 1},
                {"op": "cast", "seat": 0, "card": "p0:Obliterating Bolt", "targets": ["p0:token:Goblin Token"]},
                {"op": "pass", "seat": 0},
                {"op": "pass", "seat": 1}
              ],
              "xmage_answers": [null, null, null, null, null, null],
              "xmage_ability": ["", "", "", "", "", ""],
              "xmage_pos": [
                null,
                {"turn": 1, "step": "main1", "active": 0},
                {"turn": 1, "step": "main1", "active": 0, "resolves": true},
                null,
                {"turn": 1, "step": "main1", "active": 0},
                {"turn": 1, "step": "main1", "active": 0, "resolves": true}
              ],
              "payment": ["U", "", "", "RR", "", ""]
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
        check(snaps != null && snaps.size() == 7, "snapshots: " + (snaps == null ? "null" : snaps.size()));

        // After the Aura resolves the token is the renamed 1/1.
        JsonObject afterAura = snaps.get(3).getAsJsonObject();
        check(afterAura.get("checkpoint").getAsString().equals("step 2 (pass)"),
                "checkpoint " + afterAura.get("checkpoint"));
        boolean renamed = false;
        for (JsonElement e : afterAura.getAsJsonArray("permanents")) {
            JsonObject perm = e.getAsJsonObject();
            renamed |= perm.get("token").getAsBoolean()
                    && perm.get("name").getAsString().equals("Legitimate Businessperson");
        }
        check(renamed, "Witness Protection did not rename the token: " + afterAura.get("permanents"));

        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        check(last.get("checkpoint").getAsString().equals("step 5 (pass)"), "last checkpoint " + last.get("checkpoint"));
        JsonArray players = last.getAsJsonArray("players");
        List<String> p0Graveyard = names(players.get(0).getAsJsonObject().getAsJsonArray("graveyard"));
        check(p0Graveyard.contains("Obliterating Bolt"), "Bolt did not resolve: " + p0Graveyard);
        check(p0Graveyard.contains("Witness Protection"),
                "CR 704.5m: the Aura is not in its owner's graveyard: " + p0Graveyard);
        for (JsonElement e : last.getAsJsonArray("permanents")) {
            JsonObject perm = e.getAsJsonObject();
            check(!perm.get("token").getAsBoolean() && !perm.get("name").getAsString().equals("Witness Protection"),
                    "the renamed token or its Aura is still on the battlefield: " + perm);
        }
        System.out.println("PASS a token renamed by an Aura is still found by its scenario ref");
    }
}
