package org.mage.test.oracle;

import java.util.List;
import mage.cards.Card;
import mage.cards.repository.CardInfo;
import mage.cards.repository.CardRepository;
import mage.constants.RangeOfInfluence;
import mage.constants.Zone;
import mage.utils.SystemUtil;
import org.mage.test.player.TestComputerPlayer;
import org.mage.test.player.TestPlayer;

/** Executable check that the driver's addCard reaches a card whose real name
 * starts with a set-code-like prefix. XMage's addCard parses a leading
 * "3-6 upper-case/digit chars then '-'" as a set code, so the bare name
 * "DOT-7 Repair Squad" becomes set DOT + card "7 Repair Squad" and the lookup
 * fails. The driver override must re-spell it with the card's own set code
 * while leaving every other name unchanged.
 * Run scripts/xmage-oracle-test-hyphen-name.sh. Opens XMage's H2 card DB. */
public final class ScenarioReplayHyphenNameTest {
    /** Exposes the inherited setup queue so the test can read what addCard
     * actually stored. */
    static final class Probe extends ScenarioReplay {
        List<Card> hand(TestPlayer player) {
            return getHandCards(player);
        }
    }

    private static void equal(Object want, Object got, String what) {
        if (!want.equals(got)) {
            throw new AssertionError(what + ": expected " + want + ", got " + got);
        }
    }

    public static void main(String[] args) {
        String name = "DOT-7 Repair Squad";
        // The parser precondition: without the driver's re-spelling, addCard
        // really does look for DOT's "7 Repair Squad". This is the live rule,
        // not a copy of it.
        List<String> parsed = SystemUtil.parseSetAndCardNameCommand(name);
        equal("DOT", parsed.get(0), "XMage parses the bare name's set");
        equal("7 Repair Squad", parsed.get(1), "XMage parses the bare name's card");

        Probe driver = new Probe();
        CardInfo info = CardRepository.instance.findCard(name, true);
        if (info == null) {
            throw new AssertionError("fixture card " + name + " is not in the XMage card DB");
        }
        TestPlayer player = new TestPlayer(new TestComputerPlayer("PlayerA", RangeOfInfluence.ALL));

        // The regression: on the unfixed driver this throws "Couldn't find a
        // card:7 Repair Squad from specific set: DOT".
        driver.addCard(Zone.HAND, player, name, 1, false);
        List<Card> hand = driver.hand(player);
        equal(1, hand.size(), "hand size");
        equal(name, hand.get(0).getName(), "added card name");
        equal(info.getSetCode(), hand.get(0).getExpansionSetCode(), "added card set");

        // Control: a name the parser does not split still reaches addCard
        // unchanged and is found.
        driver.addCard(Zone.HAND, player, "Grizzly Bears", 1, false);
        equal(2, hand.size(), "hand size after control");
        equal("Grizzly Bears", hand.get(1).getName(), "control card name");

        System.out.println("PASS addCard found " + name + " (" + info.getSetCode()
                + ") and left Grizzly Bears unchanged");
    }
}
