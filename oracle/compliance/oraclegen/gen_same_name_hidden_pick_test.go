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

// TestSameNameHiddenPickContiguousGraveyardKeepsBareName pins xproof's two
// Acolyte of Affliction finds (xs:XP:acolyte-of-affliction:cast:s2 and
// :f0t0:s2): the ETB mills two Forests, then asks "you may return a permanent
// card from your graveyard to your hand" over seven setup Swamps plus the two
// milled Forests. Gorge picks p0:Swamp#4 (the first graveyard Swamp, ref
// rank-derived), so the name-only classifier calls the pick unanswerable and
// the generic route withheld it; XMage still posed its up-to-one pick with an
// empty CHOICE queue and strict-missed. The seven Swamps are one contiguous
// run in the graveyard, so removing any one leaves the same name sequence:
// the bare name is an exact answer for the outcome gorge produced.
func TestSameNameHiddenPickContiguousGraveyardKeepsBareName(t *testing.T) {
	d := rules.OracleDecision{
		Step: 8, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "hidden_pick",
		Options: 9, Min: 0, Max: 1,
		Picks: []string{"Swamp"}, PickRefs: []string{"p0:Swamp#4"}, PickIdx: []int{0},
		PickKinds: []string{"hidden_pick"},
		OptionRefs: []string{"p0:Swamp#4", "p0:Swamp#5", "p0:Swamp#6", "p0:Swamp#7",
			"p0:Swamp#8", "p0:Swamp#9", "p0:Swamp#10", "p0:Forest#2", "p0:Forest#3"},
		PickRefsInexact: []bool{true},
	}
	snaps := make([]rules.OracleSnapshot, 10)
	// Checkpoint before the step: the seven setup Swamps.
	snaps[8] = rules.OracleSnapshot{Players: []rules.OracleSnapPlayer{
		{Seat: 0, Graveyard: []string{"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp"}},
	}}
	// After it: the milled Forests joined, one Swamp went to hand.
	snaps[9] = rules.OracleSnapshot{Players: []rules.OracleSnapPlayer{
		{Seat: 0, Graveyard: []string{"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Forest", "Forest"}},
	}}
	// Shape alone still withholds; the snapshots prove the contiguous run.
	if !hiddenPickUnanswerable(d) {
		t.Fatalf("fixture must be an unanswerable same-name pick without snapshots")
	}
	if hiddenPickWithheld(d, snaps) {
		t.Fatalf("a contiguous same-name graveyard run was withheld")
	}
	got := xanswersSetup([]rules.OracleDecision{d}, 10, nil, nil, nil, nil, xanswersExtra{snaps: snaps})
	if len(got[8]) != 1 || got[8][0].Value != "Swamp" {
		t.Fatalf("contiguous-graveyard answer = %+v, want one bare Swamp", got[8])
	}

	// A filtered ask (only some graveyard cards offered) still reads the run
	// from the graveyard itself, not from the offered list: the Forest is not
	// offered, but the two Grizzly Bears stay adjacent and interchangeable.
	filtered := rules.OracleDecision{
		Step: 8, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "hidden_pick",
		Options: 3, Min: 0, Max: 1,
		Picks: []string{"Grizzly Bears"}, PickRefs: []string{"p0:Grizzly Bears#4"}, PickIdx: []int{0},
		PickKinds:       []string{"hidden_pick"},
		OptionRefs:      []string{"p0:Grizzly Bears#4", "p0:Grizzly Bears#5", "p0:Elvish Mystic#2"},
		PickRefsInexact: []bool{true},
	}
	snaps[8].Players[0].Graveyard = []string{"Grizzly Bears", "Grizzly Bears", "Forest"}
	snaps[9].Players[0].Graveyard = []string{"Forest", "Grizzly Bears"}
	if hiddenPickWithheld(filtered, snaps) {
		t.Fatalf("a contiguous same-name run behind a filtered ask was withheld")
	}
	got = xanswersSetup([]rules.OracleDecision{filtered}, 10, nil, nil, nil, nil, xanswersExtra{snaps: snaps})
	if len(got[8]) != 1 || got[8][0].Value != "Grizzly Bears" {
		t.Fatalf("filtered-graveyard answer = %+v, want one bare Grizzly Bears", got[8])
	}

	// Pothole Mole: two Forests either side of a Swamp are not one run, so
	// removing either leaves a different name sequence and the pick stays
	// withheld.
	pothole := rules.OracleDecision{
		Step: 8, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "hidden_pick",
		Options: 3, Min: 0, Max: 1,
		Picks: []string{"Forest"}, PickRefs: []string{"p0:Forest#4"}, PickIdx: []int{0},
		PickKinds:       []string{"hidden_pick"},
		OptionRefs:      []string{"p0:Forest#4", "p0:Swamp#4", "p0:Forest#5"},
		PickRefsInexact: []bool{true},
	}
	snaps[8].Players[0].Graveyard = []string{"Forest", "Swamp", "Forest"}
	snaps[9].Players[0].Graveyard = []string{"Swamp", "Forest"}
	if !hiddenPickWithheld(pothole, snaps) {
		t.Fatalf("a broken same-name graveyard run must stay withheld")
	}
	for _, xa := range xanswersSetup([]rules.OracleDecision{pothole}, 10, nil, nil, nil, nil, xanswersExtra{snaps: snaps}) {
		for _, a := range xa {
			t.Fatalf("broken-run hidden pick answer %+v scripted; want withheld", a)
		}
	}
}

// TestSameNameHiddenPickAllCopiesKeepsBareName pins DSK Greenhouse // Rickety
// Gazebo (mill four, "return up to two permanent cards"): all four offered
// objects are p0's Wastes, so which one gorge returns is invisible to the
// compared state. Withholding the pick left the ask to XMage's AI, which took
// two ([Wastes, Wastes] against gorge's one). The bare label stays scripted.
func TestSameNameHiddenPickAllCopiesKeepsBareName(t *testing.T) {
	d := rules.OracleDecision{
		Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "hidden_pick",
		Options: 4, Min: 0, Max: 2,
		Picks: []string{"Wastes"}, PickRefs: []string{"p0:Wastes#27"}, PickIdx: []int{0},
		PickKinds:       []string{"hidden_pick"},
		OptionRefs:      []string{"p0:Wastes#27", "p0:Wastes#39", "p0:Wastes#4", "p0:Wastes#2"},
		PickRefsInexact: []bool{true},
	}
	if p := ClassifySameName(d, 0); !p.Unanswerable {
		t.Fatalf("fixture must be an unanswerable same-name pick: %+v", p)
	}
	if hiddenPickUnanswerable(d) {
		t.Fatalf("an all-copies ask was withheld")
	}
	if xa := xanswersSetup([]rules.OracleDecision{d}, 2, nil, nil, nil, nil); len(xa[1]) < 1 || xa[1][0].Value != "Wastes" {
		t.Fatalf("all-copies answer = %+v, want the bare Wastes first (the up-to ask is then closed with a skip)", xa[1])
	}

	// A different name among the offers, another seat's same-named card, or
	// a token copy keeps the withhold.
	for name, refs := range map[string][]string{
		"other name":  {"p0:Wastes#27", "p0:Wastes#39", "p0:Forest#4"},
		"other seat":  {"p0:Wastes#27", "p0:Wastes#39", "p1:Wastes#4"},
		"token among": {"p0:Wastes#27", "p0:Wastes#39", "p0:token:Wastes"},
	} {
		w := d
		w.OptionRefs = refs
		if !hiddenPickUnanswerable(w) {
			t.Errorf("%s: pick not withheld", name)
		}
	}
}
