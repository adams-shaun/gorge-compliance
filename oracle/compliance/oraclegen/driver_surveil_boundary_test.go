package oraclegen

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// A partial arrange queues pile B (the selection), the skip, then pile A (the
// kept cards' order). The driver's order phase must consume exactly the kept
// labels: with more binned than kept cards a later same-step answer may name a
// binned (looked-at) card, and with more kept than binned cards every kept
// label must be consumed in PickIdx order.
func TestArrangeOrderConsumerLeavesFollowingSameNameAnswer(t *testing.T) {
	decisions := []rules.OracleDecision{
		{
			Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange",
			Options: 3, Min: 0, Max: 2,
			ArrangeLabels: []string{"Forest", "Island", "Mountain"},
			Picks:         []string{"Forest"}, PickIdx: []int{0},
			PickKinds: []string{"graveyard"},
		},
		{
			Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose_n",
			Options: 2, Min: 1, Max: 1,
			Picks: []string{"Island"}, PickIdx: []int{1}, PickKinds: []string{"label"},
		},
	}
	got := xanswers(decisions, 1, nil, nil)
	want := [][]XAnswer{{
		{Seat: 0, Kind: "choice", Value: "Island"},
		{Seat: 0, Kind: "choice", Value: "Mountain"},
		{Seat: 0, Kind: "choice", Value: "[choice_skip]"},
		{Seat: 0, Kind: "choice", Value: "Forest"},
		{Seat: 0, Kind: "choice", Value: "Island"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
	// binned (2) > kept (1): the arrange's one order label is followed
	// immediately by another answer naming a binned looked-at card.
	binned, kept, ok := libraryArrangePartition(decisions[0].ArrangeLabels, decisions[0].PickIdx, decisions[0].Options)
	if !ok || len(binned) <= len(kept) {
		t.Fatalf("boundary requires binned > kept, got binned=%v kept=%v ok=%v", binned, kept, ok)
	}
	if got[0][3].Value != "Forest" || got[0][4].Value != "Island" {
		t.Fatal("expected one order label followed by a same-step answer")
	}

	// kept (2) > binned (1): both kept labels follow the skip in PickIdx order.
	more := xanswers([]rules.OracleDecision{{
		Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange",
		Options: 3, Min: 0, Max: 2,
		ArrangeLabels: []string{"Forest", "Island", "Mountain"},
		Picks:         []string{"Mountain", "Forest"}, PickIdx: []int{2, 0},
		PickKinds: []string{"graveyard", "graveyard"},
	}}, 1, nil, nil)
	wantMore := [][]XAnswer{{
		{Seat: 0, Kind: "choice", Value: "Island"},
		{Seat: 0, Kind: "choice", Value: "[choice_skip]"},
		{Seat: 0, Kind: "choice", Value: "Mountain"},
		{Seat: 0, Kind: "choice", Value: "Forest"},
	}}
	if !reflect.DeepEqual(more, wantMore) {
		t.Fatalf("kept>binned xanswers = %#v, want %#v", more, wantMore)
	}

	source, err := os.ReadFile("../../xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, required := range []string{
		"selectionEnded = true;",
		"if (selectionEnded && !selected.isEmpty())",
		"keptOrderLabels(queue, keptNames)",
		"static List<String> keptOrderLabels(List<String> queue, List<String> keptNames)",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("scripted library ordering does not bound its order phase by the kept cards: missing %q", required)
		}
	}
}
