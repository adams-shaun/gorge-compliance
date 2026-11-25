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

// conditionalParams are the static parameters whose truth a start cannot
// establish: a condition on history or a named choice, a presence check, or an
// SVar comparison. A start places position, not history, so the key is refused
// by name rather than set up with a condition the position cannot make true.
var conditionalParams = []string{"Condition", "CheckSVar", "IsPresent", "PresentZone",
	"CheckSecondSVar", "CheckThirdSVar", "ConditionDefined"}

// staticKeyStart is a static key (static#F.S) whose effect reads an object
// beyond the card: a Continuous static whose Affected filter some carrier
// meets ("other creatures you control have trample"). The card is placed in the
// zone the static works from with one such carrier, on the seat the filter
// names, and the game plays on with no forced action: the static applies to the
// carrier from the start. The card itself is the affected object when the
// static reads Self.
//
// Three shapes need more than a placement and get their own setup:
//   - ReduceCost makes the focus spell affordable only with the reduction;
//   - a granted ability (AddAbility) is observed through the offered options,
//     with the counters, mana and target the grant needs to be offered;
//   - a play permission from another zone is exercised by focusing the cast
//     from that zone.
//
// A computed amount (AddPower:X) is observed when its SVar counts a board the
// start can place; the refused cases keep the reason that names the gap.
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
	for _, k := range conditionalParams {
		if st.Params[k] != "" {
			return "no-generator:static:conditional"
		}
	}
	switch st.ModeKind() {
	case cards.StaticContinuous:
		return continuousStaticKeyStart(reg, b, face, card, st, f)
	case cards.StaticReduceCost:
		return reduceCostKeyStart(reg, b, face, card, st, f)
	}
	if st.Mode == "CantBlockBy" {
		return cantBlockByAttackerStart(reg, b, face, card, st, f)
	}
	return "no-generator:static:" + st.Mode
}

// cantBlockByAttackerStart is a CantBlockBy static whose own card is the
// attacker it names ("CARDNAME can't be blocked by ..."): the card attacks and
// the opponent's matching creature is there to block, so an engine that misses
// the restriction blocks and the combat differs. The other reading of the mode
// (the card is the blocker, "can block only ...") is not a forced action: the
// card is simply not offered as a blocker, so it keeps its refusal.
func cantBlockByAttackerStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, st *cards.Static, f *StartFile) string {
	attacker := false
	for _, k := range []string{"ValidAttacker", "Affected", "ValidCard"} {
		attacker = attacker || filterNamesSelf(st.Params[k])
	}
	if !attacker || !face.IsCreature() || face.HasKeyword("Defender") || deadOnArrival(face) {
		return "no-generator:static:CantBlockBy"
	}
	blocker := combatFixture
	if vb := strings.TrimSpace(st.Params["ValidBlocker"]); vb != "" {
		name, s := fixtureCarrier(reg, b, card, vb, (*cards.Face).IsCreature)
		if name == "" || s != 0 {
			return "no-generator:static:CantBlockBy"
		}
		blocker = name
	}
	f.Setup.P0.Battlefield = []string{card}
	f.Setup.P1.Battlefield = []string{blocker}
	f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "attack"}
	return ""
}

// filterNamesSelf reports whether some alternative of filter reads Self.
func filterNamesSelf(filter string) bool {
	for _, a := range parseFilterWithCounters(filter) {
		if a.self {
			return true
		}
	}
	return false
}

// staticSourceZone is the zone the static works from: EffectZone when it names
// one the start can place the card in, else the battlefield. An AffectedZone
// naming another zone is only one the start can serve when the card itself is
// the affected object there (Alien Symbiosis: a graveyard play permission);
// otherwise the affected objects are somewhere the start does not model.
func staticSourceZone(st *cards.Static) (zone, why string) {
	switch z := strings.TrimSpace(st.Params["EffectZone"]); z {
	case "", "Battlefield", "All":
		zone = "Battlefield"
	case "Graveyard", "Exile", "Hand", "Library":
		zone = z
	default:
		return "", "no-generator:static:zone"
	}
	if az := strings.TrimSpace(st.Params["AffectedZone"]); az != "" && az != "Battlefield" && az != "All" && selfStatic(st) {
		if zone == "Battlefield" {
			zone = az
		}
		if zone != az {
			return "", "no-generator:static:zone"
		}
	}
	return zone, ""
}

// continuousStaticKeyStart sets up a Continuous static: the source in its zone,
// the affected carrier (or the card itself), the counters, targets and mana a
// granted ability needs to be offered, and the comparison fields the effect
// shows in.
func continuousStaticKeyStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, st *cards.Static, f *StartFile) string {
	zone, why := staticSourceZone(st)
	if why != "" {
		return why
	}
	aff := strings.TrimSpace(st.Params["Affected"])
	if aff == "" {
		aff = strings.TrimSpace(st.Params["ValidCard"])
	}
	self := selfStatic(st) || aff == ""
	if aff == "" {
		aff = "Card.Self"
	}
	if strings.Contains(aff, "EnchantedBy") || strings.Contains(aff, "EquippedBy") {
		return "no-generator:static:attached"
	}
	if playerFilter(aff) {
		// "You have hexproof": the affected object is a player, which the
		// snapshot does not compare, and no permanent can stand in.
		return "no-generator:static:unobserved"
	}
	plan, why := staticObserved(st, face)
	if why != "" {
		return why
	}
	// A play permission whose affected cards are in another zone (hand,
	// library, graveyard, exile) places one playable card there and focuses
	// that play. The source itself stays where its static works from.
	az := strings.TrimSpace(st.Params["AffectedZone"])
	permission := ""
	if az != "" && az != "Battlefield" && az != "All" && az != zone {
		if st.Params["MayPlay"] == "" {
			return "no-generator:static:zone"
		}
		switch az {
		case "Hand", "Graveyard", "Exile", "Library":
			permission = az
		default:
			return "no-generator:static:zone"
		}
	}
	placeCard(f, zone, card)
	if len(plan.selfCounters) > 0 && zone != "Battlefield" {
		return "no-generator:static:computed"
	}
	gates := filterGateCounters(aff, face, true)
	if len(gates) > 0 && zone != "Battlefield" {
		return "no-generator:static:computed"
	}
	f.Setup.P0.Counters = append(f.Setup.P0.Counters, plan.selfCounters...)
	f.Setup.P0.Counters = append(f.Setup.P0.Counters, gates...)
	for _, need := range plan.counts {
		if placeCount(reg, b, card, need, f) == "" {
			return "no-generator:static:computed"
		}
	}
	if permission != "" {
		return permissionPlayStart(reg, b, card, st, aff, permission, plan, f)
	}
	// A static whose affected objects are attacking needs a scripted attack:
	// the effect shows at the declare-attackers checkpoint, not at the point
	// the start opens.
	if attackAff, attacking := withoutFilterProp(aff, "attacking"); attacking {
		if zone != "Battlefield" {
			return "no-generator:static:zone"
		}
		return attackingStaticStart(reg, b, card, attackAff, self, plan, f)
	}
	if self {
		if why := grantedAbilityFixtures(reg, b, face, st, card, f); why != "" {
			return why
		}
		f.Compare = plan.compare
		if zone != "Battlefield" && st.Params["MayPlay"] != "" {
			return playFromZone(face, card, st, f)
		}
		return ""
	}
	name, s := fixtureCarrier(reg, b, card, aff, (*cards.Face).IsPermanent)
	if name == "" {
		name, s = landCarrier(reg, aff, basicFor(face.ManaCost))
	}
	if name == "" {
		return "no-generator:static:affected"
	}
	placeOnSeat(f, s, name)
	if cf, ok := reg.Lookup(name); ok && len(cf.Faces) > 0 {
		gates := filterGateCounters(aff, cf.Faces[0], false)
		if s == 1 {
			f.Setup.P1.Counters = append(f.Setup.P1.Counters, gates...)
		} else {
			f.Setup.P0.Counters = append(f.Setup.P0.Counters, gates...)
		}
	}
	if why := grantedAbilityFixtures(reg, b, face, st, card, f); why != "" {
		return why
	}
	f.Compare = plan.compare
	return ""
}

// playerFilter reports whether an alternative of filter names a player as the
// affected object.
func playerFilter(filter string) bool {
	for _, alt := range strings.Split(filter, ",") {
		head, _, _ := strings.Cut(strings.TrimSpace(alt), ".")
		if head == "You" || head == "Player" {
			return true
		}
	}
	return false
}

// withoutFilterProp removes one property from every alternative of filter and
// reports whether it was there. "attacking" is a position the start scripts
// (the focus attack), not a property of the carrier's face.
func withoutFilterProp(filter, prop string) (string, bool) {
	var alts []string
	found := false
	for _, raw := range strings.Split(filter, ",") {
		head, props, _ := strings.Cut(strings.TrimSpace(raw), ".")
		if props == "" {
			alts = append(alts, raw)
			continue
		}
		var keep []string
		for _, p := range strings.Split(props, "+") {
			if p == prop {
				found = true
				continue
			}
			keep = append(keep, p)
		}
		if len(keep) == 0 {
			alts = append(alts, head)
		} else {
			alts = append(alts, head+"."+strings.Join(keep, "+"))
		}
	}
	return strings.Join(alts, ","), found
}

// attackingStaticStart is a Continuous static whose affected objects are
// attacking: the source (or the affected carrier) attacks, and the comparison
// at the combat checkpoints reads the effect. A Self static's source is the
// attacker; otherwise one carrier the filter accepts is.
func attackingStaticStart(reg *cards.Registry, b *certdeck.Builder, card, aff string, self bool, plan staticObserve, f *StartFile) string {
	attacker := card
	if !self {
		name, s := fixtureCarrier(reg, b, card, aff, (*cards.Face).IsCreature)
		if name == "" || s != 0 {
			return "no-generator:static:affected"
		}
		placeOnSeat(f, 0, name)
		if cf, ok := reg.Lookup(name); ok && len(cf.Faces) > 0 {
			f.Setup.P0.Counters = append(f.Setup.P0.Counters, filterGateCounters(aff, cf.Faces[0], false)...)
		}
		attacker = name
	}
	c, ok := reg.Lookup(attacker)
	if !ok || len(c.Faces) == 0 {
		return "no-generator:static:affected"
	}
	af := c.Faces[0]
	if !af.IsCreature() || af.HasKeyword("Defender") || deadOnArrival(af) {
		return "no-generator:static:affected"
	}
	f.Focus = &StartFocus{Seat: seat(0), Card: attacker, Kind: "attack"}
	f.Compare = plan.compare
	return ""
}

// placeCard puts name in zone of seat 0. A battlefield placement replaces the
// seat's list (as every point-start does: the start lists its whole board); the
// other zones append, because a start may already have put a target or fodder
// there.
func placeCard(f *StartFile, zone, name string) {
	switch zone {
	case "Graveyard":
		f.Setup.P0.Graveyard = append(f.Setup.P0.Graveyard, name)
	case "Exile":
		f.Setup.P0.Exile = append(f.Setup.P0.Exile, name)
	case "Hand":
		f.Setup.P0.Hand = append(f.Setup.P0.Hand, name)
	case "Library":
		f.Setup.P0.Library = append(f.Setup.P0.Library, name)
	default:
		f.Setup.P0.Battlefield = []string{name}
	}
}

// placeOnSeat puts name on seat s's battlefield.
func placeOnSeat(f *StartFile, s int, name string) {
	if s == 1 {
		f.Setup.P1.Battlefield = append(f.Setup.P1.Battlefield, name)
		return
	}
	f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, name)
}

// grantedAbilityFixtures places what a granted ability needs to be offered: the
// mana for its activation cost and a permanent the ability can target. The
// offered comparison then differs from a board without the grant.
func grantedAbilityFixtures(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, st *cards.Static, card string, f *StartFile) string {
	ab := strings.TrimSpace(st.Params["AddAbility"])
	if ab == "" {
		return ""
	}
	sa := cards.ResolveSVar(face.SVars, ab)
	if sa == nil {
		return "no-generator:static:unobserved"
	}
	var mana []string
	for _, tok := range splitCost(sa.Params["Cost"]) {
		if manaTokenRE.MatchString(tok) {
			mana = append(mana, tok)
		}
	}
	f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, landsFor(mana, basicFor(face.ManaCost))...)
	vt := strings.TrimSpace(sa.Params["ValidTgts"])
	if vt == "" {
		return ""
	}
	for _, a := range parseFilter(vt) {
		if a.self {
			return "" // the source itself is the target: no fixture needed
		}
	}
	name, s := fixtureCarrier(reg, b, card, vt, (*cards.Face).IsPermanent)
	if name == "" {
		return "no-generator:static:affected"
	}
	placeOnSeat(f, s, name)
	return ""
}

// playFromZone focuses the cast of a card whose own static permits it to be
// played from another zone: the mana for its printed cost, the extra card a
// Discard RaiseCost needs, and a vanilla bearer when it is an Aura.
func playFromZone(face *cards.Face, card string, st *cards.Static, f *StartFile) string {
	cost := face.ManaCost
	if cost == "" || cost == "no cost" {
		return "no-generator:static:zone"
	}
	f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, landsFor(splitCost(cost), basicFor(cost))...)
	if face.HasTypeWord("Aura") {
		f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, combatFixture)
	}
	if why := payRaiseCost(st, basicFor(cost), f); why != "" {
		return why
	}
	f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "cast"}
	return ""
}

// payRaiseCost pays the extra cost of a play permission the start can meet: a
// plain discard. Anything else (sacrifice, remove counters, pay life) needs a
// fixture this helper does not size, so the key keeps its zone refusal.
func payRaiseCost(st *cards.Static, generic string, f *StartFile) string {
	var mana []string
	for _, tok := range splitCost(st.Params["RaiseCost"]) {
		if m := raiseDiscardRE.FindStringSubmatch(tok); m != nil {
			n, _ := strconv.Atoi(m[1])
			for range n {
				f.Setup.P0.Hand = append(f.Setup.P0.Hand, combatFixture)
			}
			continue
		}
		if m := removeAnyCounterRE.FindStringSubmatch(tok); m != nil {
			n, _ := strconv.Atoi(m[1])
			found := false
			for _, n := range f.Setup.P0.Battlefield {
				found = found || n == combatFixture
			}
			if !found {
				f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, combatFixture)
			}
			f.Setup.P0.Counters = append(f.Setup.P0.Counters,
				StartCounter{Card: combatFixture, Kind: "CHARGE", N: int32(n)})
			continue
		}
		if manaTokenRE.MatchString(tok) {
			mana = append(mana, tok)
			continue
		}
		if payLifeRE.MatchString(tok) {
			continue // a 20-life seat can pay it; the engine does at the cast
		}
		return "no-generator:static:zone"
	}
	if len(mana) > 0 {
		f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, landsFor(mana, generic)...)
	}
	return ""
}

var (
	raiseDiscardRE     = regexp.MustCompile(`^Discard<([0-9]+)/[^>]+>$`)
	removeAnyCounterRE = regexp.MustCompile(`^RemoveAnyCounter<([0-9]+)/Any/[^>]+>$`)
)

// permissionPlayStart is a MayPlay static whose affected cards are in a zone
// other than the source's (Omniscience's hand, a top-of-library play): one
// playable card of the filter is placed there and the focus plays it, so the
// offered options differ from a board without the permission.
func permissionPlayStart(reg *cards.Registry, b *certdeck.Builder, card string, st *cards.Static, aff, zone string, plan staticObserve, f *StartFile) string {
	mayFree := strings.EqualFold(strings.TrimSpace(st.Params["MayPlayWithoutManaCost"]), "True")
	name, s := fixtureCarrier(reg, b, card, aff, func(f *cards.Face) bool {
		if f.IsLand() {
			return true
		}
		return mayFree || f.ManaCost != "" && f.ManaCost != "no cost"
	})
	if name == "" {
		name = landFixture(reg, b, aff)
	}
	if name == "" || s != 0 {
		return "no-generator:static:affected"
	}
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return "no-generator:static:affected"
	}
	pf := c.Faces[0]
	kind := "cast"
	if pf.IsLand() {
		kind = "land"
	}
	placeInZone(f, 0, zone, name)
	if kind == "cast" && !mayFree {
		f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, landsFor(splitCost(pf.ManaCost), basicFor(pf.ManaCost))...)
	}
	if kind == "cast" && pf.HasTypeWord("Aura") {
		f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, combatFixture)
	}
	if why := payRaiseCost(st, basicFor(pf.ManaCost), f); why != "" {
		return why
	}
	f.Focus = &StartFocus{Seat: seat(0), Card: name, Kind: kind}
	f.Compare = plan.compare
	return ""
}

// placeInZone appends name to seat s's zone (on the top of the library, when
// that is the zone).
func placeInZone(f *StartFile, s int, zone, name string) {
	seat := &f.Setup.P0
	if s == 1 {
		seat = &f.Setup.P1
	}
	switch zone {
	case "Hand":
		seat.Hand = append(seat.Hand, name)
	case "Graveyard":
		seat.Graveyard = append(seat.Graveyard, name)
	case "Exile":
		seat.Exile = append(seat.Exile, name)
	case "Library":
		seat.Library = append(seat.Library, name)
	default:
		seat.Battlefield = append(seat.Battlefield, name)
	}
}

// reduceCostKeyStart sets up a ReduceCost static so the reduction is the only
// reason the focus spell is affordable: the printed cost is one (or Amount)
// generic mana more than the lands placed. A Card.Self reduction is the card
// cast from hand; any other reduction leaves the source on the battlefield and
// casts a cheap spell the ValidCard filter accepts.
func reduceCostKeyStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, st *cards.Static, f *StartFile) string {
	if strings.EqualFold(st.Params["Type"], "Ability") {
		// the reduction is on an activated ability, not a spell: a start would
		// need an activation fixture whose cost it can size; not built.
		return "no-generator:static:ReduceCost"
	}
	plan, why := reduceCostPlan(face, st)
	if why != "" {
		return why
	}
	self := strings.EqualFold(st.Params["ValidCard"], "Card.Self")
	// A self reduction happens on the stack, from hand; another reduction needs
	// its source on the battlefield. A static that works from the command zone
	// (a scheme or vanguard) has no zone the start can place it in.
	if ez := strings.TrimSpace(st.Params["EffectZone"]); !self && ez != "" && ez != "Battlefield" && ez != "All" ||
		self && ez != "" && ez != "All" && ez != "Stack" {
		return "no-generator:static:zone"
	}
	spell := card
	spellFace := face
	if self && (face.IsLand() || len(plan.selfCounters) > 0) {
		return "no-generator:static:ReduceCost"
	}
	if !self {
		if !face.IsPermanent() {
			return "no-generator:static:ReduceCost"
		}
		f.Setup.P0.Battlefield = []string{card}
		name, sf, why := reduceCostSpell(reg, b, card, st, plan.amount)
		if why != "" {
			return why
		}
		spell, spellFace = name, sf
	}
	lands, ok := reducedLands(spellFace.ManaCost, plan.amount)
	if !ok {
		return "no-generator:static:computed"
	}
	f.Setup.P0.Hand = []string{spell}
	f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, lands...)
	f.Setup.P0.Counters = append(f.Setup.P0.Counters, plan.selfCounters...)
	for _, need := range plan.counts {
		if placeCount(reg, b, spell, need, f) == "" {
			return "no-generator:static:computed"
		}
	}
	f.Focus = &StartFocus{Seat: seat(0), Card: spell, Kind: "cast"}
	if spellFace.HasTypeWord("Aura") {
		f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, combatFixture)
	}
	return ""
}

// reducePlan is a ReduceCost static's reduction: a fixed amount, or one per
// object an SVar counts.
type reducePlan struct {
	amount       int
	counts       []countNeed
	selfCounters []StartCounter
}

// reduceCostPlan reads Amount. A literal is the reduction; an SVar the start
// can count ("Count$Valid Artifact.YouCtrl", "Count$CardCounters.P1P1") is made
// one by placing the object it counts. Anything else is a computed amount the
// placement cannot size.
func reduceCostPlan(face *cards.Face, st *cards.Static) (reducePlan, string) {
	amount := strings.TrimSpace(st.Params["Amount"])
	if n, err := strconv.Atoi(strings.TrimPrefix(amount, "+")); err == nil {
		if n < 0 {
			return reducePlan{}, "no-generator:static:computed"
		}
		return reducePlan{amount: n}, ""
	}
	need, counter, ok := countSource(amount, face)
	if !ok {
		return reducePlan{}, "no-generator:static:computed"
	}
	p := reducePlan{amount: 1}
	if need.filter != "" {
		p.counts = append(p.counts, need)
	}
	if counter != "" {
		p.selfCounters = append(p.selfCounters, StartCounter{Card: face.Name, Kind: counter, N: 1})
	}
	return p, ""
}

// reduceCostSpell is a cheap spell the static's ValidCard filter accepts and
// whose generic cost the reduction can lower: the least busy matching card,
// preferring an inert one so the focus cast needs no target or trigger.
func reduceCostSpell(reg *cards.Registry, b *certdeck.Builder, card string, st *cards.Static, amount int) (string, *cards.Face, string) {
	valid := strings.TrimSpace(st.Params["ValidCard"])
	if valid == "" {
		valid = "Card"
	}
	color := strings.TrimSpace(st.Params["Color"])
	usable := func(f *cards.Face) bool {
		if f.IsLand() || f.ManaCost == "" || f.ManaCost == "no cost" || genericCount(f.ManaCost) < amount {
			return false
		}
		return color == "" || hasShard(f.ManaCost, color[0])
	}
	for _, f := range carrierPool(reg, b) {
		if f.Name == card || !usable(f) || !filterAccepts(valid, f) {
			continue
		}
		return f.Name, f, ""
	}
	name := pickFixture(reg, b, card, func(f *cards.Face) bool { return usable(f) && filterAccepts(valid, f) }, nil, true)
	if name == "" {
		return "", nil, "no-generator:static:ReduceCost"
	}
	c, _ := reg.Lookup(name)
	return name, c.Faces[0], ""
}

// filterAccepts reports whether some non-Self alternative of filter matches f.
func filterAccepts(filter string, f *cards.Face) bool {
	for _, a := range parseFilterWithCounters(filter) {
		if a.known && !a.self && a.test(f) {
			return true
		}
	}
	return false
}

// genericCount is the generic mana of a printed cost.
func genericCount(cost string) int {
	n := 0
	for _, tok := range splitCost(cost) {
		if v, err := strconv.Atoi(tok); err == nil {
			n += v
		}
	}
	return n
}

// reducedLands is the mana base for a spell whose generic cost an Amount
// reduction lowers: the printed lands less Amount generic basics. It is false
// when the cost has less generic mana than the reduction (the price would not
// change), so no reduction could be observed.
func reducedLands(cost string, amount int) ([]string, bool) {
	if genericCount(cost) < amount {
		return nil, false
	}
	lands := landsFor(splitCost(cost), basicFor(cost))
	if amount == 0 {
		return lands, true
	}
	generic := basicFor(cost)
	out := make([]string, 0, len(lands)-amount)
	removed := 0
	for _, l := range lands {
		if removed < amount && l == generic {
			removed++
			continue
		}
		out = append(out, l)
	}
	if removed < amount {
		return nil, false
	}
	return out, true
}

// staticObserve is what the start's comparison reads of a static's effect, and
// the fixtures a computed amount needs to be non-zero.
type staticObserve struct {
	compare      []string
	counts       []countNeed
	selfCounters []StartCounter
}

// countNeed is one object a computed amount counts, and the zone it is counted
// in ("" is the battlefield).
type countNeed struct {
	filter string
	zone   string
}

// addCount appends need unless it is already listed.
func addCount(c []countNeed, need countNeed) []countNeed {
	for _, x := range c {
		if x == need {
			return c
		}
	}
	return append(c, need)
}

// placeCount places one object of need's filter in need's zone and returns its
// name, or "" when no fixture matches. The amount then reads one instead of
// zero, which is all the start needs for the effect to show.
func placeCount(reg *cards.Registry, b *certdeck.Builder, card string, need countNeed, f *StartFile) string {
	name, s := countFixture(reg, b, card, need.filter)
	if name == "" {
		return ""
	}
	zone := need.zone
	if zone == "" {
		zone = "Battlefield"
	}
	placeInZone(f, s, zone, name)
	return name
}

// addCompare appends field unless it is already listed.
func addCompare(c []string, field string) []string {
	for _, x := range c {
		if x == field {
			return c
		}
	}
	return append(c, field)
}

// staticObserved is how the start observes a Continuous static's effect, or why
// it cannot. The comparison reads the permanents' P/T, types, colours, controller
// and counters; keywords when the item opts in; and the offered options when the
// effect grants an ability or a play permission. So an effect that only changes
// what the permanent can do (a granted ability, a play permission) sets the
// offered opt-in, a keyword grant or removal sets the keyword opt-in (the
// evergreen vocabulary, else the named one), and a pump or set whose amount is
// an SVar over a board the start can place reports the filter that amount
// counts, so one object makes it non-zero. An amount with no such source is a
// computed count the placed objects do not make: the start would read it at
// zero, and the key is refused.
func staticObserved(st *cards.Static, face *cards.Face) (staticObserve, string) {
	var out staticObserve
	observed := false
	for _, k := range []string{"AddPower", "AddToughness", "SetPower", "SetToughness"} {
		v := strings.TrimSpace(st.Params[k])
		if v == "" {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimPrefix(v, "+")); err == nil {
			observed = true
			continue
		}
		need, counter, ok := countSource(v, face)
		if !ok {
			return staticObserve{}, "no-generator:static:computed"
		}
		if need.filter != "" {
			out.counts = addCount(out.counts, need)
		}
		if counter != "" {
			out.selfCounters = append(out.selfCounters, StartCounter{Card: face.Name, Kind: counter, N: 1})
		}
		observed = true
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
		out.compare = addCompare(out.compare, oraclediff.CompareKeywords)
		observed = true
	case oraclediff.ComparedKeywords(kws, true) != "":
		out.compare = addCompare(out.compare, oraclediff.CompareKeywordsNamed)
		observed = true
	}
	if ab := strings.TrimSpace(st.Params["AddAbility"]); ab != "" {
		// A grant whose abilities come from exiled cards is a linkage a start
		// cannot place (ExiledWithSource): the offered options would not change.
		if st.Params["GainsAbilitiesOf"] != "" || cards.ResolveSVar(face.SVars, ab) == nil {
			return staticObserve{}, "no-generator:static:unobserved"
		}
		out.compare = addCompare(out.compare, oraclediff.CompareOffered)
		observed = true
	}
	if st.Params["MayPlay"] != "" {
		out.compare = addCompare(out.compare, oraclediff.CompareOffered)
		observed = true
	}
	if !observed {
		return staticObserve{}, "no-generator:static:unobserved"
	}
	return out, ""
}

// countSource reads a computed amount: a face SVar naming a plain
// "Count$Valid <filter>" board count, or a "Count$CardCounters.<KIND>" count on
// the source itself. A filtered or modified expression ("$Colors", "/Plus.1",
// Count$CardPower) is not a board count the start can make non-zero.
func countSource(v string, face *cards.Face) (need countNeed, counter string, ok bool) {
	expr := v
	if e, found := face.SVars[v]; found {
		expr = e
	}
	zone := ""
	if rest, found := strings.CutPrefix(expr, "Count$Valid "); found {
		zone = ""
		expr = rest
	} else if rest, found := strings.CutPrefix(expr, "Count$ValidGraveyard "); found {
		zone = "Graveyard"
		expr = rest
	} else if rest, found := strings.CutPrefix(expr, "Count$CardCounters."); found {
		rest = strings.TrimSpace(rest)
		if rest == "" || strings.Contains(rest, "/") {
			return countNeed{}, "", false
		}
		return countNeed{}, rest, true
	} else {
		return countNeed{}, "", false
	}
	filter := strings.TrimSpace(expr)
	if filter == "" || strings.ContainsAny(filter, "$/") {
		return countNeed{}, "", false
	}
	return countNeed{filter: filter, zone: zone}, "", true
}
