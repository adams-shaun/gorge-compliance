package main

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// soulbondPairOf is the one Soulbond pairing decision of a request.
func soulbondPairOf(t *testing.T, ds []rules.OracleDecision) rules.OracleDecision {
	t.Helper()
	var got []rules.OracleDecision
	for _, d := range ds {
		if d.Kind == "choose_n" && d.Resume == "soulbond" {
			got = append(got, d)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want one soulbond pair decision, got %+v", ds)
	}
	return got[0]
}

func soulbondPerm(ref, name string, seat int) rules.OracleSnapPerm {
	return rules.OracleSnapPerm{Ref: ref, Name: name, Controller: seat, Keywords: []string{"Soulbond"}}
}

// TestForgeSoulbondPairSource: Forge's BondEffect asks for the SOURCE creature
// on Soulbond's "another unpaired creature you control enters" half
// (ValidCards$ Creature.Self+!Paired), while gorge's $ Pair answer names the
// entering partner it offered. The export must name the source, and must leave
// the "this creature enters" half (which asks the partner) alone.
func TestForgeSoulbondPairSource(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	// The recorded decision at SUP-s338-supp's step 7: the resolving Soulbond
	// trigger is Silverblade Paladin's #other half (the stack's ability source),
	// and gorge answered the entering Aven Interrupter. Forge's one option is
	// Silverblade Paladin, so the exported pick must name it.
	recorded := rules.OracleDecision{
		Step: 7, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Via: "answer",
		Options: 1, Picks: []string{"Aven Interrupter"},
		PickIdx: []int{0}, PickRefs: []string{"p0:Aven Interrupter"},
		PickRefsInexact: []bool{false},
		OptionRefs:      []string{"p0:Aven Interrupter"},
		ObjectPicks:     []string{"p0:Aven Interrupter"},
		PickKinds:       []string{"pair"}, Resume: "soulbond", First: "Aven Interrupter", Min: 0, Max: 1,
	}
	// Silverblade Paladin is on the battlefield from setup; Aven Interrupter
	// enters at step 4 and is the pair ask at step 7, with the Soulbond ability
	// on the stack.
	silverblade := soulbondPerm("p0:Silverblade Paladin", "Silverblade Paladin", 0)
	aven := rules.OracleSnapPerm{Ref: "p0:Aven Interrupter", Name: "Aven Interrupter", Controller: 0}
	recordedSnaps := make([]rules.OracleSnapshot, 8)
	for i := range recordedSnaps {
		recordedSnaps[i].Permanents = []rules.OracleSnapPerm{silverblade}
		if i >= 4 {
			recordedSnaps[i].Permanents = append(recordedSnaps[i].Permanents, aven)
		}
	}
	recordedSnaps[7].Stack = []rules.OracleSnapStack{{Kind: "ability", Source: "p0:Silverblade Paladin"}}
	it := oraclegen.Item{ID: "xe:test:soulbond", Scenario: oraclegen.Scenario{Steps: make([]oraclegen.Step, 7)}}
	rq := forgeBuildRequest(reg, it, rules.OracleResult{Snapshots: recordedSnaps}, []rules.OracleDecision{recorded}, true)
	d := soulbondPairOf(t, rq.GorgeDecisions)
	if want := []string{"Silverblade Paladin"}; !reflect.DeepEqual(d.Picks, want) {
		t.Errorf("recorded pair picks = %q, want %q (Forge asks for the source)", d.Picks, want)
	}
	if want := []string{"p0:Silverblade Paladin"}; !reflect.DeepEqual(d.PickRefs, want) {
		t.Errorf("recorded pair pick_refs = %q, want %q", d.PickRefs, want)
	}
	if want := []string{"p0:Silverblade Paladin"}; !reflect.DeepEqual(d.ObjectPicks, want) {
		t.Errorf("recorded pair object_picks = %q, want %q", d.ObjectPicks, want)
	}
	if want := []int{0}; !reflect.DeepEqual(d.PickIdx, want) {
		t.Errorf("recorded pair pick_idx = %v, want %v", d.PickIdx, want)
	}
	if want := []bool{false}; !reflect.DeepEqual(d.PickRefsInexact, want) {
		t.Errorf("recorded pair pick_refs_inexact = %v, want %v", d.PickRefsInexact, want)
	}

	// The #self half asks the partner, so its source is among the offered
	// options and the answer stays as gorge recorded it.
	self := recorded
	self.Step = 2
	self.Picks, self.PickRefs, self.ObjectPicks = []string{"Aven Interrupter"}, []string{"p0:Aven Interrupter"}, []string{"p0:Aven Interrupter"}
	self.OptionRefs = []string{"p0:Silverblade Paladin", "p0:Aven Interrupter"}
	selfSnaps := make([]rules.OracleSnapshot, 4)
	for i := range selfSnaps {
		selfSnaps[i].Permanents = []rules.OracleSnapPerm{aven}
	}
	selfSnaps[3].Permanents = append(selfSnaps[3].Permanents, silverblade)
	selfSnaps[3].Stack = []rules.OracleSnapStack{{Kind: "ability", Source: "p0:Silverblade Paladin"}}
	selfItem := oraclegen.Item{ID: "xe:test:self", Scenario: oraclegen.Scenario{Steps: make([]oraclegen.Step, 3)}}
	got := forgeBuildRequest(reg, selfItem, rules.OracleResult{Snapshots: selfSnaps}, []rules.OracleDecision{self}, true).GorgeDecisions
	if !reflect.DeepEqual(got, []rules.OracleDecision{self}) {
		t.Errorf("self-soulbond decision changed: %+v", got)
	}

	// A token Soulbond source keeps its printed name in the label; the ref
	// stays the exact scenario ref.
	tokenPair := recorded
	tokenPair.Step = 1
	tokenSnaps := []rules.OracleSnapshot{
		{Permanents: []rules.OracleSnapPerm{{Ref: "p0:token:Soldier#2", Name: "Soldier", Controller: 0, Token: true, Keywords: []string{"Soulbond"}}}},
		{
			Stack: []rules.OracleSnapStack{{Kind: "ability", Source: "p0:token:Soldier#2"}},
			Permanents: []rules.OracleSnapPerm{
				{Ref: "p0:token:Soldier#2", Name: "Soldier", Controller: 0, Token: true, Keywords: []string{"Soulbond"}},
				aven,
			},
		},
	}
	tokenGot := forgeBuildRequest(reg, oraclegen.Item{ID: "xe:test:token", Scenario: oraclegen.Scenario{Steps: make([]oraclegen.Step, 1)}},
		rules.OracleResult{Snapshots: tokenSnaps}, []rules.OracleDecision{tokenPair}, true).GorgeDecisions
	if want := []string{"Soldier"}; !reflect.DeepEqual(tokenGot[0].Picks, want) {
		t.Errorf("token source pick = %q, want %q", tokenGot[0].Picks, want)
	}
	if want := []string{"p0:token:Soldier#2"}; !reflect.DeepEqual(tokenGot[0].PickRefs, want) {
		t.Errorf("token source pick_refs = %q, want %q", tokenGot[0].PickRefs, want)
	}

	// End to end: a real gorge replay of the #other half (Silverblade Paladin
	// is already on the battlefield when Grizzly Bears enters) exports the
	// source; the #self half (Silverblade Paladin itself enters next to
	// Grizzly Bears) exports gorge's partner answer unchanged.
	t.Run("other replay", func(t *testing.T) {
		item := oraclegen.Item{ID: "xe:test:soulbond-other", Scenario: oraclegen.Scenario{
			Name: "soulbond other", Why: "another unpaired creature enters",
			Setup: map[string]oraclegen.Seat{"p0": {
				Battlefield: []string{"Silverblade Paladin", "Forest", "Forest"},
				Hand:        []string{"Grizzly Bears"},
				LibraryTop:  []string{"Island", "Island"},
			}},
			Steps: []oraclegen.Step{
				{Op: "cast", Seat: 0, Card: "p0:Grizzly Bears", Mana: "GG"},
				{Op: "resolve", Seat: 0, Answers: []oraclegen.Answer{{Kind: "choose", Pick: []string{"Grizzly Bears"}}}},
			},
		}}
		rq, g := forgeExportItem(reg, item, true)
		if len(g.Fails) > 0 {
			t.Fatalf("gorge rejects the window: %v", g.Fails)
		}
		d := soulbondPairOf(t, rq.GorgeDecisions)
		if want := []string{"p0:Silverblade Paladin"}; !reflect.DeepEqual(d.PickRefs, want) {
			t.Errorf("other replay pick_refs = %q, want %q", d.PickRefs, want)
		}
		if want := []string{"p0:Silverblade Paladin"}; !reflect.DeepEqual(d.ObjectPicks, want) {
			t.Errorf("other replay object_picks = %q, want %q", d.ObjectPicks, want)
		}
	})

	t.Run("self replay", func(t *testing.T) {
		item := oraclegen.Item{ID: "xe:test:soulbond-self", Scenario: oraclegen.Scenario{
			Name: "soulbond self", Why: "this creature enters",
			Setup: map[string]oraclegen.Seat{"p0": {
				Battlefield: []string{"Grizzly Bears", "Plains", "Plains", "Plains"},
				Hand:        []string{"Silverblade Paladin"},
				LibraryTop:  []string{"Island", "Island"},
			}},
			Steps: []oraclegen.Step{
				{Op: "cast", Seat: 0, Card: "p0:Silverblade Paladin", Mana: "WWW"},
				{Op: "resolve", Seat: 0, Answers: []oraclegen.Answer{{Kind: "choose", Pick: []string{"Grizzly Bears"}}}},
			},
		}}
		rq, g := forgeExportItem(reg, item, true)
		if len(g.Fails) > 0 {
			t.Fatalf("gorge rejects the window: %v", g.Fails)
		}
		d := soulbondPairOf(t, rq.GorgeDecisions)
		if want := []string{"p0:Grizzly Bears"}; !reflect.DeepEqual(d.PickRefs, want) {
			t.Errorf("self replay pick_refs = %q, want %q (Forge asks the partner)", d.PickRefs, want)
		}
		if want := []string{"p0:Grizzly Bears"}; !reflect.DeepEqual(d.ObjectPicks, want) {
			t.Errorf("self replay object_picks = %q, want %q", d.ObjectPicks, want)
		}
	})

	// A list with nothing to rewrite is returned as is, and a rewrite never
	// mutates its input.
	other := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", Resume: "explore",
		Picks: []string{"Forest"}, PickRefs: []string{"p0:Forest"}, PickKinds: []string{"search"}}
	if got := forgeSoulbondPairSource(rules.OracleResult{}, []rules.OracleDecision{other}); len(got) != 1 || !reflect.DeepEqual(got[0], other) {
		t.Errorf("non-soulbond decision changed: %+v", got)
	}
	if want := []string{"p0:Aven Interrupter"}; !reflect.DeepEqual(recorded.PickRefs, want) {
		t.Errorf("input decision mutated: %+v", recorded)
	}
}
