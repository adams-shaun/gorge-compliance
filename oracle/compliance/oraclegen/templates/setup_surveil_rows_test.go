package templates

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// The three stored `diverge` verdict rows whose only difference was
// `setup p0.graveyard` (class level-B setup arrange fallback): the runner's
// fallback sends the upkeep-surveil card to the graveyard, so the scenario
// must script the same answer for XMage. XMage's unscripted default used to
// agree (it sent every card to the graveyard) but its driver now keeps an
// unscripted surveil card on top, so the Wastes the fallback sends away is
// scripted on the setup choice queue. generateServed pins the replay against
// the live corpus; a generator regression that unserves a row fails at the
// generate step.
//
// Each card's setup graveyard also holds a card the template seeds for the
// activated ability, which the Surveil never looked at. The exact queue is
// therefore only the one surveilled card, not the whole graveyard: asserting
// the whole queue catches a seeded card leaking into the selection, a kept
// card named after the skip, or a wrong order.
func TestSetupParityD1SurveilRows(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, row := range []struct {
		name  string
		gy    []string
		queue []string
	}{
		{"Broodheart Engine", []string{"Grizzly Bears", "Wastes"}, []string{"Wastes"}},
		{"Essence Anchor", []string{"Nessian Asp", "Wastes"}, []string{"Wastes"}},
		{"Morcant's Eyes", []string{"Wastes"}, []string{"Wastes"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			it, res := generateServed(t, reg, row.name, "activate#0.0")
			snap := res.Snapshots[0]
			if snap.Checkpoint != "setup" {
				t.Fatalf("precondition: %s checkpoint = %s, want setup", row.name, snap.Checkpoint)
			}
			gy := snap.Players[0].Graveyard
			if !reflect.DeepEqual(gy, row.gy) {
				t.Errorf("%s setup p0 graveyard = %v, want %v (the stored XMage list)", row.name, gy, row.gy)
			}
			if len(it.XAnswers) == 0 {
				t.Fatalf("%s has no xmage_answers", row.name)
			}
			var got []string
			for _, a := range it.XAnswers[0] {
				if a.Seat != 0 || a.Kind != "setup_choice" {
					t.Errorf("%s setup answer = %+v, want seat 0 setup_choice", row.name, a)
					continue
				}
				got = append(got, a.Value)
			}
			if !reflect.DeepEqual(got, row.queue) {
				t.Errorf("%s setup choice queue = %v, want %v (the one surveilled card selected, no skip)",
					row.name, got, row.queue)
			}
		})
	}
}
