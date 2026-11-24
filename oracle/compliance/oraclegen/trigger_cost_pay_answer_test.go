package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// A step that also scripts a lone cost_posed decline must answer its
// trigger-cost pay chooseUse yes, AHEAD of that decline's "no": XMage asks
// every optional triggered cost in resolution order, so Smuggler's Copter's
// may-draw chooseUse comes before Balthier and Fran's {1}{R}{G} one, and with
// the pay skipped the "no" shifted onto the Copter and XMage never looted what
// gorge looted (verdict row Balthier and Fran/trigger#0.0/v1, step 3
// p0.graveyard gorge "[Wastes]" against xmage "[]").
func TestTriggerCostPayScriptsYesBeforeDecline(t *testing.T) {
	pay := rules.OracleDecision{Step: 3, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Draw 1 card"},
		PickRefs: []string{"p0:Smuggler's Copter"}, PickKinds: []string{"trigger_cost_pay"},
		ObjectPicks: []string{"p0:Smuggler's Copter"}, Via: "answer"}
	decline := rules.OracleDecision{Step: 3, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 1, Min: 1, Max: 1, Picks: []string{"Do not pay"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Balthier and Fran"}, PickKinds: []string{"trigger_cost_decline"},
		CostPosed: true, ObjectPicks: []string{"p0:Balthier and Fran"}, Via: "answer"}
	got := xanswers([]rules.OracleDecision{pay, decline}, 4, nil, nil)
	want := [][]XAnswer{nil, nil, nil, {{0, "choice", "yes"}, {0, "choice", "no"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
}

// A blight trigger-cost pay with NO cost_posed decline on its step is still
// XMage's DoIfCostPaid chooseUse: BlightCost.canPay holds (gorge offered the
// pay side), DoIfCostPaid asks, and only then does BlightCost.doBlight ask for
// the creature. Scripting the yes leads that creature pick instead of letting
// the label shift onto the next ask (Boggart Mischief, Dream Seizer).
func TestBlightTriggerCostPayAloneScriptsYes(t *testing.T) {
	pay := rules.OracleDecision{Step: 2, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Blight 1"},
		PickRefs: []string{"p0:Scuzzback Scrounger"}, PickKinds: []string{"trigger_cost_pay"},
		ObjectPicks: []string{"p0:Scuzzback Scrounger"}, Via: "answer"}
	got := xanswers([]rules.OracleDecision{pay}, 4, nil, nil)
	want := [][]XAnswer{nil, nil, {{0, "choice", "yes"}}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
}

// A cost_posed decline on a DIFFERENT step does not suppress this step's
// blight pay: Grub's decline (step 0) and blight pay (step 1) are separate
// asks; the pay has its own chooseUse and scripts yes.
func TestBlightTriggerCostPayOtherStepThanDeclineScriptsYes(t *testing.T) {
	pay := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Blight 2"},
		PickRefs: []string{"p0:Grub, the Fat"}, PickKinds: []string{"trigger_cost_pay"},
		ObjectPicks: []string{"p0:Grub, the Fat"}, Via: "answer"}
	decline := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 1, Min: 1, Max: 1, Picks: []string{"Do not pay"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Grub, the Fat"}, PickKinds: []string{"trigger_cost_decline"},
		CostPosed: true, ObjectPicks: []string{"p0:Grub, the Fat"}, Via: "answer"}
	got := xanswers([]rules.OracleDecision{decline, pay}, 4, nil, nil)
	want := [][]XAnswer{{{0, "choice", "no"}}, {{0, "choice", "yes"}}, nil, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
}

// The pay side of a MANA triggered cost keeps its pool-payment skip (the
// pre-existing payment() path): a "Pay {G}" label is paid from the pool, so
// even a step that also scripts a decline never answers it.
func TestManaTriggerCostPayStaysSkipped(t *testing.T) {
	pay := rules.OracleDecision{Step: 3, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Pay {G}"},
		PickRefs: []string{"p0:Trystan, Callous Cultivator"}, PickKinds: []string{"trigger_cost_pay"},
		ObjectPicks: []string{"p0:Trystan, Callous Cultivator"}, Via: "answer"}
	decline := rules.OracleDecision{Step: 3, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 1, Min: 1, Max: 1, Picks: []string{"Do not pay"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Trystan, Callous Cultivator"}, PickKinds: []string{"trigger_cost_decline"},
		CostPosed: true, ObjectPicks: []string{"p0:Trystan, Callous Cultivator"}, Via: "answer"}
	got := xanswers([]rules.OracleDecision{pay, decline}, 4, nil, nil)
	want := [][]XAnswer{nil, nil, nil, {{0, "choice", "no"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
}

// An X-only triggered cost ("you may pay {X}", Wildborn Preserver) is XMage's
// DoIfCostPaid chooseUse like any mana cost, but gorge's TriggerCostMana is
// false for it (measured FDN xe:FDN:0:3:w24: "Missing CHOICE def ... Pay
// {X}?"). XMage asks the boolean BEFORE announceX, so the queue is "yes" then
// "X=<n>"; a decline returns before announceX, so it scripts the lone "no".
// A non-X, non-mana pay (a blight) still scripts nothing.
func TestXAnswersXOnlyTriggerCostPay(t *testing.T) {
	x := rules.OracleDecision{Step: 14, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 3, Min: 1, Max: 1, Picks: []string{"X = 0"}, PickKinds: []string{"trigger_cost_x"},
		PickRefs: []string{"X = 0"}, Via: "answer"}
	for _, label := range []string{"Pay X", "Pay {X}"} {
		pay := rules.OracleDecision{Step: 14, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Options: 2, Min: 1, Max: 1, Picks: []string{label}, PickIdx: []int{0},
			PickRefs: []string{"p0:Wildborn Preserver"}, PickKinds: []string{"trigger_cost_pay"},
			ObjectPicks: []string{"p0:Wildborn Preserver"}, Via: "answer"}
		got := XAnswers([]rules.OracleDecision{x, pay}, 15, nil)
		if want := []XAnswer{{0, "choice", "yes"}, {0, "choice", "X=0"}}; !reflect.DeepEqual(got[14], want) {
			t.Fatalf("%q answers = %v, want exactly %v", label, got[14], want)
		}
		if !xOnlyCostPay(pay) || triggerCostAnswer(pay) != "yes" {
			t.Fatalf("%q not recognised as an X-only trigger cost pay", label)
		}
	}
	decline := rules.OracleDecision{Step: 14, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Do not pay"}, PickIdx: []int{1},
		PickRefs: []string{"p0:Wildborn Preserver"}, PickKinds: []string{"trigger_cost_decline"},
		ObjectPicks: []string{"p0:Wildborn Preserver"}, Via: "answer"}
	got := XAnswers([]rules.OracleDecision{x, decline}, 15, nil)
	if want := []XAnswer{{0, "choice", "no"}}; !reflect.DeepEqual(got[14], want) {
		t.Fatalf("declined X-only answers = %v, want exactly %v (no announceX ask)", got[14], want)
	}
	for _, label := range []string{"Blight 1", "Pay {2}X", "Pay ", "Pay 2 life"} {
		d := rules.OracleDecision{Kind: "choose_n", Picks: []string{label}, PickKinds: []string{"trigger_cost_pay"}}
		if xOnlyCostPay(d) {
			t.Fatalf("%q misread as an X-only cost", label)
		}
	}
}
