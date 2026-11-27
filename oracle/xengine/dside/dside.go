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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/dirac/cardset"
	"github.com/adams-shaun/gorge/dirac/oracle"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
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

// SwapRefusedCommander is ds with every card the Commander table refuses replaced
// by a basic of the deck's colours (decks.SwapRefused), and each swap. gorge's
// Foundations precon files hold the printed lists, which the table turns away whole;
// the decks dealt must be these substituted ones for gorge and dirac to play one game.
func (t *Tables) SwapRefusedCommander(ds []decks.Deck) ([]decks.Deck, []decks.Swap, error) {
	set, err := t.CommanderSet()
	if err != nil {
		return nil, nil, err
	}
	plays := set.Supported()
	out, swaps := decks.SwapRefused(ds, func(n string) bool {
		i, ok := set.Lookup(n)
		return ok && plays[i]
	})
	return out, swaps, nil
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
	return t.lockstep(row, set, pair, seed, maxTurns, nil, nil)
}

// StartLockstep plays pair on code's table in lockstep with gorge from a scripted
// position (start, the rules.StartSpec of an xproof start file) rather than the
// ordinary genesis, as Lockstep plays a dealt game: gorge's side opens in it through
// rules.Config.Start (Matchup.Config copies it) and dirac's kernel mirrors it from the
// same deck indices (dirac/oracle kernelStart), so both engines answer the decisions
// of one position and the row is dirac's answer beside gorge's on that start.
//
// Unlike Lockstep its answers are no one's script: an xproof start item is lowered
// from gorge's own focus game, not from this replay, so the row is the answer alone
// and Locate is not called on it (the divergence's turns are the lockstep's, not the
// item's). start must be a position dirac can mirror; the caller refuses the ones
// with no kernel equivalent (player counters, minted tokens, commander
// designations), and a counter kind the kernel does not keep is refused here, naming
// it, as any other pair the table does not hold is.
func (t *Tables) StartLockstep(gameID, code string, pair [2]decks.Deck, seed uint64, maxTurns int, start *rules.StartSpec) (Row, []decision.Intent) {
	row := Row{Game: gameID, Table: "std-" + strings.ToLower(code)}
	set, err := t.Table(code)
	if err != nil {
		return refused(row, err)
	}
	return t.lockstep(row, set, pair, seed, maxTurns, nil, start)
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
	return t.lockstep(row, set, pair, seed, maxTurns, &[2]int{pair[0].CommanderIndex(), pair[1].CommanderIndex()}, nil)
}

// refused is row as a game no table holds, for err.
func refused(row Row, err error) (Row, []decision.Intent) {
	row.Status, row.Reason = StatusNA, err.Error()
	return row, nil
}

// lockstep plays pair on set, a Commander game with commanders (indices into each
// deck) when non-nil, from the scripted position start when non-nil (StartLockstep).
func (t *Tables) lockstep(row Row, set cardset.Set, pair [2]decks.Deck, seed uint64, maxTurns int, commanders *[2]int, start *rules.StartSpec) (Row, []decision.Intent) {
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
	if start != nil {
		// A position gorge itself would not open in is a refusal, not a
		// divergence: NewGorgeSide turns a rejected start into a setup
		// divergence, which would read as dirac disagreeing with gorge.
		if err := start.Validate(dk[:]); err != nil {
			return refuse(err)
		}
		// And a position dirac's kernel loader would panic on is refused here:
		// dirac's startObjs states the contract ("A bad start panics: the loader
		// refuses these before a game is built"), so this loader refuses them by
		// name. startLoaderErr mirrors the checks it can state from outside the
		// kernel; openSide's recover is the backstop for any the kernel still
		// panics on (operator 11-24: dirac never fails the run).
		if err := startLoaderErr(start, pair); err != nil {
			return refuse(err)
		}
		m.Start = start
	}
	cfg := oracle.Config{GorgeAutopaySurface: true}
	k, err := openSide(oracle.SetSide(set, tbl, cfg), m, seed)
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

// openSide opens dirac's side of one lockstep game. The kernel's start loader panics
// rather than erroring on a bad start (dirac's startObjs: "A bad start panics: the
// loader refuses these before a game is built") and startLoaderErr answers every such
// start its words state outside the kernel; this recover is the backstop for whatever
// else opening a scripted position could panic on. The panic becomes a refusal: dirac
// never takes the run's verdict down with it (operator 11-24). A side opened with no
// start cannot reach those panics - the kernel only reads a start there - so this
// guards exactly the -start lane and costs the dealt lane nothing.
func openSide(f oracle.SideFactory, m *oracle.Matchup, seed uint64) (k oracle.Side, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("dirac kernel refused the start: %v", r)
		}
	}()
	return f(m, seed)
}

// startLoaderErr refuses, naming it, any scripted start dirac's kernel loader (dirac's
// startObjs, reached through kernelStart) would panic on: an index no deck holds, a
// card two zones name, a tap / back face / counter naming a card that is not on the
// battlefield, a negative counter pile, a seat count the two-seat game does not have
// and an attach host that is out of range or not on a battlefield. It mirrors the
// numbering kernelStart hands the kernel: each seat's battlefield entries naming an
// attach are removed from the zone (the attach claims that card instead). gorge's own
// StartSpec.Validate is no substitute - it skips an out-of-range index rather than
// refusing it - and the kernel panics instead of erroring, so this loader is the one
// place left to refuse. Checks startObjs itself defers to the table (a battlefield
// non-permanent, an unattached Aura's taps) stay with the kernel: gorge's Validate
// answers the first and openSide's recover turns the rest into refusals.
func startLoaderErr(start *rules.StartSpec, pair [2]decks.Deck) error {
	if len(start.Seats) > 2 {
		return fmt.Errorf("start: %d seats named, the game has 2", len(start.Seats))
	}
	for p, seat := range start.Seats {
		deck := len(pair[p].Cards)
		attached := make(map[int]bool, len(seat.Attach))
		for _, a := range seat.Attach {
			attached[a.Card] = true
		}
		claimed := make([]bool, deck)
		claim := func(zone string, i int) error {
			if i < 0 || i >= deck {
				return fmt.Errorf("start: seat %d %s names card index %d, the deck holds %d", p, zone, i, deck)
			}
			if claimed[i] {
				return fmt.Errorf("start: seat %d names card index %d in two zones", p, i)
			}
			claimed[i] = true
			return nil
		}
		for _, z := range []struct {
			name string
			zone []int
		}{
			{"hand", seat.Hand}, {"battlefield", seat.Battlefield}, {"graveyard", seat.Graveyard},
			{"exile", seat.Exile}, {"library", seat.Library},
		} {
			for _, i := range z.zone {
				if z.name == "battlefield" && attached[i] {
					continue // kernelStart removes the attach's card from the zone; the attach claims it
				}
				if err := claim(z.name, i); err != nil {
					return err
				}
			}
		}
		onBF := make([]bool, deck)
		for _, i := range seat.Battlefield {
			if i >= 0 && i < deck {
				onBF[i] = true
			}
		}
		for _, a := range seat.Attach {
			if err := claim("attach", a.Card); err != nil {
				return err
			}
			onBF[a.Card] = true
		}
		for _, z := range []struct {
			name string
			zone []int
		}{{"tapped", seat.Tapped}, {"back face", seat.BackFace}} {
			for _, i := range z.zone {
				if i < 0 || i >= deck || !onBF[i] {
					return fmt.Errorf("start: seat %d %s card index %d is not on the battlefield", p, z.name, i)
				}
			}
		}
		for _, c := range seat.Counters {
			if c.Card < 0 || c.Card >= deck || !onBF[c.Card] {
				return fmt.Errorf("start: seat %d counters card index %d is not on the battlefield", p, c.Card)
			}
			if c.N < 0 {
				return fmt.Errorf("start: seat %d counters %d of %s on card index %d", p, c.N, c.Kind, c.Card)
			}
		}
		for _, a := range seat.Attach {
			if a.ToSeat < 0 || a.ToSeat >= len(start.Seats) || a.To < 0 || a.To >= len(pair[a.ToSeat].Cards) {
				return fmt.Errorf("start: seat %d attach host seat %d card %d is out of range", p, a.ToSeat, a.To)
			}
			host := &start.Seats[a.ToSeat]
			if !startHasIndex(host.Battlefield, a.To) && !startHasAttachCard(host.Attach, a.To) {
				return fmt.Errorf("start: seat %d attach host card index %d of seat %d is not on the battlefield", p, a.To, a.ToSeat)
			}
		}
	}
	return nil
}

func startHasIndex(l []int, i int) bool {
	for _, x := range l {
		if x == i {
			return true
		}
	}
	return false
}

func startHasAttachCard(l []rules.StartAttach, i int) bool {
	for _, a := range l {
		if a.Card == i {
			return true
		}
	}
	return false
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

// MaxInteractionSwaps bounds the cards SwapRefusedInteraction replaces in one game.
const MaxInteractionSwaps = 4

// InteractionSwap is one interaction-refused card of a set pair replaced by a basic
// land: Count copies of Card in seat Seat's deck became Basic (swaps.jsonl records
// each). Reason is the refusal the swap answered.
type InteractionSwap struct {
	Seat   int    `json:"seat"`
	Card   string `json:"card"`
	Basic  string `json:"basic"`
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// SwapRefusedInteraction is pair with the cards the set code's table refuses as an
// interaction (cardset.InteractionRefusal: each card plays alone, the two together
// stall the game at its start) replaced by basics of the deck's colours, and each
// swap. Every round asks the table whether the pair is held (the matchup Lockstep
// builds, without playing); on an interaction refusal naming cards it replaces, in the
// seat holding it fewer times, every copy of the named card the pair holds fewest
// times, then asks again, at most MaxInteractionSwaps times. The choice reads the
// pair alone, so a seed reproduces it. A pair that is held, refused for no named
// card or any other reason, or still refused after the last swap comes back as dealt,
// with no swaps: Lockstep then reads it not_applicable as before. An error is only a
// table that cannot be had (NoTables).
func (t *Tables) SwapRefusedInteraction(code string, pair [2]decks.Deck) ([2]decks.Deck, []InteractionSwap, error) {
	set, err := t.Table(code)
	if err != nil {
		return pair, nil, err
	}
	plays := set.Supported()
	playable := func(n string) bool { i, ok := set.Lookup(n); return ok && plays[i] }
	cur := pair
	var swaps []InteractionSwap
	for round := 0; ; round++ {
		_, _, _, err := set.Matchup(entries(cur[0]), entries(cur[1]))
		var ref *cardset.InteractionRefusal
		if err == nil {
			return cur, swaps, nil
		}
		if !errors.As(err, &ref) || round >= MaxInteractionSwaps {
			return pair, nil, nil
		}
		card, seat, ok := fewestNamed(cur, ref.Cards)
		if !ok {
			return pair, nil, nil
		}
		basics := decks.BasicsOf(cur[seat].Colours, playable)
		if len(basics) == 0 {
			return pair, nil, nil
		}
		nd := cur[seat]
		nd.Cards = append([]string(nil), nd.Cards...)
		n := 0
		got := make([]int, len(basics))
		for j, c := range nd.Cards {
			if c == card {
				nd.Cards[j] = basics[n%len(basics)]
				got[n%len(basics)]++
				n++
			}
		}
		sort.Strings(nd.Cards)
		cur[seat] = nd
		for i, k := range got {
			if k > 0 {
				swaps = append(swaps, InteractionSwap{Seat: seat, Card: card, Basic: basics[i], Reason: "interaction " + ref.Reason, Count: k})
			}
		}
	}
}

// fewestNamed is the card of names the pair holds fewest times (the first of a tie, in
// names' order) and the seat holding it fewer times (seat 0 on a tie); !ok when the
// pair holds none of them.
func fewestNamed(pair [2]decks.Deck, names []string) (card string, seat int, ok bool) {
	best := 0
	for _, n := range names {
		var per [2]int
		for i, d := range pair {
			for _, c := range d.Cards {
				if c == n {
					per[i]++
				}
			}
		}
		total := per[0] + per[1]
		if total == 0 || (ok && total >= best) {
			continue
		}
		card, best, ok = n, total, true
		seat = 0
		if per[0] == 0 || (per[1] > 0 && per[1] < per[0]) {
			seat = 1
		}
	}
	return card, seat, ok
}
