package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/dside"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// writeSmallManifest writes dir/<code>.json with the real set's metadata and at
// most n of its candidates its dirac table plays. Fewer than 23 playable cards
// leaves no feasible colour pair, so no deck can be built from it.
func writeSmallManifest(t *testing.T, reg *cards.Registry, tables *dside.Tables, dir, code string, n int) {
	t.Helper()
	m, err := compliance.LoadManifest(manifestDir, code)
	if err != nil {
		t.Fatal(err)
	}
	cands, _, err := decks.Candidates(reg, manifestDir, code)
	if err != nil {
		t.Fatal(err)
	}
	playable, err := tables.Playable(code, cands)
	if dside.NoTables(err) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(playable) == 0 {
		t.Fatalf("%s: no dirac-playable candidates", code)
	}
	keep := map[string]bool{}
	for _, c := range playable[:min(n, len(playable))] {
		keep[c.Name] = true
	}
	var cards []compliance.ManifestCard
	for _, mc := range m.Cards {
		if keep[mc.Name] {
			cards = append(cards, mc)
		}
	}
	m.Cards = cards
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, compliance.ManifestFileName(code)), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// copyManifest puts the real set's manifest file into dir unchanged.
func copyManifest(t *testing.T, dir, code string) {
	t.Helper()
	name := compliance.ManifestFileName(code)
	raw, err := os.ReadFile(filepath.Join(manifestDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// captureStdout runs f with os.Stdout redirected to a pipe and returns what f
// wrote, so a test can read a message the picker reports without returning it.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() {
		os.Stdout = old
	}()
	f()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

// TestRandomPickerNamesUnpairableSet: a set whose dirac table exists but whose
// playable candidates fit no colour pair fails with the set's name and
// decks.Pairable's reason, not a claim that the table is missing.
func TestRandomPickerNamesUnpairableSet(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tables := dside.NewTables(reg)
	dir := t.TempDir()
	writeSmallManifest(t, reg, tables, dir, "DSK", 5)

	_, err := randomPicker(reg, dir, []string{"DSK"}, decks.Options{}, tables, nil, 1)
	if err == nil {
		t.Fatal("randomPicker built a picker from a set with no feasible colour pair")
	}
	msg := err.Error()
	if !strings.Contains(msg, "DSK") {
		t.Errorf("error does not name the set: %v", err)
	}
	if !strings.Contains(msg, "no colour pair has 23 eligible candidates") {
		t.Errorf("error does not carry Pairable's reason: %v", err)
	}
	if strings.Contains(msg, "has a dirac table") {
		t.Errorf("error blames the dirac table, which exists: %v", err)
	}
}

// TestRandomPickerReportsDroppedSet: with one unpairable and one pairable set,
// the unpairable one is reported in the play log and the pairable one still
// deals decks.
func TestRandomPickerReportsDroppedSet(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tables := dside.NewTables(reg)
	if _, err := tables.Table("FDN"); dside.NoTables(err) {
		t.Skip(err)
	} else if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	copyManifest(t, dir, "FDN")
	writeSmallManifest(t, reg, tables, dir, "DSK", 5)

	var (
		pick picker
		err  error
	)
	log := captureStdout(t, func() {
		pick, err = randomPicker(reg, dir, []string{"DSK", "FDN"}, decks.Options{}, tables, nil, 1)
	})
	if err != nil {
		t.Fatalf("a pairable set was dropped too: %v", err)
	}
	for _, want := range []string{"DSK", "no colour pair has 23 eligible candidates"} {
		if !strings.Contains(log, want) {
			t.Errorf("play log does not report the dropped set (%q missing):\n%s", want, log)
		}
	}

	for g := 0; g < 4; g++ {
		cands, led, table := pick(g, gameSeed(1, 0, g))
		if table != "FDN" {
			t.Fatalf("game %d: table %q, want FDN", g, table)
		}
		if err := decks.Pairable(cands); err != nil {
			t.Fatalf("game %d: pairable set does not deal: %v", g, err)
		}
		if pair := decks.Pair(cands, led, "FDN", 0, g, 1); len(pair) != 2 {
			t.Fatalf("game %d: %d decks, want 2", g, len(pair))
		}
	}
}
