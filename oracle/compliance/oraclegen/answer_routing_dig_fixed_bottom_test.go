package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// TestDigFixedLookBottomRestScriptsPickOnTargetQueue pins Lone Revenant and
// Dragonlord Ojutai (xproof xpt-5c12195b): a fixed look of three or more, one
// card to hand and the rest to the library bottom. XMage's
// LookLibraryAndPickControllerEffect takes the card with chooseTarget over a
// TargetCard, the TARGET queue ("Missing TARGET def ... Ability: Dragonlord
// Ojutai"), and the bottom-order choice that follows stays on the choice queue.
func TestDigFixedLookBottomRestScriptsPickOnTargetQueue(t *testing.T) {
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name: "Dragonlord Ojutai",
		Triggers: []cards.Trigger{{
			Mode: "DamageDone",
			Effect: &cards.SA{Kind: "DB", API: "Dig", Params: map[string]string{
				"DigNum": "3", "ChangeNum": "1",
				"DestinationZone": "Hand", "DestinationZone2": "Library", "LibraryPosition2": "-1",
			}},
		}},
	}}})
	take := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "dig",
		Options: 3, Min: 1, Max: 1, Picks: []string{"Plains"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Plains"}, PickKinds: []string{"dig"},
		OptionRefs: []string{"p0:Plains", "p0:Island", "p0:Plains#2"}}
	arrange := rules.OracleDecision{Step: 1, Seat: 0, Kind: "order", GorgeKind: "arrange", Resume: "dig_arrange",
		ArrangeKind: "dig_bottom", Options: 2, Min: 2, Max: 2, Picks: []string{"Island", "Plains"},
		PickIdx: []int{0, 1}, PickKinds: []string{"dig_bottom", "dig_bottom"}}
	res := rules.OracleResult{
		Snapshots: []rules.OracleSnapshot{
			{},
			{Stack: []rules.OracleSnapStack{{Kind: "ability", Source: "p0:Dragonlord Ojutai", Controller: 0, Trigger: "0"}}},
			{},
		},
		Decisions: []rules.OracleDecision{take, arrange},
	}
	sc := Scenario{Steps: []Step{{Op: "pass"}, {Op: "pass"}}}
	want := [][]XAnswer{nil, {
		{Seat: 0, Kind: "target", Value: "Plains"},
		{Seat: 0, Kind: "choice", Value: "Island"},
	}}
	if got := xanswersForScenario(reg, res, sc, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("routed = %#v, want %#v", got, want)
	}
}
