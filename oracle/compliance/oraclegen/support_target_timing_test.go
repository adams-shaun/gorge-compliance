package oraclegen

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestSupportTargetTiming pins both Support timings at gorge 5326ad5128.
//
// A permanent's Support (an enters-the-battlefield trigger or an activated
// ability) is still a resolution KChoose with ResumeKind "counter_pick", but
// XMage declares it as a target on the source: SupportAbility adds a
// TargetPermanent to the trigger (SupportAbility.java:37) and the activated
// ability declares its target on activation. TestPlayer reads those from the
// TARGET queue when the object is announced, so a "choice" on the resolution
// step is never consumed; the answers are retimed to the announcement step
// (support_pick.go).
//
// An instant or sorcery's Support is no longer a pick: CR 701.41a with
// 601.2c announces its "up to N target creatures" while casting (gorge's
// supportTargetSA/supportAnnounced, engine/rules/support_targets.go and
// dirac/act_support.go). It is an ordinary cast target decision, which
// ChooseTargets folds into a preset Step.Targets (castSpell consumes it in
// XMage), or a generated item scripts as a cast-step TARGET answer; there is
// no counter_pick to retime and nothing may be scripted a second time.
func TestSupportTargetTiming(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	// Unity of Purpose: the support targets are announced by the cast at step
	// 4, exactly where XMage asks the spell ability's TargetCreaturePermanent.
	// The cast carries the target through Step.Targets, so XAnswers must not
	// script it again (the old retime's leftover "choice" was the bug).
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
				{Op: "cast", Seat: 0, Card: "p0:Unity of Purpose", Targets: []string{"p0:Wall of Stone"}},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1},
			},
		}
		res := runSupportScenario(t, reg, sc)
		assertCastSupportTarget(t, res, 4, []string{"p0:Wall of Stone"})
		sc, castSteps := ChooseTargets(sc, res.Decisions)
		if !castSteps[4] {
			t.Fatalf("cast step 4 not marked as carrying its own targets: %v", castSteps)
		}
		if got := sc.Steps[4].Targets; !reflect.DeepEqual(got, []string{"p0:Wall of Stone"}) {
			t.Fatalf("cast targets = %v, want the support target", got)
		}
		out := XAnswersForScenario(reg, res, sc, nil, castSteps)
		// castSpell already consumes the cast target, so nothing at all may be
		// scripted: any answer at any step (a second target at cast, a choice
		// or a stray target at a resolution step) is a regression.
		if len(out) != 0 {
			t.Fatalf("XAnswers scripts %+v; the cast target travels through castSpell and nothing is owed", out)
		}
		if got := supportCounter(res, "Wall of Stone"); got != 1 {
			t.Fatalf("Wall of Stone +1/+1 counters = %d, want 1 (the announced support target)", got)
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

	// Nissa's Judgment adds a damage sub-ability with its own optional target.
	// The support slot is the spell ability's FIRST target and the damage
	// slot its second. A cast step that names only the support target leaves
	// the damage slot genuinely omitted: gorge records it as a declined
	// cast_sub target, and XAnswers owes exactly that decline. A cast step that
	// also answers the damage target (the cast_sub answer) carries both slots;
	// the damage pick is scripted as a TARGET answer and the support target
	// still travels through castSpell.
	t.Run("spell cast with a second target omitted", func(t *testing.T) {
		sc := nissaJudgmentScenario(Step{Op: "cast", Seat: 0, Card: "p0:Nissa's Judgment",
			Targets: []string{"p0:Wall of Stone"}})
		res := runSupportScenario(t, reg, sc)
		if got := counterPickSteps(res); len(got) != 0 {
			t.Fatalf("a spell's Support still records counter_pick steps %v", got)
		}
		assertCastSupportTarget(t, res, 5, []string{"p0:Wall of Stone"})
		var subDeclined bool
		for _, d := range res.Decisions {
			if d.Step == 5 && d.Resume == "cast_sub" && d.Kind == "target" && len(d.Picks) == 0 {
				subDeclined = true
			}
		}
		if !subDeclined {
			t.Fatalf("precondition: no declined cast_sub target decision: %+v", res.Decisions)
		}
		sc, castSteps := ChooseTargets(sc, res.Decisions)
		if !castSteps[5] {
			t.Fatalf("cast step 5 not marked as carrying its own targets: %v", castSteps)
		}
		if got := sc.Steps[5].Targets; !reflect.DeepEqual(got, []string{"p0:Wall of Stone"}) {
			t.Fatalf("cast targets = %v, want just the support target", got)
		}
		out := XAnswersForScenario(reg, res, sc, nil, castSteps)
		want := []XAnswer{{Seat: 0, Kind: "target", Value: "[target_skip]"}}
		if got := out[5]; !reflect.DeepEqual(got, want) {
			t.Fatalf("cast step answers = %+v, want only the declined sub-ability target %+v (all: %+v)", got, want, out)
		}
		if got := supportCounter(res, "Wall of Stone"); got != 1 {
			t.Fatalf("Wall of Stone +1/+1 counters = %d, want 1 (the announced support target)", got)
		}
	})

	t.Run("spell cast with distinct support and damage targets", func(t *testing.T) {
		sc := nissaJudgmentScenario(Step{Op: "cast", Seat: 0, Card: "p0:Nissa's Judgment",
			Targets: []string{"p0:Wall of Stone"},
			Answers: []Answer{{Kind: "target", Pick: []string{"p1:Kobolds of Kher Keep"}}}})
		res := runSupportScenario(t, reg, sc)
		if got := counterPickSteps(res); len(got) != 0 {
			t.Fatalf("a spell's Support still records counter_pick steps %v", got)
		}
		assertCastSupportTarget(t, res, 5, []string{"p0:Wall of Stone"})
		var damage []string
		for _, d := range res.Decisions {
			if d.Step == 5 && d.Resume == "cast_sub" && d.Kind == "target" {
				damage = append(damage, d.PickRefs...)
			}
		}
		if !reflect.DeepEqual(damage, []string{"p1:Kobolds of Kher Keep"}) {
			t.Fatalf("damage sub-ability target picks = %v, want the Kobolds (decisions: %+v)", damage, res.Decisions)
		}
		sc, castSteps := ChooseTargets(sc, res.Decisions)
		if !castSteps[5] {
			t.Fatalf("cast step 5 not marked as carrying its own targets: %v", castSteps)
		}
		out := XAnswersForScenario(reg, res, sc, nil, castSteps)
		want := []XAnswer{{Seat: 0, Kind: "target", Value: "Kobolds of Kher Keep"}}
		if got := out[5]; !reflect.DeepEqual(got, want) {
			t.Fatalf("cast step answers = %+v, want only the damage target %+v (all: %+v)", got, want, out)
		}
		for step, as := range out {
			for _, a := range as {
				if a.Kind == "choice" {
					t.Fatalf("step %d scripts a support choice: %+v", step, a)
				}
			}
		}
		if got := supportCounter(res, "Wall of Stone"); got != 1 {
			t.Fatalf("Wall of Stone +1/+1 counters = %d, want 1 (the announced support target)", got)
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

// nissaJudgmentScenario builds the Nissa's Judgment scenario around castStep,
// the step-5 cast.
func nissaJudgmentScenario(castStep Step) Scenario {
	return Scenario{
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
			castStep,
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0},
		},
	}
}

// runSupportScenario runs sc and fails on a harness error or a gorge failure.
func runSupportScenario(t *testing.T, reg *cards.Registry, sc Scenario) rules.OracleResult {
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
	return res
}

// assertCastSupportTarget requires the cast step to have posed its Support
// target ask itself, picking exactly want, and to have recorded no resolution
// counter_pick for the spell.
func assertCastSupportTarget(t *testing.T, res rules.OracleResult, step int, want []string) {
	t.Helper()
	var got []string
	for _, d := range res.Decisions {
		if d.Step == step && d.Kind == "target" && d.Resume == "" && len(d.PickRefs) > 0 {
			got = append(got, d.PickRefs...)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cast step %d support target picks = %v, want %v (decisions: %+v)", step, got, want, res.Decisions)
	}
	if picks := counterPickSteps(res); len(picks) != 0 {
		t.Fatalf("spell Support still records resolution counter_pick steps %v", picks)
	}
}

// counterPickSteps lists the steps of every Support counter_pick decision.
func counterPickSteps(res rules.OracleResult) []int {
	var out []int
	for _, d := range res.Decisions {
		if d.Resume == "counter_pick" {
			out = append(out, d.Step)
		}
	}
	return out
}

// supportCounter is the final +1/+1 counter count on a battlefield permanent.
func supportCounter(res rules.OracleResult, name string) int32 {
	if len(res.Snapshots) == 0 {
		return 0
	}
	last := res.Snapshots[len(res.Snapshots)-1]
	for _, p := range last.Permanents {
		if p.Name == name {
			return p.Counters["P1P1"]
		}
	}
	return 0
}

// supportTiming runs sc, finds its Support counter_pick and asserts the
// generated answers moved to wantStep as want, with nothing left on the
// decision's resolution step and no choice answer at either step.
func supportTiming(t *testing.T, reg *cards.Registry, sc Scenario, wantStep int, want []XAnswer) []rules.OracleDecision {
	t.Helper()
	res := runSupportScenario(t, reg, sc)
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
