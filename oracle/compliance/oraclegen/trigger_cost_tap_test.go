package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// A triggered optional tap over ONE candidate (Spider-Man, To the Rescue's
// "you may tap it": the tap window's part.N == 1, its single candidate the
// source itself) is XMage's card effect posing chooseUse(Outcome.Tap) and
// tapping the source itself. The answer is the bare boolean "yes"; the pick's
// object label must NOT follow (there is no follow-up makeChoose to consume
// it, and a leftover desyncs the queue). Measured divergence: the committed
// cast-resolve row carried a stale "Missing CHOICE def" detail because the
// driver had no scripted answer and foldedOptionalAnswer answered no -- XMage
// untapped, gorge tapped.
func TestTriggerCostTapRoutesToYes(t *testing.T) {
	tap := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 1,
		Picks: []string{"Spider-Man, To the Rescue"}, PickIdx: []int{0},
		PickKinds: []string{"trigger_cost_tap"}, Min: 1, Max: 1,
		First: "Spider-Man, To the Rescue"}
	got := XAnswers([]rules.OracleDecision{tap}, 2, nil)
	if want := []XAnswer{{0, "choice", "yes"}}; !reflect.DeepEqual(got[1], want) {
		t.Fatalf("trigger_cost_tap answers = %v, want %v (XMage's chooseUse taps the source; no object label follows)", got[1], want)
	}

	// A multi-candidate tap cost (Options > 1: "tap a creature" over several
	// permanents) keeps its ordinary label routing -- the boolean arm must
	// not swallow it.
	wide := tap
	wide.Options, wide.PickKinds = 3, []string{"trigger_cost_tap", "trigger_cost_tap", "trigger_cost_tap"}
	got = XAnswers([]rules.OracleDecision{wide}, 2, nil)
	if len(got) > 1 && len(got[1]) == 1 && got[1][0].Value == "yes" {
		t.Fatalf("multi-option tap cost was answered with the lone boolean %v; it must keep its object-label routing", got[1])
	}
}

// The decline side of the same optional tap already agrees without new
// scripting: XMage's unscripted chooseUse falls back to FALSE (no tap), the
// same answer gorge recorded, so anything scripted there would be a leftover.
func TestTriggerCostTapDeclineStaysFolded(t *testing.T) {
	decline := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 1,
		Picks: []string{"Do not pay"}, PickIdx: []int{0},
		PickKinds: []string{"trigger_cost_decline"}, Min: 1, Max: 1, First: "Do not pay"}
	got := XAnswers([]rules.OracleDecision{decline}, 2, nil)
	if len(got) > 1 && len(got[1]) != 0 {
		t.Fatalf("declined optional tap answers = %v, want none (the driver's foldedOptionalAnswer FALSE default already matches)", got[1])
	}
}
