package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// The residual xproof decision_shape shapes (xp-xmage-choice-def-residual):
// a forced X on an activation, an as-enters copy decline, a blight trigger
// cost and a two-zone library/graveyard search election.

// TestActivationForcedXAnnounceIsScripted: XMage's announceX is posed for an
// activation's real mana {X} even when gorge offered the one legal value
// (Talon of Pain with no charge counters: X = 0). A Craft/XMin count is not a
// VariableManaCost and stays unscripted (Saheeli's Lattice).
func TestActivationForcedXAnnounceIsScripted(t *testing.T) {
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name: "Talon of Pain",
		Abilities: []*cards.SA{{
			Kind: "AB", API: "DealDamage",
			Params: map[string]string{"Cost": "X T SubCounter<X/CHARGE>"},
		}},
	}}})
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name: "Saheeli's Lattice",
		Abilities: []*cards.SA{{
			Kind: "AB", API: "ChangeZone",
			Params: map[string]string{"Cost": "4 R XMin1 ExileCtrlOrGrave<X/Dinosaur.Other> Exile<1/CARDNAME>"},
		}},
	}}})
	x0 := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 1, Min: 1, Max: 1, Picks: []string{"X = 0"}, PickIdx: []int{0},
		PickRefs: []string{"X = 0"}, PickKinds: []string{"x"}}
	idx := 0
	talon := Scenario{Steps: []Step{{Op: "activate", Seat: 0, Card: "p0:Talon of Pain", AbilityIndex: &idx}}}
	want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "X=0"}}}
	if got := xanswersForScenario(reg, rules.OracleResult{Decisions: []rules.OracleDecision{x0}}, talon, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("Talon X = 0 activation = %#v, want %#v", got, want)
	}
	craft := Scenario{Steps: []Step{{Op: "activate", Seat: 0, Card: "p0:Saheeli's Lattice", AbilityIndex: &idx}}}
	if got := xanswersForScenario(reg, rules.OracleResult{Decisions: []rules.OracleDecision{x0}}, craft, nil, nil); got != nil {
		t.Fatalf("Craft XMin activation = %#v, want unscripted", got)
	}
}

// TestCopyEnterAsItselfScriptsNoChooseUse: XMage's optional
// EntersBattlefieldEffect asks "Use effect of <name>?" before CopyPermanent;
// gorge's decline is that chooseUse's no. A copy pick is yes, then the target.
func TestCopyEnterAsItselfScriptsNoChooseUse(t *testing.T) {
	decline := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Resume: "etb", Options: 1, Min: 1, Max: 1, Picks: []string{"Enter as itself"},
		PickIdx: []int{0}, PickRefs: []string{"Enter as itself"}, PickKinds: []string{"clone"}}
	want := [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: "no"}}}
	if got := xanswers([]rules.OracleDecision{decline}, 2, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("Enter as itself = %#v, want %#v", got, want)
	}
	copyPick := decline
	copyPick.Options, copyPick.Picks, copyPick.PickRefs = 2, []string{"Grizzly Bears"}, []string{"p1:Grizzly Bears"}
	want = [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 0, Kind: "target", Value: "Grizzly Bears"}}}
	if got := xanswers([]rules.OracleDecision{copyPick}, 2, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("copy pick = %#v, want %#v", got, want)
	}
}

// TestBlightTriggerCostPayLeadsTriggerOrder: Boggart Mischief's may-blight is
// DoIfCostPaid's chooseUse; the yes must precede the trigger order answer that
// the same step scripts.
func TestBlightTriggerCostPayLeadsTriggerOrder(t *testing.T) {
	pay := rules.OracleDecision{Step: 7, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Blight 1"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Boggart Mischief"}, PickKinds: []string{"trigger_cost_pay"},
		ObjectPicks: []string{"p0:Boggart Mischief"}, Via: "answer"}
	order := rules.OracleDecision{Step: 7, Seat: 0, Kind: "order", GorgeKind: "trigger_order",
		Options: 2, Min: 2, Max: 2,
		Picks: []string{
			"Shambling Goblin: When CARDNAME dies, target creature an opponent controls gets -1/-1 until end of turn.",
			"Boggart Mischief: Whenever a Goblin creature you control dies, each opponent loses 1 life and you gain 1 life.",
		},
		PickIdx: []int{0, 1}, PickRefs: []string{"p0:Shambling Goblin", "p0:Boggart Mischief"},
		PickKinds: []string{"trigger", "trigger"}}
	got := xanswers([]rules.OracleDecision{pay, order}, 8, nil, nil)
	if len(got[7]) == 0 || got[7][0] != (XAnswer{Seat: 0, Kind: "choice", Value: "yes"}) {
		t.Fatalf("Boggart step 7 = %#v, want the blight yes first", got[7])
	}
}

// TestSearchZoneElectionScriptsPerZone: SearchLibraryGraveyardPutInHandEffect
// asks the library then the graveyard chooseUse; gorge's zone election is one
// boolean per prompt. A wider origin set keeps the old unscripted handling.
func TestSearchZoneElectionScriptsPerZone(t *testing.T) {
	zone := func(options int, picks ...string) rules.OracleDecision {
		kinds := make([]string, len(picks))
		for i := range kinds {
			kinds[i] = "zone"
		}
		return rules.OracleDecision{Step: 8, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Resume: "search_zones", Options: options, Min: 1, Max: options,
			Picks: picks, PickRefs: append([]string(nil), picks...), PickKinds: kinds}
	}
	for _, c := range []struct {
		name string
		d    rules.OracleDecision
		want []XAnswer
	}{
		{"library", zone(2, "Search the library"),
			[]XAnswer{{0, "choice", "yes"}, {0, "choice", "no"}}},
		{"graveyard", zone(2, "Search the graveyard"),
			[]XAnswer{{0, "choice", "no"}, {0, "choice", "yes"}}},
		{"both", zone(2, "Search the library", "Search the graveyard"),
			[]XAnswer{{0, "choice", "yes"}, {0, "choice", "yes"}}},
		{"wider origin", zone(3, "Search the library", "Search the graveyard", "Search the hand"), nil},
	} {
		all := xanswers([]rules.OracleDecision{c.d}, 9, nil, nil)
		var got []XAnswer
		if len(all) > 8 {
			got = all[8]
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %#v, want %#v", c.name, got, c.want)
		}
	}
}
