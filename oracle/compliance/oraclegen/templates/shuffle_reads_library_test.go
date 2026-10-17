package templates

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
)

// TestShuffleThenReadsLibraryCompareCensus is the corpus census of the
// shuffle-then-read-the-top mechanism (Stay Hidden, Stay Silent: shuffle the
// enchanted creature into its owner's library, then manifest dread). Every
// face the classifier matches must produce level-B items that opt into the
// graveyard-count compare, and the matched set is pinned so a classifier
// change that widens or loses it is a visible edit.
func TestShuffleThenReadsLibraryCompareCensus(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	var matched []string
	for _, c := range reg.AllCards() {
		for fi, f := range c.Faces {
			if !oraclegen.ShufflesThenReadsLibrary(f) || !oraclegen.CanShuffleLibrary(f) {
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
				found := false
				for _, o := range item.Compare {
					found = found || o == oraclegen.CompareGraveyardCount
				}
				if !found {
					t.Errorf("%s %s: compare %v lacks %q", name, req.Key, item.Compare, oraclegen.CompareGraveyardCount)
				}
			}
		}
	}
	sort.Strings(matched)
	t.Logf("matched faces: %q", matched)
	hit := false
	for _, n := range matched {
		hit = hit || n == "Stay Hidden, Stay Silent"
	}
	if !hit || len(matched) > 40 {
		t.Fatalf("census precondition: Stay Hidden matched=%t, %d faces", hit, len(matched))
	}
}

// TestCompareGraveyardCountIgnoresNamesButKeepsCount: two graveyards of the
// same size with different cards agree under graveyard_count, differ without
// it, and a different size still diverges.
func TestCompareGraveyardCountIgnoresNamesButKeepsCount(t *testing.T) {
	snap := func(gy ...string) []rules.OracleSnapshot {
		return []rules.OracleSnapshot{{Checkpoint: "setup", Players: []rules.OracleSnapPlayer{{Graveyard: gy}, {}}}}
	}
	run := func(g, x []string, compare []string) oraclediff.Verdict {
		return oraclediff.CompareOpts(rules.OracleResult{Snapshots: snap(g...)}, nil,
			oraclediff.XResult{Snapshots: snap(x...)}, compare)
	}
	opt := []string{oraclegen.CompareGraveyardCount}
	if v := run([]string{"Hill Giant"}, []string{"Wastes"}, nil); v.Status != oraclediff.Diverge {
		t.Fatalf("precondition: differing graveyards must diverge without the option, got %v", v)
	}
	if v := run([]string{"Hill Giant"}, []string{"Wastes"}, opt); v.Status != oraclediff.Agree {
		t.Fatalf("same-size graveyards must agree under the option, got %+v", v)
	}
	if v := run([]string{"Hill Giant"}, []string{"Wastes", "Wastes"}, opt); v.Status != oraclediff.Diverge {
		t.Fatalf("different-size graveyards must still diverge, got %+v", v)
	}
	if err := oraclediff.ValidateCompare(opt); err != nil {
		t.Fatal(err)
	}
}
