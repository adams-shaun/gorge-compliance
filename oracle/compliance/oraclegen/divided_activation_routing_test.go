package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// An ACTIVATION whose own target ask is divided (DividedAsYouChoose$: The
// Earth Crystal's two +1/+1 counters, Chandra, Flameshaper's 8 damage) is
// XMage's TargetAmount, whose chooser rejects the bare alias the driver queues
// from the step's targets ("Must be target amount, but found unknown
// target"). The ask is scripted in the "<ref>^X=<share>" form instead, and the
// driver then skips the step's bare targets. A CAST step keeps its castSpell
// route (no answer), so no cast scenario's bytes move.
func TestDividedActivationTargetsScriptedAsShares(t *testing.T) {
	ask := rules.OracleDecision{Step: 0, Seat: 0, Kind: "target", Via: "target", Divided: 2,
		Options: 1, Min: 1, Max: 2, Picks: []string{"Grizzly Bears (a)"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Grizzly Bears"}}
	for _, tc := range []struct {
		op   string
		want []XAnswer
	}{
		{"activate", []XAnswer{{0, "target", "p0:Grizzly Bears^X=2"}}},
		{"cast", nil},
	} {
		sc := Scenario{Steps: []Step{{Op: tc.op, Seat: 0, Card: "p0:X", Targets: []string{"p0:Grizzly Bears"}}, {Op: "resolve"}}}
		got := xanswersForScenario(rules.OracleResult{Decisions: []rules.OracleDecision{ask}}, sc, nil, map[int]bool{0: true})
		var step0 []XAnswer
		if len(got) > 0 {
			step0 = got[0]
		}
		if !reflect.DeepEqual(step0, tc.want) {
			t.Errorf("%s step answers = %v, want %v", tc.op, step0, tc.want)
		}
	}
	// The undivided activation keeps its step targets unscripted.
	plain := ask
	plain.Divided = 0
	sc := Scenario{Steps: []Step{{Op: "activate", Seat: 0, Card: "p0:X", Targets: []string{"p0:Grizzly Bears"}}}}
	if got := xanswersForScenario(rules.OracleResult{Decisions: []rules.OracleDecision{plain}}, sc, nil, map[int]bool{0: true}); len(got) > 0 && len(got[0]) > 0 {
		t.Errorf("undivided activation scripted %v, want nothing", got[0])
	}
}

// withoutDividedActivations never mutates the caller's map.
func TestWithoutDividedActivationsCopies(t *testing.T) {
	in := map[int]bool{0: true, 1: true}
	ds := []rules.OracleDecision{{Step: 1, Kind: "target", Via: "target", Divided: 3, PickRefs: []string{"p1"}}}
	sc := Scenario{Steps: []Step{{Op: "cast"}, {Op: "activate"}}}
	out := withoutDividedActivations(in, ds, sc)
	if !in[1] || len(in) != 2 {
		t.Fatalf("input mutated: %v", in)
	}
	if out[1] || !out[0] {
		t.Fatalf("out = %v, want step 1 dropped and step 0 kept", out)
	}
}
