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
		p0.Hand = []string{card}
		p0.Battlefield = landsFor(splitCost(face.ManaCost), generic)
		f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Kind: "cast"}
		return ""
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
		if a.known && a.ctrl != ctrlOpp && a.test(face) && f.Focus == nil {
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

// zoneStart is a ChangesZone trigger on another card, the card on the
// battlefield (TriggerZones Battlefield):
//
//   - an enters trigger ("whenever another creature you control enters",
//     landfall) is the carrier entering: a basic land played from hand when
//     the filter takes a land, else a carrier permanent (carrierPool) cast
//     from hand with exactly the basics its cost takes, by the seat the
//     filter names, on that seat's turn;
//   - a dies trigger ("whenever another creature you control dies") is a
//     sacrifice carrier (sacCarrier) on the named seat's battlefield
//     sacrificing itself, on that seat's turn.
func zoneStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, f *StartFile) string {
	const none = "no-generator:trigger:ChangesZone"
	if tz := trig.Params["TriggerZones"]; tz != "" && tz != "Battlefield" || selfCard(trig) {
		return none
	}
	filter := trig.Params["ValidCard"]
	origin, dest := trig.Params["Origin"], trig.Params["Destination"]
	f.Setup.P0.Battlefield = []string{card}
	seats := [2]*StartSeat{&f.Setup.P0, &f.Setup.P1}
	switch {
	case dest == "Battlefield" && (origin == "" || origin == "Any" || origin == "Hand"):
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
			if f.Name == card || !a.test(f) {
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
