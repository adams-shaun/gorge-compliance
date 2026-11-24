package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
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
	if LibraryTopPlacement(arrange, nil) {
		t.Errorf("a library_placement with no checkpoint has no destination evidence")
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

func TestLibraryTopPlacementsHonorsAlternative(t *testing.T) {
	// A top ask is explicit evidence even without a checkpoint.
	top := libraryHandMoveDecision()
	topAlt := rules.OracleDecision{
		Step: 1, Seat: 0, Kind: "choose_n", Options: 2,
		Picks: []string{"top"}, PickKinds: []string{"primary"},
		Resume: "changezone_alternative", GorgeKind: "choose", First: "top", Min: 1, Max: 1,
	}
	if got := LibraryTopPlacements([]rules.OracleDecision{top, topAlt}, nil); !got[0] {
		t.Errorf("top alternative did not mark the batch a top placement: %v", got)
	}

	// A bottom ask wins over a checkpoint that cannot tell a bottom placement
	// into an empty library from a top one.
	bottom := libraryHandMoveDecision()
	bottomAlt := topAlt
	bottomAlt.Picks = []string{"bottom"}
	bottomAlt.PickKinds = []string{"bottom"}
	empty := libraryTopSnaps(1, []string{"Island", "Forest", "Mountain"})
	if got := LibraryTopPlacements([]rules.OracleDecision{bottom, bottomAlt}, empty); got[0] {
		t.Errorf("bottom alternative was overridden by the empty-library top: %v", got)
	}
	if !LibraryTopPlacement(bottom, empty) {
		t.Errorf("precondition: the empty-library top alone matches the picks")
	}

	// Two batches in one step take successive alternatives, and each
	// alternative is consumed once.
	second := libraryHandMoveDecision()
	second.Picks = []string{"Mountain", "Island"}
	second.PickRefs = []string{"p1:Mountain", "p1:Island"}
	ds := []rules.OracleDecision{top, topAlt, second, bottomAlt}
	got := LibraryTopPlacements(ds, nil)
	if !got[0] || got[2] {
		t.Errorf("successive alternatives paired wrong: %v", got)
	}
}

func TestHandMoveOrderAsk(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	// Stunted Growth's targeted chooser is the dialog shape XMage implements
	// with putCardsOnTopOfLibrary(..., anyOrder=true).
	stunted := libraryHandMoveDecision()
	sc := Scenario{Steps: []Step{
		{Op: "cast", Seat: 0, Card: "p0:Stunted Growth"},
		{Op: "resolve", Seat: 0},
	}}
	snaps := []rules.OracleSnapshot{
		{},
		{Stack: []rules.OracleSnapStack{{Kind: "spell", Source: "p0:Stunted Growth", Controller: 0}}},
		{},
	}
	if !HandMoveOrderAsk(reg, sc, snaps, stunted) {
		t.Errorf("Stunted Growth's targeted hand-to-top move was not recognised as an order-dialog effect")
	}

	// Brainstorm is the non-order-dialog control: its caster is also the
	// chooser, and its effect moves the cards without the order ask.
	brainstorm := libraryHandMoveDecision()
	brainstorm.Seat = 0
	bsc := Scenario{Steps: []Step{
		{Op: "cast", Seat: 0, Card: "p0:Brainstorm"},
		{Op: "resolve", Seat: 0},
	}}
	bsnaps := []rules.OracleSnapshot{
		{},
		{Stack: []rules.OracleSnapStack{{Kind: "spell", Source: "p0:Brainstorm", Controller: 0}}},
		{},
	}
	if HandMoveOrderAsk(reg, bsc, bsnaps, brainstorm) {
		t.Errorf("Brainstorm was treated as an order-dialog effect")
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

	t.Run("hand_move top: selection only without a verified order dialog", func(t *testing.T) {
		// XAnswersForScenario with no registry cannot verify the resolving
		// card's dialog shape, so it scripts the selection and refuses to
		// guess the order answers.
		res := rules.OracleResult{
			Decisions: []rules.OracleDecision{libraryHandMoveDecision()},
			Snapshots: libraryTopSnaps(1, []string{"Island", "Forest", "Mountain", "Plains", "Swamp"}),
		}
		got := XAnswersForScenario(nil, res, sc, nil, nil)
		want := []XAnswer{{1, "choice", "Island^Forest^Mountain"}}
		if !reflect.DeepEqual(got[1], want) {
			t.Errorf("answers = %+v, want %+v", got[1], want)
		}
	})

	t.Run("hand_move top with a verified order dialog", func(t *testing.T) {
		// The order answers are emitted only for the decision marked by the
		// card-IR gate, and they follow the selection last-first.
		hand := libraryHandMoveDecision()
		out := xanswersSetup([]rules.OracleDecision{hand}, 2, nil, nil, nil, nil, xanswersExtra{
			snaps:     libraryTopSnaps(1, []string{"Island", "Forest", "Mountain", "Plains", "Swamp"}),
			orderAsks: map[int]bool{0: true},
		})
		want := []XAnswer{
			{1, "choice", "Island^Forest^Mountain"},
			{1, "choice", "Mountain"},
			{1, "choice", "Forest"},
		}
		if !reflect.DeepEqual(out[1], want) {
			t.Errorf("answers = %+v, want %+v", out[1], want)
		}
	})

	t.Run("library_placement bottom without snapshots keeps gorge order", func(t *testing.T) {
		// A no-checkpoint caller must not assume top: the shape is the same
		// for a bottom placement, and the historical answer kept gorge's
		// order.
		got := XAnswers([]rules.OracleDecision{libraryPlacementDecision()}, 2, nil)
		want := []XAnswer{{0, "choice", "Island"}, {0, "choice", "Forest"}, {0, "choice", "Mountain"}}
		if !reflect.DeepEqual(got[1], want) {
			t.Errorf("answers = %+v, want %+v", got[1], want)
		}
	})
}
