package org.mage.test.oracle;

import java.lang.reflect.Proxy;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import mage.abilities.Ability;
import mage.game.Game;
import mage.game.permanent.Permanent;
import org.mage.test.player.PlayerAction;

/** No H2: prove that combat refs resolve to the recorded object, not just a
 * name and a live rank, and that the per-step attack/block batches declare
 * those objects and consume their queued commands. */
public final class ScenarioReplaySameNameChoiceTest {
    private static Permanent object(UUID controller, boolean token, int order) {
        return object(controller, token, order, "Grizzly Bears");
    }

    private static Permanent object(UUID controller, boolean token, int order, String name) {
        return object(controller, token, order, name, false, false);
    }

    /** A fixture permanent with the live tap/attack state the combat commands
     * rank on. Tapped copies are what made XMage's "Name:k" index of a
     * same-name attacker diverge from the driver's setup ordinal. */
    private static Permanent object(UUID controller, boolean token, int order, String name,
            boolean tapped, boolean attacking) {
        UUID id = UUID.randomUUID();
        return (Permanent) Proxy.newProxyInstance(Permanent.class.getClassLoader(),
                new Class<?>[]{Permanent.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getId": return id;
                        case "getControllerId": return controller;
                        case "getName": return name;
                        case "isToken": return token;
                        case "getCreateOrder": return order;
                        // The target fixtures run XMage's own filter/possible
                        // set over this proxy, which asks these; the combat
                        // tests also let TestPlayer's name-command path see a
                        // legal attacker when a revert leaves commands queued.
                        case "isPhasedIn": return true;
                        case "isCopy": return false;
                        case "isControlledBy": return controller.equals(args[0]);
                        case "getCardType": return List.of(mage.constants.CardType.CREATURE);
                        case "isCreature": return true;
                        case "isTapped": return tapped;
                        case "isAttacking": return attacking;
                        case "hasSummoningSickness": return false;
                        case "getMaxBlocks": return 1;
                        case "getBlocking": return 0;
                        case "canAttack": return !tapped && !attacking;
                        case "getExpansionSetCode": return "TST";
                        // A plain card is its own main card; the bound-id
                        // resolver checks that identity for cast DFCs.
                        case "getMainCard": return proxy;
                        default: throw new AssertionError("unexpected call " + method);
                    }
                });
    }

    private static void chosen(Permanent want, Permanent got) {
        if (want != got) {
            throw new AssertionError("choice selected the wrong object");
        }
    }

    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    /** Attack and block both go through these two helpers. A recorded
     * "token:" ref must resolve to the token, not a same-name card; a card
     * ref resolves to the object the driver bound to it. The name-only XMage
     * command is ambiguous only when both kinds are present under the same
     * controller. */
    private static void combatRefs() {
        UUID own = UUID.randomUUID(), other = UUID.randomUUID();
        Permanent card = object(own, false, 1);
        Permanent first = object(own, true, 2);
        Permanent second = object(own, true, 3);
        Permanent opponentCard = object(other, false, 4);
        Permanent card2 = object(own, false, 5);
        List<Permanent> battlefield = List.of(second, opponentCard, card2, card, first);
        // The attacker path: the recorded token ref names the token, and the
        // recorded card ref names the exact bound card, even though both are
        // "Grizzly Bears" and battlefield iteration order is not creation
        // order.
        chosen(first, ScenarioReplay.combatPermanent("p0:token:Grizzly Bears", "Grizzly Bears", own, null, battlefield));
        chosen(second, ScenarioReplay.combatPermanent("p0:token:Grizzly Bears#2", "Grizzly Bears", own, null, battlefield));
        chosen(card, ScenarioReplay.combatPermanent("p0:Grizzly Bears", "Grizzly Bears", own, card.getId(), battlefield));
        chosen(card2, ScenarioReplay.combatPermanent("p0:Grizzly Bears#2", "Grizzly Bears", own, card2.getId(), battlefield));
        // A card ref's "#2" is the setup/objRef ordinal ACROSS ZONES, not a
        // live battlefield rank. After the first same-name setup object has
        // left the battlefield, "#2" is still the bound object -- and when
        // that object is gone it names nothing, rather than falling back to
        // the surviving same-name card.
        Permanent reanimated = object(own, false, 10);
        Permanent played = object(own, false, 11);
        Permanent survivorA = object(own, false, 12);
        Permanent survivorB = object(own, false, 13);
        chosen(reanimated, ScenarioReplay.combatPermanent("p0:Grizzly Bears#2", "Grizzly Bears", own,
                reanimated.getId(), List.of(first, reanimated, played)));
        // The bound #2 object has left the battlefield: two other same-name
        // cards are live, but neither is the recorded object and a live-rank
        // fallback would silently pick one.
        chosen(null, ScenarioReplay.combatPermanent("p0:Grizzly Bears#2", "Grizzly Bears", own,
                reanimated.getId(), List.of(first, survivorA, survivorB)));
        // The blocker path uses the same helpers; an ambiguous pair is one
        // where the recorded side is a token and the same controller also has
        // a card of that name, or the reverse.
        check(ScenarioReplay.combatRefAmbiguous("Grizzly Bears", own, battlefield),
                "a same-controller token and card of one name are ambiguous");
        check(!ScenarioReplay.combatRefAmbiguous("Grizzly Bears", own, List.of(first, second)),
                "two tokens of one name are distinguishable by the command's :k index");
        check(!ScenarioReplay.combatRefAmbiguous("Grizzly Bears", own, List.of(card, card2)),
                "two cards of one name are distinguishable by the command's :k index");
        check(!ScenarioReplay.combatRefAmbiguous("Grizzly Bears", other, List.of(opponentCard, first)),
                "a card on another seat does not collide with this seat's token");
        // A token copy of the renamed card under test carries XMage's
        // spelling ("Dain Ironfoot") while the scenario ref keeps the
        // corpus spelling ("Dáin Ironfoot").
        Permanent renamed = object(own, true, 6, "Dain Ironfoot");
        chosen(renamed, ScenarioReplay.combatPermanent("p0:token:Dáin Ironfoot", "Dain Ironfoot", own, null,
                List.of(renamed)));
        System.out.println("PASS token:/card combat refs resolve exactly and detect a token/card name collision");
    }

    private static void set(Object target, Class<?> type, String name, Object value) throws Exception {
        java.lang.reflect.Field f = type.getDeclaredField(name);
        f.setAccessible(true);
        f.set(target, value);
    }

    private static Object get(Object target, Class<?> type, String name) throws Exception {
        java.lang.reflect.Field f = type.getDeclaredField(name);
        f.setAccessible(true);
        return f.get(target);
    }

    private static sun.misc.Unsafe unsafe() throws Exception {
        java.lang.reflect.Field f = sun.misc.Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        return (sun.misc.Unsafe) f.get(null);
    }

    /** A ScenarioReplay instance without its H2-opening constructor, with the
     * per-scenario state the exact-combat path reads. */
    private static ScenarioReplay newOwner() throws Exception {
        ScenarioReplay owner = (ScenarioReplay) unsafe().allocateInstance(ScenarioReplay.class);
        set(owner, ScenarioReplay.class, "refAlias", new LinkedHashMap<String, String>());
        set(owner, ScenarioReplay.class, "exactAttacks", new ArrayList<>());
        set(owner, ScenarioReplay.class, "exactBlocks", new ArrayList<>());
        set(owner, ScenarioReplay.class, "gorgeName", "");
        set(owner, ScenarioReplay.class, "xmageName", "");
        set(owner, ScenarioReplay.class, "backFaceNames", new LinkedHashMap<String, String>());
        set(owner, ScenarioReplay.class, "frontCounts", new LinkedHashMap<String, Integer>());
        set(owner, ScenarioReplay.class, "setupNames", new LinkedHashMap<UUID, String>());
        set(owner, ScenarioReplay.class, "placedNames", new LinkedHashMap<UUID, String>());
        return owner;
    }

    /** The production player wrapper, with its AI delegate swapped for the
     * recorder so declareAttacker/declareBlocker do not need a full game. */
    private static org.mage.test.player.TestPlayer scripted(ScenarioReplay owner,
            mage.player.ai.ComputerPlayer computer) throws Exception {
        Class<?> type = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        java.lang.reflect.Constructor<?> ctor = type.getDeclaredConstructor(
                org.mage.test.player.TestComputerPlayer.class, ScenarioReplay.class);
        ctor.setAccessible(true);
        org.mage.test.player.TestPlayer player = (org.mage.test.player.TestPlayer) ctor.newInstance(
                new org.mage.test.player.TestComputerPlayer("PlayerA", mage.constants.RangeOfInfluence.ALL), owner);
        set(player, org.mage.test.player.TestPlayer.class, "computerPlayer", computer);
        return player;
    }

    private static void seat(ScenarioReplay owner, String field, org.mage.test.player.TestPlayer player)
            throws Exception {
        for (Class<?> c = ScenarioReplay.class; c != null; c = c.getSuperclass()) {
            try {
                set(owner, c, field, player);
                return;
            } catch (NoSuchFieldException absent) { }
        }
        throw new AssertionError("no " + field + " field");
    }

    /** Records declarations and feeds them back to the combat object the
     * production code inspects right after declaring. */
    private static final class RecordingComputerPlayer extends mage.player.ai.ComputerPlayer {
        final List<UUID> attackers = new ArrayList<>();
        final List<UUID> blockers = new ArrayList<>();

        RecordingComputerPlayer(String name) {
            super(name, mage.constants.RangeOfInfluence.ALL);
        }

        @Override
        public void declareAttacker(UUID attackerId, UUID defenderId, Game game, boolean allowUndo) {
            attackers.add(attackerId);
            ((RecordingCombat) game.getCombat()).attackers.add(attackerId);
        }

        @Override
        public void declareBlocker(UUID defenderId, UUID blockerId, UUID attackerId, Game game, boolean allowUndo) {
            blockers.add(blockerId);
            ((RecordingCombat) game.getCombat()).group().getBlockers().add(blockerId);
        }
    }

    /** A Combat whose declared state the production code's post-declaration
     * checks can read without an engine. */
    private static final class RecordingCombat extends mage.game.combat.Combat {
        final Set<UUID> attackers = new LinkedHashSet<>();
        private final UUID attackingPlayerId;
        private final mage.game.combat.CombatGroup group =
                new mage.game.combat.CombatGroup(UUID.randomUUID(), false, UUID.randomUUID());

        RecordingCombat(UUID attackingPlayerId) {
            this.attackingPlayerId = attackingPlayerId;
        }

        mage.game.combat.CombatGroup group() {
            return group;
        }

        @Override
        public Set<UUID> getAttackers() {
            return attackers;
        }

        @Override
        public UUID getAttackingPlayerId() {
            return attackingPlayerId;
        }

        @Override
        public mage.game.combat.CombatGroup findGroup(UUID attackerId) {
            return group;
        }
    }

    private static Game game(mage.game.permanent.Battlefield board, RecordingCombat combat, int turn) {
        return (Game) Proxy.newProxyInstance(Game.class.getClassLoader(),
                new Class<?>[]{Game.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getBattlefield": return board;
                        case "getCombat": return combat;
                        case "getTurnNum": return turn;
                        // Reached only by a strict miss's diagnostic; this
                        // stub game has no turn step, which it only null-checks.
                        case "getStep": return null;
                        default: throw new AssertionError("unexpected game call " + method);
                    }
                });
    }

    private static PlayerAction action(String text) {
        return new PlayerAction("combat", 1, mage.constants.PhaseStep.DECLARE_ATTACKERS, text);
    }

    private static Object attackBatch(int turn, int seat, int defenderSeat,
            List<String> refs, List<PlayerAction> actions) throws Exception {
        Class<?> type = Class.forName("org.mage.test.oracle.ScenarioReplay$ExactAttackBatch");
        java.lang.reflect.Constructor<?> ctor = type.getDeclaredConstructor(int.class, int.class, int.class);
        Object batch = ctor.newInstance(turn, seat, defenderSeat);
        set(batch, type, "refs", new ArrayList<>(refs));
        set(batch, type, "actions", new ArrayList<>(actions));
        return batch;
    }

    private static Object blockBatch(int turn, int seat, List<String> blockers, List<String> attackers,
            List<PlayerAction> actions) throws Exception {
        Class<?> type = Class.forName("org.mage.test.oracle.ScenarioReplay$ExactBlockBatch");
        java.lang.reflect.Constructor<?> ctor = type.getDeclaredConstructor(int.class, int.class);
        Object batch = ctor.newInstance(turn, seat);
        set(batch, type, "blockers", new ArrayList<>(blockers));
        set(batch, type, "attackers", new ArrayList<>(attackers));
        set(batch, type, "actions", new ArrayList<>(actions));
        return batch;
    }

    /** Exercise the production attack/block path at the declaration point:
     * selectAttackers/selectBlockers -> per-step batch -> queued-command
     * removal -> declareAttacker/declareBlocker. A token ref beside a
     * same-name card must declare the token (and vice versa), not whichever
     * permanent XMage's name command happens to find first. */
    @SuppressWarnings("unchecked")
    private static void exactCombatBatches() throws Exception {
        ScenarioReplay owner = newOwner();
        RecordingComputerPlayer computer = new RecordingComputerPlayer("PlayerA");
        org.mage.test.player.TestPlayer player = scripted(owner, computer);
        seat(owner, "playerA", player);
        seat(owner, "playerB", player);
        UUID own = player.getId();

        Permanent card = object(own, false, 20);
        Permanent card2 = object(own, false, 21);
        Permanent token = object(own, true, 22);
        Permanent token2 = object(own, true, 23);
        Permanent hero = object(own, false, 24, "Benalish Hero");
        mage.game.permanent.Battlefield board = new mage.game.permanent.Battlefield();
        board.addPermanent(card2);
        board.addPermanent(token2);
        board.addPermanent(card);
        board.addPermanent(token);
        // Bind the setup card refs the way registerAliases does: "#2" is the
        // second setup object, which need not be the live second object.
        ((Map<String, String>) get(owner, ScenarioReplay.class, "refAlias"))
                .put("p0:Grizzly Bears", "@p0:Grizzly Bears");
        ((Map<String, String>) get(owner, ScenarioReplay.class, "refAlias"))
                .put("p0:Grizzly Bears#2", "@p0:Grizzly Bears#2");
        player.addAlias("p0:Grizzly Bears", card.getId());
        player.addAlias("p0:Grizzly Bears#2", card2.getId());

        // Attack: the recorded refs are a token and a same-name card. Both
        // commands must be consumed and both objects declared exactly.
        PlayerAction tokenAttack = action("attack:Grizzly Bears");
        PlayerAction cardAttack = action("attack:Grizzly Bears:1");
        player.addAction(tokenAttack);
        player.addAction(cardAttack);
        RecordingCombat attackCombat = new RecordingCombat(own);
        Game attackGame = game(board, attackCombat, 1);
        ((List<Object>) get(owner, ScenarioReplay.class, "exactAttacks")).add(attackBatch(
                1, 0, 1, List.of("p0:token:Grizzly Bears", "p0:Grizzly Bears#2"),
                List.of(tokenAttack, cardAttack)));
        player.selectAttackers(attackGame, own);
        check(computer.attackers.equals(List.of(token.getId(), card2.getId())),
                "attack batch declared the recorded token and card exactly, got " + computer.attackers);
        check(player.getActions().isEmpty(), "attack batch removed both queued name commands");

        // Two same-name setup cards, and no token under the name: the
        // pre-existing exact path only fired for a token/card collision, so
        // this case pins the broadening to same-name cards. The step's ref
        // order must be the exact declaration order, not the
        // highest-index-first order the name commands need. A band's leader
        // follows declaration order, so the first copy stays the raw
        // "@p0:Grizzly Bears" member instead of becoming the leader.
        computer.attackers.clear();
        PlayerAction secondCardAttack = action("attack:Grizzly Bears:1");
        PlayerAction firstCardAttack = action("attack:Grizzly Bears");
        player.addAction(secondCardAttack);
        player.addAction(firstCardAttack);
        mage.game.permanent.Battlefield cardBoard = new mage.game.permanent.Battlefield();
        cardBoard.addPermanent(card);
        cardBoard.addPermanent(card2);
        RecordingCombat sameNameCombat = new RecordingCombat(own);
        Game sameNameGame = game(cardBoard, sameNameCombat, 1);
        ((List<Object>) get(owner, ScenarioReplay.class, "exactAttacks")).add(attackBatch(
                1, 0, 1, List.of("p0:Grizzly Bears", "p0:Grizzly Bears#2"),
                List.of(firstCardAttack, secondCardAttack)));
        player.selectAttackers(sameNameGame, own);
        check(computer.attackers.equals(List.of(card.getId(), card2.getId())),
                "same-name attack batch declared the step's order, got " + computer.attackers);
        check(player.getActions().isEmpty(), "same-name attack batch removed both queued name commands");

        // Mixed batch: a unique attacker declared before a same-name pair.
        // The exact path must declare every resolvable ref in step order, not
        // jump the same-name pair ahead of the unique attacker and consume
        // that attacker's recorded band answer.
        computer.attackers.clear();
        player.addAlias("p0:Benalish Hero", hero.getId());
        ((Map<String, String>) get(owner, ScenarioReplay.class, "refAlias"))
                .put("p0:Benalish Hero", "@p0:Benalish Hero");
        mage.game.permanent.Battlefield mixedBoard = new mage.game.permanent.Battlefield();
        mixedBoard.addPermanent(card);
        mixedBoard.addPermanent(hero);
        mixedBoard.addPermanent(card2);
        PlayerAction heroAttack = action("attack:Benalish Hero");
        PlayerAction mixedSecond = action("attack:Grizzly Bears:1");
        PlayerAction mixedFirst = action("attack:Grizzly Bears");
        player.addAction(heroAttack);
        player.addAction(mixedSecond);
        player.addAction(mixedFirst);
        RecordingCombat mixedCombat = new RecordingCombat(own);
        Game mixedGame = game(mixedBoard, mixedCombat, 1);
        ((List<Object>) get(owner, ScenarioReplay.class, "exactAttacks")).add(attackBatch(
                1, 0, 1, List.of("p0:Benalish Hero", "p0:Grizzly Bears", "p0:Grizzly Bears#2"),
                List.of(heroAttack, mixedFirst, mixedSecond)));
        player.selectAttackers(mixedGame, own);
        check(computer.attackers.equals(List.of(hero.getId(), card.getId(), card2.getId())),
                "mixed attack batch declared every ref in step order, got " + computer.attackers);
        check(player.getActions().isEmpty(), "mixed attack batch removed every queued name command");

        // Block: the token is the blocker in one pair and the attacker in the
        // other. Both sides resolve through the same helper.
        PlayerAction cardBlocks = action("block:Grizzly Bears$Grizzly Bears");
        PlayerAction tokenBlocks = action("block:Grizzly Bears:1$Grizzly Bears:1");
        player.addAction(cardBlocks);
        player.addAction(tokenBlocks);
        RecordingCombat blockCombat = new RecordingCombat(own);
        Game blockGame = game(board, blockCombat, 1);
        ((List<Object>) get(owner, ScenarioReplay.class, "exactBlocks")).add(blockBatch(
                1, 0, List.of("p0:Grizzly Bears", "p0:token:Grizzly Bears#2"),
                List.of("p0:token:Grizzly Bears", "p0:Grizzly Bears#2"),
                List.of(cardBlocks, tokenBlocks)));
        player.selectBlockers(null, blockGame, own);
        check(computer.blockers.equals(List.of(card.getId(), token2.getId())),
                "block batch declared the recorded card and token exactly, got " + computer.blockers);
        check(blockCombat.group().getBlockers().contains(card.getId())
                && blockCombat.group().getBlockers().contains(token2.getId()),
                "block batch registered both declarations with the combat group");
        check(player.getActions().isEmpty(), "block batch removed both queued name commands");
        System.out.println("PASS exact attack/block batches declare token:/card refs and consume their commands");
    }

    /** Life and Limb makes p0's five Forests 1/1 Saproling creatures. Four
     * were tapped for the mana to cast it, so only the fifth is an untapped
     * creature. XMage's "Name:k" attack command ranks only untapped creatures,
     * while the recorded ref's "#5" ranks all five same-name permanents:
     * "Forest:4" names nothing and the recorded attacker is a strict miss.
     * The exact batch must resolve the bound object and declare it. */
    @SuppressWarnings("unchecked")
    private static void attackWithUntappedFifthForestAfterFourTapped() throws Exception {
        ScenarioReplay owner = newOwner();
        RecordingComputerPlayer computer = new RecordingComputerPlayer("PlayerA");
        org.mage.test.player.TestPlayer player = scripted(owner, computer);
        seat(owner, "playerA", player);
        seat(owner, "playerB", player);
        UUID own = player.getId();
        String name = "Forest";
        List<Permanent> forests = new ArrayList<>();
        mage.game.permanent.Battlefield board = new mage.game.permanent.Battlefield();
        for (int i = 1; i <= 5; i++) {
            // The first four were tapped to pay for Life and Limb.
            Permanent forest = object(own, false, 40 + i, name, i <= 4, false);
            forests.add(forest);
            board.addPermanent(forest);
        }
        Map<String, String> refAlias = (Map<String, String>) get(owner, ScenarioReplay.class, "refAlias");
        for (int i = 1; i <= 5; i++) {
            String ref = "p0:" + name + (i == 1 ? "" : "#" + i);
            refAlias.put(ref, "@" + ref);
            player.addAlias(ref, forests.get(i - 1).getId());
        }
        PlayerAction attack = action("attack:Forest:4");
        player.addAction(attack);
        RecordingCombat combat = new RecordingCombat(own);
        Game game = game(board, combat, 1);
        ((List<Object>) get(owner, ScenarioReplay.class, "exactAttacks")).add(attackBatch(
                1, 0, 1, List.of("p0:Forest#5"), List.of(attack)));
        // Strict mode is the reported failure: a name command that finds no
        // attacker must fail rather than quietly declare nothing.
        set(player, org.mage.test.player.TestPlayer.class, "strictChooseMode", true);
        player.selectAttackers(game, own);
        check(computer.attackers.equals(List.of(forests.get(4).getId())),
                "the untapped fifth Forest was the recorded attacker, got " + computer.attackers);
        check(player.getActions().isEmpty(), "the Forest:4 name command was consumed by the exact declaration");
        System.out.println("PASS a same-name attacker past four tapped copies is declared exactly, not by rank");
    }

    /** The blocker mirror: p0 controls two Grizzly Bears but only the second
     * is attacking. XMage ranks a block's attacker among the currently
     * attacking same-name creatures, so "Grizzly Bears:1" names nothing.
     * The pair must be declared from the bound objects. */
    @SuppressWarnings("unchecked")
    private static void blockSecondOfTwoSameNameAttackers() throws Exception {
        ScenarioReplay owner = newOwner();
        RecordingComputerPlayer computer = new RecordingComputerPlayer("PlayerA");
        org.mage.test.player.TestPlayer player = scripted(owner, computer);
        seat(owner, "playerA", player);
        seat(owner, "playerB", player);
        UUID own = player.getId();
        Permanent hero = object(own, false, 50, "Benalish Hero");
        Permanent idle = object(own, false, 51, "Grizzly Bears");
        Permanent attacking = object(own, false, 52, "Grizzly Bears", true, true);
        mage.game.permanent.Battlefield board = new mage.game.permanent.Battlefield();
        board.addPermanent(hero);
        board.addPermanent(idle);
        board.addPermanent(attacking);
        Map<String, String> refAlias = (Map<String, String>) get(owner, ScenarioReplay.class, "refAlias");
        for (String ref : List.of("p0:Benalish Hero", "p0:Grizzly Bears", "p0:Grizzly Bears#2")) {
            refAlias.put(ref, "@" + ref);
        }
        player.addAlias("p0:Benalish Hero", hero.getId());
        player.addAlias("p0:Grizzly Bears", idle.getId());
        player.addAlias("p0:Grizzly Bears#2", attacking.getId());
        PlayerAction block = action("block:Benalish Hero$Grizzly Bears:1");
        player.addAction(block);
        RecordingCombat combat = new RecordingCombat(own);
        combat.attackers.add(attacking.getId());
        Game game = game(board, combat, 1);
        ((List<Object>) get(owner, ScenarioReplay.class, "exactBlocks")).add(blockBatch(
                1, 0, List.of("p0:Benalish Hero"), List.of("p0:Grizzly Bears#2"),
                List.of(block)));
        player.selectBlockers(null, game, own);
        check(computer.blockers.equals(List.of(hero.getId())),
                "the recorded blocker was declared exactly, got " + computer.blockers);
        check(combat.group().getBlockers().contains(hero.getId()),
                "the exact block registered with the attacking group");
        check(player.getActions().isEmpty(), "the Grizzly Bears:1 name command was consumed");
        System.out.println("PASS a blocker naming the second attacking same-name copy is declared exactly");
    }

    /** Sisters of Stone Death forces a block: XMage declares it before asking
     * the defender, so the scripted block of the same pair must not reach
     * TestPlayer (which throws "already blocking the maximum amount"). A pair
     * XMage did not declare stays queued. */
    private static void autoDeclaredBlock() throws Exception {
        ScenarioReplay owner = newOwner();
        RecordingComputerPlayer computer = new RecordingComputerPlayer("PlayerA");
        org.mage.test.player.TestPlayer player = scripted(owner, computer);
        seat(owner, "playerA", player);
        seat(owner, "playerB", player);
        UUID own = player.getId();
        UUID opponent = UUID.randomUUID();
        Permanent kobolds = object(own, false, 30, "Kobolds of Kher Keep");
        Permanent bears = object(own, false, 31, "Grizzly Bears");
        Permanent sisters = object(opponent, false, 32, "Sisters of Stone Death");
        mage.game.permanent.Battlefield board = new mage.game.permanent.Battlefield();
        board.addPermanent(kobolds);
        board.addPermanent(bears);
        board.addPermanent(sisters);
        RecordingCombat combat = new RecordingCombat(opponent);
        combat.attackers.add(sisters.getId());
        combat.group().getBlockers().add(kobolds.getId()); // declared by the requirement
        Game game = game(board, combat, 1);

        PlayerAction forced = action("block:Kobolds of Kher Keep$Sisters of Stone Death");
        player.addAction(forced);
        player.selectBlockers(null, game, own);
        check(player.getActions().isEmpty(), "the scripted block of the auto-declared pair was dropped");
        check(computer.blockers.isEmpty(), "no second declaration was made for the auto-declared pair");
        check(combat.group().getBlockers().equals(List.of(kobolds.getId())),
                "the blocker blocks once, got " + combat.group().getBlockers());

        PlayerAction other = action("block:Grizzly Bears$Sisters of Stone Death");
        player.addAction(forced);
        player.addAction(other);
        java.lang.reflect.Method drop = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer")
                .getDeclaredMethod("dropAutoDeclaredBlocks", Game.class);
        drop.setAccessible(true);
        drop.invoke(player, game);
        check(player.getActions().equals(List.of(other)),
                "a block XMage did not declare stays queued, got " + player.getActions());
        System.out.println("PASS a scripted block XMage already auto-declared is dropped, others stay queued");
    }

    /** Exercise the production name matcher, including a pre-bound STALE
     * alias. Only the choice path may override that alias by live position. */
    private static void liveMatcher() throws Exception {
        ScenarioReplay owner = (ScenarioReplay) unsafe().allocateInstance(ScenarioReplay.class);
        Class<?> type = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        java.lang.reflect.Constructor<?> ctor = type.getDeclaredConstructor(
                org.mage.test.player.TestComputerPlayer.class, ScenarioReplay.class);
        ctor.setAccessible(true);
        org.mage.test.player.TestPlayer player = (org.mage.test.player.TestPlayer) ctor.newInstance(
                new org.mage.test.player.TestComputerPlayer("PlayerA", mage.constants.RangeOfInfluence.ALL), owner);
        // playerA lives on an ancestor of the driver.
        seat(owner, "playerA", player);
        UUID own = player.getId();
        Permanent first = object(own, true, 1), second = object(own, true, 2);
        mage.game.permanent.Battlefield board = new mage.game.permanent.Battlefield();
        board.addPermanent(second);
        player.addAlias("p0:token:Grizzly Bears", first.getId());
        if (!player.hasObjectTargetNameOrAlias(first, "@p0:token:Grizzly Bears")) {
            throw new AssertionError("precondition: base matcher must see the stale alias outside a choice");
        }
        mage.game.Game game = (mage.game.Game) Proxy.newProxyInstance(mage.game.Game.class.getClassLoader(),
                new Class<?>[]{mage.game.Game.class}, (proxy, method, args) -> {
                    if (method.getName().equals("getBattlefield")) return board;
                    throw new AssertionError("unexpected game call " + method);
                });
        set(player, type, "choiceGame", game);
        if (player.hasObjectTargetNameOrAlias(first, "@p0:token:Grizzly Bears")
                || !player.hasObjectTargetNameOrAlias(second, "@p0:token:Grizzly Bears")) {
            throw new AssertionError("live choice must select the remaining token, not the stale alias");
        }
        set(player, type, "choiceGame", null);
        if (!player.hasObjectTargetNameOrAlias(first, "@p0:token:Grizzly Bears")) {
            throw new AssertionError("non-choice alias matching changed");
        }
        System.out.println("PASS production choice matcher overrides stale token aliases only inside a choice");
    }

    /** The target path must keep a token ref's alias even when setup bound
     * none, so the live choice matcher above can resolve it by creation
     * order. A card ref with no alias keeps the bare-name fallback. */
    private static void tokenTargetName() throws Exception {
        java.lang.reflect.Field f = sun.misc.Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        sun.misc.Unsafe unsafe = (sun.misc.Unsafe) f.get(null);
        ScenarioReplay owner = (ScenarioReplay) unsafe.allocateInstance(ScenarioReplay.class);
        java.util.Map<String, String> refs = new java.util.LinkedHashMap<>();
        set(owner, ScenarioReplay.class, "refAlias", refs);
        set(owner, ScenarioReplay.class, "gorgeName", "");
        set(owner, ScenarioReplay.class, "xmageName", "");
        java.lang.reflect.Method targetName = ScenarioReplay.class.getDeclaredMethod("targetName", String.class);
        targetName.setAccessible(true);
        equal("@p0:token:Grizzly Bears", targetName.invoke(owner, "p0:token:Grizzly Bears"));
        equal("@p1:token:Officer Token#2", targetName.invoke(owner, "p1:token:Officer Token#2"));
        // An answer already carrying its alias is left alone: the funnel is
        // idempotent for a hand-authored "@..." xmage_answers target.
        equal("@p0:token:Grizzly Bears#2", targetName.invoke(owner, "@p0:token:Grizzly Bears#2"));
        equal("@p0:Grizzly Bears", targetName.invoke(owner, "@p0:Grizzly Bears"));
        equal("Grizzly Bears", targetName.invoke(owner, "p0:Grizzly Bears"));
        refs.put("p0:Grizzly Bears", "@p0:Grizzly Bears");
        equal("@p0:Grizzly Bears", targetName.invoke(owner, "p0:Grizzly Bears"));
        System.out.println("PASS token target refs keep their alias for live creation-order resolution");
    }

    /** The target surface the production chooseTarget/chooseTargetAmount
     * overrides read, with the legal set and chosen id under test control. */
    private static final class PermanentTarget implements java.lang.reflect.InvocationHandler {
        private final mage.target.TargetPermanent original = new mage.target.TargetPermanent();
        private final Set<UUID> possible;
        UUID chosen;
        int chosenCount;

        PermanentTarget(UUID only) {
            possible = new LinkedHashSet<>(List.of(only));
        }

        mage.target.Target target() {
            return (mage.target.Target) Proxy.newProxyInstance(mage.target.Target.class.getClassLoader(),
                    new Class<?>[]{mage.target.Target.class}, this);
        }

        @Override
        public Object invoke(Object proxy, java.lang.reflect.Method method, Object[] args) {
            switch (method.getName()) {
                case "getAffectedAbilityControllerId": return args[0];
                case "getOriginalTarget": return original;
                case "getMinNumberOfTargets": return 1;
                case "getMaxNumberOfTargets": return 1;
                case "getSize": return chosenCount;
                case "getTargets":
                    return chosen == null ? new ArrayList<UUID>() : new ArrayList<>(List.of(chosen));
                case "clearChosen": return null;
                case "possibleTargets": return possible;
                case "contains": return false;
                case "canTarget": return true;
                case "addTarget":
                    if (args.length == 3) {
                        chosen = (UUID) args[0];
                        chosenCount++;
                        return null;
                    }
                    throw new AssertionError("unexpected addTarget overload");
                case "isChosen": return true;
                case "getChooseHint": return null;
                case "getTargetName": return "";
                case "getMessage": return "Grizzly Bears";
                case "isNotTarget": return false;
                case "toString": return "PermanentTarget";
                default: throw new AssertionError("unexpected target call " + method);
            }
        }
    }

    /** A divided-amount target with one legal recipient; the production
     * chooseTargetAmount reads its queue and chooses the recipient. */
    private static final class AmountTarget extends mage.target.TargetAmount {
        private final Set<UUID> possible;

        AmountTarget(UUID only) {
            super(mage.abilities.dynamicvalue.common.StaticValue.get(1), 1, 1);
            possible = new LinkedHashSet<>(List.of(only));
        }

        @Override
        public AmountTarget copy() {
            return this;
        }

        @Override
        public mage.filter.Filter getFilter() {
            return mage.filter.StaticFilters.FILTER_PERMANENT;
        }

        @Override
        public String getTargetedName(Game game) {
            return "";
        }

        @Override
        public boolean canChoose(UUID playerId, Ability source, Game game) {
            return true;
        }

        @Override
        public Set<UUID> possibleTargets(UUID controllerId, Ability source, Game game) {
            return possible;
        }

        @Override
        public boolean canTarget(UUID id, Ability source, Game game) {
            return true;
        }

        @Override
        public boolean canTarget(UUID playerId, UUID id, Ability source, Game game) {
            return true;
        }
    }

    /** The live choice game both target entry points need: one battlefield
     * permanent, answered lookup so the matcher can resolve a token ref. */
    private static Game liveGame(mage.game.permanent.Battlefield board, Permanent only) {
        return (Game) Proxy.newProxyInstance(Game.class.getClassLoader(),
                new Class<?>[]{Game.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getBattlefield": return board;
                        case "getRangeOfInfluence": return mage.constants.RangeOfInfluence.ALL;
                        case "getObject": return only.getId().equals(args[0]) ? only : null;
                        case "getPermanent": return only.getId().equals(args[0]) ? only : null;
                        case "getCard": return null;
                        case "isSimulation": return true;
                        case "getPlayer": return null;
                        case "getPlayers": return new LinkedHashMap<UUID, mage.players.Player>();
                        default: throw new AssertionError("unexpected game call " + method);
                    }
                });
    }

    /** The production chooseTarget/chooseTargetAmount overrides, not just the
     * matcher they hand choiceGame to: a token alias queued through the real
     * entry point must resolve against the live battlefield's creation order.
     * The setup alias deliberately points at a token that is no longer live,
     * so only the choice-game wiring makes the queued alias match. */
    private static void targetEntryPointsResolveLive() throws Exception {
        ScenarioReplay owner = (ScenarioReplay) unsafe().allocateInstance(ScenarioReplay.class);
        Class<?> type = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        java.lang.reflect.Constructor<?> ctor = type.getDeclaredConstructor(
                org.mage.test.player.TestComputerPlayer.class, ScenarioReplay.class);
        ctor.setAccessible(true);
        org.mage.test.player.TestPlayer player = (org.mage.test.player.TestPlayer) ctor.newInstance(
                new org.mage.test.player.TestComputerPlayer("PlayerA", mage.constants.RangeOfInfluence.ALL), owner);
        seat(owner, "playerA", player);
        UUID own = player.getId();
        Permanent stale = object(own, true, 1), live = object(own, true, 2);
        mage.game.permanent.Battlefield board = new mage.game.permanent.Battlefield();
        board.addPermanent(live);
        player.addAlias("p0:token:Grizzly Bears", stale.getId());
        Game game = liveGame(board, live);

        PermanentTarget permanent = new PermanentTarget(live.getId());
        player.addTarget("@p0:token:Grizzly Bears");
        check(player.chooseTarget(mage.constants.Outcome.Benefit, permanent.target(), null, game),
                "chooseTarget did not accept the live token alias");
        equal(live.getId(), permanent.chosen);
        check(player.getTargets().isEmpty(), "chooseTarget left the target answer queued");
        check(get(player, type, "choiceGame") == null, "chooseTarget must restore the outer choice game");

        AmountTarget amount = new AmountTarget(live.getId());
        player.addTarget("@p0:token:Grizzly Bears^X=1");
        check(player.chooseTargetAmount(mage.constants.Outcome.Benefit, amount, null, game),
                "chooseTargetAmount did not accept the live token alias");
        check(amount.getTargets().contains(live.getId()),
                "chooseTargetAmount chose " + amount.getTargets() + ", not the live token");
        check(player.getTargets().isEmpty(), "chooseTargetAmount left the target answer queued");
        check(get(player, type, "choiceGame") == null, "chooseTargetAmount must restore the outer choice game");
        System.out.println("PASS token target refs resolve through chooseTarget and chooseTargetAmount");
    }

    /** XMage's inline castSpell $target= form sets targets in addTargets,
     * before any chooseTarget ask. The new divided multi-target path uses the
     * amount-target name helper, so a token ref reaches this entry point as
     * "@...:token:"; it must resolve live here too. */
    @SuppressWarnings("unchecked")
    private static void inlineTargetsResolveLive() throws Exception {
        ScenarioReplay owner = (ScenarioReplay) unsafe().allocateInstance(ScenarioReplay.class);
        Class<?> type = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        java.lang.reflect.Constructor<?> ctor = type.getDeclaredConstructor(
                org.mage.test.player.TestComputerPlayer.class, ScenarioReplay.class);
        ctor.setAccessible(true);
        org.mage.test.player.TestPlayer player = (org.mage.test.player.TestPlayer) ctor.newInstance(
                new org.mage.test.player.TestComputerPlayer("PlayerA", mage.constants.RangeOfInfluence.ALL), owner);
        seat(owner, "playerA", player);
        UUID own = player.getId();
        Permanent stale = object(own, true, 1), live = object(own, true, 2);
        mage.game.permanent.Battlefield board = new mage.game.permanent.Battlefield();
        board.addPermanent(live);
        player.addAlias("p0:token:Grizzly Bears", stale.getId());
        Game game = liveGame(board, live);

        PermanentTarget permanent = new PermanentTarget(live.getId());
        mage.abilities.Modes modes = new mage.abilities.Modes();
        modes.getMode().addTarget(permanent.target());
        Ability ability = (Ability) Proxy.newProxyInstance(Ability.class.getClassLoader(),
                new Class<?>[]{Ability.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getModes": return modes;
                        case "getControllerId": return own;
                        case "getSourceId": return null;
                        case "toString": return "Test Ability";
                        default: throw new AssertionError("unexpected ability call " + method);
                    }
                });
        set(player, org.mage.test.player.TestPlayer.class, "groupsForTargetHandling",
                new String[]{"", "target=@p0:token:Grizzly Bears"});
        check(player.addTargets(ability, game), "addTargets rejected the live token alias");
        equal(live.getId(), permanent.chosen);
        check(get(player, type, "choiceGame") == null, "addTargets must restore the outer choice game");
        System.out.println("PASS inline $target= token refs resolve live through addTargets");
    }

    /** Drive the production registerAliases, not just its ordering helper: a
     * battlefield that iterates the second-created same-name copy first must
     * still bind "#1" to the first-created one. A revert to walking
     * getAllPermanents() directly binds the wrong copy and fails here. */
    private static void setupAliasOrdering() throws Exception {
        ScenarioReplay owner = newOwner();
        org.mage.test.player.TestPlayer playerA = scripted(owner, new RecordingComputerPlayer("PlayerA"));
        org.mage.test.player.TestPlayer playerB = scripted(owner, new RecordingComputerPlayer("PlayerB"));
        seat(owner, "playerA", playerA);
        seat(owner, "playerB", playerB);
        set(owner, ScenarioReplay.class, "seats", 2);
        UUID own = playerA.getId(), other = playerB.getId();
        String name = "Kjeldoran Warrior";
        Permanent first = object(own, false, 1, name);
        Permanent leader = object(own, false, 2, name);
        Permanent bystander = object(own, false, 3, "Garrison Griffin");
        Permanent opponent = object(other, false, 4, name);
        mage.game.permanent.Battlefield board = new mage.game.permanent.Battlefield();
        // Deliberately not creation order.
        board.addPermanent(leader);
        board.addPermanent(opponent);
        board.addPermanent(bystander);
        board.addPermanent(first);
        java.lang.reflect.Method register = ScenarioReplay.class.getDeclaredMethod("registerAliases", Game.class);
        register.setAccessible(true);
        register.invoke(owner, aliasGame(board));
        // registerAliases binds every seat's alias table, so either seat can
        // answer an ask about the other's object.
        for (org.mage.test.player.TestPlayer player : List.of(playerA, playerB)) {
            equal(first.getId(), player.getAliasByName("p0:" + name));
            equal(leader.getId(), player.getAliasByName("p0:" + name + "#2"));
            equal(bystander.getId(), player.getAliasByName("p0:Garrison Griffin"));
            equal(opponent.getId(), player.getAliasByName("p1:" + name));
        }
        System.out.println("PASS registerAliases binds same-name setup refs in creation order");
    }

    /** The step lowering must queue highest-index-first (XMage's name command
     * ranks untapped creatures) while the exact batch keeps the step's own
     * ref order. Reverting the reorder fails the refs assertion; reverting the
     * queue order fails the queued assertion. */
    private static void attackBatchOrdering() {
        List<String> refs = List.of("p0:Kjeldoran Warrior", "p0:Kjeldoran Warrior#2",
                "p0:Garrison Griffin");
        List<String> names = List.of("Kjeldoran Warrior", "Kjeldoran Warrior:1", "Garrison Griffin");
        List<String> queued = new ArrayList<>();
        Map<String, PlayerAction> byName = new LinkedHashMap<>();
        ScenarioReplay.ExactAttackBatch batch = ScenarioReplay.lowerAttackBatch(7, 0, 1, names, refs, name -> {
            queued.add(name);
            PlayerAction action = new PlayerAction("combat", 7,
                    mage.constants.PhaseStep.DECLARE_ATTACKERS, "attack:" + name);
            byName.put(name, action);
            return action;
        });
        equal(List.of("Kjeldoran Warrior:1", "Kjeldoran Warrior", "Garrison Griffin"), queued);
        equal(refs, batch.refs);
        equal(7, batch.turn);
        equal(0, batch.seat);
        equal(1, batch.defenderSeat);
        check(batch.actions.get(0) == byName.get("Kjeldoran Warrior"),
                "first exact action is the first copy's command");
        check(batch.actions.get(1) == byName.get("Kjeldoran Warrior:1"),
                "second exact action is the #2 command");
        check(batch.actions.get(2) == byName.get("Garrison Griffin"),
                "third exact action is the unique command");
        System.out.println("PASS attack batch queues highest-index-first but keeps step order for exact declarations");
    }

    /** The game registerAliases reads: its battlefield and an empty exile.
     * The players' hand, graveyard and library are real empty zones. */
    private static Game aliasGame(mage.game.permanent.Battlefield board) {
        return (Game) Proxy.newProxyInstance(Game.class.getClassLoader(),
                new Class<?>[]{Game.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getBattlefield": return board;
                        case "getExile": return new mage.game.Exile();
                        default: throw new AssertionError("unexpected game call " + method);
                    }
                });
    }

    public static void main(String[] args) throws Exception {
        UUID own = UUID.randomUUID(), other = UUID.randomUUID();
        Permanent original = object(own, false, 1);
        Permanent first = object(own, true, 2);
        Permanent second = object(own, true, 3);
        Permanent opponent = object(other, true, 4);
        if (first.getId().equals(second.getId()) || first.getCreateOrder() >= second.getCreateOrder()
                || !first.isToken() || original.isToken()) {
            throw new AssertionError("invalid duplicate-token fixture");
        }
        // Both iteration orders select the exact same second-created token.
        for (List<Permanent> battlefield : List.of(List.of(second, opponent, original, first),
                List.of(first, original, opponent, second))) {
            chosen(first, ScenarioReplay.tokenChoice("p0:token:Grizzly Bears", own, battlefield));
            chosen(second, ScenarioReplay.tokenChoice("p0:token:Grizzly Bears#2", own, battlefield));
            chosen(opponent, ScenarioReplay.tokenChoice("p1:token:Grizzly Bears", other, battlefield));
        }
        // Refs are positional among LIVE tokens: after a sacrifice the former
        // second token becomes #1. An old, setup-time alias must not shadow it.
        chosen(second, ScenarioReplay.tokenChoice("p0:token:Grizzly Bears", own, List.of(second)));
        chosen(null, ScenarioReplay.tokenChoice("p0:token:Grizzly Bears#2", own, List.of(second)));
        System.out.println("PASS exact token choice is creation-ordered, controller-scoped and live");
        setupAliasOrdering();
        attackBatchOrdering();
        combatRefs();
        exactCombatBatches();
        attackWithUntappedFifthForestAfterFourTapped();
        blockSecondOfTwoSameNameAttackers();
        autoDeclaredBlock();
        liveMatcher();
        tokenTargetName();
        targetEntryPointsResolveLive();
        inlineTargetsResolveLive();
    }
}
