package org.mage.test.oracle;

import com.google.gson.JsonObject;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;
import com.google.gson.JsonArray;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.cards.g.GrizzlyBears;
import mage.constants.Rarity;
import mage.game.FakeGame;
import mage.game.permanent.Permanent;
import mage.game.permanent.PermanentCard;
import mage.players.PlayerImpl;
import mage.player.ai.ComputerPlayer;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Executable check that a setup tapped_after_untap entry taps exactly the
 * one placement it names and queues no trigger. No H2 or game replay: the
 * production applySetupState runs against a real FakeGame and real
 * permanents. Run scripts/xmage-oracle-test-setup-tapped.sh.
 *
 * Two same-name permanents make "Grizzly Bears#2" the discriminating entry:
 * a by-name tap would tap both. tap() fires TAPPED and a "whenever ~ becomes
 * tapped" trigger would then fire on XMage only, so the helper must flip the
 * flag and leave the triggered queue empty. */
public final class ScenarioReplaySetupTappedTest {
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

    private static TestPlayer player(UUID id) throws Exception {
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        TestPlayer p = (TestPlayer) unsafe.allocateInstance(TestPlayer.class);
        // TestPlayer.getId() delegates to its final computerPlayer field, and
        // the id lives on PlayerImpl.playerId (also final) of that real
        // player. Neither is on TestPlayer's own hierarchy, so build a
        // ComputerPlayer shell with Unsafe and write both fields through it.
        ComputerPlayer real = (ComputerPlayer) unsafe.allocateInstance(ComputerPlayer.class);
        Field idField = PlayerImpl.class.getDeclaredField("playerId");
        unsafe.putObject(real, unsafe.objectFieldOffset(idField), id);
        Field realField = TestPlayer.class.getDeclaredField("computerPlayer");
        unsafe.putObject(p, unsafe.objectFieldOffset(realField), real);
        equal(id, p.getId());
        return p;
    }

    private static void applySetupState(ScenarioReplay driver, FakeGame game) throws Exception {
        Method method = ScenarioReplay.class
                .getDeclaredMethod("applySetupState", mage.game.Game.class);
        method.setAccessible(true);
        method.invoke(driver, game);
    }

    private static PermanentCard bears(FakeGame game, UUID ctrl) {
        Card card = new GrizzlyBears(UUID.randomUUID(),
                new CardSetInfo("Grizzly Bears", "TEST", "1", Rarity.RARE));
        PermanentCard perm = new PermanentCard(card, ctrl, game);
        game.getBattlefield().addPermanent(perm);
        return perm;
    }

    public static void main(String[] args) throws Exception {
        // The superclass constructor opens H2. Bypass it and initialise only
        // the fields the setup helper reads.
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        ScenarioReplay driver = (ScenarioReplay) unsafe.allocateInstance(ScenarioReplay.class);

        UUID ctrl = UUID.randomUUID();
        TestPlayer p0 = player(ctrl);
        TestPlayer p1 = player(UUID.randomUUID());
        set(driver, "playerA", p0);
        set(driver, "playerB", p1);
        // allocateInstance skips field initializers; applySetupState walks seats.
        set(driver, "seats", 2);
        set(driver, "gorgeName", "");
        set(driver, "xmageName", "");

        // setup: p0 taps its second Grizzly Bears after the untap step.
        JsonArray tapped = new JsonArray();
        tapped.add("Grizzly Bears#2");
        JsonObject p0setup = new JsonObject();
        p0setup.add("tapped_after_untap", tapped);
        JsonObject setup = new JsonObject();
        setup.add("p0", p0setup);
        JsonObject sc0 = new JsonObject();
        sc0.add("setup", setup);
        set(driver, "sc0", sc0);

        FakeGame game = new FakeGame();
        PermanentCard first = bears(game, ctrl);
        PermanentCard second = bears(game, ctrl);
        Map<UUID, String> setupNames = new HashMap<>();
        setupNames.put(first.getId(), "Grizzly Bears");
        setupNames.put(second.getId(), "Grizzly Bears");
        set(driver, "setupNames", setupNames);
        set(driver, "enterWithCountersApplied", new java.util.HashSet<UUID>());

        check(game.getBattlefield().getAllPermanents().size() == 2,
                "precondition: two battlefield permanents");
        check(!first.isTapped() && !second.isTapped(), "precondition: both start untapped");
        check(game.getState().getTriggered(ctrl).isEmpty(),
                "precondition: no triggered ability is queued");

        applySetupState(driver, game);

        // Which of the two is "#2" is the battlefield's iteration order, the
        // order permanentRef (the attach step's resolver) counts in.
        int tappedCount = 0;
        for (Permanent perm : game.getBattlefield().getAllPermanents()) {
            if (perm.isTapped()) {
                tappedCount++;
            }
        }
        equal(1, tappedCount);
        Permanent want = null;
        int seen = 0;
        for (Permanent perm : game.getBattlefield().getAllPermanents()) {
            if (++seen == 2) {
                want = perm;
            }
        }
        check(want != null && want.isTapped(), "Grizzly Bears#2 is tapped");
        check(game.getState().getTriggered(ctrl).isEmpty(),
                "the setup tap queued a triggered ability");

        // An entry that names no battlefield permanent fails the scenario.
        tapped = new JsonArray();
        tapped.add("Grizzly Bears#3");
        p0setup.add("tapped_after_untap", tapped);
        try {
            applySetupState(driver, game);
            throw new AssertionError("an unresolvable entry did not fail");
        } catch (java.lang.reflect.InvocationTargetException e) {
            check(e.getCause() instanceof IllegalArgumentException,
                    "unresolvable entry failed with " + e.getCause());
        }
        System.out.println("PASS setup tapped_after_untap taps one placement and queues no trigger");
    }
}
