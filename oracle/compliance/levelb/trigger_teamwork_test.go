package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// TestTeamworkTapClassification: a Taps trigger with Teamwork$ True fires only
// when its source is tapped to pay a teamwork cost (CR 702.194), which no
// attack or tap-spell cause produces, so it routes to its own sub-family; any
// other Teamwork$ shape or a gated one stays a gap.
func TestTeamworkTapClassification(t *testing.T) {
	creature := []string{"Creature"}
	for _, tc := range []struct {
		name   string
		types  []string
		params map[string]string
		want   string
	}{
		{"self teamwork tap (Agent Maria Hill)", creature, map[string]string{"ValidCard": "Card.Self", "Teamwork": "True"}, TeamworkTappedSub},
		{"another creature's teamwork tap", creature, map[string]string{"ValidCard": "Creature.YouCtrl", "Teamwork": "True"}, "trigger.gap:Taps"},
		{"conditioned teamwork tap", creature, map[string]string{"ValidCard": "Card.Self", "Teamwork": "True", "IsPresent": "Card.Self+untapped"}, "trigger.gap:Taps"},
		{"teamwork tap from the graveyard", creature, map[string]string{"ValidCard": "Card.Self", "Teamwork": "True", "TriggerZones": "Graveyard"}, "trigger.gap:Taps"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(cardOf(&cards.Face{Types: tc.types, Triggers: []cards.Trigger{trig("Taps", tc.params)}}))
			if len(got) != 1 || got[0].Sub != tc.want {
				t.Fatalf("classification = %+v, want %s", got, tc.want)
			}
		})
	}
}
