package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
)

// TestAdjudicatePhaseColumn: -phase-diff adds where phase.rs stands to each
// classified report row (a card phase lacks is "unsupported", never a
// disagreement) and leaves the ledger and every three-way field as they were.
func TestAdjudicatePhaseColumn(t *testing.T) {
	f := newAdjFixture(t, []adjCase{
		{it: adjItem("fx-h", "FX Hand Card", "cast-resolve"), forge: fx21, xmage: x21, gf: gfDiverge()},
		{it: adjItem("agree-a", "Agree Card", "cast-resolve"), forge: fx20, xmage: x20, gf: oraclediff.Verdict{Status: oraclediff.Agree}},
		{it: adjItem("gx-a", "GX Agree Card", "cast-resolve"), forge: fx21, xmage: x20, gf: gfDiverge()},
	})
	f.adjCommitted(t)
	if err := runAdjudicate(f.args()); err != nil {
		t.Fatal(err)
	}
	ledgerBefore := patternByCard(t, f.ledger)
	before := readReports(t, f.report)

	// phase.rs: agrees with Forge and XMage on FX Hand Card (from its cache),
	// lacks Agree Card, and sides with gorge on GX Agree Card.
	pcache := oraclediff.Cache{Dir: filepath.Join(f.dir, "phase-cache")}
	if err := pcache.Put("preq-fx", *fx21); err != nil {
		t.Fatal(err)
	}
	if err := pcache.Put("preq-gx", *fx20); err != nil {
		t.Fatal(err)
	}
	pdiff := filepath.Join(f.dir, "phase-diff.jsonl")
	rows := []ForgeRow{
		{ID: "fx-h", Card: "FX Hand Card", Verdict: gfDiverge(), RequestSHA: "preq-fx", ScenarioSHA: f.shas["FX Hand Card"]},
		{ID: "gx-a", Card: "GX Agree Card", Verdict: oraclediff.Verdict{Status: oraclediff.Agree}, RequestSHA: "preq-gx", ScenarioSHA: f.shas["GX Agree Card"]},
	}
	var b []byte
	for _, r := range rows {
		j, _ := json.Marshal(r)
		b = append(append(b, j...), '\n')
	}
	if err := os.WriteFile(pdiff, b, 0o644); err != nil {
		t.Fatal(err)
	}
	prep := `{"id":"agree-a","reason":"phase_lacks_card","detail":"Agree Card: no face"}` + "\n"
	if err := os.WriteFile(filepath.Join(f.dir, "phase-prep.jsonl"), []byte(prep), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.ledger); err != nil {
		t.Fatal(err)
	}
	if err := runAdjudicate(f.args("-phase-diff", pdiff, "-phase-cache", pcache.Dir)); err != nil {
		t.Fatal(err)
	}
	after := readReports(t, f.report)
	if len(after) != len(before) {
		t.Fatalf("%d report rows with phase, %d without", len(after), len(before))
	}
	want := map[string][3]string{
		"FX Hand Card":  {"FPX|G", "gorge_suspect", oraclediff.PhaseWithOthers},
		"Agree Card":    {"GFX P?", "", oraclediff.PhaseUnsupported},
		"GX Agree Card": {"GPX|F", "forge_suspect", oraclediff.PhaseWithGorge},
	}
	for i, r := range after {
		three := r
		three.Four, three.FourLabel, three.Phase, three.GPField = "", "", "", ""
		if three != before[i] {
			t.Errorf("%s: three-way fields changed:\n%+v\n%+v", r.Card, three, before[i])
		}
		w, ok := want[r.Card]
		if !ok {
			continue
		}
		delete(want, r.Card)
		if r.Four != w[0] || r.FourLabel != w[1] || r.Phase != w[2] {
			t.Errorf("%s: got %q/%q/%s, want %v", r.Card, r.Four, r.FourLabel, r.Phase, w)
		}
	}
	if len(want) != 0 {
		t.Errorf("no report row for %v", want)
	}
	ledgerAfter := patternByCard(t, f.ledger)
	if len(ledgerAfter) != len(ledgerBefore) {
		t.Fatalf("ledger changed: %v vs %v", ledgerAfter, ledgerBefore)
	}
	for k, v := range ledgerBefore {
		if ledgerAfter[k] != v {
			t.Errorf("ledger %s: %s -> %s", k, v, ledgerAfter[k])
		}
	}
}
