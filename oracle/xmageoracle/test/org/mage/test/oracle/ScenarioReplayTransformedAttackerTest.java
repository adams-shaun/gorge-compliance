package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import mage.util.RandomUtil;

/** Replays a double-faced creature that transforms mid-scenario and then
 * attacks, end to end, no JUnit: Ulvenwald Captive and seven Forests, the
 * Captive's "{5}{G}{G}: Transform" resolved in main 1 (CR 712.8, 701.28),
 * then an attack step that names it by its scenario ref, the front face
 * "p0:Ulvenwald Captive". TestPlayer's name-only attack command, queued at
 * script time with the front name, finds no "Ulvenwald Captive" once the
 * permanent is Ulvenwald Abomination; the driver must declare the live
 * permanent the ref names (xproof xs:XP:ulvenwald-captive-ulvenwald-
 * abomination:f0a1:s1, and Tangleclaw Werewolf, Shrill Howler, Kessig
 * Prowler). Run scripts/xmage-oracle-test-transformed-attacker.sh. */
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

    public static void main(String[] args) {
        JsonObject sc = JsonParser.parseString(SCENARIO).getAsJsonObject();
        RandomUtil.setSeed(ScenarioReplay.scenarioSeed(sc));
        JsonObject res = new ScenarioReplay().replay(sc);
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
        System.out.println("ScenarioReplayTransformedAttackerTest: ok");
    }
}
