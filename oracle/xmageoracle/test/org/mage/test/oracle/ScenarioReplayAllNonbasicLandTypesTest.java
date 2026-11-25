package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.HashMap;
import java.util.Map;
import java.util.TreeSet;
import java.util.UUID;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.cards.basiclands.Mountain;
import mage.cards.p.PlanarNexus;
import mage.constants.CardType;
import mage.constants.Rarity;
import mage.constants.SubType;
import mage.constants.Zone;
import mage.game.FakeGame;
import mage.game.Game;
import mage.game.permanent.Permanent;
import mage.game.permanent.PermanentCard;
import mage.player.ai.ComputerPlayer;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Executable check that the snapshot reports the types granted by "is every
 * nonbasic land type" (Planar Nexus, CR 205.3i). The flag is a field of the
 * permanent's subtype state, not a materialised subtype, so getSubtype alone
 * reads only the printed "Land". No H2 or game replay: a real FakeGame applies
 * the real PlanarNexus continuous effect, then the production snapshot method
 * is invoked by reflection. A basic Mountain is the control: the frozen list
 * must not turn its types into every nonbasic land type.
 *
 * Run scripts/xmage-oracle-test-all-nonbasic-land-types.sh. */
public final class ScenarioReplayAllNonbasicLandTypesTest {
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

    private static void set(Object target, String name, Object value) throws Exception {
        for (Class<?> type = target.getClass(); type != null; type = type.getSuperclass()) {
            try {
                Field field = type.getDeclaredField(name);
                field.setAccessible(true);
                field.set(target, value);
                return;
            } catch (NoSuchFieldException ignored) {
                // Fields from the XMage base class include playerA/playerB.
            }
        }
        throw new NoSuchFieldException(name);
    }

    /** The real ComputerPlayers behind the TestPlayers built by player(), by
     * id: the game needs the controller as a real player for its continuous
     * effects to apply. */
    private static final Map<UUID, ComputerPlayer> REAL = new HashMap<>();

    private static TestPlayer player(String name) throws Exception {
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        TestPlayer p = (TestPlayer) unsafe.allocateInstance(TestPlayer.class);
        ComputerPlayer real = new ComputerPlayer(name, mage.constants.RangeOfInfluence.ALL);
        Field realField = TestPlayer.class.getDeclaredField("computerPlayer");
        unsafe.putObject(p, unsafe.objectFieldOffset(realField), real);
        equal(real.getId(), p.getId());
        REAL.put(real.getId(), real);
        return p;
    }

    private static Permanent place(FakeGame game, Card card, UUID ctrl) {
        card.setOwnerId(ctrl);
        game.getState().addCard(card);
        PermanentCard perm = new PermanentCard(card, ctrl, game);
        game.getState().addCard(perm);
        game.addPermanent(perm, game.getState().getNextPermanentOrderNumber());
        perm.setZone(Zone.BATTLEFIELD, game);
        return perm;
    }

    /** The type names a snapshot row carries, in the row's sorted order. Also
     * rejects a duplicate: the flag must not append a type getSubtype already
     * printed. */
    private static TreeSet<String> types(JsonObject row) {
        JsonArray arr = row.getAsJsonArray("types");
        TreeSet<String> names = new TreeSet<>();
        for (int i = 0; i < arr.size(); i++) {
            check(names.add(arr.get(i).getAsString()), "duplicate type " + arr.get(i).getAsString());
        }
        return names;
    }

    private static JsonObject permanent(JsonObject snapshot, String name) {
        JsonArray perms = snapshot.getAsJsonArray("permanents");
        for (int i = 0; i < perms.size(); i++) {
            JsonObject row = perms.get(i).getAsJsonObject();
            if (name.equals(row.get("name").getAsString())) {
                return row;
            }
        }
        throw new AssertionError("snapshot has no " + name + " permanent");
    }

    public static void main(String[] args) throws Exception {
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        ScenarioReplay driver = (ScenarioReplay) unsafe.allocateInstance(ScenarioReplay.class);

        TestPlayer p0 = player("PlayerA");
        TestPlayer p1 = player("PlayerB");
        UUID ctrl = p0.getId();
        set(driver, "playerA", p0);
        set(driver, "playerB", p1);
        // allocateInstance skips field initializers.
        set(driver, "seats", 2);
        set(driver, "sc0", new JsonObject());
        // The snapshot's spelling pass reads the scenario card's names; the
        // empty defaults are skipped fields on an allocated instance.
        set(driver, "gorgeName", "Planar Nexus");
        set(driver, "xmageName", "Planar Nexus");

        FakeGame game = new FakeGame();
        game.getState().getPlayers().put(ctrl, REAL.get(ctrl));
        game.getState().getPlayers().put(p1.getId(), REAL.get(p1.getId()));

        CardSetInfo nexusSet = new CardSetInfo("Planar Nexus", "M3C", "132", Rarity.RARE);
        CardSetInfo mountainSet = new CardSetInfo("Mountain", "M3C", "133", Rarity.LAND);
        Permanent nexus = place(game, new PlanarNexus(UUID.randomUUID(), nexusSet), ctrl);
        Permanent mountain = place(game, new Mountain(UUID.randomUUID(), mountainSet), ctrl);
        game.applyEffects();

        check(nexus.isAllNonbasicLandTypes(game),
                "control: the Planar Nexus effect did not set isAllNonbasicLandTypes");
        check(!mountain.isAllNonbasicLandTypes(game),
                "control: a basic Mountain must not be every nonbasic land type");

        Method snapshot = ScenarioReplay.class.getDeclaredMethod("snapshot", String.class, Game.class);
        snapshot.setAccessible(true);
        JsonObject snap = (JsonObject) snapshot.invoke(driver, "setup", game);
        JsonObject row = permanent(snap, "Planar Nexus");

        TreeSet<String> want = new TreeSet<>();
        want.add(CardType.LAND.toString());
        for (SubType st : SubType.getNonbasicLandTypes()) {
            want.add(st.toString());
        }
        TreeSet<String> got = types(row);
        equal(want, got);
        check(!row.has("all_creature_types"),
                "Planar Nexus is not every creature type: the creature marker must stay absent");

        // Same snapshot, same driver: the control permanent keeps its printed
        // types. Keyed on the flag, the branch cannot leak to every land.
        TreeSet<String> mountainTypes = types(permanent(snap, "Mountain"));
        equal(new TreeSet<>(java.util.Arrays.asList("Basic", "Land", "Mountain")), mountainTypes);

        System.out.println("PASS Planar Nexus snapshot types " + got);
        System.out.println("PASS Mountain control types " + mountainTypes);
    }
}
