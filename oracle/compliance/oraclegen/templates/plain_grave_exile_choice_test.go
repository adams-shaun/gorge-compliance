package templates

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// TestPlainGraveExileCostFacesRideChoiceQueue pins the two plain
// ExileFromGraveCost faces measured against XMage: Feed the Cycle's forage
// OrCost and Soaring Stoneglider's "exile two cards from your graveyard or
// pay {1}{W}". Both are withNotTarget TargetCardInYourGraveyard asks reached
// through Target.choose -> Player.choose, which TestPlayer serves from
// makeChoose (the CHOICE queue), never from addTarget. A fixed-size selection
// is one makeChoose dialog, so its cards are one '^'-joined choice.
//
// The routing is by mechanism, not by face: xmQueue maps every exilecost kind
// to the choice queue and pickQueue marks only Craft's chooseTarget material
// as the exception (craft_cost_pick.go). A new plain ExileFromGraveCost face
// therefore gets this shape without a per-card table entry; these two keep
// the measured cast-cost probes from regressing to the target queue.
func TestPlainGraveExileCostFacesRideChoiceQueue(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		card string
		want []oraclegen.XAnswer
	}{
		// Feed the Cycle: the accepted forage election, then the three-card
		// graveyard exile as ONE makeChoose record.
		{"Feed the Cycle", []oraclegen.XAnswer{
			{Seat: 0, Kind: "choice", Value: "yes"},
			{Seat: 0, Kind: "choice", Value: "Wastes^Wastes^Wastes"},
		}},
		// Soaring Stoneglider: the OrCost election, then the joined two-card
		// graveyard exile.
		{"Soaring Stoneglider", []oraclegen.XAnswer{
			{Seat: 0, Kind: "choice", Value: "yes"},
			{Seat: 0, Kind: "choice", Value: "Wastes^Wastes"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.card, func(t *testing.T) {
			it, skip := Generate(reg, tc.card)
			if skip != nil {
				t.Fatalf("Generate(%s): %s", tc.card, skip.Reason)
			}
			var got []oraclegen.XAnswer
			for _, step := range it.XAnswers {
				got = append(got, step...)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%s xmage_answers = %+v, want %+v", tc.card, got, tc.want)
			}
			for _, a := range got {
				if a.Kind != "choice" {
					t.Errorf("%s scripts a %s answer %q; a plain ExileFromGraveCost is read through makeChoose, the choice queue", tc.card, a.Kind, a.Value)
				}
			}
		})
	}
}
