package oraclegen

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestBolsterTiePickRidesTargetQueue pins the xproof Dromoka, the Eternal find
// (xs:XP:dromoka-the-eternal:f0t0:s1). p0 attacks with Dromoka (5/5) and
// Nadaar, Selfless Paladin (3/3, a Dragon), so "Whenever a Dragon you control
// attacks, bolster 2" triggers twice. The first bolster takes Nadaar (the lone
// least toughness); the second sees a 5/5 tie, which gorge records as a
// choose_n/counter_pick. XMage's BolsterEffect builds a TargetPermanent and
// calls controller.chooseTarget on that tie, so TestPlayer reads the TARGET
// queue: scripting gorge's pick as a "choice" is never consumed ("Missing
// TARGET def", leftover "found [Dromoka, the Eternal]"). The pick must script
// one target answer and no choice.
func TestBolsterTiePickRidesTargetQueue(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc := Scenario{
		Name: "Dromoka, the Eternal bolster tie",
		Setup: map[string]Seat{
			"p0": {
				Battlefield: []string{"Dromoka, the Eternal", "Nadaar, Selfless Paladin"},
				LibraryTop: []string{"Forest", "Plains", "Forest", "Forest", "Plains", "Forest",
					"Forest", "Plains", "Forest", "Plains", "Forest", "Forest", "Forest", "Plains",
					"Plains", "Plains", "Plains", "Plains", "Plains", "Forest"},
			},
			"p1": {
				Battlefield: []string{"Shield Sphere"},
				LibraryTop: []string{"Plains", "Plains", "Plains", "Plains", "Plains",
					"Plains", "Plains", "Plains", "Plains", "Plains"},
			},
		},
		Steps: []Step{
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
			{Op: "attack", Seat: 0, Defender: "p1",
				Attackers: []string{"p0:Dromoka, the Eternal", "p0:Nadaar, Selfless Paladin"},
				Answers: []Answer{{Kind: "trigger_order", Pick: []string{
					"p0:Dromoka, the Eternal", "p0:Dromoka, the Eternal", "p0:Nadaar, Selfless Paladin"}}}},
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0, Answers: []Answer{{Kind: "choose", Pick: []string{"Dungeon of the Mad Mage"}}}},
			{Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0, Answers: []Answer{{Kind: "choose", Pick: []string{"p0:Dromoka, the Eternal"}}}},
			{Op: "pass", Seat: 1},
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
	var pick *rules.OracleDecision
	for i := range res.Decisions {
		d := &res.Decisions[i]
		if pickKind(*d, 0) == "counter_pick" && len(d.PickRefs) == 1 && d.PickRefs[0] == "p0:Dromoka, the Eternal" {
			pick = d
			break
		}
	}
	if pick == nil {
		t.Fatalf("precondition: no bolster tie counter_pick for Dromoka: %+v", res.Decisions)
	}
	if want := []string{"Dromoka, the Eternal"}; !reflect.DeepEqual(pick.Picks, want) {
		t.Fatalf("precondition: tie pick = %v, want %v", pick.Picks, want)
	}
	out := XAnswersForScenario(reg, res, sc, nil, nil)
	if pick.Step < 0 || pick.Step >= len(out) {
		t.Fatalf("decision step %d outside answers (%d steps)", pick.Step, len(out))
	}
	want := []XAnswer{{Seat: 0, Kind: "target", Value: "Dromoka, the Eternal"}}
	if got := out[pick.Step]; !reflect.DeepEqual(got, want) {
		t.Fatalf("step %d answers = %+v, want %+v", pick.Step, got, want)
	}
	for _, a := range out[pick.Step] {
		if a.Kind == "choice" {
			t.Fatalf("bolster tie pick still scripts a choice answer: %+v", out[pick.Step])
		}
	}
}
