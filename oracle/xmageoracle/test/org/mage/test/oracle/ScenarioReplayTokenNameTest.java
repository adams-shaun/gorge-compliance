package org.mage.test.oracle;

import java.lang.reflect.Proxy;
import java.util.List;
import java.util.UUID;
import mage.game.permanent.Permanent;

/**
 * XMage names some tokens without the " Token" suffix Forge's token scripts
 * carry: Empower's planeswalker token is "Jace" (JacePlaneswalkerToken) where
 * u_empower_jace.txt says "Jace Token". A scenario ref must still resolve it,
 * while an ordinary "<Name> Token" ref keeps its exact token and the
 * creation-order "#k" ranking is unchanged.
 *
 * <p>No H2 and no game replay: the refs are matched against fixture
 * permanents, and the XMage token names come from the token classes
 * themselves. Run scripts/xmage-oracle-test-token-name.sh.
 */
public final class ScenarioReplayTokenNameTest {
    /** A battlefield fixture with the fields tokenChoice reads. */
    private static Permanent object(UUID controller, int order, String name) {
        UUID id = UUID.randomUUID();
        return (Permanent) Proxy.newProxyInstance(Permanent.class.getClassLoader(),
                new Class<?>[]{Permanent.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getId": return id;
                        case "getControllerId": return controller;
                        case "getName": return name;
                        case "isToken": return true;
                        case "getCreateOrder": return order;
                        default: throw new AssertionError("unexpected call " + method);
                    }
                });
    }

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static void chosen(UUID controller, String ref, List<Permanent> field, Permanent want) {
        Permanent got = ScenarioReplay.tokenChoice(ref, controller, field);
        if (want != got) {
            throw new AssertionError(ref + " selected the wrong object");
        }
    }

    public static void main(String[] args) {
        UUID p0 = UUID.randomUUID();
        String jaceName = new mage.game.permanent.token.JacePlaneswalkerToken().getName();
        String treasureName = new mage.game.permanent.token.TreasureToken().getName();

        Permanent jace = object(p0, 1, jaceName);
        Permanent jaceSecond = object(p0, 2, jaceName);
        chosen(p0, "p0:token:Jace Token", List.of(jace), jace);
        // The suffix fallback keeps the live creation-order "#k" ranking.
        chosen(p0, "p0:token:Jace Token#2", List.of(jaceSecond, jace), jaceSecond);

        // An ordinary "<Name> Token" ref is unchanged, and a literal
        // "Jace Token" token still wins over the bare-named one.
        Permanent treasure = object(p0, 3, treasureName);
        Permanent bareTreasure = object(p0, 4, "Treasure");
        chosen(p0, "p0:token:Treasure Token", List.of(bareTreasure, treasure), treasure);
        Permanent jaceToken = object(p0, 5, "Jace Token");
        chosen(p0, "p0:token:Jace Token", List.of(jace, jaceToken), jaceToken);

        // The bare-name fallback is exact, not a contains match: another
        // Jace-named token must not answer a "Jace Token" ref.
        Permanent jaceSculptor = object(p0, 6, "Jace, Reality Sculptor");
        check(null == ScenarioReplay.tokenChoice("p0:token:Jace Token", p0, List.of(jaceSculptor)),
                "the suffix fallback matched a token whose name merely starts with Jace");

        // activationSourceId scans by the ref name too: a token created by the
        // window has no setup alias, so its source is found by name.
        UUID jaceId = UUID.randomUUID();
        UUID jaceSource = new ScenarioReplay().chooseActivationSource(List.of(
                new ScenarioReplay.ActivationCandidate(jaceName, "-1: Surveil 1.", jaceId)),
                "Jace Token", "-1", null);
        check(jaceId.equals(jaceSource), "activation source did not accept XMage's bare Jace token");
        UUID treasureId = UUID.randomUUID();
        UUID treasureSource = new ScenarioReplay().chooseActivationSource(List.of(
                new ScenarioReplay.ActivationCandidate(treasureName,
                        "{T}, Sacrifice this artifact: Add one mana of any color.", treasureId)),
                "Treasure Token", "{T}", null);
        check(treasureId.equals(treasureSource), "ordinary Treasure Token activation no longer resolves");

        System.out.println("PASS a bare-named XMage token resolves a Forge '<Name> Token' ref");
    }
}
