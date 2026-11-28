package scen

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Where an activated ability's card must be for its cost: an ability whose
// cost discards the card (cycling, channel) is activated from hand, one whose
// cost exiles it from the graveyard (scavenge, embalm) from the graveyard.
const (
	zoneBattlefield = "battlefield"
	zoneHand        = "hand"
	zoneGraveyard   = "graveyard"
)

// costArgs splits a cost token's <...> argument: "Sac<1/Creature.Other/another
// creature>" is "Sac" and [1 Creature.Other another creature].
func costArgs(tok string) (head string, args []string) {
	i := strings.IndexByte(tok, '<')
	if i < 0 || !strings.HasSuffix(tok, ">") {
		return tok, nil
	}
	return tok[:i], strings.Split(tok[i+1:len(tok)-1], "/")
}

// costPlan is what a start places to pay an activated ability's cost on
// seat 0 (the forced start: the cost's prerequisites are start state, never
// a play).
type costPlan struct {
	mana      []string     // mana tokens: one basic each on the battlefield
	zone      string       // where the card sits (zoneBattlefield, zoneHand, zoneGraveyard)
	field     []string     // permanents the cost sacrifices, returns or taps
	tapped    []string     // of field, those placed tapped (lands: they add no mana)
	hand      []string     // cards the cost discards
	graveyard []string     // cards the cost exiles from the graveyard
	tokens    []StartToken // tokens the cost spends (Sac<N/Kind> of a token kind), minted on the battlefield
	// bearer is the creature the card's own attachment reads (Sac<Card.AttachedBy>,
	// "sacrifice enchanted creature"): the Aura the ability is on is placed
	// attached to it (StartAttach), and the cost sacrifices it. bearerSeat is
	// the seat that must control it.
	bearer     string
	bearerSeat int
	counters   []StartCounter
	// playerCounters seed the seat itself (Card empty): the energy a
	// PayEnergy<N> spends before the focus play.
	playerCounters []StartCounter
	life           int32 // the life a PayLife<N> needs: N+1 when N is 20 or more (0: the default 20)
	// attacker, when set, is the filter (its attacking and unblocked
	// properties stripped) of the one unblocked attacker the cost returns
	// (ninjutsu): the cost is payable only inside a combat line, which only a
	// focus ability (abilityFocus) scripts; planCost refuses it elsewhere.
	attacker string
}

// planCost reads cost for the card's ability: every token a start can meet
// with placed state, or the refusal of the first it cannot
// (steps-needed:cost:<head>, as costTokens names it).
//
//   - Sac<N/Type>, Return<N/Type>: N fodder permanents of Type (fodder); of a
//     token kind (Treasure, Permanent.token), a Sac mints its tokens
//     (mintSac). An Exile<N/Type> is planned only as a spell's additional
//     cost (planCastCost): an activated ability's exile shape does not lower
//     from a placed start, so it keeps its refusal;
//   - tapXType<N/Type>: N untapped fodder creatures; tapXType<Any/...
//     withTotalPowerGEn> (crew n) one vanilla creature with power n or more;
//     of a token kind (Baylen's Permanent.token) N minted tokens, untapped
//     (summoning sickness never guards a cost's tap);
//   - Discard<1/CARDNAME>: the card in hand; Discard<N/Type>: N fodder cards
//     in hand;
//   - ExileFromGrave<1/CARDNAME>: the card in the graveyard;
//     ExileFromGrave<N/Type>: N fodder cards in the graveyard;
//   - SubCounter<N/Kind>: N counters of Kind on the card (a planeswalker's
//     LOYALTY: only those its printed loyalty lacks); SubCounter<N/Kind/NICKNAME>
//     or SubCounter<N/Kind/CARDNAME> (Migloz's OIL): the same, the counter of
//     a personal kind, planned only when the start gate admits the card on
//     the battlefield (lower.StartCardRefusal: Rasputin Dreamweaver's
//     mandatory upkeep keeps its refusal); SubCounter<N/Kind/Spec/...> (Ion
//     Storm's P1P1/Permanent): N counters of Kind on a placed inert permanent
//     of Spec (counterFodder); AddCounter<N/Kind> on the card needs nothing;
//   - PayEnergy<N>: N ENERGY counters on the seat itself, recorded in
//     PlayerCounters (a start position can hold the energy the ability
//     spends). A malformed or non-positive amount stays refused.
func planCost(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, cost, generic string) (costPlan, string) {
	return planCostMode(reg, b, face, card, cost, generic, false)
}

// planCastCost is planCost for the additional cost of a spell being cast: the
// only shape whose Exile<N/Type> a placed start lowers (Fear of Abduction,
// Soul Exchange, Necrotic Fumes). An activated ability's Exile costs keep
// their refusal: Food Chain's mana ability's colour ask and The Soul Stone's
// harness (whose graveyard-return trigger is an upkeep trigger) reach no item
// from a placed start.
func planCastCost(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, cost, generic string) (costPlan, string) {
	return planCostMode(reg, b, face, card, cost, generic, true)
}

func planCostMode(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, cost, generic string, cast bool) (costPlan, string) {
	p, why := planCostLine(reg, b, face, card, cost, generic, cast)
	if why == "" && p.attacker != "" {
		return p, "steps-needed:cost:Return"
	}
	return p, why
}

// unblockedAttackerFilter is the filter a Return cost names when it returns an
// unblocked attacker ("Creature.YouCtrl+attacking+unblocked": ninjutsu) with
// those two properties stripped, and whether the filter names both.
func unblockedAttackerFilter(spec string) (string, bool) {
	head, props, _ := strings.Cut(spec, ".")
	var keep []string
	var atk, unb bool
	for _, p := range strings.Split(props, "+") {
		switch strings.ToLower(p) {
		case "attacking":
			atk = true
		case "unblocked":
			unb = true
		default:
			keep = append(keep, p)
		}
	}
	if !atk || !unb {
		return "", false
	}
	if len(keep) == 0 {
		return head, true
	}
	return head + "." + strings.Join(keep, "+"), true
}

// planCostLine is planCost that also reads a Return of an unblocked attacker
// (costPlan.attacker): the caller must script the combat line it needs. cast
// enables the Exile<N/Type> arm, whose start only a spell's additional cost
// lowers.
func planCostLine(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, cost, generic string, cast bool) (costPlan, string) {
	p := costPlan{zone: zoneBattlefield}
	for _, tok := range splitCost(cost) {
		if manaTokenRE.MatchString(tok) {
			p.mana = append(p.mana, tok)
			continue
		}
		if tok == "T" || tok == "Q" {
			continue
		}
		if payLifeRE.MatchString(tok) {
			// PayLife<50> is unpayable from 20 life: the seat starts above it
			if n, _ := strconv.Atoi(tok[len("PayLife<") : len(tok)-1]); n >= 20 && int32(n)+1 > p.life {
				p.life = int32(n) + 1
			}
			continue
		}
		head, args := costArgs(tok)
		refusal := "steps-needed:cost:" + head
		if head == "PayEnergy" {
			// PayEnergy<N> has one argument, not the <N/Type> pair the other
			// cases read, so it is parsed before the generic N. Only a
			// positive whole amount is a cost a start can cover.
			if len(args) != 1 {
				return p, refusal
			}
			amount, err := strconv.Atoi(args[0])
			if err != nil || amount < 1 {
				return p, refusal
			}
			p.playerCounters = addPlayerCounter(p.playerCounters, "ENERGY", int32(amount))
			continue
		}
		n := 0
		if len(args) >= 2 {
			var err error
			// the budget of four placements holds for every head but SubCounter
			// (a loyalty or counter total) and a Sac or Return, whose arm
			// caps its own count (tokens beyond four, cards at four)
			if n, err = strconv.Atoi(args[0]); err != nil || n < 0 || n > 4 && head != "SubCounter" && head != "Sac" && head != "Return" {
				if !(head == "tapXType" && args[0] == "Any") {
					return p, refusal
				}
			}
		}
		self := len(args) >= 2 && (args[1] == "CARDNAME" || args[1] == "NICKNAME")
		if head == "Return" && len(args) >= 2 && !self && n == 1 && p.attacker == "" {
			if spec, ok := unblockedAttackerFilter(strings.Join(args[1:], "/")); ok {
				p.attacker = spec
				continue
			}
		}
		if len(args) >= 2 && args[1] == "Hand" {
			// Discard<N/Hand>: discard the hand, N cards of it
			args[1] = "Card"
		}
		switch {
		case head == "Discard" && n == 0 && len(args) >= 2:
		case (head == "Sac" || head == "Return" || head == "Exile") && self && n == 1:
		case head == "Discard" && self && n == 1:
			p.zone = zoneHand
		case head == "ExileFromGrave" && self && n == 1:
			p.zone = zoneGraveyard
		case (head == "Sac" || head == "Return") && len(args) >= 2 && !self:
			if _, ok := tokenFodder(reg, args[1]); ok {
				// a token kind ("Treasure", "Prism.token", "Permanent.token"):
				// the cost mints its tokens, a vanilla permanent no card
				// is (a Food card is busier than the Food token). Only a Sac
				// spends it: a token returned to hand is no cost to meet.
				if head != "Sac" || !p.mintSac(reg, args[1], n) {
					return p, refusal
				}
				continue
			}
			if head == "Sac" && args[1] == "Card.AttachedBy" {
				// "Sacrifice enchanted creature": the bearer the Aura the
				// ability is on is placed attached to (applyCost), not a
				// permanent of its own. You can sacrifice only what you
				// control, so the bearer must be on the card's side.
				bearer, bs := attachBearer(reg, b, card, face)
				if bearer == "" || bs == 1 {
					return p, refusal
				}
				p.bearer, p.bearerSeat = bearer, bs
				continue
			}
			if n > 4 {
				return p, refusal
			}
			name, tapped := sacFodder(reg, b, card, args[1], generic)
			if name == "" {
				return p, refusal
			}
			for range n {
				p.field = append(p.field, name)
				if tapped {
					p.tapped = append(p.tapped, name)
				}
			}
		case head == "Exile" && cast && len(args) >= 2 && !self:
			// "Exile a <Type> you control" as a spell's additional cost (Fear
			// of Abduction's Exile<1/Creature>): the same placed permanent a
			// Sac spends, paid by exiling instead of sacrificing. A token kind
			// mints its tokens (a permanent no card is), like a Sac of a token
			// kind. An activated ability's Exile cost is not planned: no placed
			// start lowers Food Chain's colour ask or The Soul Stone's harness
			// (an upkeep trigger), so it keeps steps-needed:cost:Exile.
			if _, ok := tokenFodder(reg, args[1]); ok {
				if !p.mintSac(reg, args[1], n) {
					return p, refusal
				}
				continue
			}
			name, tapped := sacFodder(reg, b, card, args[1], generic)
			if name == "" {
				return p, refusal
			}
			for range n {
				p.field = append(p.field, name)
				if tapped {
					p.tapped = append(p.tapped, name)
				}
			}
		case head == "tapXType" && len(args) >= 2 && !self && n <= 4:
			typ := args[1]
			if args[0] == "Any" {
				// crew n: Creature.Other+withTotalPowerGEn is one creature with
				// power n or more
				before, power, ok := strings.Cut(typ, "+withTotalPowerGE")
				if !ok {
					return p, refusal
				}
				typ, n = before+"+powerGE"+power, 1
			}
			if _, ok := tokenFodder(reg, typ); ok {
				// "Tap N untapped tokens" (Baylen's tapXType<3/Permanent.token/
				// token>): the start mints them, like a Sac of a token kind.
				// They sit untapped: a cost may tap any permanent, summoning
				// sickness guards activations and attacks, never a cost's tap.
				if !p.mintSac(reg, typ, n) {
					return p, refusal
				}
				continue
			}
			name, land := fodder(reg, b, card, typ, generic, true)
			if name == "" || land {
				return p, refusal
			}
			for range n {
				p.field = append(p.field, name)
			}
		case head == "Discard" && len(args) >= 2 && !self && n >= 1 && n <= 4:
			name, _ := fodder(reg, b, card, args[1], generic, false)
			if name == "" {
				return p, refusal
			}
			for range n {
				p.hand = append(p.hand, name)
			}
		case head == "ExileFromGrave" && len(args) >= 2 && !self && n >= 1 && n <= 4:
			name, _ := fodder(reg, b, card, args[1], generic, false)
			if name == "" {
				return p, refusal
			}
			for range n {
				p.graveyard = append(p.graveyard, name)
			}
		case head == "SubCounter" && len(args) >= 2 && n >= 1:
			kind := strings.ToUpper(args[1])
			if !placeableCounterKind(kind) {
				return p, refusal // a wildcard kind names kinds, not one to place
			}
			personal := len(args) < 3 || args[2] == "NICKNAME" || args[2] == "CARDNAME"
			if !cast && personal {
				// A personal kind's counters ride the card itself, so the
				// focus ability's start must place it on the battlefield. A
				// card the lowering's start gate refuses there cannot be
				// placed (Rasputin Dreamweaver: its upkeep trigger is
				// mandatory, and its startedTheTurnUntapped filter word makes
				// the turn-begin stamp live on every untapped permanent,
				// its own and the start's lands): keep the cost refusal
				// rather than emit a start play writes no item for. The gate
				// reads the card, not a hand of shapes, so the next refused
				// field or trigger cannot slip a new arm past this.
				if c, ok := reg.Lookup(card); !ok || lower.StartCardRefusal(c, face) != "" {
					return p, refusal
				}
			}
			if !cast && entryCounterOther(face) {
				// The card's own entry replacement puts counters on other
				// permanents (Sage of Fables's other Wizards): when the focus
				// is its battlefield ability, the placement re-runs it on
				// every permanent placed after the card, and no start field
				// can subtract the extra (lower's setup_entry_counter_other).
				// Keep the cost refusal rather than turn it into an
				// unlowerable start. A cast cost's card is in hand and enters
				// only on the line, so its replacement is not part of setup.
				return p, refusal
			}
			need := int32(n)
			bearer := card
			if len(args) >= 3 && args[2] != "NICKNAME" && args[2] != "CARDNAME" {
				// "remove a <Kind> counter from a <Spec> you control" (Ion
				// Storm's SubCounter<1/P1P1/Permanent/...>): the counters ride
				// an inert permanent of Spec the start places. counterFodder
				// picks a name the battlefield holds once and a body that
				// survives the counters: the lowering binds a counters entry
				// by card name (setup_by_name:counters), so a basic land the
				// mana already duplicates or a 1/1 a -1/-1 counter kills is no
				// bearer. A NICKNAME or CARDNAME third argument (Migloz's OIL)
				// is the personal kind: the same planCost reads for the card
				// itself.
				name, ok := counterFodder(reg, b, card, args[2], kind, n)
				if !ok {
					return p, refusal
				}
				p.field = append(p.field, name)
				bearer = name
			}
			if bearer == card && kind == "LOYALTY" {
				if l, err := strconv.Atoi(face.Loyalty); err == nil {
					need -= int32(l)
				}
			}
			if need > 0 {
				p.counters = append(p.counters, StartCounter{Card: bearer, Kind: kind, N: need})
			}
		case head == "Waterbend" && len(args) == 1:
			// "waterbend {N}": N more basics — landsFor expands the bare
			// number token into that many lands of the start's colour
			if _, err := strconv.Atoi(args[0]); err != nil {
				return p, refusal
			}
			p.mana = append(p.mana, args[0])
		case (head == "Behold" || head == "BeholdExile") && len(args) >= 2 && n <= 4:
			// "behold a <Spec>": the revealed cards stay in the caster's hand
			// (the engine's beheldCostAsk takes hand candidates too; a
			// BeholdExile's ThenExile mark does not move the ask)
			spec := strings.Join(args[1:], "/")
			var test func(*cards.Face) bool
			known := false
			for _, a := range parseFilter(spec) {
				if a.known && !a.self && a.ctrl != ctrlOpp {
					test, known = a.test, true
					break
				}
			}
			if !known {
				return p, refusal // a spec the engine's filter matches nothing of
			}
			name := pickZoneFixture(reg, b, card, test, nil, true)
			if name == "" {
				return p, refusal
			}
			for range n {
				p.hand = append(p.hand, name)
			}
		case head == "AddCounter" && len(args) == 2:
		default:
			return p, refusal
		}
	}
	return p, ""
}

// fodder is the card a cost spends of a Forge cost type ("Creature.Other",
// "Artifact;Creature", "Land"), and whether it is a basic land: a basic of
// the card's colour for a type a land meets (never for creatures, which
// tapXType taps), else the most inert carrier (carrierPool) the type takes.
// "" when none: a token type (Treasure, Food, Permanent.token) has no card.
func fodder(reg *cards.Registry, b *certdeck.Builder, card, typ, generic string, creatures bool) (string, bool) {
	filter := strings.ReplaceAll(typ, ";", ",")
	if !creatures {
		if land, _ := landCarrier(reg, filter, generic); land != "" {
			return land, true
		}
	}
	name, _ := filterCarrier(reg, b, card, filter, func(f *cards.Face) bool {
		return f.IsPermanent() && (!creatures || f.IsCreature())
	})
	return name, false
}

// sacFodder is the permanent a Sac or Return cost spends: fodder's pick when
// the inert carrier pool meets the spec, else the least busy playable
// permanent (pickFixture, or a nonbasic land for a spec no non-land card
// takes, a snow Mountain) some understood, non-Self, own-side alternative of
// the spec takes. It reports the alternative's tapped requirement (a
// Sac<Creature.tapped> is placed tapped); a permanent you do not control is
// not a cost you can pay, so an opponent-side alternative keeps the refusal.
// "" when no placement meets the spec (a named card the corpus does not hold,
// a counter-gated creature, a FieldBlock card).
func sacFodder(reg *cards.Registry, b *certdeck.Builder, card, typ, generic string) (name string, tapped bool) {
	if name, land := fodder(reg, b, card, typ, generic, false); name != "" {
		return name, land
	}
	for _, a := range parseFilterPlaced(typ) {
		if !a.known || a.self || a.ctrl == ctrlOpp {
			continue
		}
		if n := pickFixture(reg, b, card, a.test, nil, true); n != "" {
			return n, a.tapped > 0
		}
		if n := pickLandFixture(reg, b, a.test); n != "" {
			// tapped unless the filter wants it untapped: a land adds no
			// mana to the start (the focus plays stay the only affordable ones)
			return n, a.tapped >= 0
		}
	}
	return "", false
}

// counterFodder is the permanent a SubCounter cost places its counters on when
// the cost names a spec ("remove a +1/+1 counter from a creature you control"):
// the corpus's inert bearers (Wall of Stone) first, then any inert carrier, an
// assumed-untapped permanent an understood own-side alternative of the spec
// takes that survives the counter kind and whose entry counter state setup can
// restate. It is never a basic land: the start's mana lands carry that name,
// and the lowering binds a counters entry by card name
// (setup_by_name:counters), so only a name the battlefield holds once is a
// safe bearer. "" when nothing meets the spec, and the cost keeps its refusal.
func counterFodder(reg *cards.Registry, b *certdeck.Builder, card, spec, kind string, n int) (string, bool) {
	filter := strings.ReplaceAll(spec, ";", ",")
	for _, a := range parseFilterPlaced(filter) {
		if !a.known || a.self || a.ctrl == ctrlOpp {
			continue
		}
		ok := func(f *cards.Face) bool {
			return f.IsPermanent() && f.Name != card && !deadOnArrival(f) && a.matches(f, false) &&
				survivesPlacedCounters(f, kind, n) && !entryCounterUnrestatable(f) && !entryCounterOther(f)
		}
		for _, name := range bearers {
			c, found := reg.Lookup(name)
			if !found || len(c.Faces) == 0 || !b.Playable(c) || !ok(c.Faces[0]) {
				continue
			}
			return name, true
		}
		if name := pickCarrier(reg, b, card, ok); name != "" {
			return name, true
		}
	}
	return "", false
}

// survivesPlacedCounters reports whether a placed permanent survives the n
// counters of kind a cost puts on it. Only a -1/-1 counter lowers toughness
// (Forge's "M1M1"; CR 122.1b), so a creature must have base toughness above n
// times the reduction; every other kind a cost removes (P1P1, CHARGE, TIME,
// OIL) or a noncreature bearer survives.
func survivesPlacedCounters(f *cards.Face, kind string, n int) bool {
	red, minus := minusCounterToughness(kind)
	if !minus || !f.IsCreature() {
		return true
	}
	return f.Toughness() > n*red
}

// minusCounterToughness reads Forge's -N/-N counter kind (M1M1) and returns
// the toughness it removes, or false for every other kind.
func minusCounterToughness(kind string) (int, bool) {
	rest, ok := strings.CutPrefix(kind, "M")
	if !ok {
		return 0, false
	}
	i := strings.IndexByte(rest, 'M')
	if i <= 0 || i == len(rest)-1 {
		return 0, false
	}
	if _, err := strconv.Atoi(rest[:i]); err != nil {
		return 0, false
	}
	tough, err := strconv.Atoi(rest[i+1:])
	if err != nil || tough < 0 {
		return 0, false
	}
	return tough, true
}

// maxSacTokens is the most tokens one Sac cost mints (Kellogg, Dangerous Mind
// sacrifices five Treasures); a card count keeps the budget of four.
const maxSacTokens = 8

// mintSac adds the n tokens a Sac or Return spec of a token kind spends to the
// plan. False when the spec names no token the start can mint (a kind the XMage
// setup table lacks, no token kind at all, more than maxSacTokens).
func (p *costPlan) mintSac(reg *cards.Registry, typ string, n int) bool {
	script, ok := tokenFodder(reg, typ)
	if !ok || script == "" || n > maxSacTokens {
		return false
	}
	for range n {
		p.tokens = append(p.tokens, StartToken{Script: script})
	}
	return true
}

// tokenFodder is the token script a Sac or Return spec of a token kind mints,
// and whether the spec names tokens at all: ok false when the spec may name a
// card and the card scan runs; ok true with ""
// when it names a token kind no start can mint (the token script the XMage
// setup table lacks), so the refusal stands; the script when it does.
//
//   - "<Kind>.token" is a token of Kind ("Prism.token"); "Permanent.token",
//     "Card.token" and a bare ".token" are any token, the corpus's Servo.
//   - a bare kind ("Treasure", "Food", "Goblin") names a token only when the
//     corpus mints one (that token, not a card of the kind, is the vanilla
//     permanent), so a kind with no token definition falls to the card scan
//     and, with no card either, keeps the refusal. A kind that is
//     not a single type word ("Creature.Other", a dotted spec) is no token
//     kind.
func tokenFodder(reg *cards.Registry, typ string) (string, bool) {
	if kind, tail, found := strings.Cut(typ, "."); found && tail == "token" {
		switch kind {
		case "Permanent", "Card", "":
			kind = ""
		}
		script, _ := tokenScript(reg, kind)
		// the script must be a token of the named kind: tokenScript also
		// matches a key that merely contains it ("Rat" in "pirate")
		if script != "" && kind != "" {
			if c := reg.Tokens[script]; c == nil || len(c.Faces) == 0 || !c.Faces[0].HasTypeWord(kind) {
				script = ""
			}
		}
		return script, true
	}
	switch typ {
	case "", "Permanent", "Card", "Spell":
		return "", false
	}
	if !typeWord(typ) {
		return "", false
	}
	script, ok := tokenScript(reg, typ)
	if ok {
		// The token definition must be of the kind the cost names (its face
		// carries the type word): tokenScript also matches a script whose
		// key merely contains the kind ("c_a_lander_sac_search" for "Land"),
		// and a Lander is no land.
		if c := reg.Tokens[script]; c == nil || len(c.Faces) == 0 || !c.Faces[0].HasTypeWord(typ) {
			return "", false
		}
		return script, true
	}
	return "", false
}

// applyCost places the card in the zone its cost reads and the cost plan
// around it on seat 0.
func applyCost(p costPlan, card, generic string, f *StartFile) {
	s := &f.Setup.P0
	switch p.zone {
	case zoneHand:
		s.Hand = append(s.Hand, card)
	case zoneGraveyard:
		s.Graveyard = append(s.Graveyard, card)
	default:
		s.Battlefield = append(s.Battlefield, card)
		if p.bearer != "" {
			// The cost reads the card's own enchanted permanent
			// (Sac<Card.AttachedBy>): the card enters attached to the bearer
			// the plan picked (attachBearer), and the cost sacrifices it.
			placeBearer(&f.Setup.P0, &f.Setup.P1, card, p.bearer, p.bearerSeat)
		}
	}
	applyCostSupport(p, generic, f)
}

// applyCostSupport places the state a cost plan needs around the card that pays
// it, without placing the card itself, on seat 0: applyCost uses it for an
// ability the card activates, and a granted ability uses it for the carrier the
// ability was granted to (already on the battlefield).
func applyCostSupport(p costPlan, generic string, f *StartFile) {
	applyCostSupportOn(p, generic, f, &f.Setup.P0)
}

// applyCostSupportOn is applyCostSupport on seat s: a cost the cycler of a
// watcher start pays sits on the seat the trigger names, not always seat 0.
func applyCostSupportOn(p costPlan, generic string, f *StartFile, s *StartSeat) {
	s.Battlefield = append(s.Battlefield, landsFor(p.mana, generic)...)
	p.applyExtras(s)
}

// applyExtras places the cost plan's non-mana prerequisites on s: the
// permanents it sacrifices or taps, the cards it discards or exiles from the
// graveyard, its counters and the life it pays.
func (p costPlan) applyExtras(s *StartSeat) {
	s.Battlefield = append(s.Battlefield, p.field...)
	s.Tapped = append(s.Tapped, p.tapped...)
	s.Hand = append(s.Hand, p.hand...)
	s.Graveyard = append(s.Graveyard, p.graveyard...)
	s.Tokens = append(s.Tokens, p.tokens...)
	s.Counters = append(s.Counters, p.counters...)
	for _, c := range p.playerCounters {
		s.PlayerCounters = addPlayerCounter(s.PlayerCounters, c.Kind, c.N)
	}
	sortPlayerCounters(s.PlayerCounters)
	if p.life > 0 && (s.Life == nil || *s.Life < p.life) {
		l := p.life
		s.Life = &l
	}
}

// addPlayerCounter adds n of kind to cs, folding a repeat of the same kind.
// It keeps the sum in ONE entry so a cost that pays energy twice names the
// total, and so the slice never has two rows a follower would apply twice.
func addPlayerCounter(cs []StartCounter, kind string, n int32) []StartCounter {
	for i := range cs {
		if cs[i].Kind == kind {
			cs[i].N += n
			return cs
		}
	}
	return append(cs, StartCounter{Kind: kind, N: n})
}

// sortPlayerCounters orders a seat's player counters by kind. The output is a
// sorted slice, never a map walk, so a generated start is deterministic.
func sortPlayerCounters(cs []StartCounter) {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Kind < cs[j].Kind })
}

// castStart places the card in seat 0's hand with the lands for its mana cost
// and what its spell's Cost$ spends beyond mana (an additional cost:
// Sacrifice, ExileFromGrave, Discard, tapXType; planCastCost), and makes seat 0's
// focus its cast. A start placed only the mana, so a spell with an additional
// cost was never castable and play refused it as start-not-exercised (Costly
// Plunder, Fiery Conclusion, Skaab Goliath, Shared Discovery). "" when
// placed, else the refusal: a card with no mana cost (suspend-only: Ancestral
// Vision) is never cast from hand. A land is played, not cast.
func castStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, generic string, f *StartFile) string {
	p0 := &f.Setup.P0
	if face.IsLand() {
		// a land's enters trigger is its land play; three other lands of the
		// basic type its "enters tapped unless you control ..." names (or of
		// the start's colour) meet a check land's untapped condition (Idyllic
		// Grange, Federation Outpost)
		p0.Hand = []string{card}
		pad := generic
		for _, r := range face.Repls {
			for _, n := range basicOf {
				if strings.Contains(r.Params["Description"], "other "+n) {
					pad = n
				}
			}
		}
		p0.Battlefield = append(p0.Battlefield, pad, pad, pad)
		f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Kind: "land"}
		return ""
	}
	if strings.TrimSpace(face.ManaCost) == "no cost" {
		return "start:no-mana-cost"
	}
	p0.Hand = []string{card}
	p0.Battlefield = landsFor(splitCost(face.ManaCost), generic)
	f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Kind: "cast"}
	for _, sa := range face.Abilities {
		if sa.Kind != "SP" {
			continue
		}
		var extra []string
		for _, tok := range splitCost(sa.Params["Cost"]) {
			if !manaTokenRE.MatchString(tok) {
				extra = append(extra, tok)
			}
		}
		if len(extra) > 0 {
			plan, why := planCastCost(reg, b, face, card, strings.Join(extra, " "), generic)
			if why != "" {
				return why
			}
			plan.applyExtras(p0)
		}
		break
	}
	// K:AlternateAdditionalCost:A:B -- "as an additional cost, A or B": the
	// first option a start can pay (Silvergill Mentor's "behold a Merfolk or
	// pay {2}" is the {2})
	if alt, ok := face.KeywordParam("AlternateAdditionalCost"); ok {
		why := "steps-needed:cost:AlternateAdditionalCost"
		for _, opt := range strings.Split(alt, ":") {
			plan, w := planCastCost(reg, b, face, card, opt, generic)
			if w != "" {
				continue
			}
			p0.Battlefield = append(p0.Battlefield, landsFor(plan.mana, generic)...)
			plan.applyExtras(p0)
			why = ""
			break
		}
		if why != "" {
			return why
		}
	}
	// "As an additional cost to cast this spell, ..." (a RaiseCost of the
	// card itself): the additional cost's plan — the lands its mana takes
	// (a Waterbend's {N} rides in as mana tokens) and whatever else planCost
	// places (a Behold's revealed cards)
	for _, st := range face.Statics {
		if st.Mode != "RaiseCost" || st.Params["ValidCard"] != "Card.Self" || st.Params["Type"] != "Spell" ||
			!strings.HasPrefix(st.Params["Description"], "As an additional cost") {
			continue
		}
		cost := strings.TrimSpace(st.Params["Cost"])
		if !raiseCostModelled(cost) {
			continue // an optional or X cost (waterbend {X}, a "you may" exile): the bot may decline it
		}
		plan, why := planCastCost(reg, b, face, card, cost, generic)
		if why != "" {
			return why
		}
		p0.Battlefield = append(p0.Battlefield, landsFor(plan.mana, generic)...)
		plan.applyExtras(p0)
	}
	return ""
}

// raiseCostModelled reports whether an additional-cost RaiseCost is one
// planCost's mandatory arms plan: every non-mana token is a Waterbend of a
// literal N or a Behold/BeholdExile. Any other token (an X cost, a "you may"
// exile or discard, a tap or sacrifice) is left to the cast the way it was
// before additional costs were planned, not refused.
func raiseCostModelled(cost string) bool {
	modelled := false
	for _, tok := range splitCost(cost) {
		if manaTokenRE.MatchString(tok) {
			continue
		}
		head, args := costArgs(tok)
		switch {
		case head == "Waterbend" && len(args) == 1:
			if _, err := strconv.Atoi(args[0]); err != nil {
				return false
			}
		case (head == "Behold" || head == "BeholdExile") && len(args) >= 2:
		default:
			return false
		}
		modelled = true
	}
	return modelled
}
