package templates

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// Pinned corpus split of a back face's own "when this enters" trigger
// (trigger_recipes_backface_etb.go). Both directions fail: a regression to
// the generic skip lowers served, a new corpus shape moves either count.
const (
	wantBackFaceETBServed = 5  // Craft front faces: the craft cause serves them
	wantBackFaceETBNamed  = 11 // no Craft (disturb, return-transformed) or an unplaceable craft cost
	// wantManaFromCardServed is every "spell cast using mana produced by
	// CARDNAME" trigger (Tecutlan, The Myriad Pools, Barracks of the
	// Thousand): spellCastSourceManaCause serves each (the producing-source
	// provenance, engine/rules/cast_mana_sources.go).
	wantManaFromCardServed = 3
)

// TestBackFaceSelfETBCensus is the corpus-wide ratchet for the back-face
// triggers the craft/transform prelude class covers. A face-after-0 self-ETB
// fires only when the back face enters, so every such requirement is either
// served by the front face's Craft cause or skipped with the named
// "back-face enters trigger" reason -- never the bare "did not fire", and a
// requirement served without a Craft front face would be an unreal fixture.
func TestBackFaceSelfETBCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	served := map[string]bool{}
	named := map[string]string{}
	var bare []string
	manaFrom := 0
	for _, c := range reg.AllCards() {
		if len(c.Faces) < 2 {
			continue
		}
		for _, r := range levelb.Requirements(c) {
			if r.Family != "trigger" || r.Face == 0 || r.Gap != "" {
				continue
			}
			f := c.Faces[r.Face]
			idx := 0
			fmt.Sscanf(r.Slot, "%d", &idx)
			if idx < 0 || idx >= len(f.Triggers) {
				continue
			}
			tr := &f.Triggers[idx]
			key := c.Faces[0].Name + " " + r.Key
			if strings.Contains(strings.ToLower(tr.ParamStr(cards.PKValidSA)), "manafromcard") {
				if _, skip := GenerateB(reg, c.Faces[0].Name, r); skip != nil {
					t.Errorf("%s: want the mana-from-source cause to serve it, got %+v", key, skip)
				}
				manaFrom++
				continue
			}
			if r.Sub != "trigger.etb-other" {
				continue
			}
			if _, ok := backFaceSelfETB(c, f, tr); !ok {
				continue
			}
			hasCraft := craftAbilityIndex(c.Faces[0]) >= 0
			_, skip := GenerateB(reg, c.Faces[0].Name, r)
			switch {
			case skip == nil:
				if !hasCraft {
					t.Errorf("%s served without a Craft front face", key)
				}
				served[key] = true
			case strings.Contains(skip.Reason, "back-face enters trigger"):
				named[key] = skip.Reason
			default:
				bare = append(bare, key+": "+skip.Reason)
			}
		}
	}
	sort.Strings(bare)
	t.Logf("back-face self-ETB: %d served by craft, %d named skips; %d mana-from-source served", len(served), len(named), manaFrom)
	for _, want := range []string{"Clay-Fired Bricks", "Idol of the Deep King", "Jade Seedstones", "Paleontologist's Pick-Axe", "Visage of Dread"} {
		if !served[want+" trigger#1.0"] {
			t.Errorf("%s trigger#1.0 is not served by the craft cause", want)
		}
	}
	if len(bare) != 0 {
		t.Errorf("back-face self-ETBs with neither a cause nor the named skip: %v", bare)
	}
	if len(served) != wantBackFaceETBServed || len(named) != wantBackFaceETBNamed {
		t.Errorf("back-face self-ETB split = %d served / %d named, want %d / %d", len(served), len(named), wantBackFaceETBServed, wantBackFaceETBNamed)
	}
	if manaFrom != wantManaFromCardServed {
		t.Errorf("mana-from-source triggers = %d, want %d", manaFrom, wantManaFromCardServed)
	}
}
