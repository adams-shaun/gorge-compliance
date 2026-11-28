package lower

import (
	"reflect"
	"slices"
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

// TestGateRefusesChosenTypeAndNumber: a type/number chosen as the permanent
// entered is held on it for its lifetime, but setup places the permanent, not
// the choice, and the lowered setup has no field to restate it. The gate must
// therefore refuse it in every zone, as develop did before the as-enters
// answer capture was reverted. Mirrors TestGateRefusesChosenMode; reverting
// either table entry to isPlaced makes this fail, because struct_ only
// consults live for a refuse entry and the base cut has neither field set.
func TestGateRefusesChosenTypeAndNumber(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	if r := capture(e, at, true).Reasons; len(r) != 0 {
		t.Fatalf("base cut refused: %v", r)
	}
	for _, tc := range []struct {
		name, field string
		set         func(*state.Object, bool)
	}{
		{"ChosenType", "field:Object.ChosenType", func(o *state.Object, on bool) {
			o.ChosenType = ""
			if on {
				o.ChosenType = "Elf"
			}
		}},
		{"ChosenNumber", "field:Object.ChosenNumber", func(o *state.Object, on bool) {
			o.ChosenNumber = 0
			if on {
				o.ChosenNumber = 7
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// One real card object, re-zoned for each case: the cut does not
			// hold a card in every zone (the graveyard and exile are empty at a
			// turn-three main), but the gate's field walk is zone-independent
			// and must refuse the choice wherever the object sits.
			var o *state.Object
			for i := range e.G.Objs {
				if x := &e.G.Objs[i]; x.Zone == state.ZBattlefield && x.Card != nil {
					o = x
					break
				}
			}
			if o == nil {
				t.Fatalf("no battlefield object with a card to plant %s on", tc.name)
			}
			orig := o.Zone
			defer func() { o.Zone = orig }()
			for _, zone := range []state.Zone{
				state.ZBattlefield, state.ZHand, state.ZLibrary,
				state.ZGraveyard, state.ZExile, state.ZStack,
			} {
				o.Zone = zone
				tc.set(o, true)
				r := capture(e, at, true).Reasons
				tc.set(o, false)
				if !slices.Contains(r, tc.field) {
					t.Errorf("%v: reasons %v, want %s", zone, r, tc.field)
				}
			}
		})
	}
}
