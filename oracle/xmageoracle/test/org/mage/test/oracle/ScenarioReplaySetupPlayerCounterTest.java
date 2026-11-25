package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.UUID;
import mage.counters.CounterType;
import mage.counters.Counters;
import mage.players.PlayerImpl;
import mage.player.ai.ComputerPlayer;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Executable check that a setup seat's "player_counters" reach the player as
 * recorded state. No H2 or game replay: the production putSetupPlayerCounters
 * runs against a real TestPlayer/ComputerPlayer shell. Run
 * scripts/xmage-oracle-test-setup-player-counter.sh.
 *
 * The setup snapshot records a seat's counters from Player.getCountersAsCopy,
 * so asserting the counter counts after the production hook is asserting
 * exactly the names and amounts the snapshot reports. */
public final class ScenarioReplaySetupPlayerCounterTest {
    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static TestPlayer player(UUID id) throws Exception {
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        TestPlayer p = (TestPlayer) unsafe.allocateInstance(TestPlayer.class);
        // TestPlayer.getId() delegates to its final computerPlayer field, and
        // the counter map lives on PlayerImpl.counters of that real player.
        // allocateInstance skips both initializers, so write the fields the
        // production helper reads.
        ComputerPlayer real = (ComputerPlayer) unsafe.allocateInstance(ComputerPlayer.class);
        Field idField = PlayerImpl.class.getDeclaredField("playerId");
        unsafe.putObject(real, unsafe.objectFieldOffset(idField), id);
        Field countersField = PlayerImpl.class.getDeclaredField("counters");
        unsafe.putObject(real, unsafe.objectFieldOffset(countersField), new Counters());
        Field realField = TestPlayer.class.getDeclaredField("computerPlayer");
        unsafe.putObject(p, unsafe.objectFieldOffset(realField), real);
        equal(id, p.getId());
        return p;
    }

    private static JsonObject counter(String kind, int n) {
        JsonObject pc = new JsonObject();
        pc.addProperty("kind", kind);
        pc.addProperty("n", n);
        return pc;
    }

    /** Runs the production hook that writes a setup seat's player_counters:
     * straight into PlayerImpl.counters, with no ADD_COUNTERS event and no
     * COUNTER_ADDED trigger (a recorded setup state, not a counter addition). */
    private static void putSetupPlayerCounters(TestPlayer pl, JsonArray counters) throws Exception {
        Method method = ScenarioReplay.class.getDeclaredMethod(
                "putSetupPlayerCounters", TestPlayer.class, JsonArray.class);
        method.setAccessible(true);
        method.invoke(null, pl, counters);
    }

    /** Runs the call site's seat-level entry point (the "player_counters" key
     * check the setup hook goes through). */
    private static void putSeatSetupPlayerCounters(TestPlayer pl, JsonObject seat) throws Exception {
        Method method = ScenarioReplay.class.getDeclaredMethod(
                "putSeatSetupPlayerCounters", TestPlayer.class, JsonObject.class);
        method.setAccessible(true);
        method.invoke(null, pl, seat);
    }

    public static void main(String[] args) throws Exception {
        TestPlayer p0 = player(UUID.randomUUID());
        TestPlayer p1 = player(UUID.randomUUID());

        JsonArray counters = new JsonArray();
        counters.add(counter("ENERGY", 8));
        counters.add(counter("POISON", 2));
        counters.add(counter("EXPERIENCE", 3));
        putSetupPlayerCounters(p0, counters);
        equal(8, p0.getCountersCount(CounterType.ENERGY));
        equal(2, p0.getCountersCount(CounterType.POISON));
        equal(3, p0.getCountersCount(CounterType.EXPERIENCE));
        // The hook names one seat, not every seat: p1 is untouched.
        equal(0, p1.getCountersCount(CounterType.ENERGY));
        equal(0, p1.getCountersCount(CounterType.POISON));
        equal(0, p1.getCountersCount(CounterType.EXPERIENCE));
        System.out.println("PASS setup player_counters reach the seat's counters");

        // An unknown kind is a loud failure, not a silent drop.
        JsonArray bad = new JsonArray();
        bad.add(counter("NOT_A_COUNTER", 1));
        try {
            putSetupPlayerCounters(p1, bad);
            check(false, "an unknown player counter kind should fail");
        } catch (java.lang.reflect.InvocationTargetException expected) {
            check(expected.getCause() instanceof IllegalArgumentException,
                    "unknown kind should raise IllegalArgumentException, got " + expected.getCause());
        }
        equal(0, p1.getCountersTotalCount());
        System.out.println("PASS an unknown player counter kind fails loudly");

        // "n" is the COMPLETE count at the checkpoint: a seat that already
        // holds counters is moved to it, never added to (up, down and to 0).
        TestPlayer p2 = player(UUID.randomUUID());
        JsonArray seed = new JsonArray();
        seed.add(counter("ENERGY", 3));
        seed.add(counter("POISON", 4));
        seed.add(counter("EXPERIENCE", 1));
        putSetupPlayerCounters(p2, seed);
        JsonArray complete = new JsonArray();
        complete.add(counter("ENERGY", 3));
        complete.add(counter("POISON", 1));
        complete.add(counter("EXPERIENCE", 5));
        JsonObject seat = new JsonObject();
        seat.add("player_counters", complete);
        putSeatSetupPlayerCounters(p2, seat);
        equal(3, p2.getCountersCount(CounterType.ENERGY));
        equal(1, p2.getCountersCount(CounterType.POISON));
        equal(5, p2.getCountersCount(CounterType.EXPERIENCE));
        JsonArray zero = new JsonArray();
        zero.add(counter("ENERGY", 0));
        JsonObject zeroSeat = new JsonObject();
        zeroSeat.add("player_counters", zero);
        putSeatSetupPlayerCounters(p2, zeroSeat);
        equal(0, p2.getCountersCount(CounterType.ENERGY));
        equal(1, p2.getCountersCount(CounterType.POISON));
        // A seat with no player_counters key is left alone.
        putSeatSetupPlayerCounters(p2, new JsonObject());
        equal(1, p2.getCountersCount(CounterType.POISON));
        System.out.println("PASS complete player_counters set the seat, not add to it");
    }
}
