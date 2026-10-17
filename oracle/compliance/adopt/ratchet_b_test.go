package adopt

import (
	"strings"
	"testing"
)

// TestRatchetAWriteKeepsOutstandingB is the carry-forward rule: a level-A
// -write-ratchet rebuilds the level-A entries from the run, so it must keep
// each entry's existing level-B floor instead of dropping it.
func TestRatchetAWriteKeepsOutstandingB(t *testing.T) {
	floor := 384
	prev := map[string]RatchetEntry{
		"FRA": {Level: "A", Outstanding: 0, OutstandingB: &floor},
	}
	sets := []SetStatus{{Set: "FRA", Declared: "A", Outstanding: 0}}
	got := RatchetOf(sets, prev)
	e, ok := got["FRA"]
	if !ok {
		t.Fatal("RatchetOf dropped FRA")
	}
	if e.OutstandingB == nil || *e.OutstandingB != floor {
		t.Fatalf("A write dropped outstanding_b: %+v, want floor %d", e.OutstandingB, floor)
	}
	if e.Outstanding != 0 || e.Level != "A" {
		t.Fatalf("A write did not record the level-A fields: %+v", e)
	}
}

// TestCheckRatchetBFloors is the B ratchet's two directions: a measured count
// above its floor fails, one below it is slack, and a set with no B floor is
// not part of the claim.
func TestCheckRatchetBFloors(t *testing.T) {
	floor := 40
	ratchet := map[string]RatchetEntry{
		"FRA": {OutstandingB: &floor},
		"FDN": {}, // never measured at B
	}
	fails, slack := CheckRatchetB([]SetStatus{{Set: "FRA", Outstanding: 41}, {Set: "FDN", Outstanding: 999}}, ratchet)
	if len(fails) != 1 || !strings.Contains(fails[0], "FRA: 41 outstanding at level B, ratchet 40") {
		t.Errorf("growth: fails = %v, want the FRA 41 > 40 failure", fails)
	}
	if len(slack) != 0 {
		t.Errorf("growth: slack = %v, want none", slack)
	}
	fails, slack = CheckRatchetB([]SetStatus{{Set: "FRA", Outstanding: 39}}, ratchet)
	if len(fails) != 0 {
		t.Errorf("improvement: fails = %v, want none", fails)
	}
	if len(slack) != 1 || !strings.Contains(slack[0], "FRA: 40 -> 39") {
		t.Errorf("improvement: slack = %v, want the FRA 40 -> 39 entry", slack)
	}
}
