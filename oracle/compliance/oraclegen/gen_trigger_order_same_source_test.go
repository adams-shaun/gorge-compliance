package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestTriggerOrderSameSourceScriptsXMageRuleText pins the same-source form:
// when two simultaneous triggers share one source object (Thundertrap
// Trainer's ETB and Offspring, both "p0:Thundertrap Trainer"), the name form
// would answer the same name for both picks, which XMage's
// chooseTriggeredAbility cannot tell apart. The rule text is the only
// distinguishing answer, so every pick but the last (XMage pushes the last
// remaining ability without an ask) is keyed in XMage's rule-text form
// (xmageTriggerOrderKeys); the old form scripted every pick's verbatim text,
// and the unconsumed last one sat at the queue head for the next dialog.
func TestTriggerOrderSameSourceScriptsXMageRuleText(t *testing.T) {
	d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "order", GorgeKind: "trigger_order",
		Options: 2, Min: 2, Max: 2, PickIdx: []int{0, 1}, PickKinds: []string{"trigger", "trigger"},
		Picks:    []string{"Thundertrap Trainer: When this creature enters, look at the top four cards.", "Thundertrap Trainer: Offspring"},
		PickRefs: []string{"p0:Thundertrap Trainer", "p0:Thundertrap Trainer"}}
	got := XAnswers([]rules.OracleDecision{d}, 2, nil)
	want := [][]XAnswer{
		nil,
		{{0, "choice", "When this creature enters, look at the top four cards."}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v (same-source order keys all picks but the last)", got, want)
	}
}

// TestTriggerOrderDistinctSourceNamesSource is the sibling precondition: the
// same decision with DISTINCT sources still names each source (the Adrenaline
// Jockey fix). The two decisions differ only in PickRefs, so a regression that
// dropped the distinctness test would make this one fall back to text too.
func TestTriggerOrderDistinctSourceNamesSource(t *testing.T) {
	d := rules.OracleDecision{Step: 0, Seat: 0, Kind: "order", GorgeKind: "trigger_order",
		Options: 2, Min: 2, Max: 2, PickIdx: []int{0, 1}, PickKinds: []string{"trigger", "trigger"},
		Picks:    []string{"Adrenaline Jockey: Whenever you activate an exhaust ability, put a +1/+1 counter on this creature.", "Rangers' Refueler: Whenever you activate an exhaust ability, draw a card."},
		PickRefs: []string{"p0:Adrenaline Jockey", "p0:Rangers' Refueler"}}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)
	want := [][]XAnswer{{{0, "choice", "Adrenaline Jockey"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v (distinct sources named, last pick dropped)", got, want)
	}
}
