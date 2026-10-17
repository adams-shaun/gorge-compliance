package oraclegen

import "testing"

// TestDonatedCandidatesServeOwnButDontControl: a "permanent you own but don't
// control" filter is served only by the donate prelude (setup places every
// card under its owner's control), whose one target is the opponent; a base
// the creature probe cannot satisfy fails closed, and a filter without both
// words keeps the ordinary candidates.
func TestDonatedCandidatesServeOwnButDontControl(t *testing.T) {
	for _, tc := range []struct {
		filter string
		want   bool
	}{
		{"Permanent.YouOwn+YouDontCtrl", true},
		{"Creature.YouOwn+YouDontCtrl", true},
		{"Land.YouOwn+YouDontCtrl", false},
		{"Enchantment.YouOwn+YouDontCtrl", false},
	} {
		cs := candidatesFor(nil, tc.filter, "")
		if !tc.want {
			if len(cs) != 0 {
				t.Errorf("%s: candidates %+v, want none (fail closed)", tc.filter, cs)
			}
			continue
		}
		if len(cs) != 1 || cs[0].card != donatedProbe || cs[0].seat != "p0" {
			t.Fatalf("%s: candidates %+v, want the p0 %s", tc.filter, cs, donatedProbe)
		}
		pre := cs[0].pre
		if len(pre) != 2 || pre[0].Op != "activate" || pre[0].Card != "p0:"+donatedProbe ||
			len(pre[0].Targets) != 1 || pre[0].Targets[0] != "p1" || pre[1].Op != "resolve" {
			t.Fatalf("%s: prelude %+v, want the probe's one-target (opponent) activation and a resolve", tc.filter, pre)
		}
	}
	for _, c := range candidatesFor(nil, "Permanent.YouOwn", "") {
		if c.card == donatedProbe {
			t.Fatalf("Permanent.YouOwn served by the donate prelude: %+v", c)
		}
	}
}
