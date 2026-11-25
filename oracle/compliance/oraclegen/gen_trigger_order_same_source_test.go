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

// TestTriggerOrderDuplicatedSourceUsesNames: Harmonic Prodigy's Prowess and
// two Soul-Scar Mage Prowess triggers (Prodigy doubles another Wizard's
// trigger) name Soul-Scar twice, but both picks are the SAME ability of the
// SAME object, so their mutual order is unobservable (CR 603.3b). The distinct
// sources still order by name; the second Soul-Scar pick is the one XMage
// auto-pushes. Before the fix the all-"Prowess" text form keyed every pick
// "Prowess", XMage matched Soul-Scar's ability first and ordered the stack
// itself.
func TestTriggerOrderDuplicatedSourceUsesNames(t *testing.T) {
	d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "order", GorgeKind: "trigger_order",
		Options: 3, Min: 3, Max: 3, PickIdx: []int{0, 1, 2}, PickKinds: []string{"trigger", "trigger", "trigger"},
		Picks:    []string{"Harmonic Prodigy: Prowess", "Soul-Scar Mage: Prowess", "Soul-Scar Mage: Prowess"},
		PickRefs: []string{"p0:Harmonic Prodigy", "p0:Soul-Scar Mage", "p0:Soul-Scar Mage"}}
	if !triggerOrderNamesDistinct(d) {
		t.Fatalf("precondition: %v collapses to distinct source names", d.PickRefs)
	}
	got := XAnswers([]rules.OracleDecision{d}, 2, nil)
	want := [][]XAnswer{
		nil,
		{{0, "choice", "Harmonic Prodigy"}, {0, "choice", "Soul-Scar Mage"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v (order the distinct sources by name)", got, want)
	}
}

// TestTriggerOrderIdenticalTextOnDistinctObjectsKeepsTextForm pins the other
// side of the interchangeable-group rule: Bartz and Boko plus three distinct
// Lifecreed Duo objects share one rule text, so a rule-text key would match
// the first object for every pick and XMage would order the stack itself. The
// keys cannot tell the picks apart (xmageTriggerOrderKeys returns ok=false),
// so the span keeps the inert verbatim text form, one answer per pick. The
// FIN level-B verdict pins the replayed result; this pins the routing.
func TestTriggerOrderIdenticalTextOnDistinctObjectsKeepsTextForm(t *testing.T) {
	life := "Whenever another creature you control enters, you gain 1 life."
	bartz := "When CARDNAME enters, each other Bird you control deals damage equal to its power to target creature an opponent controls."
	d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "order", GorgeKind: "trigger_order",
		Options: 4, Min: 4, Max: 4, PickIdx: []int{0, 1, 2, 3}, PickKinds: []string{"trigger", "trigger", "trigger", "trigger"},
		Picks: []string{
			"Lifecreed Duo: " + life, "Lifecreed Duo: " + life, "Lifecreed Duo: " + life,
			"Bartz and Boko: " + bartz,
		},
		PickRefs: []string{"p0:Lifecreed Duo", "p0:Lifecreed Duo#2", "p0:Lifecreed Duo#3", "p0:Bartz and Boko"}}
	if triggerOrderNamesDistinct(d) {
		t.Fatalf("precondition: %v still names the repeated source", d.PickRefs)
	}
	if keys, ok := xmageTriggerOrderKeys(d); ok {
		t.Fatalf("precondition: identical text on distinct refs keyed as %q", keys)
	}
	got := XAnswers([]rules.OracleDecision{d}, 2, nil)
	want := [][]XAnswer{
		nil,
		{{0, "choice", life}, {0, "choice", life}, {0, "choice", life}, {0, "choice", bartz}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XAnswers = %#v, want %#v (unkeyable repeats keep the verbatim text form)", got, want)
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

// TestTriggerOrderEventReadingRepeatNotCollapsed: Abomination's "put that many
// counters" trigger reads its event, so a source named twice is not one
// interchangeable group: collapsing [Hulk, Abomination x2] to two distinct
// names would let XMage auto-push an instance whose damage amount gorge ordered
// elsewhere. A keyword repeat (Prowess) still collapses.
func TestTriggerOrderEventReadingRepeatNotCollapsed(t *testing.T) {
	text := "Whenever a Gamma or Villain you control deals combat damage to a player, put that many +1/+1 counters on CARDNAME."
	d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "order", GorgeKind: "trigger_order",
		Picks:    []string{"Hulk: Rampage", "Abomination: " + text, "Abomination: " + text},
		PickRefs: []string{"p0:Hulk", "p0:Abomination", "p0:Abomination"}}
	if triggerOrderNamesDistinct(d) {
		t.Errorf("event-reading repeat of one ref must not collapse to distinct names")
	}
	d.Picks = []string{"Hulk: Rampage", "Abomination: Prowess", "Abomination: Prowess"}
	if !triggerOrderNamesDistinct(d) {
		t.Errorf("a keyword repeat stays one interchangeable group")
	}
}
