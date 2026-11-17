package org.mage.test.oracle;

import java.lang.reflect.Proxy;
import java.util.List;
import java.util.UUID;
import mage.game.permanent.Permanent;

/** No H2: prove that token refs rank by creation, not battlefield iteration. */
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
     * "token:" ref must resolve to the token, not a same-name card; the
     * name-only XMage command is ambiguous only when both kinds are present
     * under the same controller. */
    private static void combatRefs() {
        UUID own = UUID.randomUUID(), other = UUID.randomUUID();
        Permanent card = object(own, false, 1);
        Permanent first = object(own, true, 2);
        Permanent second = object(own, true, 3);
        Permanent opponentCard = object(other, false, 4);
        Permanent card2 = object(own, false, 5);
        List<Permanent> battlefield = List.of(second, opponentCard, card2, card, first);
        // The attacker path: the recorded token ref names the token, and the
        // recorded card ref names the card, even though both are "Grizzly
        // Bears" and battlefield iteration order is not creation order.
        chosen(first, ScenarioReplay.combatPermanent("p0:token:Grizzly Bears", "Grizzly Bears", own, battlefield));
        chosen(second, ScenarioReplay.combatPermanent("p0:token:Grizzly Bears#2", "Grizzly Bears", own, battlefield));
        chosen(card, ScenarioReplay.combatPermanent("p0:Grizzly Bears", "Grizzly Bears", own, battlefield));
        chosen(card2, ScenarioReplay.combatPermanent("p0:Grizzly Bears#2", "Grizzly Bears", own, battlefield));
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
        chosen(renamed, ScenarioReplay.combatPermanent("p0:token:Dáin Ironfoot", "Dain Ironfoot", own,
                List.of(renamed)));
        System.out.println("PASS token:/card combat refs resolve exactly and detect a token/card name collision");
    }

    private static void set(Object target, Class<?> type, String name, Object value) throws Exception {
        java.lang.reflect.Field f = type.getDeclaredField(name);
        f.setAccessible(true);
        f.set(target, value);
    }

    /** Exercise the production name matcher, including a pre-bound STALE
     * alias. Only the choice path may override that alias by live position. */
    private static void liveMatcher() throws Exception {
        java.lang.reflect.Field f = sun.misc.Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        sun.misc.Unsafe unsafe = (sun.misc.Unsafe) f.get(null);
        ScenarioReplay owner = (ScenarioReplay) unsafe.allocateInstance(ScenarioReplay.class);
        Class<?> type = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        java.lang.reflect.Constructor<?> ctor = type.getDeclaredConstructor(
                org.mage.test.player.TestComputerPlayer.class, ScenarioReplay.class);
        ctor.setAccessible(true);
        org.mage.test.player.TestPlayer player = (org.mage.test.player.TestPlayer) ctor.newInstance(
                new org.mage.test.player.TestComputerPlayer("PlayerA", mage.constants.RangeOfInfluence.ALL), owner);
        // playerA lives on an ancestor of the driver.
        for (Class<?> c = ScenarioReplay.class; c != null; c = c.getSuperclass()) {
            try {
                set(owner, c, "playerA", player);
                break;
            } catch (NoSuchFieldException absent) { }
        }
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
        liveMatcher();
    }
}
