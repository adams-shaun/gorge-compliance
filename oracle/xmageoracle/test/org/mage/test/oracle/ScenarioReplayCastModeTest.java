package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;

/** Executable check of the driver's cast-mode acceptance list and of the
 * Adventure/Omen, Room and modal-DFC cast paths. The acceptance checks are
 * pure (cast_mode decides whether a cast step is replayed at all); the
 * Adventure/Omen, Room, modal and alternative cost cases replay a real
 * one-step game, so the script gives XMage's H2 card DB a private copy.
 * "mayplay" is the cast from exile under Reckless Impulse's play permission;
 * it must be accepted, while the modes the driver still cannot replay
 * ("plot_cast", and the per-kicker "kicked1"/"kicked2"/"kickedboth"/
 * "multikicked") must stay rejected; "kicked" pays the single Kicker ask
 * (Gatekeeper of Malakir). Run scripts/xmage-oracle-test-cast-mode.sh. */
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

    /** Azula, Cunning Usurper: target opponent exiles a nontoken creature they
     * control, then during the controller's turn the controller may cast it
     * (CR 601.2a: a card cast under a permission is cast by the permission's
     * controller). The card is owned by p1, so @p1:Shield Sphere is the ref the
     * cast names; the cast has to be in TestPlayer's playable set, which needs
     * the static permission's source zcc (Azula's permanent, not the pre-game
     * 0 XMage registered the ability with). The fixture is the xproof find:
     * p0 casts Azula on turn 1 with p1 holding only Shield Sphere, p1's ETB
     * choose exiles it, and p0 then casts it for {0}. */
    private static void mayplayOpponentOwnedExile(String fixtures) throws Exception {
        Path fixture = Paths.get(fixtures).resolve("azula-opponent-exile.jsonl");
        String line = Files.readString(fixture, StandardCharsets.UTF_8).trim();
        JsonObject sc = JsonParser.parseString(line).getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("Azula mayplay replay failed: " + res);
        }
        // The fixture's step 7 carries only seat 0's target ("target [p1]").
        // gorge's opponent (p1) choose is recorded in the scenario but is not
        // lowered into the seat-1 queue, so XMage answers the ETB's PlayerB
        // pick itself and the scripted PlayerA target stays queued to the end
        // of the replay. Pin that exact decision shape: a lowering that
        // stopped emitting the step-7 target would drop this leftover (or
        // move it to another queue) and fail here, not just in the final
        // board. oraclediff.Compare ignores leftover, so the cluster would
        // otherwise still clear after such a regression.
        equal("AssertionError: (Targets of PlayerA) Count are not equal (found [targetPlayer=PlayerB])."
                + " Make sure you use target.chooseXXX instead player.choose. Also some inner choose dialogs"
                + " can be set up only in strict mode. expected:<0> but was:<1>",
                res.has("leftover") ? res.get("leftover").getAsString() : "",
                "the step-7 target shape (target p1 with p1's choose unscripted)");
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject afterEtb = null;
        for (JsonElement e : snaps) {
            JsonObject snap = e.getAsJsonObject();
            if (snap.get("checkpoint").getAsString().startsWith("step 7 ")) {
                afterEtb = snap;
                break;
            }
        }
        if (afterEtb == null || !afterEtb.getAsJsonArray("players").get(1).getAsJsonObject()
                .getAsJsonArray("exile").toString().contains("Shield Sphere")) {
            throw new AssertionError("step 7 did not exile Shield Sphere, so the cast has nothing to find: "
                    + afterEtb);
        }
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        boolean underP0 = false;
        boolean ownerP1 = false;
        for (JsonElement e : last.getAsJsonArray("permanents")) {
            JsonObject p = e.getAsJsonObject();
            if ("Shield Sphere".equals(p.get("name").getAsString())) {
                underP0 |= p.get("controller").getAsInt() == 0;
                ownerP1 |= p.get("owner").getAsInt() == 1;
            }
        }
        if (!underP0 || !ownerP1) {
            throw new AssertionError("p1's exiled Shield Sphere is not on the battlefield under p0's control: "
                    + last);
        }
        System.out.println("PASS mayplayOpponentOwnedExile: p0 casts p1's exiled Shield Sphere"
                + " under Azula's permission");
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

    /** CR 702.143a / 116.2h: Foretell from hand is a special action, not a
     * cast. gorge records it as a cast step with cast_mode "foretell"; XMage's
     * ForetellAbility pays {2} (the GG payment is two Forests) and exiles the
     * card face down, with nothing on the stack. */
    private static void foretellFromHandExilesFaceDown() throws Exception {
        equal("true", castModeSupported("foretell"), "foretell cast_mode");
        JsonObject sc = JsonParser.parseString("{name:'Struggle for Skemfar',card:'Struggle for Skemfar',"
                + "setup:{p0:{hand:['Struggle for Skemfar'],battlefield:['Forest','Forest','Forest','Forest']}},"
                + "steps:[{op:'cast',seat:0,card:'p0:Struggle for Skemfar',cast_mode:'foretell',mana:'GG'},{op:'pass',seat:0}],"
                + "xmage_ability:['Foretell {G}',''],xmage_answers:[[],[]],"
                + "xmage_pos:[null,{turn:1,step:'main1',active:0}]}").getAsJsonObject();
        ScenarioReplay replay = new ScenarioReplay();
        JsonObject res = replay.replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("foretell replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        JsonObject p0 = last.getAsJsonArray("players").get(0).getAsJsonObject();
        equal(1, count(p0, "exile", "Struggle for Skemfar"), "Struggle for Skemfar is in exile: " + last);
        equal(0, count(p0, "hand", "Struggle for Skemfar"), "Struggle for Skemfar left the hand: " + last);
        equal(0, last.getAsJsonArray("stack").size(), "foretell does not use the stack: " + last);
        System.out.println("PASS foretellFromHandExilesFaceDown: Struggle for Skemfar is in exile foretold, stack empty");
    }

    /** CR 702.74a: Evoke is an alternative cost. gorge records the cast as
     * cast_mode "evoked" (xs:XP:emptiness:f0t0:s1: two Plains tapped, Emptiness
     * evoked for {W/B}{W/B} through two "Pay W" picks); the driver used to throw
     * "cast_mode evoked unsupported". The cast must elect XMage's EvokeAbility
     * entry of the alternative-cost menu, leaving Emptiness on the stack with
     * the two untapped Plains alone untouched and no harness row. */
    private static void evokedCastPaysEvokeCost() throws Exception {
        equal("true", castModeSupported("evoked"), "evoked cast_mode");
        // The election itself, on AbilityImpl's menu as XMage words it
        // (AlternativeSourceCostsImpl.getAlternativeCostText): Evoke is key 1,
        // the plain cast key 2. Without a match nothing is queued and the
        // non-strict replay below leaves the pick to the AI, so it passes
        // only when the AI happens to evoke.
        mage.choices.ChoiceImpl menu = new mage.choices.ChoiceImpl(false);
        menu.setMessage("You may choose an alternative cost");
        java.util.Map<String, String> keys = new java.util.LinkedHashMap<>();
        keys.put("1", "Cast with Evoke alternative cost: {W/B}{W/B} (Emptiness)");
        keys.put("2", "Cast with no alternative cost: {4}{W/B}{W/B}");
        menu.setKeyChoices(keys);
        java.util.List<String> queued = new java.util.ArrayList<>(java.util.Arrays.asList("Pay W", "Pay W"));
        java.lang.reflect.Method elect = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer")
                .getDeclaredMethod("evokeCostKey", mage.choices.Choice.class, java.util.List.class);
        elect.setAccessible(true);
        equal(true, elect.invoke(null, menu, queued), "evokeCostKey elects on XMage's Evoke menu");
        equal(java.util.Arrays.asList("1", "Pay W", "Pay W"), queued, "evoke key queued ahead of the payment picks");
        JsonObject sc = JsonParser.parseString("{id:'xs:XP:emptiness:f0t0:s1',name:'Emptiness',card:'Emptiness',"
                + "setup:{p0:{battlefield:['Plains','Plains','Plains','Plains'],hand:['Emptiness'],"
                + "library_top:['Plains','Plains','Plains','Plains']},p1:{battlefield:['Phyrexian Walker']}},"
                + "steps:[{op:'activate',seat:0,card:'p0:Plains',ability_index:0},"
                + "{op:'activate',seat:0,card:'p0:Plains#2',ability_index:0},"
                + "{op:'cast',seat:0,card:'p0:Emptiness',cast_mode:'evoked',"
                + "answers:[{kind:'choose',pick:['Pay W']},{kind:'choose',pick:['Pay W']}]}],"
                + "xmage_ability:['{T}: Add {W}.','{T}: Add {W}.',''],xmage_answers:[[],[],[]],"
                + "xmage_pos:[null,null,{turn:1,step:'main1',active:0}]}").getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("evoked Emptiness replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        boolean onStack = false;
        for (JsonElement e : last.getAsJsonArray("stack")) {
            onStack |= "Emptiness".equals(e.getAsJsonObject().get("source").getAsString());
        }
        if (!onStack) {
            throw new AssertionError("evoked Emptiness is not on the stack: " + last);
        }
        int untapped = 0;
        for (JsonElement e : last.getAsJsonArray("permanents")) {
            JsonObject o = e.getAsJsonObject();
            if (o.get("controller").getAsInt() == 0 && "Plains".equals(o.get("name").getAsString())
                    && !(o.has("tapped") && o.get("tapped").getAsBoolean())) {
                untapped++;
            }
        }
        equal(2, untapped, "the evoke cost was paid from the two tapped Plains' mana, not the others: " + last);
        System.out.println("PASS evokedCastPaysEvokeCost: Emptiness is on the stack for {W/B}{W/B}, two Plains untapped");
    }

    /** CR 709.3/709.3a: a Room is a split card cast as one of its doors.
     * cast_mode "room_alt" is the second door; the farm's step names the
     * parent card ("p0:Central Elevator"), as it does for an Adventure half,
     * and three Islands pay Promising Stairs's {2}{U}. The front door
     * ({3}{U}) is unaffordable, so a driver that casts the whole card or the
     * front half fails here. The resolved Room must enter with the back door
     * unlocked (CR 709.5 names it for that door). */
    private static void roomAltCastsSecondDoor() throws Exception {
        equal("true", castModeSupported("room_alt"), "room_alt cast_mode");
        JsonObject sc = JsonParser.parseString("{name:'Central Elevator',card:'Central Elevator',"
                + "xmage_name:'Central Elevator // Promising Stairs',"
                + "setup:{p0:{battlefield:['Island','Island','Island'],hand:['Central Elevator']}},"
                + "steps:[{op:'activate',seat:0,card:'p0:Island',ability_index:0},"
                + "{op:'activate',seat:0,card:'p0:Island#2',ability_index:0},"
                + "{op:'activate',seat:0,card:'p0:Island#3',ability_index:0},"
                + "{op:'cast',seat:0,card:'p0:Central Elevator',cast_mode:'room_alt'},"
                + "{op:'resolve',seat:0}],"
                + "xmage_ability:['{T}: Add {U}.','{T}: Add {U}.','{T}: Add {U}.','','']}").getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("room_alt replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        boolean onStack = false;
        for (JsonElement e : snaps) {
            for (JsonElement s : e.getAsJsonObject().getAsJsonArray("stack")) {
                onStack |= "Promising Stairs".equals(s.getAsJsonObject().get("source").getAsString());
            }
        }
        if (!onStack) {
            throw new AssertionError("Promising Stairs was never on the stack: " + res);
        }
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        boolean unlocked = false;
        for (JsonElement e : last.getAsJsonArray("permanents")) {
            unlocked |= "Promising Stairs".equals(e.getAsJsonObject().get("name").getAsString());
        }
        if (!unlocked) {
            throw new AssertionError("the Room did not resolve with Promising Stairs unlocked: " + last);
        }
        System.out.println("PASS roomAltCastsSecondDoor: Promising Stairs is cast for {2}{U} and the Room enters"
                + " with that door unlocked");
    }

    /** CR 712.12: a modal double-faced card may be cast with its back face
     * up. gorge records that cast as cast_mode "modal_spell" and names the
     * parent card ("p0:Egon, God of Death"), the farm's shape for an alternate
     * face; XMage casts the right half's own SpellAbility, which is named
     * "Cast Throne of Death". The back-face artifact must be the spell and
     * then the permanent -- not the front creature -- and {B} pays Throne of
     * Death's cost. */
    private static void modalSpellCastsBackFace() throws Exception {
        equal("true", castModeSupported("modal_spell"), "modal_spell cast_mode");
        JsonObject sc = JsonParser.parseString("{name:'Egon, God of Death // Throne of Death',"
                + "card:'Egon, God of Death',"
                + "setup:{p0:{hand:['Egon, God of Death']}},"
                + "steps:[{op:'cast',seat:0,card:'p0:Egon, God of Death',cast_mode:'modal_spell',mana:'B'},"
                + "{op:'resolve',seat:0}]}").getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError("modal_spell replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        boolean onStack = false;
        boolean frontOnStack = false;
        for (JsonElement e : snaps) {
            for (JsonElement s : e.getAsJsonObject().getAsJsonArray("stack")) {
                String source = s.getAsJsonObject().get("source").getAsString();
                onStack |= "Throne of Death".equals(source);
                frontOnStack |= "Egon, God of Death".equals(source);
            }
        }
        if (!onStack) {
            throw new AssertionError("Throne of Death was never cast: " + res);
        }
        if (frontOnStack) {
            throw new AssertionError("Egon, God of Death was cast instead of its back face: " + res);
        }
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        java.util.List<String> names = permanentNames(last);
        if (!names.contains("Throne of Death")) {
            throw new AssertionError("Throne of Death is not on the battlefield: " + last);
        }
        if (names.contains("Egon, God of Death")) {
            throw new AssertionError("the front face entered instead of Throne of Death: " + last);
        }
        System.out.println("PASS modalSpellCastsBackFace: Throne of Death is cast for {B} and enters the battlefield");
    }

    public static void main(String[] args) throws Exception {
        String fixtures = args.length > 0 ? args[0]
                : "oracle/xmageoracle/test/org/mage/test/oracle/fixtures";
        modalSpellCastsBackFace();
        roomAltCastsSecondDoor();
        evokedCastPaysEvokeCost();
        foretellFromHandExilesFaceDown();
        suspendFromHandExilesWithTimeCounters();
        warpedCast();
        mayplayPicksTheNamedCopy();
        mayplayOpponentOwnedExile(fixtures);
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
