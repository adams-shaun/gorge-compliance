package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.UUID;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.cards.b.BrambleElemental;
import mage.cards.g.GrizzlyBears;
import mage.cards.p.Pacifism;
import mage.cards.r.Rancor;
import mage.constants.Rarity;
import mage.constants.Zone;
import mage.game.FakeGame;
import mage.game.permanent.Permanent;
import mage.game.permanent.PermanentCard;
import mage.player.ai.ComputerPlayer;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Executable check of the driver's silent setup-attached link. No H2 or game
 * replay: the production placeSetupAttached runs against a real FakeGame. Run
 * scripts/xmage-oracle-test-setup-attach.sh.
 *
 * Two controls run first, so the later assertions are the link bypassing them
 * and not a FakeGame that never applies them: an Aura whose bearer is not
 * linked is put into the graveyard by checkStateBasedActions (CR 704.5m), and
 * Permanent.addAttachment queues Bramble Elemental's "whenever an Aura becomes
 * attached" trigger. */
public final class ScenarioReplaySetupAttachTest {
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

    /** The real ComputerPlayers behind the TestPlayers built by player(), by id. */
    private static final java.util.Map<UUID, ComputerPlayer> REAL = new java.util.HashMap<>();

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

    private static CardSetInfo info(String name, String n) {
        return new CardSetInfo(name, "TEST", n, Rarity.COMMON);
    }

    private static void sba(FakeGame game) throws Exception {
        Method m = mage.game.GameImpl.class.getDeclaredMethod("checkStateBasedActions");
        m.setAccessible(true);
        m.invoke(game);
    }

    private static FakeGame newGame(UUID ctrl) {
        FakeGame game = new FakeGame();
        game.getState().getPlayers().put(ctrl, REAL.get(ctrl));
        return game;
    }

    private static JsonObject attached(String card, String to) {
        JsonObject a = new JsonObject();
        a.addProperty("card", card);
        a.addProperty("to", to);
        return a;
    }

    /** A driver whose scenario seats p0's "attached" list and whose placements
     * are the given (ref, permanent) pairs. */
    private static ScenarioReplay driver(TestPlayer a, TestPlayer b, JsonObject entry, String[] refs, Permanent[] perms)
            throws Exception {
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        ScenarioReplay driver = (ScenarioReplay) unsafe.allocateInstance(ScenarioReplay.class);
        set(driver, "playerA", a);
        set(driver, "playerB", b);
        // allocateInstance skips field initializers.
        set(driver, "seats", 2);
        java.util.Map<String, UUID> ids = new java.util.HashMap<>();
        for (int i = 0; i < refs.length; i++) {
            ids.put(refs[i], perms[i].getId());
        }
        set(driver, "setupPermanentIds", ids);
        JsonArray list = new JsonArray();
        list.add(entry);
        JsonObject p0setup = new JsonObject();
        p0setup.add("attached", list);
        JsonObject setup = new JsonObject();
        setup.add("p0", p0setup);
        JsonObject sc0 = new JsonObject();
        sc0.add("setup", setup);
        set(driver, "sc0", sc0);
        return driver;
    }

    private static void place(ScenarioReplay driver, FakeGame game) throws Exception {
        Method method = ScenarioReplay.class.getDeclaredMethod("placeSetupAttached", mage.game.Game.class);
        method.setAccessible(true);
        method.invoke(driver, game);
    }

    public static void main(String[] args) throws Exception {
        TestPlayer p0 = player("PlayerA");
        TestPlayer p1 = player("PlayerB");
        UUID ctrl = p0.getId();

        // Control 1: an unlinked Pacifism does not survive the SBA check.
        FakeGame bare = newGame(ctrl);
        Permanent bareBears = place(bare, new GrizzlyBears(UUID.randomUUID(), info("Grizzly Bears", "1")), ctrl);
        Permanent barePacifism = place(bare, new Pacifism(UUID.randomUUID(), info("Pacifism", "2")), ctrl);
        bare.applyEffects();
        sba(bare);
        check(bare.getPermanent(barePacifism.getId()) == null, "control: an unattached Pacifism survived the SBA check");
        check(bare.getPermanent(bareBears.getId()) != null, "control: Grizzly Bears left the battlefield");

        // Control 2: addAttachment queues Bramble Elemental's trigger.
        FakeGame loud = newGame(ctrl);
        Permanent loudBramble = place(loud, new BrambleElemental(UUID.randomUUID(), info("Bramble Elemental", "3")), ctrl);
        Permanent loudRancor = place(loud, new Rancor(UUID.randomUUID(), info("Rancor", "4")), ctrl);
        loud.applyEffects();
        loudRancor.attachTo(loudBramble.getId(), null, loud);
        check(loudBramble.addAttachment(loudRancor.getId(), null, loud), "control: addAttachment refused the link");
        check(!loud.getState().getTriggered(ctrl).isEmpty(), "control: Bramble Elemental did not trigger on an attach");

        // Pacifism linked silently to Grizzly Bears survives the SBA check attached.
        FakeGame game = newGame(ctrl);
        Permanent bears = place(game, new GrizzlyBears(UUID.randomUUID(), info("Grizzly Bears", "5")), ctrl);
        Permanent pacifism = place(game, new Pacifism(UUID.randomUUID(), info("Pacifism", "6")), ctrl);
        game.applyEffects();
        place(driver(p0, p1, attached("Pacifism", "p0:Grizzly Bears"),
                new String[] {"p0:Grizzly Bears", "p0:Pacifism"}, new Permanent[] {bears, pacifism}), game);
        sba(game);
        check(game.getPermanent(pacifism.getId()) != null, "the linked Pacifism was put into the graveyard");
        equal(bears.getId(), game.getPermanent(pacifism.getId()).getAttachedTo());
        check(bears.getAttachments().contains(pacifism.getId()), "the bearer does not list the Pacifism");
        check(game.getState().getTriggered(ctrl).isEmpty(), "the link queued a trigger");

        // Rancor linked silently to Bramble Elemental leaves the queue empty.
        FakeGame quiet = newGame(ctrl);
        Permanent bramble = place(quiet, new BrambleElemental(UUID.randomUUID(), info("Bramble Elemental", "7")), ctrl);
        Permanent rancor = place(quiet, new Rancor(UUID.randomUUID(), info("Rancor", "8")), ctrl);
        quiet.applyEffects();
        place(driver(p0, p1, attached("Rancor", "p0:Bramble Elemental"),
                new String[] {"p0:Bramble Elemental", "p0:Rancor"}, new Permanent[] {bramble, rancor}), quiet);
        sba(quiet);
        check(quiet.getPermanent(rancor.getId()) != null, "the linked Rancor was put into the graveyard");
        equal(bramble.getId(), quiet.getPermanent(rancor.getId()).getAttachedTo());
        check(quiet.getState().getTriggered(ctrl).isEmpty(), "linking Rancor queued Bramble Elemental's trigger");

        // An unresolved ref fails the scenario.
        FakeGame lost = newGame(ctrl);
        Permanent lostBears = place(lost, new GrizzlyBears(UUID.randomUUID(), info("Grizzly Bears", "9")), ctrl);
        try {
            place(driver(p0, p1, attached("Pacifism", "p0:Grizzly Bears"),
                    new String[] {"p0:Grizzly Bears"}, new Permanent[] {lostBears}), lost);
            throw new AssertionError("an unresolved ref did not fail");
        } catch (java.lang.reflect.InvocationTargetException expected) {
            check(expected.getCause() instanceof IllegalArgumentException, "wrong failure " + expected.getCause());
        }
        System.out.println("PASS setup attached Auras are linked silently and survive state-based actions");
    }
}
