package org.mage.test.oracle;

import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import mage.abilities.Ability;
import mage.abilities.TriggeredAbility;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.constants.Rarity;

/** No H2, no game: the same-source trigger-order key an xproof find recorded
 * (Forge's TriggerDescription$ wording) picks the XMage ability the real card
 * prints. Hero of Oxid Ridge's key says "When {this} attacks, ..." where
 * XMage's AttacksTriggeredAbility says "Whenever {this} attacks, ..."; the
 * Nefarox key ends at "a creature." where XMage's SacrificeEffect continues
 * "of their choice". TestPlayer.chooseTriggeredAbility prefix-matches only,
 * so both were a strict "Missing CHOICE def". Run
 * scripts/xmage-oracle-test-trigger-order-wording.sh. */
public final class ScenarioReplayTriggerOrderWordingTest {
    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static List<String> rules(Card card) {
        List<String> rules = new ArrayList<>();
        for (Ability a : card.getAbilities()) {
            if (a instanceof TriggeredAbility) {
                rules.add(a.toString());
            }
        }
        return rules;
    }

    public static void main(String[] args) {
        String hero = "Hero of Oxid Ridge";
        List<String> heroRules = rules(new mage.cards.h.HeroOfOxidRidge(UUID.randomUUID(),
                new CardSetInfo(hero, "TST", "1", Rarity.MYTHIC)));
        equal(2, heroRules.size());
        String heroKey = "When {this} attacks, creatures with power 1 or less can't block this turn.";
        // Precondition: the verbatim key is the strict miss (no rule starts with it).
        for (String r : heroRules) {
            equal(false, r.startsWith(heroKey));
        }
        int at = ScenarioReplay.triggerKeyMatch(heroKey, heroRules, hero);
        equal(true, at >= 0 && heroRules.get(at).contains("creatures with power 1 or less"));

        String nefarox = "Nefarox, Overlord of Grixis";
        List<String> nefRules = rules(new mage.cards.n.NefaroxOverlordOfGrixis(UUID.randomUUID(),
                new CardSetInfo(nefarox, "TST", "1", Rarity.RARE)));
        equal(2, nefRules.size());
        String nefKey = "Whenever {this} attacks alone, defending player sacrifices a creature.";
        for (String r : nefRules) {
            equal(false, r.startsWith(nefKey));
        }
        at = ScenarioReplay.triggerKeyMatch(nefKey, nefRules, nefarox);
        equal(true, at >= 0 && nefRules.get(at).contains("attacks alone"));

        // The name spelled out folds to {this}.
        at = ScenarioReplay.triggerKeyMatch("When Hero of Oxid Ridge attacks, creatures with power 1 or less can't block this turn.",
                heroRules, hero);
        equal(true, at >= 0 && heroRules.get(at).contains("creatures with power 1 or less"));

        // A key some rule already prefix-matches is TestPlayer's own answer.
        equal(-1, ScenarioReplay.triggerKeyMatch(heroRules.get(1).substring(0, 20), heroRules, hero));
        // A key naming no ability stays unmatched (TestPlayer's strict miss).
        equal(-1, ScenarioReplay.triggerKeyMatch("When {this} dies, draw a card.", heroRules, hero));
        // A key that fits several abilities is ambiguous: no guess.
        equal(-1, ScenarioReplay.triggerKeyMatch("When {this} attacks.",
                List.of("Whenever {this} attacks, draw a card.", "Whenever {this} attacks, you gain 1 life."), hero));
        // A bare "." is no key.
        equal(-1, ScenarioReplay.triggerKeyMatch(".", heroRules, hero));
        System.out.println("ScenarioReplayTriggerOrderWordingTest ok");
    }
}
