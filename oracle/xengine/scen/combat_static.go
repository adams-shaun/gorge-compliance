package scen

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
)

// combatFixture is the creature the other seat brings to a combat key: the
// blocker of an attack key and the attacker of a block key (the level-B
// templates' own fixture, templates/combat.go). A vanilla 2/2.
const combatFixture = "Grizzly Bears"

var combatKeyRE = regexp.MustCompile(`^combat#(\d+)\.(attack|block)$`)
var staticKeyRE = regexp.MustCompile(`^static#(\d+)\.(\d+)$`)

// selfStatic reports whether a static's recipient is the card itself:
// Affected or ValidCard reads Card.Self / Creature.Self (any alternative).
func selfStatic(st *cards.Static) bool {
	for _, k := range []string{"Affected", "ValidCard"} {
		for _, alt := range strings.Split(st.Params[k], ",") {
			_, props, _ := strings.Cut(strings.TrimSpace(alt), ".")
			for _, p := range strings.Split(props, "+") {
				if p == "Self" {
					return true
				}
			}
		}
	}
	return false
}

// combatKeyStart is a level-B combat key (combat#F.attack, combat#F.block) as
// a start: the card on the battlefield since the turn began (no summoning
// sickness), and the fixture creature on the other side.
//
//	attack: the card's seat is active and the card attacks (focus attack); the
//	        fixture is the opposing seat's only creature, there to block it.
//	block:  the opposing seat is active and its fixture attacks (focus attack,
//	        seat 1); the card is the defender's only creature and may block it.
//
// The block is taken by the defending seat's own bot: the start has no block
// focus, so the line offers the block but cannot force it.
func combatKeyStart(reg *cards.Registry, c *cards.Card, card, key string, f *StartFile) string {
	m := combatKeyRE.FindStringSubmatch(key)
	if m == nil {
		return "no-generator:combat"
	}
	if m[1] != "0" {
		return "no-generator:face"
	}
	face := c.Faces[0]
	if !face.IsCreature() {
		return "no-generator:combat:noncreature"
	}
	fix, ok := reg.Lookup(combatFixture)
	if !ok || len(fix.Faces) == 0 || !fix.Faces[0].IsCreature() {
		return "no-generator:combat:fixture"
	}
	for i := range face.Statics {
		st := &face.Statics[i]
		switch {
		case m[2] == "attack" && st.Mode == "CantAttack" && selfStatic(st):
			return "no-generator:combat:cant-attack"
		case m[2] == "block" && (st.Mode == "CantBlock" || st.Mode == "CantBlockBy") && selfStatic(st):
			return "no-generator:combat:cant-block"
		}
	}
	if m[2] == "attack" && face.HasKeyword("Defender") {
		return "no-generator:combat:defender"
	}
	f.Setup.P0.Battlefield = []string{card}
	f.Setup.P1.Battlefield = []string{combatFixture}
	if m[2] == "attack" {
		f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Kind: "attack"}
		return ""
	}
	f.Active = 1
	f.Focus = &StartFocus{Seat: seatPtr(1), Card: combatFixture, Kind: "attack"}
	return ""
}

// staticKeyStart is a static key (static#F.S) whose effect reads an object
// beyond the card: a Continuous static whose Affected filter some carrier
// meets ("other creatures you control have trample"). The card is placed on the
// battlefield with one such carrier, on the seat the filter names, and the game
// plays on with no forced action: the static applies to the carrier from the
// start. The card itself is the affected object when the static reads Self.
// Every other mode, and a static no placement can satisfy, is refused with the
// mode or the condition in the reason.
func staticKeyStart(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, card, key string, f *StartFile) string {
	m := staticKeyRE.FindStringSubmatch(key)
	if m == nil {
		return "no-generator:static"
	}
	if m[1] != "0" {
		return "no-generator:face"
	}
	face := c.Faces[0]
	si, _ := strconv.Atoi(m[2])
	if si >= len(face.Statics) {
		return "no-generator:static"
	}
	st := &face.Statics[si]
	if st.ModeKind() != cards.StaticContinuous {
		return "no-generator:static:" + st.Mode
	}
	for _, k := range []string{"Condition", "CheckSVar", "IsPresent", "PresentZone", "CheckSecondSVar", "ConditionDefined"} {
		if st.Params[k] != "" {
			return "no-generator:static:conditional"
		}
	}
	if z := st.Params["AffectedZone"]; z != "" && z != "Battlefield" {
		return "no-generator:static:zone"
	}
	aff := strings.TrimSpace(st.Params["Affected"])
	if aff == "" {
		return "no-generator:static:no-affected"
	}
	// an Aura's or Equipment's bearer (gorge folds AffectedDefined into the
	// Affected filter as EnchantedBy/EquippedBy): a start file has no attach
	if strings.Contains(aff, "EnchantedBy") || strings.Contains(aff, "EquippedBy") {
		return "no-generator:static:attached"
	}
	f.Setup.P0.Battlefield = []string{card}
	if selfStatic(st) {
		return ""
	}
	name, s := filterCarrier(reg, b, card, aff, (*cards.Face).IsPermanent)
	if name == "" {
		return "no-generator:static:affected"
	}
	if s == 1 {
		f.Setup.P1.Battlefield = append(f.Setup.P1.Battlefield, name)
	} else {
		f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, name)
	}
	return ""
}
