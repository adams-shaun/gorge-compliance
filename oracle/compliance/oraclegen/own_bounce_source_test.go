package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestKorSkyfisherBouncePickIsATargetAnswer pins SUP-s280-supp
// xe:RAND:4:11:w8 (blame|diverge|p0.hand|Kor Skyfisher): "When this enters,
// return a permanent you control to its owner's hand" is XMage's
// ReturnToHandChosenControlledPermanentEffect, a chooseTarget over a
// TargetControlledPermanent, so the pick belongs on the TARGET queue. The
// generic hidden-pick route scripted it on the choice queue: strict XMage
// failed "Missing TARGET def" (w6/w7) and XMage's AI returned the Skyfisher
// itself, so p0.hand held Kor Skyfisher where gorge's held the Plains (w8).
// CR 701.8a-style choice: the controller chooses which permanent returns
// (CR 608.2d), so both engines must receive gorge's pick.
func TestKorSkyfisherBouncePickIsATargetAnswer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	zero := 0
	it := Item{
		ID: "xe:test:kor-skyfisher-bounce", Template: "xengine-window",
		Scenario: Scenario{
			Name: "Kor Skyfisher returns a Plains", Turn: 1,
			Why: "the own-bounce pick is XMage's target queue",
			Setup: map[string]Seat{
				"p0": {Hand: []string{"Kor Skyfisher"}, Battlefield: []string{"Plains", "Plains"}},
				"p1": {},
			},
			Steps: []Step{
				{Op: "activate", Seat: 0, Card: "p0:Plains", AbilityIndex: &zero},
				{Op: "activate", Seat: 0, Card: "p0:Plains#2", AbilityIndex: &zero},
				{Op: "cast", Seat: 0, Card: "p0:Kor Skyfisher", Mana: "WW"},
				{Op: "pass", Seat: 0},
				{Op: "pass", Seat: 1},
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
	var pick *rules.OracleDecision
	for i := range res.Decisions {
		if res.Decisions[i].Resume == "hidden_pick" {
			pick = &res.Decisions[i]
		}
	}
	if pick == nil {
		t.Fatalf("no hidden_pick decision: %+v", res.Decisions)
	}
	xa := XAnswersForScenario(reg, res, it.Scenario, nil, nil)
	got := xa[pick.Step]
	want := []XAnswer{{Seat: 0, Kind: "target", Value: "p0:Plains"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("step %d answers = %+v, want %+v (the TARGET queue, targetPickValue spelling, as every target ask)", pick.Step, got, want)
	}
}

// TestOwnBounceSAShape: the route is the mandatory controller-chosen
// battlefield-to-hand bounce only.
func TestOwnBounceSAShape(t *testing.T) {
	base := func(over map[string]string) *cards.SA {
		p := map[string]string{"Origin": "Battlefield", "Destination": "Hand", "Hidden": "True",
			"Mandatory": "True", "ChangeType": "Permanent.YouCtrl"}
		for k, v := range over {
			p[k] = v
		}
		return &cards.SA{API: "ChangeZone", Params: p}
	}
	for name, tc := range map[string]struct {
		sa   *cards.SA
		want bool
	}{
		"Kor Skyfisher":       {base(nil), true},
		"Karoo land":          {base(map[string]string{"ChangeType": "Land.YouCtrl", "ChangeNum": "1"}), true},
		"another creature":    {base(map[string]string{"ChangeType": "Creature.Other+YouCtrl"}), true},
		"two lands":           {base(map[string]string{"ChangeNum": "2"}), false},
		"optional":            {base(map[string]string{"Mandatory": ""}), false},
		"other chooser":       {base(map[string]string{"Chooser": "TriggeredCardController"}), false},
		"pay or bounce":       {base(map[string]string{"UnlessCost": "PayLife<2>"}), false},
		"any controller":      {base(map[string]string{"ChangeType": "Creature"}), false},
		"graveyard to hand":   {base(map[string]string{"Origin": "Graveyard"}), false},
		"battlefield to lib":  {base(map[string]string{"Destination": "Library"}), false},
		"not a ChangeZone":    {&cards.SA{API: "Draw", Params: map[string]string{}}, false},
		"visible (targeting)": {base(map[string]string{"Hidden": ""}), false},
	} {
		if got := isOwnBounceSA(tc.sa); got != tc.want {
			t.Errorf("%s: isOwnBounceSA = %v, want %v", name, got, tc.want)
		}
	}
}
