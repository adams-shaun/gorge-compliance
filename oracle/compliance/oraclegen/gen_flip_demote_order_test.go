package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestFlipLeadsDemotedSharedStep pins the splice order on a gameplay step
// that carries BOTH a recorded coin flip and a shared-step trigger order:
// the flips prepend runs AFTER demoteTriggerOrderSpans, whose span start/count
// were recorded against the loop's pre-prepend indices. A prepend first would
// shift every span index onto the step's other answers and splice the inert
// text over them -- the scripted target answer dropped, the unconsumable
// trigger-order names left live in its place.
func TestFlipLeadsDemotedSharedStep(t *testing.T) {
	order := triggerOrderDecision(1, "p0:Adrenaline Jockey", "p0:Rangers' Refueler")
	target := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Max: 1, Picks: []string{"Wastes"}, PickRefs: []string{"p0:Wastes#9"},
		PickIdx: []int{0}, PickKinds: []string{"search"}}
	flips := []rules.OracleFlip{{Turn: 1, Step: 1, Seat: 0, Win: true}}
	// Precondition: the flip is a gameplay-window one (the prepend class), the
	// order's sources are distinct (so the span is keyable), and the target
	// answer differs from the demoted text (the splice has something to lose).
	if flips[0].Step < 0 {
		t.Fatalf("precondition: the flip must be a gameplay-window flip")
	}
	if !triggerOrderNamesDistinct(order) {
		t.Fatalf("precondition: picks must have distinct source names, refs %v", order.PickRefs)
	}
	if target.Picks[0] == order.Picks[0] {
		t.Fatalf("precondition: the target pick must differ from the order's text pick")
	}
	got := xanswersSetup([]rules.OracleDecision{target, order}, 2, nil, nil, nil, flips)
	want := [][]XAnswer{
		nil,
		{{0, "flip", "win"}, {0, "target", "Wastes"},
			{0, "choice", "When this enters, do a thing."}, {0, "choice", "When this enters, do a thing."}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (flip leads; the scripted target survives the demote splice)", got, want)
	}
}

// TestSetupFlipQueueOrderBetweenSetupAnswersAndAsks pins the drive-window
// queue's lead order: the as-enters choices first (consumed by the placement
// dialogs during build()), then the flips (consumed while the drive walks to
// the target turn), then the drive's other asks (a modal pick, a yes/no, a
// labelled arrange) -- never inverted by a later reorder.
func TestSetupFlipQueueOrderBetweenSetupAnswersAndAsks(t *testing.T) {
	choice := rules.OracleDecision{Step: -1, Seat: 0, Kind: "choose_n", Resume: "etb",
		Options: 1, Min: 1, Max: 1, Picks: []string{"White"}, PickIdx: []int{0},
		PickKinds: []string{"color"}}
	ask := rules.OracleDecision{Step: -1, Seat: 0, Kind: "choose", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Yes"}, PickRefs: []string{"p0:Ashling, Rekindled"},
		PickIdx: []int{0}, PickKinds: []string{"yes"}}
	flips := []rules.OracleFlip{{Turn: 1, Step: -1, Seat: 0, Win: false}}
	// Precondition: the decision classes must be the ones the lead order is
	// asserted over (an as-enters choice and a setup drive ask), and the flip
	// a drive-window one.
	if !IsSetupChoice(choice) {
		t.Fatalf("precondition: %+v must be an as-enters setup choice", choice)
	}
	if got := setupDriveAnswers(ask, nil); !reflect.DeepEqual(got, []XAnswer{{0, "setup_choice", "yes"}}) {
		t.Fatalf("precondition: %+v must script a setup ask, got %v", ask, got)
	}
	if flips[0].Step >= 0 {
		t.Fatalf("precondition: the flip must be a drive-window flip")
	}
	got := xanswersSetup([]rules.OracleDecision{choice, ask}, 1, nil, nil, nil, flips)
	want := [][]XAnswer{{{0, "setup_choice", "White"}, {0, "setup_flip", "lose"}, {0, "setup_choice", "yes"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswersSetup = %#v, want %#v (as-enters choice, then flip, then drive ask)", got, want)
	}
}
