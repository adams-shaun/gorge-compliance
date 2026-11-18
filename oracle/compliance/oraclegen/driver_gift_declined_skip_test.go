package oraclegen_test

import (
	"os"
	"strings"
	"testing"
)

// TestDriverGiftSpellSkipOnlyWhenPromised pins the driver's gift-spell cast
// branch: the trailing target_skip closes the gift-gated second target object
// only when the gift was promised. A declined gift has no such object, and a
// queued skip is left unused ("Targets ... Count are not equal (found
// [[target_skip]])").
func TestDriverGiftSpellSkipOnlyWhenPromised(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	cast, ok := caseBody(string(b), "cast")
	if !ok {
		t.Fatal("ScenarioReplay.java has no cast case")
	}
	if !strings.Contains(cast, `giftPromisedCast = castCostPicks.contains("gift_promise");`) {
		t.Fatal("cast arm no longer derives giftPromisedCast from the step's cost picks")
	}
	i := strings.Index(cast, "addTarget(p, seat(seatOf(tg.get(0))));\n")
	if i < 0 {
		t.Fatal("gift-spell branch (queue the player target) not found")
	}
	branch := cast[i:]
	if j := strings.Index(branch, "castObjectSpell(turn, phase, p, castObject);"); j >= 0 {
		branch = branch[:j]
	}
	k := strings.Index(branch, "if (giftPromisedCast)")
	s := strings.Index(branch, "TestPlayer.TARGET_SKIP")
	if k < 0 || s < 0 || s < k {
		t.Errorf("gift-spell branch queues its target_skip outside an `if (giftPromisedCast)` guard:\n%s", branch)
	}
}
