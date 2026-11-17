package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// The pay side of a triggered optional NON-mana cost with no cost_posed
// decline in its step is XMage's DoIfCostPaid chooseUse: it is posed whenever
// the cost can be paid, so the "yes" must lead the pay's own follow-up pick
// (the discard or sacrifice). Unscripted, XMage's strict run stops at
// "Missing CHOICE def" on the Yes/No dialog (Melded Moxite "Discard a card and
// draw two cards?", Vampire Gourmand "Sacrifice another creature and ...?",
// Beetle-Headed Merchants, farm shape EntersBattlefieldTriggeredAbility /
// AttacksTriggeredAbility).
func TestXAnswersTriggerCostPayNonMana(t *testing.T) {
	pay := func(label, ref string) rules.OracleDecision {
		return rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Options: 2, Min: 1, Max: 1, Picks: []string{label}, PickIdx: []int{0},
			PickRefs: []string{ref}, PickKinds: []string{"trigger_cost_pay"},
			ObjectPicks: []string{ref}, Via: "answer"}
	}
	discard := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 7, Min: 1, Max: 1, Picks: []string{"Diplomatic Relations"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Diplomatic Relations"}, PickKinds: []string{"discard"},
		ObjectPicks: []string{"p0:Diplomatic Relations"}, Via: "answer"}
	sacrifice := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 3, Min: 1, Max: 1, Picks: []string{"Mona Lisa, Science Geek"}, PickIdx: []int{2},
		PickRefs: []string{"p0:Mona Lisa, Science Geek"}, PickKinds: []string{"sacrifice"},
		ObjectPicks: []string{"p0:Mona Lisa, Science Geek"}, Via: "answer"}

	for _, c := range []struct {
		name string
		ds   []rules.OracleDecision
		want [][]XAnswer
	}{
		{"discard", []rules.OracleDecision{pay("Discard 1 card", "p0:Melded Moxite"), discard},
			[][]XAnswer{nil, {{0, "choice", "yes"}, {0, "choice", "Diplomatic Relations"}}}},
		{"sacrifice", []rules.OracleDecision{pay("Sacrifice another creature or artifact", "p0:Beetle-Headed Merchants"), sacrifice},
			[][]XAnswer{nil, {{0, "choice", "yes"}, {0, "choice", "Mona Lisa, Science Geek"}}}},
		{"sacrifice alone", []rules.OracleDecision{pay("Sacrifice another creature", "p0:Vampire Gourmand")},
			[][]XAnswer{nil, {{0, "choice", "yes"}}}},
		// A mana payment and a blight keep their pre-existing routing.
		{"mana pay", []rules.OracleDecision{pay("Pay W", "p0:Brigid")}, nil},
		{"blight", []rules.OracleDecision{pay("Blight 1", "p0:Scuzzback Scrounger")}, nil},
	} {
		got := xanswers(c.ds, 2, nil, nil)
		if c.want == nil {
			if got != nil {
				t.Errorf("%s: xanswers = %#v, want nothing scripted", c.name, got)
			}
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: xanswers = %#v, want %#v", c.name, got, c.want)
		}
	}
}

// An as-enters colour pick beside a second replacement effect (Edgewall Inn's
// "enters tapped"): XMage first asks the controller to ORDER the replacement
// effects (chooseReplacementEffect), a decision gorge never poses. The
// generator must not invent an answer for that ask -- the driver answers it
// itself (ScenarioReplay.ScriptedChoicePlayer.chooseReplacementEffect) -- so
// the colour stays the ONLY answer and leads step zero's queue, ahead of any
// gameplay answer. A second queued answer would shift every later ask.
func TestXAnswersSetupColourIsTheOnlyReplacementAnswer(t *testing.T) {
	colour := rules.OracleDecision{Step: -1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 5, Min: 1, Max: 1, Picks: []string{"White"}, PickIdx: []int{0},
		PickRefs: []string{"White"}, PickKinds: []string{"color"}, Resume: "etb", Via: "answer"}
	got := xanswers([]rules.OracleDecision{colour}, 3, nil, nil)
	want := [][]XAnswer{{{0, "setup_choice", "White"}}, nil, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
}
