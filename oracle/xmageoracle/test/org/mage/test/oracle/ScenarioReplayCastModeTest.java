package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;

/** Executable check of the driver's cast-mode acceptance list and of the
 * Adventure/Omen cast path. The acceptance checks are pure (cast_mode decides
 * whether a cast step is replayed at all); the Adventure/Omen cases replay a
 * real one-step game, so the script gives XMage's H2 card DB a private copy.
 * "mayplay" is the cast from exile under Reckless Impulse's play permission; it
 * must be accepted, while the modes the driver still cannot replay
 * ("plot_cast") and the deliberately-unsupported "kicked" must stay rejected.
 * Run scripts/xmage-oracle-test-cast-mode.sh. */
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

    public static void main(String[] args) throws Exception {
        warpedCast();
        // Regression: the driver rejected "mayplay" by name before the replay
        // ever reached the cast, so every Reckless Impulse window was a
        // harness row. The card is in exile under a play-from-exile
        // permission; the step is still an ordinary cast on this side.
        equal("true", castModeSupported("mayplay"), "mayplay cast_mode");
        System.out.println("PASS mayplay cast_mode is accepted (cast from exile under a play permission)");

        // The modes this driver cannot replay still fail loudly rather than
        // being cast at face value. "plot_cast" is the later cast of a plotted
        // card; "kicked" stays unsupported on purpose
        // (ScenarioReplayDriverContractTest asserts it).
        equal("false", castModeSupported("plot_cast"), "plot_cast cast_mode");
        equal("false", castModeSupported("kicked"), "kicked cast_mode");
        System.out.println("PASS plot_cast stays rejected (kicked too)");

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

        // Guards the list's ordinary behaviour: no mode is the plain cast.
        equal("true", castModeSupported(""), "absent cast_mode");
        System.out.println("PASS an absent cast_mode is the ordinary cast");
    }
}
