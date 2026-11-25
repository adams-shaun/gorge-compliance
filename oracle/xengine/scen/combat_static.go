package scen

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
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

// attackModes and blockModes are the static modes that can keep an attack or a
// block from being offered or taken. A Must* mode is absent: it forces what the
// key does anyway.
var attackModes = map[string]bool{"CantAttack": true, "CantAttackUnless": true, "AttackRestrict": true}
var blockModes = map[string]bool{"CantBlock": true, "CantBlockBy": true, "CantBlockUnless": true,
	"BlockRestrict": true, "MinMaxBlocker": true, "CanBlockIfShadow": true}

// restrictsCombat reports whether st may keep the key's own action from being
// offered: an attack restriction that can cover the attacker (the card for an
// attack key, the fixture for a block key), or a block restriction that can
// cover a blocker or the attacker it blocks (a block key; an attack key's
// fixture may block or not). Its Affected, ValidCard, ValidAttacker and
// ValidBlocker filters are read alternative by alternative: one this start
// cannot show to miss the creatures (or does not understand, or an absent
// filter) counts as covering them. A Self alternative is the card alone.
func restrictsCombat(st *cards.Static, card, fix *cards.Face, attack bool) bool {
	var who []*cards.Face
	switch {
	case attackModes[st.Mode] && attack:
		who = []*cards.Face{card}
	case attackModes[st.Mode]:
		who = []*cards.Face{fix}
	case blockModes[st.Mode] && !attack:
		who = []*cards.Face{card, fix}
	default:
		return false
	}
	named := false
	for _, k := range []string{"Affected", "ValidCard", "ValidAttacker", "ValidBlocker"} {
		f := strings.TrimSpace(st.Params[k])
		if f == "" {
			continue
		}
		named = true
		for _, a := range parseFilter(f) {
			if !a.known {
				return true
			}
			for _, w := range who {
				if (!a.self || w == card) && a.test(w) {
					return true
				}
			}
		}
	}
	return !named
}

// combatKeyStart is a level-B combat key (combat#F.attack, combat#F.block) as
// a start: the card on the battlefield since the turn began (no summoning
// sickness), and the fixture creature on the other side.
//
//	attack: the card's seat is active and the card attacks (focus attack); the
//	        fixture is the opposing seat's only creature, there to block it.
//	block:  the opposing seat is active and its fixture attacks; the card is the
//	        defender's only creature and blocks it (focus block, seat 0).
//
// The block is forced: the focus is the card's seat blocking (focus kind
// "block", game/focus.go), and the fixture's seat attacks whatever its bot would
// have chosen.
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
		if !restrictsCombat(st, face, fix.Faces[0], m[2] == "attack") {
			continue
		}
		switch {
		case m[2] == "attack" && st.Mode == "CantAttack" && selfStatic(st):
			return "no-generator:combat:cant-attack"
		case m[2] == "block" && (st.Mode == "CantBlock" || st.Mode == "CantBlockBy") && selfStatic(st):
			return "no-generator:combat:cant-block"
		}
		return "no-generator:combat:restricted"
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
	f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Kind: "block"}
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
	for _, k := range []string{"AffectedZone", "EffectZone"} {
		if z := st.Params[k]; z != "" && z != "Battlefield" {
			return "no-generator:static:zone"
		}
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
	compare, why := staticObserved(st)
	if why != "" {
		return why
	}
	f.Setup.P0.Battlefield = []string{card}
	f.Compare = compare
	if selfStatic(st) {
		return ""
	}
	name, s := filterCarrier(reg, b, card, aff, (*cards.Face).IsPermanent)
	if name == "" {
		name, s = landCarrier(reg, aff, basicFor(face.ManaCost))
	}
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

// staticObserved is how the start observes a Continuous static's effect, or why
// it cannot. The comparison reads the permanents' P/T, types, colours, controller
// and counters; keywords only when the item opts in. So an effect that only
// changes what the permanent can do (CanBlockAmount, a granted ability or
// trigger) is refused, a keyword grant or removal sets the keyword opt-in
// (the evergreen vocabulary, else the named one), and a pump or set whose amount
// is not an integer is a computed count the placed objects do not make: the
// start would read it at zero.
func staticObserved(st *cards.Static) (compare []string, why string) {
	observed := false
	for _, k := range []string{"AddPower", "AddToughness", "SetPower", "SetToughness"} {
		if v := st.Params[k]; v != "" {
			if _, err := strconv.Atoi(strings.TrimSpace(v)); err != nil {
				return nil, "no-generator:static:computed"
			}
			observed = true
		}
	}
	for _, k := range []string{"AddType", "RemoveType", "RemoveCardTypes", "RemoveCreatureTypes", "SetColor", "GainControl"} {
		if st.Params[k] != "" {
			observed = true
		}
	}
	var kws []string
	for _, k := range []string{"AddKeyword", "RemoveKeyword"} {
		if v := st.Params[k]; v != "" {
			kws = append(kws, strings.Split(v, " & ")...)
		}
	}
	switch {
	case len(kws) == 0:
	case oraclediff.ComparedKeywords(kws, false) != "":
		compare, observed = []string{oraclediff.CompareKeywords}, true
	case oraclediff.ComparedKeywords(kws, true) != "":
		compare, observed = []string{oraclediff.CompareKeywordsNamed}, true
	}
	if !observed {
		return nil, "no-generator:static:unobserved"
	}
	return compare, ""
}
