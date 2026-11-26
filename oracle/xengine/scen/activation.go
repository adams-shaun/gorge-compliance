package scen

import (
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/state"
)

// The activation gates the engine reads when it offers the focus play at the
// start's step (turn 1's precombat main, step MAIN1). An ability whose gate
// fails is never offered, and a spell whose window, legendary lock or
// self-restriction fails is never cast: xproof would write a start the play
// never takes, and play refuses it as start-not-exercised. The gates here
// mirror the engine's offer-time reads (engine/rules/legal_activation.go,
// activation_phases.go, statics_cast.go, engine/effects CheckSVar and the
// present/zone-count family): where the placed state can satisfy a gate, they
// place it; where the engine's answer is deterministic and fails, they refuse
// the start by name; where the engine fails open (a count body this pin does
// not model, a non-literal comparison) they leave the start as it was.
const (
	reasonUpkeepOnly    = "start:upkeep-only"
	reasonPhaseWindow   = "start:phase-window"
	reasonFirstCombat   = "start:first-combat"
	reasonAfterBlockers = "start:after-blockers"
	reasonPlayerTurn    = "start:player-turn"
	reasonOpponentTurn  = "start:opponent-turn"
	reasonPresentCount  = "start:present-count"
	reasonPresentZone   = "start:present-zone"
	reasonMorbid        = "start:morbid"
	reasonRevolt        = "start:revolt"
	reasonMonarch       = "start:monarch"
	reasonAttackers     = "start:attackers-declared"
	reasonYourTurns     = "start:your-turns"
	reasonLifeTotal     = "start:life-total"
	reasonLegendary     = "start:legendary-sorcery"
	reasonCantBeCast    = "start:cantbecast"
	reasonCountUnmet    = "start:count-unmet"
)

// coreCardTypes is the engine's distinct-type census (legal_activation.go):
// the types Delirium and the CardTypes count read.
var coreCardTypes = []string{"Artifact", "Battle", "Creature", "Enchantment",
	"Instant", "Kindred", "Land", "Planeswalker", "Sorcery"}

// condKind lowers a Forge condition or property word to the refusal's kind:
// "wasDealtDamageThisTurn" -> "was-dealt-damage-this-turn".
func condKind(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r - 'A' + 'a')
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			dash = true
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	return b.String()
}

// cmpParse splits a Forge comparison literal ("GE2", "LE0") into its
// two-character operator and numeric rhs; ok is false for anything else,
// including the absent value (the callers default to GE1 themselves).
func cmpParse(cmp string) (op string, n int, ok bool) {
	cmp = strings.TrimSpace(cmp)
	if len(cmp) < 3 {
		return "", 0, false
	}
	n, err := strconv.Atoi(cmp[2:])
	if err != nil {
		return "", 0, false
	}
	return strings.ToUpper(cmp[:2]), n, true
}

// cmpHolds applies op to v and n.
func cmpHolds(v int, op string, n int) bool {
	switch op {
	case "GE":
		return v >= n
	case "GT":
		return v > n
	case "LE":
		return v <= n
	case "LT":
		return v < n
	case "EQ":
		return v == n
	case "NE":
		return v != n
	}
	return false
}

// ctrlOK reports whether an object on the focus seat's side (mine) may match
// a filter alternative with controller requirement ctrl.
func ctrlOK(ctrl int, mine bool) bool {
	switch ctrl {
	case ctrlYou:
		return mine
	case ctrlOpp:
		return !mine
	}
	return true
}

// focusGateRefusal is why the focus play's activation gates keep it from
// being offered at the start's step, or "" after it has placed the state the
// gates read. baitable: the play may have a spell cast before it, which can
// move the active seat, so a turn-owner gate is not decidable yet.
func focusGateRefusal(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, sa *cards.SA, card string, f *StartFile, baitable bool) string {
	fc := f.Focus
	if fc == nil || fc.Seat == nil {
		return ""
	}
	seat := *fc.Seat
	switch {
	case fc.Kind == "ability" && fc.Card == card && sa != nil:
		if why := phaseGateRefusal(sa.Params, baitable, f.Active, seat, fc.Attack != ""); why != "" {
			return why
		}
		if why := activationConditionRefusal(reg, b, card, sa, f, seat); why != "" {
			return why
		}
		if why := presentGateRefusal(reg, b, card, sa.Params, f, seat, true); why != "" {
			return why
		}
		return sVarGateRefusal(reg, b, card, face, sa.Params, f, seat)
	case fc.Kind == "cast":
		first, met := "", false
		for _, m := range face.Abilities {
			if m.Kind != "SP" {
				continue
			}
			why := phaseGateRefusal(m.Params, baitable, f.Active, seat, false)
			if why == "" {
				why = presentGateRefusal(reg, b, card, m.Params, f, seat, false)
			}
			if why == "" {
				why = sVarGateRefusal(reg, b, card, face, m.Params, f, seat)
			}
			if why == "" {
				met = true // a mode the bot may take
			} else if first == "" {
				first = why
			}
		}
		if !met && first != "" {
			return first // every SP mode's gates fail; a face with no SP ability has none to fail
		}
		if face.IsLegendary() && (face.IsInstant() || face.IsSorcery()) && !legendaryKindOn(reg, f, seat) {
			name := pickFixture(reg, b, card, func(g *cards.Face) bool {
				return g.IsLegendary() && (g.IsCreature() || g.IsPlaneswalker()) && !deadOnArrival(g)
			}, nil, true)
			if name == "" {
				return reasonLegendary
			}
			placeOn(f, seat, name)
		}
		return cantBeCastRefusal(reg, b, card, face, f, seat)
	}
	return ""
}

// phaseGateRefusal mirrors activationPhasesOK at the start's step: the
// ActivationPhases$ window (an unresolvable element fails closed), the
// first-combat and after-blockers riders (both fail at the precombat main),
// and the turn-owner gates (a non-True value fails anywhere; True requires
// the named seat to be active -- skipped while a bait may be placed, which
// can move the active seat). line: the play waits for a scripted combat
// (StartFocus.Attack), so it is taken once the blocks are declared and a window
// holding the declare blockers step (ninjutsu) serves it as the precombat main
// does.
func phaseGateRefusal(params map[string]string, baitable bool, active, seat int, line bool) string {
	if spec := strings.TrimSpace(params["ActivationPhases"]); spec != "" {
		set, unknown := state.ParsePhases(spec)
		if len(unknown) > 0 || set.Empty() || !set.Has(state.StepMain1) && !(line && set.Has(state.StepDeclareBlockers)) {
			if strings.Contains(strings.ToLower(spec), "upkeep") {
				return reasonUpkeepOnly
			}
			return reasonPhaseWindow
		}
	}
	if v, ok := params["ActivationFirstCombat"]; ok && !strings.EqualFold(strings.TrimSpace(v), "True") {
		return reasonFirstCombat
	}
	if _, ok := params["ActivationAfterBlockers"]; ok {
		return reasonAfterBlockers
	}
	if !baitable {
		if v, ok := params["PlayerTurn"]; ok {
			if !strings.EqualFold(strings.TrimSpace(v), "True") || active != seat {
				return reasonPlayerTurn
			}
		}
		if v, ok := params["OpponentTurn"]; ok {
			if !strings.EqualFold(strings.TrimSpace(v), "True") || active == seat {
				return reasonOpponentTurn
			}
		}
	}
	return ""
}

// activationConditionRefusal mirrors activationConditionOK for the
// focus play's Activation$ condition: the met conditions pass, the placeable
// ones are placed to, and an unmet condition no placement satisfies refuses
// the start (the engine withholds the ability, so the play is never offered).
func activationConditionRefusal(reg *cards.Registry, b *certdeck.Builder, card string, sa *cards.SA, f *StartFile, seat int) string {
	raw := strings.TrimSpace(sa.Params["Activation"])
	if raw == "" {
		return ""
	}
	s := focusSeat(f, seat)
	switch strings.ToLower(raw) {
	case "hellbent":
		if len(s.Hand) > 0 {
			return "start:activation-condition:hellbent"
		}
	case "threshold":
		if name := pickCarrier(reg, b, card, func(*cards.Face) bool { return true }); name != "" {
			for i := len(s.Graveyard); i < 7; i++ {
				s.Graveyard = append(s.Graveyard, name)
			}
		}
		if len(s.Graveyard) < 7 {
			return "start:activation-condition:threshold"
		}
	case "metalcraft":
		need := 3 - countTyped(reg, f, seat, "Artifact")
		if need > 0 {
			name, _ := filterCarrier(reg, b, card, "Artifact.YouCtrl", copyable(need))
			if name == "" {
				return "start:activation-condition:metalcraft"
			}
			for i := 0; i < need; i++ {
				placeOn(f, seat, name)
			}
		}
	case "delirium":
		seen := placedCoreTypes(reg, f, seat)
		for _, ty := range coreCardTypes {
			if len(seen) >= 4 {
				break
			}
			if seen[ty] {
				continue
			}
			name := coreTypeFixture(reg, b, card, ty)
			if name == "" {
				continue // no card of the type: another type serves
			}
			s.Graveyard = append(s.Graveyard, name)
			seen[ty] = true
		}
		if len(seen) < 4 {
			return "start:activation-condition:delirium"
		}
	default:
		return "start:activation-condition:" + condKind(raw)
	}
	return ""
}

// countTyped counts the placed cards of one type word on the seat's
// battlefield (the focus card itself included: it is there too).
func countTyped(reg *cards.Registry, f *StartFile, seat int, word string) int {
	n := 0
	for _, name := range focusSeat(f, seat).Battlefield {
		if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 && c.Faces[0].HasTypeWord(word) {
			n++
		}
	}
	return n
}

// placedCoreTypes is the set of core card types the seat's graveyard already
// holds.
func placedCoreTypes(reg *cards.Registry, f *StartFile, seat int) map[string]bool {
	seen := map[string]bool{}
	for _, name := range focusSeat(f, seat).Graveyard {
		c, ok := reg.Lookup(name)
		if !ok {
			continue
		}
		for _, fa := range c.Faces {
			for _, ty := range fa.Types {
				if slices.Contains(coreCardTypes, ty) {
					seen[ty] = true
				}
			}
		}
	}
	return seen
}

// coreTypeFixture is a card of one core type, placed in a graveyard (or an
// exile): a land from the mana base, anything else the least busy non-land
// card of the type.
func coreTypeFixture(reg *cards.Registry, b *certdeck.Builder, card, ty string) string {
	if ty == "Land" {
		return landFixture(reg, b, "Land")
	}
	return pickZoneFixture(reg, b, card, func(f *cards.Face) bool { return f.HasTypeWord(ty) }, nil, true)
}

// legendaryKindOn reports whether the focus seat controls a legendary
// creature or planeswalker (CR 205.4e's release of a legendary spell's lock).
func legendaryKindOn(reg *cards.Registry, f *StartFile, seat int) bool {
	for _, name := range focusSeat(f, seat).Battlefield {
		if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 {
			if g := c.Faces[0]; g.IsLegendary() && (g.IsCreature() || g.IsPlaneswalker()) {
				return true
			}
		}
	}
	return false
}

// parsePresentZones reads a PresentZone$ value into lower-case zone words;
// known is false for a word the start does not map (the engine fails closed
// there too: presentZoneFromParam).
func parsePresentZones(pz string) ([]string, bool) {
	pz = strings.TrimSpace(pz)
	if pz == "" {
		return []string{"battlefield"}, true
	}
	var out []string
	for _, tok := range strings.Split(pz, ",") {
		w := strings.ToLower(strings.TrimSpace(tok))
		switch w {
		case "battlefield", "hand", "graveyard", "library", "exile", "stack", "command":
			out = append(out, w)
		default:
			return nil, false
		}
	}
	return out, true
}

// zoneCards is the seat's placed list in the zone word.
func zoneCards(f *StartFile, seat int, zone string) []string {
	s := focusSeat(f, seat)
	switch zone {
	case "hand":
		return s.Hand
	case "graveyard":
		return s.Graveyard
	case "exile":
		return s.Exile
	case "library":
		return s.Library
	default:
		return s.Battlefield
	}
}

// zonePlaceable reports whether a start can place into the zone word.
func zonePlaceable(zone string) bool {
	return zone != "stack" && zone != "command"
}

// zonePut appends name to the zone of the focus seat.
func zonePut(f *StartFile, seat int, zone, name string) {
	s := focusSeat(f, seat)
	switch zone {
	case "hand":
		s.Hand = append(s.Hand, name)
	case "graveyard":
		s.Graveyard = append(s.Graveyard, name)
	case "exile":
		s.Exile = append(s.Exile, name)
	case "library":
		s.Library = append(s.Library, name)
	default:
		placeOn(f, seat, name)
	}
}

// presentCount counts the placed cards the IsPresent$ spec matches over the
// named zones of every seat, as the engine's offer gate counts them
// (countPresent walks both seats' battlefields; presentZoneCount every living
// seat's named zones). source: the focus card is a battlefield permanent
// whose Self alternatives match it; a cast's card is in hand and matches
// none. Only alternatives parseFilter understands count: one it does not is
// one the engine fails closed on (it never matches either).
func presentCount(reg *cards.Registry, f *StartFile, seat int, zones []string, alts []filterAlt, card string, source bool) int {
	n := 0
	for _, z := range zones {
		if !zonePlaceable(z) {
			continue
		}
		for s := 0; s < 2; s++ {
			for _, name := range zoneCards(f, s, z) {
				c, ok := reg.Lookup(name)
				if !ok || len(c.Faces) == 0 {
					continue
				}
				fa := c.Faces[0]
				for _, a := range alts {
					if !a.known {
						continue
					}
					if a.self && !(source && z == "battlefield" && s == seat && name == card) {
						continue
					}
					if !ctrlOK(a.ctrl, s == seat) || !a.matches(fa, false) {
						continue
					}
					n++
					break
				}
			}
		}
	}
	return n
}

// placePresent places count cards some alternative of the spec matches, into
// the first placeable zone named, on the side the alternative's controller
// names. It returns how many it placed.
func placePresent(reg *cards.Registry, b *certdeck.Builder, card string, f *StartFile, alts []filterAlt, raws []string, zones []string, seat, count int) int {
	placed := 0
	zone := ""
	for _, z := range zones {
		if zonePlaceable(z) {
			zone = z
			break
		}
	}
	if zone == "" {
		return 0 // no zone the start places to: the gate counts what it cannot see
	}
	for placed < count {
		done := false
		for k, a := range alts {
			if !a.known || a.self {
				continue
			}
			tseat := seat
			if a.ctrl == ctrlOpp {
				tseat = 1 - seat
			}
			var name string
			if zone == "battlefield" {
				name, _ = filterCarrier(reg, b, card, raws[k], copyable(count-placed))
				if name == "" {
					name = landFixture(reg, b, raws[k])
				}
				if name == "" {
					name = pickFixture(reg, b, card, a.test, func(g *cards.Face) bool {
						return g.IsPermanent() && (count-placed <= 1 || !g.IsLegendary())
					}, true)
				}
				if name == "" {
					continue
				}
				placeOn(f, tseat, name)
			} else {
				name = pickZoneFixture(reg, b, card, a.test, nil, true)
				if name == "" {
					continue
				}
				zonePut(f, tseat, zone, name)
			}
			placed++
			done = true
			break
		}
		if !done {
			break
		}
	}
	return placed
}

// presentGateRefusal mirrors abilityPresentHolds for the focus play's
// IsPresent$/PresentZone$/PresentCompare$ gate: the count is computed over
// the placed state, a shortfall is placed to, and a shortfall no placement
// satisfies refuses the start. A comparison the pin cannot fold into a
// literal is undecided and leaves the start as it was.
func presentGateRefusal(reg *cards.Registry, b *certdeck.Builder, card string, params map[string]string, f *StartFile, seat int, source bool) string {
	spec := strings.TrimSpace(params["IsPresent"])
	if spec == "" {
		return ""
	}
	zones, known := parsePresentZones(strings.TrimSpace(params["PresentZone"]))
	if !known {
		return reasonPresentZone
	}
	alts, raws := specAlts(spec)
	op, n, lit := cmpParse(presentCompareOf(params))
	my := presentCount(reg, f, seat, zones, alts, card, source)
	if !lit {
		// engine n >= mine: an upper bound mine already breaks is decided;
		// anything else waits for the engine's SVar rhs resolution.
		if litOK(op) && breaksUpper(my, op, n) {
			return reasonPresentCount
		}
		return ""
	}
	if cmpHolds(my, op, n) {
		return ""
	}
	need := 0
	switch op {
	case "GE", "EQ":
		// EQ: the engine's count is mine or more, so a count short of the
		// bound is placed up to it (above it, the gate is decided: unmet)
		need = n
	case "GT":
		need = n + 1
	case "NE":
		need = my + 1 // mine equals the bound: one more steps off it
	default:
		// LE/LT: the engine's count is mine or more, and mine already breaks
		// the bound
		return reasonPresentCount
	}
	if need <= my {
		return reasonPresentCount // EQ with mine above the bound
	}
	if need-my > 8 {
		return reasonPresentCount
	}
	if placed := placePresent(reg, b, card, f, alts, raws, zones, seat, need-my); placed < need-my {
		return reasonPresentCount
	}
	return ""
}

// litOK reports whether op is one cmpParse can produce.
func litOK(op string) bool {
	switch op {
	case "GE", "GT", "LE", "LT", "EQ", "NE":
		return true
	}
	return false
}

// breaksUpper reports whether v breaks an upper bound (LE/LT/EQ) op/n.
func breaksUpper(v int, op string, n int) bool {
	switch op {
	case "LE":
		return v > n
	case "LT":
		return v >= n
	case "EQ":
		return v != n
	}
	return false
}

// specAlts is a spec's alternatives as parseFilter reads them, with the raw
// strings alongside (placePresent's pick filters).
func specAlts(spec string) ([]filterAlt, []string) {
	var alts []filterAlt
	var raws []string
	for _, r := range strings.Split(spec, ",") {
		if r = strings.TrimSpace(r); r != "" {
			alts = append(alts, parseFilter(r)...)
			raws = append(raws, r)
		}
	}
	return alts, raws
}

// countProps are the numeric and distinct-set property suffixes a Count$
// zone body reads at this pin (engine/effects/count_body_zone.go).
func countPropOK(prop string) bool {
	switch prop {
	case "CardPower", "CardToughness", "CardManaCost", "CardTypes", "Colors",
		"GreatestCardPower", "GreatestCardManaCost", "GreatestCardToughness",
		"DifferentCardPower", "DifferentCardManaCost", "DifferentCardNames":
		return true
	}
	return false
}

// propValOf is a numeric property's value on a face.
func propValOf(prop string, fa *cards.Face) int {
	switch prop {
	case "CardPower", "GreatestCardPower", "DifferentCardPower":
		return fa.Power()
	case "CardToughness", "GreatestCardToughness":
		return fa.Toughness()
	case "CardManaCost", "GreatestCardManaCost", "DifferentCardManaCost":
		return int(fa.Cmc())
	}
	return 0
}

// propDistinctOf is a distinct-set property's key on a face.
func propDistinctOf(prop string, fa *cards.Face) (int, string, bool) {
	switch prop {
	case "Colors":
		return 0, faceColours(fa), true
	case "CardTypes":
		var kept []string
		for _, t := range fa.Types {
			if slices.Contains(coreCardTypes, t) {
				kept = append(kept, t)
			}
		}
		return 0, strings.Join(kept, ","), true
	case "DifferentCardNames":
		return 0, fa.Name, true
	case "DifferentCardPower", "DifferentCardManaCost":
		return propValOf(prop, fa), "", true
	}
	return 0, "", false
}

// zoneFold folds prop over the matched faces the way the engine's zone scan
// does: sums for the numeric properties, distinct sets for the rest,
// max for the Greatest family.
func zoneFold(prop string, matched []*cards.Face) int {
	if prop == "" {
		return len(matched)
	}
	switch prop {
	case "GreatestCardPower", "GreatestCardManaCost", "GreatestCardToughness":
		best := 0
		for _, fa := range matched {
			if v := propValOf(prop, fa); v > best {
				best = v
			}
		}
		return best
	case "Colors", "CardTypes", "DifferentCardNames", "DifferentCardPower", "DifferentCardManaCost":
		nums := map[int]bool{}
		strs := map[string]bool{}
		for _, fa := range matched {
			n, s, ok := propDistinctOf(prop, fa)
			if !ok {
				continue
			}
			if s != "" {
				strs[s] = true
			} else {
				nums[n] = true
			}
		}
		return len(nums) + len(strs)
	}
	sum := 0
	for _, fa := range matched {
		sum += propValOf(prop, fa)
	}
	return sum
}

// matchFaces is the placed cards the spec matches over the named zones,
// engine-side: only alternatives parseFilter understands, with the
// controller and Self mappings presentCount uses.
func matchFaces(reg *cards.Registry, f *StartFile, seat int, zones []string, alts []filterAlt, card string, source bool) []*cards.Face {
	var out []*cards.Face
	for _, z := range zones {
		if !zonePlaceable(z) {
			continue
		}
		for s := 0; s < 2; s++ {
			for _, name := range zoneCards(f, s, z) {
				c, ok := reg.Lookup(name)
				if !ok || len(c.Faces) == 0 {
					continue
				}
				fa := c.Faces[0]
				for _, a := range alts {
					if !a.known {
						continue
					}
					if a.self && !(source && z == "battlefield" && s == seat && name == card) {
						continue
					}
					if !ctrlOK(a.ctrl, s == seat) || !a.matches(fa, false) {
						continue
					}
					out = append(out, fa)
					break
				}
			}
		}
	}
	return out
}

// countZonesOf maps a Count$ zone head to the zone words it scans
// (engine/effects/count_zone_fold.go): "Valid" is the battlefield,
// "Valid<Zone>" the named one, "Valid<Zone>,<Zone>" the list. ok is false for
// a head the start does not read (ValidAll and unknown zone names).
func countZonesOf(head string) ([]string, bool) {
	rest, ok := strings.CutPrefix(head, "Valid")
	if !ok {
		return nil, false
	}
	if strings.TrimSpace(rest) == "" {
		return []string{"battlefield"}, true
	}
	var out []string
	for _, tok := range strings.Split(rest, ",") {
		switch strings.ToLower(strings.TrimSpace(tok)) {
		case "battlefield", "hand", "graveyard", "library", "exile", "stack", "command":
			out = append(out, strings.ToLower(strings.TrimSpace(tok)))
		default:
			return nil, false
		}
	}
	return out, true
}

// countBodyValue is my read of a Count$ body over the placed state, with
// evaluated false for a shape the pin does not model (the caller then leaves
// the start as it was, the AB gate's fail-open direction).
func countBodyValue(reg *cards.Registry, f *StartFile, seat int, card string, source bool, body string) (int, bool) {
	body = strings.TrimSpace(body)
	if rest, ok := strings.CutPrefix(body, "Count$"); ok {
		body = strings.TrimSpace(rest)
	}
	// dotted branch heads: <head>.<yes>.<no> reads <no> while the branch's
	// fact is false, and at the start's position no creature has died
	// (Morbid), no permanent has left (Revolt) and no one is the monarch.
	for _, h := range []string{"Morbid", "Revolt", "Monarch"} {
		rest, ok := strings.CutPrefix(body, h+".")
		if !ok {
			continue
		}
		yes, no, found := strings.Cut(rest, ".")
		_, err1 := strconv.Atoi(strings.TrimSpace(yes))
		nn, err2 := strconv.Atoi(strings.TrimSpace(no))
		if !found || err1 != nil || err2 != nil {
			return 0, false
		}
		return nn, true
	}
	switch body {
	case "AttackersDeclared":
		return 0, true // no combat at the start's step
	case "YourTurns":
		return 1, true // the focus seat's first turn is in progress
	case "PlayerCountPlayers":
		return 2, true
	case "PlayerCountOpponents":
		return 1, true
	}
	if rest, ok := strings.CutPrefix(body, "PlayerCountOpponents$"); ok {
		return playerCountOpponentsValue(reg, f, seat, card, source, strings.TrimSpace(rest))
	}
	head, arg, _ := strings.Cut(body, " ")
	arg = strings.TrimSpace(arg)
	zones, ok := countZonesOf(head)
	if !ok {
		return 0, false
	}
	spec, prop, hasProp := strings.Cut(arg, "$")
	if !hasProp {
		spec, prop = arg, ""
	} else {
		prop = strings.TrimSpace(prop)
	}
	if prop != "" && !countPropOK(prop) {
		// an unrecognised property keeps the whole token as the spec, which
		// then never matches: a decided zero.
		return 0, true
	}
	if strings.Contains(spec, "wasCastFrom") || strings.Contains(spec, "token$") {
		return 0, true // no casts, no tokens at the start's step
	}
	if strings.Contains(spec, "/") {
		return 0, false // a count op suffix this pin does not fold
	}
	alts, _ := specAlts(spec)
	matched := matchFaces(reg, f, seat, zones, alts, card, source)
	return zoneFold(prop, matched), true
}

// playerCountOpponentsValue is the PlayerCountOpponents$<Property> arm of
// countBodyValue over the placed state: the Highest/LowestValid zone-count
// extreme (engine/effects/count_player.go playerCountExtreme) — the extreme
// over the opponent seats of the count of the member's own cards the spec
// matches in the named zones. Any other property is unmodelled here (the
// scalar and HasProperty extremes the engine reads are not start-placable
// either way), and an unread zone word or a spec-less property keeps the
// head unresolvable, as the engine does.
func playerCountOpponentsValue(reg *cards.Registry, f *StartFile, seat int, card string, source bool, prop string) (int, bool) {
	zones, spec, ok := oppExtremeParts(prop)
	if !ok {
		return 0, false
	}
	n, ok := playerCountOpponentsSpecValue(reg, f, 1-seat, card, zones, spec)
	if !ok {
		return 0, false
	}
	return n, true // one opponent seat: the extreme is its count, highest or lowest alike
}

// oppExtremeParts splits a PlayerCountOpponents$ property
// ("HighestValid Land.YouCtrl", "LowestValidGraveyard Creature") into the
// zones it counts and its member-side spec; ok is false for a property this
// pin does not read.
func oppExtremeParts(prop string) (zones []string, spec string, ok bool) {
	prop = strings.TrimSpace(prop)
	rest, ok := strings.CutPrefix(prop, "Highest")
	if !ok {
		if rest, ok = strings.CutPrefix(prop, "Lowest"); !ok {
			return nil, "", false
		}
	}
	rest = strings.TrimPrefix(rest, "Valid")
	zonePart := rest
	if i := strings.IndexAny(rest, " $\t"); i >= 0 {
		zonePart, spec = rest[:i], strings.TrimSpace(rest[i+1:])
	}
	zones = []string{"battlefield"}
	if zonePart != "" {
		zones = nil
		for _, tok := range strings.Split(zonePart, ",") {
			w := strings.ToLower(strings.TrimSpace(tok))
			switch w {
			case "battlefield", "hand", "graveyard", "library", "exile", "stack", "command":
				zones = append(zones, w)
			default:
				return nil, "", false
			}
		}
	}
	if strings.TrimSpace(spec) == "" {
		return nil, "", false // the engine keeps a spec-less extreme unresolvable
	}
	return zones, spec, true
}

// playerCountOpponentsSpecValue is the per-member count of the zone-count
// extreme: the objects of one member's named zones the spec matches, read
// from the member's own perspective — a YouCtrl qualifier in the spec names
// the member (ctrlOK with mine false), and Self alternatives match nothing.
func playerCountOpponentsSpecValue(reg *cards.Registry, f *StartFile, oppSeat int, card string, zones []string, spec string) (int, bool) {
	alts, _ := specAlts(spec)
	n := 0
	for _, z := range zones {
		for _, name := range zoneCards(f, oppSeat, z) {
			c, ok := reg.Lookup(name)
			if !ok || len(c.Faces) == 0 {
				continue
			}
			fa := c.Faces[0]
			for _, a := range alts {
				if !a.known || a.self {
					continue
				}
				// the member's own cards: a YouCtrl qualifier names the member
				// (the opponent here), so its cards are "mine" to the spec
				if !ctrlOK(a.ctrl, true) || !a.matches(fa, false) {
					continue
				}
				n++
				break
			}
		}
	}
	return n, true
}

// sVarValue is the CheckSVar$ gate's value over the placed state, with
// evaluated false for a body or comparison this pin does not model.
func sVarValue(reg *cards.Registry, f *StartFile, seat int, card string, face *cards.Face, check string) (int, bool) {
	if check == "" {
		return 0, false
	}
	body, ok := face.SVars[check]
	if !ok {
		body = check // an inline expression
	}
	return countBodyValue(reg, f, seat, card, true, body)
}

// sVarGateRefusal mirrors sVarGateOK (the AB offer's CheckSVar$ gate) for the
// focus play: the value is read over the placed state, a shortfall a
// placement satisfies is placed to, a decided shortfall refuses the start by
// the head it read, and an unmodelled body leaves the start as it was (the
// offer gate fails open there).
func sVarGateRefusal(reg *cards.Registry, b *certdeck.Builder, card string, face *cards.Face, params map[string]string, f *StartFile, seat int) string {
	check := strings.TrimSpace(params["CheckSVar"])
	if check == "" {
		return ""
	}
	cmp := strings.TrimSpace(params["SVarCompare"])
	if cmp == "" {
		cmp = "GE1" // Forge's field default
	}
	op, n, lit := cmpParse(cmp)
	if !lit {
		return ""
	}
	body, ok := face.SVars[check]
	if !ok {
		body = check
	}
	body = strings.TrimSpace(body)
	if rest, ok := strings.CutPrefix(body, "Count$"); ok {
		body = strings.TrimSpace(rest) // the heads below name the bare body
	}
	val, evaluated := countBodyValue(reg, f, seat, card, true, body)
	if !evaluated {
		return ""
	}
	if cmpHolds(val, op, n) {
		return ""
	}
	if placed, ok := placeCountBody(reg, b, card, f, seat, true, body, op, n, val); ok && cmpHolds(val+placed, op, n) {
		return ""
	}
	// the decided heads: a value the start cannot change
	switch {
	case hasAnyPrefix(body, "Morbid."):
		return reasonMorbid
	case hasAnyPrefix(body, "Revolt."):
		return reasonRevolt
	case hasAnyPrefix(body, "Monarch."):
		return reasonMonarch
	case body == "AttackersDeclared":
		if f.Focus != nil && f.Focus.Attack != "" {
			return "" // the scripted combat declares attackers
		}
		return reasonAttackers
	case body == "YourTurns":
		return reasonYourTurns
	case body == "PlayerCountPlayers" || body == "PlayerCountOpponents" ||
		strings.HasPrefix(body, "PlayerCountOpponents$"):
		return reasonCountUnmet
	}
	return "" // a zone count the placement could not reach: undecided
}

// hasAnyPrefix reports whether s starts with one of the prefixes.
func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// placeOppExtreme places the opponent-side cards a failed
// PlayerCountOpponents$Highest/Lowest comparison needs: the opponent is the
// one member, so its count is the extreme either way, and the spec is read
// from its side (a YouCtrl qualifier names the opponent). It returns the
// value added and whether the property is one it reads.
func placeOppExtreme(reg *cards.Registry, b *certdeck.Builder, card string, f *StartFile, seat int, prop, op string, n, val int) (int, bool) {
	zones, spec, ok := oppExtremeParts(prop)
	if !ok {
		return 0, false
	}
	need := n
	if op == "GT" {
		need = n + 1
	}
	deficit := need - val
	if deficit <= 0 || deficit > 8 {
		return 0, true
	}
	alts, raws := specAlts(spec)
	for i := range alts {
		alts[i].ctrl = ctrlAny // the member's own cards: the seat is forced below
	}
	return placePresent(reg, b, card, f, alts, raws, zones, 1-seat, deficit), true
}

// presentCompareOf is a gate's PresentCompare$ literal, "GE1" (at least one,
// the engine's and Forge's default) when absent.
func presentCompareOf(params map[string]string) string {
	if cmp := strings.TrimSpace(params["PresentCompare"]); cmp != "" {
		return cmp
	}
	return "GE1"
}

// placeCountBody places the state a failed CheckSVar$ comparison needs. It
// returns the value added and whether the body is a zone count it read
// (dotted and scalar heads place nothing).
func placeCountBody(reg *cards.Registry, b *certdeck.Builder, card string, f *StartFile, seat int, source bool, body, op string, n, val int) (added int, ok bool) {
	body = strings.TrimSpace(body)
	if rest, ok := strings.CutPrefix(body, "Count$"); ok {
		body = strings.TrimSpace(rest)
	}
	if hasAnyPrefix(body, "Morbid.", "Revolt.", "Monarch.") ||
		body == "AttackersDeclared" || body == "YourTurns" ||
		body == "PlayerCountPlayers" || body == "PlayerCountOpponents" {
		return 0, false
	}
	if rest, ok := strings.CutPrefix(body, "PlayerCountOpponents$"); ok {
		return placeOppExtreme(reg, b, card, f, seat, strings.TrimSpace(rest), op, n, val)
	}
	head, arg, _ := strings.Cut(body, " ")
	arg = strings.TrimSpace(arg)
	zones, zok := countZonesOf(head)
	if !zok {
		return 0, false
	}
	spec, prop, hasProp := strings.Cut(arg, "$")
	if !hasProp {
		spec, prop = arg, ""
	} else {
		prop = strings.TrimSpace(prop)
	}
	if prop != "" && !countPropOK(prop) {
		return 0, false
	}
	need := n
	if op == "GT" {
		need = n + 1
	}
	if need <= 0 {
		need = 1
	}
	deficit := need - val
	if deficit <= 0 {
		return 0, true
	}
	if deficit > 8 {
		return 0, true // bound the placements; the caller leaves the start
	}
	alts, raws := specAlts(spec)
	zone := "battlefield"
	for _, z := range zones {
		if zonePlaceable(z) {
			zone = z
			break
		}
	}
	// the side an alternative's controller requirement names
	tseat := seat
	for _, a := range alts {
		if !a.known || a.self {
			continue
		}
		if a.ctrl == ctrlOpp {
			tseat = 1 - seat
		}
		break
	}
	switch {
	case prop == "":
		placed := placePresent(reg, b, card, f, alts, raws, []string{zone}, seat, deficit)
		return placed, true
	case hasAnyPrefix(prop, "Greatest"):
		// an object whose value reaches the bound
		name := pickFixture(reg, b, card, func(g *cards.Face) bool {
			return altFaceMatch(alts, g) && propValOf(prop, g) >= need
		}, nil, true)
		if name == "" && zone != "battlefield" {
			name = pickZoneFixture(reg, b, card, func(g *cards.Face) bool {
				return altFaceMatch(alts, g) && propValOf(prop, g) >= need
			}, nil, true)
		}
		if name == "" {
			return 0, true
		}
		v := propValOf(prop, faceByName(reg, name))
		zonePut(f, tseat, zone, name)
		return v, true
	case hasAnyPrefix(prop, "Different"), prop == "Colors" || prop == "CardTypes":
		// distinct values: place one object at a time whose key is new
		added := 0
		alts2, _ := specAlts(spec)
		for added < deficit {
			matched := matchFaces(reg, f, seat, []string{zone}, alts2, card, source)
			nseen := map[int]bool{}
			sseen := map[string]bool{}
			for _, g := range matched {
				if nu, s, ok2 := propDistinctOf(prop, g); ok2 {
					if s != "" {
						sseen[s] = true
					} else {
						nseen[nu] = true
					}
				}
			}
			name := pickFixture(reg, b, card, func(g *cards.Face) bool {
				nu, s, ok2 := propDistinctOf(prop, g)
				return altFaceMatch(alts2, g) && ok2 &&
					(s != "" && !sseen[s] || s == "" && !nseen[nu])
			}, nil, true)
			if name == "" && zone != "battlefield" {
				name = pickZoneFixture(reg, b, card, func(g *cards.Face) bool {
					nu, s, ok2 := propDistinctOf(prop, g)
					return altFaceMatch(alts2, g) && ok2 &&
						(s != "" && !sseen[s] || s == "" && !nseen[nu])
				}, nil, true)
			}
			if name == "" {
				return added, true
			}
			zonePut(f, tseat, zone, name)
			added++
		}
		return added, true
	default:
		// the sum properties: one object at or above the remaining deficit,
		// else any matching object with a positive value, until it holds
		added := 0
		for i := 0; i < 8 && !cmpHolds(val+added, op, n); i++ {
			rem := need - val - added
			name := pickFixture(reg, b, card, func(g *cards.Face) bool {
				return altFaceMatch(alts, g) && propValOf(prop, g) >= rem
			}, nil, true)
			if name == "" {
				name = pickFixture(reg, b, card, func(g *cards.Face) bool {
					return altFaceMatch(alts, g) && propValOf(prop, g) > 0
				}, nil, true)
			}
			if name == "" && zone != "battlefield" {
				name = pickZoneFixture(reg, b, card, func(g *cards.Face) bool {
					return altFaceMatch(alts, g) && propValOf(prop, g) > 0
				}, nil, true)
			}
			if name == "" {
				return added, true
			}
			added += propValOf(prop, faceByName(reg, name))
			zonePut(f, tseat, zone, name)
		}
		return added, true
	}
}

// faceByName is a registry face by card name, nil when absent.
func faceByName(reg *cards.Registry, name string) *cards.Face {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return nil
	}
	return c.Faces[0]
}

// altFaceMatch reports whether any known non-self alternative's face
// requirements match the face. The alternative's controller requirement is
// not part of it: it only names the side the object sits on, which the
// callers place separately.
func altFaceMatch(alts []filterAlt, fa *cards.Face) bool {
	for _, a := range alts {
		if !a.known || a.self {
			continue
		}
		if a.matches(fa, false) {
			return true
		}
	}
	return false
}

// presentGateRead is the IsPresent$/PresentZone$/PresentCompare$ gate over
// the placed state without placing anything: holds is the gate's answer and
// decided whether this pin can read it. An unread PresentZone$ word is a
// decided not-holds (the engine's gate fails closed there); a comparison the
// pin cannot fold into a literal is undecided, except for an upper bound the
// placed count already breaks (the engine's count is mine or more).
func presentGateRead(reg *cards.Registry, f *StartFile, seat int, params map[string]string, card string, source bool) (holds, decided bool) {
	spec := strings.TrimSpace(params["IsPresent"])
	if spec == "" {
		return true, true
	}
	zones, known := parsePresentZones(strings.TrimSpace(params["PresentZone"]))
	if !known {
		return false, true
	}
	alts, _ := specAlts(spec)
	op, n, lit := cmpParse(presentCompareOf(params))
	my := presentCount(reg, f, seat, zones, alts, card, source)
	if !lit {
		if litOK(op) && breaksUpper(my, op, n) {
			return false, true
		}
		return false, false
	}
	return cmpHolds(my, op, n), true
}

// cantBeCastBinds reports whether one printed CantBeCast static of the cast
// card's own face restricts the cast at the start's step. binds: the
// static's gates hold and its ValidCard$ matches the face. ok: this pin read
// the static well enough to answer — an unmodelled gate (an unread present
// comparison, an unmodelled CheckSVar body, an Affected$/ClassBand$/Origin$
// family the pin does not read) leaves the static undecided and the caller
// skips it, which is the engine's fail-closed direction for a static gate
// (the gate not holding withholds the restriction, so the cast is allowed).
func cantBeCastBinds(reg *cards.Registry, f *StartFile, seat int, card string, face *cards.Face, params map[string]string) (binds, ok bool) {
	switch strings.TrimSpace(params["Condition"]) {
	case "":
		// condBlank: the gate always holds
	case "PlayerTurn":
		if f.Active != seat {
			return false, true
		}
	default:
		// NotPlayerTurn (not the focus seat's turn) and every other
		// condition value restrictionGateHolds's switch answers false to
		return false, true
	}
	if strings.TrimSpace(params["ClassBand"]) != "" ||
		strings.TrimSpace(params["Affected"]) != "" ||
		strings.TrimSpace(params["NumLimitEachTurn"]) != "" ||
		strings.TrimSpace(params["SorceryTimingExempt"]) != "" ||
		strings.TrimSpace(params["Origin"]) != "" ||
		strings.TrimSpace(params["PresentPlayer"]) != "" {
		return false, false
	}
	if az, ok := params["AffectedZone"]; ok {
		z := strings.TrimSpace(az)
		if z != "" && !strings.EqualFold(z, "Hand") {
			return false, true // the zone word does not admit the hand
		}
	}
	if ph := strings.TrimSpace(params["Phases"]); ph != "" {
		set, unknown := state.ParsePhases(ph)
		if len(unknown) > 0 || set.Empty() || !set.Has(state.StepMain1) {
			return false, true // the window is not open at the start's step
		}
	}
	if holds, decided := presentGateRead(reg, f, seat, params, card, false); !decided || !holds {
		return false, decided
	}
	if check := strings.TrimSpace(params["CheckSVar"]); check != "" {
		cmp := strings.TrimSpace(params["SVarCompare"])
		if cmp == "" {
			cmp = "GE1" // Forge's field default
		}
		op, n, lit := cmpParse(cmp)
		if !lit {
			return false, false
		}
		body, found := face.SVars[check]
		if !found {
			body = check // an inline expression
		}
		val, evaluated := countBodyValue(reg, f, seat, card, false, body)
		if !evaluated || !cmpHolds(val, op, n) {
			return false, true
		}
	}
	spec := strings.TrimSpace(params["ValidCard"])
	if spec == "" {
		spec = "Card" // castValidCardSpec's absent read
	}
	alts, _ := specAlts(spec)
	for _, a := range alts {
		if !a.known {
			continue // an unread alternative never matches, the engine's read
		}
		if a.self || ctrlOK(a.ctrl, true) && a.matches(face, false) {
			return true, true
		}
	}
	return false, true
}

// cantBeCastRefusal is why the cast card's own CantBeCast statics keep the
// cast from being legal at the start's step: a static whose gates hold and
// whose ValidCard$ matches the card blocks the cast (start:cantbecast).
// Statics this pin does not model are skipped, the direction the engine's
// static gate takes when its own gates are unreadable.
func cantBeCastRefusal(reg *cards.Registry, b *certdeck.Builder, card string, face *cards.Face, f *StartFile, seat int) string {
	for i := range face.Statics {
		if face.Statics[i].Mode != "CantBeCast" {
			continue
		}
		if binds, ok := cantBeCastBinds(reg, f, seat, card, face, face.Statics[i].Params); ok && binds {
			return reasonCantBeCast
		}
	}
	return ""
}
