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
// chooseUse followed by the cost's own target group, which XMage poses as a
// makeChoose over the CHOICE queue (SacrificeTargetCost.pay ->
// TargetImpl.choose -> Player.choose; makeChoose accepts aliases, chooseUse
// does not). The find windows under oracle/xengine/xprep/testdata/xproof are
// the xproof scenarios: Primeval Force's three Forests and Rathi Dragon's two
// Mountains. The answers must be choice yes, then ONE aliased choice; a mode
// pick plus the alias choice alone is the reported chooseUse assertion.
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
				{Seat: 0, Kind: "choice", Value: "@p0:Forest^@p0:Forest#2^@p0:Forest#3"},
			},
		},
		{
			"rathi-dragon", 8,
			[]XAnswer{
				{Seat: 0, Kind: "choice", Value: "yes"},
				{Seat: 0, Kind: "choice", Value: "@p0:Mountain^@p0:Mountain#2"},
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

// A paid mana/life cost has no unless_cost component decision and keeps the
// ordinary mode answer; a decline stays "no"; a paid sacrifice answers the
// boolean yes. The component picks themselves ride the ordinary choose_n
// route (pinned by the find-backed test above), so the synthetic table here
// covers only the boolean's routing.
func TestUnlessCostBooleanRouting(t *testing.T) {
	paid := rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", GorgeKind: "modes",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Pay the cost"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Primeval Force"}, PickKinds: []string{"mode"}, Resume: "unless_pay"}
	sc := Scenario{Steps: []Step{{Op: "pass"}}}
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
