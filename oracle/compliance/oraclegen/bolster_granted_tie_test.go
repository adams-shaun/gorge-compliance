package oraclegen

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestBolsterGrantedTiePickRidesTargetQueue pins the granted shape of the
// Bolster tie routing. Gleam of Authority grants "{W}, {T}: Bolster 1." to the
// enchanted creature through AddAbility$ ABBolster, an SVar on the AURA, so the
// recipient's printed face carries no Bolster$ and the printed-face scan sees
// nothing. Two Raging Goblins (1/1, haste) tie for least toughness, so gorge
// records a counter_pick for the granted activation; XMage asks that tie through
// BolsterEffect's chooseTarget, so it must script one "target" answer and no
// "choice". The step names the grant by its offered label alone (Raging Goblin
// has no printed ability for an index to alias).
func TestBolsterGrantedTiePickRidesTargetQueue(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc := Scenario{
		Name: "Gleam of Authority granted bolster tie",
		Setup: map[string]Seat{
			"p0": {
				Battlefield: []string{"Gleam of Authority", "Raging Goblin", "Raging Goblin", "Plains"},
				Attached:    []SetupAttach{{Card: "Gleam of Authority", To: "p0:Raging Goblin"}},
			},
			"p1": {Battlefield: []string{"Shield Sphere"}},
		},
		Steps: []Step{
			{Op: "activate", Seat: 0, Card: "p0:Plains"},
			{Op: "activate", Seat: 0, Card: "p0:Raging Goblin", Ability: "Raging Goblin: "},
			{Op: "resolve"},
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
		if d := &res.Decisions[i]; pickKind(*d, 0) == "counter_pick" {
			pick = d
			break
		}
	}
	if pick == nil {
		t.Fatalf("precondition: no bolster tie counter_pick: %+v", res.Decisions)
	}
	if !bolsterTiePick(reg, sc, res, *pick) {
		t.Fatalf("granted bolster tie pick not recognised: decision %+v", *pick)
	}
	out := XAnswersForScenario(reg, res, sc, nil, nil)
	if pick.Step < 0 || pick.Step >= len(out) {
		t.Fatalf("decision step %d outside answers (%d steps)", pick.Step, len(out))
	}
	if len(out[pick.Step]) != 1 || out[pick.Step][0].Kind != "target" {
		t.Fatalf("step %d answers = %+v, want exactly one target answer", pick.Step, out[pick.Step])
	}
	for _, a := range out[pick.Step] {
		if a.Kind == "choice" {
			t.Fatalf("granted bolster tie pick still scripts a choice answer: %+v", out[pick.Step])
		}
	}
}
