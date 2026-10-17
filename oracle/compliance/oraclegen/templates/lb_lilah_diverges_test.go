package templates

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// The level-B rows (ticket lb-lilah) whose gorge half was an Effect-delivered
// Event$ Moved redirect with a SubAbility$/RememberChanged$ body: each
// resolving spell used to land in the graveyard behind a loud Note. The
// rider-level behaviour (plotted, delayed return, time counters, buyback,
// Foundry's election) is pinned in rules/lb_effect_exile_redirect_test.go.
func TestLBEffectDeliveredRedirectRowsExileTheSpell(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key, spell string
	}{
		{"Lilah, Undefeated Slickshot", "trigger#0.0", "Terminate"},
		{"Feather, the Redeemed", "trigger#0.0", "Giant Growth"},
		{"Gandalf of the Secret Fire", "trigger#0.0", "Shock"},
	} {
		res := d8bPlay(t, reg, tc.name, tc.key)
		d8bCastStep(t, res, tc.name)
		p := res.Snapshots[len(res.Snapshots)-1].Players[0]
		if slices.Contains(p.Graveyard, tc.spell) {
			t.Errorf("%s %s: p0.graveyard = %v holds %s, want it exiled by the redirect", tc.name, tc.key, p.Graveyard, tc.spell)
		}
		if !slices.Contains(p.Exile, tc.spell) {
			t.Errorf("%s %s: p0.exile = %v, want it to hold %s", tc.name, tc.key, p.Exile, tc.spell)
		}
	}
}
