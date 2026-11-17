package org.mage.test.oracle;

import java.lang.reflect.Proxy;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
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
        UUID id = UUID.randomUUID();
        return (Permanent) Proxy.newProxyInstance(Permanent.class.getClassLoader(),
                new Class<?>[]{Permanent.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getId": return id;
                        case "getControllerId": return controller;
                        case "getName": return name;
                        case "isToken": return token;
                        case "getCreateOrder": return order;
                        default: throw new AssertionError("unexpected call " + method);
                    }
                });
    }

    private static void chosen(Permanent want, Permanent got) {
        if (want != got) {
            throw new AssertionError("choice selected the wrong object");
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
        combatRefs();
        exactCombatBatches();
        liveMatcher();
    }
}
