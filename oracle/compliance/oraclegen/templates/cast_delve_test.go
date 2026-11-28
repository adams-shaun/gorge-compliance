package templates

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// A Delve spell's level-A item pays short of its generic mana with a graveyard
// of Swamps to delve, so gorge's delve ask (CR 702.66) is part of the scenario
// instead of a full-pool cast that never reaches it. The answer exiles every
// seeded Swamp, and the scenario's bytes moved, so a Delve cast carries
// cast-resolve generation 2.
//
// The generation is per item, not per template: an item's stamp is derived
// from the same predicate (delvePicks) that seeds its graveyard and writes its
// exile answer, so a card the Delve arm never touches keeps the v1 id and name
// its committed verdict row recorded. Shock and Rewrite Regrets are the
// non-Delve controls -- a template-wide bump moves their ids and orphans every
// cast-resolve verdict, which is what their recorded SHAs here guard.
func TestCastResolveDelveSeedsGraveyard(t *testing.T) {
	if CastResolve.Version != 1 {
		t.Fatalf("CastResolve.Version = %d, want 1: bumping it re-names every cast-resolve scenario, changed or not", CastResolve.Version)
	}
	if castResolveDelve.ID != CastResolve.ID || castResolveDelve.Version != 2 {
		t.Fatalf("castResolveDelve = %+v, want the cast-resolve id at version 2", castResolveDelve)
	}
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, mana string
		picks      int
	}{
		{"Sultai Scavenger", "B", 5},
		{"Delve too Deep", "B", 2},
		{"Shock", "R", 0},
		{"Rewrite Regrets", "CCCB", 0}, // {3}{B}: no Delve, so the C's stay in the pool
	} {
		f := faceOf(t, reg, tc.card)
		if got := delvePicks(f); got != tc.picks {
			t.Fatalf("%s: delvePicks = %d, want %d", tc.card, got, tc.picks)
		}
		version := CastResolve.Version
		if tc.picks > 0 {
			version = castResolveDelve.Version
		}
		if got := castTemplateFor(f); got.ID != CastResolve.ID || got.Version != version {
			t.Fatalf("%s: castTemplateFor = %+v, want %s v%d", tc.card, got, CastResolve.ID, version)
		}
		item, skip := Generate(reg, tc.card)
		if skip != nil {
			t.Fatalf("%s skipped: %+v", tc.card, skip)
		}
		if want := fmt.Sprintf("%s/%s/v%d", tc.card, CastResolve.ID, version); item.Template != CastResolve.ID || item.ID != want {
			t.Fatalf("%s: template %q id %q, want %q", tc.card, item.Template, item.ID, want)
		}
		if want := fmt.Sprintf("gen%d-%s", version, CastResolve.ID); item.Scenario.Name != want {
			t.Fatalf("%s: scenario name %q, want %q", tc.card, item.Scenario.Name, want)
		}
		// The cast step pays its pool: the whole cost for a non-Delve card (the
		// short pool belongs to the Delve arm alone), the coloured pips only for
		// a Delve one, whose generic mana the graveyard below pays.
		cast := castStep(t, item, tc.card)
		if cast.Mana != tc.mana {
			t.Fatalf("%s: cast pays %q, want %q", tc.card, cast.Mana, tc.mana)
		}
		if tc.picks == 0 {
			continue
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
