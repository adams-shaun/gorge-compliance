package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/engine/rules"
)

// TestForgeVerdictRowsWriteOnlyAgreeAtLevelA: only a level-A scenario Forge
// agrees on earns a row; a level-B template, a Forge disagreement and a
// scenario Forge never answered are counted and dropped.
func TestForgeVerdictRowsWriteOnlyAgreeAtLevelA(t *testing.T) {
	reg, err := loadReg(filepath.Join("..", "..", ".cards"))
	if err != nil {
		t.Fatal(err)
	}
	item := func(name string) oraclegen.Item {
		it, skip := templates.Generate(reg, name)
		if skip != nil {
			t.Fatalf("%s: %s", name, skip.Reason)
		}
		return it
	}
	shock := item("Shock")
	bolt := item("Lightning Bolt")
	giant := item("Giant Growth")
	activated := shock
	activated.Template = "activate#0"

	var lines [][]byte
	for _, it := range []oraclegen.Item{shock, bolt, giant, activated} {
		b, _ := json.Marshal(it)
		lines = append(lines, b)
	}
	scenPath := filepath.Join(t.TempDir(), "scen.jsonl")
	writeLines(t, scenPath, lines...)

	ref := strings.Repeat("ab", 20)
	driver := strings.Repeat("cd", 20)
	frows := map[string]ForgeRow{
		gate.ItemSHA(shock):     {ScenarioSHA: gate.ItemSHA(shock), Verdict: oraclediff.Verdict{Status: oraclediff.Agree}},
		gate.ItemSHA(bolt):      {ScenarioSHA: gate.ItemSHA(bolt), Verdict: oraclediff.Verdict{Status: oraclediff.Diverge, Field: "p0.life"}},
		gate.ItemSHA(activated): {ScenarioSHA: gate.ItemSHA(activated), Verdict: oraclediff.Verdict{Status: oraclediff.Agree}},
	}
	rows, counts, err := forgeVerdictRows(reg, scenPath, frows, ref, driver)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Card != "Shock" || r.Template != "cast-resolve" {
		t.Fatalf("row = %+v, want Shock's cast-resolve", r)
	}
	if r.Source != compliance.SourceForge || r.Status != compliance.StatusAgree {
		t.Errorf("source/status = %q/%q, want forge/agree", r.Source, r.Status)
	}
	if r.ForgeRef != ref || r.ForgeDriver != short12(driver) {
		t.Errorf("ref/driver = %q/%q, want %q/%q", r.ForgeRef, r.ForgeDriver, ref, short12(driver))
	}
	if r.Detail != "" || r.XMageRef != "" {
		t.Errorf("row carries detail/xmage_ref: %+v", r)
	}
	g, err := rules.RunOracleScenarioJSON(reg, shock.Raw())
	if err != nil {
		t.Fatal(err)
	}
	want := oraclediff.FreezeOpts(g, shock.Compare, forgeIgnore(shock)...)
	if !reflect.DeepEqual(r.Frozen, want) {
		t.Errorf("frozen = %+v, want gorge's own %+v", r.Frozen, want)
	}
	for key, want := range map[string]int{
		"written": 1, "forge-diff DIVERGE": 1, "level-B template": 1, "no forge-diff row": 1,
	} {
		if counts[key] != want {
			t.Errorf("counts[%q] = %d, want %d (all: %v)", key, counts[key], want, counts)
		}
	}
}

// TestForgeVerdictRowsCarryNoForgeOutput: forge-diff's own message and
// snapshot values never reach a verdict row.
func TestForgeVerdictRowsCarryNoForgeOutput(t *testing.T) {
	reg, err := loadReg(filepath.Join("..", "..", ".cards"))
	if err != nil {
		t.Fatal(err)
	}
	it, skip := templates.Generate(reg, "Shock")
	if skip != nil {
		t.Fatal(skip.Reason)
	}
	sha := gate.ItemSHA(it)
	b, _ := json.Marshal(it)
	scenPath := filepath.Join(t.TempDir(), "scen.jsonl")
	writeLines(t, scenPath, b)
	frows := map[string]ForgeRow{sha: {
		ScenarioSHA: sha,
		Verdict: oraclediff.Verdict{
			Status: oraclediff.Agree,
			Msg:    "FORGE-SENTINEL", XMage: "FORGE-SENTINEL", Gorge: "FORGE-SENTINEL",
		},
	}}
	rows, _, err := forgeVerdictRows(reg, scenPath, frows, strings.Repeat("ab", 20), strings.Repeat("cd", 20))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	for _, r := range rows {
		out, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), "FORGE-SENTINEL") {
			t.Errorf("row carries Forge output: %s", out)
		}
	}
}
