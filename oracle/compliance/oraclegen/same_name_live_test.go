package oraclegen

import (
	"reflect"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// Fleeting Reflection (OTJ): p0's Cactarantula is a copy of p1's Magebane
// Lizard until end of turn, so Ornery Tumblewagg's "+1/+1 counter on target
// creature" ask offers two objects XMage currently names "Magebane Lizard".
// The setup refs carry two names, so the census alone saw an unambiguous pick
// and emitted the bare name, which XMage bound to the copy.
func copyEffectDecision() (rules.OracleDecision, []rules.OracleSnapshot) {
	d := rules.OracleDecision{
		Step: 1, Seat: 0, Kind: "target", GorgeKind: "target", Options: 2, Min: 1, Max: 1,
		Picks: []string{"Magebane Lizard (p1)"}, PickRefs: []string{"p1:Magebane Lizard"},
		PickIdx: []int{1}, PickKinds: []string{"card"}, PickRefsInexact: []bool{false},
		OptionRefs: []string{"p0:Cactarantula", "p1:Magebane Lizard"},
	}
	copied := rules.OracleSnapshot{Permanents: []rules.OracleSnapPerm{
		{Ref: "p0:Cactarantula", Name: "Magebane Lizard"},
		{Ref: "p1:Magebane Lizard", Name: "Magebane Lizard"},
	}}
	return d, []rules.OracleSnapshot{copied, copied, copied}
}

func TestLiveNamedSameNamesAliasesTheRecordedPermanent(t *testing.T) {
	d, snaps := copyEffectDecision()
	if ClassifySameName(d, 0).Ambiguous {
		t.Fatal("fixture must be the setup-name blind spot: the census sees one distinct name")
	}
	got := XAnswers(liveNamedSameNames([]rules.OracleDecision{d}, snaps), 2, nil)
	var answers []XAnswer
	for _, step := range got {
		answers = append(answers, step...)
	}
	if len(answers) != 1 || answers[0].Value != "p1:Magebane Lizard" {
		t.Fatalf("answers = %#v, want the recorded permanent's exact ref", got)
	}
	if want := []string{"p0:Cactarantula", "p1:Magebane Lizard"}; !slices.Equal(d.OptionRefs, want) {
		t.Fatalf("the recorded decision was mutated: %v", d.OptionRefs)
	}
}

// A copy that ends inside the decision's step window (an "until end of turn"
// effect reverting at the pass the ask was posed in) still named the object
// when XMage asked, so either bounding checkpoint counts.
func TestLiveNamedSameNamesEitherCheckpointOfTheWindow(t *testing.T) {
	d, snaps := copyEffectDecision()
	reverted := rules.OracleSnapshot{Permanents: []rules.OracleSnapPerm{
		{Ref: "p0:Cactarantula", Name: "Cactarantula"},
		{Ref: "p1:Magebane Lizard", Name: "Magebane Lizard"},
	}}
	for _, tc := range []struct {
		name  string
		snaps []rules.OracleSnapshot
	}{
		{"before", []rules.OracleSnapshot{snaps[0], snaps[0], reverted}},
		{"after", []rules.OracleSnapshot{reverted, reverted, snaps[0]}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := liveNamedSameNames([]rules.OracleDecision{d}, tc.snaps)
			if !ClassifySameName(ds[0], 0).Ambiguous {
				t.Fatalf("a copy named in the ask's window must be ambiguous: %v", ds[0].OptionRefs)
			}
		})
	}
	ds := liveNamedSameNames([]rules.OracleDecision{d}, []rules.OracleSnapshot{reverted, reverted, reverted})
	if ClassifySameName(ds[0], 0).Ambiguous || !slices.Equal(ds[0].OptionRefs, d.OptionRefs) {
		t.Fatalf("no copy in the window must leave the decision alone: %v", ds[0].OptionRefs)
	}
}

// A same-seat real object already spelled "p0:Magebane Lizard" must not absorb
// the renamed copy: the census counts distinct objects by ref string.
func TestLiveNamedSameNamesKeepsOptionsDistinct(t *testing.T) {
	d, snaps := copyEffectDecision()
	d.PickRefs = []string{"p0:Magebane Lizard"}
	d.Picks = []string{"Magebane Lizard (p0)"}
	d.OptionRefs = []string{"p0:Cactarantula", "p0:Magebane Lizard"}
	snaps[1].Permanents[1].Ref = "p0:Magebane Lizard"
	ds := liveNamedSameNames([]rules.OracleDecision{d}, snaps)
	p := ClassifySameName(ds[0], 0)
	if !p.Ambiguous || p.Distinct != 2 || p.Alias != "@p0:Magebane Lizard" {
		t.Fatalf("classification = %+v, refs %v", p, ds[0].OptionRefs)
	}
}

// Both objects renamed to a third name by copy effects, neither setup ref
// carrying it: the pick's own current name must reach the census too, and the
// answer is the exact ref whichever option was recorded.
func TestLiveNamedSameNamesBothOptionsRenamed(t *testing.T) {
	d, _ := copyEffectDecision()
	d.OptionRefs = []string{"p0:Cactarantula", "p1:Grizzly Bears"}
	snap := rules.OracleSnapshot{Permanents: []rules.OracleSnapPerm{
		{Ref: "p0:Cactarantula", Name: "Magebane Lizard"},
		{Ref: "p1:Grizzly Bears", Name: "Magebane Lizard"},
	}}
	snaps := []rules.OracleSnapshot{snap, snap, snap}
	for _, pick := range d.OptionRefs {
		d.PickRefs = []string{pick}
		d.Picks = []string{"Magebane Lizard (" + pick[:2] + ")"}
		var answers []XAnswer
		for _, step := range XAnswers(liveNamedSameNames([]rules.OracleDecision{d}, snaps), 2, nil) {
			answers = append(answers, step...)
		}
		if len(answers) != 1 || answers[0].Value != pick {
			t.Fatalf("pick %s: answers = %#v, want its exact ref", pick, answers)
		}
	}
}

// The production wiring: xanswersForScenario restates the options from the
// result's own snapshots before any answer is spelled, so the recorded ask
// answers its exact ref rather than the bare name XMage binds to the copy.
func TestXAnswersForScenarioSeesCopyEffectCollision(t *testing.T) {
	d, snaps := copyEffectDecision()
	res := rules.OracleResult{Snapshots: snaps, Decisions: []rules.OracleDecision{d}}
	sc := Scenario{Steps: []Step{{Op: "pass"}, {Op: "pass"}}}
	got := xanswersForScenario(cards.NewRegistry(), res, sc, nil, nil)
	want := [][]XAnswer{nil, {{Seat: 0, Kind: "target", Value: "p1:Magebane Lizard"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routed = %#v, want %#v", got, want)
	}
}
