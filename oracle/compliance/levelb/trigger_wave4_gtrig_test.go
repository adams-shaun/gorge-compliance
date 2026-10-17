package levelb_test

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestWave4TriggerShapeClassification pins the wave-4 trigger-shape routing
// (lb4-gtrig): the Static$ True may-play cast rider, the origin-scoped land
// play, and the zone-less "When CARDNAME enters" ChangesZone line, which is a
// self-ETB the level-A cast-resolve scenario settles (it does not look back
// in time, engine/rules/trigger_zone_gate.go zoneChangeNoLookBack).
func TestWave4TriggerShapeClassification(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key, sub string
		covered        bool
	}{
		{"Intrepid Paleontologist", "trigger#0.0", levelb.MayPlayRiderSub, false},
		{"Undead Sprinter", "trigger#0.0", levelb.MayPlayRiderSub, false},
		{"Hundred-Battle Veteran", "trigger#0.0", levelb.MayPlayRiderSub, false},
		{"Noctis, Prince of Lucis", "trigger#0.0", levelb.MayPlayRiderSub, false},
		{"Leonardo, Sewer Samurai", "trigger#0.0", levelb.MayPlayRiderSub, false},
		{"Gwen Stacy", "trigger#1.0", levelb.LandPlayedZoneSub, false},
		{"Shadow of the Goblin", "trigger#0.1", levelb.LandPlayedZoneSub, false},
		{"Ghostly Dancers", "trigger#0.0", "trigger.etb-other", true},
		{"Bullseye, Death Dealer", "trigger#0.0", "trigger.etb-other", true},
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
			if r.Sub != tc.sub || r.CoveredByA != tc.covered || r.Gap != "" {
				t.Errorf("%s %s = sub %q covered %v gap %q, want %q covered %v", tc.name, tc.key, r.Sub, r.CoveredByA, r.Gap, tc.sub, tc.covered)
			}
		}
		if !found {
			t.Errorf("%s has no requirement %s", tc.name, tc.key)
		}
	}
}

// TestWave4AttachedRelative pins Eriette as trigger.attached.
func TestWave4AttachedRelative(t *testing.T) {
	c, ok := testutil.CorpusRegistry(t).Lookup("Eriette, the Beguiler")
	if !ok {
		t.Fatal("Eriette not in the corpus")
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key == "trigger#0.0" && r.Sub != "trigger.attached" {
			t.Fatalf("Eriette trigger#0.0 = %q, want trigger.attached", r.Sub)
		}
	}
}

// TestWave4TurnFaceUpYouCause: Growing Dread's "Whenever you turn a permanent
// face up" is served by the turned-face-up-other recipe (p0's own special
// action), while Oblivious Bookworm's Static$ True imprint line is a
// bookkeeping record observed through its end-step consumer.
func TestWave4TurnFaceUpYouCause(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, key, sub string }{
		{"Growing Dread", "trigger#0.1", levelb.TurnedFaceUpOtherSub},
		{"Oblivious Bookworm", "trigger#0.1", levelb.BookkeepingSub},
	} {
		c, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s not in the corpus", tc.name)
		}
		for _, r := range levelb.Requirements(c) {
			if r.Key == tc.key && r.Sub != tc.sub {
				t.Errorf("%s %s = %q, want %q", tc.name, tc.key, r.Sub, tc.sub)
			}
		}
	}
}
