package oraclegen

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// A paid UnlessCost$ whose components are OBJECT picks is XMage's boolean
// chooseUse followed by the cost's own target asks. The find windows under
// oracle/xengine/xprep/testdata/xproof are the xproof scenarios: Primeval
// Force's three Forests and Rathi Dragon's two Mountains. The answers must be
// yes + target refs; a mode pick plus an alias choice is the reported
// chooseUse assertion.
func TestUnlessPaidObjectCostUsesYesThenTargets(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		file string
		step int
		want []XAnswer
	}{
		{
			"primeval-force", 9,
			[]XAnswer{
				{Seat: 0, Kind: "choice", Value: "yes"},
				{Seat: 0, Kind: "target", Value: "p0:Forest"},
				{Seat: 0, Kind: "target", Value: "p0:Forest#2"},
				{Seat: 0, Kind: "target", Value: "p0:Forest#3"},
			},
		},
		{
			"rathi-dragon", 8,
			[]XAnswer{
				{Seat: 0, Kind: "choice", Value: "yes"},
				{Seat: 0, Kind: "target", Value: "p0:Mountain"},
				{Seat: 0, Kind: "target", Value: "p0:Mountain#2"},
			},
		},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile("../../../oracle/xengine/xprep/testdata/xproof/" + tc.file + ".json")
			if err != nil {
				t.Fatal(err)
			}
			res, err := rules.RunOracleScenarioJSON(reg, raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Fails) > 0 {
				t.Fatalf("gorge fails: %v", res.Fails)
			}
			var sc Scenario
			if err := json.Unmarshal(raw, &sc); err != nil {
				t.Fatal(err)
			}
			got := XAnswersForScenario(reg, res, sc, nil, nil)
			if tc.step >= len(got) || !reflect.DeepEqual(got[tc.step], tc.want) {
				t.Fatalf("xmage_answers[%d] = %+v, want %+v", tc.step, got[tc.step], tc.want)
			}
		})
	}
}

// The unless_cost route is keyed on the engine's resume kind, so every cost
// component kind reaches the target queue: a sacrifice, a discard, an exile
// and a tap are all XMage cost targets. A paid mana/life cost has no component
// pick and keeps the ordinary mode answer; a decline stays "no".
func TestUnlessCostPickRouting(t *testing.T) {
	paid := rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", GorgeKind: "modes",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Pay the cost"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Primeval Force"}, PickKinds: []string{"mode"}, Resume: "unless_pay"}
	cost := func(kind string, refs ...string) rules.OracleDecision {
		d := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Options: len(refs) + 1, Min: len(refs), Max: len(refs), Resume: "unless_cost",
			PickRefs: refs, OptionRefs: append(append([]string(nil), refs...), "p0:Forest#9")}
		for _, ref := range refs {
			d.Picks = append(d.Picks, oraclediffRefName(ref))
			d.PickIdx = append(d.PickIdx, 0)
			d.PickKinds = append(d.PickKinds, kind)
		}
		return d
	}
	sc := Scenario{Steps: []Step{{Op: "pass"}}}
	for _, kind := range []string{"sacrifice", "discard", "exilecost", "tapcost", "returncost", "revealcost"} {
		t.Run(kind, func(t *testing.T) {
			c := cost(kind, "p0:Forest")
			got := XAnswersForScenario(nil, rules.OracleResult{Decisions: []rules.OracleDecision{paid, c}}, sc, nil, nil)
			want := []XAnswer{
				{Seat: 0, Kind: "choice", Value: "yes"},
				{Seat: 0, Kind: "target", Value: "p0:Forest"},
			}
			if !reflect.DeepEqual(got, [][]XAnswer{want}) {
				t.Fatalf("answers = %+v, want %+v", got, want)
			}
		})
	}
	// A mana/life payment records no unless_cost component: keep the mode pick.
	got := XAnswersForScenario(nil, rules.OracleResult{Decisions: []rules.OracleDecision{paid}}, sc, nil, nil)
	if want := [][]XAnswer{{{Seat: 0, Kind: "mode", Value: "1"}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mana/life payment = %+v, want %+v", got, want)
	}
	// A decline is still the boolean no; no component decision follows it.
	decline := paid
	decline.Picks = []string{"Don't pay"}
	decline.PickIdx = []int{1}
	got = XAnswersForScenario(nil, rules.OracleResult{Decisions: []rules.OracleDecision{decline}}, sc, nil, nil)
	if want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "no"}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("decline = %+v, want %+v", got, want)
	}
}
