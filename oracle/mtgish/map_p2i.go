package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Phase-2 unit i: normaliser fixes found triaging the residual rows of the
// third round (shard r3-02: "Mayael" to "The Belligerent"). Each family is
// general (corpus-wide), not per card.

// p2iUnlessPaidByYou reports whether an UnlessCost$ is paid by the ability's
// own controller, so its action half counts as an action (p2cCost). The
// payer is you when UnlessPayer$ is absent or You, except on a Counter: a
// "counter target spell unless its controller pays" is paid by the
// spell's controller (mtgish's PlayerMayCost, which stays a cost). A
// TriggeredPlayer is you when the trigger is "your upkeep" or the like
// (ValidPlayer$ You).
func p2iUnlessPaidByYou(g *gorgeNorm, a *cards.SA, payer string) bool {
	switch payer {
	case "":
		return a.API != "Counter"
	case "You":
		return true
	case "TriggeredPlayer":
		return g.trigYou
	}
	return false
}

// p2iReplCounters maps an Effect's replacement "it enters with an
// additional counter on it" (Event$ Moved to the battlefield, ReplaceWith$ a
// PutCounter ETB$ True chain): the counters, as mtgish's EntersWithACounter
// enter flags (Recommission, Semester's End, Teferi's Time Twist).
func p2iReplCounters(a *cards.SA, r cards.Repl, svars map[string]string) []string {
	if r.Event != "Moved" || r.Params["Destination"] != "Battlefield" || !strings.Contains(r.Params["ValidCard"], "IsRemembered") {
		// (A replacement over cards the effect did not name -- "creatures
		// you control enter with an additional counter this turn" -- is
		// mtgish's generic replacement effect, the "effect" token.)
		return nil
	}
	if strings.HasPrefix(a.Params["RememberObjects"], "Triggered") {
		// "That spell enters with ..." (a cast trigger's card): mtgish's
		// generic replacement effect too.
		return nil
	}
	var out []string
	for sa, depth := cards.ResolveSVar(svars, r.Params["ReplaceWith"]), 0; sa != nil && depth < 6; depth++ {
		if sa.API == "PutCounter" && sa.Params["ETB"] == "True" && sa.Params["CounterType"] != "" {
			out = append(out, gorgeCounterTokens(sa.Params["CounterType"], literalOr(sa.Params["CounterNum"], "1"))...)
		}
		sub := sa.Params["SubAbility"]
		if sub == "" {
			break
		}
		sa = cards.ResolveSVar(svars, sub)
	}
	return out
}

// p2iLookCost adds the payment of a look step whose move is conditional on
// a cost ("for each card, put it into your graveyard unless you pay 2
// life"): the cost is done while the spell resolves (gorge: the
// sub-ability's UnlessCost$, p2cCost).
func (e *effects) p2iLookCost(n node, variant string) {
	if variant == "ForEachCardPutIntoGraveyardUnlessCost" {
		e.p2cMayCost(rawArgs(n))
	}
}

// p2iTokenFlags maps the counters a token enters with (the flags of
// CreateTokens: "The token enters with X +1/+1 counters on it"; gorge: the
// Token's WithCountersType$).
func (e *effects) p2iTokenFlags(flags any) {
	for _, f := range list(flags) {
		e.add(p2cEnterFlag(node{"_TokenFlag": variantOf(f), "args": rawArgs(f)}, variantOf(f))...)
	}
}

func init() {
	// "Discard a card for each card drawn unless you sacrifice a permanent":
	// the repeated cost is paid, so its action counts (a MayCost).
	extAction["RepeatedMayCost"] = func(e *effects, n node) {
		e.p2cMayCost(argN(n, 1))
	}
}

func init() {
	// "Pay any amount of life, then look at that many cards": the payment is
	// done (gorge: LoseLife / the sub-ability's PayLife).
	extAction["PayAnyAmountOfLife"] = func(e *effects, n node) { e.add("loselife:*") }
}
