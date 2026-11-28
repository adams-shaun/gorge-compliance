package scen

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
)

// The combat-step start: a trigger that fires in combat (a creature blocks, an
// attacker is blocked, combat damage is dealt) is one scripted combat
// (StartFocus.Attack, Block; game/focus.go) with no forced play (focus kind ""):
// the active seat attacks with exactly the Attack creature, the defending seat
// blocks it with exactly the Block creature, or with none when Block is "",
// whatever the bots would choose. Every combatant is placed on the battlefield,
// so it has been under its controller's control since the turn began and may
// attack (CR 302.6). The card is on seat 0's battlefield; it is the attacker,
// the blocker, or neither (an enchantment that reads some other creature), and
// the other combatant is Grizzly Bears (combatFixture) or the most inert
// carrier the trigger's filter takes (pickCarrier).
//
// The modes are Blocks, AttackerBlocked, AttackerBlockedByCreature and the three
// damage modes (DamageDone, DamageDoneOnce, DamageDealtOnce; DamageDone tries the
// one-attack combatStart first). Attacks is not here: attacksStart owns it (no
// block to script), and its Aura and Equipment bearer cases stay refused for want
// of an attach in a start file.
//
// Every combat trigger mode reads its two combatants (and, for damage, who is
// hurt) into the same pair of party specs, then goes through combatLine, so a
// mode added later is one more case in combatParties.

// combatTrigModes are the trigger modes combatTrigStart reads: the scripted
// combat gives the blocks, so a start drops the opp-blockers need.
var combatTrigModes = map[string]bool{"Blocks": true, "AttackerBlocked": true, "AttackerBlockedByCreature": true,
	"DamageDone": true, "DamageDoneOnce": true, "DamageDealtOnce": true}

// combatTrigParams are the trigger parameters a combat start reads or may
// leave unmet (eventParamsOK adds its base ones): the combatants' filters, the
// combat-damage flag, the turn (combatTrigStart seats the attacker by it), and
// parameters that only describe the trigger or limit how often it fires. Every
// other one (a condition, a damage amount, an IsPresent, a check of the
// controller's state) is a state the start does not establish, so the trigger
// would not fire: refused, never started without it.
var combatTrigParams = []string{"ValidCard", "ValidSource", "ValidTarget", "ValidBlocker", "ValidBlocked", "ValidBlockerAmount",
	"CombatDamage", "PlayerTurn", "OpponentTurn", "Keyword", "KeywordLine", "PrecostDesc", "StackDescription", "ResolvedLimit",
	"AtLeastOneInstance", "FirstTime"}

// party is one combatant the trigger names: the card itself, or a creature a
// filter takes ("" takes any). dealer marks the creature that deals the combat
// damage, which must have a positive power (CR 120.8: a source that would deal
// 0 damage deals none).
type party struct {
	self   bool
	filter string
	dealer bool
	// attached: the creature the card, an Aura, is attached to (ValidSource
	// Card.AttachedBy); combatLine places the Aura on a bearer that is the party.
	attached bool
}

// selfFilter reports whether some alternative of a filter reads the card
// itself ("Card.Self", "Creature.Self+kicked").
func selfFilter(s string) bool {
	for _, alt := range strings.Split(s, ",") {
		_, props, _ := strings.Cut(strings.TrimSpace(alt), ".")
		for _, p := range strings.Split(props, "+") {
			if p == "Self" {
				return true
			}
		}
	}
	return false
}

func partyOf(filter string) party {
	return party{self: selfFilter(filter), filter: strings.TrimSpace(filter), attached: attachedSource(filter)}
}

// combatParties reads a combat trigger into the attacker, the blocker (nil: no
// blocks), and the seat the attacker must be on (-1: either), or the reason it
// is refused.
func combatParties(face *cards.Face, trig *cards.Trigger) (att party, blk *party, seat int, why string) {
	none := "no-generator:trigger:" + trig.Mode
	p := trig.Params
	seat = -1
	switch trig.Mode {
	case "Blocks":
		// ValidCard is the blocker, ValidBlocked the attacker it blocks
		b := partyOf(p["ValidCard"])
		return partyOf(p["ValidBlocked"]), &b, -1, ""
	case "AttackerBlocked", "AttackerBlockedByCreature":
		if p["ValidBlockerAmount"] != "" {
			return att, nil, 0, none + ":amount"
		}
		b := partyOf(p["ValidBlocker"])
		return partyOf(p["ValidCard"]), &b, -1, ""
	}
	// the damage modes: ValidSource deals the damage, ValidTarget takes it
	if p["CombatDamage"] == "False" {
		return att, nil, 0, none + ":noncombat"
	}
	src, tgt := partyOf(p["ValidSource"]), strings.TrimSpace(p["ValidTarget"])
	src.dealer = true
	hurtsPlayer := tgt == ""
	for _, alt := range strings.Split(tgt, ",") {
		if h, _, _ := strings.Cut(strings.TrimSpace(alt), "."); h == "Player" || h == "Opponent" {
			hurtsPlayer = true
		}
	}
	switch {
	case hurtsPlayer:
		return src, nil, 0, "" // unblocked, on the card's controller's turn
	case tgt == "You":
		return src, nil, 1, "" // the other seat's creature connects with the card's controller
	case selfFilter(tgt):
		// the card is hurt: it blocks the attacker (the source's filter)
		return src, &party{self: true}, 1, ""
	case attachedSource(tgt):
		// "when enchanted creature is dealt damage" (Cracked Skull): the
		// Aura's bearer blocks the attacker and is hurt; combatLine places
		// the Aura attached (StartAttach) through the party's attached arm.
		return src, &party{attached: true}, 1, ""
	case strings.HasPrefix(tgt, "Creature"):
		return src, &party{filter: tgt}, 0, ""
	}
	return att, nil, 0, none + ":target"
}

// placePartyGates places the counters the first understood alternative of a
// party's filter requires of the chosen creature: "a creature with a counter
// on it" (HasCounters) and counters_<OP><N>_<KIND> are battlefield state a
// start meets by placing them, the same rule removalStart follows (it picks
// the carrier through the same counter-parsed alternatives).
func placePartyGates(seats [2]*StartSeat, seat int, name, filter string, chosen *cards.Face) {
	if filter == "" {
		return
	}
	for _, a := range parseFilterCounters(filter, true, false, false, false) {
		if !a.known || a.self || len(a.counters) == 0 {
			continue
		}
		if !a.matches(chosen, false) || !survivesCounters(chosen, a.counters) {
			continue
		}
		for _, g := range a.counters {
			seats[seat].Counters = append(seats[seat].Counters, StartCounter{Card: name, Kind: g.placedKind(), N: g.n})
		}
		return
	}
}

// blockable reports whether blk can block att at all (CR 509.1b, 702.9, 702.28,
// 702.36, 702.111, 702.16): the evasion keywords the fixture and the carriers do
// not meet. Menace needs two blockers and the line scripts one (CR 702.111b);
// protection from a quality the blocker has (any protection is refused, not
// read) keeps the block from being declared.
func blockable(att, blk *cards.Face) bool {
	for _, k := range []string{"Shadow", "Horsemanship", "Fear", "Intimidate", "Unblockable", "Skulk", "Menace", "Protection", "CantBeBlockedBy"} {
		if att.HasKeyword(k) {
			return false
		}
	}
	if blk.HasKeyword("Shadow") {
		return false
	}
	return !att.HasKeyword("Flying") || blk.HasKeyword("Flying") || blk.HasKeyword("Reach")
}

// combatTrigStart is a combat trigger as a start (see the block comment above).
func combatTrigStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:" + trig.Mode
	if why := eventParamsOK(trig, trig.Mode, combatTrigParams...); why != "" {
		return why
	}
	att, blk, seat, why := combatParties(face, trig)
	if why != "" {
		return why
	}
	if blk != nil && att.self && blk.self {
		return none + ":filter"
	}
	// the card is the attacker (seat 0) or the blocker (the other seat attacks)
	// unless the mode already seats the attacker: a seat the mode requires
	// stays, and a card that cannot be on it is refused by combatLine
	if seat < 0 {
		switch {
		case att.self:
			seat = 0
		case blk != nil && blk.self:
			seat = 1
		}
	}
	// "during your turn" / "during an opponent's turn": the card's controller is
	// seat 0, and the attacker's seat is the active one
	for _, t := range []struct {
		param string
		seat  int
	}{{"PlayerTurn", 0}, {"OpponentTurn", 1}} {
		switch v := trig.Params[t.param]; {
		case v == "":
		case v != "True" || seat >= 0 && seat != t.seat:
			return none + ":turn"
		default:
			seat = t.seat
		}
	}
	fix, ok := reg.Lookup(combatFixture)
	if !ok || len(fix.Faces) == 0 || !fix.Faces[0].IsCreature() {
		return none + ":fixture"
	}
	seats := []int{seat}
	if seat < 0 {
		seats = []int{0, 1}
	}
	why = none + ":filter"
	for _, s := range seats {
		g := *f
		g.Setup.P0 = StartSeat{Battlefield: []string{card}}
		g.Setup.P1 = StartSeat{}
		w := combatLine(reg, b, face, fix.Faces[0], card, att, blk, s, &g, none)
		if w == "" {
			*f = g
			return ""
		}
		why = w
	}
	return why
}

// combatLine places the attacker on attackSeat's battlefield and the blocker on
// the other's and scripts the combat. It reports the refusal, or "".
func combatLine(reg *cards.Registry, b *certdeck.Builder, face, fix *cards.Face, card string, att party, blk *party, attackSeat int, f *StartFile, none string) string {
	seats := [2]*StartSeat{&f.Setup.P0, &f.Setup.P1}
	faceOf := func(name string) *cards.Face {
		if name == card {
			return face
		}
		c, _ := reg.Lookup(name)
		return c.Faces[0]
	}
	// choose is the creature a party is: the card, the fixture or a carrier its
	// filter takes, on seat's battlefield; ok reports blockable against the
	// opposing creature other (nil: none chosen yet).
	choose := func(p party, seat int, attacker bool, other *cards.Face) (string, string) {
		pairOK := func(c *cards.Face) bool {
			switch {
			case other == nil:
				return true
			case attacker:
				return blockable(c, other) && cantBlockByBlocked(c, other) == nil
			}
			return blockable(other, c) && cantBlockByBlocked(other, c) == nil
		}
		if p.attached {
			if FieldBlock(face) != "aura-unattached" {
				return "", none + ":filter"
			}
			bearer, bs := attachBearerWhere(reg, b, card, face, func(c *cards.Face) bool {
				return c.IsCreature() && pairOK(c) && (!attacker || !c.HasKeyword("Defender")) && (!p.dealer || c.Power() > 0)
			})
			if bearer == "" || bs != seat {
				return "", none + ":filter"
			}
			placeBearer(seats[0], seats[1], card, bearer, bs)
			return bearer, ""
		}
		if p.self {
			switch {
			case seat != 0:
				return "", none + ":filter"
			case !face.IsCreature():
				return "", none + ":noncreature"
			case attacker && face.HasKeyword("Defender"):
				return "", "no-generator:combat:defender"
			case p.dealer && face.Power() <= 0:
				return "", none + ":power"
			case !pairOK(face):
				return "", none + ":evasion"
			}
			placePartyGates(seats, seat, card, p.filter, face)
			return card, ""
		}
		// The counters a party's filter requires (HasCounters,
		// counters_<OP><N>_<KIND>) are battlefield state the start meets by
		// placing them (removalStart's rule), so parseFilterCounters reads
		// them rather than leaving the alternative unknown.
		alts := parseFilterCounters(p.filter, true, false, false, false)
		fits := func(c *cards.Face) bool {
			if !c.IsCreature() || !pairOK(c) || attacker && c.HasKeyword("Defender") || p.dealer && c.Power() <= 0 {
				return false
			}
			if p.filter == "" {
				return true
			}
			for _, a := range alts {
				if a.known && !a.self && (a.ctrl == ctrlAny || a.ctrl == seat) && a.matches(c, false) && survivesCounters(c, a.counters) {
					return true
				}
			}
			return false
		}
		name := ""
		if fits(fix) {
			name = combatFixture
		} else {
			// The fixture is refused (its stats or keywords fail the pair:
			// a flying attacker needs a flying/reach blocker, and an empty
			// filter still names any creature that meets the pairing), so
			// the carrier pool stands in.
			name = pickCarrier(reg, b, card, func(c *cards.Face) bool {
				return fits(c) && FieldBlock(c) == "" && !deadOnArrival(c)
			})
		}
		if name == "" {
			return "", none + ":filter"
		}
		seats[seat].Battlefield = append(seats[seat].Battlefield, name)
		placePartyGates(seats, seat, name, p.filter, faceOf(name))
		return name, ""
	}
	// the party that is the card goes first: the other one is chosen against it
	var attacker, blocker string
	var why string
	switch {
	case blk == nil:
		attacker, why = choose(att, attackSeat, true, nil)
	case att.self:
		attacker, why = choose(att, attackSeat, true, nil)
		if why == "" {
			blocker, why = choose(*blk, 1-attackSeat, false, faceOf(attacker))
		}
	default:
		blocker, why = choose(*blk, 1-attackSeat, false, nil)
		if why == "" {
			attacker, why = choose(att, attackSeat, true, faceOf(blocker))
		}
	}
	if why != "" {
		return why
	}
	// a restriction on either creature keeps its attack or its block from being
	// taken, or the block from being declared against it (CantBlockBy on the
	// attacker, CantBlock on the blocker): the card's, a carrier's, the fixture's
	atk, blkFace := faceOf(attacker), fix
	if blocker != "" {
		blkFace = faceOf(blocker)
	}
	if blocker == "" {
		if restrictingStatic(atk, blkFace, true) != nil {
			return none + ":restricted"
		}
	} else if combatPairBlocked(atk, blkFace) != nil {
		return none + ":restricted"
	}
	f.Active = attackSeat
	f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Attack: attacker, Block: blocker}
	return ""
}
