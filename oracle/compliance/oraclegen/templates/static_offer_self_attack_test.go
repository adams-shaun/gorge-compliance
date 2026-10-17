package templates_test

import "testing"

// The Lunar Whale (Vehicle) "As long as CARDNAME attacked this turn, you may
// play the top card of your library": the offer observation must crew the
// Whale, attack with it and reach main 2 before asserting the library top is
// offered. Without the self-attack prelude the gate reads false and the row
// was skipped as a gap (lb-setupparity, stale "predicate absent" reason).
func TestStaticOfferServesSelfAttackedThisTurnGate(t *testing.T) {
	it, skip := zoneItem(t, "The Lunar Whale", "static#0.1")
	if skip != nil {
		t.Fatalf("GenerateB skipped: %s", skip.Reason)
	}
	ops := map[string]int{}
	for _, st := range it.Steps {
		ops[st.Op]++
	}
	// Precondition for the assertion: the scenario really attacks with the
	// source, otherwise a served row proves nothing about the gate.
	if ops["attack"] != 1 || ops["activate"] != 1 {
		t.Fatalf("scenario ops = %v, want one crew activation and one attack", ops)
	}
	attack := -1
	for i, st := range it.Steps {
		if st.Op == "attack" {
			attack = i
			if len(st.Attackers) != 1 || st.Attackers[0] != "p0:The Lunar Whale" {
				t.Fatalf("attackers = %v, want the Whale", st.Attackers)
			}
		}
	}
	last := it.Steps[len(it.Steps)-1]
	if len(last.Expect) != 1 || last.Expect[0].Offered == nil || attack >= len(it.Steps)-1 {
		t.Fatalf("final step lacks an offered assertion after the attack: %+v", last)
	}
	if len(it.XAbility) != len(it.Steps) {
		t.Fatalf("XAbility has %d entries for %d steps", len(it.XAbility), len(it.Steps))
	}
	if res := runZone(t, it); len(res.Fails) != 0 {
		t.Fatalf("gorge does not satisfy the scenario: %v", res.Fails)
	}
}
