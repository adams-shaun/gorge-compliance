package templates_test

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/engine/rules"
)

// TestDoorSetupChoiceScripted: Door of Destinies' level-B static serving path
// left its as-enters "choose a creature type" ask unanswered -- the static
// derivation transcribed only the cast-path bases, so the item's xmage_answers
// was nil and the XMage replay failed on the unscripted choice dialog (the
// same failure shape that unscripted coin flips produced). The fix transcribes
// the runner's recorded Step<0 decision ("setup fallback" in the transcript)
// into a setup_choice answer, exactly as the cast path does.
func TestDoorSetupChoiceScripted(t *testing.T) {
	reg := corpusReg(t)
	it, skip := templates.ItemFor(reg, "Door of Destinies", "static#0.0")
	if skip != nil {
		t.Fatalf("Door of Destinies static#0.0: %s", skip.Reason)
	}
	if len(it.Scenario.Steps) != 0 {
		t.Fatalf("steps = %d, want the pure-serving path's 0", len(it.Scenario.Steps))
	}
	if len(it.XAnswers) != 1 || len(it.XAnswers[0]) != 1 {
		t.Fatalf("xmage_answers = %v, want one setup_choice at slot 0", it.XAnswers)
	}
	a := it.XAnswers[0][0]
	if a.Seat != 0 || a.Kind != "setup_choice" || a.Value != "Bear" {
		t.Fatalf("answer = %+v, want seat 0 setup_choice Bear", a)
	}
	// The item still replays clean in gorge.
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil || len(res.Fails) != 0 {
		t.Fatalf("replay: err=%v fails=%v", err, res.Fails)
	}
	// The runner still records the decision the answer transcribes.
	if len(res.Decisions) != 1 || res.Decisions[0].Step != -1 || !slices.Equal(res.Decisions[0].Picks, []string{"Bear"}) {
		t.Fatalf("decisions = %+v, want the recorded choose_n with pick [Bear]", res.Decisions)
	}
	if res.Decisions[0].PickKinds[0] != "type" {
		t.Fatalf("pick kind = %s, want type", res.Decisions[0].PickKinds[0])
	}

	// A card with no as-enters ask and no coin flip keeps its item bytes
	// unchanged: Shock's cast-resolve sha equals the sha the committed
	// compliance verdict records for the row (oracle/compliance/verdicts/).
	shock, skip := templates.ItemFor(reg, "Shock", "cast-resolve")
	if skip != nil {
		t.Fatalf("Shock cast-resolve: %s", skip.Reason)
	}
	const shockCommitted = "531de81afe3d538d2dfdb8550ce0e3b79e1d46a7019c7c4c07774b3955f53bc5"
	if got := gate.ItemSHA(shock); got != shockCommitted {
		t.Errorf("Shock cast-resolve sha %s moved off the committed verdict's %s", got, shockCommitted)
	}
	if len(shock.XAnswers) != 0 {
		t.Errorf("Shock xmage_answers = %v, want none", shock.XAnswers)
	}
}

// TestManaCryptFlipAnswers: Mana Crypt's upkeep coin flip killed the XMage
// replay with "AssertionError: Missing FLIP COIN RESULT def for turn 1, step
// UPKEEP, PlayerA". The runner now records each flip (rules.OracleResult),
// and the level-B items script them: the drive-window flip at turn 1 is a
// setup_flip answer queued before XMage's build(), the turn-3 step flip is a
// flip answer queued while its step executes.
func TestManaCryptFlipAnswers(t *testing.T) {
	reg := corpusReg(t)
	// The requirement itself is the precondition: trigger#0.0 only exists on
	// the card that carries Mana Crypt's "flip a coin" upkeep trigger.
	trig, skip := templates.ItemFor(reg, "Mana Crypt", "trigger#0.0")
	if skip != nil {
		t.Fatalf("Mana Crypt trigger#0.0: %s", skip.Reason)
	}
	want := [][]oraclegen.XAnswer{
		{{Seat: 0, Kind: "setup_flip", Value: "lose"}},
		{{Seat: 0, Kind: "flip", Value: "win"}},
	}
	if len(trig.XAnswers) != 2 || trig.XAnswers[0][0] != want[0][0] || trig.XAnswers[1][0] != want[1][0] {
		t.Fatalf("trigger#0.0 xmage_answers = %v, want %v", trig.XAnswers, want)
	}
	// The win flip is the upkeep trigger's resolution after the turn-3
	// pass_to; the drive-window loss is turn 1's.
	res, err := rules.RunOracleScenarioJSON(reg, trig.Raw())
	if err != nil || len(res.Fails) != 0 {
		t.Fatalf("trigger#0.0 replay: err=%v fails=%v", err, res.Fails)
	}
	if len(res.Flips) != 2 || res.Flips[0] != (rules.OracleFlip{Turn: 1, Step: -1, Seat: 0, Win: false}) ||
		res.Flips[1] != (rules.OracleFlip{Turn: 3, Step: 1, Seat: 0, Win: true}) {
		t.Fatalf("trigger#0.0 flips = %v, want the turn-1 loss and the turn-3 win", res.Flips)
	}

	act, skip := templates.ItemFor(reg, "Mana Crypt", "activate#0.0")
	if skip != nil {
		t.Fatalf("Mana Crypt activate#0.0: %s", skip.Reason)
	}
	if len(act.XAnswers) != 1 || act.XAnswers[0][0] != (oraclegen.XAnswer{Seat: 0, Kind: "setup_flip", Value: "lose"}) {
		t.Fatalf("activate#0.0 xmage_answers = %v, want one setup_flip lose", act.XAnswers)
	}
}
