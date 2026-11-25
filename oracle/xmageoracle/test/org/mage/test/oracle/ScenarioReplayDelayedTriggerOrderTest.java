package org.mage.test.oracle;

import java.util.List;
import mage.abilities.common.delayed.AtTheBeginOfNextUpkeepDelayedTriggeredAbility;
import mage.abilities.effects.common.DrawCardSourceControllerEffect;
import mage.abilities.effects.common.DrawCardTargetEffect;
import mage.abilities.dynamicvalue.common.StaticValue;

/** No H2, no game: the two delayed upkeep triggers a countered Arcane Denial
 * leaves. XMage's delayed rule text puts the subject first and the timing
 * last ("You draw a card at the beginning of the next turn's upkeep."), so
 * the generator's Forge-wording key "Draw a card." prefixes neither rule and
 * the prefix-only match was a strict "Missing CHOICE def" (xproof find
 * xs:XP:arcane-denial:cast:s1). The driver's containment fallback picks the
 * one rule that contains the key. Run
 * scripts/xmage-oracle-test-delayed-trigger-order.sh. */
public final class ScenarioReplayDelayedTriggerOrderTest {
    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    public static void main(String[] args) {
        // The two delayed triggers as ArcaneDenial's spell resolution leaves
        // them: the caster's draw (the card's own effect) and the countered
        // spell controller's up-to-two (ArcaneDenialEffect's).
        AtTheBeginOfNextUpkeepDelayedTriggeredAbility casters =
                new AtTheBeginOfNextUpkeepDelayedTriggeredAbility(new DrawCardSourceControllerEffect(1, true));
        DrawCardTargetEffect upToTwoEffect = new DrawCardTargetEffect(StaticValue.get(2), false, true);
        upToTwoEffect.setText("Its controller may draw up to two cards");
        AtTheBeginOfNextUpkeepDelayedTriggeredAbility upToTwo =
                new AtTheBeginOfNextUpkeepDelayedTriggeredAbility(upToTwoEffect);
        List<String> rules = List.of(casters.toString(), upToTwo.toString());

        // Precondition: the rules are XMage's delayed wording, timing last.
        equal("You draw a card at the beginning of the next turn's upkeep.", rules.get(0));
        equal("Its controller may draw up to two cards at the beginning of the next turn's upkeep.", rules.get(1));
        // The verbatim keys prefix neither rule (the strict miss as measured).
        for (String r : rules) {
            equal(false, r.startsWith("Draw a card."));
            equal(false, r.startsWith("Draw up to two cards."));
        }

        String arcane = "Arcane Denial";
        int at = ScenarioReplay.triggerKeyMatch("Draw a card.", rules, arcane);
        equal(true, at >= 0 && rules.get(at).startsWith("You draw a card"));
        at = ScenarioReplay.triggerKeyMatch("Draw up to two cards.", rules, arcane);
        equal(true, at >= 0 && rules.get(at).contains("up to two cards"));

        // A key two different rules contain is ambiguous: no guess.
        equal(-1, ScenarioReplay.triggerKeyMatch("Draw a card.",
                List.of("Whenever {this} attacks, draw a card, then discard a card.",
                        "Whenever {this} attacks, you may draw a card, then discard two cards."),
                "Some Source"));
        // Two identical copies are interchangeable: take either.
        equal(0, ScenarioReplay.triggerKeyMatch("Draw a card.",
                List.of("You draw a card at the beginning of the next turn's upkeep.",
                        "You draw a card at the beginning of the next turn's upkeep."),
                arcane));
        // A key no rule contains stays unmatched (TestPlayer's strict miss).
        equal(-1, ScenarioReplay.triggerKeyMatch("Counterspell.", rules, arcane));
        System.out.println("ScenarioReplayDelayedTriggerOrderTest ok");
    }
}
