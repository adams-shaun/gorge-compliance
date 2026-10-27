package oraclediff

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestLibraryAfterShuffle: a "pN.library_top@K" ignore entry leaves seat N's
// library order out from checkpoint K on, and only that seat's.
func TestLibraryAfterShuffle(t *testing.T) {
	cps := []string{"setup", "step 0 (pass)", "step 1 (pass)"}
	build := func(top0, top1 string) []rules.OracleSnapshot {
		var out []rules.OracleSnapshot
		for _, cp := range cps {
			s := snap(cp)
			s.Players[0].LibraryTop = []string{top0}
			s.Players[1].LibraryTop = []string{top1}
			out = append(out, s)
		}
		return out
	}
	g := rules.OracleResult{Snapshots: build("Wastes", "Shock")}
	xs := build("Wastes", "Shock")
	for i := 1; i < len(xs); i++ {
		xs[i] = xsnapOf(xs[i], "Island", "Shock")
	}
	x := XResult{Snapshots: xs}

	if v := Compare(g, nil, x); v.Status != Diverge || v.Field != "p0.library_top" || v.Checkpoint != "step 0 (pass)" {
		t.Fatalf("no shuffle marker: order is compared, got %+v", v)
	}
	if v := Compare(g, nil, x, "p0.library_top@1"); v.Status != Agree {
		t.Fatalf("seat 0 shuffled at checkpoint 1: order must not be compared, got %+v", v)
	}
	if v := Compare(g, nil, x, "p0.library_top@2"); v.Status != Diverge || v.Checkpoint != "step 0 (pass)" {
		t.Fatalf("order before the shuffle is still compared, got %+v", v)
	}
	if v := Compare(g, nil, x, "p1.library_top@0"); v.Status != Diverge || v.Field != "p0.library_top" {
		t.Fatalf("seat 1's shuffle must not excuse seat 0, got %+v", v)
	}
	// An unshuffled seat's mismatch still diverges after the other's shuffle.
	xs[2].Players[1].LibraryTop = []string{"Mountain"}
	if v := Compare(g, nil, x, "p0.library_top@1"); v.Status != Diverge || v.Field != "p1.library_top" || v.Checkpoint != "step 1 (pass)" {
		t.Fatalf("unshuffled seat's order is still compared, got %+v", v)
	}
	// Library size stays compared after the shuffle.
	xs[2].Players[1].LibraryTop = []string{"Shock"}
	xs[2].Players[0].LibraryCount--
	if v := Compare(g, nil, x, "p0.library_top@1"); v.Status != Diverge || v.Field != "p0.library_count" {
		t.Fatalf("library_count is compared after the shuffle, got %+v", v)
	}
	// The frozen form drops the field from the marked checkpoint on, too.
	if n := strings.Count(Canonical(g.Snapshots, "p0.library_top@1"), "p0.library_top:"); n != 1 {
		t.Fatalf("canonical form lists p0.library_top at %d checkpoints, want only the setup", n)
	}
}

func xsnapOf(s rules.OracleSnapshot, top0, top1 string) rules.OracleSnapshot {
	s.Step = "PRECOMBAT_MAIN"
	s.Players = append([]rules.OracleSnapPlayer{}, s.Players...)
	s.Players[0].LibraryTop = []string{top0}
	s.Players[1].LibraryTop = []string{top1}
	return s
}
