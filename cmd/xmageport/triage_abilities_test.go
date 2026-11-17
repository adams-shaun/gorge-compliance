package main

import (
	"strings"
	"testing"
)

// shapeCases plays each body and wants it refused with code: the shape
// refusals convert a failing run only, so every body below fails an assert on
// gorge before the shape exists (status fail-expect) and is refused after.
func shapeCases(t *testing.T, cases []struct{ name, body, code string }) {
	t.Helper()
	for _, c := range cases {
		res := translateBody(t, c.body)
		if res.Scenario == nil {
			t.Errorf("%s: refused early %s: %s", c.name, res.Code, res.Detail)
			continue
		}
		if res.shape == nil || res.shape.Code != c.code {
			t.Errorf("%s: shape %v, want %s", c.name, res.shape, c.code)
			continue
		}
		runResult(testutilRegistry(t), res)
		if res.Status != "refused" || res.Code != c.code {
			t.Errorf("%s: status %s code %q (%v), want refused %s", c.name, res.Status, res.Code, res.Fails, c.code)
		}
	}
}

// A card that counts "{S} spent" on its own cast reads which sources paid;
// the runner seeds the pool, so no snow permanent ever pays (Search for Glory,
// Berg Strider). Tapping the real sources instead makes both pass, so this is
// the translator's fault and not a gorge bug.
func TestSnowManaSpentShapesRefuse(t *testing.T) {
	shapeCases(t, []struct{ name, body, code string }{
		{"search for glory", `addCard(Zone.BATTLEFIELD, playerA, "Snow-Covered Plains");
        addCard(Zone.BATTLEFIELD, playerA, "Boreal Druid");
        addCard(Zone.BATTLEFIELD, playerA, "Plains");
        addCard(Zone.HAND, playerA, "Search for Glory");
        addCard(Zone.LIBRARY, playerA, "Snow-Covered Plains");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Search for Glory");
        addTarget(playerA, "Snow-Covered Plains");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertGraveyardCount(playerA, "Search for Glory", 1);
        assertLife(playerA, 22);`, "mana-spent-by-source-type"},
		{"berg strider", `addCard(Zone.BATTLEFIELD, playerA, "Snow-Covered Island");
        addCard(Zone.BATTLEFIELD, playerA, "Island", 4);
        addCard(Zone.BATTLEFIELD, playerB, "Silvercoat Lion");
        addCard(Zone.HAND, playerA, "Berg Strider");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Berg Strider");
        addTarget(playerA, "Silvercoat Lion");
        setStrictChooseMode(true);
        setStopAt(2, PhaseStep.PRECOMBAT_MAIN);
        execute();
        assertTapped("Silvercoat Lion", true);`, "mana-spent-by-source-type"},
	})
}

// XMage answers a MayPlay alternative cost ("Cast with alternative cost: Pay
// 1 life, Exile a blue card ...") with a setChoice that follows the spell's
// addTarget; the cast must carry cast_mode alternative and pay no mana, not
// seed the printed cost and drop the pick.
func TestAltCostPickAfterTheTarget(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Boromir, Warden of the Tower");
        addCard(Zone.HAND, playerA, "Balduvian Bears");
        addCard(Zone.BATTLEFIELD, playerA, "Forest", 2);
        addCard(Zone.HAND, playerB, "Force of Will");
        addCard(Zone.HAND, playerB, "Remand");
        addCard(Zone.BATTLEFIELD, playerB, "Island", 5);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Balduvian Bears");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerB, "Force of Will");
        addTarget(playerB, "Balduvian Bears");
        setChoice(playerB, "Cast with alternative cost: Pay 1 life, Exile a blue card from your hand (source: Force of Will");
        setChoice(playerB, "Remand");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.PRECOMBAT_MAIN);
        execute();
        assertPermanentCount(playerA, "Balduvian Bears", 1);
        assertGraveyardCount(playerB, "Force of Will", 1);
        assertExileCount(playerB, "Remand", 1);
        assertLife(playerB, 19);`)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	var fow *step
	for _, st := range res.Scenario.Steps {
		if st.Op == "cast" && strings.HasSuffix(st.Card, "Force of Will") {
			fow = st
		}
	}
	if fow == nil {
		t.Fatal("no Force of Will cast step")
	}
	if fow.CastMode != "alternative" || fow.Mana != "" {
		t.Errorf("cast_mode %q mana %q, want alternative and no mana", fow.CastMode, fow.Mana)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "pass" {
		t.Errorf("status %s (%v), want pass", res.Status, res.Fails)
	}
}

// Two permissions (Karador's and Gisa and Geralf's) offer the same card as
// two identical "Cast X" options; XMage's setChoice names the permitting
// object, which no scenario field can state, so the runner takes the first
// option and the permission it spends is the wrong one.
func TestMayPlaySourceChoiceShapeRefuses(t *testing.T) {
	shapeCases(t, []struct{ name, body, code string }{
		{"karador and gisa", `addCard(Zone.BATTLEFIELD, playerA, "Plains", 4);
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 6);
        addCard(Zone.GRAVEYARD, playerA, "Accursed Horde", 1);
        addCard(Zone.GRAVEYARD, playerA, "Silvercoat Lion", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Karador, Ghost Chieftain");
        addCard(Zone.BATTLEFIELD, playerA, "Gisa and Geralf");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Accursed Horde");
        setChoice(playerA, "Gisa and Geralf");
        waitStackResolved(1, PhaseStep.PRECOMBAT_MAIN);
        checkPlayableAbility("after", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Silvercoat Lion", true);
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();`, "mayplay-source-choice"},
		{"karador and gisa cast first", `addCard(Zone.BATTLEFIELD, playerA, "Plains", 4);
        addCard(Zone.BATTLEFIELD, playerA, "Forest", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 6);
        addCard(Zone.BATTLEFIELD, playerA, "Island", 3);
        addCard(Zone.LIBRARY, playerA, "Accursed Horde", 1);
        addCard(Zone.LIBRARY, playerA, "Carrion Screecher", 1);
        addCard(Zone.GRAVEYARD, playerA, "Silvercoat Lion", 5);
        addCard(Zone.HAND, playerA, "Karador, Ghost Chieftain");
        addCard(Zone.HAND, playerA, "Gisa and Geralf");
        skipInitShuffling();
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Karador, Ghost Chieftain", true);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Gisa and Geralf");
        castSpell(3, PhaseStep.PRECOMBAT_MAIN, playerA, "Accursed Horde");
        setChoice(playerA, "Gisa and Geralf");
        waitStackResolved(3, PhaseStep.PRECOMBAT_MAIN);
        checkPlayableAbility("after", 3, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Silvercoat Lion", true);
        setStrictChooseMode(true);
        setStopAt(3, PhaseStep.BEGIN_COMBAT);
        execute();`, "mayplay-source-choice"},
	})
}

// checkPlayableAbility for the same player at the same step twice, with the
// other player's check between: the runner passes each seat's priority once,
// so the second check of the first seat would sit in the next step. XMage's
// own test (Bolas's Citadel) repeats the turn-1 checks under a "turn 3" label.
func TestRepeatedPlayableCheckShapeRefuses(t *testing.T) {
	shapeCases(t, []struct{ name, body, code string }{
		{"bolas citadel", `removeAllCardsFromLibrary(playerA);
        removeAllCardsFromLibrary(playerB);
        addCard(Zone.BATTLEFIELD, playerA, "Bolas's Citadel");
        addCard(Zone.LIBRARY, playerA, "Balduvian Bears", 2);
        addCard(Zone.LIBRARY, playerB, "Grizzly Bears", 2);
        checkPlayableAbility("A can use bolas on 1", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Balduvian Bears", true);
        checkPlayableAbility("B cant use bolas on 1", 1, PhaseStep.PRECOMBAT_MAIN, playerB, "Cast Grizzly Bears", false);
        checkPlayableAbility("A can use bolas on 3", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Balduvian Bears", true);
        checkPlayableAbility("B cant use bolas on 3", 1, PhaseStep.PRECOMBAT_MAIN, playerB, "Cast Grizzly Bears", false);
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();`, "playable-check-order"},
	})
}

// An inline check stated straight after activateAbility rides the activation
// step, before the ability resolves; XMage's test (Tarrian's Journal) states
// the post-resolution state, so the run fails on a state it never reached.
func TestCheckBeforeAbilityResolvesShapeRefuses(t *testing.T) {
	shapeCases(t, []struct{ name, body, code string }{
		{"tarrian's journal", `setStrictChooseMode(true);
        addCard(Zone.BATTLEFIELD, playerA, "Tarrian's Journal");
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 2);
        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "{2}, {T}, Discard your hand: Transform {this}.");
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        checkPermanentCount("Successfully transformed", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "The Tomb of Aclazotz", 1);`, "check-before-ability-resolves"},
	})
}

// A non-land mana source whose own mana ability puts the counter the test
// asserts (Empowered Autogenerator's charge counter): the runner pays from the
// pool, so the ability is never activated and the counter never appears.
func TestCounterAssertOnPooledManaSourceShapeRefuses(t *testing.T) {
	shapeCases(t, []struct{ name, body, code string }{
		{"empowered autogenerator", `addCard(Zone.BATTLEFIELD, playerA, "Empowered Autogenerator", 1);
        addCard(Zone.HAND, playerA, "Lightning Bolt", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Doubling Season", 1);
        setChoice(playerA, "Red");
        castSpell(3, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);
        setStrictChooseMode(true);
        setStopAt(3, PhaseStep.POSTCOMBAT_MAIN);
        execute();
        assertCounterCount(playerA, "Empowered Autogenerator", CounterType.CHARGE, 2);
        assertLife(playerB, 20 - 3);`, "mana-ability-counter"},
	})
}
