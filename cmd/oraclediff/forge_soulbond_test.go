package main

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
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

// soulbondAsk is gorge's choose_n for a Soulbond pairing: the answer names the
// offered partner, and OptionRefs records that same offered object.
func soulbondAsk(step int, pickRef string) rules.OracleDecision {
	name := refCardName(pickRef)
	return rules.OracleDecision{
		Step: step, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Via: "answer",
		Options: 1, Picks: []string{name},
		PickIdx: []int{0}, PickRefs: []string{pickRef},
		PickRefsInexact: []bool{false},
		OptionRefs:      []string{pickRef},
		ObjectPicks:     []string{pickRef},
		PickKinds:       []string{"pair"}, Resume: "soulbond", First: name, Min: 0, Max: 1,
	}
}

// soulbondTriggerSlot is the OracleSnapStack.Trigger value gorge records for
// card's Soulbond#self / #other printed trigger: the index into the face's
// Triggers slice.
func soulbondTriggerSlot(t *testing.T, reg *cards.Registry, card string, other bool) string {
	t.Helper()
	c, ok := reg.Lookup(card)
	if !ok || c == nil || len(c.Faces) == 0 {
		t.Fatalf("corpus has no card %q", card)
	}
	want := "Soulbond#self"
	if other {
		want = "Soulbond#other"
	}
	for _, f := range c.Faces {
		for i, tr := range f.Triggers {
			if tr.Params["KeywordLine"] == want {
				return strconv.Itoa(i)
			}
		}
	}
	t.Fatalf("%s has no %s trigger", card, want)
	return ""
}

func soulbondItem(id string, steps int) oraclegen.Item {
	return oraclegen.Item{ID: id, Scenario: oraclegen.Scenario{Steps: make([]oraclegen.Step, steps)}}
}

// TestForgeSoulbondPairSource: Forge's BondEffect asks for the SOURCE creature
// on Soulbond's "another unpaired creature you control enters" half
// (ValidCards$ Creature.Self+!Paired), while gorge's $ Pair answer names the
// entering partner it offered. The export must name the source, and must leave
// the "this creature enters" half (which asks the partner) alone. Which half
// fired is read from the resolving trigger's printed slot, not from which
// creature happened to enter first.
func TestForgeSoulbondPairSource(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	selfSlot := soulbondTriggerSlot(t, reg, "Silverblade Paladin", false)
	otherSlot := soulbondTriggerSlot(t, reg, "Silverblade Paladin", true)
	silverblade := soulbondPerm("p0:Silverblade Paladin", "Silverblade Paladin", 0)
	aven := rules.OracleSnapPerm{Ref: "p0:Aven Interrupter", Name: "Aven Interrupter", Controller: 0}

	// The recorded decision at SUP-s338-supp's step 7: the resolving Soulbond
	// trigger is Silverblade Paladin's #other half (the stack's ability source
	// and printed slot), and gorge answered the entering Aven Interrupter.
	// Forge's one option is Silverblade Paladin, so the exported pick must name
	// it.
	t.Run("recorded other ask", func(t *testing.T) {
		recorded := soulbondAsk(7, "p0:Aven Interrupter")
		// Silverblade Paladin is on the battlefield from setup; Aven
		// Interrupter enters at step 4 and is the pair ask at step 7, with the
		// Soulbond ability on the stack.
		snaps := make([]rules.OracleSnapshot, 8)
		for i := range snaps {
			snaps[i].Permanents = []rules.OracleSnapPerm{silverblade}
			if i >= 4 {
				snaps[i].Permanents = append(snaps[i].Permanents, aven)
			}
		}
		snaps[7].Stack = []rules.OracleSnapStack{{
			Kind: "ability", Source: "p0:Silverblade Paladin", Trigger: otherSlot,
		}}
		rq := forgeBuildRequest(reg, soulbondItem("xe:test:soulbond", 8), rules.OracleResult{Snapshots: snaps}, []rules.OracleDecision{recorded}, true)
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
	})

	// A real #self ask can pick a creature that entered after the source: the
	// Soulbond creature enters, another creature flashes in, the optional
	// #other pairing is declined, and the #self ask then picks the flashed-in
	// creature. The trigger slot says #self, so the answer (the partner) stays
	// even though the partner entered later.
	t.Run("self with later partner", func(t *testing.T) {
		self := soulbondAsk(4, "p0:Aven Interrupter")
		snaps := make([]rules.OracleSnapshot, 5)
		for i := range snaps {
			if i >= 2 {
				snaps[i].Permanents = append(snaps[i].Permanents, silverblade)
			}
			if i >= 3 {
				snaps[i].Permanents = append(snaps[i].Permanents, aven)
			}
		}
		snaps[4].Stack = []rules.OracleSnapStack{{
			Kind: "ability", Source: "p0:Silverblade Paladin", Trigger: selfSlot,
		}}
		got := forgeSoulbondPairSource(reg, rules.OracleResult{Snapshots: snaps}, []rules.OracleDecision{self})
		if !reflect.DeepEqual(got, []rules.OracleDecision{self}) {
			t.Errorf("self-soulbond decision changed: %+v", got)
		}
	})

	// The entry-order fallback (a granted/token trigger has no printed slot to
	// read) must stop at the checkpoint entering the decision. A #self source
	// that enters at step 2, with its partner present since setup, is still
	// #self even if that partner blinks at step 3 and returns at step 4;
	// reading the whole run would call it #other and rewrite the partner to the
	// source (a wrong pair).
	t.Run("fallback self ignores later blink", func(t *testing.T) {
		self := soulbondAsk(2, "p0:Aven Interrupter")
		snaps := make([]rules.OracleSnapshot, 5)
		for i := range snaps {
			if i <= 2 || i == 4 {
				snaps[i].Permanents = append(snaps[i].Permanents, aven)
			}
			if i >= 2 {
				snaps[i].Permanents = append(snaps[i].Permanents, silverblade)
			}
		}
		snaps[2].Stack = []rules.OracleSnapStack{{Kind: "ability", Source: "p0:Silverblade Paladin"}}
		got := forgeSoulbondPairSource(reg, rules.OracleResult{Snapshots: snaps}, []rules.OracleDecision{self})
		if !reflect.DeepEqual(got, []rules.OracleDecision{self}) {
			t.Errorf("fallback #self decision changed: %+v", got)
		}
	})

	// The symmetric case: a #other source present since setup must still be
	// rewritten when it blinks after the decision and returns later; reading
	// the whole run would see it as the newer entrant and skip the fix.
	t.Run("fallback other ignores later return", func(t *testing.T) {
		other := soulbondAsk(2, "p0:Aven Interrupter")
		snaps := make([]rules.OracleSnapshot, 5)
		for i := range snaps {
			if i <= 2 || i == 4 {
				snaps[i].Permanents = append(snaps[i].Permanents, silverblade)
			}
			if i >= 2 {
				snaps[i].Permanents = append(snaps[i].Permanents, aven)
			}
		}
		snaps[2].Stack = []rules.OracleSnapStack{{Kind: "ability", Source: "p0:Silverblade Paladin"}}
		got := forgeSoulbondPairSource(reg, rules.OracleResult{Snapshots: snaps}, []rules.OracleDecision{other})
		if want := []string{"p0:Silverblade Paladin"}; len(got) != 1 || !reflect.DeepEqual(got[0].PickRefs, want) {
			t.Errorf("fallback #other pick_refs = %+v, want %q", got, want)
		}
	})

	// A token Soulbond source keeps its printed name in the label; the ref
	// stays the exact scenario ref.
	t.Run("token source", func(t *testing.T) {
		tokenPair := soulbondAsk(1, "p0:Aven Interrupter")
		token := rules.OracleSnapPerm{Ref: "p0:token:Soldier#2", Name: "Soldier", Controller: 0, Token: true, Keywords: []string{"Soulbond"}}
		tokenSnaps := []rules.OracleSnapshot{
			{Permanents: []rules.OracleSnapPerm{token}},
			{
				Stack:      []rules.OracleSnapStack{{Kind: "ability", Source: "p0:token:Soldier#2"}},
				Permanents: []rules.OracleSnapPerm{token, aven},
			},
		}
		got := forgeSoulbondPairSource(reg, rules.OracleResult{Snapshots: tokenSnaps}, []rules.OracleDecision{tokenPair})
		if want := []string{"Soldier"}; len(got) != 1 || !reflect.DeepEqual(got[0].Picks, want) {
			t.Errorf("token source pick = %+v, want %q", got, want)
		}
		if want := []string{"p0:token:Soldier#2"}; len(got) != 1 || !reflect.DeepEqual(got[0].PickRefs, want) {
			t.Errorf("token source pick_refs = %+v, want %q", got, want)
		}
	})

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
	if got := forgeSoulbondPairSource(reg, rules.OracleResult{}, []rules.OracleDecision{other}); len(got) != 1 || !reflect.DeepEqual(got[0], other) {
		t.Errorf("non-soulbond decision changed: %+v", got)
	}
	base := soulbondAsk(7, "p0:Aven Interrupter")
	_ = forgeSoulbondPairSource(reg, rules.OracleResult{}, []rules.OracleDecision{base})
	if want := []string{"p0:Aven Interrupter"}; !reflect.DeepEqual(base.PickRefs, want) {
		t.Errorf("input decision mutated: %+v", base)
	}
}
