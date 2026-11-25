package scen

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
)

// A focus play that targets a creature in combat ("target attacking
// creature", "target blocking creature", "target creature blocking this") is
// taken after a scripted combat (StartFocus.Attack, Block; game/focus.go): the
// active seat attacks with exactly the Attack creature, the defending seat
// blocks it with exactly the Block creature (or not at all), and the focus
// seat then takes its play at its first chance. Every combatant is placed on
// the battlefield, so it has been under its controller's control since the
// turn began and may attack (CR 302.6); the block is declared on the
// defending seat's side only (CR 509.1a).

// Combat roles a target alternative names (combatRoleOf).
const (
	roleAttacking       = "attacking"       // Creature.attacking
	roleAttackingYou    = "attackingyou"    // attacking the focus seat
	roleBlocking        = "blocking"        // Creature.blocking
	roleBlocked         = "blocked"         // an attacker that was blocked
	roleBlockingSource  = "blockingsource"  // blocking the focus card
	roleBlockedBySource = "blockedbysource" // blocked by the focus card
)

// combatProps maps a combat property of a target filter to its role.
var combatProps = map[string]string{
	"attacking": roleAttacking, "attackedthisturn": roleAttacking,
	"attackingyou": roleAttackingYou, "attackingyouoryourpw": roleAttackingYou,
	"blocking": roleBlocking, "blocked": roleBlocked,
	"blockingsource":  roleBlockingSource,
	"blockedbysource": roleBlockedBySource, "blockedbysourcelki": roleBlockedBySource,
}

// combatRoleOf is the combat role of a raw target alternative
// ("Creature.powerLE3+attacking"), and the alternative without it
// ("Creature.powerLE3"); role "" when it names none, or names more than one.
func combatRoleOf(raw string) (role, rest string) {
	head, props, _ := strings.Cut(raw, ".")
	var keep []string
	for _, p := range strings.FieldsFunc(props, func(r rune) bool { return r == '+' }) {
		if r, ok := combatProps[strings.ToLower(p)]; ok {
			if role != "" {
				return "", raw
			}
			role = r
			continue
		}
		keep = append(keep, p)
	}
	if len(keep) == 0 {
		return role, head
	}
	return role, head + "." + strings.Join(keep, "+")
}

// combatTest is the test of the creature a combat alternative's rest
// (combatRoleOf) takes, and its controller (ctrlYou, ctrlOpp, ctrlAny, as the
// focus seat reads it), or nil when parseFilter does not read it.
func combatTest(rest string) (func(*cards.Face) bool, int) {
	alts := parseFilter(rest)
	if len(alts) != 1 || !alts[0].known || alts[0].self {
		return nil, ctrlAny
	}
	a := alts[0]
	ctrl := a.ctrl
	if strings.Contains(strings.ToLower(rest), "youdontctrl") {
		ctrl = ctrlOpp
	}
	return func(f *cards.Face) bool { return f.IsCreature() && a.test(f) }, ctrl
}

// lineMet is the permanent of the start's scripted combat that meets a
// combat target alternative, as unmet indexes it (side 0 the focus seat's,
// 1 the other's; the index in that side's battlefield), or ok false.
func lineMet(reg *cards.Registry, f *StartFile, seat int, raw string) (side, idx int, ok bool) {
	fo := f.Focus
	role, rest := combatRoleOf(raw)
	if fo == nil || role == "" || fo.Attack == "" {
		return 0, 0, false
	}
	name, owner := "", -1
	switch role {
	case roleAttacking:
		name, owner = fo.Attack, f.Active
	case roleAttackingYou:
		if f.Active != seat {
			name, owner = fo.Attack, f.Active
		}
	case roleBlocking:
		name, owner = fo.Block, 1-f.Active
	case roleBlocked:
		if fo.Block != "" {
			name, owner = fo.Attack, f.Active
		}
	case roleBlockingSource:
		if fo.Attack == f.Card {
			name, owner = fo.Block, 1-f.Active
		}
	case roleBlockedBySource:
		if fo.Block == f.Card {
			name, owner = fo.Attack, f.Active
		}
	}
	test, ctrl := combatTest(rest)
	if name == "" || test == nil || ctrl == ctrlYou && owner != seat || ctrl == ctrlOpp && owner == seat {
		return 0, 0, false
	}
	c, found := reg.Lookup(name)
	if !found || len(c.Faces) == 0 || !test(c.Faces[0]) {
		return 0, 0, false
	}
	bf := f.Setup.P0.Battlefield
	if owner == 1 {
		bf = f.Setup.P1.Battlefield
	}
	j := slices.Index(bf, name)
	if j < 0 {
		return 0, 0, false
	}
	if owner != seat {
		side = 1
	}
	return side, j, true
}

// placeLine scripts the combat an unmet combat target needs, and says whether
// one fit: for the first alternative it can serve, the creatures that fight
// (the most inert carrier the alternative's restrictions take, Grizzly Bears
// for the other combatant, the focus card itself for a "blocking this" or
// "blocked by this" target), placed on the right sides, and the active seat
// (the attacker's). A start scripts one combat: a second combat target is
// unmet. tapped is whether the focus play taps the focus card, which then
// cannot attack for it.
func placeLine(reg *cards.Registry, b *certdeck.Builder, card string, r *tgtReq, seat int, tapped bool, f *StartFile) bool {
	fo := f.Focus
	if fo == nil || fo.Attack != "" || fo.Block != "" {
		return false
	}
	seats := [2]*StartSeat{&f.Setup.P0, &f.Setup.P1}
	mine, theirs := seat, 1-seat
	self := func() bool { // the focus card is a creature on the focus seat's battlefield
		c, ok := reg.Lookup(card)
		return ok && len(c.Faces) > 0 && c.Faces[0].IsCreature() && slices.Contains(seats[mine].Battlefield, card)
	}
	selfAttacks := func() bool {
		c, _ := reg.Lookup(card)
		return !tapped && !c.Faces[0].HasKeyword("Defender")
	}
	for k := range r.alts {
		if k >= len(r.raw) {
			break
		}
		role, rest := combatRoleOf(r.raw[k])
		test, ctrl := combatTest(rest)
		if role == "" || test == nil {
			continue
		}
		pick := func(attacker bool) string {
			return pickCarrier(reg, b, card, func(c *cards.Face) bool {
				return c.Name != combatFixture && test(c) && FieldBlock(c) == "" && !deadOnArrival(c) && !(attacker && c.HasKeyword("Defender"))
			})
		}
		// target is the seat whose creature the play targets
		target := mine
		if ctrl == ctrlOpp || role == roleAttackingYou || role == roleBlockingSource || role == roleBlockedBySource {
			target = theirs
		}
		if ctrl == ctrlYou && target != mine {
			continue
		}
		var attack, block string
		active := target
		switch role {
		case roleAttacking, roleAttackingYou:
			attack = pick(true)
			if attack == "" {
				continue
			}
			seats[target].Battlefield = append(seats[target].Battlefield, attack)
		case roleBlocked:
			attack = pick(true)
			if attack == "" {
				continue
			}
			seats[target].Battlefield = append(seats[target].Battlefield, attack)
			block = combatFixture
			seats[1-target].Battlefield = append(seats[1-target].Battlefield, block)
		case roleBlocking:
			block = pick(false)
			if block == "" {
				continue
			}
			active = 1 - target
			attack = combatFixture
			seats[active].Battlefield = append(seats[active].Battlefield, attack)
			seats[target].Battlefield = append(seats[target].Battlefield, block)
		case roleBlockingSource:
			if !self() || !selfAttacks() {
				continue
			}
			block = pick(false)
			if block == "" {
				continue
			}
			active, attack = mine, card
			seats[theirs].Battlefield = append(seats[theirs].Battlefield, block)
		case roleBlockedBySource:
			if !self() {
				continue
			}
			attack = pick(true)
			if attack == "" {
				continue
			}
			active, block = theirs, card
			seats[theirs].Battlefield = append(seats[theirs].Battlefield, attack)
		default:
			continue
		}
		f.Active = active
		fo.Attack, fo.Block = attack, block
		return true
	}
	return false
}
