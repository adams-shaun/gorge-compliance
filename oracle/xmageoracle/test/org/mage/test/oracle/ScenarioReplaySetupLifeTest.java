package org.mage.test.oracle;

import com.google.gson.JsonObject;
import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.UUID;
import mage.abilities.Ability;
import mage.abilities.common.AsEntersBattlefieldAbility;
import mage.abilities.costs.common.PayLifeCost;
import mage.abilities.effects.common.TapSourceUnlessPaysEffect;
import mage.constants.Outcome;
import mage.constants.RangeOfInfluence;
import mage.game.FakeGame;
import mage.game.Game;
import mage.players.PlayerImpl;
import mage.player.ai.ComputerPlayer;
import org.mage.test.player.TestComputerPlayer;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Executable check that the setup drive pays an unscripted shock-land ask and
 * then restores the requested setup life. No H2 or game replay: the production
 * ScriptedChoicePlayer.chooseUse branch and applySetupLife helper run directly.
 * Run scripts/xmage-oracle-test-setup-life.sh.
 *
 * The ask is posed from a TapSourceUnlessPaysEffect subclass so the production
 * askedBy(TapSourceUnlessPaysEffect.class, "apply") frame is real, and the
 * FakeGame is still pre-start (step null), which is the driver's setup window. */
public final class ScenarioReplaySetupLifeTest {
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

    private static TestPlayer scriptedPlayer(ScenarioReplay owner) throws Exception {
        Class<?> type = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Constructor<?> ctor = type.getDeclaredConstructor(TestComputerPlayer.class, ScenarioReplay.class);
        ctor.setAccessible(true);
        return (TestPlayer) ctor.newInstance(
                new TestComputerPlayer("PlayerB", RangeOfInfluence.ONE), owner);
    }

    private static void applySetupLife(ScenarioReplay driver) throws Exception {
        Method method = ScenarioReplay.class.getDeclaredMethod("applySetupLife");
        method.setAccessible(true);
        method.invoke(driver);
    }

    /** A real TapSourceUnlessPaysEffect frame (askedBy keys on its apply). */
    private static final class ProbeUnlessPays extends TapSourceUnlessPaysEffect {
        private final TestPlayer player;

        ProbeUnlessPays(TestPlayer player) {
            super(new PayLifeCost(2));
            this.player = player;
        }

        @Override
        public boolean apply(Game game, Ability source) {
            return player.chooseUse(Outcome.Benefit,
                    "pay 2 life? (otherwise Breeding Pool becomes tapped)", source, game);
        }

        @Override
        public ProbeUnlessPays copy() {
            return new ProbeUnlessPays(player);
        }
    }

    public static void main(String[] args) throws Exception {
        // The superclass constructor opens H2. Bypass it and initialise only
        // the fields the setup helpers read.
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        ScenarioReplay driver = (ScenarioReplay) unsafe.allocateInstance(ScenarioReplay.class);

        FakeGame game = new FakeGame();
        check(game.getStep() == null, "precondition: the setup window is pre-start");

        Ability source = new AsEntersBattlefieldAbility(
                new TapSourceUnlessPaysEffect(new PayLifeCost(2)));

        // 1. An unscripted setup as-enters ask pays instead of recording a
        // strict miss; the branch returns before TestPlayer's strict failure.
        TestPlayer asker = scriptedPlayer(driver);
        asker.setChooseStrictMode(true);
        asker.getChoices().clear();
        boolean paid = new ProbeUnlessPays(asker).apply(game, source);
        check(paid, "unscripted setup as-enters ask should pay");
        System.out.println("PASS unscripted setup as-enters ask pays without a strict miss");

        // 2. A scripted setup Yes/No still wins: the queue is consumed and the
        // ask returns the scripted No.
        asker.getChoices().add("No");
        boolean declined = new ProbeUnlessPays(asker).apply(game, source);
        check(!declined, "a scripted setup No should decline");
        check(asker.getChoices().isEmpty(), "the scripted setup No should be consumed");
        System.out.println("PASS a scripted setup No is consumed");

        // 3. The requested life is re-applied after the placements, undoing a
        // shock land's 2-life payment.
        UUID ctrl = UUID.randomUUID();
        TestPlayer p0 = player(ctrl);
        TestPlayer p1 = player(UUID.randomUUID());
        set(driver, "playerA", p0);
        set(driver, "playerB", p1);
        set(driver, "seats", 2);

        JsonObject p0setup = new JsonObject();
        p0setup.addProperty("life", 20);
        JsonObject p1setup = new JsonObject();
        p1setup.addProperty("life", 8);
        JsonObject setup = new JsonObject();
        setup.add("p0", p0setup);
        setup.add("p1", p1setup);
        JsonObject sc0 = new JsonObject();
        sc0.add("setup", setup);
        set(driver, "sc0", sc0);

        // The state after each seat's shock land paid 2 life during placement.
        p0.initLife(18);
        p1.initLife(6);
        applySetupLife(driver);
        equal(20, p0.getLife());
        equal(8, p1.getLife());

        // A seat with no requested life keeps the value the placement left.
        p1setup.remove("life");
        p1.initLife(6);
        applySetupLife(driver);
        equal(20, p0.getLife());
        equal(6, p1.getLife());
        System.out.println("PASS setup life is re-applied after placement");
    }
}
