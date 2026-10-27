// Package lower turns a recorded gorge game (a game.Tape) into one oracle
// tape scenario an XMage or Forge follower can replay, and checks the lowering
// by replaying it through gorge's own runner: at every step the runner's
// checkpoint must show the state the live game was in (a game.Digest).
package lower

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
)

// The runner binds a card dealt at setup to "p<owner>:<name>[#n]" and finds a
// token by "p<controller>:token:<name>[#n]". game.Play records each object's
// ref in the live game's seat numbering as the decision was posed (the tape's
// Pick.Ref, Pick.AttackerRef, Pick.BattleRef, ManaRec.Ref); the lowering
// never invents one. Seats are relabelled so the starting player is p0: every
// seat index and every "pN" in a ref swaps when the tape's starter is seat 1.

// seatMap relabels live seats to scenario seats.
type seatMap struct{ swap bool }

func (m seatMap) seat(s int) int {
	if m.swap {
		return 1 - s
	}
	return s
}

// ref relabels a runner ref ("p0:Forest#2" -> "p1:Forest#2"); "" stays "".
func (m seatMap) ref(r string) string {
	if !m.swap || r == "" || r[0] != 'p' {
		return r
	}
	i := strings.IndexByte(r, ':')
	if i < 2 {
		return r
	}
	n, err := strconv.Atoi(r[1:i])
	if err != nil {
		return r
	}
	return "p" + strconv.Itoa(m.seat(n)) + r[i:]
}

// player is the scenario spelling of a seat ("p1").
func (m seatMap) player(s int) string { return "p" + strconv.Itoa(m.seat(s)) }

// pickString is how the runner's answer matcher (matchPick) is told to take
// option p: a player as "pN", an object as its ref, a yes/no by its kind,
// anything else by its label. ok is false when the option has no spelling the
// matcher reads back unambiguously.
func (m seatMap) pickString(p game.Pick) (s string, ok bool) {
	if p.Obj != 0 && p.Kind != "mana" {
		// (A mana-colour wheel offers several options for one source: its
		// label, "Add B", is what tells them apart.)
		if pl, isPlayer := p.Obj.PlayerRef(); isPlayer {
			return m.player(int(pl)), true
		}
		return m.ref(p.Ref), p.Ref != ""
	}
	switch {
	case strings.Contains(p.Kind, "player"):
		return m.player(p.Player), true
	case p.Kind == "yes" || p.Kind == "no":
		return p.Kind, true
	case p.Label != "":
		// A label shaped like a seat or card ref would be read as one.
		if isRefLike(p.Label) {
			return "", false
		}
		return p.Label, true
	case p.Kind != "":
		return p.Kind, !isRefLike(p.Kind)
	}
	return "", false
}

func isRefLike(s string) bool {
	if len(s) < 2 || s[0] != 'p' {
		return false
	}
	if _, err := strconv.Atoi(s[1:]); err == nil {
		return true
	}
	return strings.Contains(s, ":")
}

// stepSeat is entry's seat as a scenario seat.
func (m seatMap) stepSeat(e game.Entry) int { return m.seat(e.Seat) }
