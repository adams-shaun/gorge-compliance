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
// card with two mana abilities gets both (unique ids), a nonbasic land's
// explicit AB$ Mana ability gets one, and a basic land gets none (its
// intrinsic ability is engine-provided, not a requirement).
func TestGenerateManaAbilitiesIsRequirementDriven(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if items, _ := GenerateManaAbilities(reg, "Forest"); len(items) != 0 {
		t.Fatalf("Forest: %d activation items, want 0 (a basic land has no AB$ Mana requirement)", len(items))
	}
	landItems, landSkips := GenerateManaAbilities(reg, "Kher Keep")
	if len(landSkips) != 0 || len(landItems) != 1 {
		t.Fatalf("Kher Keep: %d items, %d skips; want its one AB$ Mana ability", len(landItems), len(landSkips))
	}
	if got := landItems[0].Scenario.Setup["p0"].Battlefield; !containsString(got, "Kher Keep") {
		t.Fatalf("Kher Keep: p0 battlefield %v does not hold the land", got)
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

// TestGenerateManaAbilitiesSkipsLoyalty: a loyalty ability that adds mana is
// not a mana ability (CR 605.1a); it uses the stack and follower-export
// classes it "loyalty", so an activation item here would never exercise
// mana_source and would stamp the wrong CR. The level-B activate family still
// covers it (TestActivateLoyaltyManaUsesStack).
func TestGenerateManaAbilitiesSkipsLoyalty(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Chandra, Flameshaper", "Xenagos, the Reveler"} {
		items, skips := GenerateManaAbilities(reg, name)
		if len(items) != 0 || len(skips) != 0 {
			t.Errorf("%s: %d items, %d skips; a loyalty ability is not a mana ability", name, len(items), len(skips))
		}
	}
}

// TestActivateManaTemplatePinned: ActivateMana is a level-A template, so its
// identity lives in All and its id/version are pinned to the values that name
// the generated items. TestTemplatesAreDistinctAndVersioned checks the rest.
func TestActivateManaTemplatePinned(t *testing.T) {
	if ActivateMana.ID != "activate-mana" || ActivateMana.Version != 1 {
		t.Fatalf("ActivateMana = %+v, want activate-mana v1", ActivateMana)
	}
	found := false
	for _, tp := range All {
		if tp == ActivateMana {
			found = true
		}
	}
	if !found {
		t.Fatal("ActivateMana is missing from All; the template version test would not cover it")
	}
}
