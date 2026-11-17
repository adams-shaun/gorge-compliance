package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecideStatusReplayDiff(t *testing.T) {
	for _, c := range []struct {
		name   string
		fails  []string
		diff   string
		status string
	}{
		{"clean", nil, "", "pass"},
		{"expect fail", []string{"step 1: life 20 want 19"}, "", "fail-expect"},
		{"replay diff only", nil, "obj[3]: CrewedVehicles:[] vs CrewedVehicles:[]", "fail-replay"},
		{"replay diff and expect fail", []string{"step 1: life 20 want 19"}, "x", "fail-replay"},
		{"harness outranks diff", []string{"harness: answer not offered"}, "x", "fail-harness"},
	} {
		if got := decideStatus(c.fails, c.diff); got != c.status {
			t.Errorf("%s: decideStatus = %q, want %q", c.name, got, c.status)
		}
	}
}

// TestWriteCommitSkipsReplayDiff: a scenario whose log replay differs is not
// written by -commit, even when it passes its expectations or carries a
// triaged known_bug row; a clean scenario beside it still is.
func TestWriteCommitSkipsReplayDiff(t *testing.T) {
	dir := t.TempDir()
	old := knownBugs
	knownBugs = map[string]string{}
	t.Cleanup(func() { knownBugs = old })
	mk := func(method, diff string, known bool) *result {
		sc := &portScenario{Name: method, XMage: "FooTest." + method}
		if known {
			knownBugs[sc.XMage] = "synthetic"
		}
		return &result{Family: "single", Class: "FooTest", Method: method, Rel: "x/FooTest.java",
			Scenario: sc, ReplayDiff: diff, Status: decideStatus(nil, diff)}
	}
	good, bad, badKnown := mk("good", "", false), mk("bad", "differs", false), mk("badKnown", "differs", true)
	if bad.Status != "fail-replay" {
		t.Fatalf("status = %q, want fail-replay", bad.Status)
	}
	if err := writeCommit(dir, []*result{good, bad, badKnown}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "single", "FooTest.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !contains(s, "FooTest.good") || contains(s, "FooTest.bad") {
		t.Errorf("committed file wrong:\n%s", s)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
