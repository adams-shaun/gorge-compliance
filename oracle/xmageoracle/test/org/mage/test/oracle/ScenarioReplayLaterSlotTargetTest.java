package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;

/** A lone cast target that belongs to a LATER "up to one" object of the spell.
 * Relive the Past returns up to one artifact, up to one land and up to one
 * non-Aura enchantment card from the graveyard (CR 601.2c: each is chosen
 * separately, so choosing only the land is legal). With only a land in the
 * graveyard gorge asks for the land alone and the cast step names that one
 * ref. The inline "$target=" string form puts a lone ref on the FIRST object
 * (the artifact), which rejects a Forest, and XMage reports "Can't find
 * ability to activate command" (xproof xs:XP:relive-the-past:cast:s2). Both
 * routes must return the Forest as a 5/5 creature: the driver's own routing of
 * a lone target among several optional objects, and the lowering's explicit
 * skip plan (a skip before the land, one after it).
 * Run scripts/xmage-oracle-test-later-slot-target.sh. */
public final class ScenarioReplayLaterSlotTargetTest {
    private static final String SETUP = "setup:{p0:{hand:['Relive the Past'],graveyard:['Forest']}},";
    private static final String STEPS = "steps:[{op:'cast',seat:0,card:'p0:Relive the Past',mana:'WGGGGGG',"
            + "targets:['p0:Forest']},{op:'pass',seat:0},{op:'pass',seat:1}]";

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    /** Replays the scenario and asserts the Forest came back as a 5/5. */
    private static void returnsForest(String label, String extra) {
        JsonObject sc = JsonParser.parseString("{name:'" + label + "',card:'Relive the Past'," + extra
                + SETUP + STEPS + "}").getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        check(!res.has("harness") && !res.has("error"), label + " replay failed: " + res);
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        JsonObject forest = null;
        for (JsonElement e : last.getAsJsonArray("permanents")) {
            JsonObject o = e.getAsJsonObject();
            if ("Forest".equals(o.get("name").getAsString())) {
                forest = o;
            }
        }
        check(forest != null, label + ": the Forest did not return to the battlefield: " + last);
        check("5/5".equals(forest.get("pt").getAsString()), label + ": the Forest is not a 5/5: " + forest);
        check(forest.getAsJsonArray("types").toString().contains("Creature"),
                label + ": the Forest is not a creature: " + forest);
        System.out.println("PASS " + label + ": the graveyard Forest returns as a 5/5 Elemental creature");
    }

    /** The routing predicate: a spell with several optional objects queues a
     * lone target; one object, a required object or no spell keeps the
     * inline form. */
    private static void routing() {
        mage.abilities.Ability relive = mage.cards.repository.CardRepository.instance
                .findCard("Relive the Past").createCard().getSpellAbility();
        check(ScenarioReplay.loneTargetNeedsQueue(relive), "Relive the Past's three up-to-one objects must queue a lone target");
        mage.abilities.Ability bolt = mage.cards.repository.CardRepository.instance
                .findCard("Lightning Bolt").createCard().getSpellAbility();
        check(!ScenarioReplay.loneTargetNeedsQueue(bolt), "a one-object spell must keep the inline form");
        check(!ScenarioReplay.loneTargetNeedsQueue(null), "no spell ability must keep the inline form");
        System.out.println("PASS lone-target routing (several optional objects queue; one object and none do not)");
    }

    public static void main(String[] args) {
        routing();
        // The driver's own routing: no plan on the item.
        returnsForest("later-slot lone target, no plan", "");
        // The lowering's plan: close the artifact object, then the enchantment one.
        returnsForest("later-slot lone target, skip plan",
                "xmage_target_skips:[[{at:0,slot:0},{at:1,slot:2}],null,null],");
    }
}
