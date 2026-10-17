package templates

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// TestShufflesPermanentThenDrawsCompareCensus is the corpus census of the
// shuffle-a-permanent-then-draw mechanism (Gandalf, Wandering Wizard: "{6}:
// Gandalf's owner shuffles him into their library and draws three cards").
// XMage's shuffle is random, so the shuffled card may be drawn back: every
// level-B item of a matched face compares the hand by size, and the matched
// set is pinned so a classifier change is a visible edit.
func TestShufflesPermanentThenDrawsCompareCensus(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	var matched []string
	for _, c := range reg.AllCards() {
		for fi, f := range c.Faces {
			if !oraclegen.ShufflesPermanentThenDraws(f) {
				continue
			}
			matched = append(matched, f.Name)
			name := c.Faces[0].Name
			if len(c.Faces) == 2 {
				name += " // " + c.Faces[1].Name
			}
			for _, req := range levelb.Requirements(c) {
				if req.Face != fi || req.Gap != "" || req.CoveredByA {
					continue
				}
				item, skip := GenerateB(reg, name, req)
				if skip != nil {
					continue
				}
				if !hasCompareOption(item.Compare, oraclegen.CompareHandCount) {
					t.Errorf("%s %s: compare %v lacks %q", name, req.Key, item.Compare, oraclegen.CompareHandCount)
				}
			}
		}
	}
	sort.Strings(matched)
	t.Logf("matched faces: %q", matched)
	hit := false
	for _, n := range matched {
		hit = hit || n == "Gandalf, Wandering Wizard"
	}
	if !hit || len(matched) > 40 {
		t.Fatalf("census precondition: Gandalf matched=%t, %d faces", hit, len(matched))
	}
}
