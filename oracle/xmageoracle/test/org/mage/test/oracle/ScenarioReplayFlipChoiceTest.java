package org.mage.test.oracle;

import mage.constants.MultiplayerAttackOption;
import mage.constants.Outcome;
import mage.constants.RangeOfInfluence;
import mage.game.Game;
import mage.game.TwoPlayerDuel;
import mage.game.mulligan.MulliganType;
import org.mage.test.player.TestComputerPlayer;
import org.mage.test.player.TestPlayer;

/** Executable check of the driver's scripted coin-flip results (Mana Crypt's
 * unscripted upkeep flip: "AssertionError: Missing FLIP COIN RESULT def for
 * turn 1, step UPKEEP, PlayerA"), without H2 or a full game. Run
 * scripts/xmage-oracle-test-flip-choice.sh; the replay itself is a host gate.
 * This exercises the exact flipCoinResult consumer with a queue populated as
 * queueSetupChoices ("setup_flip") and scripted ("flip") do; the addCard/build
 * integration is covered only by the XMage batch replay. */
public final class ScenarioReplayFlipChoiceTest {
    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static Game game(TestPlayer p) {
        Game g = new TwoPlayerDuel(MultiplayerAttackOption.LEFT, RangeOfInfluence.ONE,
                MulliganType.GAME_DEFAULT.getMulligan(0), 60, 20, 7);
        g.addPlayer(p, new mage.cards.decks.Deck());
        return g;
    }

    public static void main(String[] args) {
        // "win" queues the token flipCoinResult consumes and answers true.
        TestPlayer win = new TestPlayer(new TestComputerPlayer("Win", RangeOfInfluence.ONE));
        Game winGame = game(win);
        ScenarioReplay.queueFlipResult(win, "win");
        equal(1, win.getChoices().size());
        equal(TestPlayer.FLIPCOIN_RESULT_TRUE, win.getChoices().get(0));
        equal(true, win.flipCoinResult(winGame));
        equal(0, win.getChoices().size());
        System.out.println("PASS a scripted win flips heads and is consumed");

        // "lose" answers false, the side Mana Crypt's lost upkeep flip takes.
        TestPlayer lose = new TestPlayer(new TestComputerPlayer("Lose", RangeOfInfluence.ONE));
        Game loseGame = game(lose);
        ScenarioReplay.queueFlipResult(lose, "lose");
        equal(TestPlayer.FLIPCOIN_RESULT_FALSE, lose.getChoices().get(0));
        equal(false, lose.flipCoinResult(loseGame));
        equal(0, lose.getChoices().size());
        System.out.println("PASS a scripted lose flips tails and is consumed");

        // Two flips answer in FIFO order (Mana Crypt trigger#0.0: the
        // drive-window loss at turn 1, the win at turn 3).
        TestPlayer pair = new TestPlayer(new TestComputerPlayer("Pair", RangeOfInfluence.ONE));
        Game pairGame = game(pair);
        ScenarioReplay.queueFlipResult(pair, "lose");
        ScenarioReplay.queueFlipResult(pair, "win");
        equal(false, pair.flipCoinResult(pairGame));
        equal(true, pair.flipCoinResult(pairGame));
        System.out.println("PASS two scripted flips answer in queue order");

        // Precondition: an UNSCRIPTED flip fails exactly the way the committed
        // Mana Crypt rows reported, which is what the queueing removes. The
        // replay drives its players in strict-choose mode, and
        // chooseStrictModeFailed only fails when the computer may not pick.
        TestPlayer bare = new TestPlayer(new TestComputerPlayer("Bare", RangeOfInfluence.ONE));
        Game bareGame = game(bare);
        bare.setChooseStrictMode(true);
        AssertionError unscripted = null;
        try {
            bare.flipCoinResult(bareGame);
        } catch (AssertionError e) {
            unscripted = e;
        }
        if (unscripted == null || !String.valueOf(unscripted.getMessage()).contains("Missing FLIP COIN RESULT def")) {
            throw new AssertionError("expected the reported Missing FLIP COIN RESULT failure, got " + unscripted);
        }
        System.out.println("PASS an unscripted flip fails with the reported Missing FLIP COIN RESULT def");
    }
}
