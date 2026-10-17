package levelb_test

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestBookkeepingTriggerPlans pins the bookkeeping-trigger class (wave-4
// lb4w2-bookkeep, operator decision 2026-10-13: a multi-turn template, no
// exemption): every Static$ True record/reset/cleanup line of the Standard
// sets, the fact it writes or clears, the consumer trigger that reads it and
// the trigger that records it first.
func TestBookkeepingTriggerPlans(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name               string
		slot               int
		kind, key          string
		consumer, recorder int
	}{
		// "if you've played a land ... from anywhere other than your hand":
		// the LandPlayed record and its Cleanup-step reset.
		{"Spider-Man 2099", 1, levelb.BookkeepingRecord, "svar:LandsPlayedFromNotHand", 0, 1},
		{"Spider-Man 2099", 2, levelb.BookkeepingReset, "svar:LandsPlayedFromNotHand", 0, 1},
		// "Do this only once each turn": RememberPlayed$ on the consumer's
		// own Play, cleared when it leaves and at every TurnBegin.
		{"Planetarium of Wan Shi Tong", 2, levelb.BookkeepingLeaves, "remembered", 0, 0},
		{"Planetarium of Wan Shi Tong", 3, levelb.BookkeepingReset, "remembered", 0, 0},
		// "for the first time each turn": the consumer's own DBStore switch.
		{"Desert Were-Worm", 2, levelb.BookkeepingReset, "svar:TrigSwitch", 0, 0},
		// "or you turned a permanent face up this turn": the imprint record
		// and its upkeep reset.
		{"Oblivious Bookworm", 1, levelb.BookkeepingRecord, "imprinted", 0, 1},
		{"Oblivious Bookworm", 2, levelb.BookkeepingReset, "imprinted", 0, 1},
		// "until you exile another card with this creature": the
		// ImprintOnHost$ link, cleared when it leaves.
		{"Superior Foes of Spider-Man", 1, levelb.BookkeepingLeaves, "imprinted", 0, 0},
	} {
		t.Run(tc.name+" "+strconv.Itoa(tc.slot), func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s is not in the corpus", tc.name)
			}
			b, ok := levelb.BookkeepingOf(c.Faces[0], tc.slot)
			if !ok {
				t.Fatalf("trigger#0.%d is not a bookkeeping trigger", tc.slot)
			}
			if b.Kind != tc.kind || b.Key != tc.key || b.Consumer != tc.consumer || b.Recorder != tc.recorder {
				t.Fatalf("plan = %+v, want kind=%s key=%s consumer=%d recorder=%d", b, tc.kind, tc.key, tc.consumer, tc.recorder)
			}
			key := "trigger#0." + strconv.Itoa(tc.slot)
			for _, r := range levelb.Requirements(c) {
				if r.Key == key {
					if r.Sub != levelb.BookkeepingSub || r.Gap != "" {
						t.Fatalf("%s classified %q gap %q, want %s", key, r.Sub, r.Gap, levelb.BookkeepingSub)
					}
					return
				}
			}
			t.Fatalf("no requirement %s", key)
		})
	}
	// Not bookkeeping: a stack-using line, and a Static$ line whose effect
	// is not a record (Desert Were-Worm's self-ETB reset is settled by the
	// cast-resolve scenario).
	c, _ := reg.Lookup("Desert Were-Worm")
	for _, slot := range []int{0, 1} {
		if b, ok := levelb.BookkeepingOf(c.Faces[0], slot); ok {
			t.Errorf("Desert Were-Worm trigger#0.%d classified bookkeeping: %+v", slot, b)
		}
	}
}
