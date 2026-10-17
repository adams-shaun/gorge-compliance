package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestCompareEndedGameComparesOnlyTheEnd: once both engines report the game
// over (CR 104.2a), the zone residue they leave differs by implementation
// (CR 800.4a's sweep is a multiplayer rule), so only the header and life
// totals are compared; a still-running side or a life difference diverges.
func TestCompareEndedGameComparesOnlyTheEnd(t *testing.T) {
	ended := func(s rules.OracleSnapshot) rules.OracleSnapshot {
		s.Over = true
		s.Players[0].Life = 18
		return s
	}
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{snap("setup"), ended(snap("step 1 (resolve)"))}}
	g.Snapshots[1].Permanents = nil
	g.Snapshots[1].Players[0].LibraryCount = 0
	x := XResult{Snapshots: []rules.OracleSnapshot{xsnap("setup"), ended(xsnap("step 1 (resolve)"))}}
	x.Snapshots[1].Players[0].Graveyard = []string{"Shock"}
	if v := Compare(g, nil, x); v.Status != Agree {
		t.Fatalf("both ended: %+v", v)
	}

	x.Snapshots[1].Players[1].Life = 0
	if v := Compare(g, nil, x); v.Status != Diverge || v.Field != "p1.life" {
		t.Fatalf("an ended game's life totals must still be compared: %+v", v)
	}

	x.Snapshots[1] = xsnap("step 1 (resolve)")
	if v := Compare(g, nil, x); v.Status != Diverge || v.Field != "over" {
		t.Fatalf("one side still running must diverge on over: %+v", v)
	}
}
