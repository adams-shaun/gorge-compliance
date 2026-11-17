package org.mage.test.oracle;

import com.google.gson.JsonObject;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;
import mage.abilities.Ability;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.cards.g.GrizzlyBears;
import mage.cards.h.HangarbackWalker;
import mage.constants.Rarity;
import mage.counters.Counter;
import mage.counters.CounterType;
import mage.game.FakeGame;
import mage.game.Game;
import mage.game.permanent.Permanent;
import mage.game.permanent.PermanentCard;
import mage.players.PlayerImpl;
import mage.player.ai.ComputerPlayer;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Executable check that the driver seeds a scenario's setup counters as
 * recorded state (CR 614.1: a replacement effect applies to an event, and a
 * setup counter is board state, not an event). No H2 or game replay: the
 * production putSetupCounters and placeSetupCounters run against a real FakeGame
 * and real permanents. Run scripts/xmage-oracle-test-setup-counter-replacement.sh. */
public final class ScenarioReplaySetupCounterReplacementTest {
    /** A permanent whose every addCounters overload throws: the silent setup
     * path must write straight into getCounters and never raise ADD_COUNTERS. */
    private static final class NoAddCountersPermanent extends PermanentCard {
        NoAddCountersPermanent(Card card, UUID controller, Game game) {
            super(card, controller, game);
        }

        private static boolean refuse() {
            throw new AssertionError("setup counters went through Permanent.addCounters (ADD_COUNTERS event)");
        }

        @Override
        public boolean addCounters(Counter counter, Ability source, Game game) {
            return refuse();
        }

        @Override
        public boolean addCounters(Counter counter, UUID playerAddingCounters, Ability source, Game game) {
            return refuse();
        }

        @Override
        public boolean addCounters(Counter counter, UUID playerAddingCounters, Ability source, Game game,
                boolean isEffect) {
            return refuse();
        }

        @Override
        public boolean addCounters(Counter counter, UUID playerAddingCounters, Ability source, Game game,
                java.util.List<UUID> appliedEffects) {
            return refuse();
        }

        @Override
        public boolean addCounters(Counter counter, UUID playerAddingCounters, Ability source, Game game,
                java.util.List<UUID> appliedEffects, boolean isEffect) {
            return refuse();
        }

        @Override
        public boolean addCounters(Counter counter, UUID playerAddingCounters, Ability source, Game game,
                java.util.List<UUID> appliedEffects, boolean isEffect, int maxCounters) {
            return refuse();
        }
    }

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

    private static Unsafe unsafe() throws Exception {
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        return (Unsafe) field.get(null);
    }

    private static TestPlayer player(UUID id) throws Exception {
        Unsafe unsafe = unsafe();
        TestPlayer p = (TestPlayer) unsafe.allocateInstance(TestPlayer.class);
        ComputerPlayer real = (ComputerPlayer) unsafe.allocateInstance(ComputerPlayer.class);
        Field idField = PlayerImpl.class.getDeclaredField("playerId");
        unsafe.putObject(real, unsafe.objectFieldOffset(idField), id);
        Field realField = TestPlayer.class.getDeclaredField("computerPlayer");
        unsafe.putObject(p, unsafe.objectFieldOffset(realField), real);
        equal(id, p.getId());
        return p;
    }

    private static String pt(Permanent perm) {
        return perm.getPower().getValue() + "/" + perm.getToughness().getValue();
    }

    private static JsonObject kinds(String kind, int n) {
        JsonObject o = new JsonObject();
        o.addProperty(kind, n);
        return o;
    }

    /** A driver whose scenario seeds {@code counters} on p0's cardName. */
    private static ScenarioReplay driver(UUID ctrl, String cardName, JsonObject kindsByName,
            Permanent perm) throws Exception {
        Unsafe unsafe = unsafe();
        ScenarioReplay driver = (ScenarioReplay) unsafe.allocateInstance(ScenarioReplay.class);
        set(driver, "playerA", player(ctrl));
        set(driver, "playerB", player(UUID.randomUUID()));
        // allocateInstance skips field initializers.
        set(driver, "seats", 2);
        set(driver, "gorgeName", "");
        set(driver, "xmageName", "");
        JsonObject counters = new JsonObject();
        counters.add(cardName, kindsByName);
        JsonObject p0setup = new JsonObject();
        p0setup.add("counters", counters);
        JsonObject setup = new JsonObject();
        setup.add("p0", p0setup);
        JsonObject sc0 = new JsonObject();
        sc0.add("setup", setup);
        set(driver, "sc0", sc0);
        Map<UUID, String> setupNames = new HashMap<>();
        setupNames.put(perm.getId(), cardName);
        set(driver, "setupNames", setupNames);
        return driver;
    }

    private static void placeSetupCounters(ScenarioReplay driver, Game game) throws Exception {
        Method method = ScenarioReplay.class.getDeclaredMethod("placeSetupCounters", Game.class);
        method.setAccessible(true);
        method.invoke(driver, game);
    }

    private static Card bears() {
        return new GrizzlyBears(UUID.randomUUID(), new CardSetInfo("Grizzly Bears", "TEST", "1", Rarity.RARE));
    }

    public static void main(String[] args) throws Exception {
        UUID ctrl = UUID.randomUUID();

        // Precondition: a 2/2 base (Grizzly Bears) plus four +1/+1 counters via
        // the helper is 6/6 after applyEffects, and exactly four counters.
        {
            FakeGame game = new FakeGame();
            PermanentCard perm = new PermanentCard(bears(), ctrl, game);
            game.getBattlefield().addPermanent(perm);
            equal("2/2", pt(perm));
            ScenarioReplay.putSetupCounters(game, perm, kinds("P1P1", 4));
            game.applyEffects();
            equal(4, perm.getCounters(game).getCount(CounterType.P1P1));
            equal("6/6", pt(perm));
            System.out.println("PASS precondition: helper leaves four P1P1 counters (" + pt(perm) + ")");
        }

        // Replacement: FakeGame has no owning player, so it cannot run a
        // counter-doubling ADD_COUNTERS replacement (ContinuousEffectsList NPEs
        // on the effect owner). Wrap the permanent so every addCounters overload
        // throws: a replacement can only see the seed through that event, so the
        // helper never calling it means no replacement can apply.
        {
            FakeGame game = new FakeGame();
            NoAddCountersPermanent perm = new NoAddCountersPermanent(bears(), ctrl, game);
            game.getBattlefield().addPermanent(perm);
            ScenarioReplay.putSetupCounters(game, perm, kinds("P1P1", 4));
            equal(4, perm.getCounters(game).getCount(CounterType.P1P1));
            System.out.println("PASS replacement: the helper never calls Permanent.addCounters");
        }

        // Printed 0/0: the pre-start hook seeds {P1P1: 3} with no state-based
        // action in between, so the placement is 3/3 with its counters.
        {
            FakeGame game = new FakeGame();
            Card walker = new HangarbackWalker(UUID.randomUUID(),
                    new CardSetInfo("Hangarback Walker", "TEST", "1", Rarity.RARE));
            NoAddCountersPermanent perm = new NoAddCountersPermanent(walker, ctrl, game);
            game.getBattlefield().addPermanent(perm);
            check(perm.isCreature(game), "precondition: Hangarback Walker is a creature");
            equal("0/0", pt(perm));
            equal(0, perm.getCounters(game).getCount(CounterType.P1P1));
            ScenarioReplay driver = driver(ctrl, "Hangarback Walker", kinds("P1P1", 3), perm);
            placeSetupCounters(driver, game);
            equal(3, perm.getCounters(game).getCount(CounterType.P1P1));
            equal("3/3", pt(perm));
            check(game.getBattlefield().getAllPermanents().size() == 1, "the 0/0 placement is still on the battlefield");
            // A second call (start() runs once, but the guard must hold) adds nothing.
            placeSetupCounters(driver, game);
            equal(3, perm.getCounters(game).getCount(CounterType.P1P1));
            System.out.println("PASS printed 0/0 placement keeps its setup counters (" + pt(perm) + ")");
        }
    }
}
