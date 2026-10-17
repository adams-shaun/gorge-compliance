package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// Pyrrhic Strike's CharmNum$ X is Count$OptionalGenericCostPaid.2.1 ("choose
// one; if this spell's additional cost was paid, choose both instead"), so its
// combinations offer both counts, the single-mode ones first: the declined
// cast keeps its historical first plan and the paid cast (the level-B
// optional-cost item) finds a two-mode plan.
func TestCharmCombinationsCoverOptionalCostCount(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Pyrrhic Strike")
	if !ok {
		t.Fatal("Pyrrhic Strike not in corpus")
	}
	combos := CharmCombinations(c.Faces[0])
	if len(combos) == 0 || len(combos[0].Modes) != 1 {
		t.Fatalf("first combination %+v, want a single mode", combos)
	}
	both := false
	for _, combo := range combos {
		both = both || len(combo.Modes) == 2
	}
	if !both {
		t.Fatalf("no two-mode combination among %d", len(combos))
	}
}
