package mtgish

// Phase 2, unit b: residual T4 effect classes found triaging the
// alphabetical second third of the corpus.

// mayCostActionsP2B are the costs of a "you may <X>. If you do, ..."
// (mtgish MayCost/MustCost) that Forge's cost grammar has no cost for, so
// a script always writes X as an effect: putting a card from hand, a
// graveyard, exile or the library onto the battlefield or into a hand,
// looking, searching, destroying, transforming, untapping another
// permanent, creating tokens or a continuous effect. Those are mapped as
// the action. A cost Forge can write as a cost (sacrifice, exile, discard,
// pay life or energy, tap, return to hand, counters, reveal, mill, draw)
// stays unmapped on both sides: scripts write it as a Cost$ or UnlessCost$,
// which the gorge side does not compare either (measured: mapping those
// adds more rows than it removes).
var mayCostActionsP2B = map[string]bool{
	"PutACardFromHandOnBattlefield":          true,
	"PutACardFromGraveyardIntoHand":          true,
	"PutGraveyardCardOntoBattlefield":        true,
	"PutACardOfTypeMilledThisWayIntoHand":    true,
	"PutExiledCardOntoBattlefield":           true,
	"PutGraveyardCardOnTopOfLibrary":         true,
	"PutTopCardOfLibraryOfTypeOnBattlefield": true,
	"LookAtTopOfLibrary":                     true,
	"SearchLibrary":                          true,
	"DestroyPermanent":                       true,
	"TransformPermanent":                     true,
	"UntapPermanent":                         true,
	"CreateTokens":                           true,
	"CreatePermanentLayerEffectUntil":        true,
	"AbandonScheme":                          true,
}

// mayCostEffectP2B maps a MayCost/MustCost cost listed in
// mayCostActionsP2B as the action it names, and reports whether it did.
func mayCostEffectP2B(e *effects, c any) bool {
	if !mayCostActionsP2B[variantOf(c)] {
		return false
	}
	sub := &effects{}
	sub.costAsAction(c)
	for _, t := range sub.effects {
		if IsUnmapped(t) {
			return false
		}
	}
	if len(sub.effects) == 0 {
		return false
	}
	e.add(sub.effects...)
	return true
}

// replCanonP2B folds mtgish's spellings of one replaced event into the
// token the gorge side uses: Forge has one CreateToken replacement event
// whichever way the Oracle text words it ("an effect would create one or
// more tokens", "one or more tokens would be created", "a player would
// create a token").
var replCanonP2B = map[string]string{
	"replace:anynumberoftokenswouldbecreated": "replace:aneffectwouldcreateanynumberoftokens",
	"replace:aplayerwouldcreatetokens":        "replace:aneffectwouldcreateanynumberoftokens",
	"replace:aplayerwouldcreateatoken":        "replace:aneffectwouldcreateanynumberoftokens",
}
