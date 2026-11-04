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

// seatMap relabels live seats to scenario seats. rngOK lowers an entry that
// consumed engine randomness (a window's item opts out of the library order)
// where a whole-game tape stops at it.
type seatMap struct{ swap, rngOK bool }

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
// option p: a player as "pN", an object as its ref, an election by the picked
// option's kind or label, anything else by its label. ok is false when the
// option has no spelling the matcher reads back unambiguously.
func (m seatMap) pickString(p game.Pick) (s string, ok bool) {
	if electionByLabel(p.Kind) {
		// Every option of this decision carries the same source object in Obj
		// (an unless-cost's permanent, a modal spell's source, a riot creature),
		// so a ref matches the first option whatever was picked. The label is
		// the spelling that tells the options apart.
		if p.Label == "" || isRefLike(p.Label) {
			return "", false
		}
		return p.Label, true
	}
	if p.Obj != 0 && electionByKind(p.Kind) {
		// The kind names the picked option; its shared source ref would match
		// option 0 instead.
		return p.Kind, !isRefLike(p.Kind)
	}
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

// electionByLabel reports whether every option of this election shares one
// Kind, so only the picked option's Label names it: the runner reads any
// option carrying the decision's source in Obj as option 0. The set is the
// census of gorge's same-Kind elections; a kind used both this way and to
// select an object (tap, top, trigger, ...) is deliberately absent.
func electionByLabel(kind string) bool {
	switch kind {
	case "mode", "riot", "unleash", "trigger_cost_x", "trigger_cost_counter", "manaconvert":
		return true
	}
	return false
}

// electionByKind reports whether the picked option's Kind is itself the
// answer: the sibling options of this decision have distinct Kinds, so the
// runner matches the kind before it would match the shared source ref. It is
// gated on a non-zero Obj by the caller: without a source object the switch
// below already spells these by label or kind, so only the shared-source case
// changes.
func electionByKind(kind string) bool {
	switch kind {
	case "yes", "no",
		"apply", "decline",
		"trigger_cost_pay", "trigger_cost_pay_manual", "trigger_cost_decline",
		"cumulative_pay", "cumulative_pay_manual", "cumulative_sac",
		"echo_pay", "echo_pay_manual", "echo_sac",
		"opening_yes", "opening_no",
		"suspend_cast_yes", "suspend_cast_no", "defeat_cast_yes", "defeat_cast_no",
		"time_travel_skip", "time_travel_add", "time_travel_remove",
		"aor_skip", "command_zone", "leave",
		"endure_counters", "endure_spirit":
		return true
	}
	// The counter-kind suffix is part of the kind (aor_remove:charge).
	return strings.HasPrefix(kind, "aor_remove:") || strings.HasPrefix(kind, "aor_put:")
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
