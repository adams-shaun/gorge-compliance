package oraclegen

import (
	"os"
	"strings"
	"testing"
)

// XMage is not part of go test, so pin the driver source: the setup counters
// are seeded by placeSetupCounters from the pre-start hook, after the
// cheat() placements and BEFORE start() runs the first state-based-action
// check, so a would-die placement (a printed 0/0, an X-cost creature's X=0
// board) survives with its counters, as gorge emits them with the placement.
// add() only places the card; it must not re-apply the counters.
func TestDriverAppliesSetupCountersWithPlacement(t *testing.T) {
	source, err := os.ReadFile("../../xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)

	// Both game constructions (TwoPlayerDuel and FreeForAll) hook start():
	// placeSetupCounters must run before super.start in each.
	if n := strings.Count(java, "placeSetupCounters(this);"); n != 2 {
		t.Fatalf("expected placeSetupCounters(this) in both start() overrides, found %d", n)
	}
	rest := java
	for seen := 0; seen < 2; seen++ {
		hookAt := strings.Index(rest, "placeSetupCounters(this);")
		superAt := strings.Index(rest[hookAt:], "super.start(choosingPlayerId);")
		if hookAt < 0 || superAt < 0 {
			t.Fatal("a start() override is missing super.start after placeSetupCounters")
		}
		between := rest[hookAt : hookAt+superAt]
		if strings.Contains(between, "public void start(") {
			t.Fatalf("placeSetupCounters must run before super.start in the same override:\n%s", between)
		}
		rest = rest[hookAt+superAt:]
	}

	// placeSetupCounters runs once per game and build() resets that.
	place := javaFunc(t, java, "private void placeSetupCounters(Game g) {")
	if !strings.Contains(place, "if (setupCountersPlaced) {") || !strings.Contains(place, "setupCountersPlaced = true;") {
		t.Fatalf("placeSetupCounters must run once per game; helper:\n%s", place)
	}
	build := javaFunc(t, java, "private void build(JsonObject sc) {")
	if !strings.Contains(build, "setupCountersPlaced = false;") {
		t.Fatal("build() must reset setupCountersPlaced")
	}

	// add() only places the card: no counters, no enter-with-counters map.
	addFn := javaFunc(t, java, "private int add(JsonObject s, String key, Zone zone, TestPlayer p) {")
	for _, bad := range []string{"counters.get(", "setEnterWithCounters", "enterWithCountersApplied", "addCounters(", "putSetupCounters("} {
		if strings.Contains(addFn, bad) {
			t.Fatalf("add() must not apply setup counters (found %s); add():\n%s", bad, addFn)
		}
	}
	// The placement bookkeeping the entry-history normaliser relies on stays.
	for _, want := range []string{"stageBackFace(p, n);", "setupBattlefield.merge"} {
		if !strings.Contains(addFn, want) {
			t.Fatalf("add() lost %s; add():\n%s", want, addFn)
		}
	}
}
