package mtgish

import "github.com/adams-shaun/gorge/engine/cards"

// Mappings the orchestrator added while merging the units: pairs whose two
// sides fell to different units.

func init() {
	// Claiming an Attraction's prize: Forge's ClaimThePrize API and mtgish's
	// ClaimThePrize action.
	extAction["ClaimThePrize"] = func(e *effects, n node) { e.add("claimtheprize") }
	extAPI["ClaimThePrize"] = func(a *cards.SA, svars map[string]string) []string { return []string{"claimtheprize"} }
}
