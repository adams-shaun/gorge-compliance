package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import mage.abilities.Ability;
import mage.abilities.common.SimpleStaticAbility;
import mage.abilities.effects.common.InfoEffect;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.cards.d.DoublingSeason;
import mage.cards.m.MirkwoodBats;
import mage.constants.Rarity;
import mage.constants.Zone;
import mage.counters.CounterType;
import mage.game.FakeGame;
import mage.game.permanent.Permanent;
import mage.game.permanent.PermanentCard;
import mage.game.permanent.token.ClueArtifactToken;
import mage.player.ai.ComputerPlayer;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Executable check of the driver's silent setup-token placement. No H2 or game
 * replay: the production placeSetupTokens runs against a real FakeGame with
 * Mirkwood Bats ("whenever you create or sacrifice a token") and Doubling
 * Season (CreateToken and AddCounters replacement) on the battlefield. Run
 * scripts/xmage-oracle-test-setup-token.sh.
 *
 * Two controls run first, so the later assertions are the placement bypassing
 * them and not a FakeGame that never applies them: a created token is doubled
 * by Doubling Season, and a CREATED_TOKEN event queues Mirkwood Bats. */
public final class ScenarioReplaySetupTokenTest {
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
    private static final java.util.Map<UUID, ComputerPlayer> REAL = new java.util.HashMap<>();

    private static TestPlayer player(String name) throws Exception {
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        // TestPlayer's constructor needs the test harness; build a shell with
        // Unsafe and give it a real ComputerPlayer to delegate to.
        TestPlayer p = (TestPlayer) unsafe.allocateInstance(TestPlayer.class);
        ComputerPlayer real = new ComputerPlayer(name, mage.constants.RangeOfInfluence.ALL);
        Field realField = TestPlayer.class.getDeclaredField("computerPlayer");
        unsafe.putObject(p, unsafe.objectFieldOffset(realField), real);
        equal(real.getId(), p.getId());
        REAL.put(real.getId(), real);
        return p;
    }

    private static void place(FakeGame game, Card card, UUID ctrl) {
        card.setOwnerId(ctrl);
        game.getState().addCard(card);
        PermanentCard perm = new PermanentCard(card, ctrl, game);
        game.getState().addCard(perm);
        game.addPermanent(perm, game.getState().getNextPermanentOrderNumber());
        perm.setZone(Zone.BATTLEFIELD, game);
    }

    private static List<Permanent> tokens(FakeGame game, UUID ctrl) {
        List<Permanent> out = new ArrayList<>();
        for (Permanent perm : game.getBattlefield().getAllPermanents()) {
            if (perm.isToken() && ctrl.equals(perm.getControllerId())) {
                out.add(perm);
            }
        }
        return out;
    }

    private static JsonObject token(String script) {
        JsonObject t = new JsonObject();
        t.addProperty("script", script);
        return t;
    }

    public static void main(String[] args) throws Exception {
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        ScenarioReplay driver = (ScenarioReplay) unsafe.allocateInstance(ScenarioReplay.class);

        TestPlayer p0 = player("PlayerA");
        UUID ctrl = p0.getId();
        set(driver, "playerA", p0);
        set(driver, "playerB", player("PlayerB"));
        // allocateInstance skips field initializers.
        set(driver, "seats", 2);

        FakeGame game = new FakeGame();
        game.getState().getPlayers().put(ctrl, REAL.get(ctrl));
        place(game, new MirkwoodBats(UUID.randomUUID(), new CardSetInfo("Mirkwood Bats", "TEST", "1", Rarity.RARE)), ctrl);
        place(game, new DoublingSeason(UUID.randomUUID(), new CardSetInfo("Doubling Season", "TEST", "2", Rarity.RARE)), ctrl);
        game.applyEffects();

        // Control: a created token is doubled, and a CREATED_TOKEN event (which
        // FakeGame does not deliver on its own) queues Mirkwood Bats.
        Ability source = new SimpleStaticAbility(Zone.OUTSIDE, new InfoEffect("control"));
        source.setControllerId(ctrl);
        new ClueArtifactToken().putOntoBattlefield(1, game, source, ctrl);
        game.applyEffects();
        equal(2, tokens(game, ctrl).size());
        game.fireEvent(new mage.game.events.CreatedTokenEvent(source, (mage.game.permanent.PermanentToken) tokens(game, ctrl).get(0)));
        check(!game.getState().getTriggered(ctrl).isEmpty(), "control: Mirkwood Bats did not trigger on a CREATED_TOKEN event");
        for (Permanent perm : tokens(game, ctrl)) {
            game.getBattlefield().removePermanent(perm.getId());
        }
        check(tokens(game, ctrl).isEmpty(), "control tokens removed");
        // getTriggered returns a copy: the control's trigger stays queued, so
        // the placement is held to adding none to it.
        int queued = game.getState().getTriggered(ctrl).size();

        // setup: p0 has a Treasure, then a Goblin with two +1/+1 counters.
        JsonObject goblin = token("r_1_1_goblin");
        JsonObject counters = new JsonObject();
        counters.addProperty("P1P1", 2);
        goblin.add("counters", counters);
        JsonArray list = new JsonArray();
        list.add(token("c_a_treasure_sac"));
        list.add(goblin);
        JsonObject p0setup = new JsonObject();
        p0setup.add("tokens", list);
        JsonObject setup = new JsonObject();
        setup.add("p0", p0setup);
        JsonObject sc0 = new JsonObject();
        sc0.add("setup", setup);
        set(driver, "sc0", sc0);

        Method method = ScenarioReplay.class.getDeclaredMethod("placeSetupTokens", mage.game.Game.class);
        method.setAccessible(true);
        method.invoke(driver, game);

        List<Permanent> placed = tokens(game, ctrl);
        equal(2, placed.size());
        int treasures = 0;
        Permanent gob = null;
        for (Permanent perm : placed) {
            if (perm.getName().startsWith("Treasure")) {
                treasures++;
            } else if (perm.getName().startsWith("Goblin")) {
                gob = perm;
            }
        }
        equal(1, treasures);
        check(gob != null, "the Goblin token is on the battlefield");
        equal(2, gob.getCounters(game).getCount(CounterType.P1P1));
        equal("3/3", gob.getPower().getValue() + "/" + gob.getToughness().getValue());
        check(!gob.hasSummoningSickness(), "the placed Goblin is summoning sick");
        equal(queued, game.getState().getTriggered(ctrl).size());

        // The driver's own bookkeeping: a mapped stem names its token.
        equal("Treasure Token", ScenarioReplay.setupToken("c_a_treasure_sac").getName());

        // A stem the table does not map fails the scenario.
        try {
            ScenarioReplay.setupToken("w_9_9_unmapped");
            throw new AssertionError("an unmapped stem did not fail");
        } catch (IllegalArgumentException expected) {
            // fails the scenario, as required
        }
        System.out.println("PASS setup tokens are placed silently, with their counters, not summoning sick");
    }
}
