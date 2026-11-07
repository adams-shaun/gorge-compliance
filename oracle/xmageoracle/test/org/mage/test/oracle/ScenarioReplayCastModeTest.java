package org.mage.test.oracle;

/** Executable check of the driver's cast-mode acceptance list, without H2 or a
 * game: cast_mode decides whether a cast step is replayed at all, so the
 * wiring check is the acceptance predicate itself. "mayplay" is the cast from
 * exile under Reckless Impulse's play permission; it must be accepted, while
 * the modes the driver still cannot replay ("plot_cast", "adventure_alt") and
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
        equal("false", castModeSupported("adventure_alt"), "adventure_alt cast_mode");
        equal("false", castModeSupported("kicked"), "kicked cast_mode");
        System.out.println("PASS plot_cast and adventure_alt stay rejected (kicked too)");

        // Guards the list's ordinary behaviour: no mode is the plain cast.
        equal("true", castModeSupported(""), "absent cast_mode");
        System.out.println("PASS an absent cast_mode is the ordinary cast");
    }
}
