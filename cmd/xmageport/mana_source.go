package main

import (
	"regexp"
	"strings"
)

// The runner pays a cast from the pool: a step's mana is seeded, so no land is
// tapped and no Treasure sacrificed. Two shapes make the verdict depend on
// WHICH permanents paid, and the translator cannot state that, so it refuses
// them rather than play a different game:
//
//   - mana-spent-by-source-type: a card that is live in the test counts or
//     reacts to mana "from a Treasure" / "from a Desert" spent on a spell
//     (Rain of Riches, Cataclysmic Prospecting), or to "{S}" spent on it
//     (Search for Glory, Berg Strider). Gorge tags pooled mana with its
//     source only when the source's own mana ability is activated. A land's
//     own rider on its mana ("If that mana is spent on a creature spell, it
//     gains haste", Hall of the Bandit Lord; "... can't be countered",
//     Boseiju) is the same when the emitter leaves the source to the pool: the
//     pool-seeded mana carries no such tag.
//   - tapped-target-mana-source: a battlefield permanent's "target tapped
//     nonland permanent" ability is aimed at a mana source the other seat
//     pays with (The Spear of Bashenga on Mind Stone, "tapped to cast
//     Hireling"). XMage taps the source to pay; the pool-seeded cast leaves
//     it untapped, so the target is illegal.
//
// Both scans are anchored: the mana-source text only arms when that card is
// on the battlefield or is itself cast in the test, and the tapped-target
// text must be the exact "target tapped nonland permanent" ability, so an
// unrelated "target tapped creature" card in hand cannot refuse the row.
var (
	reManaFromSourceSpent = regexp.MustCompile(`(?i)mana from an? [a-z]+ (?:was )?spent|\{S\}[^.\n]*\bspent\b|\bspent[^.\n]*\{S\}`)
	// reManaRiderSpent is a land's own rider on its mana ("If that mana is spent
	// on a creature spell, it gains haste"). The emitter pays a cast by
	// activating such a source when the cost needs it (source_payment.go), which
	// carries the rider, so it refuses only a rider source it left to the pool.
	reManaRiderSpent          = regexp.MustCompile(`(?i)\bthat mana is spent\b`)
	reTargetTappedNonlandPerm = regexp.MustCompile(`(?i)target tapped nonland permanent`)
)

// manaSourceRefusal reports the refusal for one of those two shapes, or nil.
func (t *translator) manaSourceRefusal() *refusal {
	oracleHas := func(name string, re *regexp.Regexp) bool {
		c, ok := t.lookup(name)
		if !ok {
			return false
		}
		for _, f := range c.Faces {
			if f != nil && re.MatchString(f.Oracle) {
				return true
			}
		}
		return false
	}
	hasCast := false
	for _, a := range t.actions {
		if a.kind == "cast" {
			hasCast = true
		}
	}
	var tappedSeats [maxSeats]bool
	for p := 0; p < t.seats(); p++ {
		for _, ac := range t.setup[p] {
			if !oracleHas(ac.name, reManaFromSourceSpent) &&
				!(oracleHas(ac.name, reManaRiderSpent) && !t.sourceStatedPaid(ac.name)) {
				continue
			}
			// A card that counts the mana spent is live only on the
			// battlefield (Rain of Riches) or as the spell being cast
			// (Cataclysmic Prospecting). A never-cast card in hand or a
			// graveyard does not arm the shape.
			if hasCast && (ac.zone == "battlefield" || t.castsCard(p, ac.name)) {
				return refuse("mana-spent-by-source-type", "%s reads the source of the mana spent", ac.name)
			}
		}
		for _, ac := range t.setup[p] {
			if ac.zone == "battlefield" && oracleHas(ac.name, reTargetTappedNonlandPerm) {
				tappedSeats[p] = true
			}
		}
	}
	anyTapped := false
	for _, b := range tappedSeats {
		anyTapped = anyTapped || b
	}
	if !anyTapped {
		return nil
	}
	casts := map[int]bool{}
	for _, a := range t.actions {
		if a.kind == "cast" {
			casts[a.seat] = true
		}
	}
	for _, ch := range t.choiceList {
		name, ok := strings.CutPrefix(ch.pick, "name:")
		if !ch.target || !ok {
			continue
		}
		// The pick must answer the tapped-target ability's own trigger: its
		// controller answers it, and the statement before it is an attack or
		// an activation, not a spell (a Shatter aimed at the same Mind Stone
		// is not the Spear's target).
		if !tappedSeats[ch.seat] {
			continue
		}
		if owner := t.choiceOwner(ch); owner == nil || owner.kind == "cast" || owner.kind == "play" {
			continue
		}
		for p := 0; p < t.seats(); p++ {
			for _, ac := range t.setup[p] {
				if ac.zone != "battlefield" || ac.tapped || !sameName(ac.name, name) || tappedSeats[p] || !casts[p] {
					continue
				}
				if c, ok := t.lookup(name); ok {
					for _, f := range c.Faces {
						if f != nil && !f.IsLand() && len(f.ManaAbilities()) > 0 {
							return refuse("tapped-target-mana-source", "%s is targeted and tapped to pay for the seat's cast", name)
						}
					}
				}
			}
		}
	}
	return nil
}

// castsCard reports whether the test casts or plays name for seat p.
func (t *translator) castsCard(p int, name string) bool {
	for _, a := range t.actions {
		if a.seat == p && (a.kind == "cast" || a.kind == "play") && sameName(a.card, name) {
			return true
		}
	}
	return false
}

// choiceOwner returns the last action stated before ch, the one whose ask the
// choice answers, or nil.
func (t *translator) choiceOwner(ch choice) *action {
	var owner *action
	for _, a := range t.actions {
		if a.src < ch.src && (owner == nil || a.src > owner.src) {
			owner = a
		}
	}
	return owner
}
