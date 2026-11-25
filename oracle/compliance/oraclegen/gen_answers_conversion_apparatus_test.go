package oraclegen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// The xproof find xs_XP_conversion-apparatus_f0a2_s1 (cycle 11251811): p0
// activates Conversion Apparatus's third ability ("{T}, Pay {E}{E}{E}: Add
// three mana in any combination of colors", Forge Produced$ Combo Any |
// Amount$ 3). The activation's cost stage poses BOTH asks as plain chooses
// with the cost prefix on every label ("Pay 3 energy: ..."), never a
// "mana_color" resume, so the allocation arms never fired and XMage's
// AddManaInAnyCombinationEffect multi-amount ("Distribute mana among colors")
// was left to its AI -- which distributed W/U/B where gorge allocated W/W/U
// (pool WWU vs WUB, strict_miss decision_shape).
const conversionApparatusScenario = `{
 "name": "xproof conversion-apparatus f0a2 seed 1", "turn": 1, "cr": [],
 "why": "xproof find xs_XP_conversion-apparatus_f0a2_s1, library_top compacted",
 "setup": {
  "p0": {"battlefield": ["Conversion Apparatus"], "library_top": ["Plains", "Plains", "Plains", "Plains", "Plains"], "life": 20, "player_counters": [{"kind": "ENERGY", "n": 3}]},
  "p1": {"library_top": ["Plains", "Plains", "Plains", "Plains", "Plains"], "life": 20}
 },
 "steps": [
  {"op": "activate", "seat": 0, "card": "p0:Conversion Apparatus", "ability_index": 2, "answers": [
   {"kind": "choose", "pick": ["Pay 3 energy: Add three mana in any combination of colors"]},
   {"kind": "choose", "pick": ["Pay 3 energy: Add W", "Pay 3 energy: Add W", "Pay 3 energy: Add U"]}
  ]}
 ]
}`

// TestConversionApparatusComboAnyCostStaged proves the cost-staged Combo Any
// allocation scripts the WUBRG multi-amount: five "amount" answers, 2 W / 1 U
// per gorge's staged picks and zeroes for the rest, exactly what XMage's
// AddManaInAnyCombinationEffect getMultiAmountWithIndividualConstraints
// consumes in its manaSymbols order.
func TestConversionApparatusComboAnyCostStaged(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := rules.RunOracleScenarioJSON(reg, []byte(conversionApparatusScenario))
	if err != nil {
		t.Fatalf("scenario did not run: %v", err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("gorge failed the scenario: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}

	// Precondition: the allocation is the staged plain choose this fix routes,
	// not a bare "mana_color" resume the old arms already covered.
	var alloc *rules.OracleDecision
	for i := range res.Decisions {
		d := &res.Decisions[i]
		if d.Min == d.Max && d.Max == 3 && len(d.Picks) == 3 && d.Resume == "" {
			if alloc != nil {
				t.Fatalf("two staged allocation decisions: %+v", res.Decisions)
			}
			alloc = d
		}
	}
	if alloc == nil {
		t.Fatalf("no staged 3-unit allocation posed; decisions: %+v", res.Decisions)
	}
	if alloc.Resume == "mana_color" {
		t.Fatalf("precondition: the allocation carries a mana_color resume the old arms own: %+v", alloc)
	}
	for k, label := range alloc.Picks {
		if !strings.HasPrefix(label, "Pay 3 energy: Add ") {
			t.Fatalf("pick %d label %q is not a cost-staged Add option", k, label)
		}
	}

	// The last snapshot's pool: gorge allocated W, W, U.
	last := res.Snapshots[len(res.Snapshots)-1]
	var pool string
	for _, p := range last.Players {
		if p.Seat == 0 {
			pool = p.Pool
		}
	}
	if pool != "WWU" {
		t.Fatalf("precondition: gorge pool %q, want WWU", pool)
	}

	var sc Scenario
	if err := json.Unmarshal([]byte(conversionApparatusScenario), &sc); err != nil {
		t.Fatal(err)
	}
	got := XAnswersForScenario(reg, res, sc, nil, nil)
	if alloc.Step >= len(got) {
		t.Fatalf("allocation step %d outside the answer stream (%d steps)", alloc.Step, len(got))
	}
	var amounts []XAnswer
	for _, a := range got[alloc.Step] {
		if a.Kind == "amount" {
			amounts = append(amounts, a)
		}
	}
	want := []XAnswer{
		{0, "amount", "2"}, {0, "amount", "1"}, {0, "amount", "0"}, {0, "amount", "0"}, {0, "amount", "0"},
	}
	if len(amounts) != len(want) {
		t.Fatalf("staged allocation amounts = %+v, want %+v", amounts, want)
	}
	for i := range want {
		if amounts[i] != want[i] {
			t.Errorf("amount %d = %+v, want %+v", i, amounts[i], want[i])
		}
	}
}

// TestStagedManaAllocationGate pins the gate's negatives: a bare-resume
// allocation (the mana_color arms own it), an unbounded ask, a single unit, a
// restricted set's option count and a non-colour pick all stay unscripted.
func TestStagedManaAllocationGate(t *testing.T) {
	yes := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Resume: "",
		Options: 15, Min: 3, Max: 3,
		Picks:     []string{"Pay 3 energy: Add W", "Pay 3 energy: Add W", "Pay 3 energy: Add U"},
		PickIdx:   []int{0, 5, 1},
		PickKinds: []string{"mana", "mana", "mana"},
	}
	if as, ok := stagedManaAllocationAnswers(yes); !ok || len(as) != 5 || as[0].Value != "2" || as[1].Value != "1" || as[2].Value != "0" {
		t.Fatalf("staged Combo Any = %+v, %v; want five WUBRG amounts 2,1,0,0,0", as, ok)
	}
	for name, d := range map[string]rules.OracleDecision{
		"bare mana_color resume": {
			Kind: "choose_n", Resume: "mana_color", Options: 15, Min: 3, Max: 3,
			Picks: []string{"Add W", "Add W", "Add U"}, PickIdx: []int{0, 5, 1},
			PickKinds: []string{"mana", "mana", "mana"},
		},
		"unbounded up-to ask": {
			Kind: "choose_n", Resume: "", Options: 15, Min: 0, Max: 3,
			Picks: []string{"Pay 3 energy: Add W", "Pay 3 energy: Add W"}, PickIdx: []int{0, 5},
			PickKinds: []string{"mana", "mana"},
		},
		"single unit": {
			Kind: "choose_n", Resume: "", Options: 5, Min: 1, Max: 1,
			Picks: []string{"Pay 1: Add W"}, PickIdx: []int{0}, PickKinds: []string{"mana"},
		},
		"restricted set option count": {
			Kind: "choose_n", Resume: "", Options: 6, Min: 3, Max: 3,
			Picks:     []string{"Pay 3 energy: Add U", "Pay 3 energy: Add U", "Pay 3 energy: Add R"},
			PickIdx:   []int{0, 2, 1},
			PickKinds: []string{"mana", "mana", "mana"},
		},
		"non-colour pick": {
			Kind: "choose_n", Resume: "", Options: 15, Min: 3, Max: 3,
			Picks:     []string{"Pay 3 energy: Add W", "Grizzly Bears", "Pay 3 energy: Add U"},
			PickIdx:   []int{0, 5, 1},
			PickKinds: []string{"mana", "card", "mana"},
		},
	} {
		if _, ok := stagedManaAllocationAnswers(d); ok {
			t.Errorf("%s: scripted, want gate refusal (%+v)", name, d)
		}
	}
}
