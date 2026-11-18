package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
)

func graveyardOrderSnap(checkpoint string, gy []string) rules.OracleSnapshot {
	return rules.OracleSnapshot{
		Checkpoint: checkpoint, Turn: 1, Step: "main1",
		Players: []rules.OracleSnapPlayer{
			{Seat: 0, Life: 20, Hand: []string{}, LibraryCount: 30},
			{Seat: 1, Life: 20, Hand: []string{}, Graveyard: gy, LibraryCount: 30},
		},
	}
}

// graveyardOrderClass runs diff over a two-pass window whose p1 graveyard is
// before after step 0 and gorge / other after step 1, and returns the class.
func graveyardOrderClass(t *testing.T, before, gorgeGY, otherGY []string) string {
	t.Helper()
	const id = "xe:T:0:0:w1"
	playDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(playDir, "tapes"), 0o755); err != nil {
		t.Fatal(err)
	}
	snaps := func(last []string) []rules.OracleSnapshot {
		return []rules.OracleSnapshot{
			graveyardOrderSnap("setup", nil),
			graveyardOrderSnap("step 0 (pass)", before),
			graveyardOrderSnap("step 1 (pass)", last),
		}
	}
	write := func(path string, v any) {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	it := oraclegen.Item{ID: id, Template: lower.Template, Scenario: oraclegen.Scenario{
		Steps: []oraclegen.Step{{Op: "pass"}, {Op: "pass"}},
	}}
	write(filepath.Join(playDir, "items.jsonl"), it)
	write(filepath.Join(playDir, "xitems.jsonl"), it)
	write(filepath.Join(playDir, "gorge.jsonl"), gorgeRow{ID: id, Result: rules.OracleResult{Snapshots: snaps(gorgeGY)}})
	write(filepath.Join(playDir, "lower.jsonl"), lowerRow{
		ID: id, Tape: "tapes/w.json", Status: lower.StatusOK,
		StepEntry: []int{0, 1}, Steps: 2, FirstEntry: 0,
	})
	write(filepath.Join(playDir, "tapes", "w.json"), game.Tape{
		Format:  game.TapeFormat,
		Decks:   []game.Deck{{Cards: []string{"Forest"}}, {Cards: []string{"Forest"}}},
		Entries: []game.Entry{{Seq: 0, Kind: "priority"}, {Seq: 1, Kind: "priority"}},
	})
	rowsPath := filepath.Join(playDir, "rows.jsonl")
	type engineRow struct {
		oraclediff.XResult
		Engine string `json:"engine"`
	}
	write(rowsPath, engineRow{XResult: oraclediff.XResult{ID: id, Name: id, Snapshots: snaps(otherGY)}, Engine: "xmage"})
	out := filepath.Join(t.TempDir(), "out")
	if err := diff(nil, playDir, "xmage", rowsPath, out, ""); err != nil {
		t.Fatal(err)
	}
	return verdictClasses(t, filepath.Join(out, "verdicts.jsonl"))[id]
}

// TestDiffGraveyardOrderSimultaneous: two cards that entered a graveyard in the
// same step swap places and compare equal (CR 404.3); cards that entered in
// different steps and swap places still differ.
func TestDiffGraveyardOrderSimultaneous(t *testing.T) {
	a, b := "Akal Pakal, First Among Equals", "Poison Dart Frog"
	if got := graveyardOrderClass(t, nil, []string{a, b}, []string{b, a}); got != classify.ClassHarness {
		t.Fatalf("same-step arrivals swapped: class %q, want %q", got, classify.ClassHarness)
	}
	if got := graveyardOrderClass(t, []string{a}, []string{a, b}, []string{b, a}); got != classify.ClassDiverge {
		t.Fatalf("different-step arrivals swapped: class %q, want %q", got, classify.ClassDiverge)
	}
}
