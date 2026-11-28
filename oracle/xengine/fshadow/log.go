// Package fshadow is the Forge-led follower (docs: gorge's
// docs/design/2026-11-17-xengine-commander-diffs.md, section 3 step 4): it
// reads one game's forge-shadow-1 log, which Forge wrote while it hosted the
// game, and replays that game in gorge, answering every gorge decision with
// the answer Forge's seat gave. gorge is built with
// rules.NewHypotheticalChancePlanned, so Forge's shuffles and random draws
// are gorge's too, and each cast is paid with an explicit PaymentPlan of the
// sources Forge tapped. A cast gorge has no such plan for, or an activation it
// does not offer (a filter land's or a Signet's mana, which gorge's planner
// does not plan), is paid by hand: the follower activates those sources at
// priority, then takes the offer (Result.HandPayments counts them). It does so
// only where Forge's answer names one gorge option (a card with one way to be
// cast, an ability both engines list) and the floating mana pays it with no
// further ask. At every
// snap line gorge's state is compared with Forge's through
// oraclediff.CompareOpts, the comparison classify uses.
//
// The MVP stops at the first hard divergence (Follow's Result.Hard); option-set
// differences that leave Forge's answer on gorge's list are soft and only
// recorded. It covers mulligans, the starting seat, priority (pass, land,
// cast), attackers, blockers and targets; any other ask kind is a harness
// divergence (what "error", field "harness:...").
//
// Forge's recorder writes no ask line for a modal answer (a charm's modes, a
// modal trigger's), so a gorge modes decision is answered by default (the
// first modes it requires; Result.Modes counts them) and consumes no log line.
//
// Forge's recorder writes no ask line for a scry, surveil or other
// library-arranging answer, so a gorge arrange decision is answered by
// default (offered cards stay on top in order; Result.Arranges counts them)
// and consumes no log line.
//
// Forge's recorder writes one target ask over a whole sub-ability chain, with
// its targets split by targeting part ("target_slots"), while gorge asks each
// targeting link its own decision. The line answers the first of them; the
// parts left answer the later asks with the same Source before any further line
// is read, the next trigger's target line included (Result.TargetParts counts
// them). A root or triggered link's ask names its link on the source's ability
// chain, which picks its part: gorge asks nothing for an optional link with no
// candidate, while Forge still counts its empty part. A decision the parts
// cannot answer, or one whose part cannot be told, is a harness divergence
// (field "target_slots").
//
// Forge's recorder writes a target ask only where its targeting hook is asked,
// so a gorge target decision whose log line is an ask of another kind (the
// seat's next priority) is answered by default (the first Min options;
// Result.Targets counts them) and consumes no log line. A cast or an
// activation carries the targets Forge chose for it in its priority answer
// ("targets"), split by targeting part ("target_slots"), so each target
// decision gorge then poses for that object (the decision's Source) is
// answered from the next part instead (Result.CastTargets counts them), still
// consuming no line: a part Forge left empty answers an optional ask with
// nothing, a mandatory ask passes over it, and a combined modal ask takes one
// part per mode. A target line that follows the cast wins over these parts. A
// decision the next part cannot answer, or an optional sub-ability ask that
// could be an empty part's or a later one's, is a harness divergence (field
// "cast_targets").
//
// Forge's recorder writes no ask for a "may" trigger's yes/no, so a gorge
// optional-trigger decision (trigger_optional) is answered by default (the
// first option, yes; Result.Optionals counts them) and consumes no log line.
//
// Forge's recorder writes no ask for the order of a seat's simultaneously
// triggered abilities, so a gorge trigger-order decision (trigger_order) is
// answered by default (the offered order; Result.Orders counts them) and
// consumes no log line.
//
// Forge's recorder writes no ask for a commander's owner choosing the command
// zone (CR 903.9), so a gorge commander_zone decision is answered from the
// next snapshot's command zone (Result.CommanderZones counts them) and consumes
// no log line. Any other decision of a kind the log never carries (a
// replacement choice) is answered by default (the first Min options;
// Result.OtherDefaults counts them), likewise consuming none.
//
// # The forge-shadow-1 log
//
// One JSON object per line, told apart by "t". Card identities are Forge's
// own ids; the header binds each to (seat, decklist index), which is how
// gorge's ObjIDs are bound at genesis (Follow verifies the names).
//
//	header   {"t":"header","format":"forge-shadow-1","seed":N,"forge_ref":"...","game_format":"constructed|commander",
//	          "mulligans":K,"max_turns":T,"max_ms":M,"seats":[{"seat":0,"name":"..","cards":[{"id":101,"name":"Forest"},..],"commanders":[idx..]}]}
//	shuffle  {"t":"shuffle","seat":0,"order":[ids, top first]}            the opening shuffles (before start) and every later one
//	start    {"t":"start","seat":1}                                       the seat taking the first turn
//	mull     {"t":"mull","seat":1,"keep":false,"hand":[ids]}              or {"keep":true,"bottom":[ids],"hand":[ids]}
//	ask      {"t":"ask","n":7,"seat":1,"turn":3,"step":"main1","kind":"priority","options":[..],"answer":{..}}
//	rand     {"t":"rand","kind":"coin|die","bound":2,"value":1}            coin 1 heads 0 tails; die 1..bound
//	snap     {"t":"snap","n":3,"snapshot":{rules.OracleSnapshot}}         checkpoint "t<turn>:<step>", at the start of a step with an empty stack
//	end      {"t":"end","winner":0,"reason":"AllOpponentsLost"}           Forge's GameEndReason, or "unfinished"; no winner for a draw
//
// An ask's kind is Forge's: priority, attack, block, target (and mode, choose,
// optional, order, ... which the MVP refuses). Its options are what Forge
// offered (Option) and its answer what the seat chose (Answer); a priority
// answer's action is pass, land, cast, activate or other, and a cast lists the
// mana sources Forge tapped for it. A ref is {"card":id}, {"player":seat} or
// {"spell":id}. Forge asks priority in steps gorge does not (the turn-1 untap
// step): such an ask is always a pass, and the follower skips it, as it
// skips a snap of a step gorge has already left. The line grammar is
// forge-oracle's ShadowLog (adams-shaun/forge, forge-oracle/.../ShadowLog.java);
// fields it marks as extras (policy, index, label, ...) are ignored, except a
// mull line's hand, which tells a state-neutral mulligan after the hand has
// emptied from an ordinary one, and a priority answer's mode, which rules out
// paying a flashback or kicked cast by hand.
package fshadow

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/adams-shaun/gorge/engine/rules"
)

// Format is the log's format tag, the header's "format".
const Format = "forge-shadow-1"

// The line types ("t").
const (
	THeader  = "header"
	TShuffle = "shuffle"
	TStart   = "start"
	TMull    = "mull"
	TAsk     = "ask"
	TRand    = "rand"
	TSnap    = "snap"
	TEnd     = "end"
)

// Header is the log's first line: the game's decks and caps.
type Header struct {
	T          string `json:"t"`
	Format     string `json:"format"`
	Seed       uint64 `json:"seed"`
	ForgeRef   string `json:"forge_ref,omitempty"`
	GameFormat string `json:"game_format,omitempty"` // "constructed" (default) or "commander"
	// Mulligans is Forge's counted London mulligan cap (its starting hand
	// size, 7). Setup adds FreeMulligan back when it converts the header to
	// gorge's Config, whose allowance counts the free first mulligan too.
	Mulligans int `json:"mulligans,omitempty"`
	// FreeMulligan is Forge's first-mulligan-is-free rule (more than two
	// seats). gorge derives the same rule from the seat count (CR 103.5c) and
	// has no Config input for it, so a header that disagrees is a harness
	// finding (setup-inexpressible).
	FreeMulligan bool   `json:"free_mulligan,omitempty"`
	MaxTurns     int    `json:"max_turns,omitempty"`
	MaxMS        int    `json:"max_ms,omitempty"`
	Seats        []Seat `json:"seats"`
}

// Seat is one seat's deck in library-build order.
type Seat struct {
	Seat       int    `json:"seat"`
	Name       string `json:"name"`
	Cards      []Card `json:"cards"`
	Commanders []int  `json:"commanders,omitempty"` // indices into Cards
}

// Card is one decklist entry: Forge's card id and the card's name.
type Card struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Shuffle is one library shuffle's result, top first.
type Shuffle struct {
	T     string `json:"t"`
	Seat  int    `json:"seat"`
	Order []int  `json:"order"`
}

// Start names the seat that takes the first turn.
type Start struct {
	T    string `json:"t"`
	Seat int    `json:"seat"`
}

// Mull is one keep-or-mulligan answer; a keep lists the cards put on the
// bottom (CR 103.5). Hand is Forge's hand after the decision (the recorder
// writes it as an extra); it is a pointer so an omitted extra (nil, older
// logs and synthetic fixtures) stays distinct from a recorded empty hand
// (&[]), the state a mulligan after the hand has emptied leaves.
type Mull struct {
	T      string `json:"t"`
	Seat   int    `json:"seat"`
	Keep   bool   `json:"keep"`
	Bottom []int  `json:"bottom,omitempty"`
	Hand   *[]int `json:"hand,omitempty"`
}

// emptyHand reports whether the line records a decision made with no cards in
// hand, as opposed to a log or fixture that omits the hand extra.
func (m *Mull) emptyHand() bool { return m.Hand != nil && len(*m.Hand) == 0 }

// Ask is one decision Forge's seat answered.
type Ask struct {
	T       string   `json:"t"`
	N       int      `json:"n"`
	Seat    int      `json:"seat"`
	Turn    int      `json:"turn"`
	Step    string   `json:"step"`
	Kind    string   `json:"kind"`
	Options []Option `json:"options"`
	Answer  Answer   `json:"answer"`
}

// Forge's ask kinds the MVP answers.
const (
	KindPriority = "priority"
	KindAttack   = "attack"
	KindBlock    = "block"
	KindTarget   = "target"
)

// Option is one choice Forge offered. Priority: Action (land, cast,
// activate, other), Card and Ability. Attack: Card attacks Player, or the
// planeswalker or battle DefenderCard. Block: Card blocks Attacker. Target:
// a ref (Card, Player or Spell).
type Option struct {
	Action       string `json:"action,omitempty"`
	Card         int    `json:"card,omitempty"`
	Ability      int    `json:"ability,omitempty"`
	Player       *int   `json:"player,omitempty"`
	Spell        int    `json:"spell,omitempty"`
	DefenderCard int    `json:"defender_card,omitempty"`
	Attacker     int    `json:"attacker,omitempty"`
}

// Answer is the seat's answer to an ask.
type Answer struct {
	Action  string `json:"action,omitempty"` // priority: pass, land, cast, activate, other
	Card    int    `json:"card,omitempty"`
	Source  int    `json:"source,omitempty"` // target: the host card
	Ability int    `json:"ability,omitempty"`
	Tapped  []int  `json:"tapped,omitempty"`  // the mana sources tapped to pay
	Targets []Ref  `json:"targets,omitempty"` // target; also a cast's or activation's
	// TargetSlots is how many of Targets each targeting part of the spell or
	// ability holds, in chain order; 0 is a part Forge chose nothing for. A
	// log without it is read as one part holding every target.
	TargetSlots []int `json:"target_slots,omitempty"`
	// Mode is Forge's mark on a cast that is not the ordinary one
	// ("flashback", "kicked"); the recorder marks no other.
	Mode    string   `json:"mode,omitempty"`
	Attacks []Attack `json:"attacks,omitempty"` // attack
	Blocks  []Block  `json:"blocks,omitempty"`  // block
}

// targetParts is Targets split by TargetSlots, one entry per targeting part
// (Read holds the two to one length).
func (a *Answer) targetParts() [][]Ref {
	if len(a.TargetSlots) == 0 {
		if len(a.Targets) == 0 {
			return nil
		}
		return [][]Ref{slices.Clone(a.Targets)}
	}
	rest := slices.Clone(a.Targets)
	parts := make([][]Ref, len(a.TargetSlots))
	for i, n := range a.TargetSlots {
		n = max(0, min(n, len(rest)))
		parts[i], rest = rest[:n:n], rest[n:]
	}
	return parts
}

// checkTargetSlots rejects target_slots that do not split targets exactly.
func (a *Answer) checkTargetSlots() error {
	if len(a.TargetSlots) == 0 {
		return nil
	}
	held := 0
	for _, n := range a.TargetSlots {
		if n < 0 {
			return fmt.Errorf("target_slots %v: a negative count", a.TargetSlots)
		}
		held += n
	}
	if held != len(a.Targets) {
		return fmt.Errorf("target_slots %v hold %d targets, the answer has %d", a.TargetSlots, held, len(a.Targets))
	}
	return nil
}

// Ref is a card, a player, or a spell on the stack (by its host card's id).
type Ref struct {
	Card   int  `json:"card,omitempty"`
	Player *int `json:"player,omitempty"`
	Spell  int  `json:"spell,omitempty"`
}

// Attack is one attacker and the seat it attacks; an attack on a
// planeswalker or battle is Defender -1 with DefenderCard.
type Attack struct {
	Card         int `json:"card"`
	Defender     int `json:"defender"`
	DefenderCard int `json:"defender_card,omitempty"`
}

// Block is one blocker and the attacker it blocks.
type Block struct {
	Card     int `json:"card"`
	Attacker int `json:"attacker"`
}

// Rand is one random draw that is neither a shuffle nor the starting toss:
// a coin (Bound 2, Value 1 heads, 0 tails) or a die (Value 1..Bound). Forge
// logs no random pick yet (its AtRandom choices have no hook).
type Rand struct {
	T     string `json:"t"`
	Kind  string `json:"kind"`
	Bound int    `json:"bound"`
	Value int    `json:"value"`
}

// Snap is Forge's state at the start of a step with an empty stack.
type Snap struct {
	T        string               `json:"t"`
	N        int                  `json:"n"`
	Snapshot rules.OracleSnapshot `json:"snapshot"`
}

// End is the game's end.
type End struct {
	T      string `json:"t"`
	Winner *int   `json:"winner,omitempty"`
	Reason string `json:"reason,omitempty"` // Forge's GameEndReason name, or ReasonUnfinished
}

// ReasonUnfinished is an end line's reason for a game Forge stopped early (a
// turn or time cap): the follower stops clean there.
const ReasonUnfinished = "unfinished"

// Entry is one parsed line: Line is its 1-based line number (the log offset a
// divergence reports) and exactly one of the pointers is set, per T.
type Entry struct {
	Line    int
	T       string
	Header  *Header
	Shuffle *Shuffle
	Start   *Start
	Mull    *Mull
	Ask     *Ask
	Rand    *Rand
	Snap    *Snap
	End     *End
}

// Log is a whole parsed log.
type Log struct {
	Header  Header
	Entries []Entry // every line after the header, in order
}

// maxLine caps one line; a snapshot of a 4-seat pod is well under it.
const maxLine = 16 << 20

// Read parses a forge-shadow-1 log. The first line must be its header.
func Read(r io.Reader) (*Log, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	l := &Log{}
	n := 0
	for sc.Scan() {
		n++
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var tag struct {
			T string `json:"t"`
		}
		if err := json.Unmarshal(b, &tag); err != nil {
			return nil, fmt.Errorf("fshadow: line %d: %w", n, err)
		}
		e := Entry{Line: n, T: tag.T}
		var v any
		switch tag.T {
		case THeader:
			e.Header = &Header{}
			v = e.Header
		case TShuffle:
			e.Shuffle = &Shuffle{}
			v = e.Shuffle
		case TStart:
			e.Start = &Start{}
			v = e.Start
		case TMull:
			e.Mull = &Mull{}
			v = e.Mull
		case TAsk:
			e.Ask = &Ask{}
			v = e.Ask
		case TRand:
			e.Rand = &Rand{}
			v = e.Rand
		case TSnap:
			e.Snap = &Snap{}
			v = e.Snap
		case TEnd:
			e.End = &End{}
			v = e.End
		default:
			return nil, fmt.Errorf("fshadow: line %d: unknown line type %q", n, tag.T)
		}
		if err := json.Unmarshal(b, v); err != nil {
			return nil, fmt.Errorf("fshadow: line %d (%s): %w", n, tag.T, err)
		}
		if e.Ask != nil {
			if err := e.Ask.Answer.checkTargetSlots(); err != nil {
				return nil, fmt.Errorf("fshadow: line %d (%s): %w", n, tag.T, err)
			}
		}
		if e.Header != nil {
			if len(l.Entries) > 0 || l.Header.T != "" {
				return nil, fmt.Errorf("fshadow: line %d: a second header", n)
			}
			l.Header = *e.Header
			continue
		}
		if l.Header.T == "" {
			return nil, fmt.Errorf("fshadow: line %d: %s before the header", n, tag.T)
		}
		l.Entries = append(l.Entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("fshadow: %w", err)
	}
	if l.Header.T == "" {
		return nil, fmt.Errorf("fshadow: no header")
	}
	if l.Header.Format != Format {
		return nil, fmt.Errorf("fshadow: format %q, want %q", l.Header.Format, Format)
	}
	if len(l.Header.Seats) < 2 {
		return nil, fmt.Errorf("fshadow: %d seats, want at least 2", len(l.Header.Seats))
	}
	for i, s := range l.Header.Seats {
		if s.Seat != i {
			return nil, fmt.Errorf("fshadow: header seat %d is listed as seat %d", i, s.Seat)
		}
	}
	return l, nil
}

// Write writes l back in its line form, header first: the golden fixtures'
// writer and the doctoring tests' round trip.
func Write(w io.Writer, l *Log) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	h := l.Header
	h.T = THeader
	if err := enc.Encode(h); err != nil {
		return err
	}
	for _, e := range l.Entries {
		var v any
		switch {
		case e.Shuffle != nil:
			v = e.Shuffle
		case e.Start != nil:
			v = e.Start
		case e.Mull != nil:
			v = e.Mull
		case e.Ask != nil:
			v = e.Ask
		case e.Rand != nil:
			v = e.Rand
		case e.Snap != nil:
			v = e.Snap
		case e.End != nil:
			v = e.End
		default:
			return fmt.Errorf("fshadow: entry at line %d has no body", e.Line)
		}
		if err := enc.Encode(v); err != nil {
			return err
		}
	}
	return nil
}
