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
func TestSetupParityD1SurveilRows(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, row := range []struct {
		name string
		gy   []string
	}{
		{"Broodheart Engine", []string{"Grizzly Bears", "Wastes"}},
		{"Essence Anchor", []string{"Nessian Asp", "Wastes"}},
		{"Morcant's Eyes", []string{"Wastes"}},
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
			// The same answer must reach XMage: the card the fallback sent to
			// the graveyard is selected by name on the setup choice queue, so
			// the driver's library chooser picks it instead of keeping it.
			scripted := false
			for _, as := range it.XAnswers {
				for _, a := range as {
					if a.Seat == 0 && a.Kind == "setup_choice" && a.Value == row.gy[len(row.gy)-1] {
						scripted = true
					}
				}
			}
			if !scripted {
				t.Errorf("%s xmage_answers = %v, want a setup_choice naming %q (the surveil card the fallback sent to the graveyard)",
					row.name, it.XAnswers, row.gy[len(row.gy)-1])
			}
		})
	}
}
