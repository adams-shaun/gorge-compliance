package levelb_test

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestSelfCauseTriggerClassification pins the routing the self-cause change
// added: a self-transform trigger, a self
// cycling trigger and an origin-free land-play trigger now name a servable
// sub-family, while the shapes their causes cannot produce stay gaps --
// Shadow of the Goblin's not-owned hand land. The
// origin-scoped land plays (Gwen Stacy's from-exile, Shadow of the Goblin's
// not-from-hand) are served by trigger.land-played-zone.
func TestSelfCauseTriggerClassification(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, key, sub string }{
		{"Ashling, Rekindled", "trigger#0.1", "trigger.transformed"},
		{"Ashling, Rekindled", "trigger#1.0", "trigger.transformed"},
		{"Brigid, Clachan's Heart", "trigger#0.1", "trigger.transformed"},
		{"Sidequest: Raise a Chocobo", "trigger#1.0", "trigger.transformed"},
		{"Ultimecia, Time Sorceress", "trigger#1.0", "trigger.transformed"},
		{"Basri, Tomorrow's Champion", "trigger#0.0", "trigger.cycled"},
		{"Webstrike Elite", "trigger#0.0", "trigger.cycled"},
		{"The Endstone", "trigger#0.0", "trigger.land-played"},
		{"Gwen Stacy", "trigger#1.0", "trigger.land-played-zone"},
		{"Shadow of the Goblin", "trigger#0.1", "trigger.land-played-zone"},
		{"Shadow of the Goblin", "trigger#0.2", "trigger.gap:LandPlayed"},
	} {
		c, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s not in the corpus", tc.name)
		}
		found := false
		for _, r := range levelb.Requirements(c) {
			if r.Key != tc.key {
				continue
			}
			found = true
			if r.Sub != tc.sub {
				t.Fatalf("%s %s classified %s, want %s", tc.name, tc.key, r.Sub, tc.sub)
			}
		}
		if !found {
			t.Fatalf("%s carries no requirement %s", tc.name, tc.key)
		}
	}
}
