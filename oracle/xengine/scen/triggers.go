package scen

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
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
//     from a placed card: the setup drive would fire the card's own phase
//     trigger before the focus step (Span declines an optional one, so it would
//     not fire the key, and a mandatory one refuses the span outright), so the
//     card is cast from hand on turn 1 with exactly its mana, and the game
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
		// The card's own upkeep or draw trigger would fire on the setup drive
		// before the focus step (a mandatory one refuses the span; Span can only
		// decline an optional one). Cast it on turn 1, after that upkeep, so the
		// trigger cannot fire before the step the key names.
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

// selfTransformAbility is the index of face's own activated transform
// ability ("{2}{B}: Transform CARDNAME.", a SetState Mode$ Transform of the
// card itself), or -1. A transform effect that names another permanent
// (Waxing Moon's target, Elvish Vatkeeper's Incubator) is not the card's.
func selfTransformAbility(face *cards.Face) int {
	for i, sa := range face.Abilities {
		if sa == nil || !sa.IsActivated() || sa.API != "SetState" || !strings.EqualFold(sa.Params["Mode"], "Transform") {
			continue
		}
		switch d := strings.TrimSpace(sa.Params["Defined"]); d {
		case "Self", "CARDNAME", "NICKNAME":
			return i
		case "":
			// no Defined$: self unless the effect names a target
			if strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
				return i
			}
		}
	}
	return -1
}

// transformAbilityGated reports an activated transform ability whose
// activation reads a condition or history the start does not model
// (Kindly Stranger's Activation$ Delirium): focusing it would build a start
// whose play is never offered. The caller then tries a one-card enabler.
func transformAbilityGated(sa *cards.SA) bool {
	for k := range sa.Params {
		switch {
		case k == "Activation", k == "UnlessCost", k == "UnlessPayer", k == "IsPresent",
			k == "PresentDefined", k == "PresentCompare", strings.HasPrefix(k, "Condition"),
			strings.HasPrefix(k, "CheckSVar"):
			return true
		}
	}
	return false
}

// transformEnabler is true when it put a one-card transform cause into the
// start: Moonmist for a Human creature ("Transform all Humans"). It is cast
// on the start's line, so the card stands on its front face and the
// enabler's resolution transforms it into the back face whose Transformed
// trigger the key reads. A non-Human front has no enabler this recipe can
// place (Waxing Moon's single target is not forced by a start's cast), so it
// is refused by transformRefusal.
func transformEnabler(reg *cards.Registry, front *cards.Face, card string, f *StartFile) bool {
	if !front.IsCreature() || !front.HasTypeWord("Human") || !cardInCorpus(reg, "Moonmist") {
		return false
	}
	// The enabler is a start-only fixture, not a certdeck enabler: Moonmist is
	// AI-excluded from the builder's playable pool, but a start deck may hold
	// and cast any corpus card.
	const enabler = "Moonmist"
	f.Setup.P0.Battlefield = []string{card}
	castCarrier(reg, &f.Setup.P0, enabler)
	f.Focus = &StartFocus{Seat: seatPtr(0), Card: enabler, Kind: "cast"}
	return true
}

// cardInCorpus reports whether the registry holds name with a face.
func cardInCorpus(reg *cards.Registry, name string) bool {
	c, ok := reg.Lookup(name)
	return ok && len(c.Faces) > 0
}

// transformRefusal names why front has no placeable transform cause, as the
// narrower reason of a Transformed trigger the start cannot serve: a
// day/night border, a conditional phase (upkeep count, four Birds, an
// equipped creature, a may-pay cost), an attack trigger, a spell-count
// transform, or no transform cause at all.
func transformRefusal(front *cards.Face) string {
	const none = "no-generator:trigger:Transformed"
	if front.HasKeyword("Daybound") || front.HasKeyword("Nightbound") {
		return none + ":day-night"
	}
	for _, t := range front.Triggers {
		if t.Effect == nil || t.Effect.API != "SetState" || !strings.EqualFold(t.Effect.Params["Mode"], "Transform") {
			continue
		}
		switch t.Mode {
		case "Phase":
			return none + ":condition"
		case "Attacks", "AttackersDeclared":
			return none + ":attack"
		case "SpellCast", "SpellCastOrCopy":
			return none + ":spellcast"
		default:
			return none + ":trigger"
		}
	}
	return none + ":no-cause"
}

// transformedStart is a "when this creature transforms into CARDNAME"
// trigger (Mode Transformed) on a back face. A BackFace placement is an
// enter, not a transform (CR 701.27), so the trigger never fires from it:
// the card stands on its front face and the start's own line transforms it,
// either with the front face's activated transform ability or with a
// one-card enabler (transformEnabler). A card with no placeable cause is
// refused by name (transformRefusal).
func transformedStart(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, face *cards.Face, card, generic string, trig *cards.Trigger, f *StartFile) string {
	const none = "no-generator:trigger:Transformed"
	if !selfCard(trig) {
		return none + ":not-self" // a watcher's "whenever a permanent you control transforms" needs another transform, not this card's
	}
	if why := eventParamsOK(trig, "Transformed", "ValidCard"); why != "" {
		return why
	}
	if c.AlternateMode != "DoubleFaced" {
		// a modal or other two-faced card can be entered on a face but never
		// transformed into one
		return none + ":not-transforming"
	}
	fi := -1
	for i, cf := range c.Faces {
		if cf == face {
			fi = i
			break
		}
	}
	if fi != 1 {
		// transforming INTO the front face would need the back face placed
		// and transformed back: not a back-face start
		return none + ":front"
	}
	front := c.Faces[0]
	if why := FieldBlock(front); why != "" {
		return "battlefield:" + why
	}
	// The front face is placed and transformed at genesis. An as-enters
	// choice there is answered after setup, so the live cut carries the
	// entry's turn stamps; until the lowering can restate them the start
	// keeps the specific refusal instead of emitting a start it refuses.
	if why := asEntersPlacedRefusal(front); why != "" {
		return why
	}
	// A daybound or nightbound permanent cannot be transformed by another
	// effect or ability (CR 702.145b), and a battlefield permanent with an
	// upkeep or draw trigger refuses the lowering's cut (lower.phaseTriggered),
	// so neither can be served by a start that places the card and transforms
	// it. Refuse by the narrower cause before trying an enabler.
	if front.HasKeyword("Daybound") || front.HasKeyword("Nightbound") {
		return none + ":day-night"
	}
	if upkeepOrDraw(front) {
		return none + ":condition"
	}
	abilityWhy := ""
	if i := selfTransformAbility(front); i >= 0 && !transformAbilityGated(front.Abilities[i]) {
		if why := abilityFocus(reg, b, front, card, generic, i, front.Abilities[i], f); why == "" {
			return ""
		} else {
			abilityWhy = why
		}
	}
	if transformEnabler(reg, front, card, f) {
		return ""
	}
	if abilityWhy != "" {
		return abilityWhy
	}
	return transformRefusal(front)
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

// upkeepOrDraw reports a beginning-of-upkeep or draw-step trigger on face. It
// is lower.PhaseTriggeredFace, the lowering gate's own predicate, so the
// generator cannot place a permanent the gate would refuse for a reason the
// two disagree about.
func upkeepOrDraw(face *cards.Face) bool { return lower.PhaseTriggeredFace(face) }

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
	if filterHasProp(filter, "AttachedBy") || filterHasProp(filter, "EnchantedBy") || filterHasProp(filter, "EquippedBy") {
		// The pinned gorge reads EquippedBy, EnchantedBy and AttachedBy as one
		// predicate (effects/compiled_predicate.go: all three spellings
		// compile to the same term, "the permanent src is attached to"), so
		// the bearer-death start built for AttachedBy is the same start: an
		// Aura placed attached (StartAttach) to its Enchant bearer which the
		// opponent murders, an Equipment attached with Magnetic Theft to an
		// end-step self-sacrifice. A card that is neither an Aura (for
		// EnchantedBy) nor an Equipment (for EquippedBy) keeps the ":attached"
		// refusal: nothing can be attached to its bearer by a start.
		return attachedByStart(reg, b, face, card, filter, f)
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
		var targets []string
		if name == "" {
			// The inert pool has no permanent the filter takes: the enters
			// lane falls back to the shapes the static lane already serves
			// (entersFallback), each a cast, so the entering object is a
			// cast and the trigger reads the same event.
			name, s, targets = entersFallback(reg, b, card, filter)
			if name == "" {
				return none + ":filter"
			}
		}
		castCarrier(reg, seats[s], name)
		if len(targets) > 0 {
			// the entered Aura's bearer, placed beside its caster
			seats[s].Battlefield = append(seats[s].Battlefield, targets...)
		}
		// An IsCommander filter names a card the seat must designate: the
		// engine reads Card.IsCommander through Game.CommanderOf list
		// membership (CR 903.3's designation is an attribute of the card, not
		// a printed characteristic), so an undesignated entering carrier would
		// never fire the trigger.
		designateCommander(reg, seats[s], name, filter)
		f.Active = s
		f.Focus = &StartFocus{Seat: seatPtr(s), Card: name, Kind: "cast", Targets: targets}
	case origin == "Battlefield" && (dest == "Graveyard" || dest == "Any") || origin == "Any" && dest == "Graveyard":
		// "Put into a graveyard from anywhere" (Origin Any) is met by a
		// permanent dying, the same start as a dies trigger.
		name, s := sacCarrier(reg, b, card, filter)
		if name == "" {
			// No sacrifice carrier is the type the filter names (a Goblin, a
			// Zombie, a creature with a counter): place a vanilla one and
			// let the opponent's Murder kill it.
			if !removalStart(reg, b, card, filter, "Murder", f) {
				return none + ":filter"
			}
			return ""
		}
		seats[s].Battlefield = append(seats[s].Battlefield, name)
		seats[s].Battlefield = append(seats[s].Battlefield, sacLands(reg, name)...)
		f.Active = s
		f.Focus = &StartFocus{Seat: seatPtr(s), Card: name, Kind: "ability"}
	case origin == "Battlefield" && leavesToHand(dest):
		// "Leaves the battlefield to a hand, exile or library" with no
		// graveyard: the opponent's Unsummon returns the carrier.
		if !removalStart(reg, b, card, filter, "Unsummon", f) {
			return none + ":filter"
		}
	default:
		return none
	}
	return ""
}

// leavesToHand reports a Destination list ("Ante,Command,Exile,Hand,Library",
// "Hand") a creature returned to its owner's hand meets.
func leavesToHand(dest string) bool {
	return slices.Contains(strings.Split(dest, ","), "Hand")
}

// removalStart is a zone-change trigger on a creature the filter takes, other
// than the key card: the carrier is placed on the battlefield of the seat the
// filter names (seat 0 when it says none), with the counters the filter
// requires, and the other seat casts the removal spell (Murder, Unsummon) at
// it as its only play. The carrier is the most inert creature that matches
// (carrierPool), so the one event the start offers is the removal. It reports
// false, leaving f untouched, when no creature the filter takes is in the
// pool or the spell is not in the corpus.
func removalStart(reg *cards.Registry, b *certdeck.Builder, card, filter, removal string, f *StartFile) bool {
	seats := [2]*StartSeat{&f.Setup.P0, &f.Setup.P1}
	for _, a := range parseFilterCounters(filter, true, false, false, false) {
		if !a.known || a.self {
			continue
		}
		name := pickCarrier(reg, b, card, func(c *cards.Face) bool {
			return c.IsCreature() && FieldBlock(c) == "" && a.matches(c, false) && survivesCounters(c, a.counters)
		})
		sp := removal
		if name == "" {
			// The filter takes a land (a "whenever a land you control dies"
			// watcher): Murder cannot destroy one, so the destroy spell
			// becomes Stone Rain, which destroys target land; Unsummon
			// returns any permanent and needs no swap. The inert pool holds
			// no lands (inertCarrier), so the land comes from the basics and
			// the least busy playable nonbasic.
			land := removalLand(reg, b, card, a)
			if land == "" {
				continue
			}
			if sp == "Murder" {
				sp = "Stone Rain"
			}
			if !spell(reg, b, sp) {
				continue
			}
			name = land
		}
		cs := 0
		if a.ctrl == ctrlOpp {
			cs = 1
		}
		caster := 1 - cs
		if !focusCast(reg, b, seats[caster], sp) {
			return false
		}
		seats[cs].Battlefield = append(seats[cs].Battlefield, name)
		for _, g := range a.counters {
			seats[cs].Counters = append(seats[cs].Counters, StartCounter{Card: name, Kind: g.placedKind(), N: g.n})
		}
		f.Active = caster
		f.Focus = &StartFocus{Seat: seatPtr(caster), Card: sp, Kind: "cast", Targets: []string{name}}
		return true
	}
	return false
}

// removalLand is a land the (counter-parsed) alternative a takes: a basic
// first, then the least busy playable nonbasic, never the key card. "" when
// no land matches (the alternative names a creature kind, a state or an
// understood-but-landless shape).
func removalLand(reg *cards.Registry, b *certdeck.Builder, card string, a filterAlt) string {
	for _, basic := range []string{"Plains", "Island", "Swamp", "Mountain", "Forest", "Wastes"} {
		if basic == card {
			continue
		}
		c, ok := reg.Lookup(basic)
		if ok && len(c.Faces) > 0 && a.matches(c.Faces[0], false) {
			return basic
		}
	}
	return pickLandFixture(reg, b, func(f *cards.Face) bool { return f.Name != card && a.matches(f, false) })
}

// castSatisfiable are the filter properties the entering object of a cast
// makes true at the trigger event itself: the enters lane casts the carrier
// from its controller's hand on that controller's turn, so "wasCastByYou"
// holds of the permanent entering. "kicked" is deliberately absent: a plain
// cast is not a kicked one, and the pinned gorge cannot cast a kicked spell
// at all (effects.Supported() lacks the Kicker primitive, so no kicked card
// is certdeck-Playable).
var castSatisfiable = map[string]bool{"wasCastByYou": true, "wasCast": true}

// strippedFilter drops the castSatisfiable properties from every alternative
// of an enters filter: ok reports that at least one was dropped and every
// alternative is one parseFilter understands without them. The carrier the
// stripped filter picks is cast, which makes the dropped properties true of
// the entering object; any other unknown property keeps the filter refused.
func strippedFilter(filter string) (string, bool) {
	if !strings.Contains(filter, "wasCast") {
		return "", false
	}
	var alts []string
	dropped := false
	for _, raw := range strings.Split(filter, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		head, props, has := strings.Cut(raw, ".")
		if !has {
			alts = append(alts, raw)
			continue
		}
		var keep []string
		for _, p := range strings.Split(props, "+") {
			if castSatisfiable[p] {
				dropped = true
				continue
			}
			keep = append(keep, p)
		}
		if len(keep) > 0 {
			alts = append(alts, head+"."+strings.Join(keep, "+"))
		} else {
			alts = append(alts, head)
		}
	}
	if !dropped {
		return "", false
	}
	rest := strings.Join(alts, ",")
	for _, a := range parseFilter(rest) {
		if !a.known {
			return "", false
		}
	}
	return rest, true
}

// entersFallback is the entering permanent when the inert carrier pool has
// none the filter takes. Three shapes, each still a cast of the entering
// object, so the trigger reads the same event the start offers:
//
//   - a cast-history property ("whenever a creature you cast this turn
//     enters": wasCastByYou) is met by the cast itself, so the stripped
//     filter picks the carrier (strippedFilter);
//   - the simplest playable permanent the filter takes, the static lane's
//     fixture scan (castFixtureCarrierIn) for a class no vanilla card holds
//     (a Detective);
//   - an Aura the filter takes (a Cartouche) cast attached: a bare Aura is
//     FieldBlock-refused on the battlefield, so the start casts it at a
//     vanilla bearer its Enchant keyword takes and names the bearer.
//
// "" keeps the ":filter" refusal: an alternative no arm can meet (a face-down
// permanent, a counter gate or a token state the cast cannot establish) is not
// answered. A setup counter cannot sit on a card still in hand, and a token is
// not a card, so those alternatives are castableAlt-rejected rather than
// matched by their test alone.
func entersFallback(reg *cards.Registry, b *certdeck.Builder, card, filter string) (string, int, []string) {
	filters := []string{filter}
	if stripped, ok := strippedFilter(filter); ok {
		filters = []string{stripped, filter}
	}
	for _, flt := range filters {
		// commanderOK: the enters arm designates an IsCommander fixture it
		// casts (the inert pool can never hold one), and the designation is
		// what makes the entering object the engine reads as a commander.
		if n, s := castFixtureCarrierIn(reg, b, card, flt, (*cards.Face).IsPermanent, true, true); n != "" {
			return n, s, nil
		}
	}
	return entersAuraCarrier(reg, b, card, filter)
}

// entersAuraCarrier is the Aura a "whenever an <Aura type> you control
// enters" trigger takes, the vanilla bearer it enters attached to, and the
// seat casting it: the simplest playable Aura the filter takes (a Cartouche),
// cast at the most inert permanent its Enchant keyword takes. The bearer is
// named in the cast's Targets so the bot aims the Aura at it. "" when no
// alternative takes a plain Aura or its Enchant names no placeable permanent.
func entersAuraCarrier(reg *cards.Registry, b *certdeck.Builder, card, filter string) (string, int, []string) {
	for _, a := range parseFilterStatic(filter) {
		if !a.known || a.self || !castableAlt(a) {
			continue
		}
		aura := pickZoneFixture(reg, b, card, func(f *cards.Face) bool {
			return f.HasTypeWord("Aura") && !f.IsCreature() && FieldBlock(f) == "aura-unattached" && a.matches(f, false)
		}, nil, true)
		if aura == "" {
			continue
		}
		c, _ := reg.Lookup(aura)
		bearer, bs := attachBearer(reg, b, aura, c.Faces[0])
		s := 0
		if a.ctrl == ctrlOpp {
			s = 1
		}
		if bearer == "" || bs != s {
			continue
		}
		return aura, s, []string{bearer}
	}
	return "", 0, nil
}

// survivesCounters reports that a creature carrying gates is still alive at
// the start: -1/-1 counters (M1M1) must leave a toughness.
func survivesCounters(c *cards.Face, gates []counterGate) bool {
	for _, g := range gates {
		if g.kind == "M1M1" && c.Toughness() <= int(g.n) {
			return false
		}
	}
	return true
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

// designateCommander records name as a commander of seat when the trigger's
// filter names the IsCommander property: the engine resolves Card.IsCommander
// through Game.CommanderOf list membership, not a printed keyword (CR 903.3
// says the designation is an attribute of the card itself), so a start whose
// carrier was picked for the IsCommander face test must also designate it or
// the trigger never reads it and the focus is never exercised. Only a face
// CR 903.3 could legally designate (a legendary creature) is recorded: a
// carrier that matched another alternative of a multi-alternative filter
// stays undesignated, and a start the trigger never fires is refused by the
// executor as not-exercised rather than claiming an illegal commander.
func designateCommander(reg *cards.Registry, seat *StartSeat, name, filter string) {
	if name == "" || !filterHasProp(filter, "IsCommander") {
		return
	}
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return
	}
	if f := c.Faces[0]; f.IsLegendary() && f.IsCreature() {
		seat.Commanders = append(seat.Commanders, name)
	}
}

// damageStart is "whenever a creature dealt damage by CARDNAME this turn dies"
// (Creature.DamagedBy): the card attacks and a creature it can kill blocks it.
// Both are on the battlefield since the turn began, so the card may attack
// (CR 302.6); combat damage marks the blocker and the card's own trigger reads
// that the blocker died from its damage.
func damageStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, f *StartFile) string {
	const none = "no-generator:trigger:ChangesZone"
	// The card's own CDA is not a bare-placement death here: it is the key's
	// card, and recipe's post-needs check (cdaToughnessZero) judges it over
	// the finished start, so only a printed 0 toughness is one.
	if !face.IsCreature() || numericToughnessDead(face) {
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
//     keyword takes and the trigger filter also takes (the attachment property
//     removed: "Card.EnchantedBy+Horror" needs a Horror), and the opponent's
//     Murder destroys the bearer;
//   - an Equipment cannot enter attached from a start (gorge's StartAttach is
//     the non-cast Aura entry), so a Magnetic Theft cast attaches it to a
//     creature that sacrifices itself at the beginning of the end step
//     (endStepSacrificeBearer) and that the trigger filter also takes, which
//     then dies while equipped. An Equipment that is itself a creature (an
//     Artifact Creature Equipment) is refused instead: Magnetic Theft's second
//     target is then ambiguous and the tape attaches it to itself.
func attachedByStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, filter string, f *StartFile) string {
	const none = "no-generator:trigger:ChangesZone"
	p0, p1 := &f.Setup.P0, &f.Setup.P1
	// The trigger filter with its attachment property removed: the bearer must
	// satisfy the rest of the alternative too, or the trigger could never fire.
	alts := attachFilter(filter)
	switch {
	case FieldBlock(face) == "aura-unattached":
		if upkeepOrDraw(face) {
			// The Aura is placed on the battlefield, so its own upkeep or draw
			// trigger fires before the cut and the lowering refuses the start
			// (Endless Evil). The other lanes cast their fixture and so never
			// place it; the attachment lane cannot.
			return none + ":filter"
		}
		if auraHidesBearer(face) {
			// The Aura makes the bearer untargetable (Curator's Ward's
			// hexproof): the opponent's Murder is never offered, so the bearer
			// cannot die while attached.
			return none + ":filter"
		}
		bearer, bs := attachBearerFor(reg, b, card, face, alts)
		spell := ""
		if bearer == "" {
			// An "Enchant land" Aura (Nissa's Zendikon): no creature is a
			// legal bearer, so the basic its Enchant keyword takes is, and
			// the destroy spell is Stone Rain (Murder takes creatures).
			if land, ls := attachLandBearer(reg, face, alts); land != "" {
				bearer, bs, spell = land, ls, "Stone Rain"
			}
			if bearer == "" {
				return none + ":filter"
			}
		}
		p0.Battlefield = []string{card}
		placeBearer(p0, p1, card, bearer, bs)
		if !murderFocus(reg, b, p1, f, 1, spell) {
			return none + ":filter"
		}
		// The bearer is the key object's attached creature (or land): name it
		// so the removal cannot be aimed at another permanent a need places.
		f.Focus.Targets = []string{bearer}
	case face.HasTypeWord("Equipment"):
		if face.IsCreature() {
			// Magnetic Theft asks for the Equipment, then for a creature; an
			// Equipment that is also a creature answers the second ask with
			// itself, so the intended bearer never dies equipped.
			return none + ":filter"
		}
		bearer := endStepSacrificeBearer(reg, b, card, func(c *cards.Face) bool {
			return bearerMatchesSeat(alts, c, 0)
		})
		if bearer == "" {
			return none + ":filter"
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

// attachFilter parses the trigger filter with its attachment property removed
// from every alternative ("Card.EnchantedBy+Horror" gives "Card.Horror"), so a
// bearer the start picks satisfies the trigger's own gate and not only the
// attachment. An alternative without an attachment property describes another
// event, not this bearer, and is dropped. A property the bearer lane cannot
// establish (a counter gate, a game-history property) leaves the alternative
// unknown, which refuses the start rather than serving a bearer that is not
// the one the trigger reads.
func attachFilter(filter string) []filterAlt {
	var stripped []string
	for _, prop := range []string{"AttachedBy", "EnchantedBy", "EquippedBy"} {
		if s := bearerFilter(filter, prop); s != "" {
			stripped = append(stripped, s)
		}
	}
	return parseFilter(strings.Join(stripped, ","))
}

// ctrlAgree reports whether two alternatives' controller requirements can hold
// of one permanent (ctrlAny, -1, agrees with everything).
func ctrlAgree(a, b int) bool { return a == ctrlAny || b == ctrlAny || a == b }

// ctrlPick is the seat two agreeing controller requirements name.
func ctrlPick(a, b int) int {
	if a != ctrlAny {
		return a
	}
	return b
}

// auraHidesBearer reports a static on the Aura that makes the permanent it is
// attached to untargetable: hexproof, shroud or protection granted to the
// enchanted/equipped permanent. The bearer-death start's removal is the
// opponent's Murder, which such a bearer never offers as a target, so the
// trigger could not fire.
func auraHidesBearer(face *cards.Face) bool {
	for i := range face.Statics {
		st := &face.Statics[i]
		affected := strings.ToLower(st.Params["AffectedDefined"] + " " + st.Params["Affected"])
		if !strings.Contains(affected, "enchanted") && !strings.Contains(affected, "equipped") {
			continue
		}
		for _, kw := range strings.Split(st.Params["AddKeyword"], " & ") {
			kw = strings.TrimSpace(kw)
			if kw == "Hexproof" || kw == "Shroud" || strings.HasPrefix(kw, "Protection") {
				return true
			}
		}
	}
	return false
}

// survivesAura reports that creature c, enchanted or equipped by aura, keeps a
// toughness above zero: an Aura whose own static shrinks its bearer
// (Infectious Rage +2/-1, Screams from Within -1/-1) would otherwise kill a
// small bearer to state-based actions during setup, so the key would be
// exercised by that death and not by the focus play. Pumps whose amount is not
// a number are not read (they cannot be counted here).
func survivesAura(aura, c *cards.Face) bool {
	if !c.IsCreature() {
		return true
	}
	t := c.Toughness()
	add := 0
	for i := range aura.Statics {
		st := &aura.Statics[i]
		affected := strings.ToLower(st.Params["AffectedDefined"] + " " + st.Params["Affected"])
		if !strings.Contains(affected, "enchanted") && !strings.Contains(affected, "equipped") {
			continue
		}
		if v, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(st.Params["SetToughness"]), "+")); err == nil {
			t = v
		}
		if v, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(st.Params["AddToughness"]), "+")); err == nil {
			add += v
		}
	}
	return t+add > 0
}

// bearerMatchesSeat reports whether some alternative of the stripped trigger
// filter takes c when the start places it on seat.
func bearerMatchesSeat(alts []filterAlt, c *cards.Face, seat int) bool {
	for _, a := range alts {
		if !a.known || a.self || !a.matches(c, false) {
			continue
		}
		if a.ctrl == ctrlAny || (a.ctrl == ctrlYou) == (seat == 0) {
			return true
		}
	}
	return false
}

// attachBearerFor is attachBearer for an attachment trigger: the most inert
// creature both the Aura's Enchant keyword and the stripped trigger filter
// (attachFilter) take, and the seat that must control it. "" when no creature
// satisfies both.
func attachBearerFor(reg *cards.Registry, b *certdeck.Builder, card string, face *cards.Face, alts []filterAlt) (string, int) {
	req, ok := enchantReq(face)
	if !ok {
		return "", 0
	}
	reqAlts := parseFilter(strings.Join(req.raw, ","))
	for _, a := range alts {
		if !a.known || a.self {
			continue
		}
		for _, r := range reqAlts {
			if !r.known || r.self || !ctrlAgree(a.ctrl, r.ctrl) {
				continue
			}
			seat := ctrlPick(a.ctrl, r.ctrl)
			if seat == ctrlAny {
				seat = 0
			}
			name := pickCarrier(reg, b, card, func(c *cards.Face) bool {
				return c.IsPermanent() && FieldBlock(c) == "" && !deadOnArrival(c) &&
					!c.HasKeyword("Hexproof") && !c.HasKeyword("Shroud") &&
					survivesAura(face, c) &&
					a.matches(c, false) && r.matches(c, false)
			})
			if name != "" {
				return name, seat
			}
		}
	}
	return "", 0
}

// attachLandBearer is attachBearerFor for an "Enchant land" Aura: the basic
// land both the Enchant keyword and the stripped trigger filter take, or "".
func attachLandBearer(reg *cards.Registry, face *cards.Face, alts []filterAlt) (string, int) {
	req, ok := enchantReq(face)
	if !ok {
		return "", 0
	}
	reqAlts := parseFilter(strings.Join(req.raw, ","))
	for _, a := range alts {
		if !a.known || a.self {
			continue
		}
		for _, r := range reqAlts {
			if !r.known || r.self || !ctrlAgree(a.ctrl, r.ctrl) {
				continue
			}
			seat := ctrlPick(a.ctrl, r.ctrl)
			if seat == ctrlAny {
				seat = 0
			}
			for _, basic := range append([]string{basicFor(face.ManaCost)}, "Plains", "Island", "Swamp", "Mountain", "Forest") {
				c, ok := reg.Lookup(basic)
				if !ok || len(c.Faces) == 0 {
					continue
				}
				if a.matches(c.Faces[0], false) && r.matches(c.Faces[0], false) {
					return basic, seat
				}
			}
		}
	}
	return "", 0
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

// attachKeyAura gives the key's own Aura, which p0 placed on the battlefield
// bare, an inert bearer its Enchant keyword takes (attachBearer) and attaches
// it (StartAttach): a bare Aura is refused (FieldBlock aura-unattached), and
// the bearer is inert, so the start's focus play stays the only offer. False,
// leaving both seats untouched, when the keyword names no permanent a carrier
// can be.
func attachKeyAura(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, p0, p1 *StartSeat) bool {
	bearer, bs := attachBearer(reg, b, card, face)
	if bearer == "" {
		return false
	}
	placeBearer(p0, p1, card, bearer, bs)
	return true
}

// attachBearer is the most inert permanent the Aura's Enchant keyword takes (a
// carrier creature, else a basic land) and the seat that must control it, or
// "" when the keyword names no permanent a start can place (a player).
func attachBearer(reg *cards.Registry, b *certdeck.Builder, card string, face *cards.Face) (string, int) {
	return attachBearerWhere(reg, b, card, face, nil)
}

// attachBearerWhere is attachBearer narrowed by keep (nil: no narrowing): the
// combat starts need a bearer that can attack and deal damage.
func attachBearerWhere(reg *cards.Registry, b *certdeck.Builder, card string, face *cards.Face, keep func(*cards.Face) bool) (string, int) {
	req, ok := enchantReq(face)
	if !ok {
		return "", 0
	}
	filter := strings.Join(req.raw, ",")
	fits := func(c *cards.Face) bool {
		return c.IsPermanent() && FieldBlock(c) == "" && !deadOnArrival(c) && (keep == nil || keep(c))
	}
	if n, seat := filterCarrier(reg, b, card, filter, fits); n != "" {
		return n, seat
	}
	// A keyword that takes no carrier creature (Enchant:Forest, Enchant:land
	// you control) still takes a permanent a start file can place: a basic
	// land. The carrier pool holds no land (inertCarrier excludes one), so the
	// fallback walks the basics the filter names, in the fixed basic order,
	// and takes the first that fits; it starts at Plains rather than a
	// colour-matched basic, since the bearer is inert for the focus event (it
	// can still tap for mana). A player or a planeswalker has no such inert
	// basic here and stays a refusal.
	for _, a := range parseFilter(filter) {
		if !a.known || a.self {
			continue
		}
		for _, basic := range []string{"Plains", "Island", "Swamp", "Mountain", "Forest"} {
			c, ok := reg.Lookup(basic)
			if !ok || len(c.Faces) == 0 || !fits(c.Faces[0]) || !a.matches(c.Faces[0], false) {
				continue
			}
			if a.ctrl == ctrlOpp {
				return basic, 1
			}
			return basic, 0
		}
	}
	return "", 0
}

// attackingBearer is a creature an Aura's Enchant keyword takes that attacks
// and deals combat damage: it has power (CR 120.8) and no Defender.
func attackingBearer(c *cards.Face) bool {
	return c.IsCreature() && c.Power() > 0 && !c.HasKeyword("Defender")
}

// attachedSource reports whether a damage or block trigger's source/target
// filter is "the permanent this attaches to": Card.AttachedBy,
// Card.EnchantedBy, Card.EquippedBy (and the Creature spellings). The pinned
// gorge compiles all three to one predicate (effects/compiled_predicate.go),
// so they are all the bearer shape an Aura bearer start reads.
func attachedSource(filter string) bool {
	head, props, _ := strings.Cut(strings.TrimSpace(filter), ".")
	if head != "Card" && head != "Creature" {
		return false
	}
	switch props {
	case "AttachedBy", "EnchantedBy", "EquippedBy":
		return true
	}
	return false
}

// modifiedSource reads a damage source filter with one "modified" property
// (CR 700.9: a permanent with a counter, an Equipment or an Aura on it) and
// returns the filter without it; a +1/+1 counter on the creature that deals
// the damage meets the property.
func modifiedSource(filter string) (string, bool) {
	head, props, _ := strings.Cut(strings.TrimSpace(filter), ".")
	if strings.Contains(filter, ",") || props == "" {
		return filter, false
	}
	var rest []string
	found := false
	for _, p := range strings.Split(props, "+") {
		if p == "modified" {
			found = true
			continue
		}
		rest = append(rest, p)
	}
	if !found {
		return filter, false
	}
	if len(rest) == 0 {
		return head, true
	}
	return head + "." + strings.Join(rest, "+"), true
}

// murderFocus is the opponent's Murder destroying the card or its bearer: the
// hand and lands are placed on seat, which takes the cast as its focus.
func murderFocus(reg *cards.Registry, b *certdeck.Builder, seat *StartSeat, f *StartFile, s int, spell string) bool {
	if spell == "" {
		spell = "Murder"
	}
	if !focusCast(reg, b, seat, spell) {
		return false
	}
	f.Active = s
	f.Focus = &StartFocus{Seat: seatPtr(s), Card: spell, Kind: "cast"}
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
// beginning of the end step (Ball Lightning) that keep also takes, or "": the
// body an Equipment can be attached to with Magnetic Theft and then lose,
// which fires its AttachedBy trigger while the Equipment is still attached.
// keep is the stripped attachment trigger's filter (attachFilter), so the
// bearer is one the trigger itself reads; nil accepts any.
func endStepSacrificeBearer(reg *cards.Registry, b *certdeck.Builder, card string, keep func(*cards.Face) bool) string {
	accepts := func(n string) bool {
		if n == card {
			return false
		}
		if keep == nil {
			return true
		}
		c, ok := reg.Lookup(n)
		return ok && len(c.Faces) > 0 && keep(c.Faces[0])
	}
	if v, ok := endStepSacrifices.Load(reg); ok {
		for _, n := range v.([]string) {
			if accepts(n) {
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
		if accepts(n) {
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

// targetCarriers are the safe spells a "whenever you cast a spell that
// targets this" (heroic) start casts: the first inert own-targeters, then an
// Aura for a filter that names one. They all target a creature its
// controller controls, so the card itself is a legal target.
var targetCarriers = append(slices.Clone(ownTargeters), "Rancor")

// targetSelf reports whether the trigger's TargetsValid names the card itself
// ("whenever you cast a spell that targets this": heroic).
func targetSelf(t *cards.Trigger) bool {
	for _, a := range parseFilter(t.Params["TargetsValid"]) {
		if a.known && a.self {
			return true
		}
	}
	return false
}

// filterTakes reports whether an understood, non-Self alternative of filter
// takes f, and whether any alternative was understood at all.
func filterTakes(filter string, f *cards.Face) (takes, known bool) {
	for _, a := range parseFilter(filter) {
		if !a.known || a.self || a.ctrl == ctrlOpp {
			continue
		}
		known = true
		if a.test(f) {
			takes = true
		}
	}
	return takes, known
}

// targetCarrier is the first targetCarrier keep accepts and the ValidCard
// filter takes, or "".
func targetCarrier(reg *cards.Registry, b *certdeck.Builder, card, filter string, keep func(*cards.Face) bool) string {
	for _, name := range targetCarriers {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		cf := c.Faces[0]
		if cf.Name == card || !b.Playable(c) || !keep(cf) {
			continue
		}
		if filter != "" {
			if takes, known := filterTakes(filter, cf); !known || !takes {
				continue
			}
		}
		return name
	}
	return ""
}

// bareCastParams are the SpellCast trigger params a start accounts for
// directly: presentation, when the cast happens (spellStart reads
// TriggerZones, ValidActivatingPlayer, OpponentTurn and
// ActivatorThisTurnCast), or the target shapes the heroic arm reads
// (TargetsValid, IsSingleTarget). Every other param narrows the cast, its
// target or a fire-time condition -- ValidSA/ValidSAonCard/Increment,
// CheckSVar/SVarCompare, IsPresent/PresentCompare, a spend gate -- and the
// inert carrier (Memnite) does not meet it. This is deliberately a whitelist:
// a gate param added to the corpus (or a new keyword expansion's marker) is
// refused by default instead of writing a start whose trigger can never fire.
var bareCastParams = map[string]bool{
	"Mode": true, "TriggerDescription": true, "Execute": true,
	"TriggerZones": true, "ValidCard": true,
	"ValidActivatingPlayer": true, "ActivatorThisTurnCast": true,
	"OpponentTurn": true, "TargetsValid": true, "IsSingleTarget": true,
	"OptionalDecider": true, "ActivationLimit": true, "ResolvedLimit": true,
	"GameActivationLimit": true, "OrderDuplicates": true,
	"Secondary": true, "Static": true, "NoResolvingCheck": true,
	// Keyword expansion provenance (addKeywordTrigger): the expansion's
	// marker is the param that gates it (Increment, not in this map).
	"Keyword": true, "KeywordLine": true,
}

// bareCast reports whether a SpellCast trigger asks nothing more of the cast
// than that it is a spell. selfTarget allows the two target-shape params the
// heroic arm meets with targetCarrier -- TargetsValid$ Card.Self and
// IsSingleTarget$, every targetCarrier naming exactly one target -- while the
// no-filter arm (an arbitrary inert carrier) refuses both. Any other param
// outside bareCastParams is a gate neither arm satisfies.
func bareCast(t *cards.Trigger, selfTarget bool) bool {
	for p := range t.Params {
		if (p == "TargetsValid" || p == "IsSingleTarget") && !selfTarget {
			return false
		}
		if !bareCastParams[p] {
			return false
		}
	}
	return true
}

// spellStart is a SpellCast (or SpellCastOrCopy) trigger. "When you cast
// this spell" (Card.Self, TriggerZones Stack or none) is the card cast from
// hand. Otherwise the card is on the battlefield and the seat that
// ValidActivatingPlayer names casts a carrier (carrierPool) that ValidCard
// takes, with exactly its mana: on its own turn, or, for a NonActive caster,
// an instant on the other seat's turn. A trigger with no ValidCard and no
// other cast gate takes any spell; one whose TargetsValid is the card itself
// (heroic) casts a carrier that targets it (targetCarrier).
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
	filter := trig.Params["ValidCard"]
	var name string
	switch {
	case targetSelf(trig) && bareCast(trig, true):
		// "whenever you cast a spell that targets this": the caster's own
		// targeter is the carrier and the card is its target.
		if caster != 0 || !face.IsCreature() {
			return none + ":filter"
		}
		name = targetCarrier(reg, b, card, filter, keep)
	case strings.TrimSpace(filter) == "":
		// No ValidCard: any spell satisfies the cast, but only a trigger
		// with no other gate does. A trigger that narrows the spell
		// (ValidSA, Increment, a target shape, a condition) refuses here
		// instead of being handed a carrier its own matcher rejects.
		if !bareCast(trig, false) {
			return none + ":filter"
		}
		name = pickCarrier(reg, b, card, keep)
	default:
		// A ValidCard filter, including the non-Self TargetsValid shapes
		// (the start-not-exercised lane): keep the established carrier path.
		name, _ = filterCarrier(reg, b, card, filter, keep)
		if name == "" && bareCast(trig, false) {
			name = permanentSpellCarrier(reg, b, face, card, filter, caster, keep)
		}
	}
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

// permanentSpellCarrier is the spell a ValidCard filter that names Equipment
// or Aura needs, which the inert pool (carrierPool) never holds: an Equipment
// spell is the simplest non-creature Equipment the filter takes (cast, it
// needs no target); an Aura spell is an Aura that targets the card itself
// (targetCarrier), so the card, a creature the caster controls, is the
// Aura's one bearer. "" when the filter names neither (a stat or kicker
// property stays refused), or no such carrier exists for this caster.
func permanentSpellCarrier(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, filter string, caster int, keep func(*cards.Face) bool) string {
	var equipment, aura bool
	for _, alt := range strings.Split(filter, ",") {
		switch strings.TrimSpace(alt) {
		case "Equipment", "Equipment.YouCtrl":
			equipment = true
		case "Aura", "Aura.YouCtrl":
			aura = true
		}
	}
	if equipment {
		takes := func(f *cards.Face) bool {
			if !f.HasTypeWord("Equipment") || f.IsCreature() || FieldBlock(f) != "" || !keep(f) || !plainCost(f) {
				return false
			}
			t, known := filterTakes(filter, f)
			return known && t
		}
		if n := pickZoneFixture(reg, b, card, takes, nil, true); n != "" {
			return n
		}
	}
	if aura && caster == 0 && face.IsCreature() {
		return targetCarrier(reg, b, card, filter, func(f *cards.Face) bool { return keep(f) && plainCost(f) })
	}
	return ""
}

// plainCost reports whether a card's mana cost is one basic land per symbol
// (no X, snow, colourless-only or Phyrexian symbol), castCarrier's lands.
func plainCost(f *cards.Face) bool {
	if f.ManaCost == "" || f.ManaCost == "no cost" {
		return false
	}
	for _, tok := range splitCost(f.ManaCost) {
		if !manaTokenRE.MatchString(tok) || tok == "X" || tok == "S" || tok == "C" {
			return false
		}
	}
	return true
}

// attackCarrier is the creature that attacks for the card: the card itself
// when it is a creature the filter takes (an empty filter takes any), else
// a carrier creature (carrierPool) the filter takes, placed on seat 0's
// battlefield beside it.
func attackCarrier(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, filter string, f *StartFile) string {
	if face.IsCreature() && filter == "" {
		return card
	}
	if face.IsCreature() {
		for _, a := range parseFilterCounters(filter, true, false, false, true) {
			if a.known && !a.other && a.ctrl != ctrlOpp && a.matches(face, false) && survivesCounters(face, a.counters) {
				placeAttackGates(&f.Setup.P0, card, a.counters)
				designateCommander(reg, &f.Setup.P0, card, filter)
				return card
			}
		}
	}
	if filter == "" {
		filter = "Creature.YouCtrl"
	}
	// The carrier arm, filterCarrier's rule (the first understood, non-Self
	// alternative some inert creature matches; an OppCtrl alternative names a
	// seat 1 attacker, never taken here) with the counter gates read and
	// placed, so "a creature you control with a counter on it" attacks.
	for _, a := range parseFilterCounters(filter, true, false, false, true) {
		if !a.known || a.self || a.ctrl == ctrlOpp {
			continue
		}
		keep := func(c *cards.Face) bool {
			return c.IsCreature() && FieldBlock(c) == "" && !deadOnArrival(c) && survivesCounters(c, a.counters)
		}
		takes := func(c *cards.Face) bool { return keep(c) && a.matches(c, false) }
		name := pickCarrier(reg, b, card, takes)
		if name == "" && a.commander {
			// The inert carrier pool can never hold a card the seat would
			// designate as its commander, so an IsCommander alternative is
			// not refused on an empty pool: the same fixture scan the static
			// lane uses (fixtureCarrierMode's pickFixture), the same
			// fixtureLess ordering, so the attacker stays deterministic.
			name = pickFixture(reg, b, card, a.test, takes, true)
		}
		if name == "" {
			continue
		}
		f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, name)
		placeAttackGates(&f.Setup.P0, name, a.counters)
		designateCommander(reg, &f.Setup.P0, name, filter)
		return name
	}
	return ""
}

// placeAttackGates places an attacker's required counters on the seat holding
// it.
func placeAttackGates(seat *StartSeat, name string, gates []counterGate) {
	for _, g := range gates {
		seat.Counters = append(seat.Counters, StartCounter{Card: name, Kind: g.placedKind(), N: g.n})
	}
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
	var modified bool
	switch trig.Mode {
	case "DamageDone":
		vt := trig.Params["ValidTarget"]
		src := trig.Params["ValidSource"]
		if (vt == "Player" || vt == "Opponent") && trig.Params["CombatDamage"] != "False" &&
			attachedSource(src) && FieldBlock(face) == "aura-unattached" {
			return auraAttackStart(reg, b, face, card, f, none)
		}
		if trig.Params["CombatDamage"] != "True" || vt != "Player" && vt != "Opponent" {
			return none
		}
		filter = src
		filter, modified = modifiedSource(filter)
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
	if modified {
		f.Setup.P0.Counters = append(f.Setup.P0.Counters, StartCounter{Card: attacker, Kind: "P1P1", N: 1})
	}
	f.Focus = &StartFocus{Seat: seatPtr(0), Card: attacker, Kind: "attack"}
	return ""
}

// auraAttackStart is "whenever enchanted creature deals (combat) damage to a
// player" (ValidSource Card.AttachedBy): the Aura is placed attached
// (StartAttach) to a bearer its Enchant keyword takes that can attack, on the
// Aura's controller's side, and the bearer attacks a seat with no blocker.
func auraAttackStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, f *StartFile, none string) string {
	bearer, bs := attachBearerWhere(reg, b, card, face, attackingBearer)
	if bearer == "" || bs != 0 {
		return none + ":filter"
	}
	f.Setup.P0.Battlefield = []string{card}
	placeBearer(&f.Setup.P0, &f.Setup.P1, card, bearer, bs)
	f.Focus = &StartFocus{Seat: seatPtr(0), Card: bearer, Kind: "attack"}
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
//
// prelude says the card's own entry runs a state-backed as-enters replacement:
// it is cast from hand at turn 1's first main (the targeters are instants, so
// the caster can act on the controller's turn) instead of being placed at
// genesis, which would carry entry stamps the lowering's setup cannot restate.
// The card's controller is then the active seat and the cast is the prelude.
func targetStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, f *StartFile, prelude bool) string {
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
	// numericToughnessDead, not deadOnArrival: the CDA of the key's own card
	// is judged over the finished start (cdaToughnessZero), so a Harmonious
	// Grovestrider the Murder cast targets is not dead here.
	if target == card && numericToughnessDead(face) {
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
	if prelude {
		// The entry stamps are the cast's, not a genesis placement's: the card
		// starts in hand with the lands its cost takes, and the controller is
		// the active seat so it can cast a sorcery-speed permanent.
		castCarrier(reg, &f.Setup.P0, card)
		f.Prelude, f.PreludeSeat = card, 0
	} else {
		f.Setup.P0.Battlefield = []string{card}
	}
	if target != card {
		seats[tseat].Battlefield = append(seats[tseat].Battlefield, target)
	}
	castCarrier(reg, seats[caster], spellName)
	if prelude {
		f.Active = 0
	} else {
		f.Active = caster
	}
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
// you" (Attacked You) is the opponent's carrier attacking on its turn. A
// filter naming the key card's attached creature (Card.AttachedBy,
// Creature.EnchantedBy, Card.EquippedBy) is the bearer shape: an Aura is
// placed attached (StartAttach) and its bearer attacks (auraAttacksStart); an
// Equipment cannot enter attached from a start, and attaching it needs a
// Magnetic Theft cast before the attack, a second turn the one focus play
// cannot take, so it stays refused (:equipped).
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
	switch prop := bearerFilterProp(filter); prop {
	case "EquippedBy":
		// The key Equipment cannot enter attached from a start (gorge's
		// StartAttach is the Aura entry): attaching it needs a Magnetic Theft
		// cast before the attack, a second turn the one focus play cannot
		// take.
		return none + ":equipped"
	case "AttachedBy", "EnchantedBy":
		if why := auraAttacksStart(reg, b, face, card, trig, bearerFilter(filter, prop), f); why != "" {
			return none + ":" + why
		}
		return ""
	}
	// parseFilterCommander: the IsCommander alternative is understood here
	// because attackCarrier designates the attacker it picks (the engine
	// reads Card.IsCommander through Game.CommanderOf, so an undesignated
	// attacker would never fire the trigger).
	for _, a := range parseFilterCommander(filter) {
		if a.self || !a.known {
			// An unread property is not a carrier test: a counter gate is a
			// placement and a tapped state a battlefield state. attackCarrier
			// reads the gates (parseFilterCounters) and places them on the
			// card itself or its carrier; a property it cannot establish (a
			// game-history gate) leaves the card's own attack, when it can.
			if !face.IsCreature() {
				return none + ":filter"
			}
			if attacker := attackCarrier(reg, b, face, card, filter, f); attacker != "" {
				f.Focus = &StartFocus{Seat: seatPtr(0), Card: attacker, Kind: "attack"}
				return ""
			}
			f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Kind: "attack"}
			return ""
		}
	}
	switch att := trig.Params["Attacked"]; {
	case att == "":
	case strings.HasPrefix(att, "You"):
		// the opponent attacks the card's controller
		name, s := filterCarrierCommander(reg, b, card, filter, func(c *cards.Face) bool {
			return c.IsCreature() && !c.HasKeyword("Defender") && !deadOnArrival(c)
		})
		if name == "" || s != 0 {
			return none + ":filter"
		}
		f.Setup.P1.Battlefield = append(f.Setup.P1.Battlefield, name)
		designateCommander(reg, &f.Setup.P1, name, filter)
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

// bearerFilterProp is the attachment property some alternative of the filter
// names in its bare form ("Card.AttachedBy", "Creature.EquippedBy"): the
// trigger reads the creature the key card is attached to, not a carrier the
// board can simply match. "" when none does (a negated or qualified form,
// "!EquippedBy" or "EquippedBy Aura", is not a plain bearer and stays "").
func bearerFilterProp(filter string) string {
	for _, prop := range []string{"AttachedBy", "EnchantedBy", "EquippedBy"} {
		if filterHasProp(filter, prop) {
			return prop
		}
	}
	return ""
}

// bearerFilter strips the bearer property from the filter alternatives that
// carry it ("Card.AttachedBy+powerLE2" gives "Card.powerLE2",
// "Creature.EnchantedBy" gives "Creature"), dropping the rest: the bearer is
// the attacker, so the trigger's gate on it is that alternative without the
// attachment property, and an alternative that does not name the bearer
// describes an attacker the start does not place.
func bearerFilter(filter, prop string) string {
	var out []string
	for _, alt := range strings.Split(filter, ",") {
		alt = strings.TrimSpace(alt)
		head, props, _ := strings.Cut(alt, ".")
		has := head == prop
		if has {
			head = "Card"
		}
		var kept []string
		for _, p := range strings.Split(props, "+") {
			switch {
			case p == prop:
				has = true
			case p != "":
				kept = append(kept, p)
			}
		}
		if !has {
			continue
		}
		a := head
		if len(kept) > 0 {
			a += "." + strings.Join(kept, "+")
		}
		out = append(out, a)
	}
	return strings.Join(out, ",")
}

// auraAttacksStart is an Attacks trigger whose filter names the key Aura's
// bearer (Card.AttachedBy, Creature.EnchantedBy, minus that property in
// bearerFilter): the Aura is placed attached (StartAttach) to the most inert
// creature its Enchant keyword takes that the stripped filter takes and that
// can attack (no Defender, no death on arrival), and that bearer is the focus
// attack. "whenever enchanted creature attacks you" (Attacked You) is the
// opponent's bearer attacking on its turn.
func auraAttacksStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, filter string, f *StartFile) string {
	if FieldBlock(face) != "aura-unattached" {
		return "filter" // not an unattached Aura: nothing to place attached
	}
	req, ok := enchantReq(face)
	if !ok {
		return "filter" // the Aura's Enchant keyword names no object
	}
	reqAlts := parseFilter(strings.Join(req.raw, ","))
	bearer, bs := filterCarrier(reg, b, card, filter, func(c *cards.Face) bool {
		if !c.IsCreature() || c.HasKeyword("Defender") || FieldBlock(c) != "" || deadOnArrival(c) {
			return false
		}
		for _, a := range reqAlts {
			if a.matches(c, false) {
				return true
			}
		}
		return false
	})
	if bearer == "" {
		return "filter" // the Enchant keyword takes no attackable bearer
	}
	if att := trig.Params["Attacked"]; att != "" && (bs != 1 || !strings.HasPrefix(att, "You")) {
		// "attacks you" is the opponent's bearer attacking the Aura's
		// controller; any other target of the attack the start does not place
		return "attacked"
	}
	placeBearer(&f.Setup.P0, &f.Setup.P1, card, bearer, bs)
	f.Active = bs
	f.Focus = &StartFocus{Seat: seatPtr(bs), Card: bearer, Kind: "attack"}
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
		if !murderFocus(reg, b, p1, f, 1, "") {
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
