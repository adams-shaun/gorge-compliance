package scen

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// reasonDayNight names a start the lowering cannot restate: the day/night
// designation (CR 731) is set by the game itself (a daybound or nightbound
// permanent entering, "it becomes day/night"), and the lowered digest refuses
// Game.DayNight (lower/gate_table.go) because neither the Forge nor the XMage
// follower digest exports it. Play then wrote status refused for every seed
// and keyproof read the unlowered start as start-not-exercised. Naming the
// reason here keeps the key owed under its own class.
const reasonDayNight = "start:day-night"

// setsDayNight reports whether any face of the card gives the game a
// day/night designation when the card is on the battlefield or resolves:
// Daybound / Nightbound (CR 702.145), or a DayTime effect (an ability or an
// SVar sub-ability, "it becomes night"). A card that only reads the
// designation (Count$Night, a DayTime$ replacement gate) is not matched: it
// leaves the game with neither day nor night.
func setsDayNight(c *cards.Card) bool {
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		if f.HasKeyword("Daybound") || f.HasKeyword("Nightbound") {
			return true
		}
		for _, sa := range f.Abilities {
			if sa != nil && sa.API == "DayTime" {
				return true
			}
		}
		for _, v := range f.SVars {
			if strings.Contains(v, "DB$ DayTime") {
				return true
			}
		}
	}
	return false
}

// dayNightRefusal returns reasonDayNight when the finished start would set
// the designation: the focus card, or any permanent either seat starts with.
// It reads the placements, not the key, so every recipe that builds a start
// (cast, activate, trigger, static, combat; a fixture or target the start
// placed) is covered by the one call.
func dayNightRefusal(reg *cards.Registry, f *StartFile) string {
	names := slices.Concat(f.Setup.P0.Battlefield, f.Setup.P1.Battlefield)
	if f.Focus != nil {
		names = append(names, f.Focus.Card)
	}
	for _, n := range names {
		if c, ok := reg.Lookup(n); ok && setsDayNight(c) {
			return reasonDayNight
		}
	}
	return ""
}
