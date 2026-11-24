package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// libraryPlacementDecision is the measured Harmonic Convergence CR 401.4
// arrange: a forced order over the cards one sweep put on top, first pick
// nearest the top.
func libraryPlacementDecision() rules.OracleDecision {
	return rules.OracleDecision{
		Step: 1, Seat: 0, Kind: "order", Options: 3,
		Picks:           []string{"Island", "Forest", "Mountain"},
		PickIdx:         []int{0, 1, 2},
		PickRefs:        []string{"p0:Island", "p0:Forest", "p0:Mountain"},
		PickRefsInexact: []bool{false, false, false},
		OptionRefs:      []string{"p0:Island", "p0:Forest", "p0:Mountain"},
		ArrangeLabels:   []string{"Island", "Forest", "Mountain"},
		ArrangeKind:     "library_placement",
		ObjectPicks:     []string{"p0:Island", "p0:Forest", "p0:Mountain"},
		PickKinds:       []string{"library_placement", "library_placement", "library_placement"},
		Via:             "answer", GorgeKind: "arrange", First: "Island", Min: 3, Max: 3,
	}
}

// libraryHandMoveDecision is the measured Stunted Growth hand selection: three
// cards chosen from hand, first pick nearest the top.
func libraryHandMoveDecision() rules.OracleDecision {
	return rules.OracleDecision{
		Step: 1, Seat: 1, Kind: "choose_n", Options: 3,
		Picks:           []string{"Island", "Forest", "Mountain"},
		PickIdx:         []int{0, 1, 2},
		PickRefs:        []string{"p1:Island", "p1:Forest", "p1:Mountain"},
		PickRefsInexact: []bool{true, true, true},
		OptionRefs:      []string{"p1:Island", "p1:Forest", "p1:Mountain"},
		ObjectPicks:     []string{"p1:Island", "p1:Forest", "p1:Mountain"},
		PickKinds:       []string{"hand_move", "hand_move", "hand_move"},
		Resume:          "hand_move", Via: "answer", GorgeKind: "choose",
		First: "Island", Min: 3, Max: 3,
	}
}

// libraryTopSnaps is the checkpoint after step 1 for a seat whose library top
// is top.
func libraryTopSnaps(seat int, top []string) []rules.OracleSnapshot {
	players := []rules.OracleSnapPlayer{{Seat: 0}, {Seat: 1}}
	players[seat].LibraryTop = top
	return []rules.OracleSnapshot{
		{},
		{},
		{Players: players},
	}
}

func TestLibraryTopPlacement(t *testing.T) {
	arrange := libraryPlacementDecision()
	if !LibraryTopPlacement(arrange, nil) {
		t.Errorf("shape-only library_placement is the measured top arrangement")
	}
	if !LibraryTopPlacement(arrange, libraryTopSnaps(0, []string{"Island", "Forest", "Mountain"})) {
		t.Errorf("library_placement with the picks at the top was not detected")
	}
	if LibraryTopPlacement(arrange, libraryTopSnaps(0, []string{"Mountain", "Island", "Forest"})) {
		t.Errorf("library_placement with the picks below the top was called a top placement")
	}

	hand := libraryHandMoveDecision()
	if LibraryTopPlacement(hand, nil) {
		t.Errorf("a hand_move has no shape-only destination evidence")
	}
	if !LibraryTopPlacement(hand, libraryTopSnaps(1, []string{"Island", "Forest", "Mountain", "Plains", "Swamp"})) {
		t.Errorf("hand_move with the picks at the top was not detected")
	}
	if LibraryTopPlacement(hand, libraryTopSnaps(1, []string{"Mountain", "Island", "Forest", "Plains", "Swamp"})) {
		t.Errorf("hand_move with the picks below the top was called a top placement")
	}
	// A hand -> battlefield move leaves the picks in Permanents, not the
	// library top, even when the same names are in the library.
	snaps := libraryTopSnaps(1, []string{"Island", "Forest", "Mountain", "Plains", "Swamp"})
	snaps[2].Permanents = []rules.OracleSnapPerm{
		{Name: "Island"}, {Name: "Forest"}, {Name: "Mountain"},
	}
	if LibraryTopPlacement(hand, snaps) {
		t.Errorf("a hand -> battlefield move was called a top placement")
	}
}

func TestXAnswersLibraryTopPlacement(t *testing.T) {
	sc := Scenario{Steps: []Step{{Op: "pass"}, {Op: "pass"}}}

	t.Run("library_placement top", func(t *testing.T) {
		res := rules.OracleResult{
			Decisions: []rules.OracleDecision{libraryPlacementDecision()},
			Snapshots: libraryTopSnaps(0, []string{"Island", "Forest", "Mountain"}),
		}
		got := XAnswersForScenario(nil, res, sc, nil, nil)
		want := []XAnswer{{0, "choice", "Mountain"}, {0, "choice", "Forest"}}
		if !reflect.DeepEqual(got[1], want) {
			t.Errorf("answers = %+v, want %+v", got[1], want)
		}
	})

	t.Run("library_placement bottom", func(t *testing.T) {
		res := rules.OracleResult{
			Decisions: []rules.OracleDecision{libraryPlacementDecision()},
			Snapshots: libraryTopSnaps(0, []string{"Mountain", "Island", "Forest"}),
		}
		got := XAnswersForScenario(nil, res, sc, nil, nil)
		want := []XAnswer{{0, "choice", "Island"}, {0, "choice", "Forest"}, {0, "choice", "Mountain"}}
		if !reflect.DeepEqual(got[1], want) {
			t.Errorf("answers = %+v, want %+v", got[1], want)
		}
	})

	t.Run("hand_move top: selection then order answers", func(t *testing.T) {
		res := rules.OracleResult{
			Decisions: []rules.OracleDecision{libraryHandMoveDecision()},
			Snapshots: libraryTopSnaps(1, []string{"Island", "Forest", "Mountain", "Plains", "Swamp"}),
		}
		got := XAnswersForScenario(nil, res, sc, nil, nil)
		want := []XAnswer{
			{1, "choice", "Island^Forest^Mountain"},
			{1, "choice", "Mountain"},
			{1, "choice", "Forest"},
		}
		if !reflect.DeepEqual(got[1], want) {
			t.Errorf("answers = %+v, want %+v", got[1], want)
		}
	})
}
