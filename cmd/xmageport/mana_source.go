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
//   - mana-spent-by-source-type: a card counts or reacts to mana "from a
//     Treasure" / "from a Desert" spent on a spell (Rain of Riches,
//     Cataclysmic Prospecting). Gorge tags pooled mana with its source only
//     when the source's own mana ability is activated.
//   - tapped-target-mana-source: a card targets a tapped permanent and the
//     test aims it at a mana source the other seat pays with (The Spear of
//     Bashenga on Mind Stone, "tapped to cast Hireling"). XMage taps the
//     source to pay; the pool-seeded cast leaves it untapped, so the target
//     is illegal.
var (
	reManaFromSourceSpent = regexp.MustCompile(`(?i)mana from an? [a-z]+ (?:was )?spent`)
	reTargetTapped        = regexp.MustCompile(`(?i)target tapped`)
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
	tappedTargets := false
	for p := 0; p < 2; p++ {
		for _, ac := range t.setup[p] {
			if oracleHas(ac.name, reManaFromSourceSpent) {
				return refuse("mana-spent-by-source-type", "%s reads the source of the mana spent", ac.name)
			}
			tappedTargets = tappedTargets || oracleHas(ac.name, reTargetTapped)
		}
	}
	if !tappedTargets {
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
		for p := 0; p < 2; p++ {
			for _, ac := range t.setup[p] {
				if ac.zone != "battlefield" || ac.tapped || !sameName(ac.name, name) || !casts[p] {
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
