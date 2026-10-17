package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// A setup-placed permanent's as-enters "choose a number" (Talion, the Kindly
// Lord, CR 614.12) is queued before setup as XMage's getAmount answer
// "X=<n>", so both engines enter the permanent with the same chosen number.
// Unscripted, XMage's AI chose its own and Talion's trigger#0.0 row compared
// two different numbers (WOE, lb4w2-diverge).
func TestSetupNumberChoiceIsQueuedBeforeSetup(t *testing.T) {
	setup := rules.OracleDecision{
		Step: -1, Seat: 0, Kind: "choose_n", Resume: "etb",
		Picks: []string{"1"}, PickKinds: []string{"number"},
	}
	cast := rules.OracleDecision{
		Step: 1, Seat: 1, Kind: "target", Via: "answer", GorgeKind: "target",
		Picks: []string{"a"}, PickRefs: []string{"p0"}, PickKinds: []string{"player"},
	}
	got := XAnswers([]rules.OracleDecision{setup, cast}, 3, nil)
	if len(got) == 0 || len(got[0]) == 0 || !reflect.DeepEqual(got[0][0], XAnswer{Seat: 0, Kind: "setup_choice", Value: "X=1"}) {
		t.Fatalf("setup number answers = %#v, want step zero led by setup_choice X=1", got)
	}
}

// Only a setup-time numeric as-enters pick is a setup number choice: a
// gameplay number pick keeps the step-time "choice" arm, and a non-numeric
// pick is not mapped.
func TestSetupNumberChoiceScope(t *testing.T) {
	for _, d := range []rules.OracleDecision{
		{Step: 0, Seat: 0, Kind: "choose_n", Resume: "etb", Picks: []string{"3"}, PickKinds: []string{"number"}},
		{Step: -1, Seat: 0, Kind: "choose_n", Resume: "etb", Picks: []string{"Human"}, PickKinds: []string{"type"}},
		{Step: -1, Seat: 0, Kind: "choose_n", Resume: "etb", Picks: []string{"odd"}, PickKinds: []string{"number"}},
	} {
		if _, ok := SetupNumberChoice(d); ok {
			t.Errorf("%+v classified as a setup number choice", d)
		}
	}
}
