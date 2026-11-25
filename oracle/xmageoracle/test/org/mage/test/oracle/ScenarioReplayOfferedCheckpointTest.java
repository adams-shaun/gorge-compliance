package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.PrintWriter;
import java.io.StringReader;
import java.io.StringWriter;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/** Pins whose options a pass checkpoint's offered set reports. After p0's
 * lone pass on an empty stack the non-active seat receives priority
 * (CR 117.3d), so the step-0 snapshot must list that seat's options -- empty
 * when it has none -- not the passer's still-listed mana ability. This is the
 * xproof a-realm-reborn / greenhouse-rickety-gazebo static0.0 shape: the setup
 * checkpoint agrees with gorge, the pass checkpoint used to repeat p0's
 * offered set. Run scripts/xmage-oracle-test-offered-checkpoint.sh. */
public final class ScenarioReplayOfferedCheckpointTest {
    private static void equal(Object want, Object got, String what) {
        if (!java.util.Objects.equals(want, got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    private static JsonObject replay(String scenario) throws Exception {
        StringWriter out = new StringWriter();
        int n;
        try (BufferedReader in = new BufferedReader(new StringReader(scenario));
             PrintWriter writer = new PrintWriter(out)) {
            n = ScenarioReplay.replayLines(new ScenarioReplay(), in, writer);
        }
        equal(1, n, "scenario rows");
        JsonObject res = JsonParser.parseString(out.toString().trim()).getAsJsonObject();
        if (res.has("harness") && !res.get("harness").isJsonNull()) {
            throw new AssertionError("harness: " + res.get("harness").getAsString());
        }
        if (res.has("error") && !res.get("error").isJsonNull()) {
            throw new AssertionError("error: " + res.get("error").getAsString());
        }
        return res;
    }

    /** The snapshot's offered set as source|kind, the compared identity
     * (labels are the comparer's job, not this pin's). */
    private static List<String> offered(JsonObject snap) {
        List<String> out = new ArrayList<>();
        JsonArray offers = snap.getAsJsonArray("offered");
        if (offers == null) {
            return out;
        }
        for (JsonElement e : offers) {
            JsonObject o = e.getAsJsonObject();
            out.add(o.get("source").getAsString() + "|" + o.get("kind").getAsString());
        }
        Collections.sort(out);
        return out;
    }

    /** The find shape: p0 has a mana source, p1 has nothing. Setup offers
     * p0's Forest; the step-0 checkpoint after p0's pass offers the non-active
     * seat's empty set. Before the fix the checkpoint still saw p0's priority
     * and repeated "p0:Forest|activate". */
    private static void passHandoffReportsNonActiveSeat() throws Exception {
        JsonObject res = replay("{"
                + "\"id\":\"offered-pass-handoff-empty\","
                + "\"name\":\"p0's lone pass reports the non-active seat's empty offered set\","
                + "\"turn\":1,"
                + "\"compare\":[\"offered\"],"
                + "\"xmage_pos\":[{\"turn\":1,\"step\":\"main1\",\"active\":0}],"
                + "\"setup\":{\"p0\":{\"battlefield\":[\"Forest\"]},\"p1\":{}},"
                + "\"steps\":[{\"op\":\"pass\",\"seat\":0}]"
                + "}");
        JsonArray snaps = res.getAsJsonArray("snapshots");
        equal(2, snaps.size(), "setup plus one step checkpoint");
        equal(Collections.singletonList("p0:Forest|activate"), offered(snaps.get(0).getAsJsonObject()),
                "setup offers p0's mana ability");
        equal(Collections.emptyList(), offered(snaps.get(1).getAsJsonObject()),
                "step 0 (pass) offers the non-active seat's empty set, not p0's Forest");
        equal(1, snaps.get(1).getAsJsonObject().get("priority").getAsInt(),
                "step 0 (pass) names the non-active seat as the priority holder");
    }

    /** The stronger direction: both seats have a mana source, so the step-0
     * offered set names p1's Forest rather than p0's. An empty set could
     * otherwise pass for a holder that simply has nothing. */
    private static void passHandoffOffersTheNewHolder() throws Exception {
        JsonObject res = replay("{"
                + "\"id\":\"offered-pass-handoff-nonactive\","
                + "\"name\":\"p0's lone pass offers p1's Forest, not p0's\","
                + "\"turn\":1,"
                + "\"compare\":[\"offered\"],"
                + "\"xmage_pos\":[{\"turn\":1,\"step\":\"main1\",\"active\":0}],"
                + "\"setup\":{\"p0\":{\"battlefield\":[\"Forest\"]},\"p1\":{\"battlefield\":[\"Forest\"]}},"
                + "\"steps\":[{\"op\":\"pass\",\"seat\":0}]"
                + "}");
        JsonArray snaps = res.getAsJsonArray("snapshots");
        equal(2, snaps.size(), "setup plus one step checkpoint");
        equal(Collections.singletonList("p0:Forest|activate"), offered(snaps.get(0).getAsJsonObject()),
                "setup offers only the active seat's Forest");
        equal(Collections.singletonList("p1:Forest|activate"), offered(snaps.get(1).getAsJsonObject()),
                "step 0 (pass) offers the new holder's Forest");
    }

    /** The hand-authored shape (no positions): a p0/p1 pass pair. p0's pass
     * hands priority to p1, so step 0 offers p1's Forest; p1's pass resolves
     * nothing but completes the round on an empty stack, so step 1's
     * checkpoint is already at the next step's p0 priority and offers p0's. */
    private static void legacyPairFirstPassHandsOff() throws Exception {
        JsonObject res = replay("{"
                + "\"id\":\"offered-legacy-pair-handoff\","
                + "\"name\":\"a legacy pair's first pass offers the other seat's Forest\","
                + "\"turn\":1,"
                + "\"compare\":[\"offered\"],"
                + "\"setup\":{\"p0\":{\"battlefield\":[\"Forest\"]},\"p1\":{\"battlefield\":[\"Forest\"]}},"
                + "\"steps\":[{\"op\":\"pass\",\"seat\":0},{\"op\":\"pass\",\"seat\":1}]"
                + "}");
        JsonArray snaps = res.getAsJsonArray("snapshots");
        equal(3, snaps.size(), "setup plus two step checkpoints");
        equal(Collections.singletonList("p0:Forest|activate"), offered(snaps.get(0).getAsJsonObject()),
                "setup offers only the active seat's Forest");
        equal(Collections.singletonList("p1:Forest|activate"), offered(snaps.get(1).getAsJsonObject()),
                "step 0 (p0's pass) offers p1's Forest");
        equal(Collections.singletonList("p0:Forest|activate"), offered(snaps.get(2).getAsJsonObject()),
                "step 1 (p1's resolving pass) is already at p0's next-step priority");
    }

    /** The xproof mm-menon / icetill-explorer shape: both seats pass through
     * beginning of combat, so the second pass enters declare attackers while p0
     * has an attackable creature. The active player declares attackers as a
     * turn-based action before anyone gets priority (CR 508.1, 117.3a), so
     * gorge's pending decision is the declaration and its checkpoint offers
     * nothing. XMage still names p0 there and used to list p0's mana ability.
     * The earlier pass checkpoints still offer the holder's options, so an
     * empty set at step 3 is the declaration, not a holder that happens to have
     * nothing. */
    private static void passIntoDeclareAttackersOffersNothing() throws Exception {
        JsonObject res = replay("{"
                + "\"id\":\"offered-declare-attackers-pending\","
                + "\"name\":\"a pass into declare attackers offers nothing before the declaration\","
                + "\"turn\":1,"
                + "\"compare\":[\"offered\"],"
                + "\"xmage_pos\":["
                + "{\"turn\":1,\"step\":\"main1\",\"active\":0},"
                + "{\"turn\":1,\"step\":\"begin-combat\",\"active\":0,\"moved\":true},"
                + "{\"turn\":1,\"step\":\"begin-combat\",\"active\":0},"
                + "{\"turn\":1,\"step\":\"declare-attackers\",\"active\":0,\"moved\":true}],"
                + "\"setup\":{\"p0\":{\"battlefield\":[\"Memnite\",\"Forest\"]},\"p1\":{}},"
                + "\"steps\":[{\"op\":\"pass\",\"seat\":0},{\"op\":\"pass\",\"seat\":1},"
                + "{\"op\":\"pass\",\"seat\":0},{\"op\":\"pass\",\"seat\":1}]"
                + "}");
        JsonArray snaps = res.getAsJsonArray("snapshots");
        equal(5, snaps.size(), "setup plus four step checkpoints");
        equal(Collections.singletonList("p0:Forest|activate"), offered(snaps.get(2).getAsJsonObject()),
                "step 1 (pass into begin combat) offers the active seat's Forest");
        equal("DECLARE_ATTACKERS", snaps.get(4).getAsJsonObject().get("step").getAsString(),
                "step 3 checkpoint is the declare-attackers step");
        equal(0, snaps.get(4).getAsJsonObject().get("priority").getAsInt(),
                "step 3 (pass into declare attackers) still names p0 as the priority holder");
        equal(Collections.emptyList(), offered(snaps.get(4).getAsJsonObject()),
                "step 3 (pass into declare attackers) offers nothing before the declaration");
    }

    /** The no-legal-attacker sibling: p0 has only a land, so gorge records the
     * forced empty declaration inside the step transition and the checkpoint is
     * a live priority decision. Its offered set is p0's own Forest, not the
     * empty set a pending declaration would publish (CR 508.1's no-option
     * path). */
    private static void passIntoDeclareAttackersWithoutAnAttackerOffersTheHoldersOptions() throws Exception {
        JsonObject res = replay("{"
                + "\"id\":\"offered-declare-attackers-no-attacker\","
                + "\"name\":\"a pass into declare attackers with no legal attacker offers the holder's options\","
                + "\"turn\":1,"
                + "\"compare\":[\"offered\"],"
                + "\"xmage_pos\":["
                + "{\"turn\":1,\"step\":\"main1\",\"active\":0},"
                + "{\"turn\":1,\"step\":\"begin-combat\",\"active\":0,\"moved\":true},"
                + "{\"turn\":1,\"step\":\"begin-combat\",\"active\":0},"
                + "{\"turn\":1,\"step\":\"declare-attackers\",\"active\":0,\"moved\":true}],"
                + "\"setup\":{\"p0\":{\"battlefield\":[\"Forest\"]},\"p1\":{}},"
                + "\"steps\":[{\"op\":\"pass\",\"seat\":0},{\"op\":\"pass\",\"seat\":1},"
                + "{\"op\":\"pass\",\"seat\":0},{\"op\":\"pass\",\"seat\":1}]"
                + "}");
        JsonArray snaps = res.getAsJsonArray("snapshots");
        equal(5, snaps.size(), "setup plus four step checkpoints");
        equal("DECLARE_ATTACKERS", snaps.get(4).getAsJsonObject().get("step").getAsString(),
                "step 3 checkpoint is the declare-attackers step");
        equal(0, snaps.get(4).getAsJsonObject().get("priority").getAsInt(),
                "step 3 (pass into declare attackers) names p0 as the priority holder");
        equal(Collections.singletonList("p0:Forest|activate"), offered(snaps.get(4).getAsJsonObject()),
                "step 3 (pass into declare attackers) keeps p0's offered set when no attacker is legal");
    }

    /** The declare-blockers twin: p0 attacks with Memnite, then both seats pass
     * into declare blockers. p1 controls no creature, so there is no legal
     * blocker and gorge resolves the forced empty declaration before its
     * checkpoint. The priority holder p0's Mountain survives; blanking on the
     * step name alone would lose it. */
    private static void passIntoDeclareBlockersWithoutABlockerOffersTheHoldersOptions() throws Exception {
        JsonObject res = replay("{"
                + "\"id\":\"offered-declare-blockers-no-blocker\","
                + "\"name\":\"a pass into declare blockers with no legal blocker offers the holder's options\","
                + "\"turn\":1,"
                + "\"compare\":[\"offered\"],"
                + "\"xmage_pos\":["
                + "{\"turn\":1,\"step\":\"main1\",\"active\":0},"
                + "{\"turn\":1,\"step\":\"begin-combat\",\"active\":0,\"moved\":true},"
                + "{\"turn\":1,\"step\":\"begin-combat\",\"active\":0},"
                + "{\"turn\":1,\"step\":\"declare-attackers\",\"active\":0,\"moved\":true},"
                + "null,"
                + "{\"turn\":1,\"step\":\"declare-attackers\",\"active\":0},"
                + "{\"turn\":1,\"step\":\"declare-blockers\",\"active\":0,\"moved\":true}],"
                + "\"setup\":{\"p0\":{\"battlefield\":[\"Memnite\",\"Mountain\"]},\"p1\":{}},"
                + "\"steps\":[{\"op\":\"pass\",\"seat\":0},{\"op\":\"pass\",\"seat\":1},"
                + "{\"op\":\"pass\",\"seat\":0},{\"op\":\"pass\",\"seat\":1},"
                + "{\"op\":\"attack\",\"seat\":0,\"attackers\":[\"p0:Memnite\"],\"defender\":\"p1\"},"
                + "{\"op\":\"pass\",\"seat\":0},{\"op\":\"pass\",\"seat\":1}]"
                + "}");
        JsonArray snaps = res.getAsJsonArray("snapshots");
        equal(8, snaps.size(), "setup plus seven step checkpoints");
        equal(true, attacking(snaps.get(5).getAsJsonObject(), "Memnite"),
                "step 4 declared Memnite as an attacker");
        equal("DECLARE_BLOCKERS", snaps.get(7).getAsJsonObject().get("step").getAsString(),
                "step 6 checkpoint is the declare-blockers step");
        equal(0, snaps.get(7).getAsJsonObject().get("priority").getAsInt(),
                "step 6 (pass into declare blockers) names p0 as the priority holder");
        equal(Collections.singletonList("p0:Mountain|activate"), offered(snaps.get(7).getAsJsonObject()),
                "step 6 (pass into declare blockers) keeps p0's offered set when no blocker is legal");
    }

    /** Whether the named permanent is attacking at this snapshot. */
    private static boolean attacking(JsonObject snap, String name) {
        for (JsonElement e : snap.getAsJsonArray("permanents")) {
            JsonObject p = e.getAsJsonObject();
            if (name.equals(p.get("name").getAsString())) {
                return p.get("attacking").getAsBoolean();
            }
        }
        throw new AssertionError("no permanent " + name + " at " + snap.get("checkpoint").getAsString());
    }

    public static void main(String[] args) throws Exception {
        passHandoffReportsNonActiveSeat();
        System.out.println("PASS a lone pass checkpoint offers the non-active seat's empty set");
        passHandoffOffersTheNewHolder();
        System.out.println("PASS a lone pass checkpoint offers the new holder's options");
        legacyPairFirstPassHandsOff();
        System.out.println("PASS a legacy pair's first pass offers the new holder's options");
        passIntoDeclareAttackersOffersNothing();
        System.out.println("PASS a pass into declare attackers offers nothing before the declaration");
        passIntoDeclareAttackersWithoutAnAttackerOffersTheHoldersOptions();
        System.out.println("PASS a pass into declare attackers without an attacker keeps the holder's options");
        passIntoDeclareBlockersWithoutABlockerOffersTheHoldersOptions();
        System.out.println("PASS a pass into declare blockers without a blocker keeps the holder's options");
    }
}
