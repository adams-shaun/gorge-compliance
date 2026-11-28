package main

import (
	"slices"
	"strings"
)

// maxSeats is the largest seat count a translated scenario carries: the
// runner's four-seat game (rules.oracleScenario.Players).
const maxSeats = 4

// xmageBase is what a test class's base class means for the translation.
type xmageBase struct {
	Seats  int      // 2 or 4
	Order  []string // XMage player identifiers in turn order; index = gorge seat
	Refuse string   // non-empty: refusal code for every method of the class
}

// twoSeat is the default two-player base (CardTestPlayerBase).
var twoSeat = xmageBase{Seats: 2, Order: []string{"playerA", "playerB"}}

// fourSeat is XMage's four-player game: its base classes create players A, B,
// C, D and play them A -> D -> C -> B (the classes' own comment). gorge plays
// seats in seat order, so playerA is p0, playerD p1, playerC p2, playerB p3,
// and XMage's turn numbers then equal gorge's.
var fourSeat = xmageBase{Seats: 4, Order: []string{"playerA", "playerD", "playerC", "playerB"}}

// xmageBases maps a test class's base class to its translation. A class whose
// base is not listed keeps translate's base-class / ai / multiplayer refusal.
//
// The refused bases: CardTestMultiPlayerBase (and MultiplayerTriggerTest,
// which extends it) plays with a range of influence of one (rule 801, out of
// this project's rules scope; gorge has no range of influence); the Commander
// duel and three-player bases deal each player a Commander deck file the
// translator cannot express; ...WithAIHelps lets the AI choose.
var xmageBases = map[string]xmageBase{
	"CardTestPlayerBase":                   twoSeat,
	"":                                     twoSeat,
	"CardTestCommander4Players":            fourSeat,
	"CardTestMultiPlayerBaseWithRangeAll":  fourSeat,
	"CardTestMultiPlayerBase":              {Refuse: "range-one"},
	"MultiplayerTriggerTest":               {Refuse: "range-one"},
	"CardTestCommander3PlayersFFA":         {Refuse: "commander-deck"},
	"CardTestOathbreaker3PlayersFFA":       {Refuse: "commander-deck"},
	"CardTestCommanderDuelBase":            {Refuse: "commander-deck"},
	"CardTestCommander4PlayersWithAIHelps": {Refuse: "ai"},
}

// seats is the scenario's seat count: 2 unless the class's base seats more
// (a translator built without a base is the two-player game).
func (t *translator) seats() int {
	if t.base.Seats == 0 {
		return 2
	}
	return t.base.Seats
}

// seatOf maps an XMage player identifier to its gorge seat: its index in the
// base's turn order.
func (t *translator) seatOf(v value) (int, bool) {
	if v.kind != vIdent {
		return 0, false
	}
	order := t.base.Order
	if order == nil {
		order = twoSeat.Order
	}
	if i := slices.Index(order, v.s); i >= 0 {
		return i, true
	}
	return 0, false
}

// p0 is seatOf with an unknown player read as seat 0.
func (t *translator) p0(v value) int {
	p, _ := t.seatOf(v)
	return p
}

// activeSeat is the seat whose turn turn is, seats playing in seat order.
func activeSeat(turn, seats int) int { return (turn - 1) % seats }

// active is the seat whose turn turn is.
func (t *translator) active(turn int) int { return activeSeat(turn, t.seats()) }

// isSeatRef reports whether s is a seat ref (p0 .. p3) rather than a name.
func isSeatRef(s string) bool {
	return len(s) == 2 && s[0] == 'p' && s[1] >= '0' && s[1] < '0'+maxSeats
}

// otherSeats lists every seat but p, in seat order.
func (t *translator) otherSeats(p int) []int {
	var out []int
	for q := 0; q < t.seats(); q++ {
		if q != p {
			out = append(out, q)
		}
	}
	return out
}

// otherSeatRefs finds name among every seat but p: the refs of the one seat
// that has it. Two or more seats having it is ambiguous (ambiguous-owner);
// no seat having it returns nil.
func (t *translator) otherSeatRefs(p int, name string, refs func(int, string) []string) (int, []string, *refusal) {
	found, seat := []string(nil), -1
	for _, q := range t.otherSeats(p) {
		rs := refs(q, name)
		if len(rs) == 0 {
			continue
		}
		if found != nil {
			return 0, nil, refuse("ambiguous-owner", "%s on p%d and p%d", name, seat, q)
		}
		found, seat = rs, q
	}
	return seat, found, nil
}

// opponent is the implicit "other player" of a two-seat game. With more
// seats it has no single answer: the caller (named in where) refuses
// multi-seat-shape.
func (t *translator) opponent(p int, where string) (int, *refusal) {
	if t.seats() > 2 {
		return 0, refuse("multi-seat-shape", "%s: no single opponent of p%d among %d seats", where, p, t.seats())
	}
	return 1 - p, nil
}

// oneSeatRefs finds name among every seat's objects: the refs of the one seat
// that has it, or many when two or more seats do (XMage resolves a bare name
// over every object, so the owner is then ambiguous).
func (t *translator) oneSeatRefs(name string) (refs []string, many bool) {
	for p := 0; p < t.seats(); p++ {
		rs := t.anyRefs(p, name)
		if len(rs) == 0 {
			continue
		}
		if refs != nil {
			return nil, true
		}
		refs = rs
	}
	return refs, false
}

// playerNameSeat maps a choice string naming a player by its in-game name
// ("PlayerB": the base classes create players named so) to that player's
// seat. Only a game of more than two seats reads it: there the choice picks
// one opponent among several, and the two-seat translation is left as it was.
func (t *translator) playerNameSeat(s string) (int, bool) {
	if t.seats() <= 2 || len(s) != len("PlayerA") || !strings.HasPrefix(s, "Player") {
		return 0, false
	}
	return t.seatOf(value{kind: vIdent, s: "player" + s[len("Player"):]})
}

// blockActive is the active seat ref a block step records for a game of more
// than two seats, where the attacker is not simply the blocker's other seat
// ("" in a two-seat game).
func (t *translator) blockActive(act int) string {
	if t.seats() <= 2 {
		return ""
	}
	return pref(act)
}
