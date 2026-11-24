package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;

/** Executable check of the driver's cast-mode acceptance list and of the
 * Adventure/Omen cast path. The acceptance checks are pure (cast_mode decides
 * whether a cast step is replayed at all); the Adventure/Omen and alternative
 * cost cases replay a real one-step game, so the script gives XMage's H2 card
 * DB a private copy. "mayplay" is the cast from exile under Reckless Impulse's
 * play permission; it must be accepted, while the modes the driver still
 * cannot replay ("plot_cast", and the per-kicker "kicked1"/"kicked2"/
 * "kickedboth"/"multikicked") must stay rejected; "kicked" pays the single
 * Kicker ask (Gatekeeper of Malakir). Run scripts/xmage-oracle-test-cast-mode.sh. */
public final class ScenarioReplayCastModeTest {
    private static String castModeSupported(String mode) throws Exception {
        Class<?> replay = ScenarioReplay.class;
        java.lang.reflect.Method m = replay.getDeclaredMethod("castModeSupported", String.class);
        m.setAccessible(true);
        return String.valueOf(m.invoke(null, mode));
    }

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    /** Replays a one-step scenario that casts the spell half of parent under
     * mode, targeting a Grizzly Bears, and asserts the half -- not the parent
     * card -- is the stack's spell once the cast step has run. namesParent is
     * the farm's shape: the step's card ref names the dealt parent card
     * ("p0:Bonecrusher Giant"), which the cast arm must resolve to the half's
     * XMage name. When false the step already names the half, the shape the
     * previous test used; both must replay. */
    private static void stackHas(String parent, String half, String mode, String mana,
            boolean namesParent) throws Exception {
        String card = namesParent ? parent : half;
        JsonObject sc = JsonParser.parseString("{name:'" + half + "',card:'" + parent + "',"
                + "setup:{p0:{hand:['" + parent + "']},p1:{battlefield:['Grizzly Bears']}},"
                + "steps:[{op:'cast',seat:0,card:'p0:" + card + "',cast_mode:'" + mode + "',mana:'" + mana
                + "',targets:['p1:Grizzly Bears']}]}").getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError(half + " replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        JsonArray stack = last.getAsJsonArray("stack");
        boolean foundHalf = false;
        boolean foundParent = false;
        for (JsonElement e : stack) {
            JsonObject o = e.getAsJsonObject();
            String source = o.get("source").getAsString();
            foundHalf |= half.equals(source);
            foundParent |= parent.equals(source);
        }
        if (!foundHalf) {
            throw new AssertionError(half + " (" + mode + ") is not on the stack: " + last);
        }
        if (foundParent) {
            throw new AssertionError(parent + " was cast instead of its " + mode + " half " + half + ": " + last);
        }
        System.out.println("PASS " + mode + " casts " + half + " by its own name"
                + (namesParent ? " from a parent-card step" : " from a half-named step") + "; it is on the stack");
    }

    /** Replays a one-step warped cast of Bygone Colossus ({9}, Warp {3}) with
     * only {3} of mana: the spell is on the stack only if XMage's warp
     * SpellAbility ("<card> with Warp") was the one cast and paid. */
    private static void warpedCast() throws Exception {
        equal("true", castModeSupported("warped"), "warped cast_mode");
        JsonObject sc = JsonParser.parseString("{name:'Bygone Colossus',card:'Bygone Colossus',"
                + "setup:{p0:{hand:['Bygone Colossus']}},"
                + "steps:[{op:'cast',seat:0,card:'p0:Bygone Colossus',cast_mode:'warped',mana:'CCC'}]}")
                .getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("warped replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        boolean onStack = false;
        for (JsonElement e : last.getAsJsonArray("stack")) {
            onStack |= "Bygone Colossus".equals(e.getAsJsonObject().get("source").getAsString());
        }
        if (!onStack) {
            throw new AssertionError("warped Bygone Colossus is not on the stack: " + last);
        }
        System.out.println("PASS warped casts Bygone Colossus for its {3} warp cost; it is on the stack");
    }

    /** cast_mode "kicked": gorge paid Gatekeeper of Malakir's {B} kicker, so
     * XMage's "Pay {B} kicker cost?" ask is answered Yes with no scripted
     * answer behind it. The kicked ETB (CR 702.33d) then has the targeted
     * player sacrifice their creature; the same cast without the mode pays
     * only {B}{B} and the trigger never happens (CR 603.4 intervening if). */
    private static void kickedGatekeeper() throws Exception {
        String setup = "setup:{p0:{hand:['Gatekeeper of Malakir']},p1:{battlefield:['Grizzly Bears']}},";
        JsonObject kicked = JsonParser.parseString("{name:'kicked Gatekeeper',card:'Gatekeeper of Malakir',"
                + setup + "steps:[{op:'cast',seat:0,card:'p0:Gatekeeper of Malakir',cast_mode:'kicked',"
                + "mana:'BBB'},{op:'pass',seat:0},{op:'pass',seat:1},"
                + "{op:'resolve',seat:0,answers:[{kind:'target',pick:['p1']}]}]}").getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(kicked, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("kicked Gatekeeper replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        if (!permanentNames(last).contains("Gatekeeper of Malakir")) {
            throw new AssertionError("kicked Gatekeeper did not resolve: " + last);
        }
        if (permanentNames(last).contains("Grizzly Bears")
                || !last.getAsJsonArray("players").get(1).getAsJsonObject()
                        .getAsJsonArray("graveyard").toString().contains("Grizzly Bears")) {
            throw new AssertionError("the kicked ETB did not make p1 sacrifice Grizzly Bears: " + last);
        }
        JsonObject plain = JsonParser.parseString("{name:'plain Gatekeeper',card:'Gatekeeper of Malakir',"
                + setup + "steps:[{op:'cast',seat:0,card:'p0:Gatekeeper of Malakir',mana:'BB'},"
                + "{op:'pass',seat:0},{op:'pass',seat:1},{op:'resolve',seat:0}]}").getAsJsonObject();
        res = new ScenarioReplay().replayOnce(plain, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("plain Gatekeeper replay failed: " + res);
        }
        snaps = res.getAsJsonArray("snapshots");
        last = snaps.get(snaps.size() - 1).getAsJsonObject();
        if (!permanentNames(last).contains("Grizzly Bears")) {
            throw new AssertionError("an unkicked Gatekeeper must not trigger its sacrifice: " + last);
        }
        System.out.println("PASS kicked Gatekeeper of Malakir pays its kicker and p1 sacrifices Grizzly Bears");
    }

    /** The generator's own kicker election -- a scripted leading "yes" on the
     * cast step -- is consumed by the kicker ask, so strict mode (which fails
     * on any unconsumed or missing answer) passes and the kicked ETB runs. */
    private static void kickedGatekeeperScriptedYes() throws Exception {
        JsonObject sc = JsonParser.parseString("{name:'scripted kicked Gatekeeper',card:'Gatekeeper of Malakir',"
                + "setup:{p0:{hand:['Gatekeeper of Malakir']},p1:{battlefield:['Grizzly Bears']}},"
                + "xmage_answers:[[{seat:0,kind:'choice',value:'yes'}],null,null,"
                + "[{seat:0,kind:'target',value:'p1'}]],"
                + "steps:[{op:'cast',seat:0,card:'p0:Gatekeeper of Malakir',cast_mode:'kicked',mana:'BBB'},"
                + "{op:'pass',seat:0},{op:'pass',seat:1},{op:'resolve',seat:0}]}").getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, true);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("strict scripted-yes kicked Gatekeeper failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        if (permanentNames(snaps.get(snaps.size() - 1).getAsJsonObject()).contains("Grizzly Bears")) {
            throw new AssertionError("the scripted-yes kicked ETB did not make p1 sacrifice: " + res);
        }
        System.out.println("PASS a scripted yes is consumed by the kicker ask under strict mode");
    }

    /** gorge sends multikicked/replicated/squadded casts as the kernel's single
     * "kicked", and a two-cost kicker would be paid twice. Each must come back
     * as a loud harness error, not hang on an endlessly re-asked kicker, pay
     * both kickers, or decline Replicate and replay a false divergence. */
    private static void kickedRejectsUnmodelledShapes() throws Exception {
        String[][] cases = {
            {"Joraga Warcaller", "1G"}, // Multikicker {1}{G}
            {"Ana Battlemage", "2U"},   // two kicker costs
            {"Pyromatics", "1R"},       // Replicate, no Kicker ability
        };
        for (String[] c : cases) {
            JsonObject sc = JsonParser.parseString("{name:'kicked " + c[0] + "',card:'" + c[0] + "',"
                    + "setup:{p0:{hand:['" + c[0] + "']}},"
                    + "steps:[{op:'cast',seat:0,card:'p0:" + c[0] + "',cast_mode:'kicked',mana:'" + c[1]
                    + "'},{op:'pass',seat:0}]}").getAsJsonObject();
            String harness;
            try {
                JsonObject res = new ScenarioReplay().replayOnce(sc, false);
                harness = res.has("harness") ? res.get("harness").getAsString() : "";
            } catch (Throwable t) {
                harness = String.valueOf(t.getMessage());
            }
            if (!harness.contains("cast_mode kicked") || !harness.contains("unsupported")) {
                throw new AssertionError("kicked " + c[0] + " must be a loud harness error, got: " + harness);
            }
        }
        System.out.println("PASS kicked on a multikicker, multi-cost kicker and replicate card is rejected loudly");
    }

    private static java.util.List<String> permanentNames(JsonObject snap) {
        java.util.List<String> out = new java.util.ArrayList<>();
        for (JsonElement e : snap.getAsJsonArray("permanents")) {
            out.add(e.getAsJsonObject().get("name").getAsString());
        }
        return out;
    }

    /** A plain cast whose cast_mode is "alternative": the mode is the election,
     * so the driver picks XMage's first alternative-cost key even when the
     * generator queued no answer. Land Grant ({1}{G}) offers "reveal your
     * hand" when the hand holds no land cards; an answer from the AI's default
     * (the last key, "Cast with no alternative cost") taps the Island and Tree
     * of Tales instead. The spell must be on the stack with both lands
     * untapped, as gorge's step 7 in SUP-s167 (xe:RAND:2:0:w16) has them. */
    private static void landGrantAlternativeCost() throws Exception {
        equal("true", castModeSupported("alternative"), "alternative cast_mode");
        JsonObject sc = JsonParser.parseString("{name:'Land Grant',card:'Land Grant',"
                + "setup:{p0:{battlefield:['Island','Tree of Tales'],hand:['Land Grant'],"
                + "library_top:['Forest','Island','Forest','Island']}},"
                + "steps:[{op:'cast',seat:0,card:'p0:Land Grant',cast_mode:'alternative'}]}")
                .getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("alternative Land Grant replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        boolean onStack = false;
        for (JsonElement e : last.getAsJsonArray("stack")) {
            onStack |= "Land Grant".equals(e.getAsJsonObject().get("source").getAsString());
        }
        if (!onStack) {
            throw new AssertionError("alternative Land Grant is not on the stack: " + last);
        }
        for (JsonElement e : last.getAsJsonArray("permanents")) {
            JsonObject o = e.getAsJsonObject();
            if (o.get("controller").getAsInt() != 0) {
                continue;
            }
            String name = o.get("name").getAsString();
            if (("Island".equals(name) || "Tree of Tales".equals(name))
                    && o.has("tapped") && o.get("tapped").getAsBoolean()) {
                throw new AssertionError(name + " was tapped for the ordinary cost: " + last);
            }
        }
        System.out.println("PASS alternative Land Grant reveals the hand, leaves Island and Tree of Tales"
                + " untapped and is on the stack");
    }

    /** Mine Collapse's alternative cost is a sacrifice, not a mana cost.
     * cast_mode "alternative" elects XMage's key 1 and the step's queued
     * "@p0:Mountain" is that cost's own target. The key election must be
     * inserted ahead of the pick, not written over it: overwriting it left
     * the stale "Cast with no alternative cost" decline for the sacrifice
     * ask, which fails as a wrong choice and sacrifices nothing. */
    private static void mineCollapseAlternativeSacrifice() throws Exception {
        equal("true", castModeSupported("alternative"), "alternative cast_mode");
        JsonObject sc = JsonParser.parseString("{name:'Mine Collapse',card:'Mine Collapse',"
                + "setup:{p0:{battlefield:['Mountain','Mountain','Mountain','Mountain'],"
                + "hand:['Mine Collapse']},p1:{battlefield:['Grizzly Bears']}},"
                + "xmage_answers:[[{seat:0,kind:'choice',value:'@p0:Mountain'}]],"
                + "steps:[{op:'cast',seat:0,card:'p0:Mine Collapse',cast_mode:'alternative',"
                + "targets:['p1:Grizzly Bears']}]}").getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("alternative Mine Collapse replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        equal(1, count(last, "stack", "Mine Collapse"), "Mine Collapse is on the stack: " + last);
        JsonObject p0 = last.getAsJsonArray("players").get(0).getAsJsonObject();
        equal(1, count(p0, "graveyard", "Mountain"), "one Mountain was sacrificed: " + last);
        int mountains = 0;
        for (JsonElement e : last.getAsJsonArray("permanents")) {
            JsonObject o = e.getAsJsonObject();
            if (!"Mountain".equals(o.get("name").getAsString())) {
                continue;
            }
            mountains++;
            if (o.has("tapped") && o.get("tapped").getAsBoolean()) {
                throw new AssertionError("the alternative cost tapped a Mountain for mana: " + last);
            }
        }
        equal(3, mountains, "the other Mountains stayed on the battlefield: " + last);
        System.out.println("PASS alternative Mine Collapse sacrifices the scripted Mountain, taps no land,"
                + " and is on the stack");
    }

    private static String mayplayCastObject(String mode, String alias, String castName, String refSpelling,
            String card) throws Exception {
        java.lang.reflect.Method m = ScenarioReplay.class.getDeclaredMethod("mayplayCastObject",
                String.class, String.class, String.class, String.class, String.class);
        m.setAccessible(true);
        return String.valueOf(m.invoke(null, mode, alias, castName, refSpelling, card));
    }

    private static int count(JsonObject snap, String zone, String name) {
        int n = 0;
        for (JsonElement e : snap.getAsJsonArray(zone)) {
            n += name.equals(e.isJsonObject() ? e.getAsJsonObject().get("source").getAsString()
                    : e.getAsString()) ? 1 : 0;
        }
        return n;
    }

    /** CR 601.2a: a mayplay cast names the card by where it is. p0 holds one
     * Misthollow Griffin (hand) and has another in exile (castable from there);
     * the step names the exiled copy ("#2": hand is counted before exile). The
     * exiled copy must be the spell, and the hand copy must stay in hand. The
     * name form ("Cast Misthollow Griffin") took the first same-name match. */
    private static void mayplayPicksTheNamedCopy() throws Exception {
        equal("@p0:Ring", mayplayCastObject("mayplay", "@p0:Ring", "Ring", "Ring", "Ring"), "mayplay alias");
        equal("Ring", mayplayCastObject("mayplay", null, "Ring", "Ring", "Ring"), "mayplay, no alias bound");
        equal("Ring", mayplayCastObject("", "@p0:Ring", "Ring", "Ring", "Ring"), "ordinary cast keeps the name");
        equal("Fire", mayplayCastObject("mayplay", "@p0:Fire", "Fire", "Fire // Ice", "Fire"),
                "a split half keeps its half name");
        JsonObject sc = JsonParser.parseString("{name:'Misthollow Griffin',card:'Misthollow Griffin',"
                + "setup:{p0:{hand:['Misthollow Griffin'],exile:['Misthollow Griffin']}},"
                + "steps:[{op:'cast',seat:0,card:'p0:Misthollow Griffin#2',cast_mode:'mayplay',mana:'UUUU'}]}")
                .getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("mayplay replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        equal(1, count(last, "stack", "Misthollow Griffin"), "Griffin spells on the stack: " + last);
        JsonObject p0 = last.getAsJsonArray("players").get(0).getAsJsonObject();
        equal(1, count(p0, "hand", "Misthollow Griffin"), "hand Griffin stayed in hand: " + last);
        equal(0, count(p0, "exile", "Misthollow Griffin"), "exiled Griffin was the one cast: " + last);
        System.out.println("PASS a mayplay cast of the exiled copy leaves the same-name hand copy in hand");
    }

    /** CR 702.62a / 116.2f: Suspend from hand is a special action, not a cast.
     * gorge records it as a cast step with cast_mode "suspend"; XMage's
     * SuspendAbility pays the suspend cost ({0} for Lotus Bloom) and exiles the
     * card with three time counters, with nothing on the stack. */
    private static void suspendFromHandExilesWithTimeCounters() throws Exception {
        equal("true", castModeSupported("suspend"), "suspend cast_mode");
        JsonObject sc = JsonParser.parseString("{name:'Lotus Bloom',card:'Lotus Bloom',"
                + "setup:{p0:{hand:['Lotus Bloom']}},"
                + "steps:[{op:'cast',seat:0,card:'p0:Lotus Bloom',cast_mode:'suspend'},{op:'pass',seat:0}],"
                + "xmage_ability:['Suspend 3',''],xmage_answers:[[],[]],"
                + "xmage_pos:[null,{turn:1,step:'main1',active:0}]}").getAsJsonObject();
        ScenarioReplay replay = new ScenarioReplay();
        JsonObject res = replay.replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("suspend replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        JsonObject p0 = last.getAsJsonArray("players").get(0).getAsJsonObject();
        equal(1, count(p0, "exile", "Lotus Bloom"), "Lotus Bloom is in exile: " + last);
        equal(0, count(p0, "hand", "Lotus Bloom"), "Lotus Bloom left the hand: " + last);
        equal(0, last.getAsJsonArray("stack").size(), "suspend does not use the stack: " + last);
        equal(3, replay.exileCounters("Lotus Bloom", "time"), "time counters on the suspended card");
        System.out.println("PASS suspendFromHandExilesWithTimeCounters: Lotus Bloom is in exile with 3 time counters, stack empty");
    }

    public static void main(String[] args) throws Exception {
        suspendFromHandExilesWithTimeCounters();
        warpedCast();
        mayplayPicksTheNamedCopy();
        // Regression: the driver rejected "mayplay" by name before the replay
        // ever reached the cast, so every Reckless Impulse window was a
        // harness row. The card is in exile under a play-from-exile
        // permission; the step is still an ordinary cast on this side.
        equal("true", castModeSupported("mayplay"), "mayplay cast_mode");
        System.out.println("PASS mayplay cast_mode is accepted (cast from exile under a play permission)");

        // The modes this driver cannot replay still fail loudly rather than
        // being cast at face value. "plot_cast" is the later cast of a plotted
        // card; the per-kicker modes need XMage's asks in order and stay
        // unwired (ScenarioReplayDriverContractTest asserts the same list).
        equal("false", castModeSupported("plot_cast"), "plot_cast cast_mode");
        for (String m : new String[] {"kicked1", "kicked2", "kickedboth", "multikicked"}) {
            equal("false", castModeSupported(m), m + " cast_mode");
        }
        System.out.println("PASS plot_cast and the per-kicker modes stay rejected");
        equal("true", castModeSupported("kicked"), "kicked cast_mode");
        kickedGatekeeper();
        kickedGatekeeperScriptedYes();
        kickedRejectsUnmodelledShapes();

        // The Adventure and Omen halves are cast by the half's own spell name.
        equal("true", castModeSupported("adventure_alt"), "adventure_alt cast_mode");
        equal("true", castModeSupported("omen_alt"), "omen_alt cast_mode");
        System.out.println("PASS adventure_alt and omen_alt are accepted");

        // Real replays in the farm's shape: the step names the parent card and
        // the driver must cast the inset half, not the creature front. The
        // last case keeps the half-named shape working too.
        stackHas("Bonecrusher Giant", "Stomp", "adventure_alt", "RR", true);
        stackHas("Purging Stormbrood", "Absorb Essence", "omen_alt", "WW", true);
        stackHas("Bonecrusher Giant", "Stomp", "adventure_alt", "RR", false);

        // cast_mode "alternative" on the card's own cost: the generator queues
        // no answer, so the driver must elect XMage's first alternative itself.
        landGrantAlternativeCost();
        // ... and when the elected alternative cost has its own scripted pick
        // (Mine Collapse sacrifices a Mountain), the key election must not
        // overwrite that pick.
        mineCollapseAlternativeSacrifice();

        // Guards the list's ordinary behaviour: no mode is the plain cast.
        equal("true", castModeSupported(""), "absent cast_mode");
        System.out.println("PASS an absent cast_mode is the ordinary cast");
    }
}
