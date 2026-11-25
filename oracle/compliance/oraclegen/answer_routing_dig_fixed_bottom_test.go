package oraclegen

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// digBottomFixture is a fixed look of three: one card taken, two arranged to
// the library bottom, from a trigger named card whose Dig carries params.
func digBottomFixture(card string, params map[string]string) (*cards.Registry, rules.OracleResult, Scenario) {
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name: card,
		Triggers: []cards.Trigger{{
			Mode:   "DamageDone",
			Effect: &cards.SA{Kind: "DB", API: "Dig", Params: params},
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
			{Stack: []rules.OracleSnapStack{{Kind: "ability", Source: "p0:" + card, Controller: 0, Trigger: "0"}}},
			{},
		},
		Decisions: []rules.OracleDecision{take, arrange},
	}
	return reg, res, Scenario{Steps: []Step{{Op: "pass"}, {Op: "pass"}}}
}

// TestDigFixedLookBottomRestScriptsPickOnTargetQueue pins the fixed look of
// three or more, one card to hand and the rest to the library bottom (xproof
// xpt-5c12195b). XMage's LookLibraryAndPickControllerEffect takes the card
// with chooseTarget over a TargetCard, the TARGET queue ("Missing TARGET def
// ... Ability: Dragonlord Ojutai"), and the bottom-order choice that follows
// stays on the choice queue. The compiled corpus omits DestinationZone2 and
// LibraryPosition2 for Lone Revenant and Dragonlord Ojutai (the engine
// defaults them at execution time), so the omitted-params shape and both
// explicit spellings must all route the same way.
func TestDigFixedLookBottomRestScriptsPickOnTargetQueue(t *testing.T) {
	want := [][]XAnswer{nil, {
		{Seat: 0, Kind: "target", Value: "Plains"},
		{Seat: 0, Kind: "choice", Value: "Island"},
	}}
	for name, params := range map[string]map[string]string{
		"omitted (the corpus shape)": {"DigNum": "3", "NoReveal": "True"},
		"explicit library bottom": {"DigNum": "3", "ChangeNum": "1", "DestinationZone": "Hand",
			"DestinationZone2": "Library", "LibraryPosition2": "-1"},
		"explicit library, position omitted": {"DigNum": "3", "ChangeNum": "1", "DestinationZone": "Hand",
			"DestinationZone2": "Library"},
	} {
		reg, res, sc := digBottomFixture("Dragonlord Ojutai", params)
		if got := xanswersForScenario(reg, res, sc, nil, nil); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: routed = %#v, want %#v", name, got, want)
		}
	}
}

// TestDigFixedLookTopRestStaysOnChoiceQueue: a rest put back on TOP of the
// library (LibraryPosition2$ 0; Ancestral Knowledge) is not the bottom-rest
// dialog. XMage's card picks with controller.choose (the choice queue), so the
// take must not move to the target queue.
func TestDigFixedLookTopRestStaysOnChoiceQueue(t *testing.T) {
	reg, res, sc := digBottomFixture("Ancestral Knowledge", map[string]string{
		"DigNum": "3", "ChangeNum": "1", "DestinationZone": "Hand",
		"DestinationZone2": "Library", "LibraryPosition2": "0"})
	got := xanswersForScenario(reg, res, sc, nil, nil)
	if len(got) != 2 || len(got[1]) == 0 || got[1][0].Kind == "target" {
		t.Fatalf("routed = %#v, want the take on the choice queue", got)
	}
}

// TestDigFixedLookBottomRestOnRealCorpus runs the two cards the ticket names
// through the registry cmd/xengine uses, not a hand-built IR.
func TestDigFixedLookBottomRestOnRealCorpus(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "..", ".cards")); err != nil {
		t.Skip("needs the corpus (make fetch-cards compile-cards)")
	}
	reg := censusRegistry(t)
	for _, card := range []string{"Lone Revenant", "Dragonlord Ojutai"} {
		c, ok := reg.Lookup(card)
		if !ok {
			t.Fatalf("%s is not in the corpus", card)
		}
		src, ok := digSourceOnCard(reg, card)
		if !ok || !src.known || src.dynamic || !digRestTargetQueue(src) {
			t.Errorf("%s: digSource = %+v (ok %v), want a known fixed look with a bottom Library rest; faces %d",
				card, src, ok, len(c.Faces))
		}
	}
}
