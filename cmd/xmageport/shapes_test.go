package main

import "testing"

// runRefuses translates body, plays it and fails unless the run refuses with
// code (a shape converts a failing run only).
func runRefuses(t *testing.T, body, code string) {
	t.Helper()
	res := translateBody(t, body)
	if res.Scenario != nil {
		runResult(testutilRegistry(t), res)
	}
	if res.Code != code || res.Status != "refused" {
		t.Fatalf("status %s code %q (%s %v), want refused %s", res.Status, res.Code, res.Detail, res.Fails, code)
	}
}

// A card with two kicker costs is elected by XMage one boolean per cost, in the
// order it is written ({1}{G} first); gorge offers kicked1, kicked2 and
// kickedboth with Forge's cost order and the translator read one cost, so the
// second yes was never consumed (KickerTest).
func TestTwoKickerCostsElectionRefuses(t *testing.T) {
	runRefuses(t, `addCard(Zone.BATTLEFIELD, playerA, "Island", 3);
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 3);
        addCard(Zone.HAND, playerA, "Sunscape Battlemage", 1);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Sunscape Battlemage");
        setChoice(playerA, false);
        setChoice(playerA, true);
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPermanentCount(playerA, "Sunscape Battlemage", 1);
        assertHandCount(playerA, 2);`, "kicker-and-or")
}

// Wand of the Worldsoul gives the next spell convoke; XMage taps the Bear for it
// with an addTarget, which the runner (it seeds the whole cost) cannot place
// (GiftTest).
func TestConvokeTapStatedAsATargetRefuses(t *testing.T) {
	runRefuses(t, `addCard(Zone.HAND, playerA, "Blooming Blast");
        addCard(Zone.BATTLEFIELD, playerA, "Wand of the Worldsoul");
        addCard(Zone.BATTLEFIELD, playerA, "Runeclaw Bear");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.BATTLEFIELD, playerB, "Grizzly Bears");
        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "{T}: The next spell you cast this turn");
        waitStackResolved(1, PhaseStep.PRECOMBAT_MAIN);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Blooming Blast", "Grizzly Bears");
        setChoice(playerA, false);
        addTarget(playerA, "Runeclaw Bear");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertGraveyardCount(playerB, "Grizzly Bears", 1);
        assertTapped("Runeclaw Bear", true);`, "convoke-tap-target")
}

// Eternalize puts a token copy onto the battlefield; an assert by the card's
// name binds the card left in the graveyard or exile, not the token, so the
// 4/4 it states could only be judged against the wrong object (EternalizeTest).
func TestEternalizedCardAssertRefuses(t *testing.T) {
	res := translateBody(t, `addCard(Zone.GRAVEYARD, playerA, "Steadfast Sentinel", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 10);
        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Eternalize");
        setStopAt(1, PhaseStep.POSTCOMBAT_MAIN);
        execute();
        assertPermanentCount(playerA, "Steadfast Sentinel", 1);
        assertPowerToughness(playerA, "Steadfast Sentinel", 4, 4);`)
	if res.Code != "token-copy-assert" {
		t.Fatalf("code %q (%s), want token-copy-assert", res.Code, res.Detail)
	}
}

// XMage's assertPowerToughness(name) holds when ANY permanent of that name has
// the P/T, so two asserts with different values are both true for two copies
// that differ. The ref of an assert is the first copy, so the second assert
// was judged against the wrong object (EvolveTest).
func TestAnyScopePowerToughnessConflictRefuses(t *testing.T) {
	const body = `addCard(Zone.BATTLEFIELD, playerA, "Forest", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Experiment One", 1);
        addCard(Zone.HAND, playerA, "Experiment One");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Experiment One");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPowerToughness(playerA, "Experiment One", 2, 2);
        assertPowerToughness(playerA, "Experiment One", 3, 3);`
	if res := translateBody(t, body); res.Code != "any-scope-pt-conflict" {
		t.Fatalf("code %q (%s), want any-scope-pt-conflict", res.Code, res.Detail)
	}
	// one copy, or two asserts that agree, name no ambiguity
	one := `addCard(Zone.BATTLEFIELD, playerA, "Experiment One", 1);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPowerToughness(playerA, "Experiment One", 1, 1);
        assertPowerToughness(playerA, "Experiment One", 2, 2);`
	if res := translateBody(t, one); res.Code == "any-scope-pt-conflict" {
		t.Fatalf("a single copy refused: %s", res.Detail)
	}
}
