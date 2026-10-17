package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// A triggered "you may pay <cost>. If you do, ..." is XMage's DoIfCostPaid:
// one chooseUse ("Pay {W} and transform Brigid?"), posed even when the cost
// is an unpayable mana cost, with the mana paid by XMage itself. Gorge poses
// a payment window (one "Tap <source> for mana" ask per source) and then the
// pay/decline ask. The window scripts nothing and the pay/decline ask is the
// boolean (measured strict misses on ECL Brigid, Grub and Oko: "Missing
// CHOICE def ... Pay {B} and transform Grub, Notorious Auntie?").
func TestTriggeredCostWindowRoutesToChooseUse(t *testing.T) {
	tap := func(label string) rules.OracleDecision {
		return rules.OracleDecision{Step: 2, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2,
			Picks: []string{label}, PickIdx: []int{0}, PickKinds: []string{"activate"}, Min: 1, Max: 1, First: label}
	}
	pay := rules.OracleDecision{Step: 2, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2,
		Picks: []string{"Pay W"}, PickIdx: []int{0}, PickKinds: []string{"trigger_cost_pay"}, Min: 1, Max: 1, First: "Pay W",
		TriggerCostMana: true}
	decline := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 1,
		Picks: []string{"Do not pay"}, PickIdx: []int{0}, PickKinds: []string{"trigger_cost_decline"}, Min: 1, Max: 1, First: "Do not pay",
		TriggerCostMana: true}

	got := XAnswers([]rules.OracleDecision{tap("Tap Plains for mana"), pay}, 3, nil)
	if want := []XAnswer{{0, "choice", "yes"}}; !reflect.DeepEqual(got[2], want) {
		t.Fatalf("paid triggered cost answers = %v, want %v (window taps unscripted, pay is yes)", got[2], want)
	}
	got = XAnswers([]rules.OracleDecision{decline}, 1, nil)
	if want := []XAnswer{{0, "choice", "no"}}; !reflect.DeepEqual(got[0], want) {
		t.Fatalf("decline-only triggered cost answers = %v, want %v (XMage still asks its chooseUse)", got[0], want)
	}
	// An unpayable NON-mana cost (Bushy Bodyguard's forage) is never asked
	// by XMage, which checks canPay first: nothing is scripted.
	component := decline
	component.TriggerCostMana = false
	got = XAnswers([]rules.OracleDecision{component}, 1, nil)
	if len(got) > 0 && len(got[0]) != 0 {
		t.Fatalf("decline-only non-mana triggered cost answers = %v, want none", got[0])
	}
	// A non-mana cost's pay pick (Scuzzback Scrounger's blight) keeps the
	// generic routing: XMage asks the blight target, not a chooseUse.
	blight := pay
	blight.Picks, blight.First, blight.TriggerCostMana = []string{"Blight 1"}, "Blight 1", false
	if triggerCostAnswer(blight) != "" {
		t.Fatal("a blight cost's pay pick was scripted as a chooseUse")
	}
	// A source tap NOT followed by a triggered cost's ask (a priority-time
	// mana activation) keeps its ordinary mapping.
	r := newAnswerRouting([]rules.OracleDecision{tap("Tap Plains for mana")})
	if r.triggerCostWindow(0) {
		t.Fatal("a lone source tap was claimed as a triggered-cost window")
	}
}
