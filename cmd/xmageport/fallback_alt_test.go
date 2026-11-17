package main

import (
	"reflect"
	"testing"
)

func TestFallbackAlternativesTried(t *testing.T) {
	trig := []dumpOption{{0, "trigger", "A", ""}, {1, "trigger", "B", ""}, {2, "trigger", "C", ""}}
	got := fallbackAlternatives("trigger_order", trig)
	want := [][]string{{"A", "B", "C"}, {"A", "C", "B"}, {"B", "A", "C"}, {"B", "C", "A"}, {"C", "A", "B"}, {"C", "B", "A"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("three triggers: %v, want the 6 orders %v", got, want)
	}

	tgt := []dumpOption{{0, "target", "a", ""}, {1, "target", "b", ""}, {2, "target", "c", ""}, {3, "target", "d", ""}}
	got = fallbackAlternatives("target", tgt)
	want = [][]string{{"a"}, {"b"}, {"c"}, {"d"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("four targets: %v, want %v", got, want)
	}

	// caps and other kinds
	five := append(append([]dumpOption(nil), trig...), dumpOption{3, "trigger", "D", ""}, dumpOption{4, "trigger", "E", ""})
	if got := fallbackAlternatives("trigger_order", five); got != nil {
		t.Errorf("five triggers: %v, want none (past the cap)", got)
	}
	if got := len(fallbackAlternatives("trigger_order", five[:4])); got != 24 {
		t.Errorf("four triggers: %d orders, want 24", got)
	}
	nine := make([]dumpOption, 9)
	if got := fallbackAlternatives("target", nine); got != nil {
		t.Errorf("nine targets: %v, want none (past the cap)", got)
	}
	if got := fallbackAlternatives("choose", tgt); got != nil {
		t.Errorf("choose: %v, want none", got)
	}
	// equal labels are one answer
	if got := fallbackAlternatives("target", []dumpOption{{0, "target", "x", ""}, {1, "target", "x", ""}}); len(got) != 1 {
		t.Errorf("duplicate labels: %v, want one answer", got)
	}

	fp, ok := firstFallbackPoint([]string{
		"setup done",
		"step cast_spell p0 x",
		"  [pass_to fallback] p0 attackers -> []",
		"step resolve",
		`  [resolve fallback] p1 trigger_order -> ["B" "A, the \"x\""]`,
	})
	if !ok || fp.Step != 1 || fp.Seat != 1 || fp.Kind != "trigger_order" || !reflect.DeepEqual(fp.Picked, []string{"B", `A, the "x"`}) {
		t.Errorf("firstFallbackPoint = %+v, %v", fp, ok)
	}
}
