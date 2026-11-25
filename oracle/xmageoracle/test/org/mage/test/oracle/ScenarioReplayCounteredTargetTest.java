package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;

/** A cast naming the card of a spell an earlier step cast and that has since
 * been countered. The driver's cast history holds the name, but the spell left
 * the stack for the graveyard, so a spell that can only target a graveyard
 * card (Reclaim) must take the ordinary target route, not the
 * "\=" form XMage waits on forever ("Can't find ability to
 * activate command: Cast Reclaim\=Reach Through Mists\=Reach
 * Through Mists", xproof xs:XP:wild-unraveling:cast:s1). The scenario is that
 * find cut after the Reclaim cast: p1 casts Reach Through Mists, p0 counters
 * it with Wild Unraveling, p1 casts Reclaim on it.
 * Run scripts/xmage-oracle-test-countered-target.sh. */
public final class ScenarioReplayCounteredTargetTest {
    private static final String SCENARIO =
            "{'id':'xs:XP:wild-unraveling:cast:s1','card':'Wild Unraveling','template':'xproof-start','"
            + "xmage_ability':['','{T}: Add {G}.','{T}: Add {U}.','','','{T}: Add {U}.','{T}: Add {U}.','"
            + "{T}: Add {U}.','','','','','','','','','','',''],'xmage_pos':[{'turn':1,'step':'main1','ac"
            + "tive':0},null,null,null,{'turn':1,'step':'main1','active':0},null,null,null,null,{'turn':1"
            + ",'step':'main1','active':0},{'turn':1,'step':'main1','active':0,'resolves':true},{'turn':1"
            + ",'step':'main1','active':0},null,{'turn':1,'step':'main1','active':0},{'turn':1,'step':'ma"
            + "in1','active':0,'resolves':true},{'turn':1,'step':'main1','active':0},{'turn':1,'step':'be"
            + "gin-combat','active':0,'moved':true},{'turn':1,'step':'begin-combat','active':0},{'turn':1"
            + ",'step':'declare-attackers','active':0,'moved':true}],'payment':['','','','U','','','','',"
            + "'UUU','','','','G','','','','','',''],'name':'reclaim a countered spell','turn':1,'cr':[],"
            + "'setup':{'p0':{'battlefield':['Island','Island','Island','Island'],'hand':['Wild Unravelin"
            + "g'],'life':20},'p1':{'battlefield':['Forest','Island'],'hand':['Reclaim','Reach Through Mi"
            + "sts'],'life':20}},'steps':[{'op':'pass','seat':0},{'op':'activate','seat':1,'card':'p1:For"
            + "est','ability_index':0},{'op':'activate','seat':1,'card':'p1:Island','ability_index':0},{'"
            + "op':'cast','seat':1,'card':'p1:Reach Through Mists'},{'op':'pass','seat':1},{'op':'activat"
            + "e','seat':0,'card':'p0:Island','ability_index':0},{'op':'activate','seat':0,'card':'p0:Isl"
            + "and#2','ability_index':0},{'op':'activate','seat':0,'card':'p0:Island#3','ability_index':0"
            + "},{'op':'cast','seat':0,'card':'p0:Wild Unraveling','targets':['p1:Reach Through Mists'],'"
            + "answers':[{'kind':'choose','pick':['Pay 1']}]},{'op':'pass','seat':0},{'op':'pass','seat':"
            + "1},{'op':'pass','seat':0},{'op':'cast','seat':1,'card':'p1:Reclaim','targets':['p1:Reach T"
            + "hrough Mists']},{'op':'pass','seat':1},{'op':'pass','seat':0},{'op':'pass','seat':0},{'op'"
            + ":'pass','seat':1},{'op':'pass','seat':0},{'op':'pass','seat':1}]}";

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    public static void main(String[] args) {
        JsonObject sc = JsonParser.parseString(SCENARIO).getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        check(!res.has("harness") && !res.has("error"), "replay failed: " + res);
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject p1 = snaps.get(snaps.size() - 1).getAsJsonObject().getAsJsonArray("players")
                .get(1).getAsJsonObject();
        check(p1.getAsJsonArray("graveyard").toString().contains("Reclaim")
                && !p1.getAsJsonArray("graveyard").toString().contains("Reach Through Mists"),
                "Reclaim did not take the countered spell out of the graveyard: " + p1);
        check(p1.getAsJsonArray("library_top").get(0).getAsString().equals("Reach Through Mists"),
                "Reach Through Mists is not on top of the library: " + p1);
        System.out.println("PASS Reclaim on a countered spell's card puts it on top of the library ("
                + snaps.size() + " snapshots)");
    }
}
