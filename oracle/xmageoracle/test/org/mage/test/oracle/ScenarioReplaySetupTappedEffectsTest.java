package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.cards.basiclands.Forest;
import mage.cards.s.ScoriaCat;
import mage.constants.Rarity;
import mage.constants.RangeOfInfluence;
import mage.constants.Zone;
import mage.counters.CounterType;
import mage.game.FakeGame;
import mage.game.permanent.Permanent;
import mage.game.permanent.PermanentCard;
import mage.player.ai.ComputerPlayer;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Executable check that the setup snapshot re-applies continuous effects after
 * the setup's tap state, not just after counters and speed. No H2 or game
 * replay: the production applySetupState runs against a real FakeGame with a
 * real Scoria Cat ("gets +3/+3 as long as you control no untapped lands") and
 * six real Forests. Run scripts/xmage-oracle-test-setup-tapped-effects.sh.
 *
 * The setup snapshot writes exactly the P/T asserted here. This is the
 * regression for a stale first snapshot: setTapped(true) does not itself
 * recompute, so before the fix the Cat stayed 4/4 beside a tapped board. The
 * plain setup "tapped" placement path taps during addCard, before this hook,
 * and is exercised the same way by flipping the flags first. */
public final class ScenarioReplaySetupTappedEffectsTest {
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
        // TestPlayer's constructor needs the test harness; build a shell with
        // Unsafe and give it a real ComputerPlayer to delegate to.
        TestPlayer p = (TestPlayer) unsafe.allocateInstance(TestPlayer.class);
        ComputerPlayer real = new ComputerPlayer(name, RangeOfInfluence.ALL);
        Field realField = TestPlayer.class.getDeclaredField("computerPlayer");
        unsafe.putObject(p, unsafe.objectFieldOffset(realField), real);
        equal(real.getId(), p.getId());
        REAL.put(real.getId(), real);
        return p;
    }

    private static PermanentCard place(FakeGame game, Card card, UUID ctrl) {
        card.setOwnerId(ctrl);
        game.getState().addCard(card);
        PermanentCard perm = new PermanentCard(card, ctrl, game);
        game.getState().addCard(perm);
        game.addPermanent(perm, game.getState().getNextPermanentOrderNumber());
        perm.setZone(Zone.BATTLEFIELD, game);
        return perm;
    }

    private static void applySetupState(ScenarioReplay driver, FakeGame game) throws Exception {
        Method method = ScenarioReplay.class
                .getDeclaredMethod("applySetupState", mage.game.Game.class);
        method.setAccessible(true);
        method.invoke(driver, game);
    }

    private static String pt(Permanent perm) {
        return perm.getPower().getValue() + "/" + perm.getToughness().getValue();
    }

    /** Runs one setup board: six Forests and a 3/3 Scoria Cat with one +1/+1
     * counter, so 4/4 unboosted and 7/7 with all six lands tapped. When
     * {@code tappedAfterUntap} the taps are in the setup's tapped_after_untap
     * list; otherwise they are the plain setup "tapped" placement, modelled by
     * flipping the flags before the hook as addCard(..., tapped) does. The Cat
     * is 4/4 before the hook because the initial six untapped lands suppress
     * the boost, so a stale first snapshot stays 4/4. */
    private static String setupPt(boolean tappedAfterUntap, boolean allTapped) throws Exception {
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        ScenarioReplay driver = (ScenarioReplay) unsafe.allocateInstance(ScenarioReplay.class);

        TestPlayer p0 = player("PlayerA");
        UUID ctrl = p0.getId();
        TestPlayer p1 = player("PlayerB");
        set(driver, "playerA", p0);
        set(driver, "playerB", p1);
        // allocateInstance skips field initializers; applySetupState walks seats.
        set(driver, "seats", 2);
        set(driver, "gorgeName", "");
        set(driver, "xmageName", "");

        FakeGame game = new FakeGame();
        game.getState().getPlayers().put(ctrl, REAL.get(ctrl));
        game.getState().getPlayers().put(p1.getId(), REAL.get(p1.getId()));
        game.getState().setActivePlayerId(ctrl);

        List<PermanentCard> lands = new ArrayList<>();
        for (int i = 0; i < 6; i++) {
            lands.add(place(game, new Forest(UUID.randomUUID(),
                    new CardSetInfo("Forest", "TEST", Integer.toString(i + 1), Rarity.LAND)), ctrl));
        }
        PermanentCard cat = place(game, new ScoriaCat(UUID.randomUUID(),
                new CardSetInfo("Scoria Cat", "TEST", "1", Rarity.RARE)), ctrl);
        cat.getCounters(game).addCounter(CounterType.P1P1.createInstance(1));
        game.applyEffects();

        // Precondition: six untapped lands, so the conditional boost is off and
        // the counter is the only layer-7d effect. A stale hook leaves this.
        equal("4/4", pt(cat));

        int tapped = allTapped ? 6 : 5;
        JsonArray names = new JsonArray();
        for (int i = 1; i <= tapped; i++) {
            names.add("Forest#" + i);
        }
        JsonObject p0setup = new JsonObject();
        p0setup.add(tappedAfterUntap ? "tapped_after_untap" : "tapped", names);
        JsonObject setup = new JsonObject();
        setup.add("p0", p0setup);
        JsonObject sc0 = new JsonObject();
        sc0.add("setup", setup);
        set(driver, "sc0", sc0);

        Map<UUID, String> setupNames = new HashMap<>();
        for (PermanentCard land : lands) {
            setupNames.put(land.getId(), "Forest");
        }
        setupNames.put(cat.getId(), "Scoria Cat");
        set(driver, "setupNames", setupNames);

        if (!tappedAfterUntap) {
            // addCard's tapped placement flips the flag during placement, i.e.
            // after the last effects pass and before applySetupState.
            for (int i = 0; i < tapped; i++) {
                lands.get(i).setTapped(true);
            }
        }

        applySetupState(driver, game);
        return pt(cat);
    }

    public static void main(String[] args) throws Exception {
        // All six lands tapped by the setup: 3/3 + one counter + the +3/+3
        // static, read against the tapped board. Before the fix this is 4/4.
        equal("7/7", setupPt(true, true));
        // One land untapped: the conditional static is correctly off.
        equal("4/4", setupPt(true, false));
        // The plain setup "tapped" placement is board state too, so the same
        // unconditional recompute must cover it.
        equal("7/7", setupPt(false, true));
        equal("4/4", setupPt(false, false));
        System.out.println("PASS setup tap state reaches the snapshot P/T (7/7 tapped, 4/4 one untapped)");
    }
}
