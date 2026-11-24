package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
)

// libraryHandMove is the measured Dream Cache hand -> library batch: gorge
// chose Island then Forest for the top, so Island ends topmost. PickRefsInexact
// is [true,false] here (the archived shape is [true,true]) so the reversal is
// observable in that array as well.
func libraryHandMove() rules.OracleDecision {
	return rules.OracleDecision{
		Step: 1, Seat: 0, Kind: "choose_n", Options: 3,
		Picks:           []string{"Island", "Forest"},
		PickIdx:         []int{0, 1},
		PickRefs:        []string{"p0:Island#4", "p0:Forest"},
		PickRefsInexact: []bool{true, false},
		OptionRefs:      []string{"p0:Island#4", "p0:Forest", "p0:Mountain"},
		ObjectPicks:     []string{"p0:Island#4", "p0:Forest"},
		PickKinds:       []string{"hand_move", "hand_move"},
		Resume:          "hand_move", Via: "answer", GorgeKind: "choose",
		First: "Island", Min: 2, Max: 2,
	}
}

// libraryAlternative is the spell's follow-up "top"/"bottom" ask. It is the
// spell controller's position ask, so its seat can differ from the batch's;
// First is "top" for both alternatives.
func libraryAlternative(pick string, idx int, kind string) rules.OracleDecision {
	return rules.OracleDecision{
		Step: 1, Seat: 1, Kind: "choose_n", Options: 2,
		Picks: []string{pick}, PickIdx: []int{idx}, PickRefs: []string{pick},
		PickRefsInexact: []bool{false}, PickKinds: []string{kind},
		Resume: "changezone_alternative", Via: "answer", GorgeKind: "choose",
		First: "top", Min: 1, Max: 1,
	}
}

// libraryPlacement is the measured Harmonic Convergence CR 401.4 arrange: a
// forced "order" over the enchantments one ChangeZoneAll sweep put on top,
// first pick nearest the top. Unlike a hand_move batch it has no card
// selection and no changezone_alternative; the arrangement itself is the whole
// answer.
func libraryPlacement() rules.OracleDecision {
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

// libraryTopSnapshot is the checkpoint after step 1 for a seat whose library
// top is top. Its two entries after the picked cards tell a real top placement
// from a hand -> battlefield move whose same-named cards happen to be in the
// library.
func libraryTopSnapshot(top []string) []rules.OracleSnapshot {
	return []rules.OracleSnapshot{
		{},
		{},
		{Players: []rules.OracleSnapPlayer{{Seat: 0, LibraryTop: top}}},
	}
}

// libraryDecisionCopy deep-copies decisions through their JSON form, so a
// rewrite that reverses an array in place changes the copy's marker too.
func libraryDecisionCopy(t *testing.T, ds []rules.OracleDecision) []rules.OracleDecision {
	t.Helper()
	b, err := json.Marshal(ds)
	if err != nil {
		t.Fatalf("marshal decisions: %v", err)
	}
	var cp []rules.OracleDecision
	if err := json.Unmarshal(b, &cp); err != nil {
		t.Fatalf("unmarshal decisions: %v", err)
	}
	return cp
}

// TestForgeLibraryTopOrder: a hand_move batch paired with a "top"
// changezone_alternative in the same step is exported in reverse so Forge's
// one-card-at-a-time "put it on top" asks rebuild gorge's top-first order; a
// "bottom" pair, an unpaired batch, and a decision that is not a multi-card
// hand_move are left alone, and the input is never mutated.
func TestForgeLibraryTopOrder(t *testing.T) {
	t.Run("top pair reversed", func(t *testing.T) {
		ds := []rules.OracleDecision{libraryHandMove(), libraryAlternative("top", 0, "primary")}

		got := forgeLibraryTopOrder(ds, nil)
		if len(got) != len(ds) {
			t.Fatalf("%d decisions, want %d: %+v", len(got), len(ds), got)
		}
		d := got[0]
		if want := []string{"Forest", "Island"}; !reflect.DeepEqual(d.Picks, want) {
			t.Errorf("picks = %v, want %v", d.Picks, want)
		}
		if want := []string{"p0:Forest", "p0:Island#4"}; !reflect.DeepEqual(d.PickRefs, want) {
			t.Errorf("pick_refs = %v, want %v", d.PickRefs, want)
		}
		if want := []int{1, 0}; !reflect.DeepEqual(d.PickIdx, want) {
			t.Errorf("pick_idx = %v, want %v", d.PickIdx, want)
		}
		if want := []bool{false, true}; !reflect.DeepEqual(d.PickRefsInexact, want) {
			t.Errorf("pick_refs_inexact = %v, want %v", d.PickRefsInexact, want)
		}
		if want := []string{"p0:Forest", "p0:Island#4"}; !reflect.DeepEqual(d.ObjectPicks, want) {
			t.Errorf("object_picks = %v, want %v", d.ObjectPicks, want)
		}
		// All picks moved the hand, so the kinds are the same reversed.
		if want := []string{"hand_move", "hand_move"}; !reflect.DeepEqual(d.PickKinds, want) {
			t.Errorf("pick_kinds = %v, want %v", d.PickKinds, want)
		}
		// First is option 0's label, not the first pick, and the offered
		// options do not reorder with the picks.
		if d.First != "Island" {
			t.Errorf("first = %q, want Island (option 0's label)", d.First)
		}
		if want := []string{"p0:Island#4", "p0:Forest", "p0:Mountain"}; !reflect.DeepEqual(d.OptionRefs, want) {
			t.Errorf("option_refs = %v, want %v", d.OptionRefs, want)
		}
		// The paired alternative is a decision in its own right, unchanged.
		if !reflect.DeepEqual(got[1], ds[1]) {
			t.Errorf("paired alternative changed: %+v", got[1])
		}
	})

	t.Run("library_placement top reversed", func(t *testing.T) {
		ds := []rules.OracleDecision{libraryPlacement()}
		top := libraryTopSnapshot([]string{"Island", "Forest", "Mountain"})

		got := forgeLibraryTopOrder(ds, top)
		d := got[0]
		if want := []string{"Mountain", "Forest", "Island"}; !reflect.DeepEqual(d.Picks, want) {
			t.Errorf("picks = %v, want %v", d.Picks, want)
		}
		if want := []string{"p0:Mountain", "p0:Forest", "p0:Island"}; !reflect.DeepEqual(d.PickRefs, want) {
			t.Errorf("pick_refs = %v, want %v", d.PickRefs, want)
		}
		if want := []int{2, 1, 0}; !reflect.DeepEqual(d.PickIdx, want) {
			t.Errorf("pick_idx = %v, want %v", d.PickIdx, want)
		}
		if want := []string{"p0:Mountain", "p0:Forest", "p0:Island"}; !reflect.DeepEqual(d.ObjectPicks, want) {
			t.Errorf("object_picks = %v, want %v", d.ObjectPicks, want)
		}
		// The offered order is not pick order: the options and labels stay.
		if want := []string{"p0:Island", "p0:Forest", "p0:Mountain"}; !reflect.DeepEqual(d.OptionRefs, want) {
			t.Errorf("option_refs = %v, want %v", d.OptionRefs, want)
		}
		if want := []string{"Island", "Forest", "Mountain"}; !reflect.DeepEqual(d.ArrangeLabels, want) {
			t.Errorf("arrange_labels = %v, want %v", d.ArrangeLabels, want)
		}
		if d.First != "Island" {
			t.Errorf("first = %q, want Island (option 0's label)", d.First)
		}
	})

	t.Run("full top reorder reversed", func(t *testing.T) {
		// Sensei's Divining Top look-at-three after the response draw put Top
		// back on top: gorge answers [Top, Plains#3, Plains#4] top-first.
		d := rules.OracleDecision{
			Step: 1, Seat: 0, Kind: "order", Options: 3,
			Picks:         []string{"Sensei's Divining Top", "Plains", "Plains"},
			PickIdx:       []int{0, 1, 2},
			PickRefs:      []string{"p0:Sensei's Divining Top", "p0:Plains#3", "p0:Plains#4"},
			OptionRefs:    []string{"p0:Sensei's Divining Top", "p0:Plains#3", "p0:Plains#4"},
			ArrangeLabels: []string{"Sensei's Divining Top", "Plains", "Plains"},
			ArrangeKind:   "bottom",
			PickKinds:     []string{"bottom", "bottom", "bottom"},
			Resume:        "arrange", Via: "answer", GorgeKind: "arrange",
			First: "Sensei's Divining Top", Min: 3, Max: 3,
		}
		ds := []rules.OracleDecision{d}
		before := libraryDecisionCopy(t, ds)
		got := forgeLibraryTopOrder(ds, nil)
		if want := []string{"Plains", "Plains", "Sensei's Divining Top"}; !reflect.DeepEqual(got[0].Picks, want) {
			t.Errorf("picks = %v, want %v", got[0].Picks, want)
		}
		if want := []string{"p0:Plains#4", "p0:Plains#3", "p0:Sensei's Divining Top"}; !reflect.DeepEqual(got[0].PickRefs, want) {
			t.Errorf("pick_refs = %v, want %v", got[0].PickRefs, want)
		}
		if want := []int{2, 1, 0}; !reflect.DeepEqual(got[0].PickIdx, want) {
			t.Errorf("pick_idx = %v, want %v", got[0].PickIdx, want)
		}
		if !reflect.DeepEqual(ds, before) {
			t.Errorf("input mutated: %+v", ds)
		}
		// A one-card order and a Dig remainder are not reversed.
		one := d
		one.Picks, one.PickIdx, one.PickRefs, one.PickKinds = d.Picks[:1], d.PickIdx[:1], d.PickRefs[:1], d.PickKinds[:1]
		one.Options, one.Min, one.Max = 1, 1, 1
		dig := d
		dig.ArrangeKind = "dig_arrange"
		for name, x := range map[string]rules.OracleDecision{"one": one, "dig_arrange": dig} {
			if xs := []rules.OracleDecision{x}; !reflect.DeepEqual(forgeLibraryTopOrder(xs, nil), xs) {
				t.Errorf("%s reversed", name)
			}
		}
	})

	t.Run("library_placement bottom unchanged", func(t *testing.T) {
		// A bottom arrange leaves the picks below the old top. The explicit
		// bottom ask is not represented on a library_placement arrange, so
		// the after-step checkpoint is the evidence: it must not match.
		ds := []rules.OracleDecision{libraryPlacement()}
		bottom := libraryTopSnapshot([]string{"Mountain", "Island", "Forest"})
		if got := forgeLibraryTopOrder(ds, bottom); !reflect.DeepEqual(got, ds) {
			t.Errorf("bottom library_placement changed: %+v", got)
		}
	})

	t.Run("top placement from the after-step library", func(t *testing.T) {
		// Stunted Growth's hand_move batch has no changezone_alternative; the
		// checkpoint after its step shows the three picks at the library top
		// in pick order, which is the top-placement evidence.
		ds := []rules.OracleDecision{libraryHandMove()}
		top := libraryTopSnapshot([]string{"Island", "Forest", "Mountain"})
		got := forgeLibraryTopOrder(ds, top)
		if want := []string{"Forest", "Island"}; !reflect.DeepEqual(got[0].Picks, want) {
			t.Errorf("top hand_move picks = %v, want %v", got[0].Picks, want)
		}

		// A bottom placement leaves the picks below the old top: no reversal.
		bottom := libraryTopSnapshot([]string{"Mountain", "Island", "Forest"})
		if got := forgeLibraryTopOrder(ds, bottom); !reflect.DeepEqual(got, ds) {
			t.Errorf("bottom hand_move changed: %+v", got)
		}
	})

	t.Run("bottom pair into an empty library unchanged", func(t *testing.T) {
		// The probe that broke the checkpoint-only reading: a bottom
		// placement into an empty library leaves the picks as the new top in
		// pick order, indistinguishable from a top placement. The explicit
		// "bottom" alternative is the evidence that must win.
		ds := []rules.OracleDecision{libraryHandMove(), libraryAlternative("bottom", 1, "bottom")}
		empty := libraryTopSnapshot([]string{"Island", "Forest"})
		got := forgeLibraryTopOrder(ds, empty)
		if !reflect.DeepEqual(got[0], ds[0]) {
			t.Errorf("bottom-into-empty hand_move was reversed: %+v", got[0])
		}
	})

	t.Run("bottom pair unchanged", func(t *testing.T) {
		ds := []rules.OracleDecision{libraryHandMove(), libraryAlternative("bottom", 1, "bottom")}
		got := forgeLibraryTopOrder(ds, nil)
		if len(got) != len(ds) {
			t.Fatalf("%d decisions, want %d: %+v", len(got), len(ds), got)
		}
		if !reflect.DeepEqual(got[0], ds[0]) {
			t.Errorf("bottom hand_move changed: %+v", got[0])
		}
		if !reflect.DeepEqual(got[1], ds[1]) {
			t.Errorf("bottom alternative changed: %+v", got[1])
		}
	})

	t.Run("no paired alternative unchanged", func(t *testing.T) {
		// A batch with no alternative at all (hand -> battlefield).
		alone := []rules.OracleDecision{libraryHandMove()}
		if got := forgeLibraryTopOrder(alone, nil); !reflect.DeepEqual(got, alone) {
			t.Errorf("unpaired hand_move changed: %+v", got)
		}
		// The alternative is in a later step: a different move, so no pair.
		later := libraryAlternative("top", 0, "primary")
		later.Step = 2
		ds := []rules.OracleDecision{libraryHandMove(), later}
		if got := forgeLibraryTopOrder(ds, nil); !reflect.DeepEqual(got, ds) {
			t.Errorf("hand_move paired across a step boundary: %+v", got)
		}
	})

	t.Run("non-hand_move unchanged", func(t *testing.T) {
		top := libraryAlternative("top", 0, "primary")
		// A two-pick choose with another resume is not a hand -> library batch.
		other := libraryHandMove()
		other.Resume = "choose"
		if ds := []rules.OracleDecision{other, top}; !reflect.DeepEqual(forgeLibraryTopOrder(ds, nil), ds) {
			t.Errorf("a non-hand_move choose_n was reversed")
		}
		// A single-pick hand_move is not a batch.
		single := libraryHandMove()
		single.Picks = single.Picks[:1]
		single.PickIdx = single.PickIdx[:1]
		single.PickRefs = single.PickRefs[:1]
		single.PickRefsInexact = single.PickRefsInexact[:1]
		single.ObjectPicks = single.ObjectPicks[:1]
		single.PickKinds = single.PickKinds[:1]
		if ds := []rules.OracleDecision{single, top}; !reflect.DeepEqual(forgeLibraryTopOrder(ds, nil), ds) {
			t.Errorf("a single-pick hand_move was reversed")
		}
		// Pick kinds that are not all hand_move carry no evidence of a move.
		mixed := libraryHandMove()
		mixed.PickKinds = []string{"hand_move", "primary"}
		if ds := []rules.OracleDecision{mixed, top}; !reflect.DeepEqual(forgeLibraryTopOrder(ds, nil), ds) {
			t.Errorf("a choose_n with non-hand_move pick kinds was reversed")
		}
	})

	t.Run("each batch takes the next alternative", func(t *testing.T) {
		// Two hand -> library moves in one step (a second Dream Cache) pair
		// with their own alternatives in runner-log order: the first batch
		// must not consume the second batch's alternative.
		first, second := libraryHandMove(), libraryHandMove()
		second.Picks = []string{"Mountain", "Island"}
		second.PickRefs = []string{"p0:Mountain", "p0:Island#4"}
		second.PickIdx = []int{2, 0}
		second.ObjectPicks = []string{"p0:Mountain", "p0:Island#4"}
		ds := []rules.OracleDecision{
			first, libraryAlternative("top", 0, "primary"),
			second, libraryAlternative("bottom", 1, "bottom"),
		}
		got := forgeLibraryTopOrder(ds, nil)
		if want := []string{"p0:Forest", "p0:Island#4"}; !reflect.DeepEqual(got[0].PickRefs, want) {
			t.Errorf("first (top) batch pick_refs = %v, want %v", got[0].PickRefs, want)
		}
		if !reflect.DeepEqual(got[2], ds[2]) {
			t.Errorf("second (bottom) batch changed: %+v", got[2])
		}
	})

	t.Run("input not mutated", func(t *testing.T) {
		ds := []rules.OracleDecision{libraryHandMove(), libraryAlternative("top", 0, "primary")}
		before := libraryDecisionCopy(t, ds)
		_ = forgeLibraryTopOrder(ds, nil)
		if !reflect.DeepEqual(ds, before) {
			t.Errorf("input mutated:\n got %+v\nwant %+v", ds, before)
		}
	})

	t.Run("gate: after-step top reaches the request", func(t *testing.T) {
		// Stunted Growth has no changezone_alternative: the request builder
		// must pass g.Snapshots through to the helper, or the batch is not
		// reversed and Forge's own top placement reverses it.
		it := oraclegen.Item{
			ID: "stunted-growth/top", Card: "Stunted Growth", Template: "cast-resolve",
			Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{
				{Op: "cast", Seat: 0, Card: "p0:Stunted Growth"},
				{Op: "resolve", Seat: 1},
			}},
		}
		ds := []rules.OracleDecision{libraryHandMove()}
		g := rules.OracleResult{Snapshots: libraryTopSnapshot([]string{"Island", "Forest", "Mountain"})}
		forge := forgeBuildRequest(nil, it, g, ds, true).GorgeDecisions
		if want := []string{"p0:Forest", "p0:Island#4"}; !reflect.DeepEqual(forge[0].PickRefs, want) {
			t.Errorf("forge path did not reverse the after-step top hand_move: %+v", forge)
		}
	})

	t.Run("gate: after-step top reaches the request for a library_placement", func(t *testing.T) {
		// The snapshot must reach the export for the arrange shape too, not
		// only for a hand_move batch: the request builder is the only place
		// that passes g.Snapshots through.
		it := oraclegen.Item{
			ID: "harmonic-convergence/top", Card: "Harmonic Convergence", Template: "cast-resolve",
			Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{
				{Op: "cast", Seat: 0, Card: "p0:Harmonic Convergence"},
				{Op: "resolve", Seat: 0},
			}},
		}
		ds := []rules.OracleDecision{libraryPlacement()}
		top := rules.OracleResult{Snapshots: libraryTopSnapshot([]string{"Island", "Forest", "Mountain"})}
		forge := forgeBuildRequest(nil, it, top, ds, true).GorgeDecisions
		if want := []string{"p0:Mountain", "p0:Forest", "p0:Island"}; !reflect.DeepEqual(forge[0].PickRefs, want) {
			t.Errorf("forge path did not reverse the after-step top library_placement: %+v", forge)
		}
		bottom := rules.OracleResult{Snapshots: libraryTopSnapshot([]string{"Mountain", "Island", "Forest"})}
		forge = forgeBuildRequest(nil, it, bottom, ds, true).GorgeDecisions
		if want := []string{"p0:Island", "p0:Forest", "p0:Mountain"}; !reflect.DeepEqual(forge[0].PickRefs, want) {
			t.Errorf("forge path reversed a bottom library_placement: %+v", forge)
		}
	})

	t.Run("gate", func(t *testing.T) {
		it := oraclegen.Item{
			ID: "dreamcache/top", Card: "Dream Cache", Template: "cast-resolve",
			Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{
				{Op: "cast", Seat: 0, Card: "p0:Dream Cache"},
				{Op: "resolve", Seat: 0},
			}},
		}
		ds := []rules.OracleDecision{libraryHandMove(), libraryAlternative("top", 0, "primary")}
		before := libraryDecisionCopy(t, ds)

		forge := forgeBuildRequest(nil, it, rules.OracleResult{}, ds, true).GorgeDecisions
		if len(forge) != 2 || !reflect.DeepEqual(forge[0].PickRefs, []string{"p0:Forest", "p0:Island#4"}) {
			t.Errorf("forge path did not reverse the top hand_move: %+v", forge)
		}
		follower := forgeBuildRequest(nil, it, rules.OracleResult{}, ds, false).GorgeDecisions
		if !reflect.DeepEqual(follower, before) {
			t.Errorf("follower path changed the decisions:\n got %+v\nwant %+v", follower, before)
		}
	})
}
