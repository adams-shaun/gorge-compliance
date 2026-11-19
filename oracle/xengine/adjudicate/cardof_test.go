package adjudicate

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
)

// TestCardOfSkipsLeftSourceLabel: the comparator's <left-battlefield> stand-in
// labels a source that left the battlefield (CR 113.7a, 603.10a). It is not a
// card, so the adjudicator must not attribute a ticket to it. The exact
// SUP-s148-supp window is a p1 dies trigger whose source died beside the
// sourceless monarch trigger (CR 725.2); the next object the values name is
// the monarch designation, and a real source after the stand-in is still used.
func TestCardOfSkipsLeftSourceLabel(t *testing.T) {
	it := oraclegen.Item{ID: "xe:RAND:2:18:w12"}
	for range 27 {
		it.Steps = append(it.Steps, oraclegen.Step{Op: "pass"})
	}
	f := classify.Finding{
		Step:  26,
		Gorge: "[ability:<left-battlefield>@p1, ability:@p0]",
		Other: "[ability:<left-battlefield>@p1, ability:The Monarch@p0]",
	}
	if got := cardOf(it, f); got != "The Monarch" {
		t.Fatalf("cardOf = %q, want The Monarch", got)
	}
	f.Other = "[ability:<left-battlefield>@p1, ability:Brood of Cockroaches@p0]"
	if got := cardOf(it, f); got != "Brood of Cockroaches" {
		t.Fatalf("cardOf = %q, want Brood of Cockroaches", got)
	}
	f.Gorge = "[ability:<left-battlefield>@p1]"
	f.Other = "[ability:<left-battlefield>@p1]"
	if got := cardOf(it, f); got != "" {
		t.Fatalf("cardOf = %q, want no card", got)
	}
	// A sourceless trigger (CR 725.2) is not a card either. The Grave
	// Venerations row in the diagnosed register has exactly this shape: the
	// empty source must be skipped without swallowing the separator and the
	// label that follows it.
	f.Gorge = "[ability:@p0, ability:Grave Venerations@p0]"
	f.Other = f.Gorge
	if got := cardOf(it, f); got != "Grave Venerations" {
		t.Fatalf("cardOf = %q, want Grave Venerations", got)
	}
	// Both non-card labels before the real object: the left-battlefield
	// stand-in, then a sourceless trigger, then the card the scan must name.
	f.Gorge = "[ability:<left-battlefield>@p1, ability:@p0, ability:Real Card@p0]"
	f.Other = f.Gorge
	if got := cardOf(it, f); got != "Real Card" {
		t.Fatalf("cardOf = %q, want Real Card", got)
	}
	// A lone sourceless trigger names no card, so no sentinel ticket.
	f.Gorge = "[ability:@p0]"
	f.Other = f.Gorge
	if got := cardOf(it, f); got != "" {
		t.Fatalf("cardOf = %q, want no card", got)
	}
}
