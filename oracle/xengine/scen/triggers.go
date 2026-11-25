package scen

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Forced-line starts for trigger keys whose event the start must cause (the
// forced start, recipe.go). Each returns "" when it built the start into f,
// else the refusal.

// phaseSeat is the seat whose step a Phase trigger's ValidPlayer names: 0 for
// You, 1 for Opponent, -1 for any player (absent, Player). ok is false for a
// player filter the start cannot meet (You.descended reads this turn's
// history).
func phaseSeat(vp string) (seat int, why string) {
	switch vp {
	case "You":
		return 0, ""
	case "Opponent":
		return 1, ""
	case "", "Player":
		return -1, ""
	}
	if _, prop, ok := strings.Cut(vp, "."); ok {
		return 0, "history:" + strings.ToLower(prop)
	}
	return 0, "no-generator:trigger:Phase:" + vp
}

// phaseStart is a beginning-of-step trigger ("at the beginning of your end
// step", "at the beginning of combat on your turn", "at the beginning of your
// upkeep"). A start's span begins at turn 1's first precombat main phase, so:
//
//   - a step later in turn 1 (beginning of combat, the second main phase, the
//     end step) is reached with the card already on the battlefield, the turn
//     the named player's (Active); "if it is tapped" (IsPresent Card.tapped,
//     the DSK Survivors) is met by the card attacking (focus attack), which
//     taps it before the second main phase;
//   - a step before it (upkeep, draw, the first main phase) cannot be reached
//     from a placed card (the lowering refuses a battlefield upkeep or draw
//     trigger at the cut, which the setup drive would fire a second time), so
//     the card is cast from hand on turn 1 with exactly its mana, and the game
//     runs to the named player's next such step: turn 3 for You, turn 2 for an
//     opponent or any player.
func phaseStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, generic string, trig *cards.Trigger, f *StartFile) string {
	if tz := trig.Params["TriggerZones"]; tz != "" && tz != "Battlefield" {
		return "no-generator:trigger:Phase"
	}
	seat, why := phaseSeat(trig.Params["ValidPlayer"])
	if why != "" {
		return why
	}
	p0 := &f.Setup.P0
	ph := trig.Params["Phase"]
	late := ph == "BeginCombat" || ph == "Main2" || ph == "End of Turn" || ph == "Main" && trig.Params["PhaseCount"] == "2"
	early := ph == "Upkeep" || ph == "Draw" || ph == "Main1"
	present := trig.Params["IsPresent"]
	tapped := strings.Contains(present, "tapped") && !strings.Contains(present, "untapped")
	cast := func() string {
		if !face.IsPermanent() {
			return "no-generator:trigger:Phase"
		}
		return castStart(reg, b, face, card, generic, f)
	}
	switch {
	case late && tapped:
		// "if it is tapped", "if you control a tapped creature": the card
		// attacks, on the battlefield since the turn began (no summoning
		// sickness).
		if !face.IsCreature() || seat == 1 || !selfOrYourCreature(trig) {
			return "no-generator:trigger:Phase:tapped"
		}
		p0.Battlefield = []string{card}
		f.Active, f.MaxTurns = 0, 1
		f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Kind: "attack"}
	case late && upkeepOrDraw(face):
		// The card's own upkeep or draw trigger would refuse a battlefield
		// placement at the cut (lower.phaseTriggered): cast it on turn 1, before
		// the step.
		if seat == 1 {
			return "no-generator:trigger:Phase:upkeep-card"
		}
		if why := cast(); why != "" {
			return why
		}
		f.Active, f.MaxTurns = 0, 1
	case late:
		p0.Battlefield = []string{card}
		f.Active, f.MaxTurns = max(seat, 0), 1
	case early:
		if why := cast(); why != "" {
			return why
		}
		f.Active, f.MaxTurns = 0, 2
		if seat == 0 {
			f.MaxTurns = 3
		}
	default:
		return "no-generator:trigger:Phase"
	}
	presentCarrier(reg, b, face, card, trig, f)
	return ""
}

// presentCarrier meets an intervening "if you control an artifact" (IsPresent
// with no PresentDefined or PresentCompare: at least one) with a carrier
// permanent placed on the battlefield of the seat the filter names, unless
// the card itself already meets it.
func presentCarrier(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, f *StartFile) {
	present := trig.Params["IsPresent"]
	if present == "" || strings.Contains(present, "Self") || strings.Contains(present, "tapped") ||
		trig.Params["PresentDefined"] != "" || trig.Params["PresentCompare"] != "" {
		return
	}
	for _, a := range parseFilter(present) {
		if a.known && a.ctrl != ctrlOpp && a.matches(face, false) && f.Focus == nil {
			return // the card, on the battlefield, is the permanent
		}
	}
	name, s := filterCarrier(reg, b, card, present, (*cards.Face).IsPermanent)
	if name == "" {
		return
	}
	seat := &f.Setup.P0
	if s == 1 {
		seat = &f.Setup.P1
	}
	seat.Battlefield = append(seat.Battlefield, name)
}

// selfOrYourCreature reports whether a Phase trigger's tapped condition reads
// the card itself (PresentDefined Self) or a creature its controller
// controls: an attack by the card meets either.
func selfOrYourCreature(trig *cards.Trigger) bool {
	p := trig.Params["IsPresent"]
	if trig.Params["PresentDefined"] == "Self" {
		return p == "Card.tapped"
	}
	return p == "Creature.YouCtrl+tapped" || p == "Creature.tapped+YouCtrl"
}

// upkeepOrDraw reports a beginning-of-upkeep or draw-step trigger on face (the
// lowering's phaseTriggered, read from the card).
func upkeepOrDraw(face *cards.Face) bool {
	for _, t := range face.Triggers {
		if !strings.EqualFold(t.Mode, "Phase") {
			continue
		}
		ph := strings.ToLower(t.Params["Phase"])
		if strings.Contains(ph, "upkeep") || strings.Contains(ph, "draw") {
			return true
		}
	}
	return false
}

func seatPtr(n int) *int { return &n }

// triggerFilter is a zone-change trigger's moved-card filter: ValidCard, or
// ValidCards for the one-or-more copy (ChangesZoneAll). One reader so the
// carrier recipe and the token-need skip cannot disagree on the filter.
func triggerFilter(trig *cards.Trigger) string {
	if trig.Mode == "ChangesZoneAll" {
		if filter := trig.Params["ValidCards"]; filter != "" {
			return filter
		}
	}
	return trig.Params["ValidCard"]
}

// zoneStart is a ChangesZone trigger on another card, the card on the
// battlefield (TriggerZones Battlefield):
//
//   - an enters trigger ("whenever another creature you control enters",
//     landfall) is the carrier entering: a one-token maker cast when the
//     filter takes a token (tokenCarrier), a basic land played from hand when
//     the filter takes a land, else a carrier permanent (carrierPool) cast
//     from hand with exactly the basics its cost takes, by the seat the
//     filter names, on that seat's turn;
//   - a dies trigger ("whenever another creature you control dies") is a
//     sacrifice carrier (sacCarrier) on the named seat's battlefield
//     sacrificing itself, on that seat's turn;
//   - a filter only damage history can meet (Creature.DamagedBy) is
//     damageStart: the card attacks and a blocker it kills dies to combat
//     damage;
//   - a filter only an attachment can meet (Card.AttachedBy) is
//     attachedByStart: an Aura on its bearer (StartAttach) murdered, or an
//     Equipment attached with Magnetic Theft to an end-step sacrifice.
func zoneStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, f *StartFile) string {
	const none = "no-generator:trigger:ChangesZone"
	if tz := trig.Params["TriggerZones"]; tz != "" && tz != "Battlefield" || selfCard(trig) {
		return none
	}
	filter := triggerFilter(trig)
	origin, dest := trig.Params["Origin"], trig.Params["Destination"]
	if filterHasProp(filter, "DamagedBy") {
		return damageStart(reg, b, face, card, f)
	}
	if filterHasProp(filter, "AttachedBy") {
		return attachedByStart(reg, b, face, card, f)
	}
	f.Setup.P0.Battlefield = []string{card}
	seats := [2]*StartSeat{&f.Setup.P0, &f.Setup.P1}
	switch {
	case dest == "Battlefield" && (origin == "" || origin == "Any" || origin == "Hand"):
		if name, s := tokenCarrier(reg, b, card, filter); name != "" {
			castCarrier(reg, seats[s], name)
			f.Active = s
			f.Focus = &StartFocus{Seat: seatPtr(s), Card: name, Kind: "cast"}
			return ""
		}
		if land, s := landCarrier(reg, filter, basicFor(face.ManaCost)); land != "" {
			seats[s].Hand = append(seats[s].Hand, land)
			f.Active = s
			f.Focus = &StartFocus{Seat: seatPtr(s), Card: land, Kind: "land"}
			return ""
		}
		name, s := filterCarrier(reg, b, card, filter, (*cards.Face).IsPermanent)
		if name == "" {
			return none + ":filter"
		}
		castCarrier(reg, seats[s], name)
		f.Active = s
		f.Focus = &StartFocus{Seat: seatPtr(s), Card: name, Kind: "cast"}
	case origin == "Battlefield" && (dest == "Graveyard" || dest == "Any"):
		name, s := sacCarrier(reg, b, card, filter)
		if name == "" {
			return none + ":filter"
		}
		seats[s].Battlefield = append(seats[s].Battlefield, name)
		seats[s].Battlefield = append(seats[s].Battlefield, sacLands(reg, name)...)
		f.Active = s
		f.Focus = &StartFocus{Seat: seatPtr(s), Card: name, Kind: "ability"}
	default:
		return none
	}
	return ""
}

// filterHasProp reports whether some alternative of a Forge filter names prop
// ("Creature.DamagedBy", "Card.AttachedBy"): a property a start has to cause,
// not one a carrier can simply match.
func filterHasProp(filter, prop string) bool {
	for _, alt := range strings.Split(filter, ",") {
		head, props, _ := strings.Cut(strings.TrimSpace(alt), ".")
		if head == prop {
			return true
		}
		for _, p := range strings.Split(props, "+") {
			if p == prop {
				return true
			}
		}
	}
	return false
}

// damageStart is "whenever a creature dealt damage by CARDNAME this turn dies"
// (Creature.DamagedBy): the card attacks and a creature it can kill blocks it.
// Both are on the battlefield since the turn began, so the card may attack
// (CR 302.6); combat damage marks the blocker and the card's own trigger reads
// that the blocker died from its damage.
func damageStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, f *StartFile) string {
	const none = "no-generator:trigger:ChangesZone"
	if !face.IsCreature() || deadOnArrival(face) {
		return none + ":damaged-noncreature"
	}
	victim := pickCarrier(reg, b, card, func(c *cards.Face) bool {
		return c.IsCreature() && FieldBlock(c) == "" && !deadOnArrival(c) && c.Toughness() <= face.Power()
	})
	if victim == "" {
		return none + ":damaged-victim"
	}
	f.Setup.P0.Battlefield = []string{card}
	f.Setup.P1.Battlefield = append(f.Setup.P1.Battlefield, victim)
	f.Active = 0
	f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Attack: card, Block: victim}
	return ""
}

// attachedByStart is "whenever the creature this is attached to dies"
// (Card.AttachedBy):
//
//   - an Aura is placed attached (StartAttach) to the bearer its Enchant
//     keyword takes, and the opponent's Murder destroys the bearer;
//   - an Equipment cannot enter attached from a start (gorge's StartAttach is
//     the non-cast Aura entry), so a Magnetic Theft cast attaches it to a
//     creature that sacrifices itself at the beginning of the end step
//     (endStepSacrificeBearer), which then dies while equipped.
func attachedByStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, f *StartFile) string {
	const none = "no-generator:trigger:ChangesZone"
	p0, p1 := &f.Setup.P0, &f.Setup.P1
	switch {
	case FieldBlock(face) == "aura-unattached":
		bearer, bs := attachBearer(reg, b, card, face)
		if bearer == "" {
			return none + ":filter"
		}
		p0.Battlefield = []string{card}
		placeBearer(p0, p1, card, bearer, bs)
		if !murderFocus(reg, b, p1, f, 1) {
			return none + ":filter"
		}
		// The bearer is the key object's attached creature: name it so Murder
		// cannot be aimed at another creature a need places.
		f.Focus.Targets = []string{bearer}
	case face.HasTypeWord("Equipment"):
		bearer := endStepSacrificeBearer(reg, b, card)
		if bearer == "" {
			return none + ":attached-equipment"
		}
		p0.Battlefield = []string{card, bearer}
		if !focusCast(reg, b, p0, "Magnetic Theft") {
			return none + ":attached-equipment"
		}
		f.Active = 0
		// Magnetic Theft asks for the Equipment, then the creature: name the
		// key Equipment and the bearer it must attach to, not a need's body.
		f.Focus = &StartFocus{Seat: seatPtr(0), Card: "Magnetic Theft", Kind: "cast", Targets: []string{card, bearer}}
	default:
		return none + ":attached"
	}
	return ""
}

// placeBearer puts the Aura card's bearer on seat bs (the one its Enchant
// keyword names) and attaches the Aura, which p0 controls, to it.
func placeBearer(p0, p1 *StartSeat, card, bearer string, bs int) {
	at := StartAttach{Card: card, To: bearer, Opp: bs == 1}
	if bs == 1 {
		p1.Battlefield = append(p1.Battlefield, bearer)
	} else {
		p0.Battlefield = append(p0.Battlefield, bearer)
	}
	p0.Attach = append(p0.Attach, at)
}

// attachBearer is the most inert creature the Aura's Enchant keyword takes and
// the seat that must control it, or "" when the keyword names no permanent a
// carrier can be (a player).
func attachBearer(reg *cards.Registry, b *certdeck.Builder, card string, face *cards.Face) (string, int) {
	req, ok := enchantReq(face)
	if !ok {
		return "", 0
	}
	return filterCarrier(reg, b, card, strings.Join(req.raw, ","), func(c *cards.Face) bool {
		return c.IsPermanent() && FieldBlock(c) == "" && !deadOnArrival(c)
	})
}

// murderFocus is the opponent's Murder destroying the card or its bearer: the
// hand and lands are placed on seat, which takes the cast as its focus.
func murderFocus(reg *cards.Registry, b *certdeck.Builder, seat *StartSeat, f *StartFile, s int) bool {
	if !focusCast(reg, b, seat, "Murder") {
		return false
	}
	f.Active = s
	f.Focus = &StartFocus{Seat: seatPtr(s), Card: "Murder", Kind: "cast"}
	return true
}

// focusCast puts name in seat's hand with exactly the basics its cost takes,
// after checking the corpus holds it. It does not ask certdeck.Playable: a
// forced focus (Magnetic Theft) may carry Forge's AI:RemoveDeck hint, which
// only says a deck-building AI would not pick it, not that xproof cannot cast
// it as the focus.
func focusCast(reg *cards.Registry, _ *certdeck.Builder, seat *StartSeat, name string) bool {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return false
	}
	castCarrier(reg, seat, name)
	return true
}

// endStepSacrifices holds each registry's ranked creatures that sacrifice
// themselves at the beginning of the end step, built once.
var endStepSacrifices sync.Map // *cards.Registry -> []string

// endStepSacrificeBearer is the first creature that sacrifices itself at the
// beginning of the end step (Ball Lightning), or "": the body an Equipment can
// be attached to with Magnetic Theft and then lose, which fires its
// AttachedBy trigger while the Equipment is still attached.
func endStepSacrificeBearer(reg *cards.Registry, b *certdeck.Builder, card string) string {
	if v, ok := endStepSacrifices.Load(reg); ok {
		for _, n := range v.([]string) {
			if n != card {
				return n
			}
		}
		return ""
	}
	var out []string
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 || !b.Playable(c) {
			continue
		}
		f := c.Faces[0]
		if !f.IsCreature() || deadOnArrival(f) {
			continue
		}
		for _, t := range f.Triggers {
			if !strings.EqualFold(t.Mode, "Phase") || !strings.Contains(strings.ToLower(t.Params["Phase"]), "end") {
				continue
			}
			if t.Effect == nil || t.Effect.API != "Sacrifice" || !strings.Contains(t.Effect.Params["SacValid"], "Self") {
				continue
			}
			out = append(out, f.Name)
			break
		}
	}
	sort.Strings(out)
	endStepSacrifices.Store(reg, out)
	for _, n := range out {
		if n != card {
			return n
		}
	}
	return ""
}

// preferredSacCarriers are tried first by sacCarrier: {W} and {G} creatures
// whose one ability is "Sacrifice this: prevent all combat damage this
// turn", free to activate and with no target or choice.
var preferredSacCarriers = []string{"Kami of False Hope", "Spore Frog"}

// sacCarriers holds each registry's ranked sacrifice carriers, built once.
var sacCarriers sync.Map // *cards.Registry -> []*cards.Face

// sacPool is every permanent whose one ability sacrifices it (Sac<1/CARDNAME>,
// with mana and {T} at most) for an effect with no target or choice: prevent
// combat damage, gain or draw a fixed amount. Ranked: the preferred ones,
// then by the ability's mana, then name.
func sacPool(reg *cards.Registry, b *certdeck.Builder) []*cards.Face {
	if v, ok := sacCarriers.Load(reg); ok {
		return v.([]*cards.Face)
	}
	type ranked struct {
		f    *cards.Face
		mana int
	}
	var rs []ranked
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 || !b.Playable(c) {
			continue
		}
		f := c.Faces[0]
		if !f.IsPermanent() || f.IsLand() || len(f.Abilities) != 1 || len(f.Triggers)+len(f.Statics)+len(f.Repls) > 0 || !evergreenOnly(f) {
			continue
		}
		sa := f.Abilities[0]
		if sa.Sub != nil || sa.Params["ValidTgts"] != "" || sa.Params["Defined"] != "" && sa.Params["Defined"] != "You" {
			continue
		}
		switch sa.API {
		case "Fog":
		case "GainLife", "Draw":
			n := sa.Params["LifeAmount"] + sa.Params["NumCards"]
			if _, err := strconv.Atoi(n); n != "" && err != nil {
				continue
			}
		default:
			continue
		}
		sac, mana, ok := false, 0, true
		for _, tok := range splitCost(sa.Params["Cost"]) {
			switch {
			case tok == "Sac<1/CARDNAME>":
				sac = true
			case tok == "T":
				if f.IsCreature() {
					ok = false // a creature's {T} would wait out summoning sickness on a cast carrier
				}
			case manaTokenRE.MatchString(tok) && tok != "X" && tok != "S" && tok != "C":
				mana += len(landsFor([]string{tok}, "Wastes"))
			default:
				ok = false
			}
		}
		if sac && ok {
			rs = append(rs, ranked{f, mana})
		}
	}
	pref := func(f *cards.Face) int {
		if i := slices.Index(preferredSacCarriers, f.Name); i >= 0 {
			return i
		}
		return len(preferredSacCarriers)
	}
	sort.SliceStable(rs, func(i, j int) bool {
		a, c := rs[i], rs[j]
		if pa, pc := pref(a.f), pref(c.f); pa != pc {
			return pa < pc
		}
		if a.mana != c.mana {
			return a.mana < c.mana
		}
		return a.f.Name < c.f.Name
	})
	out := make([]*cards.Face, len(rs))
	for i, r := range rs {
		out[i] = r.f
	}
	sacCarriers.Store(reg, out)
	return out
}

// sacCarrier is the first sacrifice carrier (sacPool) the filter takes, a
// carrier for "dies" or "is put into a graveyard from the battlefield", and
// the seat the filter's controller names.
func sacCarrier(reg *cards.Registry, b *certdeck.Builder, card, filter string) (string, int) {
	for _, a := range parseFilter(filter) {
		if !a.known || a.self {
			continue
		}
		for _, f := range sacPool(reg, b) {
			if f.Name == card || !a.matches(f, false) {
				continue
			}
			if a.ctrl == ctrlOpp {
				return f.Name, 1
			}
			return f.Name, 0
		}
	}
	return "", 0
}

// sacLands is the basics a sacrifice carrier's ability takes.
func sacLands(reg *cards.Registry, name string) []string {
	c, _ := reg.Lookup(name)
	f := c.Faces[0]
	mana, _ := costTokens(f.Abilities[0].Params["Cost"])
	return landsFor(mana, basicFor(f.ManaCost))
}

// spellStart is a SpellCast (or SpellCastOrCopy) trigger. "When you cast
// this spell" (Card.Self, TriggerZones Stack or none) is the card cast from
// hand. Otherwise the card is on the battlefield and the seat that
// ValidActivatingPlayer names casts a carrier (carrierPool) that ValidCard
// takes, with exactly its mana: on its own turn, or, for a NonActive caster,
// an instant on the other seat's turn.
func spellStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, generic string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:" + trig.Mode
	p0 := &f.Setup.P0
	if selfCard(trig) {
		return castStart(reg, b, face, card, generic, f)
	}
	if tz := trig.Params["TriggerZones"]; tz != "" && tz != "Battlefield" {
		return none
	}
	caster, offTurn := 0, false
	vap := trig.Params["ValidActivatingPlayer"]
	who, prop, _ := strings.Cut(vap, ".")
	switch who {
	case "", "You", "Player":
	case "Opponent":
		caster = 1
	default:
		return none + ":caster"
	}
	switch prop {
	case "":
	case "NonActive":
		offTurn = true
	case "Opponent":
		caster = 1
	default:
		return none + ":caster"
	}
	if trig.Params["OpponentTurn"] == "True" {
		offTurn = true
	}
	// "your second spell each turn" (ActivatorThisTurnCast EQ2, GT1, GE2):
	// that many casts of the carrier.
	times := 1
	if n := trig.Params["ActivatorThisTurnCast"]; n != "" {
		_, cmp, ok := compare("cmc" + n)
		if !ok {
			return none + ":history"
		}
		for times = 1; times <= 3 && !cmp(times); times++ {
		}
		if times > 3 {
			return none + ":history"
		}
	}
	keep := func(cf *cards.Face) bool { return !offTurn || cf.IsInstant() }
	if times > 1 {
		// every cast takes its own lands: a free carrier keeps them few
		keep = func(cf *cards.Face) bool { return (!offTurn || cf.IsInstant()) && cf.Cmc() == 0 }
	}
	name, _ := filterCarrier(reg, b, card, trig.Params["ValidCard"], keep)
	if name == "" {
		return none + ":filter"
	}
	p0.Battlefield = []string{card}
	seats := [2]*StartSeat{p0, &f.Setup.P1}
	for range times {
		castCarrier(reg, seats[caster], name)
	}
	f.Active = caster
	if offTurn {
		f.Active = 1 - caster
	}
	f.Focus = &StartFocus{Seat: seatPtr(caster), Card: name, Kind: "cast"}
	if times > 1 {
		f.Focus.Times = times
	}
	return ""
}

// attackCarrier is the creature that attacks for the card: the card itself
// when it is a creature the filter takes (an empty filter takes any), else
// a carrier creature (carrierPool) the filter takes, placed on seat 0's
// battlefield beside it.
func attackCarrier(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, filter string, f *StartFile) string {
	if face.IsCreature() {
		if filter == "" {
			return card
		}
		for _, a := range parseFilter(filter) {
			if a.known && !a.other && a.ctrl != ctrlOpp && a.matches(face, false) {
				return card
			}
		}
	}
	if filter == "" {
		filter = "Creature.YouCtrl"
	}
	name, s := filterCarrier(reg, b, card, filter, (*cards.Face).IsCreature)
	if name == "" || s != 0 {
		return ""
	}
	f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, name)
	return name
}

// combatStart is a trigger an attack fires: "whenever this deals combat
// damage to a player" (DamageDone, CombatDamage) and "whenever you attack"
// (AttackersDeclared). The card is on the battlefield since the turn began
// (no summoning sickness) and the attacker (attackCarrier) attacks a seat
// with no creatures, so the damage is dealt.
func combatStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:" + trig.Mode
	if tz := trig.Params["TriggerZones"]; tz != "" && tz != "Battlefield" {
		return none
	}
	var filter string
	switch trig.Mode {
	case "DamageDone":
		vt := trig.Params["ValidTarget"]
		if trig.Params["CombatDamage"] != "True" || vt != "Player" && vt != "Opponent" {
			return none
		}
		filter = trig.Params["ValidSource"]
		if selfCard(trig) || filter == "Card.Self" {
			if !face.IsCreature() {
				return none + ":noncreature"
			}
			filter = ""
		}
	case "AttackersDeclared":
		if ap := trig.Params["AttackingPlayer"]; ap != "" && ap != "You" {
			return none
		}
		if trig.Params["ValidAttackersAmount"] != "" {
			return none + ":amount"
		}
		filter = trig.Params["ValidAttackers"]
	}
	f.Setup.P0.Battlefield = []string{card}
	attacker := attackCarrier(reg, b, face, card, filter, f)
	if attacker == "" {
		return none + ":filter"
	}
	f.Focus = &StartFocus{Seat: seatPtr(0), Card: attacker, Kind: "attack"}
	return ""
}

// Targeting carriers: the opponent's spell that targets the card is the first
// of eventFixtures.Targeters that takes the target's type (Murder for a
// creature, Disenchant for an artifact or enchantment, ...), and the first of
// ownTargeters the corpus plays is its controller's: "target creature you
// control", so a creature a need placed for the opponent is never the target.
var ownTargeters = []string{"Ranger's Guile", "Blossoming Defense"}

// sourceCaster is the seat that casts the spell a "becomes the target of"
// trigger's ValidSource names, or -1 when the source is not a spell a start
// can cast (an ability, an Aura spell). An unqualified source, a Ward
// trigger and an OppCtrl source are the opponent's; a YouCtrl one the
// card's controller's. accepts reports whether the targeter spell is a
// source the filter takes ("Instant,Sorcery").
func sourceCaster(trig *cards.Trigger) (caster int, accepts func(*cards.Face) bool) {
	src := trig.Params["ValidSource"]
	if src == "" || trig.Params["Ward"] == "True" {
		return 1, func(*cards.Face) bool { return true }
	}
	caster = -1
	var tests []func(*cards.Face) bool
	for _, raw := range strings.Split(src, ",") {
		raw = strings.TrimSpace(raw)
		head, props, _ := strings.Cut(raw, ".")
		switch head {
		case "SpellAbility", "Spell":
			raw = "Card"
			if props != "" {
				raw += "." + props
			}
		case "Card", "Instant", "Sorcery", "Creature", "Permanent":
		default:
			continue // "Ability.Backup" and the like: not a spell
		}
		for _, a := range parseFilter(raw) {
			if !a.known || strings.Contains(props, "Aura") {
				continue
			}
			c := 1
			if a.ctrl == ctrlYou {
				c = 0
			}
			if caster >= 0 && caster != c {
				continue
			}
			caster = c
			a := a
			tests = append(tests, func(f *cards.Face) bool { return a.test(f) })
		}
	}
	if caster < 0 {
		return -1, nil
	}
	return caster, func(f *cards.Face) bool {
		for _, t := range tests {
			if t(f) {
				return true
			}
		}
		return false
	}
}

// targetStart is "whenever <a permanent> becomes the target of a spell or
// ability" (BecomesTarget): the target is the card itself (Card.Self), or the
// permanent the ValidTarget filter takes (the card when it takes the card, else
// an inert carrier on the seat the filter names), on the battlefield. The
// spell that targets it (sourceCaster, eventFixtures.Targeters) is cast by
// the opponent on its turn, or by the card's controller (Ranger's Guile:
// target creature you control). A permanent that could be another spell's
// target (a creature beside the card that the filter does not take) is
// refused when the target is another permanent. When the target is the card
// itself, a permanent that placeNeeds adds beside it later can also be a legal
// target and is not checked here (the start-not-exercised lane reads it).
func targetStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:BecomesTarget"
	if why := eventParamsOK(trig, "BecomesTarget", "ValidTarget", "ValidSource", "Ward", "Keyword", "KeywordLine", "FirstTime", "Valiant"); why != "" {
		return why
	}
	caster, accepts := sourceCaster(trig)
	if caster < 0 {
		return none + ":source"
	}
	// the target: the card when the filter takes it, else a carrier
	filter := inZone.Replace(trig.Params["ValidTarget"])
	target, tseat, targetFace := "", 0, face
	for _, a := range parseFilter(filter) {
		if a.known && (a.self || a.test(face) && a.ctrl != ctrlOpp && !a.other) {
			target = card
			break
		}
	}
	if target == "" {
		name, s := filterCarrier(reg, b, card, filter, (*cards.Face).IsPermanent)
		if name == "" {
			return none + ":target"
		}
		c, _ := reg.Lookup(name)
		target, tseat, targetFace = name, s, c.Faces[0]
	}
	if target == card && deadOnArrival(face) {
		return none + ":dead" // an X creature placed without its X dies at once
	}
	// the spell: the first the target is a legal target of and the source
	// filter takes; never one the card could also be the target of, when the
	// target is another permanent
	spells := slices.Clone(eventFixtures.Targeters)
	if caster == 0 {
		spells = append(slices.Clone(ownTargeterSpells), spells...)
	}
	spellName, ambiguous := "", false
	for _, t := range spells {
		if t.own && tseat != caster || !t.takes(targetFace) || !spell(reg, b, t.name) {
			continue
		}
		if c, _ := reg.Lookup(t.name); c == nil || !accepts(c.Faces[0]) {
			continue
		}
		if target != card && t.takes(face) && !(t.own && tseat != 0) {
			ambiguous = true
			continue
		}
		spellName = t.name
		break
	}
	if spellName == "" {
		if ambiguous {
			return none + ":filter"
		}
		return none + ":source"
	}
	seats := seatsOf(f)
	f.Setup.P0.Battlefield = []string{card}
	if target != card {
		seats[tseat].Battlefield = append(seats[tseat].Battlefield, target)
	}
	castCarrier(reg, seats[caster], spellName)
	f.Active = caster
	f.Focus = &StartFocus{Seat: seatPtr(caster), Card: spellName, Kind: "cast"}
	return ""
}

// lifeStart is "whenever you gain life" (LifeGained, ValidPlayer You): the
// card on the battlefield and Chaplain's Blessing ({W} sorcery, you gain 5
// life) cast on its controller's turn.
func lifeStart(reg *cards.Registry, b *certdeck.Builder, card string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:LifeGained"
	if vp := trig.Params["ValidPlayer"]; vp != "You" || trig.Params["TriggerZones"] != "Battlefield" || trig.Params["OpponentTurn"] != "" {
		return none
	}
	const spell = "Chaplain's Blessing"
	c, ok := reg.Lookup(spell)
	if !ok || !b.Playable(c) {
		return none
	}
	f.Setup.P0.Battlefield = []string{card}
	castCarrier(reg, &f.Setup.P0, spell)
	f.Focus = &StartFocus{Seat: seatPtr(0), Card: spell, Kind: "cast"}
	return ""
}

// attacksStart is "whenever <a creature> attacks" (Attacks): the card on the
// battlefield since the turn began (no summoning sickness, CR 302.6) and the
// attacker attacks, the focus (focus attack). The attacker is the card when
// the trigger reads the card itself (Card.Self), else the most inert carrier
// creature the ValidCard filter takes (attackCarrier): "whenever a creature
// you control with power 2 or less attacks" is a Memnite attacking beside an
// enchantment that could never attack itself. "Whenever a creature attacks
// you" (Attacked You) is the opponent's carrier attacking on its turn.
func attacksStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:Attacks"
	if tz := trig.Params["TriggerZones"]; tz != "" && tz != "Battlefield" {
		return none
	}
	filter := strings.TrimSpace(trig.Params["ValidCard"])
	f.Setup.P0.Battlefield = []string{card}
	if selfCard(trig) || filter == "Creature.Self" || filter == "" {
		if !face.IsCreature() {
			return none + ":noncreature" // a Vehicle, or an artifact that animates first
		}
		f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Kind: "attack"}
		return ""
	}
	for _, a := range parseFilter(filter) {
		if a.self || !a.known {
			// equipped or enchanted creature, an unread property: the card
			// itself attacks, when it can
			if !face.IsCreature() {
				return none + ":filter"
			}
			f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Kind: "attack"}
			return ""
		}
	}
	switch att := trig.Params["Attacked"]; {
	case att == "":
	case strings.HasPrefix(att, "You"):
		// the opponent attacks the card's controller
		name, s := filterCarrier(reg, b, card, filter, func(c *cards.Face) bool {
			return c.IsCreature() && !c.HasKeyword("Defender") && !deadOnArrival(c)
		})
		if name == "" || s != 0 {
			return none + ":filter"
		}
		f.Setup.P1.Battlefield = append(f.Setup.P1.Battlefield, name)
		f.Active = 1
		f.Focus = &StartFocus{Seat: seatPtr(1), Card: name, Kind: "attack"}
		return ""
	default:
		return none + ":attacked"
	}
	attacker := attackCarrier(reg, b, face, card, filter, f)
	if attacker == "" {
		return none + ":filter"
	}
	f.Focus = &StartFocus{Seat: seatPtr(0), Card: attacker, Kind: "attack"}
	return ""
}

// selfDiesStart is a ChangesZone Card.Self from the battlefield to the
// graveyard (the recipe branch before zoneStart): a creature is Murdered; any
// other permanent is destroyed by the strongest spell that takes it, an Aura
// first placed attached to a bearer (StartAttach).
func selfDiesStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, f *StartFile) string {
	const none = "no-generator:trigger:ChangesZone"
	if !face.IsPermanent() {
		return none + ":noncreature-self"
	}
	p0, p1 := &f.Setup.P0, &f.Setup.P1
	p0.Battlefield = []string{card}
	if face.IsCreature() {
		if !murderFocus(reg, b, p1, f, 1) {
			return none + ":filter"
		}
		// The key card is the creature that must die: name it so no need's
		// enabler is Murdered instead.
		f.Focus.Targets = []string{card}
		return ""
	}
	if FieldBlock(face) == "aura-unattached" {
		bearer, bs := attachBearer(reg, b, card, face)
		if bearer == "" {
			return none + ":noncreature-self"
		}
		placeBearer(p0, p1, card, bearer, bs)
	}
	name := destroySpell(reg, b, face)
	if name == "" || !focusCast(reg, b, p1, name) {
		return none + ":noncreature-self"
	}
	f.Active = 1
	// The key card is the permanent that must die: a destroy spell's other
	// legal targets (the Aura's bearer, an artifact enabler) must not take it.
	f.Focus = &StartFocus{Seat: seatPtr(1), Card: name, Kind: "cast", Targets: []string{card}}
	return ""
}

// destroySpell is the first corpus spell a start can cast that destroys face:
// Naturalize/Disenchant for an artifact or enchantment, Shatter for an
// artifact, Vindicate for any permanent.
func destroySpell(reg *cards.Registry, b *certdeck.Builder, face *cards.Face) string {
	for _, n := range []string{"Naturalize", "Disenchant", "Shatter", "Vindicate"} {
		c, ok := reg.Lookup(n)
		if !ok || len(c.Faces) == 0 || !b.Playable(c) {
			continue
		}
		sp := c.Faces[0]
		for _, a := range sp.Abilities {
			if a.Kind != "SP" || a.API != "Destroy" {
				continue
			}
			for _, alt := range parseFilter(a.Params["ValidTgts"]) {
				if alt.known && alt.matches(face, false) {
					return n
				}
			}
		}
	}
	return ""
}

// zoneAllStart is a ChangesZoneAll trigger on another card, the card on the
// battlefield: the same entering, dying and leaving-a-graveyard causes as
// zoneStart, read from ValidCards. A one-or-more copy of a zone change is the
// same event with a batch filter, so one object moving is enough.
func zoneAllStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, f *StartFile) string {
	const none = "no-generator:trigger:ChangesZoneAll"
	if tz := trig.Params["TriggerZones"]; tz != "" && tz != "Battlefield" || selfCard(trig) {
		return none
	}
	filter := triggerFilter(trig)
	origin, dest := trig.Params["Origin"], trig.Params["Destination"]
	f.Setup.P0.Battlefield = []string{card}
	seats := [2]*StartSeat{&f.Setup.P0, &f.Setup.P1}
	switch {
	case dest == "Battlefield" && (origin == "" || origin == "Any" || origin == "Hand" || origin == "Library"):
		if name, s := tokenCarrier(reg, b, card, filter); name != "" {
			castCarrier(reg, seats[s], name)
			f.Active = s
			f.Focus = &StartFocus{Seat: seatPtr(s), Card: name, Kind: "cast"}
			return ""
		}
		if land, s := landCarrier(reg, filter, basicFor(face.ManaCost)); land != "" {
			seats[s].Hand = append(seats[s].Hand, land)
			f.Active = s
			f.Focus = &StartFocus{Seat: seatPtr(s), Card: land, Kind: "land"}
			return ""
		}
		name, s := filterCarrier(reg, b, card, filter, (*cards.Face).IsPermanent)
		if name == "" {
			return none + ":filter"
		}
		castCarrier(reg, seats[s], name)
		f.Active = s
		f.Focus = &StartFocus{Seat: seatPtr(s), Card: name, Kind: "cast"}
	case origin == "Battlefield" && (dest == "Graveyard" || dest == "Any"):
		name, s := sacCarrier(reg, b, card, filter)
		if name == "" {
			return none + ":filter"
		}
		seats[s].Battlefield = append(seats[s].Battlefield, name)
		seats[s].Battlefield = append(seats[s].Battlefield, sacLands(reg, name)...)
		f.Active = s
		f.Focus = &StartFocus{Seat: seatPtr(s), Card: name, Kind: "ability"}
	case origin == "Graveyard" && (dest == "Any" || dest == "Hand" || dest == "Exile" || dest == "Battlefield"):
		name, s := filterCarrier(reg, b, card, filter, func(c *cards.Face) bool {
			return c.IsPermanent() && (dest != "Battlefield" || c.IsCreature())
		})
		if name == "" {
			return none + ":filter"
		}
		seats[s].Graveyard = append(seats[s].Graveyard, name)
		probe := "Regrowth" // any card in a graveyard to hand: leaves the graveyard
		switch dest {
		case "Exile":
			probe = "Cremate"
		case "Battlefield":
			probe = "Resurrection"
		}
		if !focusCast(reg, b, seats[s], probe) {
			return none + ":graveyard-probe"
		}
		f.Active = s
		f.Focus = &StartFocus{Seat: seatPtr(s), Card: probe, Kind: "cast"}
	default:
		return none
	}
	return ""
}
