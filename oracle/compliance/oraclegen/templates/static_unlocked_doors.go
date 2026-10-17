package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// staticUnlockedDoorRooms are the Room cards the unlocked-doors fixture casts
// one door of each: neither door cast has an unlock trigger or an enters
// effect that asks anything (Unholy Annex's end-step trigger and Prop Room's
// untap static act later or never during the scenario), so each cast unlocks
// exactly one door and poses nothing (CR 709.5).
var staticUnlockedDoorRooms = []struct{ card, door string }{
	{"Unholy Annex // Ritual Chamber", "Unholy Annex"},
	{"Dazzling Theater // Prop Room", "Prop Room"},
}

// staticUnlockedDoorsFixture is the fixture for a static whose count reads
// the unlocked doors among Rooms p0 controls (Rampaging Soulrager's
// Count$UnlockedDoors, "two or more unlocked doors"): a Room placed by setup
// has no unlocked door in either engine, so the prelude casts one door of
// each fixture Room.
func staticUnlockedDoorsFixture(reg *cards.Registry, f *cards.Face, st cards.Static) (staticFixture, bool) {
	reads := false
	for _, body := range staticSVarBodies(f, st) {
		reads = reads || strings.Contains(body, "Count$UnlockedDoors")
	}
	if !reads {
		return staticFixture{}, false
	}
	var pre conditionPrelude
	for _, r := range staticUnlockedDoorRooms {
		c, ok := reg.Lookup(r.card)
		if !ok {
			return staticFixture{}, false
		}
		var mana string
		for _, face := range c.Faces {
			if face.Name == r.door {
				pool, why := oraclegen.PoolFor(face.ManaCost)
				if why != "" {
					return staticFixture{}, false
				}
				mana = pool
			}
		}
		if mana == "" {
			return staticFixture{}, false
		}
		pre.hand = append(pre.hand, r.card)
		pre.steps = append(pre.steps,
			oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + r.door, Mana: mana},
			oraclegen.Step{Op: "resolve"})
	}
	return staticFixture{conditionPrelude: pre}, true
}
