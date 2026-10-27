package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

func TestSameStepCheckpoint(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"step 7 (pass)", "step 7 (pass_to)", true},
		{"step 7 (pass)", "step 7 (pass)", true},
		{"step 7 (pass)", "step 8 (pass)", false},
		{"step 7 (pass)", "step 17 (pass)", false},
		{"main1", "main2", false},
		{"main1", "main1", true},
		{"main1", "step 7 (pass)", false},
	} {
		if got := sameStepCheckpoint(c.a, c.b); got != c.want {
			t.Errorf("sameStepCheckpoint(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestCompareSnapCheckpoint(t *testing.T) {
	snap := func(cp string) rules.OracleSnapshot { return rules.OracleSnapshot{Checkpoint: cp} }
	if v, ok := compareSnap(snap("step 7 (pass)"), snap("step 7 (pass_to)"), nil, nil); !ok {
		t.Fatalf("same step, different op = %+v, want a match", v)
	}
	for _, c := range [][2]string{{"step 7 (pass)", "step 8 (pass)"}, {"main1", "main2"}} {
		v, ok := compareSnap(snap(c[0]), snap(c[1]), nil, nil)
		if ok || v.Status != Harness || v.Engine != "both" {
			t.Errorf("%q vs %q = %+v (ok %v), want a harness verdict", c[0], c[1], v, ok)
		}
	}
}
