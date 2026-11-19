package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.util.ArrayList;
import java.util.List;

/**
 * A forced resolution (waitStackResolved) must settle the state-based actions
 * (and triggers) that a real priority grant would run before the checkpoint is
 * taken. CR 704.3: "the game checks for any of the listed conditions for
 * state-based actions, then performs all applicable state-based actions
 * simultaneously as a single event"; CR 704.5m: "If an Aura is attached to an
 * illegal object or player, or is not attached to an object or player, that
 * Aura is put into its owner's graveyard."
 *
 * <p>Bleed Dry ("Target creature gets -13/-13 until end of turn. If that
 * creature would die this turn, exile it instead.") exiles the enchanted
 * Blood Burglar. The Aura then leaves in the state-based check, so the
 * checkpoint after the forced resolution must show it in p0's graveyard.
 * Before the driver settled the forced resolution, the checkpoint ran before
 * the game loop's next checkStateAndTriggered: the Aura was still on the
 * battlefield with a dangling attached_to, matching the SUP-s159-supp
 * xe:RAND:4:10:w27 farm row.
 *
 * <p>Three shapes force the resolution: a pass-run (CMD_WAIT_RESOLVE_ONE),
 * the scenario's "resolve" op, and a pass_to's deferred resolution. All go
 * through the driver's overridden waitStackResolved, which arms the next
 * checkpoint; that is the one shared entry point, so every forced-resolution
 * site is covered by the same settle. The pass-run case is the failing-first
 * regression (without the override the resolving-pass checkpoint is pre-SBA);
 * the other two pin the other callers of the shared entry point.
 *
 * <p>Run scripts/xmage-oracle-test-resolve-settle.sh. Needs the out-of-tree
 * XMage build (the constructor opens the card DB).
 */
public final class ScenarioReplayResolveSettleTest {
    private static final String RESOLVING_PASS = """
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

    private static final String RESOLVE_OP = """
            {
              "id": "test:bleed-dry-aura-resolve",
              "card": "Bleed Dry",
              "template": "xengine-window",
              "name": "Bleed Dry exiles an enchanted creature; the resolve op settles the Aura",
              "turn": 1,
              "why": "CR 704.3/704.5m regression: a forced resolve op must settle state-based actions before its snapshot",
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
                {"op": "resolve", "seat": 0}
              ],
              "xmage_answers": [null, null],
              "xmage_ability": ["", ""],
              "payment": ["BBBB", ""]
            }
            """;

    private static final String PASS_TO = """
            {
              "id": "test:bleed-dry-aura-pass-to",
              "card": "Bleed Dry",
              "template": "xengine-window",
              "name": "Bleed Dry exiles an enchanted creature; the pass_to deferred resolution settles the Aura",
              "turn": 1,
              "why": "CR 704.3/704.5m regression: a pass_to deferred resolution must settle state-based actions before its snapshot",
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
                {"op": "pass_to", "seat": 0, "decision": "choose", "step": "main1"},
                {"op": "pass_to", "seat": 0, "decision": "priority", "step": "main1"}
              ],
              "xmage_answers": [null, null, null],
              "xmage_ability": ["", "", ""],
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

    /** Replay one scenario and assert the settled Aura at its last checkpoint. */
    private static void assertSettled(String source, String wantCheckpoint, int wantSnapshots) {
        JsonObject scenario = JsonParser.parseString(source).getAsJsonObject();
        JsonObject result = new ScenarioReplay().replay(scenario);
        check(result.get("harness") == null, scenario.get("id") + " harness row: " + result.get("harness"));

        JsonArray snaps = result.getAsJsonArray("snapshots");
        check(snaps != null && snaps.size() == wantSnapshots,
                scenario.get("id") + " snapshots: " + (snaps == null ? "null" : snaps.size()));
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        check(last.get("checkpoint").getAsString().equals(wantCheckpoint),
                scenario.get("id") + " last checkpoint is " + last.get("checkpoint")
                        + ", want " + wantCheckpoint);

        JsonArray players = last.getAsJsonArray("players");
        List<String> p0Graveyard = names(players.get(0).getAsJsonObject().getAsJsonArray("graveyard"));
        check(p0Graveyard.contains("Bleed Dry"), scenario.get("id") + ": Bleed Dry did not resolve: " + p0Graveyard);
        check(p0Graveyard.contains("Binding Agony"),
                "CR 704.5m: the Aura is not in its owner's graveyard at " + wantCheckpoint + ": " + p0Graveyard);
        List<String> p1Exile = names(players.get(1).getAsJsonObject().getAsJsonArray("exile"));
        check(p1Exile.contains("Blood Burglar"),
                scenario.get("id") + ": the enchanted creature was not exiled: " + p1Exile);

        for (JsonElement e : last.getAsJsonArray("permanents")) {
            JsonObject perm = e.getAsJsonObject();
            check(!perm.get("name").getAsString().equals("Binding Agony"),
                    "the Aura is still on the battlefield at " + wantCheckpoint + ": " + perm);
        }
    }

    public static void main(String[] args) {
        assertSettled(RESOLVING_PASS, "step 2 (pass)", 4);
        assertSettled(RESOLVE_OP, "step 1 (resolve)", 3);
        assertSettled(PASS_TO, "step 2 (pass_to)", 4);
        System.out.println("PASS forced resolution settles the unattached Aura into its owner's graveyard "
                + "before the checkpoint (pass-run, resolve op and pass_to)");
    }
}
