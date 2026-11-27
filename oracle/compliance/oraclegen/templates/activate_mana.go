// Level-A activation template for a mana ability. It exists because the
// ordinary level-A scenarios only ever cast a card: a follower driver's
// `activate` step and the `neutral.ability` entry its mana-source mapping
// reads were exercised by level-B scenarios alone. follower-check needs that
// coverage at level A, where it runs.
//
// The builder reuses the level-B activate family, so every mana ability that
// family can serve (its costs, fixtures and restrictions) is served here too;
// the item is only relabelled as a level-A template. The requirement list is
// the same levelb.Requirements classifier the gate uses, so a new
// activation shape cannot be missed by a hand-rolled API check.
package templates

import (
	"fmt"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// ActivateMana activates one nonland mana ability and resolves it. Its own
// version: bumping it stales only these items (they are not committed
// verdicts, but follower-check output names the version).
var ActivateMana = Template{ID: "activate-mana", Version: 1}

// manaTemplate names the level-A template for the ordinal-th mana ability
// (0-based, in IR order). The first keeps ActivateMana.ID so the common case
// reads as one template; a later one carries its ordinal because a card can
// hold several mana abilities and the item ID/template must stay unique.
// Numbering by mana-ability ordinal, not by IR slot, gives the first item the
// base id whatever slot its ability sits at.
func manaTemplate(ordinal int) (string, int) {
	if ordinal == 0 {
		return ActivateMana.ID, ActivateMana.Version
	}
	return fmt.Sprintf("%s-%d", ActivateMana.ID, ordinal), ActivateMana.Version
}

// GenerateManaAbilities returns one level-A activation item per battlefield
// mana ability on the card's front face, in IR order. It is the append step
// `oraclediff gen -mana-abilities` runs. Generate's own item is unchanged, so
// the XMage pass keeps its committed level-A bytes; a no-mana-cost mana
// source (Sol Talisman) gets its activation here instead of its skip.
//
// A skip names the one ability that could not be served. A card with no mana
// ability returns no items and no skips.
func GenerateManaAbilities(reg *cards.Registry, name string) ([]oraclegen.Item, []*oraclegen.Skip) {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return nil, []*oraclegen.Skip{{Card: name, Reason: "not in corpus"}}
	}
	f := c.Faces[0]
	if oraclegen.HasType(f, "Land") {
		// A land's intrinsic mana ability is played through play-land and
		// has no forge `AB$ Mana` requirement; the nonland source is the
		// shape follower-check could not reach.
		return nil, nil
	}
	var items []oraclegen.Item
	var skips []*oraclegen.Skip
	ordinal := 0
	for _, req := range levelb.Requirements(c) {
		if req.Face != 0 || req.Sub != "activate.mana" {
			continue
		}
		it, skip := GenerateB(reg, name, req)
		if skip != nil {
			skips = append(skips, skip)
			continue
		}
		items = append(items, relabelManaItem(it, name, ordinal))
		ordinal++
	}
	return items, skips
}

// relabelManaItem turns a level-B activate item into the level-A activation
// item. The scenario bytes stay as the activate family built them; only the
// identity and rules citation change, so the follower's step/neutral export
// is identical.
func relabelManaItem(it oraclegen.Item, name string, ordinal int) oraclegen.Item {
	template, version := manaTemplate(ordinal)
	it.Template = template
	it.ID = fmt.Sprintf("%s/%s/v%d", name, template, version)
	it.Scenario.Name = fmt.Sprintf("gen%d-%s", version, template)
	it.Scenario.CR = []string{"602.2", "605.1a"}
	it.Scenario.Why = "generated level-A scenario"
	return it
}
