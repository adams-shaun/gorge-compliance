package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import mage.util.RandomUtil;

/** Replays a double-faced creature that transforms mid-scenario and then
 * attacks, end to end, no JUnit.
 *
 * <p>The first case was placed on the battlefield by setup: Ulvenwald Captive
 * and seven Forests, the Captive's "{5}{G}{G}: Transform" resolved in main 1
 * (CR 712.8, 701.28), then an attack step that names it by its scenario ref,
 * the front face "p0:Ulvenwald Captive". TestPlayer's name-only attack
 * command, queued at script time with the front name, finds no "Ulvenwald
 * Captive" once the permanent is Ulvenwald Abomination; the driver must
 * declare the live permanent the ref names (xproof xs:XP:ulvenwald-captive-
 * ulvenwald-abomination:f0a1:s1, and Tangleclaw Werewolf, Shrill Howler,
 * Kessig Prowler).
 *
 * <p>The second case CASTS the card from hand, so its setup object is the
 * hand card while the permanent XMage makes is the front half: Cloistered
 * Youth with two Plains, cast in main 1, the upkeep trigger answered yes on
 * turn 3 (it becomes Unholy Fiend), then "p0:Cloistered Youth" attacks. The
 * alias bound at setup names the main card's id, which is neither the cast
 * spell's id nor the permanent's id, so the combat ref must resolve through
 * the permanent's {@code getMainCard()} identity (xproof
 * xs:XP:cloistered-youth-unholy-fiend:f0t0:s1).
 *
 * <p>Run scripts/xmage-oracle-test-transformed-attacker.sh. */
public final class ScenarioReplayTransformedAttackerTest {

    private static final String SCENARIO = "{"
            + "id:'test:transformed-attacker',name:'transformed attacker',turn:1,"
            + "setup:{"
            + "p0:{battlefield:['Ulvenwald Captive','Forest','Forest','Forest','Forest','Forest','Forest','Forest'],"
            + "library_top:['Forest','Forest','Forest','Forest','Forest','Forest','Forest','Forest','Forest','Forest'],life:20},"
            + "p1:{library_top:['Plains','Plains','Plains','Plains','Plains','Plains','Plains','Plains','Plains','Plains'],life:20}},"
            + "steps:["
            + "{op:'activate',seat:0,card:'p0:Forest',ability_index:0},"
            + "{op:'activate',seat:0,card:'p0:Forest#2',ability_index:0},"
            + "{op:'activate',seat:0,card:'p0:Forest#3',ability_index:0},"
            + "{op:'activate',seat:0,card:'p0:Forest#4',ability_index:0},"
            + "{op:'activate',seat:0,card:'p0:Forest#5',ability_index:0},"
            + "{op:'activate',seat:0,card:'p0:Forest#6',ability_index:0},"
            + "{op:'activate',seat:0,card:'p0:Forest#7',ability_index:0},"
            + "{op:'activate',seat:0,card:'p0:Ulvenwald Captive',ability_index:1},"
            + "{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:0},{op:'pass',seat:1},"
            + "{op:'pass',seat:0},{op:'pass',seat:1},"
            + "{op:'attack',seat:0,attackers:['p0:Ulvenwald Captive'],defender:'p1'}],"
            + "xmage_ability:['{T}: Add {G}.','{T}: Add {G}.','{T}: Add {G}.','{T}: Add {G}.','{T}: Add {G}.',"
            + "'{T}: Add {G}.','{T}: Add {G}.','{5}{G}{G}','','','','','','',''],"
            + "xmage_answers:[null,null,null,null,null,null,null,null,null,null,null,null,null,null,null],"
            + "xmage_pos:[null,null,null,null,null,null,null,null,"
            + "{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0,resolves:true},"
            + "{turn:1,step:'main1',active:0},{turn:1,step:'begin-combat',active:0,moved:true},"
            + "{turn:1,step:'begin-combat',active:0},{turn:1,step:'declare-attackers',active:0,moved:true},null],"
            + "payment:['','','','','','','','GGGGGGG','','','','','','','']}";

    /** Cloistered Youth cast from hand, transformed by its upkeep trigger.
     * The recorded prefix ends at the attack step; the passes carry gorge's
     * positions so the replay reaches turn 3's upkeep before the trigger. */
    private static final String CAST_DFC_SCENARIO = """
            {
              "id": "test:cast-transformed-attacker",
              "name": "transformed attacker cast from hand",
              "turn": 1,
              "setup": {"p0":{"battlefield":["Plains","Plains"],"hand":["Cloistered Youth"],"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20},"p1":{"library_top":["Plains","Plains","Plains","Plains","Plains","Plains","Plains","Plains"],"life":20}},
              "steps": [
                {"op":"activate","seat":0,"card":"p0:Plains","ability_index":0},
                {"op":"activate","seat":0,"card":"p0:Plains#2","ability_index":0},
                {"op":"cast","seat":0,"card":"p0:Cloistered Youth"},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"play","seat":1,"card":"p1:Plains"},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0,"answers":[{"kind":"trigger_optional","pick":["yes"]}]},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"play","seat":0,"card":"p0:Plains#3"},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"pass","seat":0},
                {"op":"pass","seat":1},
                {"op":"attack","seat":0,"attackers":["p0:Cloistered Youth"],"defender":"p1"}
              ],
              "xmage_answers": [null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,[{"seat":0,"kind":"choice","value":"yes"}]],
              "xmage_ability": ["{T}: Add {W}.","{T}: Add {W}.",""],
              "xmage_pos": [
                null,
                null,
                null,
                {"turn":1,"step":"main1","active":0},
                {"turn":1,"step":"main1","active":0,"resolves":true},
                {"turn":1,"step":"main1","active":0},
                {"turn":1,"step":"begin-combat","active":0,"moved":true},
                {"turn":1,"step":"begin-combat","active":0},
                {"turn":1,"step":"declare-attackers","active":0,"moved":true},
                {"turn":1,"step":"declare-attackers","active":0},
                {"turn":1,"step":"end-combat","active":0,"moved":true},
                {"turn":1,"step":"end-combat","active":0},
                {"turn":1,"step":"main2","active":0,"moved":true},
                {"turn":1,"step":"main2","active":0},
                {"turn":1,"step":"end","active":0,"moved":true},
                {"turn":1,"step":"end","active":0},
                {"turn":2,"step":"upkeep","active":1,"moved":true},
                {"turn":2,"step":"upkeep","active":1},
                {"turn":2,"step":"draw","active":1,"moved":true},
                {"turn":2,"step":"draw","active":1},
                {"turn":2,"step":"main1","active":1,"moved":true},
                null,
                {"turn":2,"step":"main1","active":1},
                {"turn":2,"step":"begin-combat","active":1,"moved":true},
                {"turn":2,"step":"begin-combat","active":1},
                {"turn":2,"step":"declare-attackers","active":1,"moved":true},
                {"turn":2,"step":"declare-attackers","active":1},
                {"turn":2,"step":"end-combat","active":1,"moved":true},
                {"turn":2,"step":"end-combat","active":1},
                {"turn":2,"step":"main2","active":1,"moved":true},
                {"turn":2,"step":"main2","active":1},
                {"turn":2,"step":"end","active":1,"moved":true},
                {"turn":2,"step":"end","active":1},
                {"turn":3,"step":"upkeep","active":0,"moved":true},
                {"turn":3,"step":"upkeep","active":0},
                {"turn":3,"step":"upkeep","active":0,"resolves":true},
                {"turn":3,"step":"upkeep","active":0},
                {"turn":3,"step":"draw","active":0,"moved":true},
                {"turn":3,"step":"draw","active":0},
                {"turn":3,"step":"main1","active":0,"moved":true},
                null,
                {"turn":3,"step":"main1","active":0},
                {"turn":3,"step":"begin-combat","active":0,"moved":true},
                {"turn":3,"step":"begin-combat","active":0},
                {"turn":3,"step":"declare-attackers","active":0,"moved":true},
                null
              ],
              "payment": ["","","WW"]
            }
            """;

    public static void main(String[] args) {
        runSetupPlacedCase();
        runCastFromHandCase();
        System.out.println("ScenarioReplayTransformedAttackerTest: ok");
    }

    private static JsonObject replay(String scenario) {
        JsonObject sc = JsonParser.parseString(scenario).getAsJsonObject();
        RandomUtil.setSeed(ScenarioReplay.scenarioSeed(sc));
        return new ScenarioReplay().replay(sc);
    }

    /** #501's case: the DFC was placed by setup, so its alias id is the
     * permanent's own id. */
    private static void runSetupPlacedCase() {
        JsonObject res = replay(SCENARIO);
        if (res.has("harness")) {
            throw new AssertionError("transformed attacker: " + res.get("harness").getAsString());
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        int attack = 15; // snapshot 0 is the setup, so step 14's is 15
        if (snaps == null || snaps.size() <= attack) {
            throw new AssertionError("no snapshot after the attack step: " + (snaps == null ? 0 : snaps.size()));
        }
        boolean declared = false;
        for (JsonElement e : snaps.get(attack).getAsJsonObject().getAsJsonArray("permanents")) {
            JsonObject perm = e.getAsJsonObject();
            String name = perm.get("name").getAsString();
            if (name.equals("Ulvenwald Captive")) {
                throw new AssertionError("Ulvenwald Captive did not transform before combat");
            }
            if (name.equals("Ulvenwald Abomination") && perm.get("attacking").getAsBoolean()) {
                declared = true;
            }
        }
        if (!declared) {
            throw new AssertionError("Ulvenwald Abomination was not declared as the attacker: "
                    + snaps.get(attack));
        }
        System.out.println("PASS setup-placed DFC declares its bound permanent exactly after transforming");
    }

    /** A DFC cast from hand: the setup alias names the main card, while the
     * permanent XMage makes is the front half. It has transformed by combat,
     * so the front-face attack command must still declare Unholy Fiend. */
    private static void runCastFromHandCase() {
        JsonObject res = replay(CAST_DFC_SCENARIO);
        if (res.has("harness")) {
            throw new AssertionError("cast transformed attacker: " + res.get("harness").getAsString());
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        int attack = 46; // snapshot 0 is the setup; the attack is step 45
        if (snaps == null || snaps.size() <= attack) {
            throw new AssertionError("no snapshot after the attack step: " + (snaps == null ? 0 : snaps.size()));
        }
        boolean declared = false;
        for (JsonElement e : snaps.get(attack).getAsJsonObject().getAsJsonArray("permanents")) {
            JsonObject perm = e.getAsJsonObject();
            String name = perm.get("name").getAsString();
            if (name.equals("Cloistered Youth")) {
                throw new AssertionError("Cloistered Youth did not transform before combat");
            }
            if (name.equals("Unholy Fiend") && perm.get("attacking").getAsBoolean()) {
                declared = true;
            }
        }
        if (!declared) {
            throw new AssertionError("Unholy Fiend was not declared as the attacker: "
                    + snaps.get(attack));
        }
        System.out.println("PASS cast-from-hand DFC resolves through its main-card id and attacks as Unholy Fiend");
    }
}
