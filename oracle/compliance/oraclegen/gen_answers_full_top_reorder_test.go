package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestXAnswersAvenFateshaperFullTopReorder(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	zero := 0
	it := Item{
		ID:       "xe:test:aven-fateshaper-top-reorder",
		Template: "xengine-window",
		Scenario: Scenario{
			Name: "Aven Fateshaper activate at a cut pass", Turn: 1,
			Why: "the full top reorder is XMage's N-1 top prompt, not the all-to-bottom answer order",
			Setup: map[string]Seat{
				"p0": {
					Battlefield: []string{"Aven Fateshaper", "Island", "Island", "Island", "Island", "Island"},
					LibraryTop:  []string{"Forest", "Mountain", "Plains", "Swamp", "Grizzly Bears", "Shock", "Wastes", "Island"},
				},
				"p1": {LibraryTop: []string{"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp"}},
			},
			Steps: []Step{
				{Op: "activate", Seat: 0, Card: "p0:Island", AbilityIndex: &zero},
				{Op: "activate", Seat: 0, Card: "p0:Island#2", AbilityIndex: &zero},
				{Op: "activate", Seat: 0, Card: "p0:Island#3", AbilityIndex: &zero},
				{Op: "activate", Seat: 0, Card: "p0:Island#4", AbilityIndex: &zero},
				{Op: "activate", Seat: 0, Card: "p0:Island#5", AbilityIndex: &zero},
				{Op: "activate", Seat: 0, Card: "p0:Aven Fateshaper", AbilityIndex: &zero},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1},
			},
		},
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("gorge rejects the window: %v", res.Fails)
	}
	var arrange *rules.OracleDecision
	for i := range res.Decisions {
		if res.Decisions[i].GorgeKind == "arrange" {
			arrange = &res.Decisions[i]
		}
	}
	if arrange == nil {
		t.Fatal("no arrange decision was recorded")
	}
	if arrange.Resume != "arrange" || arrange.ArrangeKind != "bottom" ||
		arrange.Min != 4 || arrange.Max != 4 || arrange.Options != 4 || len(arrange.Picks) != 4 {
		t.Fatalf("precondition: not the full top reorder shape: %+v", *arrange)
	}
	got := XAnswersForScenario(reg, res, it.Scenario, nil, nil)
	if arrange.Step < 0 || arrange.Step >= len(got) {
		t.Fatalf("arrange step %d outside %d answer steps", arrange.Step, len(got))
	}
	want := []XAnswer{{0, "choice", "Swamp"}, {0, "choice", "Plains"}, {0, "choice", "Mountain"}}
	if !reflect.DeepEqual(got[arrange.Step], want) {
		t.Fatalf("top-reorder answers = %+v, want %+v", got[arrange.Step], want)
	}
}
