package org.mage.test.oracle;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.List;
import java.util.UUID;
import mage.MageObjectImpl;
import mage.abilities.SpellAbility;
import mage.abilities.costs.mana.ManaCost;
import mage.abilities.costs.mana.ManaCostsImpl;
import mage.cards.decks.Deck;
import mage.constants.MultiplayerAttackOption;
import mage.constants.RangeOfInfluence;
import mage.game.Game;
import mage.game.TwoPlayerDuel;
import mage.game.mulligan.MulliganType;
import mage.players.ManaPool;
import mage.players.ManaPoolItem;
import org.mage.test.player.TestComputerPlayer;
import org.mage.test.player.TestPlayer;

/** No database or H2: drives ScriptedChoicePlayer.playMana through a real
 * {1}{G} cost payment. The recorded multiset must decide the colour split, and
 * an unusable multiset must fall back to auto-payment rather than fail the
 * harness. Run scripts/xmage-oracle-test-payment.sh. */
public final class ScenarioReplayPaymentTest {
    private static final class Source extends MageObjectImpl {
        Source() {
            this.name = "Payment source";
        }

        @Override
        public Source copy() {
            return this;
        }
    }

    @SuppressWarnings("unchecked")
    private static void addMana(ManaPool pool, int red, int green) throws Exception {
        Field f = ManaPool.class.getDeclaredField("manaItems");
        f.setAccessible(true);
        ((List<ManaPoolItem>) f.get(pool)).add(
                new ManaPoolItem(red, green, 0, 0, 0, 0, new Source(), UUID.randomUUID(), false));
    }

    private static ScriptedPayment newPlayer(Game game) throws Exception {
        Class<?> cls = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Constructor<?> ctor = cls.getDeclaredConstructor(TestComputerPlayer.class, ScenarioReplay.class);
        ctor.setAccessible(true);
        TestPlayer p = (TestPlayer) ctor.newInstance(
                new TestComputerPlayer("A", RangeOfInfluence.ONE), null);
        game.addPlayer(p, new Deck());
        return new ScriptedPayment(p);
    }

    /** The private player's manual-payment window, reached reflectively so the
     * production beginPayment/endPayment are what runs. */
    private static final class ScriptedPayment {
        private final TestPlayer player;
        private final Method begin;
        private final Method end;

        ScriptedPayment(TestPlayer player) throws Exception {
            this.player = player;
            this.begin = player.getClass().getDeclaredMethod("beginPayment", String.class);
            this.begin.setAccessible(true);
            this.end = player.getClass().getDeclaredMethod("endPayment");
            this.end.setAccessible(true);
        }

        void begin(String letters) throws Exception {
            begin.invoke(player, letters);
        }

        void end() throws Exception {
            end.invoke(player);
        }
    }

    private static Game game() {
        return new TwoPlayerDuel(MultiplayerAttackOption.LEFT, RangeOfInfluence.ONE,
                MulliganType.GAME_DEFAULT.getMulligan(0), 60, 20, 7);
    }

    /** Pay a {1}{G} cost from the pool and return whether it was paid. */
    private static boolean pay(TestPlayer p, Game game) {
        ManaCostsImpl<ManaCost> costs = new ManaCostsImpl<>("{1}{G}");
        SpellAbility ability = new SpellAbility(costs, "Payment test");
        ability.setControllerId(p.getId());
        return costs.pay(ability, game, ability, p.getId(), false);
    }

    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    @SuppressWarnings("unchecked")
    private static void testRecordedSplit() throws Exception {
        // Pool RGG, gorge's recorded spend GG: both units are green, so the
        // red unit is what remains.
        Game game = game();
        ScriptedPayment sp = newPlayer(game);
        TestPlayer p = sp.player;
        addMana(p.getManaPool(), 1, 2);
        sp.begin("GG");
        equal(true, pay(p, game));
        equal("R", ScenarioReplay.pool(p.getManaPool()));
        sp.end();
        System.out.println("PASS recorded GG pays {1}{G} from two green, leaving R");
    }

    @SuppressWarnings("unchecked")
    private static void testGorgeSplit() throws Exception {
        // The sweep's window: pool RGG and gorge spent R for {1} and G for the
        // pip. The driver must follow that split, leaving G (not its own
        // auto-payment's G-for-generic, which leaves R).
        Game game = game();
        ScriptedPayment sp = newPlayer(game);
        TestPlayer p = sp.player;
        addMana(p.getManaPool(), 1, 2);
        sp.begin("RG");
        equal(true, pay(p, game));
        equal("G", ScenarioReplay.pool(p.getManaPool()));
        sp.end();
        System.out.println("PASS recorded RG pays {1}{G} as gorge did, leaving G");
    }

    private static void testShortMultisetFallsBack() throws Exception {
        // The second ask cannot use the recorded unit (no green left in the
        // pool): playMana's null branch must restore auto-payment instead of
        // leaving TestPlayer in manual mode with no choice, which Assert.fails
        // the whole harness. The fallback itself is the production method;
        // auto-payment from here belongs to a started game (the host replay).
        Game game = game();
        ScriptedPayment sp = newPlayer(game);
        TestPlayer p = sp.player;
        addMana(p.getManaPool(), 1, 1);
        sp.begin("GG");
        equal(false, p.getManaPool().isAutoPayment());
        Method fallback = p.getClass().getDeclaredMethod("fallBackToAutoPayment");
        fallback.setAccessible(true);
        fallback.invoke(p);
        equal(true, p.getManaPool().isAutoPayment());
        sp.end();
        System.out.println("PASS an unusable recorded multiset falls back to auto-payment");
    }

    public static void main(String[] args) throws Exception {
        testRecordedSplit();
        testGorgeSplit();
        testShortMultisetFallsBack();
    }
}
