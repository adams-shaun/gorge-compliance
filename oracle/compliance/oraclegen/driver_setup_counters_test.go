package oraclegen

import (
	"os"
	"strings"
	"testing"
)

// javaFunc returns the body of the 4-space-indented ScenarioReplay.java method
// whose signature line starts with sig.
func javaFunc(t *testing.T, java, sig string) string {
	t.Helper()
	start := strings.Index(java, sig)
	if start < 0 {
		t.Fatalf("ScenarioReplay.java has no %q", sig)
	}
	end := strings.Index(java[start:], "\n    }")
	if end < 0 {
		t.Fatalf("%q is unterminated", sig)
	}
	return java[start : start+end]
}

// XMage is not part of go test, so pin the driver source. Setup counters are
// recorded state: placeSetupCounters writes them straight onto the permanents
// (putSetupCounters, no ADD_COUNTERS event) before start(), and the setup
// snapshot callback's applySetupState no longer adds counters, it only does
// the tapped/speed state. The executable XMage-side test covers the result.
func TestDriverReappliesSetupCountersBeforeSnapshot(t *testing.T) {
	source, err := os.ReadFile("../../xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)

	// The setup callback still runs applySetupState, then aliases, then the
	// snapshot.
	callbackStart := strings.Index(java, `runCode("setup", TURN, MAIN, playerA, (info, p, g) -> {`)
	if callbackStart < 0 {
		t.Fatal("ScenarioReplay.java has no setup snapshot callback")
	}
	callbackEnd := strings.Index(java[callbackStart:], "            });")
	if callbackEnd < 0 {
		t.Fatal("setup snapshot callback is unterminated")
	}
	callback := java[callbackStart : callbackStart+callbackEnd]
	stateAt := strings.Index(callback, "applySetupState(g);")
	aliasAt := strings.Index(callback, "registerAliases(g);")
	snapshotAt := strings.Index(callback, "snaps.add(snapshot(info, g));")
	if stateAt < 0 || aliasAt < 0 || snapshotAt < 0 || !(stateAt < aliasAt && aliasAt < snapshotAt) {
		t.Fatalf("setup must apply state before aliases and snapshot; callback:\n%s", callback)
	}

	// applySetupState must not seed counters (placeSetupCounters did, before
	// start()).
	helper := javaFunc(t, java, "private void applySetupState(Game g) {")
	for _, bad := range []string{"addCounters(", "putSetupCounters(", `"counters"`} {
		if strings.Contains(helper, bad) {
			t.Fatalf("applySetupState must not add setup counters (found %s); helper:\n%s", bad, helper)
		}
	}

	// placeSetupCounters writes recorded state (no event), then recomputes
	// effects so counter-gated statics see the counters.
	place := javaFunc(t, java, "private void placeSetupCounters(Game g) {")
	putAt := strings.Index(place, "putSetupCounters(g, perm,")
	applyAt := strings.Index(place, "g.applyEffects();")
	if putAt < 0 || applyAt < 0 || putAt >= applyAt {
		t.Fatalf("placeSetupCounters must put the counters and then apply effects; helper:\n%s", place)
	}
	if strings.Contains(place, "addCounters(") {
		t.Fatalf("placeSetupCounters must not fire ADD_COUNTERS via addCounters; helper:\n%s", place)
	}
	if !strings.Contains(place, "counters name ") {
		t.Fatalf("placeSetupCounters must keep the loud unknown-name throw; helper:\n%s", place)
	}
}
