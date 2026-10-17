package oraclediff

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

func lostSnap(seats int, lost int) rules.OracleSnapshot {
	s := rules.OracleSnapshot{Checkpoint: "step 0 (resolve)"}
	for i := 0; i < seats; i++ {
		s.Players = append(s.Players, rules.OracleSnapPlayer{Seat: i, Life: 20, Lost: i == lost})
	}
	return s
}

// TestLostComparedOnlyInMultiplayer: a seat's "lost the game" is a compared
// field only in a game of three or more seats, the only kind that goes on
// past a loss (CR 800.4a). A two-seat snapshot renders no such field, so
// every two-seat canonical form, and so every frozen hash, is unchanged.
func TestLostComparedOnlyInMultiplayer(t *testing.T) {
	if c := Canonical([]rules.OracleSnapshot{lostSnap(2, 1)}); strings.Contains(c, "lost") {
		t.Fatalf("two-seat canonical form carries lost:\n%s", c)
	}
	g, x := lostSnap(3, 1), lostSnap(3, -1)
	v, ok := compareSnap(g, x, nil, nil)
	if ok || v.Field != "p1.lost" {
		t.Fatalf("three-seat lost mismatch = %+v (ok %v), want a p1.lost divergence", v, ok)
	}
	if _, ok := compareSnap(g, lostSnap(3, 1), nil, nil); !ok {
		t.Fatal("equal three-seat snapshots diverge")
	}
}
