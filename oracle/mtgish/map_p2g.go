package mtgish

import (
	"fmt"
	"strings"
)

// p2gPlayerEffect maps the PlayerEffect shapes unit g found rendered with
// the wrong verb; ok false falls through to the generic player-effect token.
//
// OnceEachTurnMayAction wrapping a cast "for alternate cost" ("once during
// each of your turns, you may cast a spell from your hand by paying life
// equal to its mana value") is an alternative cost, as gorge's MayPlay
// static with MayPlayAltManaCost$ (mayPlayP2E).
func p2gPlayerEffect(x any) ([]string, bool) {
	if variantOf(x) == "OnceEachTurnMayAction" && strings.Contains(fmt.Sprint(x), "ForAlternateCost") {
		return []string{"alternativecost"}, true
	}
	return nil, false
}

// p2gMayPlay is mayPlayP2E, except that a MayPlay carrying flash beside an
// alternative or free cost stays "mayplay": mtgish splits those across a
// flash and an alternative-cost effect in ways one token cannot match, and
// the earlier-triaged rows for them keep their keys.
func p2gMayPlay(p map[string]string) string {
	if p["MayPlayWithFlash"] == "True" && (p["MayPlayAltManaCost"] != "" || p["MayPlayWithoutManaCost"] == "True") {
		return "mayplay"
	}
	return mayPlayP2E(p)
}
