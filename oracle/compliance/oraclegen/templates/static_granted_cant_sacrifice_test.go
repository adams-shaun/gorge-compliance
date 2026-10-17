package templates

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
)

// TestGrantedCantSacrificeServed: Zurgo, Thunder's Decree's end-step grant
// "This token can't be sacrificed" (CR 701.21a) is served by its own Mobilize
// tokens: the delayed sacrifice trigger (CR 702.181a) is on the stack at the
// end step and the Warrior tokens survive it.
func TestGrantedCantSacrificeServed(t *testing.T) {
	reg := loadGenRegistry(t)
	const card = "Zurgo, Thunder's Decree"
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("%s not in the corpus", card)
	}
	var req levelb.Requirement
	for _, r := range levelb.Requirements(c) {
		if r.Key == "static#0.0" {
			req = r
		}
	}
	if req.Key == "" {
		t.Fatalf("precondition: %s has no static#0.0 requirement", card)
	}
	it, skip := GenerateB(reg, card, req)
	if skip != nil {
		t.Fatalf("skipped: %s", skip.Reason)
	}
	res := runItemScenario(t, reg, it, true)
	last := res.Snapshots[len(res.Snapshots)-1]
	if last.Step != "end" || len(last.Stack) != 0 {
		t.Fatalf("last checkpoint %s with stack %v, want the end step with the sacrifice trigger resolved", last.Step, last.Stack)
	}
	if n := ownTokens(last); n != 2 {
		t.Fatalf("%d Warrior tokens after the sacrifice trigger resolved, want 2", n)
	}
}
