package levelb_test

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestSelfLTBRealCards classifies corpus cards whose own trigger fires when
// they leave the battlefield for somewhere other than the graveyard alone.
func TestSelfLTBRealCards(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, key, sub, gap string }{
		{"Featherbrained Filcher", "trigger#0.0", "trigger.ltb-self", ""},
		{"Mouser Foundry", "trigger#0.1", "trigger.ltb-self", ""},
		{"Three Tree Scribe", "trigger#0.0", "trigger.ltb-self", ""},
		{"Syr Vondam, Sunstar Exemplar", "trigger#0.1", "trigger.ltb-self", ""},
		// A `Static$ True` self-cleanup trigger (ChangesZone | Origin$
		// Battlefield | Destination$ Any, DB$ Cleanup body) never reaches the
		// stack. It was a permanent named gap ("level B: static trigger",
		// tickets g17 + agent-20261009T090319Z-9b434465) until the operator
		// decision of 2026-10-13 (wave-4 lb4w2-bookkeep): its consequence IS
		// cross-engine observable through its consumer once the source is
		// flickered (CR 400.7, a new object) -- Planetarium's "only once each
		// turn" fires again, Superior Foes' first exiled card stays playable.
		// The bookkeeping template serves both (trigger_bookkeeping.go).
		{"Planetarium of Wan Shi Tong", "trigger#0.2", levelb.BookkeepingSub, ""},
		{"Superior Foes of Spider-Man", "trigger#0.1", levelb.BookkeepingSub, ""},
	} {
		t.Run(tc.name+" "+tc.key, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s is not in the corpus", tc.name)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				if r.Sub != tc.sub || r.Gap != tc.gap {
					t.Fatalf("%s %s: sub=%q gap=%q, want sub=%q gap=%q", tc.name, tc.key, r.Sub, r.Gap, tc.sub, tc.gap)
				}
				return
			}
			t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
		})
	}
}
