package templates_test

import (
	"slices"
	"sort"
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// static_exileperm_class_test.go is the wave-5 group X ratchet: the exile-zone
// permission and exiled-with link rows the engine now implements must each be
// SERVED by a generated scenario that observes the mechanism (the count's
// grant on the card; the offered cast and the price it charges).

// TestStaticExiledWithCountServesVeteranSurvivor: three turns of the Survival
// trigger exile three graveyard cards with the card, so its "three or more
// cards exiled with" static grants +3/+3 and hexproof (CR 406.6 / 607.2a).
func TestStaticExiledWithCountServesVeteranSurvivor(t *testing.T) {
	s, served := stateFixtureServed(t, "Veteran Survivor")
	if !served {
		t.Fatal("static#0.0 is skipped, want the Survival count fixture to serve it")
	}
	var exile []string
	for _, p := range s.Players {
		if p.Seat == 0 {
			exile = p.Exile
		}
	}
	if len(exile) != 3 {
		t.Fatalf("p0 exile = %v, want the three cards the Survival trigger exiled", exile)
	}
	for i := range s.Permanents {
		p := &s.Permanents[i]
		if p.Name == "Veteran Survivor" && p.Controller == 0 {
			if p.PT != "5/4" || !slices.Contains(p.Keywords, "Hexproof") {
				t.Fatalf("Veteran Survivor %s %v, want 5/4 hexproof", p.PT, p.Keywords)
			}
			return
		}
	}
	t.Fatal("precondition: Veteran Survivor is on p0's final battlefield")
}

// TestStaticExilePermRowsServed: the two may-play alternative-cost rows are
// served with the observation that proves the price -- Valgavoth's offered
// cast of the card its replacement exiled and the life it pays, Warped
// Space's elected {0} alternative on an exile cast.
func TestStaticExilePermRowsServed(t *testing.T) {
	for _, tc := range []struct{ card, key, probe, castMode string }{
		{"Valgavoth, Terror Eater", "static#0.0", "p1:Grizzly Bears", ""},
		{"Charred Foyer // Warped Space", "static#1.0", "p0:Misthollow Griffin", "alternative"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			it, skip := zoneItem(t, tc.card, tc.key)
			if skip != nil {
				t.Fatalf("GenerateB: %s", skip.Reason)
			}
			cast := -1
			for i, st := range it.Steps {
				if st.Op == "cast" && st.Card == tc.probe {
					cast = i
				}
			}
			if cast < 0 || it.Steps[cast].CastMode != tc.castMode {
				t.Fatalf("no cast of %s with cast_mode %q in %+v", tc.probe, tc.castMode, it.Steps)
			}
			res := runZone(t, it)
			if len(res.Fails) != 0 {
				t.Fatalf("gorge does not satisfy the scenario: %v", res.Fails)
			}
			final := res.Snapshots[len(res.Snapshots)-1]
			if !onBattlefieldRef(final, tc.probe) {
				t.Fatalf("%s did not resolve onto the battlefield", tc.probe)
			}
			// The price: Valgavoth's cast paid 2 life (Grizzly Bears' mana
			// value) and no mana; Warped Space's paid nothing at all.
			if want := map[string]int32{"p1:Grizzly Bears": 18, "p0:Misthollow Griffin": 20}[tc.probe]; final.Players[0].Life != want || final.Players[0].Pool != "" {
				t.Fatalf("p0 life %d pool %q after the cast, want %d and an empty pool", final.Players[0].Life, final.Players[0].Pool, want)
			}
		})
	}
}

// onBattlefieldRef reports whether the snapshot has a permanent with ref.
func onBattlefieldRef(s rules.OracleSnapshot, ref string) bool {
	for _, p := range s.Permanents {
		if p.Ref == ref {
			return true
		}
	}
	return false
}

// wantMayPlayAltCostCensus is the corpus-wide split of every Mode$
// Continuous MayPlay$ True static carrying MayPlayAltManaCost$ (measured at
// lb5-exileperm). It is a ratchet: served may only rise and each skip reason
// may only fall; lower a skip and raise served in the same commit.
//
// The five served rows: Valgavoth, Terror Eater and Warped Space (this
// class), Tlincalli Hunter, Dragon's Smile and Conspiracy Unraveler. The ten
// "not observable" rows are mostly hand-cast discounts (Darksteel Monolith's
// shape, priced by alternativeCosts but with no offer fixture here) and the
// library-top life-payment permissions (Bolas's Citadel's shape), which the
// engine now opens but no fixture observes yet.
var wantMayPlayAltCostCensus = map[string]int{
	"served": 5,
	"skip:static effect not observable on a probe or the card":                                       10,
	"skip:static graveyard probe is exiled by the card's own Moved replacement when setup places it": 1,
}

func TestMayPlayAltCostCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := map[string]int{}
	var served []string
	for _, c := range reg.AllCards() {
		for _, r := range levelb.Requirements(c) {
			if r.Family != "static" || r.Face >= len(c.Faces) {
				continue
			}
			slot, err := strconv.Atoi(r.Slot)
			if err != nil || slot >= len(c.Faces[r.Face].Statics) {
				continue
			}
			st := c.Faces[r.Face].Statics[slot]
			if st.Mode != "Continuous" || !st.HasParam(cards.PKMayPlayAltManaCost) {
				continue
			}
			switch {
			case r.Gap != "":
				got["gap:"+r.Gap]++
			default:
				if _, skip := templates.GenerateB(reg, c.Faces[0].Name, r); skip != nil {
					got["skip:"+skip.Reason]++
				} else {
					got["served"]++
					served = append(served, c.Faces[0].Name+" "+r.Key)
				}
			}
		}
	}
	sort.Strings(served)
	t.Logf("served: %v", served)
	for k, want := range wantMayPlayAltCostCensus {
		if k == "served" {
			if got[k] < want {
				t.Errorf("served = %d, want >= %d", got[k], want)
			}
			continue
		}
		if got[k] > want {
			t.Errorf("%s = %d, want <= %d", k, got[k], want)
		}
	}
	for k, n := range got {
		if _, ok := wantMayPlayAltCostCensus[k]; !ok && k != "served" && n > 0 {
			t.Errorf("new census bucket %q = %d", k, n)
		}
	}
}
