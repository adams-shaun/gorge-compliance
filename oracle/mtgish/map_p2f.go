package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Phase-2 unit f: normaliser fixes found triaging the residual rows of the
// corpus from "Raffine's Guidance" to the end of the alphabet. Each family
// is general (corpus-wide), not per card.

// p2fSearchDelivery folds the delivery of a search that parks its cards in
// the library: "search your library for a card, reveal it, then put it
// into your hand or onto the battlefield (by a condition, an opponent's
// pick, a payment)". Scripts search with Destination$ Library (or with
// no Destination$, leaving the cards where they are), remember
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
		if t == "search:library" || t == "search:" {
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

// p2fLookStays are the look/reveal steps that leave the cards in the
// library: shuffled back in ("then shuffle the rest into your library") or
// left on top in the same order. Neither is a move, as the gorge side reads
// a DigUntil shuffled back (p2dDigUntilShuffledBack), a Dig whose rest a
// following Shuffle shuffles (p2dDigRestStays), and a PeekAndReveal.
var p2fLookStays = map[string]bool{
	"PutTheRemainingCardsBackIntoLibraryAndShuffle":      true,
	"LeaveTheRemainingCardsOnTopOfLibraryInTheSameOrder": true,
	"LeaveRemainingCardsOnTopOfLibraryInSameOrder":       true,
}

// p2fShuffledAfter reports a DigUntil whose revealed cards go back into
// the library that its next sub-ability shuffles ("then shuffle the rest
// into your library", the Shuffle written as its own SA): a shuffle, not a
// move, as p2dDigUntilShuffledBack reads Shuffle$ True.
func p2fShuffledAfter(a *cards.SA, dest string) bool {
	return dest == "Library" && a.Sub != nil && a.Sub.API == "Shuffle"
}

// p2fParamTokens maps parameters that carry an effect of their own.
func p2fParamTokens(a *cards.SA) []string {
	p := a.Params
	var out []string
	switch a.API {
	case "PutCounter", "PutCounterAll":
		if p["RemovePhase"] != "" {
			// "Remove those counters at the beginning of the next end
			// step": a delayed trigger removing them, as mtgish's
			// CreateFutureTrigger(RemoveCounters).
			out = append(out, "delayed", "countersremove")
		}
	case "ChangeZone":
		if p2fThreeZoneSearch(p["Origin"]) && p["Defined"] == "" {
			// "Search its owner's graveyard, hand and library for ... and
			// exile them" written as one hidden ChangeZone over the three
			// zones (read as the search): the graveyard and the hand are
			// moves too, as mtgish's SearchPlayersLibraryAndGraveyardAndHand
			// (map_c2.go) and the scripts that split it per zone.
			d := strings.ToLower(p["Destination"])
			out = append(out, "move:graveyard>"+d, "move:hand>"+d)
		}
	case "DigUntil":
		if d := p["OptionalNoDestination"]; d != "" && p["OptionalFoundMove"] == "True" {
			// "You may put that card onto the battlefield. If you don't,
			// put it into your hand": the declined card moves too.
			out = append(out, "move:library>"+strings.ToLower(d))
		}
	}
	return out
}

func init() {
	// A one-card ChangeZone whose target is a spell or a permanent
	// ("return target spell or nonland permanent"): both origins, as
	// mtgish writes one return per zone (p2dOriginChoice). Graveyard,Exile
	// is not one: scripts list Exile beside the graveyard for cards a
	// replacement exiled (measured: 64 rows more).
	for _, o := range []string{"Battlefield,Stack", "Stack,Battlefield"} {
		p2dOriginChoice[o] = true
	}
	// mtgish's one action for the same choice: the card comes from the
	// stack or the battlefield, one of the two (map_c2.go had move:*>hand).
	extAction["ReturnSpellOrPermanentToOwnersHand"] = func(e *effects, _ node) {
		e.merge([]*effects{{effects: []string{"move:battlefield>hand"}}, {effects: []string{"move:stack>hand"}}})
	}
}

// p2fThreeZoneSearch reports an Origin$ of exactly the graveyard, the hand
// and the library, in any order.
func p2fThreeZoneSearch(o string) bool {
	zs := map[string]bool{}
	for _, z := range strings.Split(o, ",") {
		zs[strings.TrimSpace(z)] = true
	}
	return len(zs) == 3 && zs["Graveyard"] && zs["Hand"] && zs["Library"]
}

func init() {
	// "If the copy is a permanent spell, it gains haste": the condition
	// wraps the copy's modifiers (SpellEffect.IfPermanentSpell); the
	// ability gained is the same grant (c1CopyWithEffects).
	for _, v := range []string{"CopySpellWithModifiers", "CopySpellAndMayChooseNewTargetsWithEffects"} {
		extAction[v] = func(e *effects, n node) {
			a := argsOf(n)
			if len(a) < 2 {
				c1CopyWithEffects(e, n)
				return
			}
			var mods []any
			for _, x := range list(a[1]) {
				if variantOf(x) == "IfPermanentSpell" {
					mods = append(mods, argsOf(x)...)
					continue
				}
				mods = append(mods, x)
			}
			c1CopyWithEffects(e, node{"_Action": variantOf(n), "args": []any{a[0], mods}})
		}
	}
}

// p2fDamageEach repeats a damage token once per recipient of a
// MultipleRecipients ("deals X damage to any other target and X damage to
// itself"): scripts deal each recipient's damage with its own DealDamage.
// A group recipient ("each creature and each player") is one DamageAll.
func p2fDamageEach(recipient any, tok string) []string {
	n := 1
	if variantOf(recipient) == "MultipleRecipients" {
		rs := argsOf(recipient)
		single := len(rs) > 1
		for _, r := range rs {
			if strings.HasPrefix(variantOf(r), "Each") {
				// "Each creature and each player": one DamageAll.
				single = false
			}
		}
		if single {
			n = len(rs)
		}
	}
	out := make([]string, n)
	for i := range out {
		out[i] = tok
	}
	return out
}

func init() {
	// "Enters with your choice of a flying counter or a hexproof counter":
	// the listed counters as alternatives, as a script's PutCounter over
	// CounterType$ Flying,Hexproof lists each (map_d2.go had counters:*).
	extEnter["EntersWithACounterOfChoice"] = func(x any) []string {
		var subs []*effects
		for _, c := range argsOf(x) {
			subs = append(subs, &effects{effects: []string{"counters:" + counterType(c) + ":1"}})
		}
		e := &effects{}
		e.merge(subs)
		return e.effects
	}
}

func init() {
	// Actions inside a look or reveal that are shaped as the _Action of the
	// same name ("if an artifact is revealed, put it onto the battlefield
	// and sacrifice this artifact"; "you lose life equal to ...").
	for _, v := range []string{"SacrificePermanent", "LoseLife", "GainLife", "TransformPermanent"} {
		lookActionP2B[v] = true
	}
}

// p2fCDATypes maps a CDA_Types rule other than changeling. A fixed type
// list ("is also a Cleric, Rogue, Warrior, and Wizard", "is every creature
// type") is a characteristic-defining ability, which scripts flag
// CharacteristicDefining$ (the gorge side's "cda"); a chosen type ("is
// the chosen type in addition", a type variable) stays a type-changing
// static.
func p2fCDATypes(x any) string {
	if strings.Contains(variantOf(x), "Variable") {
		return "type"
	}
	return "cda"
}

func init() {
	// "You may activate loyalty abilities of it any time you could cast an
	// instant": a timing permission, which scripts write as the
	// planeswalker's flash-speed activation static (the gorge side's
	// "flash"), not a permission to play a card.
	for _, v := range []string{
		"MayActivateLoyaltyAbilityOfPlaneswalkerAnyTimeTheyCouldCastAnInstant",
		"MayActivateLoyaltyAbilityOfPlaneswalkerDuringEachPlayersTurnAndAnyTimeTheyCouldCastAnInstant",
		"MayActivateLoyaltyAbilityOfPlaneswalkersDuringEachPlayersTurnAndAnyTimeTheyCouldCastAnInstant",
	} {
		d2PlayerSynonyms[v] = "flash"
	}
	// "They can't phase in until ...": the game effect, as the static.
	extAction["CreateGameEffect"] = func(e *effects, n node) {
		if v := variantOf(argN(n, 1)); v == "PermanentsCantPhaseIn" || v == "PermanentCantPhaseIn" {
			e.add(tokCantPhaseIn)
			return
		}
		e.add(gameEffectToken(argN(n, 1)))
	}
}
