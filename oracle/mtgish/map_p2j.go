package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Phase 2, unit j: normaliser fixes found triaging shard r3-03 (the
// leftover pending rows of units a-f, "The Celestial Toymaker" ..
// "Zoraline"). Each is a place where both encodings say the same thing and
// the two normalisers rendered it differently.

func init() {
	// A ChooseNumber's result branches: the highest, the lowest, "not the
	// lowest" and a guesser's right and wrong answers each run a
	// sub-ability (Wheel of Misfortune, The Toymaker's Trap).
	extSVarRefs["ChooseNumber"] = func(a *cards.SA) []string {
		p := a.Params
		return []string{p["ChooseNumberSubAbility"], p["Lowest"], p["Highest"], p["NotLowest"], p["NotHighest"],
			p["GuessWrong"], p["GuessCorrect"]}
	}
	// MultiplePiles: the chosen (and unchosen) pile's sub-ability (Whims of
	// the Fates sacrifices the chosen piles).
	extSVarRefs["MultiplePiles"] = func(a *cards.SA) []string {
		return []string{a.Params["ChosenPile"], a.Params["UnchosenPile"]}
	}
	// "Pay any amount of life": the payment is the loss, as the script's
	// Cost$ PayLife<X> reads (Vizkopa Confessor).
	extAction["PayAnyAmountOfLife"] = func(e *effects, _ node) { e.add("loselife:*") }
	// "Your opponents can't block with creatures with ...": S:Mode$
	// CantBlockBy naming only blockers, rule:cantblock (Void Winnower).
	d2PlayerSynonyms["CantBlockWithCreatures"] = "rule:cantblock"
}

// p2jParamTokens maps parameters of an SA that gorgeAPIEffects does not
// read.
func p2jParamTokens(a *cards.SA) []string {
	p := a.Params
	var out []string
	switch a.API {
	case "AddTurn":
		if p["SkipUntap"] == "True" {
			// "Skip the untap step of that turn."
			out = append(out, "player:skipsuntapstep")
		}
	case "Animate", "AnimateAll":
		if p["RemoveNonManaAbilities"] == "True" {
			// "Loses all abilities except mana abilities."
			out = append(out, "loseabilities")
		}
	case "MakeCard":
		if ct := p["WithCounter"]; ct != "" {
			// "Conjure a card onto the battlefield with eight charge
			// counters on it."
			out = append(out, gorgeCounterTokens(ct, literalOr(p["WithCounterNum"], "1"))...)
		}
	}
	return out
}

// p2jCloneExtras maps a Clone inside an as-enters replacement: "enter
// tapped as a copy", "it gains haste until end of turn".
func p2jCloneExtras(a *cards.SA) []string {
	var out []string
	if a.Params["IntoPlayTapped"] == "True" {
		out = append(out, "etbtapped")
	}
	for _, k := range cards.SplitKeywordList(a.Params["PumpKeywords"]) {
		out = append(out, "grant:"+grantKeyword(k))
	}
	return out
}

// p2jEffectRepl maps an Effect's "if a card would be put into a graveyard
// from anywhere this turn, exile it instead" (Yawgmoth's Will, Forgotten
// Cellar): mtgish's CreateReplaceWouldPutIntoGraveyardUntil, one unnamed
// effect. A replacement bound to the cards the Effect lets you cast
// (Origin$ Stack, or ValidCard$ the remembered cards) is part of that
// permission in mtgish, not an action of its own.
func p2jEffectRepl(r cards.Repl) []string {
	p := r.Params
	if r.Event == "Moved" && p["Destination"] == "Graveyard" && p["Origin"] == "" && p["ReplaceWith"] != "" &&
		!strings.Contains(p["ValidCard"], "IsRemembered") && !strings.Contains(p["ValidLKI"], "IsRemembered") &&
		!strings.Contains(p["ValidLKI"], "MayPlaySource") {
		return []string{"effect"}
	}
	return nil
}
