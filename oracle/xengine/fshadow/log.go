// Package fshadow is the Forge-led follower (docs: gorge's
// docs/design/2026-11-17-xengine-commander-diffs.md, section 3 step 4): it
// reads one game's forge-shadow-1 log, which Forge wrote while it hosted the
// game, and replays that game in gorge, answering every gorge decision with
// the answer Forge's seat gave. gorge is built with
// rules.NewHypotheticalChancePlanned, so Forge's shuffles and random draws
// are gorge's too, and each cast is paid with an explicit PaymentPlan of the
// sources Forge tapped. At every snap line gorge's state is compared with
// Forge's through oraclediff.CompareOpts, the comparison classify uses.
//
// The MVP stops at the first hard divergence (Follow's Result.Hard); option-set
// differences that leave Forge's answer on gorge's list are soft and only
// recorded. It covers mulligans, the starting seat, priority (pass, land,
// cast), attackers, blockers and targets; any other ask kind is a harness
// divergence (what "error", field "harness:...").
//
// # The forge-shadow-1 log
//
// One JSON object per line, told apart by "t". Card identities are Forge's
// own ids; the header binds each to (seat, decklist index), which is how
// gorge's ObjIDs are bound at genesis (Follow verifies the names).
//
//	header   {"t":"header","format":"forge-shadow-1","seed":N,"forge_ref":"...","game_format":"constructed|commander",
//	          "mulligans":K,"max_turns":T,"max_ms":M,"seats":[{"seat":0,"name":"..","cards":[{"id":101,"name":"Forest"},..],"commanders":[idx..]}]}
//	shuffle  {"t":"shuffle","seat":0,"order":[ids, top first]}            the opening shuffle and every later one, in order per seat
//	start    {"t":"start","seat":1}                                       the seat taking the first turn
//	mull     {"t":"mull","seat":1,"keep":false}                           or {"keep":true,"bottom":[ids]}
//	ask      {"t":"ask","n":7,"seat":1,"turn":3,"step":"main1","kind":"priority","options":[..],"answer":{..}}
//	rand     {"t":"rand","kind":"coin|die|pick","bound":2,"value":1}
//	snap     {"t":"snap","n":3,"snapshot":{rules.OracleSnapshot}}         at the start of a step with an empty stack
//	end      {"t":"end","winner":0,"reason":".."}                         winner omitted for a draw or a capped game
//
// An ask's kind is Forge's: priority, attack, block, target (and mode, choose,
// optional, order, ... which the MVP refuses). Its options are what Forge
// offered (Option) and its answer what the seat chose (Answer); a priority
// answer's action is pass, land, cast or activate, and a cast lists the mana
// sources Forge tapped for it.
package fshadow

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"

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
	Mulligans  int    `json:"mulligans,omitempty"`   // London mulligans each seat may take
	MaxTurns   int    `json:"max_turns,omitempty"`
	MaxMS      int    `json:"max_ms,omitempty"`
	Seats      []Seat `json:"seats"`
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
// bottom (CR 103.5).
type Mull struct {
	T      string `json:"t"`
	Seat   int    `json:"seat"`
	Keep   bool   `json:"keep"`
	Bottom []int  `json:"bottom,omitempty"`
}

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
// activate), Card and Ability. Attack: Card attacks Player. Block: Card
// blocks Attacker. Target: Card, or Player when Card is 0.
type Option struct {
	Action   string `json:"action,omitempty"`
	Card     int    `json:"card,omitempty"`
	Ability  int    `json:"ability,omitempty"`
	Player   *int   `json:"player,omitempty"`
	Attacker int    `json:"attacker,omitempty"`
}

// Answer is the seat's answer to an ask.
type Answer struct {
	Action  string   `json:"action,omitempty"` // priority: pass, land, cast, activate
	Card    int      `json:"card,omitempty"`
	Ability int      `json:"ability,omitempty"`
	Tapped  []int    `json:"tapped,omitempty"`  // the mana sources tapped to pay
	Targets []Ref    `json:"targets,omitempty"` // target
	Attacks []Attack `json:"attacks,omitempty"` // attack
	Blocks  []Block  `json:"blocks,omitempty"`  // block
}

// Ref is a card or a player.
type Ref struct {
	Card   int  `json:"card,omitempty"`
	Player *int `json:"player,omitempty"`
}

// Attack is one attacker and the player it attacks.
type Attack struct {
	Card     int `json:"card"`
	Defender int `json:"defender"`
}

// Block is one blocker and the attacker it blocks.
type Block struct {
	Card     int `json:"card"`
	Attacker int `json:"attacker"`
}

// Rand is one random draw that is not a shuffle: a coin, a die, a random pick.
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
	Reason string `json:"reason,omitempty"`
}

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
