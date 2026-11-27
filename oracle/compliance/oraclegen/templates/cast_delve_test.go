package templates

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// A Delve spell's level-A item pays short of its generic mana with a graveyard
// of Swamps to delve, so gorge's delve ask (CR 702.66) is part of the scenario
// instead of a full-pool cast that never reaches it. The answer exiles every
// seeded Swamp, and the template version moved to 2 because the generated
// scenario changed.
func TestCastResolveDelveSeedsGraveyard(t *testing.T) {
	if CastResolve.Version != 2 {
		t.Fatalf("CastResolve.Version = %d, want 2", CastResolve.Version)
	}
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, mana string
		picks      int
	}{
		{"Sultai Scavenger", "B", 5},
		{"Delve too Deep", "B", 2},
	} {
		item, skip := Generate(reg, tc.card)
		if skip != nil {
			t.Fatalf("%s skipped: %+v", tc.card, skip)
		}
		sc := item.Scenario
		grave := sc.Setup["p0"].Graveyard
		if len(grave) != tc.picks {
			t.Fatalf("%s: p0 graveyard has %d cards, want %d Swamps", tc.card, len(grave), tc.picks)
		}
		for i, card := range grave {
			if card != "Swamp" {
				t.Fatalf("%s: p0 graveyard[%d] = %q, want Swamp", tc.card, i, card)
			}
		}
		cast := sc.Steps[0]
		if cast.Mana != tc.mana {
			t.Fatalf("%s: cast pays %q, want %q", tc.card, cast.Mana, tc.mana)
		}
		last := cast.Answers[len(cast.Answers)-1]
		want := make([]string, tc.picks)
		for i := range want {
			want[i] = "Swamp"
		}
		if last.Kind != "choose" || !reflect.DeepEqual(last.Pick, want) {
			t.Fatalf("%s: last cast answer = %+v, want choose of %d Swamps", tc.card, last, tc.picks)
		}
	}
}
