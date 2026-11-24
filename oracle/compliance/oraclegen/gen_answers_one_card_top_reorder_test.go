package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// A one-card effRearrangeTopOfLibrary arrange (a one-card library under
// Sensei's Divining Top or Index) is a full top reorder: XMage's
// putCardsOnTopOfLibrary asks nothing about a single card, so scripting a
// choice would leave an unconsumed answer.
func TestXAnswersOneCardFullTopReorderEmitsNothing(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange", Resume: "arrange", ArrangeKind: "bottom",
		Min: 1, Max: 1, Options: 1, Picks: []string{"Forest"},
	}
	got := xanswersSetup([]rules.OracleDecision{d}, 1, nil, nil, nil, nil)
	for step, as := range got {
		if len(as) != 0 {
			t.Fatalf("step %d: one-card full top reorder scripted %+v, want no answers", step, as)
		}
	}
}
