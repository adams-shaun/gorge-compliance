package lower

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// valiantItem is the xproof find xs:XP:valiant-endeavor:cast:s1: six Plains
// cast Valiant Endeavor, its two d6 roll 3 and 6 on step 8, and the last step
// answers the choose-one-result ask with "die 1: result 3".
func valiantItem(rolls []oraclegen.DieRoll, pick string) oraclegen.Item {
	steps := []oraclegen.Step{}
	zero := 0
	for _, c := range []string{"p0:Plains", "p0:Plains#2", "p0:Plains#3", "p0:Plains#4", "p0:Plains#5", "p0:Plains#6"} {
		steps = append(steps, oraclegen.Step{Op: "activate", Seat: 0, Card: c, AbilityIndex: &zero})
	}
	steps = append(steps,
		oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:Valiant Endeavor"},
		oraclegen.Step{Op: "pass", Seat: 0},
		oraclegen.Step{Op: "pass", Seat: 1},
		oraclegen.Step{Op: "pass", Seat: 0, Answers: []oraclegen.Answer{{Kind: "choose", Pick: []string{pick}}}},
		oraclegen.Step{Op: "pass", Seat: 1})
	lib := make([]string, 20)
	for i := range lib {
		lib[i] = "Plains"
	}
	life := int32(20)
	return oraclegen.Item{ID: "xs:XP:valiant-endeavor:cast:s1", Template: SpanTemplate, DieRolls: rolls, Scenario: oraclegen.Scenario{
		Name: "valiant", Turn: 1,
		Setup: map[string]oraclegen.Seat{
			"p0": {Battlefield: []string{"Plains", "Plains", "Plains", "Plains", "Plains", "Plains"}, Hand: []string{"Valiant Endeavor"}, LibraryTop: lib, Life: &life},
			"p1": {LibraryTop: lib, Life: &life}},
		Steps: steps}}
}

func valiantDecisions(t *testing.T, rolls []oraclegen.DieRoll, pick string) []rules.OracleDecision {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	g, err := rules.RunOracleScenarioJSON(reg, valiantItem(rolls, pick).Raw())
	if err != nil {
		t.Fatal(err)
	}
	return g.Decisions
}

// TestLowerChooseDieResult: gorge asks Valiant Endeavor's "choose one result"
// as a choose_n over the dice, picked by the label "die 1: result 3". Forge's
// chooseNumber wants the chosen value, so the lowering hands it the bare
// number, on the same step and seat, and touches no other decision.
func TestLowerChooseDieResult(t *testing.T) {
	rolls := []oraclegen.DieRoll{
		{Step: 8, Seat: 0, Sides: 6, Natural: 3, Result: 3},
		{Step: 8, Seat: 0, Sides: 6, Natural: 6, Result: 6},
	}
	raw := valiantDecisions(t, rolls, "die 1: result 3")
	at := -1
	for i, d := range raw {
		if _, _, ok := oraclegen.DieResultPick(d); ok {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no choose-one-result decision in gorge's list: %+v", raw)
	}
	got := ChooseDieResultAsNumber(raw)
	if len(got) != len(raw) {
		t.Fatalf("%d decisions, want %d", len(got), len(raw))
	}
	d := got[at]
	if d.Step != 8 || d.Seat != 0 || d.Kind != "choose_n" ||
		!reflect.DeepEqual(d.Picks, []string{"3"}) || !reflect.DeepEqual(d.PickKinds, []string{"number"}) {
		t.Errorf("lowered decision = %+v, want step 8 seat 0 choose_n picking number 3", d)
	}
	for i := range raw {
		if i != at && !reflect.DeepEqual(got[i], raw[i]) {
			t.Errorf("decision %d changed: %+v -> %+v", i, raw[i], got[i])
		}
	}
	if raw[at].Picks[0] != "die 1: result 3" {
		t.Errorf("the input was modified: %+v", raw[at])
	}

	// The second die lowers to its own value.
	second := ChooseDieResultAsNumber(valiantDecisions(t, rolls, "die 2: result 6"))
	if d := second[at]; !reflect.DeepEqual(d.Picks, []string{"6"}) {
		t.Errorf("die 2 lowered to %v, want 6", d.Picks)
	}
}

// TestLowerChooseDieResultXMage: XMage asks Valiant Endeavor's choice as one
// chooseUse (yes keeps the first result) only when the results differ, so a
// tie drops gorge's decision and a real choice scripts yes or no by the die.
func TestLowerChooseDieResultXMage(t *testing.T) {
	apart := []oraclegen.DieRoll{
		{Step: 8, Seat: 0, Sides: 6, Natural: 3, Result: 3},
		{Step: 8, Seat: 0, Sides: 6, Natural: 6, Result: 6},
	}
	tied := []oraclegen.DieRoll{
		{Step: 8, Seat: 0, Sides: 6, Natural: 4},
		{Step: 8, Seat: 0, Sides: 6, Natural: 4},
	}
	for _, c := range []struct {
		pick  string
		rolls []oraclegen.DieRoll
		want  []oraclegen.XAnswer
	}{
		{"die 1: result 3", apart, []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}}},
		{"die 2: result 6", apart, []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "no"}}},
		{"die 1: result 4", tied, nil},
	} {
		ds := DropTiedDieResultChoices(valiantDecisions(t, c.rolls, c.pick), c.rolls)
		var got []oraclegen.XAnswer
		for _, d := range ds {
			if _, _, ok := oraclegen.DieResultPick(d); ok {
				got = append(got, oraclegen.XAnswers([]rules.OracleDecision{d}, 12, nil)[d.Step]...)
			}
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: XMage answers %+v, want %+v", c.pick, got, c.want)
		}
	}
}
