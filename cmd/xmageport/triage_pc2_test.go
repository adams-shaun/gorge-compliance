package main

import (
	"strings"
	"testing"
)

// XMage runs an assert written before execute() against the game as it is
// before the test's addCard calls land, so "assertHandCount(playerA, 0)" at
// the top of a method states the empty starting state, not the state at the
// stop step. Sergeant John Benton asserts 0 cards in each hand up front and
// 2 each at the end: reading both against the final state failed the test
// on a contradiction the test never states.
func TestAssertBeforeExecuteIsNotAFinalExpect(t *testing.T) {
	playsToPass(t, `assertHandCount(playerA, 0);
        assertHandCount(playerB, 0);
        addCard(Zone.BATTLEFIELD, playerA, "Sergeant John Benton");
        attack(1, playerA, "Sergeant John Benton");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertHandCount(playerA, 2);
        assertHandCount(playerB, 2);`)
	res := translateBody(t, `assertHandCount(playerA, 0);
        addCard(Zone.BATTLEFIELD, playerA, "Sergeant John Benton");
        attack(1, playerA, "Sergeant John Benton");
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertHandCount(playerA, 2);`)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	if n := len(res.Scenario.Expect); n != 1 {
		t.Fatalf("%d expects, want only the one written after execute()", n)
	}
	if !strings.Contains(res.Scenario.Why, "before execute()") {
		t.Fatalf("why %q does not note the dropped assert", res.Scenario.Why)
	}
}

// Teysa Karlov doubles Ministrant of Obligation's Afterlife trigger, and the
// test orders the two identical triggers with setChoice(playerA, "Afterlife"):
// the name of the keyword, not rule text starting "When". It is a trigger
// order like any other (gorge keeps the offered order), so it must not be
// placed as an answer to the sacrifice ask in front of it.
func TestKeywordNamedTriggerOrderChoiceIsDropped(t *testing.T) {
	playsToPass(t, `addCard(Zone.BATTLEFIELD, playerA, "Teysa Karlov");
        addCard(Zone.BATTLEFIELD, playerB, "Revel in Riches");
        addCard(Zone.BATTLEFIELD, playerA, "Bloodthrone Vampire");
        addCard(Zone.BATTLEFIELD, playerA, "Ministrant of Obligation");
        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Sacrifice");
        setChoice(playerA, "Ministrant of Obligation");
        setChoice(playerA, "Afterlife"); // order identical triggers
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertPowerToughness(playerA, "Bloodthrone Vampire", 3, 3);
        assertGraveyardCount(playerA, "Ministrant of Obligation", 1);
        assertPermanentCount(playerA, "Spirit Token", 4);`)
}

// addTarget(playerA, "Bear Cub", 2) names two different Bear Cubs: a repeated
// bare ref binds the first one twice and the runner rejects the second as
// not offered, so the pick for the later copies needs the #2 ordinal.
func TestRepeatedTargetPickNamesTheNextCopy(t *testing.T) {
	playsToPass(t, `setStrictChooseMode(true);
        addCard(Zone.BATTLEFIELD, playerA, "Bear Cub", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Forest", 4);
        addCard(Zone.BATTLEFIELD, playerB, "Sea Monster");
        addCard(Zone.HAND, playerA, "Terrific Team-Up");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Terrific Team-Up");
        addTarget(playerA, "Bear Cub", 2);
        addTarget(playerA, "Sea Monster");
        setStopAt(1, PhaseStep.PRECOMBAT_MAIN);
        execute();
        assertGraveyardCount(playerB, "Sea Monster", 1);
        assertPowerToughness(playerA, "Bear Cub", 3, 2, Filter.ComparisonScope.All);`)
}

// Clone's setChoice names the creature it copies and the test's addTarget
// then names the same card for a trigger of the permanent that entered
// (Irma, Part-Time Mutant copies "Craw Wurm", the Clone). Setup names only
// the opponent's Craw Wurm; the target is the copy, controlled by the caster.
// The ref cannot name it, so the shape is refused instead of answering with
// the opponent's creature.
func TestTargetNamingTheCopiedCardRefuses(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Irma, Part-Time Mutant");
        addCard(Zone.HAND, playerA, "Clone");
        addCard(Zone.BATTLEFIELD, playerA, "Island", 4);
        addCard(Zone.BATTLEFIELD, playerB, "Craw Wurm");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Clone");
        setChoice(playerA, true);
        setChoice(playerA, "Craw Wurm");
        addTarget(playerA, "Craw Wurm");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.POSTCOMBAT_MAIN);
        execute();
        assertPermanentCount(playerA, "Irma, Part-Time Mutant", 1);`)
	if res.Status != "refused" || res.Code != "copy-ambiguous-ref" {
		t.Fatalf("status %s code %q (%s), want refused copy-ambiguous-ref", res.Status, res.Code, res.Detail)
	}
}

// A count asserted by a name no card has (Awakening of Vitu-Ghazi renames the
// land it animates "Vitu-Ghazi"): the scenario's count reads the printed face
// name, so it can only read 0. The row refuses when the run fails that count;
// the same count by a token's name (Banana) or a part of a card's name
// (Avacyn) that the run satisfies stays a pass.
func TestCountByAnAnimatedNameRefusesOnlyWhenItFails(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Forest", 5);
        addCard(Zone.HAND, playerA, "Awakening of Vitu-Ghazi");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Awakening of Vitu-Ghazi");
        addTarget(playerA, "Forest");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertGraveyardCount(playerA, "Awakening of Vitu-Ghazi", 1);
        assertPermanentCount(playerA, "Vitu-Ghazi", 1);`)
	if res.Scenario == nil {
		t.Fatalf("refused early %s: %s", res.Code, res.Detail)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "refused" || res.Code != "count-name-not-a-card" {
		t.Fatalf("status %s code %q (%s), want refused count-name-not-a-card", res.Status, res.Code, res.Detail)
	}
	// a name part of a real card's name that the run satisfies stays a pass
	playsToPass(t, `addCard(Zone.BATTLEFIELD, playerA, "Forest");
        addCard(Zone.BATTLEFIELD, playerA, "Vitu-Ghazi, the City-Tree");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPermanentCount(playerA, "Vitu-Ghazi", 1);`)
	// a count that fails by reading too MANY (RoleTest: "Cursed" matches
	// Cursed Courtier as well as its Cursed Role) is not evidence of a rename
	res = translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Plains", 3);
        addCard(Zone.BATTLEFIELD, playerA, "Nexus Wardens");
        addCard(Zone.HAND, playerA, "Cursed Courtier");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cursed Courtier");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertPermanentCount(playerA, "Cursed", 1);`)
	if res.Scenario == nil {
		t.Fatalf("refused early %s: %s", res.Code, res.Detail)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "fail-expect" {
		t.Fatalf("status %s code %q (%v), want the fail-expect of a count that read 2", res.Status, res.Code, res.Fails)
	}
}
