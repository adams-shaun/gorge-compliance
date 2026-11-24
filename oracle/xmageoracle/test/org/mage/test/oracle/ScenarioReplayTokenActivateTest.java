package org.mage.test.oracle;

/** Executable check of the driver's token-activation selector. No H2 or game
 * replay: a token the window created is no CardRepository card, so its rule
 * text and mana-ness come from the XMage token the setup table maps to its
 * name. A Treasure's cost reads "Sacrifice this artifact" in XMage, while the
 * scenario prefix (oraclegen.XMageAbility) keeps the card form "{this}".
 * Run scripts/xmage-oracle-test-token-activate.sh. */
public final class ScenarioReplayTokenActivateTest {
    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    public static void main(String[] args) {
        check(ScenarioReplay.tokenRuleStartsWith("{T}, Sacrifice this artifact: Add one mana of any color.",
                "{T}, Sacrifice {this}"), "{this} must read as XMage's \"this artifact\"");
        check(ScenarioReplay.tokenRuleStartsWith("{2}, Sacrifice {this}: Draw a card.", "{2}, Sacrifice {this}"),
                "a literal {this} still matches");
        check(!ScenarioReplay.tokenRuleStartsWith("{T}: Add {G}.", "{T}, Sacrifice {this}"),
                "a different cost must not match");
        String treasure = ScenarioReplay.tokenAbilityText("Treasure Token", "{T}, Sacrifice {this}");
        check(treasure.startsWith("{T}, Sacrifice this artifact"), "Treasure rule text = " + treasure);
        check(ScenarioReplay.isTokenManaAbilityText("Treasure Token", treasure),
                "a Treasure's ability is a mana ability (CR 605.1a)");
        check(ScenarioReplay.tokenAbilityText("No Such Token", "Crew 1").equals("Crew 1"),
                "an unmapped token keeps the scenario prefix");
        check(!ScenarioReplay.isTokenManaAbilityText("No Such Token", "Crew 1"),
                "Crew on an unmapped token is not a mana ability");
        System.out.println("ScenarioReplayTokenActivateTest: ok");
    }
}
