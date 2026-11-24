package org.mage.test.oracle;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.List;
import java.util.UUID;
import mage.MageObjectImpl;
import mage.abilities.Ability;
import mage.abilities.SpellAbility;
import mage.abilities.effects.AsThoughEffectImpl;
import mage.abilities.effects.AsThoughManaEffect;
import mage.abilities.costs.mana.ManaCost;
import mage.abilities.costs.mana.ManaCostsImpl;
import mage.cards.decks.Deck;
import mage.constants.AsThoughEffectType;
import mage.constants.Duration;
import mage.constants.MultiplayerAttackOption;
import mage.constants.Outcome;
import mage.constants.ManaType;
import mage.constants.PhaseStep;
import mage.constants.RangeOfInfluence;
import mage.game.Game;
import mage.game.TwoPlayerDuel;
import mage.game.mulligan.MulliganType;
import mage.players.ManaPool;
import mage.players.ManaPoolItem;
import mage.players.Player;
import org.mage.test.player.PlayerAction;
import org.mage.test.player.TestComputerPlayer;
import org.mage.test.player.TestPlayer;

/** Drives ScriptedChoicePlayer.playMana through a real {1}{G} cost payment,
 * with no game: the recorded multiset must decide the colour split, a
 * generic-only ask whose plan names no pool unit is payable by any colour, an
 * exhausted multiset or an unpayable specific pip must fall back to
 * auto-payment rather than fail the harness, and the replay's runCode payment
 * hooks must reach the ScriptedChoicePlayer even though TestPlayer hands their
 * payloads the inner computer player. Run scripts/xmage-oracle-test-payment.sh. */
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

    @SuppressWarnings("unchecked")
    private static void addRedWhite(ManaPool pool, int red, int white) throws Exception {
        Field f = ManaPool.class.getDeclaredField("manaItems");
        f.setAccessible(true);
        ((List<ManaPoolItem>) f.get(pool)).add(
                new ManaPoolItem(red, 0, 0, white, 0, 0, new Source(), UUID.randomUUID(), false));
    }

    private static ScriptedPayment newPlayer(Game game) throws Exception {
        return newPlayer(game, null);
    }

    /** With a real driver as the player's owner, for tests that run a cost's
     * target ask (the player's choose reads the owner's cast flags). */
    private static ScriptedPayment newPlayer(Game game, ScenarioReplay owner) throws Exception {
        Class<?> cls = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Constructor<?> ctor = cls.getDeclaredConstructor(TestComputerPlayer.class, ScenarioReplay.class);
        ctor.setAccessible(true);
        TestPlayer p = (TestPlayer) ctor.newInstance(
                new TestComputerPlayer("A", RangeOfInfluence.ONE), owner);
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

    /** Pay cost from the pool and return whether it was paid. */
    private static boolean pay(TestPlayer p, Game game, String cost) {
        ManaCostsImpl<ManaCost> costs = new ManaCostsImpl<>(cost);
        SpellAbility ability = new SpellAbility(costs, "Payment test");
        ability.setControllerId(p.getId());
        return costs.pay(ability, game, ability, p.getId(), false);
    }

    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    /** Execute a registered runCode action the way TestPlayer.priority does:
     * its payload is handed the player's INNER computer player
     * (TestPlayer.java's codePayload.run call), which is exactly the object
     * the old payment hooks wrongly cast to ScriptedChoicePlayer. */
    private static void runQueued(TestPlayer p, PlayerAction action, Game game) throws Exception {
        Field f = TestPlayer.class.getDeclaredField("computerPlayer");
        f.setAccessible(true);
        Player inner = (Player) f.get(p);
        action.getCodePayload().run(action.getActionName(), inner, game);
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
        equal(true, pay(p, game, "{1}{G}"));
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
        equal(true, pay(p, game, "{1}{G}"));
        equal("G", ScenarioReplay.pool(p.getManaPool()));
        sp.end();
        System.out.println("PASS recorded RG pays {1}{G} as gorge did, leaving G");
    }

    @SuppressWarnings("unchecked")
    private static void testRunCodeRecordedPayment() throws Exception {
        // The real replay shape: queuePaymentStart/queuePaymentEnd register
        // runCode actions on the TestPlayer wrapper, and TestPlayer dispatches
        // each payload with the wrapper's INNER computer player. The hooks must
        // therefore reach the ScriptedChoicePlayer through the captured
        // TestPlayer, not the payload argument. Before that fix this case threw
        // ClassCastException: TestComputerPlayer cannot be cast to
        // ScriptedChoicePlayer.
        Game game = game();
        ScriptedPayment sp = newPlayer(game);
        TestPlayer p = sp.player;
        addMana(p.getManaPool(), 1, 2);
        ScenarioReplay replay = new ScenarioReplay();

        Method start = ScenarioReplay.class.getDeclaredMethod(
                "queuePaymentStart", String.class, TestPlayer.class);
        start.setAccessible(true);
        int beforeStart = p.getActions().size();
        start.invoke(replay, "RG", p);
        equal(beforeStart + 1, p.getActions().size());
        PlayerAction begin = p.getActions().get(p.getActions().size() - 1);
        equal("payment RG", begin.getActionName());
        equal(PhaseStep.PRECOMBAT_MAIN, begin.getStep());
        runQueued(p, begin, game);
        equal(false, p.getManaPool().isAutoPayment());
        equal(true, pay(p, game, "{1}{G}"));
        equal("G", ScenarioReplay.pool(p.getManaPool()));

        Method end = ScenarioReplay.class.getDeclaredMethod(
                "queuePaymentEnd", TestPlayer.class);
        end.setAccessible(true);
        int beforeEnd = p.getActions().size();
        end.invoke(replay, p);
        equal(beforeEnd + 1, p.getActions().size());
        PlayerAction finish = p.getActions().get(p.getActions().size() - 1);
        equal("payment end", finish.getActionName());
        runQueued(p, finish, game);
        equal(true, p.getManaPool().isAutoPayment());
        System.out.println("PASS a recorded-payment cast through the runCode hooks follows gorge's split");
    }

    private static void testQueuedStepChoiceSurvivesPayment() throws Exception {
        // The step queues its own answers before the cast runs: a resolution
        // Yes/No, a skip marker, or an object pick. TestPlayer.playMana reads
        // choices.get(0), so manual payment must put its own colour answer in
        // front of that queue instead of appending it, or playMana consumes
        // (and Assert.fails on) the step's answer.
        for (String queued : new String[] {"Yes", TestPlayer.CHOICE_SKIP, "Bird"}) {
            Game game = game();
            ScriptedPayment sp = newPlayer(game);
            TestPlayer p = sp.player;
            addMana(p.getManaPool(), 1, 2);
            p.addChoice(queued);
            sp.begin("RG");
            equal(true, pay(p, game, "{1}{G}"));
            equal("G", ScenarioReplay.pool(p.getManaPool()));
            equal(1, p.getChoices().size());
            equal(queued, p.getChoices().get(0));
            sp.end();
        }
        System.out.println("PASS a queued step answer survives manual payment");
    }

    private static void testExhaustedMultisetFallsBack() throws Exception {
        // Pool R+G, gorge recorded one green. The first ask consumes it, so a
        // later ask in the same cost finds the multiset empty (payment is "",
        // not null). playMana must fall back instead of leaving TestPlayer in
        // manual mode with no choice, which Assert.fails the whole harness.
        Game game = game();
        ScriptedPayment sp = newPlayer(game);
        TestPlayer p = sp.player;
        addMana(p.getManaPool(), 1, 1);
        // Auto-payment consults the battlefield, which asks each player for
        // its range; fill it as beginTurn does in a started game.
        p.updateRange(game);
        sp.begin("G");
        // The generic ask cannot pay from the exhausted multiset. It must not
        // Assert.fail; the pool must be back on auto-payment. (Whether the
        // auto path can finish depends on the battlefield, not this fallback.)
        pay(p, game, "{1}{G}");
        equal(true, p.getManaPool().isAutoPayment());
        sp.end();
        System.out.println("PASS an exhausted recorded multiset falls back to auto-payment");
    }

    private static void testUnusableGenericPlanPaysFromPool() throws Exception {
        // Pool RR, gorge recorded GG for a generic-only {2}. No plan letter
        // names a pool unit, but the generic part accepts any colour: the
        // manual window pays RR rather than switching to auto-payment. This is
        // the dh2 plan-preference contract (and the shape the sweep fixed: a
        // dual land's chosen colour can leave the pool off the recorded plan).
        Game game = game();
        ScriptedPayment sp = newPlayer(game);
        TestPlayer p = sp.player;
        addMana(p.getManaPool(), 2, 0);
        p.updateRange(game);
        sp.begin("GG");
        equal(true, pay(p, game, "{2}"));
        equal(false, p.getManaPool().isAutoPayment());
        equal("", ScenarioReplay.pool(p.getManaPool()));
        sp.end();
        System.out.println("PASS a generic ask off the plan is paid from the pool, not auto-payment");
    }

    private static void testUnpayablePipFallsBack() throws Exception {
        // Pool RR, plan GG, cost {1}{G}: the specific {G} pip cannot be paid
        // by a red unit, so the window must fall back to auto-payment (which
        // may float a new source) instead of spending a unit the cost rejects.
        Game game = game();
        ScriptedPayment sp = newPlayer(game);
        TestPlayer p = sp.player;
        addMana(p.getManaPool(), 2, 0);
        p.updateRange(game);
        sp.begin("GG");
        pay(p, game, "{1}{G}");
        equal(true, p.getManaPool().isAutoPayment());
        sp.end();
        System.out.println("PASS an unpayable coloured pip falls back to auto-payment");
    }

    /** Vizier of the Menagerie's shape: any pool unit pays any pip. */
    private static final class AnyTypeSpend extends AsThoughEffectImpl implements AsThoughManaEffect {
        AnyTypeSpend() {
            super(AsThoughEffectType.SPEND_OTHER_MANA, Duration.EndOfGame, Outcome.Benefit);
        }

        private AnyTypeSpend(AnyTypeSpend e) {
            super(e);
        }

        @Override
        public boolean apply(Game game, Ability source) {
            return true;
        }

        @Override
        public AnyTypeSpend copy() {
            return new AnyTypeSpend(this);
        }

        @Override
        public boolean applies(java.util.UUID objectId, Ability source, java.util.UUID affected, Game game) {
            return true;
        }

        @Override
        public ManaType getAsThoughManaType(ManaType manaType, ManaPoolItem mana,
                java.util.UUID affected, Ability source, Game game) {
            return mana.getFirstAvailable();
        }
    }

    private static void testAnyTypeConversionPaysPip() throws Exception {
        // Pool RRR, plan RR, cost {1}{G}, with an any-type asThough effect:
        // the red units pay the green pip (CR 609.4b), so the driver must not
        // fall back to auto-payment (which taps a land for G) and the pool
        // keeps its one unspent R with no floated G.
        Game game = game();
        ScriptedPayment sp = newPlayer(game);
        TestPlayer p = sp.player;
        addMana(p.getManaPool(), 3, 0);
        p.updateRange(game);
        game.getContinuousEffects().addEffect(new AnyTypeSpend(),
                new mage.abilities.common.SimpleStaticAbility(new AnyTypeSpend()));
        sp.begin("RR");
        equal(true, pay(p, game, "{1}{G}"));
        equal("R", ScenarioReplay.pool(p.getManaPool()));
        equal(false, p.getManaPool().isAutoPayment());
        sp.end();
        System.out.println("PASS an any-type conversion pays the pip from the recorded RR, leaving R");
    }

    private static void testHybridPipFollowsPlan() throws Exception {
        // TMT Raph & Leo window: pool WWRR, cost {1}{R/W}{R/W}, gorge spent
        // WRR (both hybrid pips with R, the generic with W). XMage's hybrid
        // cost holds both halves, so the old WUBRG walk spent a floating W
        // for each hybrid pip and left R where gorge keeps W (CR 107.4e).
        Game game = game();
        ScriptedPayment sp = newPlayer(game);
        TestPlayer p = sp.player;
        addRedWhite(p.getManaPool(), 2, 2);
        p.updateRange(game);
        sp.begin("WRR");
        equal(true, pay(p, game, "{1}{R/W}{R/W}"));
        equal("W", ScenarioReplay.pool(p.getManaPool()));
        sp.end();
        System.out.println("PASS a hybrid pip is paid with the plan's half, leaving W");
    }

    private static void testTapPaymentPickParsing() throws Exception {
        Class<?> cls = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method pick = cls.getDeclaredMethod("tapPaymentPick", String.class);
        pick.setAccessible(true);
        equal("[Crimson Operative, 1]", java.util.Arrays.toString((String[]) pick.invoke(null, "Tap Crimson Operative for 1")));
        equal("[Hexing Squelcher, R]", java.util.Arrays.toString((String[]) pick.invoke(null, "Tap Hexing Squelcher for R")));
        for (String no : new String[] {"Tap Elves for mana", "Tap Giant Koi to waterbend for 1",
                "Tap Grizzly Bears (reduce by 2)", "Tap Grizzly Bears instead of paying 1", "Island", "Tap  for 1", null}) {
            if (pick.invoke(null, no) != null) {
                throw new AssertionError("label parsed as a payment tap: " + no);
            }
        }
        Method colour = cls.getDeclaredMethod("tapPaymentColourName", String.class);
        colour.setAccessible(true);
        equal("Red", colour.invoke(null, "R"));
        equal("Colorless", colour.invoke(null, "1"));
        System.out.println("PASS payment-tap labels parse to the permanent and its contribution");
    }

    /** An improvise-shaped special action (rule text "Improvise (...)", an
     * artifact target) with the recorded tap pick at the front of the choice
     * queue and the recorded multiset spent: playMana must translate the pick
     * to the action's choice key and target, keep manual payment, and take
     * the action, not fall back to auto-payment and leave the pick queued. */
    private static void testImproviseTapIsTakenAsSpecialAction() throws Exception {
        Game game = game();
        ScriptedPayment sp = newPlayer(game);
        TestPlayer p = sp.player;
        p.updateRange(game);
        mage.game.permanent.PermanentToken artifact = new mage.game.permanent.PermanentToken(
                new mage.game.permanent.token.TreasureToken(), p.getId(), game);
        game.addPermanent(artifact, 0);
        // The real improvise special action, built the way the cast's
        // payment cycle builds it for {4}{U}.
        ManaCostsImpl<ManaCost> costs = new ManaCostsImpl<>("{4}{U}");
        SpellAbility spell = new SpellAbility(costs, "Improvise test");
        spell.setControllerId(p.getId());
        new mage.abilities.keyword.ImproviseAbility().addSpecialAction(spell, game, costs);
        sp.begin("");
        p.addChoice("Tap Treasure Token for 1");
        p.addChoice(TestPlayer.CHOICE_SKIP);
        Method queue = p.getClass().getDeclaredMethod("queueTapPaymentAction", Game.class);
        queue.setAccessible(true);
        equal(true, queue.invoke(p, game));
        equal("[Improvise]", p.getChoices().toString());
        equal("[Treasure Token]", p.getTargets().toString());
        sp.end();
        System.out.println("PASS an improvise tap pick becomes the special action's choice key and target");
    }

    /** A plain card with a given name, enough to sit in a graveyard. */
    private static final class NamedCard extends mage.cards.CardImpl {
        NamedCard(UUID ownerId, String name) {
            super(ownerId, new mage.cards.CardSetInfo(name, "TST", "1", mage.constants.Rarity.COMMON),
                    new mage.constants.CardType[] {mage.constants.CardType.SORCERY}, "{B}");
        }

        private NamedCard(NamedCard card) {
            super(card);
        }

        @Override
        public NamedCard copy() {
            return new NamedCard(this);
        }
    }

    private static UUID addToGraveyard(Game game, TestPlayer p, String name) {
        mage.cards.Card card = new NamedCard(p.getId(), name);
        game.loadCards(new java.util.HashSet<>(java.util.Collections.singleton(card)), p.getId());
        game.getState().setZone(card.getId(), mage.constants.Zone.GRAVEYARD);
        p.getGraveyard().add(card);
        return card.getId();
    }

    /** The real Delve special action for a cost with its generic unpaid, built
     * the way the cast's payment cycle builds it. */
    private static mage.abilities.SpecialAction delveAction(Game game, TestPlayer p, String cost) {
        ManaCostsImpl<ManaCost> costs = new ManaCostsImpl<>(cost);
        SpellAbility spell = new SpellAbility(costs, "Delve test");
        spell.setControllerId(p.getId());
        new mage.abilities.keyword.DelveAbility(false).addSpecialAction(spell, game, costs);
        return game.getState().getSpecialActions().getControlledBy(p.getId(), true)
                .values().iterator().next();
    }

    /** The recorded Delve exile pick, the bare card name gorge's choose_n
     * records, at the front of the choice queue with the ask's trailing
     * [choice_skip]: the hook must translate it to the Delve action's choice
     * key (its own rule text: TestPlayer matches special actions by rule
     * prefix and the Delve rule leads with its exile cost) and the card to
     * its target, and activating that action must exile the card. A name
     * outside the graveyard, or no Delve action, must queue nothing. */
    private static void testDelveExilePickIsTakenAsSpecialAction() throws Exception {
        Game game = game();
        ScriptedPayment sp = newPlayer(game, new ScenarioReplay());
        TestPlayer p = sp.player;
        p.updateRange(game);
        UUID strength = addToGraveyard(game, p, "Grave Strength");
        addToGraveyard(game, p, "Other Card");
        Method queue = p.getClass().getDeclaredMethod("queueTapPaymentAction", Game.class);
        queue.setAccessible(true);
        // No Delve action yet: nothing to take, nothing consumed.
        sp.begin("");
        p.addChoice("Grave Strength");
        equal(false, queue.invoke(p, game));
        equal("[Grave Strength]", p.getChoices().toString());
        p.getChoices().clear();
        mage.abilities.SpecialAction action = delveAction(game, p, "{1}{B}{B}{B}{B}");
        // A name that is no graveyard card queues nothing.
        p.addChoice("Not In Graveyard");
        equal(false, queue.invoke(p, game));
        equal("[Not In Graveyard]", p.getChoices().toString());
        p.getChoices().clear();
        p.addChoice("Grave Strength");
        p.addChoice(TestPlayer.CHOICE_SKIP);
        equal(true, queue.invoke(p, game));
        equal("[" + action.getRule(true) + ", Grave Strength]", p.getChoices().toString());
        equal("[]", p.getTargets().toString());
        // The key is one TestPlayer's manual special-action branch takes.
        if (!action.getRule(true).startsWith(p.getChoices().get(0))) {
            throw new AssertionError("choice key is not a rule prefix");
        }
        // TestPlayer's manual branch drops the key, then activates the action,
        // whose exile cost reads the queued name from the choice queue. (activate, not
        // activateAbility: resolving Delve's effect needs the spell on the
        // stack, and the cost payment is what exiles the card.)
        p.getChoices().remove(0);
        action.setControllerId(p.getId());
        equal(true, action.activate(game, false));
        equal(false, p.getGraveyard().contains(strength));
        sp.end();
        System.out.println("PASS a delve exile pick becomes the special action's choice key and card name");
    }

    /** A shortfall of two with two recorded picks: XMage's manual payment
     * takes one graveyard card per activation, so the hook consumes one pick
     * and leaves the second queued for the next payment cycle (gorge re-poses
     * its ask per pick) rather than queueing a name no target ask takes. */
    private static void testDelveTwoPicksTakeOnePerActivation() throws Exception {
        Game game = game();
        ScriptedPayment sp = newPlayer(game, new ScenarioReplay());
        TestPlayer p = sp.player;
        p.updateRange(game);
        UUID first = addToGraveyard(game, p, "Grave Strength");
        UUID second = addToGraveyard(game, p, "Other Card");
        Method queue = p.getClass().getDeclaredMethod("queueTapPaymentAction", Game.class);
        queue.setAccessible(true);
        sp.begin("");
        mage.abilities.SpecialAction action = delveAction(game, p, "{2}{B}");
        p.addChoice("Grave Strength");
        p.addChoice("Other Card");
        p.addChoice(TestPlayer.CHOICE_SKIP);
        equal(true, queue.invoke(p, game));
        equal("[" + action.getRule(true) + ", Grave Strength, Other Card, " + TestPlayer.CHOICE_SKIP + "]",
                p.getChoices().toString());
        equal("[]", p.getTargets().toString());
        p.getChoices().remove(0);
        action.setControllerId(p.getId());
        equal(true, action.activate(game, false));
        equal(false, p.getGraveyard().contains(first));
        equal(true, p.getGraveyard().contains(second));
        // Next cycle: the action is re-added and takes the second pick.
        equal("[Other Card, " + TestPlayer.CHOICE_SKIP + "]", p.getChoices().toString());
        game.getState().getSpecialActions().clear();
        mage.abilities.SpecialAction again = delveAction(game, p, "{1}{B}");
        equal(true, queue.invoke(p, game));
        equal("[" + again.getRule(true) + ", Other Card]", p.getChoices().toString());
        sp.end();
        System.out.println("PASS a delve action takes one recorded pick per activation");
    }

    public static void main(String[] args) throws Exception {
        testTapPaymentPickParsing();
        testImproviseTapIsTakenAsSpecialAction();
        testDelveExilePickIsTakenAsSpecialAction();
        testDelveTwoPicksTakeOnePerActivation();
        testRecordedSplit();
        testGorgeSplit();
        testRunCodeRecordedPayment();
        testQueuedStepChoiceSurvivesPayment();
        testExhaustedMultisetFallsBack();
        testUnusableGenericPlanPaysFromPool();
        testUnpayablePipFallsBack();
        testAnyTypeConversionPaysPip();
        testHybridPipFollowsPlan();
    }
}
