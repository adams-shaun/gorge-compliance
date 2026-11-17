package main

import (
	"regexp"
	"strings"
)

// noteShape records the first shape refusal found while building a scenario.
// It converts a failing run only (translate: res.shape), so a test of the same
// shape that passes stays covered.
func (t *translator) noteShape(r *refusal) {
	if t.emitShape == nil {
		t.emitShape = r
	}
}

// permittingObjectPick reports whether pick, the first setChoice after a cast
// of card, names another card of seat's (on the battlefield, or in hand to be
// cast first) that permits the cast: card starts in a graveyard, exile or the
// library, and XMage's choice picks which of several permissions (Karador,
// Gisa and Geralf) it spends. Gorge lists the card once
// per permission under identical "Cast X" labels, so no scenario field can
// say which one, and the runner takes the first.
func (t *translator) permittingObjectPick(seat int, card, pick string) bool {
	name, ok := strings.CutPrefix(pick, "name:")
	if !ok {
		return false
	}
	fromZone, permitting := false, false
	for _, pl := range t.places[seat] {
		switch {
		case sameName(pl.name, card) && (pl.zone == "graveyard" || pl.zone == "exile" || pl.zone == "library_top"):
			fromZone = true
		case sameName(pl.name, name) && (pl.zone == "battlefield" || pl.zone == "hand") && !sameName(pl.name, card):
			permitting = true
		}
	}
	return fromZone && permitting
}

// reManaAbilityCounter matches a mana ability that puts a counter on the way:
// "{T}, put a counter: add" (Empowered Autogenerator) or the counter as the
// whole cost, "Put a -0/-1 counter on Wall of Roots: Add {G}". The counter is
// a side effect of paying.
var reManaAbilityCounter = regexp.MustCompile(`(?is)\{T\}[^\n]*\bput an? [^.\n]*counter\b[^\n]*\badd\b|(?:^|\\n)put an? [^.\n:\\]*counter[^.\n:\\]*:\s*add\b`)

// manaAbilityPutsCounter reports whether the named card's own mana ability
// puts a counter on it. The runner pays a cast from the pool, so that ability
// is never activated and an assert on the counter can only fail.
func (t *translator) manaAbilityPutsCounter(name string) bool {
	c, ok := t.lookup(name)
	if !ok {
		return false
	}
	for _, f := range c.Faces {
		if f != nil && reManaAbilityCounter.MatchString(f.Oracle) {
			return true
		}
	}
	return false
}
