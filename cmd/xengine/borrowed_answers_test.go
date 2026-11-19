package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// decisionsAt is the decisions a result logged on one scenario step.
func decisionsAt(g rules.OracleResult, step int) []rules.OracleDecision {
	var out []rules.OracleDecision
	for _, d := range g.Decisions {
		if d.Step == step {
			out = append(out, d)
		}
	}
	return out
}

// TestReproInputKeepsBorrowedAnswers pins the repro cut of a divergence at a
// pass whose resolution answers asks from the next step. The fixture is the
// xe:RAND:2:7:w16 window (Wakanda Forever!): the step 9 pass resolves the spell
// and poses both library-search asks, and gorge's settleResolutionAsk
// (CR 608.2c) answers them from the step 10 activate's scripted picks. A cut
// at step 9 drops that step, so the replay takes the harness fallback and logs
// empty choose_n decisions; the repro must keep the answer-carrying step(s)
// so its input replays to the same gorge result.
func TestReproInputKeepsBorrowedAnswers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "borrowed_answers_item.json"))
	if err != nil {
		t.Fatal(err)
	}
	var full oraclegen.Item
	if err := json.Unmarshal(raw, &full); err != nil {
		t.Fatal(err)
	}
	const pass = 9
	if len(full.Steps) <= pass+1 || full.Steps[pass].Op != "pass" || len(full.Steps[pass+1].Answers) == 0 {
		t.Fatalf("fixture is not the borrowed-answer shape: %d steps, step %d op %q, step %d answers %d",
			len(full.Steps), pass, full.Steps[pass].Op, pass+1, len(full.Steps[pass+1].Answers))
	}
	gFull, err := rules.RunOracleScenarioJSON(reg, full.Raw())
	if err != nil {
		t.Fatalf("full item: %v", err)
	}
	want := decisionsAt(gFull, pass)
	if len(want) == 0 {
		t.Fatalf("full item logged no decisions at the diverging pass: %+v", gFull.Decisions)
	}
	f := classify.Finding{
		ID: full.ID, Engine: "forge", Class: classify.ClassDiverge, Step: pass,
		Checkpoint: gFull.Snapshots[pass+1].Checkpoint, Field: "p0.library_top",
	}
	dir := t.TempDir()
	if err := classify.WriteReproFrom(dir, f, lower.Lowered{Item: full}, gFull, oraclediff.XResult{}, classify.Origin{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "input.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var cut oraclegen.Item
	if err := json.Unmarshal(bytes.TrimSpace(b), &cut); err != nil {
		t.Fatalf("repro input.jsonl: %v", err)
	}
	if len(cut.Steps) <= pass+1 || len(cut.Steps[pass+1].Answers) == 0 {
		t.Fatalf("repro cut has %d steps: the pass's borrowed answers are gone", len(cut.Steps))
	}
	gCut, err := rules.RunOracleScenarioJSON(reg, cut.Raw())
	if err != nil {
		t.Fatalf("repro input: %v", err)
	}
	if got := decisionsAt(gCut, pass); !reflect.DeepEqual(got, want) {
		t.Fatalf("repro decisions at step %d = %+v, want the full item's %+v", pass, got, want)
	}
	n := min(len(gFull.Snapshots), len(gCut.Snapshots))
	if !reflect.DeepEqual(gFull.Snapshots[:n], gCut.Snapshots[:n]) {
		t.Fatalf("repro checkpoints differ from the full item's in the first %d", n)
	}
	// The cut keeps every answer-carrying step, not just the first: a kept
	// pass can itself borrow from a still later answer step.
	synthetic := oraclegen.Item{Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{
		{Op: "pass"}, {Op: "pass"},
		{Op: "activate", Answers: []oraclegen.Answer{{Kind: "choose", Pick: []string{"p0:Mountain"}}}},
		{Op: "pass"},
		{Op: "cast", Answers: []oraclegen.Answer{{Kind: "choose", Pick: []string{"p0:Forest"}}}},
		{Op: "pass"},
	}}}
	if got := len(classify.Truncate(synthetic, classify.Finding{Checkpoint: "step 0 (pass)", Step: 0}).Steps); got != 5 {
		t.Fatalf("cut of an item with answer steps at 2 and 4 has %d steps, want 5", got)
	}
}
