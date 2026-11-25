package scen

import (
	"regexp"
	"slices"
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
				if (!a.self || w == card) && a.matches(w, false) {
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
	fi, _ := strconv.Atoi(m[1])
	if fi >= len(c.Faces) {
		return "no-generator:face"
	}
	face := c.Faces[fi]
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
// start. The card itself is the affected object when the static reads Self; an
// Aura's or Equipment's static is attached to a bearer (attachStart), and a
// condition the start can place is met (staticCondition). Every other mode, and
// a static no placement can satisfy, is refused with the mode or the condition
// in the reason.
func staticKeyStart(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, card, key string, f *StartFile) string {
	m := staticKeyRE.FindStringSubmatch(key)
	if m == nil {
		return "no-generator:static"
	}
	fi, _ := strconv.Atoi(m[1])
	if fi >= len(c.Faces) {
		return "no-generator:face"
	}
	face := c.Faces[fi]
	si, _ := strconv.Atoi(m[2])
	if si >= len(face.Statics) {
		return "no-generator:static"
	}
	st := &face.Statics[si]
	if st.ModeKind() != cards.StaticContinuous {
		return "no-generator:static:" + st.Mode
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
	enchanted, equipped := attachedAffected(aff)
	attached := enchanted != "" || equipped != ""
	compare, why := staticObserved(st)
	if why != "" {
		return why
	}
	f.Setup.P0.Battlefield = []string{card}
	f.Compare = compare
	switch {
	case attached:
		// an Aura's or Equipment's bearer (gorge folds AffectedDefined into the
		// Affected filter as EnchantedBy/EquippedBy): the start attaches it
		eff := effectOf(st)
		reach, affected, why := attachStart(reg, b, face, card, enchanted, equipped, eff, f)
		if why != "" {
			return why
		}
		if why := staticCondition(reg, b, face, card, st, eff, f); why != "" {
			return why
		}
		// the bearer is the only choice the engines' AIs have that matters
		if !onlyTargetsShow(reg, card, reach, affected, eff, f) {
			return "no-generator:static:bearer"
		}
		return ""
	case selfStatic(st):
	default:
		name, s := staticCarrier(reg, b, card, aff, basicFor(face.ManaCost), effectOf(st))
		if name == "" {
			return "no-generator:static:affected"
		}
		placeOn(f, s, name)
	}
	return staticCondition(reg, b, face, card, st, effectOf(st), f)
}

// staticCarrier is the permanent an unattached static affects: the first
// carrier (else basic land) that meets an alternative of the Affected filter
// and on which the static's effect shows (a creature already 1/1 is not shown a
// 1/1 set, a Swamp is not shown "is a Swamp"), with the seat that controls it:
// the filter's, else seat 1 for a control effect (the caster's own is theirs)
// and seat 0 otherwise. "" when none.
func staticCarrier(reg *cards.Registry, b *certdeck.Builder, card, aff, generic string, eff staticEffect) (string, int) {
	alts := parseFilter(aff)
	seatOf := func(a filterAlt) int {
		if a.ctrl == ctrlOpp || a.ctrl == ctrlAny && eff.gain {
			return 1
		}
		return 0
	}
	for _, a := range alts {
		if !a.known || a.self {
			continue
		}
		s := seatOf(a)
		if n := pickCarrier(reg, b, card, func(f *cards.Face) bool { return f.IsPermanent() && a.test(f) && eff.shows(f, s) }); n != "" {
			return n, s
		}
	}
	for _, a := range alts {
		if !a.known || a.self {
			continue
		}
		s := seatOf(a)
		for _, basic := range append([]string{generic}, "Plains", "Island", "Swamp", "Mountain", "Forest") {
			if c, ok := reg.Lookup(basic); ok && len(c.Faces) > 0 && a.test(c.Faces[0]) && eff.shows(c.Faces[0], s) {
				return basic, s
			}
		}
	}
	return "", 0
}

// placeOn adds a permanent to seat s's battlefield.
func placeOn(f *StartFile, s int, name string) {
	if s == 1 {
		f.Setup.P1.Battlefield = append(f.Setup.P1.Battlefield, name)
	} else {
		f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, name)
	}
}

// attachedAffected reads an Affected filter for the objects an attachment
// affects: the alternatives that carry the bare EnchantedBy (an Aura's bearer)
// or EquippedBy (an Equipment's) property, each with that property removed
// ("Creature.Human+EnchantedBy" gives "Creature.Human"), joined back into a
// filter; "" when no alternative carries it. A negated or qualified form
// ("!EnchantedBy", "EnchantedBy Aura") is not an attached bearer, so it is left
// to the ordinary Affected path, which refuses a property it does not read.
func attachedAffected(aff string) (enchanted, equipped string) {
	var enc, eq []string
	for _, raw := range strings.Split(aff, ",") {
		head, props, _ := strings.Cut(strings.TrimSpace(raw), ".")
		kept := []string{}
		var isEnc, isEq bool
		for _, p := range strings.Split(props, "+") {
			switch p {
			case "EnchantedBy":
				isEnc = true
			case "EquippedBy":
				isEq = true
			default:
				if p != "" {
					kept = append(kept, p)
				}
			}
		}
		if head == "EnchantedBy" || head == "EquippedBy" {
			isEnc, isEq, head = head == "EnchantedBy", head == "EquippedBy", "Card"
		}
		alt := head
		if len(kept) > 0 {
			alt += "." + strings.Join(kept, "+")
		}
		switch {
		case isEnc && !isEq:
			enc = append(enc, alt)
		case isEq && !isEnc:
			eq = append(eq, alt)
		}
	}
	return strings.Join(enc, ","), strings.Join(eq, ",")
}

// staticEffect is what a Continuous static visibly does to the object it
// affects, in what the start's comparison reads: it takes control (GainControl$
// You: seen only on a permanent the other seat controls, as the caster's own
// already is theirs), or changes the object's own characteristics (face: its
// power or toughness, a type it lacks or has, its colour, a keyword the
// comparison reads). A param it cannot show on a placed permanent (a computed
// amount, a chosen type or colour, a ControlGain that is not You's) adds nothing,
// so a static none of whose params it can show has no bearer to prove it on.
type staticEffect struct {
	gain  bool
	faces []func(*cards.Face) bool
}

// shows reports whether the static visibly changes a permanent of face f that
// seat s controls.
func (e staticEffect) shows(f *cards.Face, s int) bool {
	if e.gain && s == 1 {
		return true
	}
	for _, t := range e.faces {
		if t(f) {
			return true
		}
	}
	return false
}

// basicLandTypes are the basic land types.
var basicLandTypes = map[string]bool{"Plains": true, "Island": true, "Swamp": true, "Mountain": true, "Forest": true}

// cardTypeWords are the card types a RemoveCardTypes static removes.
var cardTypeWords = []string{"Artifact", "Battle", "Creature", "Enchantment", "Land", "Planeswalker", "Instant", "Sorcery", "Kindred"}

// effectOf reads the effect of st, the params staticObserved counts.
func effectOf(st *cards.Static) staticEffect {
	var e staticEffect
	add := func(t func(*cards.Face) bool) { e.faces = append(e.faces, t) }
	e.gain = strings.TrimSpace(st.Params["GainControl"]) == "You"
	if n, err := strconv.Atoi(strings.TrimSpace(st.Params["AddPower"])); err == nil && n != 0 {
		add((*cards.Face).IsCreature)
	}
	if n, err := strconv.Atoi(strings.TrimSpace(st.Params["AddToughness"])); err == nil && n != 0 {
		add((*cards.Face).IsCreature)
	}
	if n, err := strconv.Atoi(strings.TrimSpace(st.Params["SetPower"])); err == nil {
		add(func(f *cards.Face) bool { return f.IsCreature() && f.Power() != n })
	}
	if n, err := strconv.Atoi(strings.TrimSpace(st.Params["SetToughness"])); err == nil {
		add(func(f *cards.Face) bool { return f.IsCreature() && f.Toughness() != n })
	}
	// RemoveLandTypes with an added basic type sets the land types: a land
	// shows it only when its basic types are not exactly the added ones (an
	// Island made "an Island" is unchanged), so those words are read together
	var landTypes []string
	setLand := st.Params["RemoveLandTypes"] == "True"
	for _, t := range strings.Split(st.Params["AddType"], " & ") {
		switch t = strings.TrimSpace(t); {
		case t == "" || t == "ChosenType" || !typeWord(t):
		case setLand && basicLandTypes[t]:
			landTypes = append(landTypes, t)
		default:
			add(func(f *cards.Face) bool { return !f.HasTypeWord(t) })
		}
	}
	for _, t := range strings.Split(st.Params["RemoveType"], " & ") {
		if t = strings.TrimSpace(t); t != "" && typeWord(t) {
			add(func(f *cards.Face) bool { return f.HasTypeWord(t) })
		}
	}
	// the card types are replaced by the added ones: a permanent shows it only
	// when it has a card type the static does not add back (a Forest made "a
	// Land & Forest" keeps its type, and so does an Island made "a Land")
	if st.Params["RemoveCardTypes"] == "True" {
		added := strings.Split(st.Params["AddType"], " & ")
		add(func(f *cards.Face) bool {
			for _, t := range cardTypeWords {
				if f.HasTypeWord(t) && !slices.Contains(added, t) {
					return true
				}
			}
			return false
		})
	}
	if st.Params["RemoveCreatureTypes"] == "True" {
		add((*cards.Face).IsCreature)
	}
	if setLand {
		add(func(f *cards.Face) bool {
			for t := range basicLandTypes {
				if f.HasTypeWord(t) != slices.Contains(landTypes, t) {
					return true
				}
			}
			return false
		})
	}
	if v := st.Params["SetColor"]; v != "" {
		want, ok := "", true
		for _, w := range strings.Split(v, " & ") {
			if l, found := colourWord[strings.TrimSpace(w)]; found {
				want += string(l)
			} else if strings.TrimSpace(w) != "Colorless" {
				ok = false
			}
		}
		if ok {
			sorted := ""
			for _, l := range "WUBRG" {
				if strings.ContainsRune(want, l) {
					sorted += string(l)
				}
			}
			add(func(f *cards.Face) bool { return faceColours(f) != sorted })
		}
	}
	var kws []string
	for _, k := range []string{"AddKeyword", "RemoveKeyword"} {
		if v := st.Params[k]; v != "" {
			kws = append(kws, strings.Split(v, " & ")...)
		}
	}
	named := oraclediff.ComparedKeywords(kws, false) == ""
	for _, k := range strings.Split(st.Params["AddKeyword"], " & ") {
		if k = strings.TrimSpace(k); k != "" && oraclediff.ComparedKeywords([]string{k}, named) != "" {
			add(func(f *cards.Face) bool { return !f.HasKeyword(k) })
		}
	}
	for _, k := range strings.Split(st.Params["RemoveKeyword"], " & ") {
		if k = strings.TrimSpace(k); k != "" && oraclediff.ComparedKeywords([]string{k}, named) != "" {
			add(func(f *cards.Face) bool { return f.HasKeyword(k) })
		}
	}
	return e
}

// bearerFor is the most inert permanent that meets both what an Aura enchants
// or an Equipment equips (reach) and what the static affects (affected, the
// attached property removed) and on which the static's effect (eff) shows, so
// the start's comparison sees the key at work: the walls first (bearers), else
// a carrier, else a basic land. It also returns the seat that controls it;
// "" when none. A control effect puts the bearer on seat 1 (the caster's own
// permanent is theirs already). An alternative holding a property the filter
// code does not read is skipped, so such a static is refused rather than given
// a bearer it may not affect.
func bearerFor(reg *cards.Registry, b *certdeck.Builder, card, reach, affected, generic string, eff staticEffect) (string, int) {
	type pair struct {
		test func(*cards.Face) bool
		seat int
	}
	var pairs []pair
	for _, r := range parseFilter(reach) {
		for _, a := range parseFilter(affected) {
			if !r.known || r.self || !a.known || a.self || (r.ctrl != ctrlAny && a.ctrl != ctrlAny && r.ctrl != a.ctrl) {
				continue
			}
			seat := 0
			switch {
			case r.ctrl == ctrlOpp || a.ctrl == ctrlOpp:
				seat = 1
			case r.ctrl == ctrlAny && a.ctrl == ctrlAny && eff.gain:
				seat = 1
			}
			r, a, seat := r, a, seat
			pairs = append(pairs, pair{func(f *cards.Face) bool { return r.test(f) && a.test(f) && eff.shows(f, seat) }, seat})
		}
	}
	for _, p := range pairs {
		for _, w := range bearers {
			if c, ok := reg.Lookup(w); ok && len(c.Faces) > 0 && b.Playable(c) && !deadOnArrival(c.Faces[0]) && p.test(c.Faces[0]) {
				return w, p.seat
			}
		}
	}
	for _, p := range pairs {
		if n := pickCarrier(reg, b, card, func(f *cards.Face) bool { return f.IsPermanent() && p.test(f) }); n != "" {
			return n, p.seat
		}
	}
	for _, p := range pairs {
		for _, basic := range append([]string{generic}, "Plains", "Island", "Swamp", "Mountain", "Forest") {
			if c, ok := reg.Lookup(basic); ok && len(c.Faces) > 0 && p.test(c.Faces[0]) {
				return basic, p.seat
			}
		}
	}
	return "", 0
}

// onlyTargetsShow reports whether every permanent the start has on the
// battlefield that the Aura could enchant or the equip could target (reach)
// both meets what the static affects and shows its effect: whichever of them
// the engines' AIs pick, the key is at work, so a disagreement is the static's
// and not a pick's, and they are all the same permanent (a name), so the cast
// or equip has no pick to make (the same name under the other seat is another). The cost lands of an Enchant-land Aura, and a second
// creature a condition places, are such targets; the card itself is not.
func onlyTargetsShow(reg *cards.Registry, card, reach, affected string, eff staticEffect, f *StartFile) bool {
	rs, as := parseFilter(reach), parseFilter(affected)
	ctrlOK := func(c, s int) bool { return c == ctrlAny || c == s }
	type pick struct {
		seat int
		name string
	}
	var targets []pick
	for s, bf := range [][]string{f.Setup.P0.Battlefield, f.Setup.P1.Battlefield} {
		skipped := false
		for _, n := range bf {
			if n == card && s == 0 && !skipped {
				skipped = true
				continue
			}
			c, ok := reg.Lookup(n)
			if !ok || len(c.Faces) == 0 {
				return false
			}
			face := c.Faces[0]
			legal := false
			for _, r := range rs {
				if !r.known || ctrlOK(r.ctrl, s) && r.test(face) {
					legal = true
				}
			}
			if !legal {
				continue
			}
			// two different permanents are two different picks, and the
			// engines' AIs need not make the same one
			if pk := (pick{s, n}); !slices.Contains(targets, pk) {
				targets = append(targets, pk)
			}
			if len(targets) > 1 {
				return false
			}
			affects := false
			for _, a := range as {
				if a.known && !a.self && ctrlOK(a.ctrl, s) && a.test(face) {
					affects = true
				}
			}
			if !affects || !eff.shows(face, s) {
				return false
			}
		}
	}
	return true
}

// attachStart sets up the bearer of an attached static. An Aura is cast from
// hand (with exactly the lands its cost takes) on the bearer already on the
// battlefield: the focus is the cast, whose one legal target is the bearer. An
// Equipment sits on the battlefield beside its bearer with the lands of its
// equip cost, and the focus is the equip ability. The bearer meets the
// static's Affected filter as well as the Aura's or equip's target (enchanted
// and equipped are that filter, the attached property removed). Anything else
// attached (a Fortify, a Reconfigure, a Bestow) is refused.
func attachStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, enchanted, equipped string, eff staticEffect, f *StartFile) (reach, affected, why string) {
	const attached, noBearer = "no-generator:static:attached", "no-generator:static:bearer"
	generic := basicFor(face.ManaCost)
	p0 := &f.Setup.P0
	switch {
	case enchanted != "" && FieldBlock(face) == "aura-unattached":
		er, ok := enchantReq(face)
		if !ok {
			return "", "", attached
		}
		for _, tok := range splitCost(face.ManaCost) {
			if !manaTokenRE.MatchString(tok) || tok == "X" || tok == "S" {
				return "", "", attached
			}
		}
		reach = strings.Join(er.raw, ",")
		bearer, s := bearerFor(reg, b, card, reach, enchanted, generic, eff)
		if bearer == "" {
			return "", "", noBearer
		}
		p0.Battlefield = landsFor(splitCost(face.ManaCost), generic)
		p0.Hand = []string{card}
		placeOn(f, s, bearer)
		f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "cast"}
		return reach, enchanted, ""
	case equipped != "":
		for i, sa := range face.Abilities {
			if sa.Params["Keyword"] != "Equip" {
				continue
			}
			plan, why := planCost(reg, b, face, card, sa.Params["Cost"], generic)
			if why != "" {
				return "", "", why
			}
			if plan.zone != zoneBattlefield {
				return "", "", attached
			}
			bearer, s := bearerFor(reg, b, card, sa.Params["ValidTgts"], equipped, generic, eff)
			if bearer == "" {
				return "", "", noBearer
			}
			p0.Battlefield = nil
			applyCost(plan, card, generic, f)
			placeOn(f, s, bearer)
			f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "ability", Ability: i}
			return sa.Params["ValidTgts"], equipped, ""
		}
	}
	return "", "", attached
}

// staticCondition places what a static's condition reads, or refuses it as
// "no-generator:static:conditional". Understood: IsPresent (a battlefield count
// of at least n: n carriers meeting the filter), CheckSVar on the controller's
// life total, and the named conditions PlayerTurn (the start's active seat is
// seat 0), Hellbent (no card in hand), Metalcraft (three artifacts) and
// Threshold (seven cards in the graveyard). A static with none is unconditioned.
func staticCondition(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, st *cards.Static, eff staticEffect, f *StartFile) string {
	const refused = "no-generator:static:conditional"
	for _, k := range []string{"CheckSecondSVar", "SecondSVarCompare", "ConditionDefined", "ConditionPresent", "PresentDefined"} {
		if st.Params[k] != "" {
			return refused
		}
	}
	p0 := &f.Setup.P0
	if filter := st.Params["IsPresent"]; filter != "" {
		if z := st.Params["PresentZone"]; z != "" && z != "Battlefield" {
			return refused
		}
		n, ok := atLeast(st.Params["PresentCompare"])
		if !ok || n > 7 {
			return refused
		}
		pl := st.Params["PresentPlayer"]
		if pl != "" && pl != "You" && pl != "Any" && pl != "Opponent" {
			return refused
		}
		// a permanent the start already has (an Aura's bearer, an Equipment's)
		// counts, and so needs no second one a pick could be made between
		if pl != "Opponent" {
			n -= presentOn(reg, filter, card, eff.gain, f)
		}
		if n > 0 {
			name, s := filterCarrier(reg, b, card, filter, copyable(n))
			if name == "" {
				name, s = landCarrier(reg, filter, basicFor(face.ManaCost))
			}
			if name == "" {
				return refused
			}
			if pl == "Opponent" {
				s = 1
			}
			for i := 0; i < n; i++ {
				placeOn(f, s, name)
			}
		}
	} else if st.Params["PresentZone"] != "" || st.Params["PresentCompare"] != "" {
		return refused
	}
	if sv := st.Params["CheckSVar"]; sv != "" {
		if face.SVars[sv] != "Count$YourLifeTotal" {
			return refused
		}
		life, ok := lifeFor(st.Params["SVarCompare"])
		if !ok {
			return refused
		}
		p0.Life = &life
	} else if st.Params["SVarCompare"] != "" {
		return refused
	}
	switch c := st.Params["Condition"]; c {
	case "":
	case "PlayerTurn":
		if f.Active != 0 {
			return refused
		}
	case "Hellbent":
		if len(p0.Hand) > 0 {
			return refused
		}
	case "Metalcraft":
		name, s := filterCarrier(reg, b, card, "Artifact.YouCtrl", copyable(3))
		if name == "" {
			return refused
		}
		for i := 0; i < 3; i++ {
			placeOn(f, s, name)
		}
	case "Threshold":
		name := pickCarrier(reg, b, card, func(*cards.Face) bool { return true })
		if name == "" {
			return refused
		}
		for i := 0; i < 7; i++ {
			p0.Graveyard = append(p0.Graveyard, name)
		}
	default:
		return refused
	}
	return ""
}

// copyable is the permanents a start may place n of under one name: any, for
// one; for more, not a legendary one (the legend rule would put all but one
// in the graveyard, and the condition would not be met).
func copyable(n int) func(*cards.Face) bool {
	return func(f *cards.Face) bool { return f.IsPermanent() && (n <= 1 || !f.IsLegendary()) }
}

// presentOn counts the permanents the start has on the battlefield that meet
// a present filter, not the card itself. gain: the focus moves a permanent of
// seat 1 (the attached bearer) to seat 0 before the condition is read.
func presentOn(reg *cards.Registry, filter, card string, gain bool, f *StartFile) int {
	alts := parseFilter(filter)
	count := 0
	for s, bf := range [][]string{f.Setup.P0.Battlefield, f.Setup.P1.Battlefield} {
		skipped := false
		for _, n := range bf {
			if n == card && s == 0 && !skipped {
				skipped = true
				continue
			}
			c, ok := reg.Lookup(n)
			if !ok || len(c.Faces) == 0 {
				continue
			}
			end := s
			if gain {
				end = 0
			}
			for _, a := range alts {
				if a.known && !a.self && (a.ctrl == ctrlAny || a.ctrl == end) && a.test(c.Faces[0]) {
					count++
					break
				}
			}
		}
	}
	return count
}

// atLeast is the count a PresentCompare ("" is GE1, "GE2", "GT1") asks for.
func atLeast(cmp string) (int, bool) {
	if cmp == "" {
		return 1, true
	}
	op, num := cmp[:min(2, len(cmp))], strings.TrimPrefix(cmp, cmp[:min(2, len(cmp))])
	n, err := strconv.Atoi(num)
	switch {
	case err != nil || n < 0:
		return 0, false
	case op == "GE":
		return max(n, 1), true
	case op == "GT":
		return n + 1, true
	}
	return 0, false
}

// lifeFor is a life total (from the start's 20) that meets an SVarCompare
// ("GE25", "LE5"): the bound itself, one past it for a strict comparison.
func lifeFor(cmp string) (int32, bool) {
	if len(cmp) < 3 {
		return 0, false
	}
	n, err := strconv.Atoi(cmp[2:])
	if err != nil || n < 1 || n > 1000 {
		return 0, false
	}
	switch cmp[:2] {
	case "GE", "LE", "EQ":
		return int32(n), true
	case "GT":
		return int32(n + 1), true
	case "LT":
		return int32(n - 1), true
	}
	return 0, false
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
