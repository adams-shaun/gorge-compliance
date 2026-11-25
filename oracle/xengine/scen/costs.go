package scen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
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
	mana      []string // mana tokens: one basic each on the battlefield
	zone      string   // where the card sits (zoneBattlefield, zoneHand, zoneGraveyard)
	field     []string // permanents the cost sacrifices, returns or taps
	tapped    []string // of field, those placed tapped (lands: they add no mana)
	hand      []string // cards the cost discards
	graveyard []string // cards the cost exiles from the graveyard
	counters  []StartCounter
}

// planCost reads cost for the card's ability: every token a start can meet
// with placed state, or the refusal of the first it cannot
// (steps-needed:cost:<head>, as costTokens names it).
//
//   - Sac<N/Type>, Return<N/Type>: N fodder permanents of Type (fodder);
//   - tapXType<N/Type>: N untapped fodder creatures; tapXType<Any/...
//     withTotalPowerGEn> (crew n) one vanilla creature with power n or more;
//   - Discard<1/CARDNAME>: the card in hand; Discard<N/Type>: N fodder cards
//     in hand;
//   - ExileFromGrave<1/CARDNAME>: the card in the graveyard;
//     ExileFromGrave<N/Type>: N fodder cards in the graveyard;
//   - SubCounter<N/Kind>: N counters of Kind on the card (a planeswalker's
//     LOYALTY: only those its printed loyalty lacks); AddCounter<N/Kind> on
//     the card needs nothing.
func planCost(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, cost, generic string) (costPlan, string) {
	p := costPlan{zone: zoneBattlefield}
	for _, tok := range splitCost(cost) {
		if manaTokenRE.MatchString(tok) {
			p.mana = append(p.mana, tok)
			continue
		}
		if tok == "T" || tok == "Q" || payLifeRE.MatchString(tok) {
			continue
		}
		head, args := costArgs(tok)
		refusal := "steps-needed:cost:" + head
		n := 0
		if len(args) >= 2 {
			var err error
			if n, err = strconv.Atoi(args[0]); err != nil || n < 0 || n > 4 && head != "SubCounter" {
				if !(head == "tapXType" && args[0] == "Any") {
					return p, refusal
				}
			}
		}
		self := len(args) >= 2 && (args[1] == "CARDNAME" || args[1] == "NICKNAME")
		if len(args) >= 2 && args[1] == "Hand" {
			// Discard<N/Hand>: discard the hand, N cards of it
			args[1] = "Card"
		}
		switch {
		case head == "Discard" && n == 0 && len(args) >= 2:
		case (head == "Sac" || head == "Return") && self && n == 1:
		case head == "Discard" && self && n == 1:
			p.zone = zoneHand
		case head == "ExileFromGrave" && self && n == 1:
			p.zone = zoneGraveyard
		case (head == "Sac" || head == "Return") && len(args) >= 2 && !self:
			names, land := fodder(reg, b, card, args[1], generic, false)
			if names == "" {
				return p, refusal
			}
			for range n {
				p.field = append(p.field, names)
				if land {
					p.tapped = append(p.tapped, names)
				}
			}
		case head == "tapXType" && len(args) >= 2 && !self:
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
			name, land := fodder(reg, b, card, typ, generic, true)
			if name == "" || land {
				return p, refusal
			}
			for range n {
				p.field = append(p.field, name)
			}
		case head == "Discard" && len(args) >= 2 && !self && n >= 1:
			name, _ := fodder(reg, b, card, args[1], generic, false)
			if name == "" {
				return p, refusal
			}
			for range n {
				p.hand = append(p.hand, name)
			}
		case head == "ExileFromGrave" && len(args) >= 2 && !self && n >= 1:
			name, _ := fodder(reg, b, card, args[1], generic, false)
			if name == "" {
				return p, refusal
			}
			for range n {
				p.graveyard = append(p.graveyard, name)
			}
		case head == "SubCounter" && len(args) == 2 && n >= 1:
			kind := strings.ToUpper(args[1])
			need := int32(n)
			if kind == "LOYALTY" {
				if l, err := strconv.Atoi(face.Loyalty); err == nil {
					need -= int32(l)
				}
			}
			if need > 0 {
				p.counters = append(p.counters, StartCounter{Card: card, Kind: kind, N: need})
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
	}
	applyCostSupport(p, generic, f)
}

// applyCostSupport places the state a cost plan needs around the card that pays
// it, without placing the card itself: applyCost uses it for an ability the
// card activates, and a granted ability uses it for the carrier the ability was
// granted to (already on the battlefield).
func applyCostSupport(p costPlan, generic string, f *StartFile) {
	s := &f.Setup.P0
	s.Battlefield = append(s.Battlefield, landsFor(p.mana, generic)...)
	s.Battlefield = append(s.Battlefield, p.field...)
	s.Tapped = append(s.Tapped, p.tapped...)
	s.Hand = append(s.Hand, p.hand...)
	s.Graveyard = append(s.Graveyard, p.graveyard...)
	s.Counters = append(s.Counters, p.counters...)
}
