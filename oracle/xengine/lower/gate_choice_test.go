package lower

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/state"
)

// TestGateChosenColorZone: the setup drive answers a placed permanent's
// as-enters colour ask from the battlefield loop, so a battlefield object's
// ChosenColor is restatable and passes. Every other zone has no placement to
// answer it, so it must refuse. The engine clears the field when a permanent
// leaves the battlefield, which makes the off-battlefield shape latent rather
// than impossible, so the zone guard is the only thing keeping the gate
// fail-closed.
func TestGateChosenColorZone(t *testing.T) {
	w := &walker{g: &state.Game{}, why: map[string]bool{}}
	for _, tc := range []struct {
		zone state.Zone
		want bool
	}{
		{state.ZBattlefield, false},
		{state.ZGraveyard, true},
		{state.ZExile, true},
		{state.ZHand, true},
		{state.ZStack, true},
	} {
		o := &state.Object{Zone: tc.zone, ChosenColor: "Red"}
		if got := w.live("Object", "ChosenColor", reflect.ValueOf(o.ChosenColor), o); got != tc.want {
			t.Errorf("zone %v: ChosenColor live = %v, want %v", tc.zone, got, tc.want)
		}
	}
}
