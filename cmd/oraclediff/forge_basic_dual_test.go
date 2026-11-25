package main

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestForgeAbilityLinesBasicDualColour: a land with two basic land types has
// two intrinsic mana abilities with the same line, and gorge's choice between
// them is dropped from the request (the step names the ability by index), so
// the Forge driver met two "Mana T" candidates and failed "ability identity".
// Each step's line now carries its ability's colour; a lone basic land's line
// is unchanged.
func TestForgeAbilityLinesBasicDualColour(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Underground Sea", "Savannah", "Commercial District"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s missing from corpus", name)
		}
		f := c.Faces[0]
		var idx []int
		for i, sa := range f.Abilities {
			if sa != nil && sa.Line == oraclegen.IntrinsicManaLine {
				idx = append(idx, i)
			}
		}
		if len(idx) != 2 {
			t.Fatalf("%s: %d intrinsic mana abilities, want 2", name, len(idx))
		}
		var steps []oraclegen.Step
		for i := range idx {
			steps = append(steps, oraclegen.Step{Op: "activate", Card: "p0:" + name, AbilityIndex: &idx[i]})
		}
		lines := forgeAbilityLines(reg, oraclegen.Item{Scenario: oraclegen.Scenario{Steps: steps}}, rules.OracleResult{})
		a, b := f.Abilities[idx[0]].Params["Produced"], f.Abilities[idx[1]].Params["Produced"]
		if a == b {
			t.Fatalf("%s: both intrinsic abilities produce %s", name, a)
		}
		if got, want := lines["0"], oraclegen.IntrinsicManaLine+" "+a; got != want {
			t.Errorf("%s ability %d line = %q, want %q", name, idx[0], got, want)
		}
		if got, want := lines["1"], oraclegen.IntrinsicManaLine+" "+b; got != want {
			t.Errorf("%s ability %d line = %q, want %q", name, idx[1], got, want)
		}
	}
	zero := 0
	plains := oraclegen.Item{Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{{Op: "activate", Card: "p0:Plains", AbilityIndex: &zero}}}}
	if got := forgeAbilityLines(reg, plains, rules.OracleResult{})["0"]; got != oraclegen.IntrinsicManaLine {
		t.Errorf("Plains line = %q, want %q", got, oraclegen.IntrinsicManaLine)
	}
}
