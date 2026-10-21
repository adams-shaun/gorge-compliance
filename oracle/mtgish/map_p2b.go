package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

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

// mayCostEffectP2B maps a MayCost cost listed in mayCostActionsP2B as the
// action it names, and reports whether it did. A MustCost ("exile it. If
// you do, ...": the action is mandatory, its "if you do" makes it a cost)
// is mapped for every action except a sacrifice, a discard or a life
// payment, which scripts write as a Cost$ or UnlessCost$ (measured).
func mayCostEffectP2B(e *effects, must bool, c any) bool {
	v := variantOf(c)
	switch {
	case must && (strings.HasPrefix(v, "Sacrifice") || v == "DiscardACard" || v == "PayLife"):
		return false
	case !must && !mayCostActionsP2B[v]:
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

// paramTokensP2B maps the second action some APIs carry in their own
// parameters: PutCounter(All)'s CounterType2$ ("a +1/+1 counter on each
// creature and a loyalty counter on each planeswalker") and a zone
// change's WithCountersType$ ("exile it with N time counters on it").
func paramTokensP2B(a *cards.SA) []string {
	p := a.Params
	var out []string
	switch a.API {
	case "PutCounter", "PutCounterAll":
		if ct := p["CounterType2"]; ct != "" {
			n := p["CounterNum2"]
			if n == "" {
				n = p["CounterNum"]
			}
			out = append(out, gorgeCounterTokens(ct, literalOr(n, "1"))...)
		}
	case "ChangeZone", "ChangeZoneAll":
		if ct := p["WithCountersType"]; ct != "" {
			out = append(out, gorgeCounterTokens(ct, literalOr(p["WithCountersAmount"], "1"))...)
		}
	}
	return out
}

// enterFlagP2B maps the counters a permanent is put onto the battlefield
// with or exiled with (EntersWith*/ExilesWith* flags), as the gorge side
// maps a zone change's WithCountersType$ (paramTokensP2B).
func enterFlagP2B(n node, v string) []string {
	switch v {
	case "EntersWithACounter", "ExilesWithACounter":
		return []string{"counters:" + counterType(rawArgs(n)) + ":1"}
	case "EntersWithNumberCounters", "ExilesWithNumberCounters":
		return []string{"counters:" + counterType(argN(n, 1)) + ":" + gameNumber(argN(n, 0))}
	}
	return nil
}
