package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Phase 2, unit c of the mtgish cross-check: normaliser fixes found
// triaging the T4 rows of the third shard (cards Giant Opportunity ..
// Quintorius, Loremaster). Each fix is a place where both encodings say the
// same thing and the two normalisers rendered it differently.

// p2cMustCost summarises a cost the player must pay ("sacrifice two
// Swamps. If you can't, ..."; mtgish's MustCost). Paying it is done, so it
// is the action, as EachPlayerMustCost and PlayerMustCost are; a payment of
// mana or energy is a payment and maps to nothing on both sides.
func (e *effects) p2cMustCost(c any) {
	switch variantOf(c) {
	case "PayEnergy":
	case "UnspecializeGraveyardCard":
		// Alchemy specialize, undone from the graveyard: gorge's
		// SetState Mode$ Unspecialize.
		e.add("setstate:unspecialize")
	case "AnteAPermanent":
		e.add("move:battlefield>ante")
	case "ShuffleACardFromHandIntoLibrary":
		e.add("move:hand>library:1")
	default:
		e.costAsAction(c)
	}
}

// p2cCost is the gorge side of p2cMustCost: a resolving sub-ability's
// Cost$ (or UnlessCost$) written "Mandatory <cost>" is a cost its controller
// must pay when it resolves ("sacrifice it. If you do, ..."), so the parts
// that are actions count as actions. An optional cost ("you may sacrifice
// ...") stays a cost, as mtgish's MayCost.
func (g *gorgeNorm) p2cCost(e *effects, a *cards.SA, depth int) {
	if depth == 0 {
		return
	}
	for _, k := range []string{"Cost", "UnlessCost"} {
		c, ok := strings.CutPrefix(a.Params[k], "Mandatory ")
		if !ok {
			continue
		}
		e.add(p2cCostActions(c)...)
	}
}

// p2cCostActions maps the action parts of a Forge cost string.
func p2cCostActions(c string) []string {
	var out []string
	for _, part := range strings.Fields(c) {
		head, _, _ := strings.Cut(part, "<")
		switch head {
		case "Sac":
			out = append(out, "sacrifice")
		case "PayLife":
			n := "*"
			if _, rest, ok := strings.Cut(part, "<"); ok {
				n = literalOr(strings.TrimSuffix(rest, ">"), "*")
			}
			out = append(out, "loselife:"+n)
		case "Exile":
			out = append(out, "move:battlefield>exile")
		case "ExileFromGrave":
			out = append(out, "move:graveyard>exile")
		case "Discard":
			out = append(out, "discard:*")
		}
	}
	return out
}
