package templates

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// wantStaticContinuousCensus pins, per set, how many static.continuous
// requirements the static template serves and how many it skips, keyed by the
// skip reason. A skip is either the generic observability reason (gorge showed
// no effect on any probe or the card: a conditional self static the fixture
// leaves false, or a filter gorge does not apply) or a narrower named one (a
// qualifier the setup cannot give a probe, a grant outside the compared
// evergreen keywords, an amount counted from state the fixture does not make
// observable). It fails in both directions. Git history records why each count moved.
var wantStaticContinuousCensus = map[string]map[string]int{
	"BIG": {
		"served": 3,
	},
	"EOE": {
		"served": 63,
	},
	"FDN": {
		"served": 68,
		"skip:static effect not observable on a probe or the card":                                   3,
		"skip:static grants an activated ability (needs the driver's activate on a granted ability)": 1,
		"skip:static amount is a computed count the fixture does not make observable":                1,
		"skip:static needs counters on the affected permanent":                                       1,
	},
	"FRA": {
		"served": 38,
		"skip:static effect not observable on a probe or the card": 1,
	},
	// DFT includes the Surveyor cycle's graveyard AddAbility$ grant, served by
	// the gated-grant observation's off-battlefield arm; its four former engine-gap skips are gone.
	// The Last Ride's crewed -X/-X life static is the computed-count row the crew prelude
	// serves, and Midnight Mangler's Condition$ NotPlayerTurn static is the generic-observability row the
	// advanced-scenario path serves.
	"DFT": {
		"served": 65,
	},
}

// staticCensusSets are the sets TestStaticContinuousCensus pins. It extends
// the level-A census sets (activateCensusSets) with DFT, whose Surveyor cycle
// carries the graveyard AddAbility$ grant (served) -- pinning DFT is what
// keeps the grant's served rows in the census.
var staticCensusSets = append([]string{"DFT"}, activateCensusSets...)

func TestStaticContinuousCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..", "..", "..")
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)

	got := map[string]map[string]int{}
	for _, set := range staticCensusSets {
		printed, err := compliance.LoadPrinted(filepath.Join(root, "oracle", "compliance", "printed"), set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		counts := map[string]int{}
		for _, name := range printed.Cards {
			card, ok := compliance.CorpusNameFold(has, folded, name)
			if !ok {
				t.Errorf("%s: %q not in the corpus", set, name)
				continue
			}
			c, _ := reg.Lookup(card)
			for _, r := range levelb.Requirements(c) {
				if r.Sub != "static.continuous" || r.Gap != "" {
					continue
				}
				if _, skip := GenerateB(reg, card, r); skip != nil {
					counts["skip:"+skip.Reason]++
				} else {
					counts["served"]++
				}
			}
		}
		got[set] = counts
	}
	for _, set := range staticCensusSets {
		if diff := activateCensusDiff(wantStaticContinuousCensus[set], got[set]); diff != "" {
			t.Errorf("%s static.continuous census mismatch:\n%s", set, diff)
		}
	}
}
