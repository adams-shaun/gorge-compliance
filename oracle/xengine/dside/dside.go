// Package dside puts dirac beside gorge in an xengine game. A game whose decks a
// dirac table can play is played first in lockstep (dirac/oracle), the two engines
// compared at every decision, and the answers gorge gave are then replayed as the
// sweep's game (game.Options.Script), so each window of that game has dirac's answer
// beside gorge's. A game dirac cannot play reads as not applicable, never as a
// divergence.
package dside

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/dirac/cardset"
	"github.com/adams-shaun/gorge/dirac/oracle"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
)

// How a game went on dirac.
const (
	StatusClean      = "clean"          // the engines agreed to the end of the game or a turn cap
	StatusDiverged   = "diverged"       // they disagreed at a decision
	StatusCapacity   = "capacity"       // dirac stopped at a declared capacity
	StatusUnmodelled = "unmodelled"     // dirac stopped at a rule it does not model
	StatusHang       = "hang"           // no result in GameTimeout
	StatusNA         = "not_applicable" // no game: Reason says why
)

// Dirac's answer for one item.
const (
	Agree   = "D=G"  // dirac posed and reached what gorge did through the item
	Diverge = "D!=G" // dirac differs from gorge within the item
	NotAppl = "D-"   // dirac has no answer here
)

const (
	// GameTimeout bounds one lockstep game. A game takes well under a second, so a
	// game past it is a hang, and its goroutine is left behind.
	GameTimeout = 30 * time.Second
	diffClip    = 20 // Row.Diff lines kept
)

// Row is one game's line of dirac.jsonl. Intents is how many answers gorge
// committed in lockstep. FromTurn and ToTurn are the turns of the answer before the
// disagreement and of the decision it showed at, or both the turn of the stall.
type Row struct {
	Game       string   `json:"game"`
	Status     string   `json:"status"`
	Table      string   `json:"table,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Unverified int      `json:"unverified,omitempty"` // deck lines dirac plays without evidence
	Decisions  int      `json:"decisions,omitempty"`
	Intents    int      `json:"intents,omitempty"`
	FromTurn   int32    `json:"from_turn,omitempty"`
	ToTurn     int32    `json:"to_turn,omitempty"`
	What       string   `json:"what,omitempty"`
	Field      string   `json:"field,omitempty"`
	Diff       []string `json:"diff,omitempty"`
	Implicates []string `json:"implicates,omitempty"`
}

// Locate sets the row's turns from the tape of the game the lockstep's answers
// replayed into.
func (r *Row) Locate(t *game.Tape) {
	if r.Status != StatusDiverged && r.Status != StatusCapacity && r.Status != StatusUnmodelled {
		return
	}
	turn := func(i int) int32 {
		i = min(i, len(t.Entries)-1)
		if i < 0 {
			return 0
		}
		return t.Entries[i].Turn
	}
	r.FromTurn, r.ToTurn = turn(r.Intents-1), turn(r.Intents)
	if r.Status != StatusDiverged {
		r.FromTurn = r.ToTurn
	}
}

// Answer is dirac's answer for an item of this game, from its id
// (xe:<set>:<chunk>:<game>:w<turn>, or :tape for the whole game).
func (r Row) Answer(id string) string {
	_, suffix, ok := SplitID(id)
	if !ok {
		return NotAppl
	}
	switch r.Status {
	case StatusClean:
		return Agree
	case StatusDiverged, StatusCapacity, StatusUnmodelled:
	default:
		return NotAppl
	}
	n, err := strconv.Atoi(strings.TrimPrefix(suffix, "w"))
	if err != nil || !strings.HasPrefix(suffix, "w") {
		if r.Status == StatusDiverged {
			return Diverge
		}
		return NotAppl
	}
	switch turn := int32(n); {
	case turn < r.FromTurn:
		return Agree
	case r.Status == StatusDiverged && turn <= r.ToTurn:
		return Diverge
	}
	return NotAppl
}

// GameID is the id of game g of chunk c: the prefix of its items' ids.
func GameID(set string, chunk, g int) string { return fmt.Sprintf("xe:%s:%d:%d", set, chunk, g) }

// SplitID cuts an item id into its game's id and what follows it.
func SplitID(id string) (gameID, suffix string, ok bool) {
	p := strings.SplitN(id, ":", 5)
	if len(p) != 5 {
		return "", "", false
	}
	return strings.Join(p[:4], ":"), p[4], true
}

// Tables serves the Standard sets' dirac tables to one play.
type Tables struct {
	reg  *cards.Registry
	sets map[string]cardset.Set
}

// NewTables is a server over reg's corpus.
func NewTables(reg *cards.Registry) *Tables {
	return &Tables{reg: reg, sets: map[string]cardset.Set{}}
}

// Table is set code's table. An error wrapping cardset.ErrNoTables means none
// are built for this gorge and corpus.
func (t *Tables) Table(code string) (cardset.Set, error) {
	if s, ok := t.sets[code]; ok {
		return s, nil
	}
	s, err := cardset.SynthByName("std-" + strings.ToLower(code))
	if err != nil {
		return nil, err
	}
	// The pool plays every lowered card; a card dirac refuses by name is known to
	// play wrongly, so it stays out of the decks and a pair holding one is turned
	// away, as kfuzz -synth does. An unverified card plays.
	s = cardset.AllowUnverified(s)
	t.sets[code] = s
	return s, nil
}

// CommanderTable is dirac's Commander deck pool (gorge dirac/oracle/pools.go, Pools):
// the gorge Foundations Commander precons. A later pool of more Commander decks
// replaces this one name.
const CommanderTable = "cmd5"

// CommanderSet is the Commander deck pool's table, from the deck pools' registry
// rather than the Standard sets'. An error wrapping cardset.ErrNoTables means none
// are built for this gorge and corpus.
func (t *Tables) CommanderSet() (cardset.Set, error) {
	if s, ok := t.sets[CommanderTable]; ok {
		return s, nil
	}
	s, err := cardset.ByName(CommanderTable)
	if err != nil {
		return nil, err
	}
	s = cardset.AllowUnverified(s)
	t.sets[CommanderTable] = s
	return s, nil
}

// Playable is the candidates of set code its table plays.
func (t *Tables) Playable(code string, cands []decks.Card) ([]decks.Card, error) {
	s, err := t.Table(code)
	if err != nil {
		return nil, err
	}
	plays := s.Supported()
	var out []decks.Card
	for _, c := range cands {
		if i, ok := s.Lookup(c.Name); ok && plays[i] {
			out = append(out, c)
		}
	}
	return out, nil
}

// entries is a sorted deck as a table's deck lines.
func entries(d decks.Deck) []cardset.Entry {
	var es []cardset.Entry
	for _, n := range d.Cards {
		if k := len(es); k > 0 && es[k-1].Name == n {
			es[k-1].Count++
			continue
		}
		es = append(es, cardset.Entry{Name: n, Count: 1})
	}
	return es
}

// NotApplicable is the row of a game no table holds.
func NotApplicable(gameID, reason string) Row {
	return Row{Game: gameID, Status: StatusNA, Reason: reason}
}

// Lockstep plays pair on code's table in lockstep with gorge under the explore
// policy, on the autopay surface the explore seats pay on. It returns the row and
// the answers gorge committed, which game.Options.Script replays. A pair the table
// refuses has no answers.
func (t *Tables) Lockstep(gameID, code string, pair [2]decks.Deck, seed uint64, maxTurns int) (Row, []decision.Intent) {
	row := Row{Game: gameID, Table: "std-" + strings.ToLower(code)}
	set, err := t.Table(code)
	if err != nil {
		return refused(row, err)
	}
	return t.lockstep(row, set, pair, seed, maxTurns, nil)
}

// CommanderLockstep plays a 1v1 Commander pair on dirac's Commander table
// (CommanderTable) as Lockstep plays a constructed one. The commanders are the decks'
// own (decks.Deck.CommanderIndex), the indices game.Play gives Config.Commanders, so
// the answers replay into the same game. A pair the table does not hold has no answers.
func (t *Tables) CommanderLockstep(gameID string, pair [2]decks.Deck, seed uint64, maxTurns int) (Row, []decision.Intent) {
	row := Row{Game: gameID, Table: CommanderTable}
	set, err := t.CommanderSet()
	if err != nil {
		return refused(row, err)
	}
	return t.lockstep(row, set, pair, seed, maxTurns, &[2]int{pair[0].CommanderIndex(), pair[1].CommanderIndex()})
}

// refused is row as a game no table holds, for err.
func refused(row Row, err error) (Row, []decision.Intent) {
	row.Status, row.Reason = StatusNA, err.Error()
	return row, nil
}

// lockstep plays pair on set, a Commander game with commanders (indices into each
// deck) when non-nil.
func (t *Tables) lockstep(row Row, set cardset.Set, pair [2]decks.Deck, seed uint64, maxTurns int, commanders *[2]int) (Row, []decision.Intent) {
	refuse := func(err error) (Row, []decision.Intent) { return refused(row, err) }
	tbl, _, _, err := set.Matchup(entries(pair[0]), entries(pair[1]))
	if err != nil {
		return refuse(err)
	}
	var dk [2][]*cards.Card
	for i, d := range pair {
		for _, n := range d.Cards {
			c, ok := t.reg.Lookup(n)
			if !ok {
				return refuse(fmt.Errorf("card %q not found", n))
			}
			dk[i] = append(dk[i], c)
			if j, ok := set.Lookup(n); ok && set.Status(j) == cardset.StatusUnverified {
				row.Unverified++
			}
		}
	}
	m, err := oracle.NewMatchup(t.reg, [2]string{"p0", "p1"}, dk, 0)
	if err != nil {
		return refuse(err)
	}
	if commanders != nil {
		m.Commander = true
		m.Commanders = *commanders
	}
	cfg := oracle.Config{GorgeAutopaySurface: true}
	k, err := oracle.SetSide(set, tbl, cfg)(m, seed)
	if err != nil {
		return refuse(err)
	}
	type outcome struct {
		res     oracle.Result
		intents []decision.Intent
	}
	done := make(chan outcome, 1)
	go func() {
		defer oracle.CloseSide(k)
		var o outcome
		rc := oracle.RunConfig{Config: cfg, MaxTurns: int32(maxTurns), Finish: func(g *oracle.GorgeSide) {
			o.intents = append(o.intents, g.E.L.Intents...)
		}}
		o.res = oracle.Lockstep(m, seed, k, oracle.ExplorePolicy(seed), rc)
		done <- o
	}()
	select {
	case o := <-done:
		row.classify(o.res, m)
		row.Intents = len(o.intents)
		return row, o.intents
	case <-time.After(GameTimeout):
		row.Status = StatusHang
		return row, nil
	}
}

func (r *Row) classify(res oracle.Result, m *oracle.Matchup) {
	r.Decisions = res.Decisions
	switch {
	case res.Div != nil:
		r.Status, r.What, r.Field = StatusDiverged, res.Div.What, res.Div.Field
		r.Diff = res.Div.Diff[:min(len(res.Div.Diff), diffClip)]
		if res.Div.Field == "panic" {
			r.Reason, _, _ = strings.Cut(res.Div.Detail, "\n")
		}
		r.Implicates = res.Div.Implicates(m)
	case res.StallUnmodelled:
		r.Status, r.Reason = StatusUnmodelled, res.Stall
	case strings.HasPrefix(res.Stall, oracle.KernelStallPrefix):
		r.Status, r.Reason = StatusCapacity, res.Stall
	default:
		r.Status = StatusClean
	}
}

// NoTables reports whether err says the dirac tables are not built.
func NoTables(err error) bool { return errors.Is(err, cardset.ErrNoTables) }
