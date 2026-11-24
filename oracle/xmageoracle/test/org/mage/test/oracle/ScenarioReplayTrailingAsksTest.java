package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.Arrays;
import java.util.List;
import sun.misc.Unsafe;

/** Executable regression checks for the asks gorge never records and XMage
 * poses anyway. Run scripts/xmage-oracle-test-trailing-asks.sh.
 *
 *  - An unscripted ask posed after every checkpoint was captured is outside
 *    the item: the strict row stands and the miss is kept as trailing_miss,
 *    so the classifier no longer files it as decision_shape (xproof:
 *    Unscrupulous Contractor's ETB, which the window's last pass lets
 *    resolve). A miss before the last checkpoint still reruns loose.
 *  - A scripted scry/surveil puts its piles back without ORDER asks, pile A
 *    moved last-first so its first card ends topmost (Candy Trail).
 *  - A choose ask whose every candidate must be taken is forced (Abyssal
 *    Gorestalker's sacrifice of its controller's lone creature). */
public final class ScenarioReplayTrailingAsksTest {
    /** A replayOnce double: the strict run strict-misses after capturing
     * {@code strictSnaps} snapshots; the loose run returns a clean row. */
    static final class StubDriver extends ScenarioReplay {
        int strictSnaps;
        int strictCalls;
        int looseCalls;

        @Override
        JsonObject replayOnce(JsonObject sc, boolean strict) {
            JsonObject res = new JsonObject();
            res.addProperty("strict", strict);
            JsonArray snaps = new JsonArray();
            int n = strict ? strictSnaps : steps(sc).size() + 1;
            for (int i = 0; i < n; i++) {
                snaps.add(new JsonObject());
            }
            if (strict) {
                strictCalls++;
                res.addProperty("harness", "AssertionError: Missing CHOICE def for turn 1, step PRECOMBAT_MAIN, PlayerA");
            } else {
                looseCalls++;
            }
            res.add("snapshots", snaps);
            return res;
        }
    }

    private static StubDriver driver(int strictSnaps) throws Exception {
        // The superclass constructor scans CardRepository (H2); bypass it.
        Field f = Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        StubDriver d = (StubDriver) ((Unsafe) f.get(null)).allocateInstance(StubDriver.class);
        d.strictSnaps = strictSnaps;
        return d;
    }

    private static void check(boolean ok, String what) {
        if (!ok) {
            throw new AssertionError(what);
        }
    }

    private static Class<?> player() throws Exception {
        return Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
    }

    public static void main(String[] args) throws Exception {
        JsonObject sc = JsonParser.parseString("{\"id\":\"x\",\"steps\":[{\"op\":\"cast\"},{\"op\":\"pass\"},{\"op\":\"pass\"}]}").getAsJsonObject();

        // Every checkpoint captured (setup + 3 steps): trailing, no rerun.
        StubDriver after = driver(4);
        JsonObject row = after.replay(sc);
        check(row.get("strict").getAsBoolean(), "a trailing miss lost the strict row");
        check(!row.has("harness") && !row.has("strict_miss"), "a trailing miss was still reported: " + row);
        check(row.get("trailing_miss").getAsString().startsWith("AssertionError: Missing CHOICE def"), "trailing_miss not kept: " + row);
        check(after.looseCalls == 0, "a trailing miss reran loose");
        System.out.println("PASS trailing miss after the last checkpoint: " + row.get("trailing_miss").getAsString());

        // A miss with the last checkpoint still ahead is the item's own.
        StubDriver before = driver(3);
        row = before.replay(sc);
        check(!row.get("strict").getAsBoolean() && row.has("strict_miss") && !row.has("trailing_miss"),
                "a miss inside the window was not rerun loose: " + row);
        check(before.looseCalls == 1, "loose rerun count " + before.looseCalls);
        System.out.println("PASS miss inside the window still strict_miss");

        check(ScenarioReplay.trailingStrictMiss(4, 3), "4 snapshots of 3 steps is complete");
        check(!ScenarioReplay.trailingStrictMiss(3, 3), "3 snapshots of 3 steps is not");
        check(!ScenarioReplay.trailingStrictMiss(1, 0), "a stepless item has no window to trail");

        // Pile A goes back last-first, so its first card ends topmost.
        Method moves = player().getDeclaredMethod("arrangedTopMoves", List.class);
        moves.setAccessible(true);
        check(moves.invoke(null, Arrays.asList("top", "second", "third")).equals(Arrays.asList("third", "second", "top")),
                "pile A move order");
        System.out.println("PASS scry/surveil pile A placement order");

        Method forced = player().getDeclaredMethod("forcedPick", int.class, int.class, String.class, boolean.class);
        forced.setAccessible(true);
        check((boolean) forced.invoke(null, 2, 1, null, false), "sacrifice two over one creature is forced");
        check((boolean) forced.invoke(null, 1, 1, "Island", false), "a head naming no candidate leaves it forced");
        check(!(boolean) forced.invoke(null, 1, 2, null, false), "two candidates for one pick is a choice");
        check(!(boolean) forced.invoke(null, 0, 1, null, false), "an optional ask is not forced");
        check(!(boolean) forced.invoke(null, 1, 0, null, false), "no candidate is not forced");
        check(!(boolean) forced.invoke(null, 1, 1, "Snail", true), "a head naming the candidate is the ask's own");
        check(!(boolean) forced.invoke(null, 1, 1, "[choice_skip]", false), "a queued skip is the ask's own");
        System.out.println("PASS forced choose (every candidate needed, head not its answer)");
    }
}
