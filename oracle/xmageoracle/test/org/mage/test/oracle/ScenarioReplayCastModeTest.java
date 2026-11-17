package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;

/** Executable check of the driver's cast-mode acceptance list, without H2 or a
 * game: cast_mode decides whether a cast step is replayed at all, so the
 * wiring check is the acceptance predicate itself. "mayplay" is the cast from
 * exile under Reckless Impulse's play permission; it must be accepted, while
 * the modes the driver still cannot replay ("plot_cast") and
 * the deliberately-unsupported "kicked" must stay rejected.
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

    /** Replays a one-step scenario that casts the spell half of parent by its
     * half name under mode, targeting a Grizzly Bears, and asserts the half
     * is the stack's spell once the cast step has run. */
    private static void stackHas(String parent, String half, String mode, String mana) throws Exception {
        JsonObject sc = JsonParser.parseString("{name:'" + half + "',card:'" + parent + "',"
                + "setup:{p0:{hand:['" + parent + "']},p1:{battlefield:['Grizzly Bears']}},"
                + "steps:[{op:'cast',seat:0,card:'p0:" + half + "',cast_mode:'" + mode + "',mana:'" + mana
                + "',targets:['p1:Grizzly Bears']}]}").getAsJsonObject();
        JsonObject res = new ScenarioReplay().replayOnce(sc, false);
        if (res.has("harness") || res.has("error")) {
            throw new AssertionError(half + " replay failed: " + res);
        }
        JsonArray snaps = res.getAsJsonArray("snapshots");
        JsonObject last = snaps.get(snaps.size() - 1).getAsJsonObject();
        JsonArray stack = last.getAsJsonArray("stack");
        boolean found = false;
        for (JsonElement e : stack) {
            JsonObject o = e.getAsJsonObject();
            found |= half.equals(o.get("source").getAsString());
        }
        if (!found) {
            throw new AssertionError(half + " (" + mode + ") is not on the stack: " + last);
        }
        System.out.println("PASS " + mode + " casts " + half + " by its own name; it is on the stack");
    }

    public static void main(String[] args) throws Exception {
        // Regression: the driver rejected "mayplay" by name before the replay
        // ever reached the cast, so every Reckless Impulse window was a
        // harness row. The card is in exile under a play-from-exile
        // permission; the step is still an ordinary cast on this side.
        equal("true", castModeSupported("mayplay"), "mayplay cast_mode");
        System.out.println("PASS mayplay cast_mode is accepted (cast from exile under a play permission)");

        // The modes this driver cannot replay still fail loudly rather than
        // being cast at face value. "plot_cast" is the later cast of a plotted
        // card; "adventure_alt" has no wired alternative. "kicked" stays
        // unsupported on purpose (ScenarioReplayDriverContractTest asserts it).
        equal("false", castModeSupported("plot_cast"), "plot_cast cast_mode");
        equal("false", castModeSupported("kicked"), "kicked cast_mode");
        System.out.println("PASS plot_cast stays rejected (kicked too)");

        // The Adventure and Omen halves are cast by the half's own spell name.
        equal("true", castModeSupported("adventure_alt"), "adventure_alt cast_mode");
        equal("true", castModeSupported("omen_alt"), "omen_alt cast_mode");
        System.out.println("PASS adventure_alt and omen_alt are accepted");

        // Real replays: the half is cast by its own name and sits on the stack.
        stackHas("Bonecrusher Giant", "Stomp", "adventure_alt", "RR");
        stackHas("Purging Stormbrood", "Absorb Essence", "omen_alt", "WW");

        // Guards the list's ordinary behaviour: no mode is the plain cast.
        equal("true", castModeSupported(""), "absent cast_mode");
        System.out.println("PASS an absent cast_mode is the ordinary cast");
    }
}
