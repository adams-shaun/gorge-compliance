package templates

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// activateCensusSets are the four level-A sets the activate census pins,
// matching oracle/compliance/levelb's requirement census.
var activateCensusSets = []string{"BIG", "EOE", "FDN", "FRA"}

// wantActivateCensus pins, per set, every non-gap activate requirement's
// outcome: served, split by sub-family, or skipped, keyed by the skip reason
// with the no-fixture target list folded away. The test fails in both directions: a template or
// classifier change that changes a count shows up as a diff, and so does a
// stale pin. activate.zone:<Z> requirements (an activation zone other than
// hand or graveyard) are gaps the level-B classifier owns, so they are not
// counted here (oracle/compliance/levelb/census_test.go pins them).
//
// The activate.hand and activate.graveyard sub-families are served by this
// template: the card starts in p0's hand
// or graveyard and the activate step owns its channel-style cost.
// Git history records why each count moved.
var wantActivateCensus = map[string]map[string]int{
	"BIG": {
		// +1 served: Worldwalker Helm's "target artifact token you control"
		// gets the Thraben Inspector token prelude.
		// +1 served: Transmutation Font's different-names artifact-token
		// sacrifice gets three distinct token makers.
		"served:activate.battlefield": 13,
		"served:activate.hand":        2,
		"served:activate.mana":        4,
	},
	"EOE": {
		// +1 served at the merge of the tapXType change and the setup-counter
		// change: Secluded Starforge's announced tapXType<X/Artifact> count
		// (served with one tapped catalogue artifact, the X the tap election's
		// own selection) and Sunstar Chaplain's counter-removal cost (its
		// fixture bearer seeded with the counters) both generate; each side
		// alone skipped the other's row.
		"served:activate.battlefield": 39,
		"served:activate.mana":        23,
	},
	"FDN": {
		// +2 served (r5): Lathril's tapXType<10/Elf> after the Elf catalogue
		// grew to ten corpus Elves (the count-above-catalogue skip empties),
		// and Zul Ashur's Creature.Zombie+YouOwn@Graveyard target after the
		// zone candidates learned subtype-qualified card filters (the
		// no-fixture skip empties).
		"served:activate.battlefield": 91,
		"served:activate.graveyard":   2,
		"served:activate.mana":        53,
	},
	"FRA": {
		// +1 served at the merge of the ThisTurnEntered change and the
		// setup-counter change: Gallia, the Merrymaker's "target creature that
		// entered the battlefield this turn" (the mid-turn entry candidate) and
		// Chandra, Chill of Compliance's announced SubCounter cost both
		// generate; each side alone skipped the other's row. Re-measured at the
		// 2026-10-09 main merge: the graveyard candidate main serves lifts
		// graveyard 8 -> 9, and both prior skip buckets empty (the merged
		// generator serves the YouScryThisTurn SVar row and the no-fixture row
		// that each side alone still skipped). Re-measured for the
		// attackedThisTurn predicate change: Hexhaven Dueling Arena
		// activate#0.1 is served by an attack + pass_to main2 prelude, so the
		// named skip bucket is gone and battlefield rises 73 -> 74.
		"served:activate.battlefield": 74,
		"served:activate.graveyard":   9,
		"served:activate.hand":        9,
		"served:activate.mana":        27,
	},
}

// TestActivateCensus measures every non-gap activate requirement in the four
// level-A sets and pins served versus skipped counts.
func TestActivateCensus(t *testing.T) {
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
				t.Errorf("%s: %q not in the corpus", set, name)
				continue
			}
			c, _ := reg.Lookup(card)
			for _, r := range levelb.Requirements(c) {
				if r.Family != "activate" || r.Gap != "" {
					continue
				}
				_, skip := GenerateB(reg, card, r)
				if skip == nil {
					counts["served:"+r.Sub]++
					continue
				}
				counts[activateSkipBucket(skip.Reason)]++
			}
		}
		got[set] = counts
	}

	for _, set := range activateCensusSets {
		if diff := activateCensusDiff(wantActivateCensus[set], got[set]); diff != "" {
			t.Errorf("%s activate census mismatch:\n%s", set, diff)
		}
	}
}

// activateSkipBucket folds the no-fixture reason's target list away so the
// ratchet groups by cause, not by fixture detail.
func activateSkipBucket(reason string) string {
	if strings.HasPrefix(reason, "activate no fixture") {
		return "skip:activate no fixture"
	}
	return "skip:" + reason
}

// activateCensusDiff reports, in both directions, every bucket whose count
// moved.
func activateCensusDiff(want, got map[string]int) string {
	keys := map[string]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range got {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var lines []string
	for _, k := range sorted {
		if want[k] != got[k] {
			lines = append(lines, fmt.Sprintf("  %-45s want %4d got %4d", k, want[k], got[k]))
		}
	}
	return strings.Join(lines, "\n")
}
