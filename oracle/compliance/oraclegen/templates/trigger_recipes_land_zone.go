package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Probes of the origin-scoped land-play cause (levelb.LandPlayedZoneSub).
var (
	// graveyardLandGranters let their controller play lands from the
	// graveyard (a Continuous MayPlay$ static): the cause plays a Forest out
	// of p0's graveyard under the first one gorge and XMage both carry.
	graveyardLandGranters = []string{"Crucible of Worlds", "Ramunap Excavator"}
	// exileLandProbe exiles the top two cards of its caster's library and
	// lets them be played until the end of the next turn ({2}{R} sorcery):
	// the cause casts it over two Mountains on the library top and plays one
	// of them from exile.
	exileLandProbe = "Light Up the Stage"
)

// landPlayedZoneRecipe serves a LandPlayed trigger scoped to a non-hand origin
// (Ghost-Spider's "play a land from exile", Shadow of the Goblin's "from
// anywhere other than your hand"): one cause per origin the line names that a
// probe can supply, graveyard first. A first-main-phase trigger on the source
// (Shadow of the Goblin's Unreliable Visions) is resolved first, so the land
// drop is offered at sorcery speed.
func landPlayedZoneRecipe(reg *cards.Registry, f *cards.Face, t *cards.Trigger) ([]triggerCause, string) {
	origin := strings.ToLower(t.ParamStr(cards.PKOrigin))
	prelude := setupResolvePrelude(f)
	var out []triggerCause
	if strings.Contains(origin, "graveyard") {
		for _, g := range graveyardLandGranters {
			if _, ok := reg.Lookup(g); !ok {
				continue
			}
			out = append(out, triggerCause{
				prelude:     prelude,
				battlefield: []string{g},
				graveyard:   []string{"Forest"},
				steps:       []oraclegen.Step{{Op: "play", Seat: 0, Card: "p0:Forest"}},
			})
		}
	}
	if strings.Contains(origin, "exile") {
		if cast, ok := castProbe(reg, exileLandProbe); ok {
			out = append(out, triggerCause{
				prelude:    prelude,
				hand:       []string{exileLandProbe},
				libraryTop: []string{"Mountain", "Mountain"},
				steps: []oraclegen.Step{
					cast, {Op: "resolve"},
					{Op: "play", Seat: 0, Card: "p0:Mountain"},
				},
			})
		}
	}
	if len(out) == 0 {
		return nil, "land played from " + origin + ": no probe supplies that origin"
	}
	return out, ""
}
