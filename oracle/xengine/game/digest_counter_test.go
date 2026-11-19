package game

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// TestDigestCountersLiveMatchSnapshot: a permanent's counter must have one
// digest key whichever builder made the digest. gorge stores a "-0/-1"
// counter as "M0M1" while the live digest canonicalises every P/T kind to
// "P0M1" (internal/ptcounter); if DigestOfSnapshot copies the snapshot key
// verbatim, lower.SelfCheck compares "field[i].counters.M0M1: 1" with
// "field[i].counters.P0M1: <absent>" and drops the item as a phantom
// lower_gap. counterMap is the live path DigestOfGame uses.
func TestDigestCountersLiveMatchSnapshot(t *testing.T) {
	cases := []struct {
		kinds []string
		want  map[string]int32
	}{
		{[]string{"M0M1"}, map[string]int32{"P0M1": 1}},
		{[]string{"M1M0"}, map[string]int32{"M1P0": 1}},
		{[]string{"P1P1"}, map[string]int32{"P1P1": 1}},
		{[]string{"M0M1", "P0M1"}, map[string]int32{"P0M1": 2}}, // one counter, two spellings
		{[]string{"M0M1", "M0M1"}, map[string]int32{"P0M1": 2}},
	}
	for _, c := range cases {
		var live []state.Counter
		snapCounters := map[string]int32{}
		for _, kind := range c.kinds {
			live = append(live, state.Counter{Kind: kind, N: 1})
			snapCounters[kind]++
		}
		if got := counterMap(live); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("counterMap(%v) = %v, want %v", c.kinds, got, c.want)
		}
		snap := DigestOfSnapshot(rules.OracleSnapshot{Permanents: []rules.OracleSnapPerm{{
			Ref: "p0:Wall of Roots", Name: "Wall of Roots", Controller: 0, Owner: 0,
			Counters: snapCounters,
		}}})
		if len(snap.Field) != 1 {
			t.Fatalf("%v: snapshot digest has %d permanents", c.kinds, len(snap.Field))
		}
		if got := snap.Field[0].Counters; !reflect.DeepEqual(got, c.want) {
			t.Fatalf("snapshot counters for %v = %v, want %v", c.kinds, got, c.want)
		}
		if liveMap, snapMap := counterMap(live), snap.Field[0].Counters; !reflect.DeepEqual(liveMap, snapMap) {
			t.Fatalf("%v: live digest %v, snapshot digest %v", c.kinds, liveMap, snapMap)
		}
	}
}
