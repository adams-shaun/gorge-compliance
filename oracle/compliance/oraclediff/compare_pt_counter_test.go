package oraclediff

import "testing"

// Every power/toughness counter folds to one canonical P/M label, whichever
// vocabulary wrote it -- gorge's snapshot ("P1P0", "M0M1", mixed signs) or a
// follower's printed "+N/+M" -- so a Balduvian Hydra's +1/+0 counters compare
// equal on both sides and no future non-±1/±1 P/T counter can split the
// label again (CR 122.1a). Non-P/T counter kinds keep their own spelling.
func TestNormCounterPT(t *testing.T) {
	cases := []struct {
		gorge, follower, want string
	}{
		{"P1P0", "+1/+0", "P1P0"},      // Balduvian Hydra's X counters
		{"P0P1", "+0/+1", "P0P1"},      //
		{"P2P2", "+2/+2", "P2P2"},      // an X=2 pack of +1/+1s
		{"P1P1", "+1/+1", "P1P1"},      // the original +1/+1 fold
		{"M1M1", "-1/-1", "M1M1"},      // the original -1/-1 fold
		{"M0M1", "+0/-1", "P0M1"},      // Wall of Roots: a zero is P0 either way
		{"M1M0", "-1/+0", "M1P0"},      //
		{"P1M1", "+1/-1", "P1M1"},      // mixed signs are one vocabulary too
		{"p 1 p 0", " +1/+0 ", "P1P0"}, // case and spaces do not reach the label
		{"P01P00", "+1/+0", "P1P0"},    // digits are numbers, not strings
	}
	for _, c := range cases {
		if got := normCounter(c.gorge); got != c.want {
			t.Fatalf("normCounter(%q) = %q, want %q", c.gorge, got, c.want)
		}
		if got := normCounter(c.follower); got != c.want {
			t.Fatalf("normCounter(%q) = %q, want %q", c.follower, got, c.want)
		}
	}
	for _, c := range []struct{ kind, want string }{
		{"LORE", "LORE"},
		{"CHARGE", "CHARGE"},
		{"deathtouched", "DEATHTOUCHED"},
		{"lore counter", "LORECOUNTER"},
		{"PUPA", "PUPA"},
	} {
		if got := normCounter(c.kind); got != c.want {
			t.Fatalf("normCounter(%q) = %q, want %q (non-P/T kinds keep their spelling)", c.kind, got, c.want)
		}
	}
}
