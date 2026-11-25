package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// TestPassPlansDeclaredLevelBRowWithChangedScenario: the compliance pass
// (scripts/compliance-pass.sh) generates at the set's declared level, so a
// level-B verdict row whose scenario changed is planned for an XMage replay.
// The pass once generated level A only, so the BLB and MSH activate#0.1 rows
// the gate checks at level B stayed "verdict is for an older scenario" however
// often it ran.
func TestPassPlansDeclaredLevelBRowWithChangedScenario(t *testing.T) {
	dir := filepath.Join("..", "..", ".cards")
	tmp := t.TempDir()
	mdir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(mdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "declared.json"), []byte(`{"TINY": "B"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(compliance.Manifest{Code: "TINY", Cards: []compliance.ManifestCard{{Name: "Prodigal Sorcerer"}}})
	if err != nil {
		t.Fatal(err)
	}
	mf := filepath.Join(mdir, "TINY.json")
	if err := os.WriteFile(mf, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	scen := filepath.Join(tmp, "scen.jsonl")
	if err := runGen(dir, mf, scen, "declared", ""); err != nil {
		t.Fatal(err)
	}
	var b *oraclegen.Item
	for _, line := range readLinesBytes(t, scen) {
		var it oraclegen.Item
		if err := json.Unmarshal(line, &it); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(it.Template, "#") {
			b = &it
		}
	}
	if b == nil {
		t.Fatal("gen -level declared on a set declared B wrote no level-B scenario")
	}

	// A committed row for that scenario, current in every way but its scenario.
	const ref, driver = "ref1", "aaaaaaaaaaaa"
	vdir := filepath.Join(tmp, "verdicts")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	row := compliance.VerdictRow{Card: b.Card, Template: b.Template, ID: b.ID, ScenarioSHA: "older",
		XMageRef: ref, XMageDriver: driver, Status: compliance.StatusAgree}
	if gate.ItemSHA(*b) == row.ScenarioSHA {
		t.Fatal("fixture row already has the current scenario sha")
	}
	if err := compliance.MergeVerdicts(vdir, []compliance.VerdictRow{row}); err != nil {
		t.Fatal(err)
	}
	replay, rediff := filepath.Join(tmp, "replay.jsonl"), filepath.Join(tmp, "rediff.jsonl")
	if err := runPlan(dir, scen, vdir, ref, driver, filepath.Join(tmp, "cache"), replay, rediff); err != nil {
		t.Fatal(err)
	}
	planned := false
	for _, line := range readLinesBytes(t, replay) {
		var it oraclegen.Item
		if err := json.Unmarshal(line, &it); err != nil {
			t.Fatal(err)
		}
		if it.ID == b.ID {
			planned = true
		}
	}
	if !planned {
		t.Fatalf("%s: the changed level-B scenario was not planned for replay", b.ID)
	}
}

func TestResolveLevel(t *testing.T) {
	p := filepath.Join(t.TempDir(), "declared.json")
	if err := os.WriteFile(p, []byte(`{"BLB": "B", "SPM": "A"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ level, set, want string }{
		{"declared", "BLB", "B"}, {"declared", "SPM", "A"}, {"declared", "ZZZ", "A"}, {"B", "SPM", "B"}, {"A", "BLB", "A"},
	} {
		got, err := resolveLevel(c.level, p, c.set)
		if err != nil || got != c.want {
			t.Errorf("resolveLevel(%q, %q) = %q, %v; want %q", c.level, c.set, got, err, c.want)
		}
	}
}
