package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestSameNameHiddenPickNeverBareName pins DFT xe:RAND:1:5:w13 (Pothole Mole's
// "you may return a land card from your graveyard to your hand"): gorge picks
// the older setup Forest of two same-named p0 graveyard cards, and its ref is
// rank-derived (pick_refs_inexact). XMage's makeChoose matches a bare label by
// name alone, so it would return whichever Forest it meets first. The owner
// does not separate the candidates (both are p0's), so no exact answer exists
// and the lowering must withhold the pick rather than script the bare name
// (CR 608.2d: the player's choice names one object).
func TestSameNameHiddenPickNeverBareName(t *testing.T) {
	d := rules.OracleDecision{
		Step: 11, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "hidden_pick",
		Options: 3, Min: 0, Max: 1,
		Picks: []string{"Forest"}, PickRefs: []string{"p0:Forest#4"}, PickIdx: []int{0},
		PickKinds:       []string{"hidden_pick"},
		OptionRefs:      []string{"p0:Forest#4", "p0:Swamp#4", "p0:Forest#5"},
		PickRefsInexact: []bool{true},
	}
	// Precondition: no exact identity exists for this pick.
	if p := ClassifySameName(d, 0); !p.Ambiguous || !p.Unanswerable || p.Alias != "" {
		t.Fatalf("fixture must be an unanswerable same-name pick: %+v", p)
	}
	if ownerQualifiedRef(d, 0) != "" {
		t.Fatalf("fixture must not be separable by owner")
	}
	// xanswersSetup returns nil when no step scripts an answer.
	for _, xa := range xanswersSetup([]rules.OracleDecision{d}, 12, nil, nil, nil, nil) {
		for _, a := range xa {
			t.Fatalf("hidden pick answer %+v scripted; want the unanswerable pick withheld (a bare name matches both Forests)", a)
		}
	}

	// Controls: an exact same-name pick still carries its alias, and a pick
	// whose name is unique keeps the plain label.
	exact := d
	exact.PickRefsInexact = []bool{false}
	if xa := xanswersSetup([]rules.OracleDecision{exact}, 12, nil, nil, nil, nil); len(xa[11]) != 1 || xa[11][0].Value != "@p0:Forest#4" {
		t.Fatalf("exact answer = %+v, want @p0:Forest#4", xa[11])
	}
	uniq := d
	uniq.OptionRefs = []string{"p0:Forest#4", "p0:Swamp#4"}
	if xa := xanswersSetup([]rules.OracleDecision{uniq}, 12, nil, nil, nil, nil); len(xa[11]) != 1 || xa[11][0].Value != "Forest" {
		t.Fatalf("unique-name answer = %+v, want Forest", xa[11])
	}
}
