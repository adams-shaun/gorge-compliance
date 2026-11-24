package oraclegen

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestSupportTargetTiming pins the Support retiming. gorge asks Support$ N at
// resolution as a KChoose with ResumeKind "counter_pick", but XMage declares
// it as a target on the source: SupportEffect adds a TargetCreaturePermanent
// to an instant/sorcery's spell ability (SupportEffect.java:25) and
// SupportAbility adds a TargetPermanent to the enters-the-battlefield trigger
// (SupportAbility.java:37). TestPlayer reads those from the TARGET queue when
// the object is announced (the cast step, the trigger's announcement step), so
// a "choice" on the resolution step is never consumed ("Missing TARGET def",
// leftover "found [Wall of Stone]" on xs_XP_unity-of-purpose_cast_s2 and
// xs_XP_nissa-s-judgment_cast_s1/s2).
func TestSupportTargetTiming(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	// Unity of Purpose (the xproof find's setup): the support pick resolves at
	// step 6 but XMage asks the spell's target during the cast at step 4.
	t.Run("spell cast", func(t *testing.T) {
		sc := Scenario{
			Name: "Unity of Purpose support",
			Setup: map[string]Seat{
				"p0": {
					Battlefield: []string{"Island", "Island", "Island", "Island", "Wall of Stone"},
					Hand:        []string{"Unity of Purpose"},
					LibraryTop: []string{"Mountain", "Island", "Mountain", "Island", "Island", "Mountain",
						"Mountain", "Mountain", "Mountain", "Island", "Island", "Mountain", "Island",
						"Island", "Island", "Island", "Island", "Mountain", "Mountain", "Island"},
				},
				"p1": {LibraryTop: []string{"Plains", "Plains", "Plains", "Plains", "Plains", "Plains",
					"Plains", "Plains", "Plains", "Plains"}},
			},
			Steps: []Step{
				{Op: "activate", Seat: 0, Card: "p0:Island", AbilityIndex: supportIntPtr(0)},
				{Op: "activate", Seat: 0, Card: "p0:Island#2", AbilityIndex: supportIntPtr(0)},
				{Op: "activate", Seat: 0, Card: "p0:Island#3", AbilityIndex: supportIntPtr(0)},
				{Op: "activate", Seat: 0, Card: "p0:Island#4", AbilityIndex: supportIntPtr(0)},
				{Op: "cast", Seat: 0, Card: "p0:Unity of Purpose"},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1},
				{Op: "pass", Seat: 0, Answers: []Answer{{Kind: "choose", Pick: []string{"p0:Wall of Stone"}}}},
				{Op: "pass", Seat: 1},
			},
		}
		cases := supportTiming(t, reg, sc, 4, []XAnswer{{Seat: 0, Kind: "target", Value: "Wall of Stone"}})
		if got := decisionStep(t, cases); got != 6 {
			t.Fatalf("resolution step = %d, want 6", got)
		}
	})

	// Aerie Auxiliary: the support trigger resolves at step 8, but XMage asks
	// its target when the trigger is announced at step 6 (the pass that
	// resolves the creature spell). One of two eligible creatures is picked,
	// so the "up to two" slot needs the explicit target_skip close.
	t.Run("permanent enters trigger", func(t *testing.T) {
		sc := Scenario{
			Name: "Aerie Auxiliary support",
			Setup: map[string]Seat{
				"p0": {
					Battlefield: []string{"Plains", "Plains", "Plains", "Plains", "Wall of Stone", "Grizzly Bears"},
					Hand:        []string{"Aerie Auxiliary"},
					LibraryTop: []string{"Plains", "Plains", "Plains", "Plains", "Plains", "Plains",
						"Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains",
						"Plains", "Plains", "Plains", "Plains", "Plains", "Plains", "Plains"},
				},
				"p1": {LibraryTop: []string{"Island", "Island", "Island", "Island", "Island", "Island",
					"Island", "Island", "Island", "Island"}},
			},
			Steps: []Step{
				{Op: "activate", Seat: 0, Card: "p0:Plains", AbilityIndex: supportIntPtr(0)},
				{Op: "activate", Seat: 0, Card: "p0:Plains#2", AbilityIndex: supportIntPtr(0)},
				{Op: "activate", Seat: 0, Card: "p0:Plains#3", AbilityIndex: supportIntPtr(0)},
				{Op: "activate", Seat: 0, Card: "p0:Plains#4", AbilityIndex: supportIntPtr(0)},
				{Op: "cast", Seat: 0, Card: "p0:Aerie Auxiliary"},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1, Answers: []Answer{{Kind: "choose", Pick: []string{"p0:Grizzly Bears"}}}},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1},
			},
		}
		cases := supportTiming(t, reg, sc, 6, []XAnswer{
			{Seat: 0, Kind: "target", Value: "Grizzly Bears"},
			{Seat: 0, Kind: "target", Value: "[target_skip]"},
		})
		if got := decisionStep(t, cases); got != 8 {
			t.Fatalf("resolution step = %d, want 8", got)
		}
	})

	// Nissa's Judgment also has a damage sub-ability whose target the cast
	// step carried inline. The support target is the spell ability's FIRST
	// target, so an inline preset would fill that slot and make XMage skip the
	// "up to" ask entirely. The retime must clear the preset and script the
	// sub-ability's target after the support answers, support first.
	t.Run("spell cast with a second target", func(t *testing.T) {
		sc := Scenario{
			Name: "Nissa's Judgment support and damage target",
			Setup: map[string]Seat{
				"p0": {
					Battlefield: []string{"Forest", "Forest", "Forest", "Forest", "Forest", "Wall of Stone"},
					Hand:        []string{"Nissa's Judgment"},
					LibraryTop: []string{"Mountain", "Mountain", "Mountain", "Forest", "Mountain", "Mountain",
						"Forest", "Mountain", "Forest", "Mountain", "Mountain", "Mountain", "Forest",
						"Forest", "Forest", "Forest", "Forest", "Forest", "Forest", "Forest"},
				},
				"p1": {
					Battlefield: []string{"Kobolds of Kher Keep"},
					LibraryTop: []string{"Plains", "Plains", "Plains", "Plains", "Plains", "Plains",
						"Plains", "Plains", "Plains", "Plains"},
				},
			},
			Steps: []Step{
				{Op: "activate", Seat: 0, Card: "p0:Forest", AbilityIndex: supportIntPtr(0)},
				{Op: "activate", Seat: 0, Card: "p0:Forest#2", AbilityIndex: supportIntPtr(0)},
				{Op: "activate", Seat: 0, Card: "p0:Forest#3", AbilityIndex: supportIntPtr(0)},
				{Op: "activate", Seat: 0, Card: "p0:Forest#4", AbilityIndex: supportIntPtr(0)},
				{Op: "activate", Seat: 0, Card: "p0:Forest#5", AbilityIndex: supportIntPtr(0)},
				{Op: "cast", Seat: 0, Card: "p0:Nissa's Judgment", Targets: []string{"p1:Kobolds of Kher Keep"}},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1},
				{Op: "pass", Seat: 0, Answers: []Answer{{Kind: "choose", Pick: []string{
					"p0:Wall of Stone", "p1:Kobolds of Kher Keep"}}}},
			},
		}
		raw, err := json.Marshal(sc)
		if err != nil {
			t.Fatal(err)
		}
		res, err := rules.RunOracleScenarioJSON(reg, raw)
		if err != nil {
			t.Fatalf("run scenario: %v", err)
		}
		if len(res.Fails) > 0 {
			t.Fatalf("gorge fails: %v", res.Fails)
		}
		out := XAnswersForScenario(reg, res, sc, nil, map[int]bool{5: true})
		want := []XAnswer{
			{Seat: 0, Kind: "target", Value: "Wall of Stone"},
			{Seat: 0, Kind: "target", Value: "Kobolds of Kher Keep"},
			{Seat: 0, Kind: "target", Value: "Kobolds of Kher Keep"},
		}
		if got := out[5]; !reflect.DeepEqual(got, want) {
			t.Fatalf("cast step answers = %+v, want %+v (all: %+v)", got, want, out)
		}
		if got := sc.Steps[5].Targets; len(got) != 0 {
			t.Fatalf("cast step still carries inline targets %v; XMage would pre-fill the support slot", got)
		}
		for _, a := range out[5] {
			if a.Kind == "choice" {
				t.Fatalf("cast step scripts a choice answer: %+v", a)
			}
		}
	})

	// An activated support ability declares its target when the ability is
	// activated, so the answers belong on the activate step even though gorge
	// asks the pick when the ability resolves.
	t.Run("activated ability", func(t *testing.T) {
		sc := Scenario{
			Name: "Joraga Auxiliary support",
			Setup: map[string]Seat{
				"p0": {
					Battlefield: []string{"Joraga Auxiliary", "Grizzly Bears", "Wall of Stone",
						"Forest", "Forest", "Forest", "Forest", "Plains", "Plains"},
					LibraryTop: []string{"Forest", "Forest", "Forest", "Forest", "Forest", "Forest",
						"Forest", "Forest", "Forest", "Forest"},
				},
				"p1": {LibraryTop: []string{"Island", "Island", "Island", "Island", "Island", "Island",
					"Island", "Island", "Island", "Island"}},
			},
			Steps: []Step{
				{Op: "activate", Seat: 0, Card: "p0:Joraga Auxiliary", AbilityIndex: supportIntPtr(0)},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1},
				{Op: "pass", Seat: 0, Answers: []Answer{{Kind: "choose", Pick: []string{"p0:Grizzly Bears"}}}},
				{Op: "pass", Seat: 1},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1},
			},
		}
		supportTiming(t, reg, sc, 0, []XAnswer{
			{Seat: 0, Kind: "target", Value: "Grizzly Bears"},
			{Seat: 0, Kind: "target", Value: "[target_skip]"},
		})
	})
}

// supportTiming runs sc, finds its Support counter_pick and asserts the
// generated answers moved to wantStep as want, with nothing left on the
// decision's resolution step and no choice answer at either step.
func supportTiming(t *testing.T, reg *cards.Registry, sc Scenario, wantStep int, want []XAnswer) []rules.OracleDecision {
	t.Helper()
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	res, err := rules.RunOracleScenarioJSON(reg, raw)
	if err != nil {
		t.Fatalf("run scenario: %v", err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("gorge fails: %v", res.Fails)
	}
	var pick *rules.OracleDecision
	for i := range res.Decisions {
		d := &res.Decisions[i]
		if d.Resume == "counter_pick" {
			pick = d
			break
		}
	}
	if pick == nil {
		t.Fatalf("precondition: no Support counter_pick decision: %+v", res.Decisions)
	}
	out := XAnswersForScenario(reg, res, sc, nil, nil)
	if got := out[wantStep]; !reflect.DeepEqual(got, want) {
		t.Fatalf("step %d answers = %+v, want %+v (all: %+v)", wantStep, got, want, out)
	}
	if got := out[pick.Step]; len(got) != 0 {
		t.Fatalf("resolution step %d still scripts answers: %+v", pick.Step, got)
	}
	for _, step := range []int{wantStep, pick.Step} {
		for _, a := range out[step] {
			if a.Kind == "choice" {
				t.Fatalf("step %d still scripts a choice answer for the support pick: %+v", step, a)
			}
		}
	}
	return res.Decisions
}

// decisionStep is the resolution step of the one Support decision supportTiming
// found, so each subtest can pin where gorge records it.
func decisionStep(t *testing.T, ds []rules.OracleDecision) int {
	t.Helper()
	for _, d := range ds {
		if d.Resume == "counter_pick" {
			return d.Step
		}
	}
	t.Fatal("no Support counter_pick decision")
	return -1
}

func supportIntPtr(n int) *int { return &n }
