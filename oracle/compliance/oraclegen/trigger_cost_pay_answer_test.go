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

// A trigger-cost pay with NO cost_posed decline on its step stays skipped:
// XMage's own AI answers its chooseUse, which the measured rows already agree
// with. Scuzzback Scrounger's two blight pays are this shape, and scripting
// them yes leaks a boolean onto the blight TARGET ask (verdict row
// Scuzzback Scrounger/trigger#0.0/v1 HARNESSes "Found wrong choice command:
// Yes ... Select a creature you control").
func TestTriggerCostPayAloneStaysSkipped(t *testing.T) {
	pay := rules.OracleDecision{Step: 2, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Blight 1"},
		PickRefs: []string{"p0:Scuzzback Scrounger"}, PickKinds: []string{"trigger_cost_pay"},
		ObjectPicks: []string{"p0:Scuzzback Scrounger"}, Via: "answer"}
	if got := xanswers([]rules.OracleDecision{pay}, 4, nil, nil); got != nil {
		t.Fatalf("xanswers = %#v, want nothing scripted for a pay with no cost_posed decline", got)
	}
}

// A cost_posed decline on a DIFFERENT step does not script this step's pay:
// Grub's decline (step 0) and blight pay (step 1) are separate asks, and the
// "no" cannot reach the pay's chooseUse. Scripting the pay here would only
// shift the later answers.
func TestTriggerCostPayInOtherStepThanDeclineStaysSkipped(t *testing.T) {
	pay := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Blight 2"},
		PickRefs: []string{"p0:Grub, the Fat"}, PickKinds: []string{"trigger_cost_pay"},
		ObjectPicks: []string{"p0:Grub, the Fat"}, Via: "answer"}
	decline := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 1, Min: 1, Max: 1, Picks: []string{"Do not pay"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Grub, the Fat"}, PickKinds: []string{"trigger_cost_decline"},
		CostPosed: true, ObjectPicks: []string{"p0:Grub, the Fat"}, Via: "answer"}
	got := xanswers([]rules.OracleDecision{decline, pay}, 4, nil, nil)
	want := [][]XAnswer{{{0, "choice", "no"}}, nil, nil, nil}
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
