package mtgish

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Phase-2 unit h: normaliser fixes found triaging shard r3-01 (Death or
// Glory .. Maralen of the Mornsong Avatar). Each family is general.

// p2hCount reads a literal repeat count ("2"); 1 for anything else.
func p2hCount(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// p2hParamTokens adds the tokens of parameters gorgeAPIEffects does not
// read: a repeated verb written as a count.
func p2hParamTokens(a *cards.SA) []string {
	p := a.Params
	var out []string
	switch a.API {
	case "Explore":
		// "Explores, then explores again": Num$ 2 is two explores, as
		// mtgish's two actions.
		for i := 1; i < p2hCount(p["Num"]); i++ {
			out = append(out, "explore")
		}
	case "SkipTurn":
		for i := 1; i < p2hCount(p["NumTurns"]); i++ {
			out = append(out, "skipturn")
		}
	case "Pump":
		if p["DefinedLandwalk"] != "" {
			out = append(out, "grant:landwalk")
		}
	}
	return out
}

// p2hPlayerEffect maps the PlayerEffect variants that carry actions: "you
// may play the exiled card; when you do, <actions>" is a play permission
// and a delayed trigger running them, as the gorge side's Effect with a
// StaticAbilities$ MayPlay and a Triggers$.
func p2hPlayerEffect(x any) ([]string, bool) {
	if variantOf(x) != "MayPlayExiledCardWithTrigger" {
		return nil, false
	}
	sub := &effects{}
	sub.walk(argN(x, 1))
	return append([]string{"mayplay", "delayed"}, sub.effects...), true
}

// p2hAllAddAbility reports a layer-effect list made only of granted
// abilities.
func p2hAllAddAbility(ls []any) bool {
	for _, l := range ls {
		if variantOf(l) != "AddAbility" {
			return false
		}
	}
	return len(ls) > 0
}
