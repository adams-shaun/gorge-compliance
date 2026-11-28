package main

import (
	"reflect"
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
