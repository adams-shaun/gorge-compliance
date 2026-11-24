package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestActivateCostFixtureSelfNamed pins that a CARDNAME cost filter names the
// source itself (CR 118.3): no catalogue stand-in is offered as the payer, so
// the generator scripts no choice answer for it.
func TestActivateCostFixtureSelfNamed(t *testing.T) {
	for _, tok := range []string{
		"ExileFromGrave<1/CARDNAME/this card>",
		"ExileFromGrave<1/CARDNAME>",
	} {
		if got := filterCostFixturesX(tok, 0); got != nil {
			t.Errorf("filterCostFixturesX(%q) = %v, want none", tok, got)
		}
		if got := activationCostFixturesX(tok, 0); got != nil {
			t.Errorf("activationCostFixturesX(%q) = %v, want none", tok, got)
		}
	}
}

// TestActivateCostSelfNamedScriptsNoAnswer pins the generated level-B row of
// Morbius the Living Vampire (ExileFromGrave<1/CARDNAME/this card>): no cost
// fixture in the graveyard and no scripted choice for the self-exile.
func TestActivateCostSelfNamedScriptsNoAnswer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	req := activateRowRequirement(t, reg, "Morbius the Living Vampire", "activate#0.0")
	it, skip := GenerateB(reg, "Morbius the Living Vampire", req)
	if skip != nil {
		t.Fatalf("skipped: %s", skip.Reason)
	}
	for _, c := range it.Scenario.Setup["p0"].Graveyard {
		if c == "Colossal Dreadmaw" {
			t.Errorf("stray cost fixture %q in p0 graveyard", c)
		}
	}
	// answers[0] is the activation step's (the cost payment); the resolve
	// step's own Dig answers follow it and are legitimate.
	if len(it.XAnswers) == 0 || len(it.XAnswers[0]) != 0 {
		t.Errorf("activation step answers = %+v, want none", it.XAnswers)
	}
}
