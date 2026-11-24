package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.util.ArrayList;
import java.util.List;

/**
 * A permanent seeded by scenario setup was under its controller's control
 * before the first turn began. CR 302.6 / 603.4: Mad Dog ("At the beginning of
 * your end step, if Mad Dog didn't attack or come under your control this
 * turn, sacrifice it.") must therefore trigger at its controller's end step
 * when it never attacked. XMage models "under your control this turn" as
 * Permanent.wasControlledFromStartOfControllerTurn(), so the question this
 * pins is whether the driver's seeded permanents carry that flag.
 *
 * <p>Case 1: p0's seeded Mad Dog, p0 active, no attack: the trigger is on the
 * stack at p0's end step. Case 2: p1's seeded Mad Dog while p0 is active: its
 * trigger is "your end step", so nothing is on the stack at p0's end step.
 *
 * <p>Run scripts/xmage-oracle-test-setup-controlled-since.sh. Needs the
 * out-of-tree XMage build (the constructor opens the card DB).
 */
public final class ScenarioReplaySetupControlledSinceTest {
    private static String scenario(String id, String dogSeat) {
        String p0 = dogSeat.equals("p0") ? "[\"Mountain\", \"Mountain\", \"Mad Dog\"]" : "[\"Mountain\", \"Mountain\"]";
        String p1 = dogSeat.equals("p1") ? "[\"Mountain\", \"Mad Dog\"]" : "[\"Mountain\"]";
        return """
                {
                  "id": "%s",
                  "card": "Mad Dog",
                  "template": "xengine-window",
                  "name": "seeded Mad Dog end-step trigger",
                  "turn": 1,
                  "why": "CR 603.4: a seeded permanent was controlled since before the turn began",
                  "setup": {
                    "p0": {
                      "battlefield": %s,
                      "hand": [], "graveyard": [], "exile": [],
                      "library_top": ["Mountain", "Mountain", "Mountain", "Mountain", "Mountain"],
                      "life": 20
                    },
                    "p1": {
                      "battlefield": %s,
                      "hand": [], "graveyard": [], "exile": [],
                      "library_top": ["Mountain", "Mountain", "Mountain", "Mountain", "Mountain"],
                      "life": 20
                    }
                  },
                  "steps": [
                    {"op": "pass_to", "seat": 0, "decision": "priority", "step": "end"},
                    {"op": "pass_to", "seat": 0, "decision": "priority", "step": "end", "active": "p1"}
                  ],
                  "xmage_answers": [null, null],
                  "xmage_ability": ["", ""],
                  "payment": ["", ""]
                }
                """.formatted(id, p0, p1);
    }

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    /** The stack of snapshot {@code at} as "kind:source@controller", top first. */
    private static List<String> endStepStack(String source, int at) {
        JsonObject sc = JsonParser.parseString(source).getAsJsonObject();
        JsonObject result = new ScenarioReplay().replay(sc);
        check(result.get("harness") == null, sc.get("id") + " harness row: " + result.get("harness"));
        JsonArray snaps = result.getAsJsonArray("snapshots");
        check(snaps != null && snaps.size() > 0, sc.get("id") + " has no snapshots");
        check(snaps.size() > at, sc.get("id") + " has " + snaps.size() + " snapshots, want index " + at);
        JsonObject last = snaps.get(at).getAsJsonObject();
        List<String> out = new ArrayList<>();
        for (var e : last.getAsJsonArray("stack")) {
            JsonObject o = e.getAsJsonObject();
            out.add(o.get("kind").getAsString() + ":" + o.get("source").getAsString()
                    + "@p" + o.get("controller").getAsInt());
        }
        return out;
    }

    public static void main(String[] args) {
        List<String> own = endStepStack(scenario("test:mad-dog-own", "p0"), 1);
        check(own.equals(List.of("ability:Mad Dog@p0")),
                "CR 603.4: p0's seeded Mad Dog must trigger at p0's end step; stack was " + own);
        List<String> other = endStepStack(scenario("test:mad-dog-other", "p1"), 1);
        check(other.isEmpty(),
                "p1's Mad Dog must not trigger at p0's end step; stack was " + other);
        List<String> second = endStepStack(scenario("test:mad-dog-other-own-turn", "p1"), 2);
        check(second.equals(List.of("ability:Mad Dog@p1")),
                "CR 603.4: p1's seeded Mad Dog must trigger at p1's own end step; stack was " + second);
        System.out.println("PASS a seeded Mad Dog triggers at its own controller's end step and not at the other seat's");
    }
}
