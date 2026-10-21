package mtgish

import "github.com/adams-shaun/gorge/engine/cards"

// Mappings the orchestrator added while merging the units: pairs whose two
// sides fell to different units.

func init() {
	// Claiming an Attraction's prize: Forge's ClaimThePrize API and mtgish's
	// ClaimThePrize action.
	extAction["ClaimThePrize"] = func(e *effects, n node) { e.add("claimtheprize") }
	extAPI["ClaimThePrize"] = func(a *cards.SA, svars map[string]string) []string { return []string{"claimtheprize"} }
	// Planar-deck manipulation (Planechase), mapped onto the tokens the gorge
	// side emits for the same scripts (Dig/DigUntil over the planar deck,
	// Planeswalk, ChaosEnsues, RunChaos). C2 proposed excluding these; they
	// compare, so they are mapped instead.
	planar := map[string][]string{
		"RevealTopCardOfPlanarDeck":              {"dig:1"},
		"PutTopOfPlanarDeckOnBottomOfPlanarDeck": {"move:library>planardeck"},
		"RevealCardsFromTheTopOfPlanarDeckUntilRevealAPlaneCardThenChaosEnsuresOnThatPlaneThenPutAllCardsOnBottomInAnyOrder":               {"diguntil", "move:library>planardeck", "chaosensues"},
		"RevealCardsFromTheTopOfPlanarDeckUntilRevealAPlaneCardThenPlaneswalkToItWhileNotPlaneswalkingAwayAndPutTheRestOnBottomInAnyOrder": {"diguntil", "move:library>planardeck", "planeswalk"},
		"RevealCardsFromTheTopOfPlanarDeckUntilRevealNumberPlaneCardsAndPutAPlaneCardFromAmongOnTopAndTheRestOnBottomInAnyOrder":           {"diguntil", "move:library>planardeck"},
		"RevealCardsFromTheTopOfPlanarDeckUntilRevealNumberPlaneCardsSimultaneouslyPlaneswalkToThemThenPutOnBottomInAnyOrder":              {"diguntil", "move:library>planardeck", "planeswalk"},
		"RevealTheTopNumberCardsOfPlanarDeckAndTriggerEachCHAOSAbilityThenPutOnBottomOfPlanarDeckInAnyOrder":                               {"dig:*", "move:planardeck>planardeck", "runchaos"},
	}
	for v, toks := range planar {
		toks := toks
		extAction[v] = func(e *effects, n node) { e.add(toks...) }
	}
}
