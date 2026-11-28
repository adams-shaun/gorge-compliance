package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The record and the committed scenarios must agree in both directions:
// every scenario whose why reports dropped choices has a row with that
// count, and no row describes a test the scenarios say drops nothing.
// Triage cells stay inside their closed set and cite a rule where claimed.
func TestDroppedChoicesRecordMatchesScenarios(t *testing.T) {
	measured, err := droppedFromScenarios("../../scenarios/xmageport")
	if err != nil {
		t.Fatal(err)
	}
	record, err := loadDroppedRecord("dropped_choices.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if len(measured) == 0 {
		t.Fatal("no scenario reports a dropped choice: the walk found nothing")
	}
	got := map[string]int{}
	for _, r := range record {
		if _, dup := got[r.XMage]; dup {
			t.Errorf("%s: duplicate row", r.XMage)
		}
		if r.XMage == "" || !strings.Contains(r.XMage, "#") {
			t.Errorf("row %q is not a Class#method key", r.XMage)
		}
		if !droppedClasses[r.Class] {
			t.Errorf("%s: class %q is not in the closed set", r.XMage, r.Class)
		}
		if droppedNeedsCR[r.Class] && r.CR == "" {
			t.Errorf("%s: class %q requires a cr", r.XMage, r.Class)
		}
		if r.Dropped <= 0 {
			t.Errorf("%s: dropped must be positive, got %d", r.XMage, r.Dropped)
		}
		got[r.XMage] = r.Dropped
	}
	for k, n := range measured {
		if g, ok := got[k]; !ok {
			t.Errorf("record lacks a row for %s (scenarios drop %d choices)", k, n)
		} else if g != n {
			t.Errorf("%s: record says dropped=%d, scenarios say %d", k, g, n)
		}
	}
	for k, g := range got {
		if _, ok := measured[k]; !ok {
			t.Errorf("record has a row for %s (dropped=%d) no scenario drops", k, g)
		}
	}
	if !sortedByXMage(record) {
		t.Error("the record is not sorted by xmage")
	}
	// The triage deliverable itself: the 30 tests with the most dropped
	// choices must carry a class. A count change legitimately reopens one
	// (mergeDropped clears it), so this fails only when triage is missing.
	ranked := make([]droppedRow, len(record))
	copy(ranked, record)
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Dropped != ranked[j].Dropped {
			return ranked[i].Dropped > ranked[j].Dropped
		}
		return ranked[i].XMage < ranked[j].XMage
	})
	top := 30
	if len(ranked) < top {
		top = len(ranked)
	}
	for _, r := range ranked[:top] {
		if r.Class == "" {
			t.Errorf("%s: among the %d highest dropped counts but untriaged", r.XMage, top)
		}
	}
}

func sortedByXMage(rows []droppedRow) bool {
	for i := 1; i < len(rows); i++ {
		if rows[i-1].XMage >= rows[i].XMage {
			return false
		}
	}
	return true
}

// mergeDropped keeps the triage of a row whose count did not change, clears
// the class of a row whose count changed (the old triage argued the old
// count) but keeps its cr and note as context for the re-triage, drops a row
// whose test no longer drops choices, and adds a row for a test that newly
// does.
func TestMergeDropped(t *testing.T) {
	record := []droppedRow{
		{XMage: "A#keep", Dropped: 3, Class: "harness", Note: "kept"},
		{XMage: "A#count", Dropped: 2, Class: "xmage-extra-ask", CR: "701.2a", Note: "stale triage"},
		{XMage: "A#gone", Dropped: 1, Class: "setup-trigger", Note: "no longer drops"},
	}
	measured := map[string]int{"A#keep": 3, "A#count": 5, "A#new": 1}
	want := []droppedRow{
		{XMage: "A#count", Dropped: 5, CR: "701.2a", Note: "stale triage"},
		{XMage: "A#keep", Dropped: 3, Class: "harness", Note: "kept"},
		{XMage: "A#new", Dropped: 1},
	}
	got := mergeDropped(record, measured)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeDropped:\n got %+v\nwant %+v", got, want)
	}
	if again := mergeDropped(got, measured); !reflect.DeepEqual(again, want) {
		t.Errorf("mergeDropped is not idempotent: %+v", again)
	}
}

// writeScenarioTree writes one generated port file so a temp root looks like
// the committed scenarios/xmageport tree.
func writeScenarioTree(t *testing.T, root string, scenarios []portScenario) {
	t.Helper()
	p := filepath.Join(root, "cards", "Test.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(portFile{Scenarios: scenarios})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// syncDroppedRecord is the -commit path's whole write: it must keep triage
// for an unchanged count, clear the class (only) for a changed count, drop a
// vanished test and add a new one, and be byte-idempotent. A missing
// scenario tree is an empty measurement, not an error.
func TestSyncDroppedRecord(t *testing.T) {
	dir := t.TempDir()
	scen := filepath.Join(dir, "scenarios")
	writeScenarioTree(t, scen, []portScenario{
		{XMage: "A#keep", Why: "x 3 XMage choice(s) gorge never asks were dropped."},
		{XMage: "A#count", Why: "x 5 XMage choice(s) gorge never asks were dropped."},
		{XMage: "A#new", Why: "x 2 XMage choice(s) gorge never asks were dropped."},
	})
	record := filepath.Join(dir, "dropped_choices.tsv")
	if err := writeDroppedRecord(record, []droppedRow{
		{XMage: "A#keep", Dropped: 3, Class: "harness", Note: "kept"},
		{XMage: "A#count", Dropped: 2, Class: "xmage-extra-ask", CR: "601.2b", Note: "stale triage"},
		{XMage: "A#gone", Dropped: 1, Class: "setup-trigger", Note: "no longer drops"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := syncDroppedRecord(scen, record); err != nil {
		t.Fatal(err)
	}
	got, err := loadDroppedRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	want := []droppedRow{
		{XMage: "A#count", Dropped: 5, CR: "601.2b", Note: "stale triage"},
		{XMage: "A#keep", Dropped: 3, Class: "harness", Note: "kept"},
		{XMage: "A#new", Dropped: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("syncDroppedRecord:\n got %+v\nwant %+v", got, want)
	}
	first, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := syncDroppedRecord(scen, record); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("syncDroppedRecord is not byte-idempotent")
	}

	// A -commit run to a fresh output dir has no generated scenarios.
	fresh := filepath.Join(dir, "fresh.tsv")
	if err := syncDroppedRecord(filepath.Join(dir, "missing"), fresh); err != nil {
		t.Fatalf("missing scenario tree: %v", err)
	}
	raw, err := os.ReadFile(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != droppedHeader+"\n" {
		t.Errorf("missing scenario tree wrote %q, want just the header", raw)
	}
}

// writeOutputs (what `go run ./cmd/xmageport` always writes) must put the
// measured dropped count in report.tsv's last column, so the run reports the
// divergence even without -commit. It must not write summary.txt: main writes
// that only after the -commit sync, so a pre-sync untriaged count can never
// be what the file says.
func TestWriteOutputsReportDroppedColumn(t *testing.T) {
	old := droppedRecordPath
	droppedRecordPath = filepath.Join(t.TempDir(), "dropped_choices.tsv")
	t.Cleanup(func() { droppedRecordPath = old })
	out := t.TempDir()
	results := []*result{
		{Family: "cards", Class: "DroppedTest", Method: "drops", Status: "pass", Dropped: 6},
		{Family: "cards", Class: "CleanTest", Method: "clean", Status: "pass"},
	}
	if err := writeOutputs(out, results); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "report.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	wantHeader := "family\tclass\tmethod\tstatus\tcode\tdetail\tfirst_fail\tunsupported\tdropped"
	if len(lines) != 3 {
		t.Fatalf("report.tsv has %d lines, want 3: %q", len(lines), raw)
	}
	if lines[0] != wantHeader {
		t.Errorf("report.tsv header:\n got %q\nwant %q", lines[0], wantHeader)
	}
	for i, want := range []string{"6", "0"} {
		f := strings.Split(lines[i+1], "\t")
		if len(f) != 9 {
			t.Fatalf("report.tsv line %d has %d columns, want 9: %q", i+2, len(f), lines[i+1])
		}
		if f[len(f)-1] != want {
			t.Errorf("report.tsv line %d dropped=%q, want %q", i+2, f[len(f)-1], want)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "summary.txt")); !os.IsNotExist(err) {
		t.Error("writeOutputs wrote summary.txt before the -commit sync; summary must be written after it")
	}
}
