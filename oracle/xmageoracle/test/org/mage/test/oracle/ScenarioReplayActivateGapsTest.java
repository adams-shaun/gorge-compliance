package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import mage.util.RandomUtil;

/** Replays the activate-gaps shapes end to end, no JUnit: a cast whose
 * recorded pool-spend plan cannot name the generic units, casts whose plan
 * must be preferred over a floating unit of another colour (including a
 * colorless unit), and the Saddle/Crew cost chooser with exactly one legal
 * creature (the shape xprep now scripts).
 * Run scripts/xmage-oracle-test-activate-gaps.sh. */
public final class ScenarioReplayActivateGapsTest {

    /** Deal each scenario the same RNG seed replayLines gives it, then fail
     * on the harness row TestPlayer writes instead of an exception. */
    private static JsonObject passes(String name, String json) {
        JsonObject sc = JsonParser.parseString(json).getAsJsonObject();
        RandomUtil.setSeed(ScenarioReplay.scenarioSeed(sc));
        JsonObject res = new ScenarioReplay().replay(sc);
        if (res.has("harness")) {
            throw new AssertionError(name + ": " + res.get("harness").getAsString());
        }
        return res;
    }

    /** Seat 0's mana pool in the snapshot at `at` (0 is the setup snapshot). */
    private static String poolAt(JsonObject res, int at) {
        JsonArray snaps = res.getAsJsonArray("snapshots");
        if (snaps == null || snaps.size() <= at) {
            throw new AssertionError("no snapshot " + at);
        }
        return snaps.get(at).getAsJsonObject().getAsJsonArray("players")
                .get(0).getAsJsonObject().get("pool").getAsString();
    }

    /** gorge spent WUUU from a pool that holds UUWW: after the {U} pip the
     * three generic units are payable by ANY colours, but the plan's last U
     * names no pool unit. The payment window must fall through to the pool
     * instead of handing the cast to auto-payment (which has no untapped
     * source left) and failing "Can't find ability". */
    private static void manualPaymentGenericFallback() {
        passes("manual payment generic fallback", "{"
                + "id:'test:activate-gaps-manual-payment',"
                + "name:'activate gaps manual payment',turn:1,"
                + "setup:{"
                + "p0:{battlefield:['Island','Island','Plains','Plains'],"
                + "hand:['Loan Shark'],"
                + "library_top:['Island','Island','Island','Island','Island','Island','Island','Island','Island','Island'],life:20},"
                + "p1:{library_top:['Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain'],life:20}},"
                + "steps:["
                + "{op:'activate',seat:0,card:'p0:Island',ability_index:0},"
                + "{op:'activate',seat:0,card:'p0:Island#2',ability_index:0},"
                + "{op:'activate',seat:0,card:'p0:Plains',ability_index:0},"
                + "{op:'activate',seat:0,card:'p0:Plains#2',ability_index:0},"
                + "{op:'cast',seat:0,card:'p0:Loan Shark'}],"
                + "xmage_ability:['{T}: Add {U}.','{T}: Add {U}.','{T}: Add {W}.','{T}: Add {W}.',''],"
                + "xmage_answers:[null,null,null,null,null],"
                + "xmage_pos:[{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0},"
                + "{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0}],"
                + "payment:['','','','','WUUU']}");
    }

    /** The plan is a PREFERENCE, not a restriction: with a floating W outside
     * gorge's recorded GG, the generic part must take the plan's second G, not
     * the cheaper-to-index W. Plains/Forest/Forest tap for WGG; Grizzly Bears
     * ({1}{G}) with payment GG leaves W, exactly what gorge's post-cast pool
     * holds. The old generic loop walked WUBRGC and spent W for {1}, leaving G,
     * a false snapshot divergence. */
    private static void manualPaymentKeepsFloatingColour() {
        JsonObject res = passes("manual payment keeps floating colour", "{"
                + "id:'test:activate-gaps-plan-preference',"
                + "name:'activate gaps plan preference',turn:1,"
                + "setup:{"
                + "p0:{battlefield:['Plains','Forest','Forest'],"
                + "hand:['Grizzly Bears'],"
                + "library_top:['Island','Island','Island','Island','Island','Island','Island','Island','Island','Island'],life:20},"
                + "p1:{library_top:['Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain'],life:20}},"
                + "steps:["
                + "{op:'activate',seat:0,card:'p0:Plains',ability_index:0},"
                + "{op:'activate',seat:0,card:'p0:Forest',ability_index:0},"
                + "{op:'activate',seat:0,card:'p0:Forest#2',ability_index:0},"
                + "{op:'cast',seat:0,card:'p0:Grizzly Bears'}],"
                + "xmage_ability:['{T}: Add {W}.','{T}: Add {G}.','{T}: Add {G}.',''],"
                + "xmage_answers:[null,null,null,null],"
                + "xmage_pos:[{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0},"
                + "{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0}],"
                + "payment:['','','','GG']}");
        String before = poolAt(res, 3);
        if (!before.equals("WGG")) {
            throw new AssertionError("precondition: pool before the cast must be WGG, got " + before);
        }
        String after = poolAt(res, 4);
        if (!after.equals("W")) {
            throw new AssertionError("expected the plan's GG to be spent, leaving W, got " + after);
        }
    }

    /** COLORLESS is XMage's generic bucket, not a specific pip: with a Wastes
     * floating beside the plan's W, the generic part must still take W (the
     * recorded unit) and leave C. The old loop's {C} arm spent C first because
     * it ran before the generic phase, leaving W -- the same class of false
     * snapshot divergence as the floating-colour case, for the colorless kind
     * of pool unit. */
    private static void manualPaymentColorlessFollowsPlan() {
        JsonObject res = passes("manual payment colorless follows plan", "{"
                + "id:'test:activate-gaps-colorless-plan-preference',"
                + "name:'activate gaps colorless plan preference',turn:1,"
                + "setup:{"
                + "p0:{battlefield:['Plains','Wastes','Forest'],"
                + "hand:['Grizzly Bears'],"
                + "library_top:['Island','Island','Island','Island','Island','Island','Island','Island','Island','Island'],life:20},"
                + "p1:{library_top:['Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain','Mountain'],life:20}},"
                + "steps:["
                + "{op:'activate',seat:0,card:'p0:Plains',ability_index:0},"
                + "{op:'activate',seat:0,card:'p0:Wastes',ability_index:0},"
                + "{op:'activate',seat:0,card:'p0:Forest',ability_index:0},"
                + "{op:'cast',seat:0,card:'p0:Grizzly Bears'}],"
                + "xmage_ability:['{T}: Add {W}.','{T}: Add {C}.','{T}: Add {G}.',''],"
                + "xmage_answers:[null,null,null,null],"
                + "xmage_pos:[{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0},"
                + "{turn:1,step:'main1',active:0},{turn:1,step:'main1',active:0}],"
                + "payment:['','','','WG']}");
        String before = poolAt(res, 3);
        if (!before.equals("WGC")) {
            throw new AssertionError("precondition: pool before the cast must be WGC, got " + before);
        }
        String after = poolAt(res, 4);
        if (!after.equals("C")) {
            throw new AssertionError("expected the plan's W to be spent, leaving C, got " + after);
        }
    }

    /** Saddle 2 with one other untapped creature: the Tap cost's chooser has a
     * single legal object, but XMage still poses it, so the answer must be on
     * the choice queue (xprep's single-option cost pick used to be dropped). */
    private static void saddleSingleOptionCost() {
        passes("saddle single-option cost", "{"
                + "id:'test:activate-gaps-saddle',name:'activate gaps saddle',turn:1,"
                + "setup:{"
                + "p0:{battlefield:['Quilled Charger','Grizzly Bears'],"
                + "library_top:['Forest','Forest','Forest','Forest','Forest'],life:20},"
                + "p1:{library_top:['Mountain','Mountain','Mountain','Mountain','Mountain'],life:20}},"
                + "steps:[{op:'activate',seat:0,card:'p0:Quilled Charger',ability_index:0}],"
                + "xmage_ability:['Saddle 2'],"
                + "xmage_answers:[[{seat:0,kind:'choice',value:'Grizzly Bears'}]],"
                + "xmage_pos:[{turn:1,step:'main1',active:0}],"
                + "payment:['']}");
    }

    /** Crew 2 with one other untapped creature: the same single-object Tap
     * cost chooser through the Vehicle's Crew ability. */
    private static void crewSingleOptionCost() {
        passes("crew single-option cost", "{"
                + "id:'test:activate-gaps-crew',name:'activate gaps crew',turn:1,"
                + "setup:{"
                + "p0:{battlefield:['Mobile Homestead','Grizzly Bears'],"
                + "library_top:['Forest','Forest','Forest','Forest','Forest'],life:20},"
                + "p1:{library_top:['Mountain','Mountain','Mountain','Mountain','Mountain'],life:20}},"
                + "steps:[{op:'activate',seat:0,card:'p0:Mobile Homestead',ability_index:0}],"
                + "xmage_ability:['Crew 2'],"
                + "xmage_answers:[[{seat:0,kind:'choice',value:'Grizzly Bears'}]],"
                + "xmage_pos:[{turn:1,step:'main1',active:0}],"
                + "payment:['']}");
    }

    public static void main(String[] args) {
        manualPaymentGenericFallback();
        manualPaymentKeepsFloatingColour();
        manualPaymentColorlessFollowsPlan();
        saddleSingleOptionCost();
        crewSingleOptionCost();
        System.out.println("PASS manual-payment generic fallback and plan preference "
                + "(coloured and colorless units); Saddle/Crew single-option cost choices");
    }
}
