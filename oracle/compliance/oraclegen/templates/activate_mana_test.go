package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestActivateManaLevelAItems: every named nonland mana source gets a level-A
// activation item that starts on the battlefield and activates a Mana
// ability, including Sol Talisman, whose ManaCost is "no cost" (so its
// ordinary Generate item used to be a skip).
func TestActivateManaLevelAItems(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{
		"The Alright Henge",
		"Torgal, A Fine Hound",
		"Sol Talisman",
		"Riven Turnbull",
		"Princess Lucrezia",
	} {
		t.Run(name, func(t *testing.T) {
			c, ok := reg.Lookup(name)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("%s is not in the corpus", name)
			}
			items, skips := GenerateManaAbilities(reg, name)
			if len(skips) != 0 {
				t.Fatalf("%s: skips %+v", name, skips)
			}
			if len(items) != 1 {
				t.Fatalf("%s: %d activation items, want 1", name, len(items))
			}
			it := items[0]
			if it.Template != ActivateMana.ID || it.Scenario.Name != "gen1-"+ActivateMana.ID {
				t.Errorf("%s: template %q name %q, want %q", name, it.Template, it.Scenario.Name, ActivateMana.ID)
			}
			if it.ID != name+"/"+ActivateMana.ID+"/v1" {
				t.Errorf("%s: id %q", name, it.ID)
			}
			if got := it.Scenario.Setup["p0"].Battlefield; !containsString(got, name) {
				t.Errorf("%s: p0 battlefield %v does not hold the source", name, got)
			}
			var act *oraclegen.Step
			for i := range it.Scenario.Steps {
				if it.Scenario.Steps[i].Op == "activate" {
					act = &it.Scenario.Steps[i]
				}
			}
			if act == nil {
				t.Fatalf("%s: no activate step", name)
			}
			if act.AbilityIndex == nil || *act.AbilityIndex < 0 || *act.AbilityIndex >= len(c.Faces[0].Abilities) {
				t.Fatalf("%s: activate step ability_index %v", name, act.AbilityIndex)
			}
			if sa := c.Faces[0].Abilities[*act.AbilityIndex]; !strings.EqualFold(sa.API, "Mana") {
				t.Fatalf("%s: activate step names %q ability, want Mana", name, sa.API)
			}
		})
	}
}

// TestGenerateManaAbilitiesIsRequirementDriven: the item set comes from
// levelb.Requirements' activate.mana rows, not a hand-rolled API check, so a
// card with two mana abilities gets both (unique ids) and a land gets none
// (play-land already plays it).
func TestGenerateManaAbilitiesIsRequirementDriven(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if items, _ := GenerateManaAbilities(reg, "Forest"); len(items) != 0 {
		t.Fatalf("Forest: %d activation items, want 0 (play-land serves it)", len(items))
	}
	for _, name := range []string{"The Alright Henge", "Torgal, A Fine Hound", "Talisman of Unity"} {
		c, _ := reg.Lookup(name)
		want := 0
		for _, req := range levelb.Requirements(c) {
			if req.Face == 0 && req.Sub == "activate.mana" {
				want++
			}
		}
		items, _ := GenerateManaAbilities(reg, name)
		if len(items) != want {
			t.Errorf("%s: %d items, requirements name %d", name, len(items), want)
		}
		if name == "Talisman of Unity" && want != 2 {
			t.Fatalf("Talisman of Unity: requirements name %d mana abilities, want 2 (the sibling check)", want)
		}
		seen := map[string]bool{}
		for _, it := range items {
			if seen[it.ID] {
				t.Errorf("%s: duplicate id %q", name, it.ID)
			}
			seen[it.ID] = true
		}
	}
}
