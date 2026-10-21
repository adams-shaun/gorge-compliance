package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Phase-2 unit f: normaliser fixes found triaging the residual rows of the
// corpus from "Raffine's Guidance" to the end of the alphabet. Each family
// is general (corpus-wide), not per card.

// extActionList holds hooks the walker runs over every mtgish action list
// (Actions.ActionList) before descending into it: patterns that read one
// action together with its later siblings.
var extActionList []func(e *effects, l []any)

// p2fHasVariant reports whether variant v occurs anywhere under x.
func p2fHasVariant(x any, v string) bool {
	found := false
	walk(x, func(n node, enum, variant string) bool {
		if variant == v {
			found = true
		}
		return !found
	})
	return found
}

func init() {
	// "You may exile <permanent>. If you do, return it to the battlefield"
	// (a flicker): mtgish writes the exile as a MayCost, which stays a
	// cost elsewhere (map_p2d.go), but a flicker's exile is the first half
	// of the effect, and every script writes it as an optional ChangeZone
	// (measured: no flicker script pays it as a Cost$/UnlessCost$). So a
	// MayCost exile whose later sibling puts the exiled card back onto
	// the battlefield is the exile.
	extActionList = append(extActionList, p2fFlicker)
}

// p2fFlicker is the flicker hook over one action list, and over the
// action lists an If/Unless in it carries (they are bare arrays, not
// ActionList nodes, so the walker never hooks them itself).
func p2fFlicker(e *effects, l []any) {
	for i, a := range l {
		if enumOf(a) == "Action" && (variantOf(a) == "If" || variantOf(a) == "Unless") {
			for _, x := range argsOf(a) {
				if sub := list(x); sub != nil && enumOf(sub[0]) == "Action" {
					p2fFlicker(e, sub)
				}
			}
		}
		if enumOf(a) != "Action" || variantOf(a) != "MayCost" {
			continue
		}
		c := rawArgs(a)
		if !strings.HasPrefix(variantOf(c), "Exile") {
			continue
		}
		back := false
		for _, b := range l[i+1:] {
			if p2fHasVariant(b, "PutExiledCardOntoBattlefield") {
				back = true
			}
		}
		if back {
			e.costAsAction(c)
		}
	}
}

// p2fSearchDelivery folds the delivery of a search that parks its cards in
// the library: "search your library for a card, reveal it, then put it
// into your hand or onto the battlefield (by a condition, an opponent's
// pick, a payment)". Scripts search with Destination$ Library, remember
// the found cards, then move them out with later ChangeZones over the
// remembered or chosen cards; mtgish writes one SearchLibrary whose steps
// say where the cards go (search:*, or its destination). The later moves
// from the library are the search's own delivery, not new moves (nor new
// searches: a ChangeZone an opponent picks for reads as one).
func (g *gorgeNorm) p2fSearchDelivery(a *cards.SA, toks []string) []string {
	if a.API != "ChangeZone" && a.API != "ChangeZoneAll" {
		return toks
	}
	p := a.Params
	if g.parked && p2fDelivers(p) {
		var out []string
		for _, t := range toks {
			if !strings.HasPrefix(t, "move:library>") && !strings.HasPrefix(t, "search:") {
				out = append(out, t)
			}
		}
		return out
	}
	for _, t := range toks {
		if t == "search:library" {
			g.parked = true
		}
	}
	return toks
}

// p2fDelivers reports whether a ChangeZone moves the cards an earlier
// search parked: from the library, by the remembered or chosen cards.
func p2fDelivers(p map[string]string) bool {
	if !strings.EqualFold(strings.TrimSpace(strings.SplitN(p["Origin"], ",", 2)[0]), "Library") {
		return false
	}
	switch p["Defined"] {
	case "Remembered", "ChosenCard", "ChosenCards", "Imprinted":
		return true
	}
	return strings.Contains(p["ChangeType"], "IsRemembered")
}

// p2fReflexiveEnter maps the reflexive half of an "as it enters" copy
// choice ("you may have it enter as a copy of X. When you do, exile X"):
// the actions the reflexive trigger runs, which a script writes as a
// sub-ability of its Clone replacement.
func p2fReflexiveEnter(x any) []string {
	if !strings.HasPrefix(variantOf(x), "Reflexive_") {
		return nil
	}
	sub := &effects{}
	for _, a := range argsOf(x) {
		if enumOf(a) == "Actions" {
			sub.walk(a)
		}
	}
	return sub.effects
}

// p2fMayCostOr maps "you may <A> or <B>. If you do, ..." as the
// alternatives when every alternative is an action a script writes as an
// effect (mayCostActionsP2B), as one MayCost of either would be.
func p2fMayCostOr(e *effects, c any) bool {
	alts := argsOf(c)
	if len(alts) == 0 {
		return false
	}
	for _, a := range alts {
		if !mayCostActionsP2B[variantOf(a)] {
			return false
		}
	}
	sub := &effects{}
	sub.alternativeCosts(alts)
	for _, t := range sub.effects {
		if IsUnmapped(t) {
			return false
		}
	}
	e.add(sub.effects...)
	return len(sub.effects) > 0
}

func init() {
	// "From your graveyard onto the battlefield", the graveyard twin of
	// PutACardFromHandOnBattlefield: a script writes it as a ChangeZone.
	mayCostActionsP2B["PutACardFromPlayersGraveyardOnBattlefield"] = true
}
