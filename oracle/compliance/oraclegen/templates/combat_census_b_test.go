package templates

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// wantCombatCensus pins, per set, every non-gap combat requirement's outcome.
// Git history records why each count moved.
var wantCombatCensus = map[string]map[string]int{
	"BIG": {"served:combat.attack": 4, "served:combat.block": 4},
	"EOE": {"served:combat.attack": 49, "served:combat.block": 49},
	"FDN": {"served:combat.attack": 127, "served:combat.block": 127},
	"FRA": {"served:combat.attack": 63, "served:combat.block": 63},
}

func TestCombatCensusB(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..", "..", "..")
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)
	got := map[string]map[string]int{}
	for _, set := range activateCensusSets {
		printed, err := compliance.LoadPrinted(filepath.Join(root, "oracle", "compliance", "printed"), set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		counts := map[string]int{}
		for _, name := range printed.Cards {
			card, ok := compliance.CorpusNameFold(has, folded, name)
			if !ok {
				t.Errorf("%s: %q not in corpus", set, name)
				continue
			}
			c, _ := reg.Lookup(card)
			for _, req := range levelb.Requirements(c) {
				if !combatSubs(req.Sub) || req.Gap != "" {
					continue
				}
				_, skip := GenerateB(reg, card, req)
				if skip == nil {
					counts["served:"+req.Sub]++
				} else {
					counts["skip:"+skip.Reason]++
				}
			}
		}
		got[set] = counts
	}
	for _, set := range activateCensusSets {
		if diff := activateCensusDiff(wantCombatCensus[set], got[set]); diff != "" {
			t.Errorf("%s combat census mismatch:\n%s", set, diff)
		}
	}
}
