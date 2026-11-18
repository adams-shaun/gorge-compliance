package fshadow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/server/matchrecord"
)

// HarnessUnmapped marks a Forge answer the follower has no gorge mapping
// for, HarnessChance a chance draw the logs do not align on: follower
// tickets, never gorge defects (adjudicate.Divergence.Harness).
const (
	HarnessUnmapped = "answer-unmapped"
	HarnessChance   = "chance-misaligned"
	HarnessSetup    = "setup-inexpressible"
)

// chanceHarness classifies an engine error: a planner failure is the
// follower's chance alignment, anything else is gorge's.
func chanceHarness(err error) string {
	if strings.Contains(err.Error(), "hypothetical") {
		return HarnessChance
	}
	return ""
}

// The hard-divergence kinds (design section 5's "what").
const (
	WhatOptions       = "options" // soft only: the option sets differ
	WhatAnswerMissing = "answer-missing"
	WhatSnapshot      = "snapshot"
	WhatKind          = "kind"
	WhatTerminal      = "terminal"
	WhatError         = "error"
)

// DefaultMaxDecisions is the follower's decision cap (design section 6).
const DefaultMaxDecisions = 20000

// Options tunes Follow.
type Options struct {
	// MaxDecisions caps the gorge decisions answered; 0 is
	// DefaultMaxDecisions. A capped game stops clean with Result.Capped.
	MaxDecisions int
}

// Soft is one option-set difference that left Forge's answer on gorge's
// list: recorded, never a stop.
type Soft struct {
	Line      int      `json:"line"`
	Ask       int      `json:"ask"`
	Seat      int      `json:"seat"`
	Kind      string   `json:"kind"` // gorge's decision.Kind
	OnlyForge []string `json:"only_forge,omitempty"`
	OnlyGorge []string `json:"only_gorge,omitempty"`
	Cards     []string `json:"cards,omitempty"`
}

// Divergence is the first hard difference, as the follower found it. Its
// file form (design section 3 step 5) is File, which WriteDivergence writes.
type Divergence struct {
	N        int
	What     string
	Kind     string // gorge's decision.Kind
	Decks    []string
	Seed     uint64
	ForgeRef string
	Line     int // the log line (1-based) the follower stopped on
	Ask      int
	Seat     int
	Field    string
	Gorge    string
	Forge    string
	Detail   string
	// Harness is non-empty when the difference is the follower's, not an
	// engine's (adjudicate's answer-unmapped, chance-misaligned, ...).
	Harness string
	// Cards are the implicated cards, sorted, at most 2.
	Cards []Implicated
	// GorgeSnapshot and ForgeSnapshot are both states at a snapshot
	// divergence.
	GorgeSnapshot *rules.OracleSnapshot
	ForgeSnapshot *rules.OracleSnapshot
	// Record is gorge's game up to the divergence: Match, the intent log and
	// the chance transcript the planners produced.
	Record *matchrecord.Record
}

// Result is one followed game.
type Result struct {
	Decisions int         `json:"decisions"` // gorge decisions answered
	Asks      int         `json:"asks"`      // log asks consumed
	Snaps     int         `json:"snaps"`     // snapshots compared
	Soft      []Soft      `json:"soft,omitempty"`
	Hard      *Divergence `json:"-"` // written on its own by WriteDivergence
	Capped    bool        `json:"capped,omitempty"`
	// SkippedAsks and SkippedSnaps count the Forge-only priority passes and
	// the snapshots of steps gorge gives no decision in (the untap step).
	SkippedAsks  int `json:"skipped_asks,omitempty"`
	SkippedSnaps int `json:"skipped_snaps,omitempty"`
	// Modes counts the modal picks (a charm's modes, an unless-pay ask) the
	// log has no line for, which the follower answered by default.
	Modes int `json:"modes,omitempty"`
	// Arranges counts the arrange decisions (scry, surveil, ...) the log has
	// no line for, which the follower answered by default.
	Arranges int `json:"arranges,omitempty"`
	// End is how the follow finished: "end" (the log's end line matched),
	// "unfinished" (Forge stopped the game early), "log" (the log ran out
	// first), "cap" or "divergence".
	End string `json:"end"`
}

// follower is one Follow's state.
type follower struct {
	log  *Log
	reg  *cards.Registry
	cfg  rules.Config
	e    *rules.Engine
	toG  map[int]state.ObjID // Forge id -> gorge ObjID
	toF  map[state.ObjID]int // gorge ObjID -> Forge id
	name map[int]string      // Forge id -> card name

	shuffles map[int][]*Shuffle // per seat, in order
	rands    []*Rand
	start    int

	setupHarness string // a header gorge's Config cannot express

	pos      int  // next unread entry
	mullKept bool // the current mull line's keep has been answered; its bottom is next
	res      Result
}

// Follow replays l in gorge. A setup failure (an unknown card, a binding
// that does not hold) is an error; every in-game difference is a Result.
func Follow(reg *cards.Registry, l *Log, opt Options) (*Result, error) {
	f := &follower{log: l, reg: reg, toG: map[int]state.ObjID{}, toF: map[state.ObjID]int{}, name: map[int]string{}, shuffles: map[int][]*Shuffle{}, start: -1}
	if err := f.setup(); err != nil {
		return nil, err
	}
	max := opt.MaxDecisions
	if max <= 0 {
		max = DefaultMaxDecisions
	}
	f.run(max)
	return &f.res, nil
}

func (f *follower) setup() error {
	h := f.log.Header
	cfg := rules.Config{Seed: h.Seed, Tokens: f.reg.Tokens, NameUniverse: f.reg.Universe(), Mulligans: h.Mulligans}
	next := state.ObjID(1)
	for _, s := range h.Seats {
		cs := make([]*cards.Card, 0, len(s.Cards))
		for k, c := range s.Cards {
			card, ok := f.reg.Lookup(c.Name)
			if !ok {
				return fmt.Errorf("fshadow: seat %d card %d: %q is not in the corpus", s.Seat, k, c.Name)
			}
			if _, dup := f.toG[c.ID]; dup {
				return fmt.Errorf("fshadow: seat %d card %d: Forge id %d is listed twice", s.Seat, k, c.ID)
			}
			cs = append(cs, card)
			// Genesis adds each seat's deck in order, seat by seat
			// (rules.newWithRNG), so the k-th card of seat s is the next id.
			f.toG[c.ID] = next
			f.toF[next] = c.ID
			f.name[c.ID] = c.Name
			next++
		}
		cfg.Names = append(cfg.Names, s.Name)
		cfg.Decks = append(cfg.Decks, cs)
	}
	switch h.GameFormat {
	case "", "constructed":
	case "commander":
		cfg.Format = rules.FormatCommander
		cfg.Commanders = make([][]int, len(h.Seats))
		for i, s := range h.Seats {
			cfg.Commanders[i] = append([]int(nil), s.Commanders...)
		}
	default:
		return fmt.Errorf("fshadow: game format %q", h.GameFormat)
	}
	for _, e := range f.log.Entries {
		switch {
		case e.Shuffle != nil:
			f.shuffles[e.Shuffle.Seat] = append(f.shuffles[e.Shuffle.Seat], e.Shuffle)
		case e.Rand != nil:
			f.rands = append(f.rands, e.Rand)
		case e.Start != nil:
			if f.start >= 0 {
				return fmt.Errorf("fshadow: line %d: a second start line", e.Line)
			}
			f.start = e.Start.Seat
		}
	}
	if f.start < 0 || f.start >= len(h.Seats) {
		return fmt.Errorf("fshadow: no start line naming a seat of %d", len(h.Seats))
	}
	if h.FreeMulligan != (len(h.Seats) > 2) {
		f.setupHarness = fmt.Sprintf("free_mulligan %v in a %d-seat game: gorge grants the free mulligan from the seat count alone (CR 103.5c) and has no Config input for it", h.FreeMulligan, len(h.Seats))
	}
	f.cfg = cfg
	e, err := rules.NewHypotheticalChancePlanned(cfg, nil, f.planShuffle, f.planChance)
	if err != nil {
		return fmt.Errorf("fshadow: genesis: %w", err)
	}
	f.e = e
	for fid, gid := range f.toG {
		o := e.G.Obj(gid)
		if o == nil || !bindsName(o.Card, f.name[fid]) {
			return fmt.Errorf("fshadow: Forge id %d (%q) does not bind to gorge object %d", fid, f.name[fid], gid)
		}
	}
	return nil
}

// bindsName reports whether Forge's name for a card is gorge's card: its
// front face's name, or for a split card (CR 709.4) both halves joined by
// " // ", which is the name Forge's card database and header use for one
// (Dusk // Dawn; an adventurer or a DFC is named by its front face).
func bindsName(c *cards.Card, name string) bool {
	if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
		return false
	}
	if strings.EqualFold(c.Faces[0].Name, name) {
		return true
	}
	if !strings.EqualFold(c.AlternateMode, "Split") {
		return false
	}
	parts := make([]string, 0, len(c.Faces))
	for _, fc := range c.Faces {
		if fc != nil {
			parts = append(parts, fc.Name)
		}
	}
	return strings.EqualFold(strings.Join(parts, " // "), name)
}

// planShuffle returns Forge's logged order for the seat's next shuffle.
func (f *follower) planShuffle(c rules.ShuffleContext) ([]state.ObjID, error) {
	ss := f.shuffles[int(c.Player)]
	if c.Ordinal >= len(ss) {
		return nil, fmt.Errorf("seat %d shuffle %d: the log has %d shuffles for it", c.Player, c.Ordinal, len(ss))
	}
	s := ss[c.Ordinal]
	if len(s.Order) != len(c.Library) {
		return nil, fmt.Errorf("seat %d shuffle %d: Forge shuffled %d cards, gorge %d", c.Player, c.Ordinal, len(s.Order), len(c.Library))
	}
	in := map[state.ObjID]bool{}
	for _, sc := range c.Library {
		in[sc.ID] = true
	}
	out := make([]state.ObjID, 0, len(s.Order))
	for _, fid := range s.Order {
		g, ok := f.toG[fid]
		if !ok || !in[g] {
			return nil, fmt.Errorf("seat %d shuffle %d: Forge id %d is not in gorge's library", c.Player, c.Ordinal, fid)
		}
		out = append(out, g)
	}
	return out, nil
}

// planChance answers gorge's non-shuffle draws: the first is the starting
// toss (rules.genesisDeal draws it before any shuffle, over every seat), the
// rest are the log's rand lines in order.
func (f *follower) planChance(c rules.ChanceContext) (int, error) {
	if c.Ordinal == 0 {
		if c.Bound != len(f.log.Header.Seats) {
			return 0, fmt.Errorf("toss: bound %d, %d seats", c.Bound, len(f.log.Header.Seats))
		}
		return f.start, nil
	}
	k := c.Ordinal - 1
	if k >= len(f.rands) {
		return 0, fmt.Errorf("draw %d (bound %d): the log has %d rand lines", k, c.Bound, len(f.rands))
	}
	return randValue(f.rands[k], k, c.Bound)
}

// randValue maps a Forge rand line onto gorge's draw. gorge's coin wins on 0
// (effects.FlipCoinWin), and the log records the face, not the call: the
// follower takes heads (1) as the flipper's win, an assumption a misaligned
// flip shows up against as a later snapshot difference. A die is 1..bound in
// Forge and 0..bound-1 drawn in gorge.
func randValue(r *Rand, k, bound int) (int, error) {
	if r.Bound != bound {
		return 0, fmt.Errorf("draw %d (%s): Forge's bound %d, gorge's %d", k, r.Kind, r.Bound, bound)
	}
	switch r.Kind {
	case "coin":
		if r.Value != 0 && r.Value != 1 {
			return 0, fmt.Errorf("draw %d: coin value %d", k, r.Value)
		}
		return 1 - r.Value, nil
	case "die":
		if r.Value < 1 || r.Value > r.Bound {
			return 0, fmt.Errorf("draw %d: die value %d of %d", k, r.Value, r.Bound)
		}
		return r.Value - 1, nil
	}
	return 0, fmt.Errorf("draw %d: rand kind %q", k, r.Kind)
}

// stepOrder is gorge's step names in turn order.
var stepOrder = map[string]int{
	"untap": 0, "upkeep": 1, "draw": 2, "main1": 3, "begin-combat": 4, "declare-attackers": 5,
	"declare-blockers": 6, "first-strike-damage": 7, "combat-damage": 7, "end-combat": 8, "main2": 9, "end": 10, "cleanup": 11,
}

// at is a (turn, step) position, comparable in game order; before turn 1
// (the mulligans) is the lowest.
func at(turn int, step string) int {
	if turn <= 0 {
		return -1
	}
	return turn*100 + stepOrder[step]
}

func (f *follower) gorgeAt() int { return at(int(f.e.G.Turn), f.e.G.Step.String()) }

func (f *follower) run(max int) {
	if err := f.e.AdvanceHypothetical(); err != nil {
		f.hard(WhatError, nil, Divergence{Detail: err.Error(), Harness: chanceHarness(err)})
		return
	}
	if f.setupHarness != "" {
		f.hard(WhatError, nil, Divergence{Line: 1, Field: "free_mulligan", Harness: HarnessSetup, Detail: f.setupHarness})
		return
	}
	for {
		// Snapshots before the next decision line describe the state gorge
		// is in now; one of a step gorge has already left (the untap step,
		// where gorge asks nothing) has no gorge counterpart, nor does a
		// Forge priority pass there.
		for f.pos < len(f.log.Entries) {
			e := f.log.Entries[f.pos]
			if e.Shuffle != nil || e.Rand != nil || e.Start != nil {
				f.pos++ // consumed by the planners
				continue
			}
			if a := e.Ask; a != nil && a.Kind == KindPriority && (a.Answer.Action == "pass" || a.Answer.Action == "") && at(a.Turn, a.Step) < f.gorgeAt() {
				f.res.SkippedAsks++
				f.pos++
				continue
			}
			if e.Snap == nil {
				break
			}
			if at(int(e.Snap.Snapshot.Turn), e.Snap.Snapshot.Step) < f.gorgeAt() && !f.e.G.Over {
				f.res.SkippedSnaps++
				f.pos++
				continue
			}
			if !f.compareSnap(e) {
				return
			}
			f.pos++
		}
		d := f.e.Pending()
		if f.e.G.Over || d == nil {
			f.terminal()
			return
		}
		if f.res.Decisions >= max {
			f.res.Capped, f.res.End = true, "cap"
			return
		}
		if f.pos >= len(f.log.Entries) {
			f.res.End = "log"
			return
		}
		e := f.log.Entries[f.pos]
		if e.End != nil && e.End.Reason == ReasonUnfinished && e.End.Winner == nil {
			f.res.End = ReasonUnfinished
			return
		}
		if e.End != nil {
			f.hard(WhatTerminal, d, Divergence{Line: e.Line, Gorge: fmt.Sprintf("%s for seat %d", d.Kind, d.Player), Forge: endString(e.End)})
			return
		}
		in, ok := f.answer(d, e)
		if !ok {
			return
		}
		if err := f.e.SubmitHypothetical(in); err != nil {
			f.hard(WhatError, d, Divergence{Line: e.Line, Detail: "submit: " + err.Error(), Harness: chanceHarness(err), Cards: f.entryCards(e)})
			return
		}
		f.res.Decisions++
		if err := f.e.AdvanceHypothetical(); err != nil {
			f.hard(WhatError, d, Divergence{Line: e.Line, Detail: "advance: " + err.Error(), Harness: chanceHarness(err)})
			return
		}
	}
}

// answer translates the log entry at f.pos into an intent for d, advancing
// f.pos past what it consumed. A false return has already set Hard.
func (f *follower) answer(d *decision.Decision, e Entry) (decision.Intent, bool) {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	if d.Kind == decision.KMulligan {
		if e.Mull == nil {
			f.hard(WhatKind, d, Divergence{Line: e.Line, Gorge: string(d.Kind), Forge: e.T + forgeKind(e)})
			return in, false
		}
		m := e.Mull
		if m.Seat != int(d.Player) {
			f.hard(WhatKind, d, Divergence{Line: e.Line, Field: "seat", Gorge: fmt.Sprint(d.Player), Forge: fmt.Sprint(m.Seat)})
			return in, false
		}
		if f.mullKept {
			// The bottoming ask of a kept hand.
			idx, missing := f.objIndices(d, m.Bottom)
			if missing != "" || len(idx) != d.Min {
				f.hard(WhatAnswerMissing, d, Divergence{Line: e.Line, Field: "bottom", Forge: fmt.Sprint(m.Bottom), Gorge: optKeys(f, d), Detail: missing})
				return in, false
			}
			in.Choices = idx
			f.mullKept = false
			f.pos++
			return in, true
		}
		want := "mulligan"
		if m.Keep {
			want = "keep"
		}
		for i, o := range d.Options {
			if o.Kind == want {
				in.Choices = []int{i}
				if m.Keep && len(m.Bottom) > 0 {
					f.mullKept = true // the same line answers the bottoming ask next
				} else {
					f.pos++
				}
				return in, true
			}
		}
		f.hard(WhatAnswerMissing, d, Divergence{Line: e.Line, Forge: want, Gorge: optKeys(f, d)})
		return in, false
	}
	if d.Kind == decision.KModes {
		// Forge's recorder logs no modal answer (a charm's modes, a
		// modal trigger's, a nested Charm): the AI answers through hooks that
		// write no ask line, so the next line belongs to a later decision.
		// Answer by default -- the first modes the ask requires, at least
		// one -- and consume no line; Forge's real choice, if it differs,
		// shows at the next snapshot.
		return f.defaultModes(d), true
	}
	if d.Kind == decision.KArrange {
		// Forge's recorder logs no scry/surveil/dig-order answer (its
		// arrangeForScry and arrangeForSurveil hooks write no ask line), so
		// the log has nothing to match this decision against and the next
		// line belongs to a later decision. Answer by default -- the
		// offered cards stay on top in offered order, as many as the ask
		// allows -- and consume no line; Forge's real choice, if it
		// differs, shows at the next library_top snapshot.
		return f.defaultArrange(d), true
	}
	if e.Ask == nil {
		f.hard(WhatKind, d, Divergence{Line: e.Line, Gorge: string(d.Kind), Forge: e.T})
		return in, false
	}
	a := e.Ask
	f.res.Asks++
	if gk, ok := gorgeKind(a.Kind); !ok {
		f.hard(WhatError, d, Divergence{Line: e.Line, Ask: a.N, Harness: HarnessUnmapped, Detail: "unsupported ask kind " + a.Kind, Forge: a.Kind, Gorge: string(d.Kind)})
		return in, false
	} else if gk != d.Kind || a.Seat != int(d.Acting()) {
		f.hard(WhatKind, d, Divergence{Line: e.Line, Ask: a.N, Gorge: fmt.Sprintf("%s seat %d", d.Kind, d.Acting()), Forge: fmt.Sprintf("%s seat %d", a.Kind, a.Seat)})
		return in, false
	}
	gorgeKeys := f.gorgeOptionKeys(d)
	f.soft(e, d, gorgeKeys)
	var err *Divergence
	switch d.Kind {
	case decision.KPriority:
		in, err = f.priority(d, a)
	case decision.KAttackers:
		in, err = f.attackers(d, a)
	case decision.KBlockers:
		in, err = f.blockers(d, a)
	case decision.KTarget:
		in, err = f.targets(d, a)
	}
	if err != nil {
		err.Line, err.Ask = e.Line, a.N
		if err.Gorge == "" {
			err.Gorge = strings.Join(gorgeKeys, " ")
		}
		if err.Cards == nil {
			err.Cards = f.entryCards(e)
		}
		f.hard(err.What, d, *err)
		return in, false
	}
	f.pos++
	return in, true
}

func (f *follower) priority(d *decision.Decision, a *Ask) (decision.Intent, *Divergence) {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	pick := func(kind string, obj state.ObjID) (decision.Intent, *Divergence) {
		for i, o := range d.Options {
			if o.Kind == kind && o.Obj == obj {
				in.Choices = []int{i}
				return in, nil
			}
		}
		return in, &Divergence{What: WhatAnswerMissing, Forge: answerKey(a.Answer)}
	}
	switch a.Answer.Action {
	case "pass", "":
		return pick("pass", 0)
	case "land":
		g, ok := f.toG[a.Answer.Card]
		if !ok {
			return in, f.unbound(a.Answer.Card)
		}
		return pick("play_land", g)
	case "cast":
		g, ok := f.toG[a.Answer.Card]
		if !ok {
			return in, f.unbound(a.Answer.Card)
		}
		return f.cast(d, a, g)
	}
	return in, &Divergence{What: WhatError, Harness: HarnessUnmapped, Detail: "unsupported priority action " + a.Answer.Action, Forge: a.Answer.Action}
}

// cast pays a cast with the payment plan whose activations tap exactly the
// sources Forge tapped. A cast Forge paid with nothing and gorge offers as a
// plain option is that option.
func (f *follower) cast(d *decision.Decision, a *Ask, obj state.ObjID) (decision.Intent, *Divergence) {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	want := make([]state.ObjID, 0, len(a.Answer.Tapped))
	for _, fid := range a.Answer.Tapped {
		g, ok := f.toG[fid]
		if !ok {
			return in, f.unbound(fid)
		}
		want = append(want, g)
	}
	sortIDs(want)
	offered := false
	for _, act := range f.e.EnsurePaymentPlanAlternatives() {
		if act.Cast.Object != obj {
			continue
		}
		offered = true
		for _, p := range act.Plans {
			got := make([]state.ObjID, 0, len(p.Activations))
			for _, x := range p.Activations {
				got = append(got, x.Source)
			}
			sortIDs(got)
			if equalIDs(got, want) {
				in.Payment = &decision.PaymentSelection{ActionID: act.ID, Plan: decision.ClonePaymentPlan(p)}
				return in, nil
			}
		}
	}
	if len(want) == 0 {
		for i, o := range d.Options {
			if o.Kind == "cast" && o.Obj == obj {
				in.Choices = []int{i}
				return in, nil
			}
		}
	}
	if offered {
		// gorge offers the cast, but no plan taps Forge's sources: the
		// design's payment class.
		return in, &Divergence{What: WhatAnswerMissing, Field: "payment", Forge: fmt.Sprintf("cast:%d tapping %v", a.Answer.Card, a.Answer.Tapped)}
	}
	return in, &Divergence{What: WhatAnswerMissing, Forge: answerKey(a.Answer)}
}

func (f *follower) attackers(d *decision.Decision, a *Ask) (decision.Intent, *Divergence) {
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}
	for _, at := range a.Answer.Attacks {
		g, ok := f.toG[at.Card]
		if !ok {
			return in, f.unbound(at.Card)
		}
		var battle state.ObjID
		if at.Defender < 0 {
			b, ok := f.toG[at.DefenderCard]
			if !ok {
				return in, f.unbound(at.DefenderCard)
			}
			battle = b
		}
		i := -1
		for k, o := range d.Options {
			if o.Kind == "attacker" && o.Obj == g && o.Battle == battle && (battle != 0 || int(o.Player) == at.Defender) {
				i = k
				break
			}
		}
		if i < 0 {
			return in, &Divergence{What: WhatAnswerMissing, Forge: attackKey(at.Card, at.Defender, at.DefenderCard)}
		}
		in.Choices = append(in.Choices, i)
	}
	return in, nil
}

func (f *follower) blockers(d *decision.Decision, a *Ask) (decision.Intent, *Divergence) {
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}
	for _, b := range a.Answer.Blocks {
		g, ok := f.toG[b.Card]
		if !ok {
			return in, f.unbound(b.Card)
		}
		at, ok := f.toG[b.Attacker]
		if !ok {
			return in, f.unbound(b.Attacker)
		}
		i := -1
		for k, o := range d.Options {
			if o.Kind == "block" && o.Obj == g && o.Attacker == at {
				i = k
				break
			}
		}
		if i < 0 {
			return in, &Divergence{What: WhatAnswerMissing, Forge: fmt.Sprintf("block:%d:%d", b.Card, b.Attacker)}
		}
		in.Choices = append(in.Choices, i)
	}
	return in, nil
}

func (f *follower) targets(d *decision.Decision, a *Ask) (decision.Intent, *Divergence) {
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}
	for _, r := range a.Answer.Targets {
		i := -1
		if id := r.Card + r.Spell; id != 0 {
			// A spell is its host card's object on the stack.
			g, ok := f.toG[id]
			if !ok {
				return in, f.unbound(id)
			}
			for k, o := range d.Options {
				if o.Obj == g {
					i = k
					break
				}
			}
		} else if r.Player != nil {
			for k, o := range d.Options {
				if o.Obj == 0 && int(o.Player) == *r.Player {
					i = k
					break
				}
			}
		}
		if i < 0 {
			return in, &Divergence{What: WhatAnswerMissing, Forge: refKey(r)}
		}
		in.Choices = append(in.Choices, i)
	}
	return in, nil
}

func (f *follower) unbound(fid int) *Divergence {
	return &Divergence{What: WhatError, Harness: HarnessUnmapped, Forge: fmt.Sprint(fid), Detail: "a Forge id the header does not bind (a token or a card made in game)"}
}

// objIndices maps Forge ids onto d's option indices by object.
func (f *follower) objIndices(d *decision.Decision, ids []int) ([]int, string) {
	out := []int{}
	for _, fid := range ids {
		g, ok := f.toG[fid]
		if !ok {
			return nil, fmt.Sprintf("unbound Forge id %d", fid)
		}
		i := -1
		for k, o := range d.Options {
			if o.Obj == g {
				i = k
				break
			}
		}
		if i < 0 {
			return nil, fmt.Sprintf("Forge id %d is not offered", fid)
		}
		out = append(out, i)
	}
	return out, ""
}

// gorgeOptionKeys is d's option set in the comparison's key form, Forge ids
// in place of ObjIDs. Priority lists land plays and casts (payment actions
// included) only: activations are not compared yet.
func (f *follower) gorgeOptionKeys(d *decision.Decision) []string {
	var out []string
	switch d.Kind {
	case decision.KPriority:
		for _, o := range d.Options {
			switch o.Kind {
			case "play_land":
				out = append(out, "land:"+f.fid(o.Obj))
			case "cast":
				out = append(out, "cast:"+f.fid(o.Obj))
			}
		}
		for _, a := range f.e.EnsurePaymentActions() {
			out = append(out, "cast:"+f.fid(a.Cast.Object))
		}
	case decision.KAttackers:
		for _, o := range d.Options {
			if o.Kind == "attacker" {
				if o.Battle != 0 {
					out = append(out, fmt.Sprintf("attack:%s:c%s", f.fid(o.Obj), f.fid(o.Battle)))
				} else {
					out = append(out, fmt.Sprintf("attack:%s:p%d", f.fid(o.Obj), o.Player))
				}
			}
		}
	case decision.KBlockers:
		for _, o := range d.Options {
			if o.Kind == "block" {
				out = append(out, fmt.Sprintf("block:%s:%s", f.fid(o.Obj), f.fid(o.Attacker)))
			}
		}
	case decision.KTarget:
		for _, o := range d.Options {
			if o.Obj != 0 && f.e != nil && f.e.G.Obj(o.Obj) != nil && f.e.G.Obj(o.Obj).Zone == state.ZStack {
				out = append(out, "spell:"+f.fid(o.Obj))
			} else if o.Obj != 0 {
				out = append(out, "card:"+f.fid(o.Obj))
			} else {
				out = append(out, fmt.Sprintf("player:%d", o.Player))
			}
		}
	}
	return dedupe(out)
}

func forgeOptionKeys(a *Ask) []string {
	var out []string
	for _, o := range a.Options {
		switch a.Kind {
		case KindPriority:
			if o.Action == "land" || o.Action == "cast" {
				out = append(out, fmt.Sprintf("%s:%d", o.Action, o.Card))
			}
		case KindAttack:
			p := -1
			if o.Player != nil {
				p = *o.Player
			}
			out = append(out, attackKey(o.Card, p, o.DefenderCard))
		case KindBlock:
			out = append(out, fmt.Sprintf("block:%d:%d", o.Card, o.Attacker))
		case KindTarget:
			out = append(out, refKey(Ref{Card: o.Card, Player: o.Player, Spell: o.Spell}))
		}
	}
	return dedupe(out)
}

// soft records the option-set difference of one ask, if any.
func (f *follower) soft(e Entry, d *decision.Decision, gorgeKeys []string) {
	onlyF, onlyG := diff(forgeOptionKeys(e.Ask), gorgeKeys)
	if len(onlyF) == 0 && len(onlyG) == 0 {
		return
	}
	f.res.Soft = append(f.res.Soft, Soft{Line: e.Line, Ask: e.Ask.N, Seat: e.Ask.Seat, Kind: string(d.Kind), OnlyForge: onlyF, OnlyGorge: onlyG, Cards: f.keyCards(append(append([]string{}, onlyF...), onlyG...))})
}

// sameLibraryDepth cuts every seat's library_top in Forge's snapshot to the
// libraryTopN cards gorge's names: Forge's SnapshotWriter names 5
// (LIBRARY_TOP_N), gorge's 3, so every snapshot of a real Forge game differed
// on p<k>.library_top at its first checkpoint (farm cmdr4 pods 13000 and
// 13002: a 3-name list against a 5-name one, the first 3 equal). The cards
// beyond gorge's depth are not compared; the ones within it still are.
func sameLibraryDepth(x rules.OracleSnapshot) rules.OracleSnapshot {
	ps := make([]rules.OracleSnapPlayer, len(x.Players))
	copy(ps, x.Players)
	for i := range ps {
		if len(ps[i].LibraryTop) > libraryTopN {
			ps[i].LibraryTop = ps[i].LibraryTop[:libraryTopN:libraryTopN]
		}
	}
	x.Players = ps
	return x
}

func (f *follower) compareSnap(e Entry) bool {
	f.res.Snaps++
	x := sameLibraryDepth(e.Snap.Snapshot)
	g := Snapshot(f.e, x.Checkpoint)
	v := oraclediff.CompareOpts(rules.OracleResult{Snapshots: []rules.OracleSnapshot{g}}, nil, oraclediff.XResult{Snapshots: []rules.OracleSnapshot{x}}, nil)
	if v.Status == oraclediff.Agree {
		return true
	}
	dv := Divergence{Line: e.Line, Field: v.Field, Gorge: v.Gorge, Forge: v.XMage, Detail: v.Msg, GorgeSnapshot: &g, ForgeSnapshot: &x}
	for _, n := range cardsInDiff(v.Gorge + "; " + v.XMage) {
		dv.Cards = append(dv.Cards, Implicated{Name: n})
	}
	f.hard(WhatSnapshot, f.e.Pending(), dv)
	return false
}

func (f *follower) terminal() {
	// Skip trailing snapshots; the next decision-free line must be the end.
	for f.pos < len(f.log.Entries) && f.log.Entries[f.pos].End == nil {
		e := f.log.Entries[f.pos]
		if e.Ask != nil || e.Mull != nil {
			f.hard(WhatTerminal, nil, Divergence{Line: e.Line, Gorge: f.gorgeEnd(), Forge: "the game goes on: " + e.T + forgeKind(e)})
			return
		}
		f.pos++
	}
	if f.pos >= len(f.log.Entries) {
		f.res.End = "log"
		return
	}
	end := f.log.Entries[f.pos]
	if end.End.Reason == ReasonUnfinished && end.End.Winner == nil && f.e.G.Over {
		f.hard(WhatTerminal, nil, Divergence{Line: end.Line, Gorge: f.gorgeEnd(), Forge: endString(end.End)})
		return
	}
	if !f.e.G.Over {
		f.hard(WhatTerminal, nil, Divergence{Line: end.Line, Gorge: f.gorgeEnd(), Forge: endString(end.End)})
		return
	}
	gw := -1
	if !f.e.G.Draw {
		gw = int(f.e.G.Winner)
	}
	fw := -1
	if end.End.Winner != nil {
		fw = *end.End.Winner
	}
	if gw != fw {
		f.hard(WhatTerminal, nil, Divergence{Line: end.Line, Field: "winner", Gorge: f.gorgeEnd(), Forge: endString(end.End)})
		return
	}
	f.res.End = "end"
}

func (f *follower) gorgeEnd() string {
	g := f.e.G
	switch {
	case !g.Over:
		return "game goes on"
	case g.Draw:
		return "draw"
	}
	return fmt.Sprintf("winner %d", g.Winner)
}

// hard records the first hard divergence, filling the game-wide fields.
func (f *follower) hard(what string, d *decision.Decision, dv Divergence) {
	h := f.log.Header
	dv.N, dv.What, dv.Seed, dv.ForgeRef = 1, what, h.Seed, h.ForgeRef
	for _, s := range h.Seats {
		dv.Decks = append(dv.Decks, s.Name)
	}
	if d != nil {
		dv.Kind = string(d.Kind)
		dv.Seat = int(d.Acting())
	}
	if dv.Line == 0 && f.pos < len(f.log.Entries) {
		dv.Line = f.log.Entries[f.pos].Line
	}
	sort.Slice(dv.Cards, func(i, j int) bool { return dv.Cards[i].Name < dv.Cards[j].Name })
	if len(dv.Cards) > 2 {
		dv.Cards = dv.Cards[:2]
	}
	if f.e != nil && f.e.L != nil && !f.e.L.NoHash {
		if r, err := matchrecord.New(f.cfg, f.e.L, matchrecord.Producer{Tool: "xengine fshadow", ForgeRef: h.ForgeRef}); err == nil {
			r.Chance = f.e.ChanceTranscript()
			dv.Record = r
		} else if dv.Detail == "" {
			dv.Detail = "record: " + err.Error()
		}
	}
	f.res.Hard = &dv
	f.res.End = "divergence"
}

// entryCards names the cards an ask's answer touches. A priority action or a
// combat declaration names no ability of the card, so its primitive is the
// action itself (action:land, action:cast, action:attack, action:block).
func (f *follower) entryCards(e Entry) []Implicated {
	if e.Ask == nil {
		return nil
	}
	a := e.Ask.Answer
	var out []Implicated
	seen := map[string]bool{}
	add := func(id int, prim string) {
		n, ok := f.name[id]
		if !ok || id == 0 || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, Implicated{Name: n, Primitive: prim})
	}
	if a.Action != "" && a.Action != "pass" {
		add(a.Card, "action:"+a.Action)
	}
	add(a.Source, "")
	for _, r := range a.Targets {
		add(r.Card+r.Spell, "")
	}
	for _, at := range a.Attacks {
		add(at.Card, "action:attack")
		add(at.DefenderCard, "")
	}
	for _, b := range a.Blocks {
		add(b.Card, "action:block")
		add(b.Attacker, "action:attack")
	}
	return out
}

// keyCards names the cards option keys carry.
func (f *follower) keyCards(keys []string) []string {
	var out []string
	for _, k := range keys {
		parts := strings.Split(k, ":")
		if len(parts) < 2 {
			continue
		}
		var id int
		if _, err := fmt.Sscan(parts[1], &id); err == nil {
			if n, ok := f.name[id]; ok {
				out = append(out, n)
			}
		}
	}
	return dedupe(out)
}

func (f *follower) fid(g state.ObjID) string {
	if id, ok := f.toF[g]; ok {
		return fmt.Sprint(id)
	}
	return fmt.Sprintf("g%d", g)
}

func optKeys(f *follower, d *decision.Decision) string {
	var out []string
	for _, o := range d.Options {
		if o.Obj != 0 {
			out = append(out, o.Kind+":"+f.fid(o.Obj))
		} else {
			out = append(out, o.Kind)
		}
	}
	return strings.Join(out, " ")
}

// defaultModes answers a modes decision the log has no line for: the first
// Min modes in offered order (at least one when the ask allows any), within
// Max and, for a budgeted pick, within MaxSum. A Repeatable ask may name a
// mode more than once, so its picks cycle through the offered modes.
func (f *follower) defaultModes(d *decision.Decision) decision.Intent {
	k := d.Min
	if k < 1 {
		k = 1
	}
	if k > d.Max {
		k = d.Max
	}
	if !d.Repeatable && k > len(d.Options) {
		k = len(d.Options)
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}
	if len(d.Options) == 0 {
		f.res.Modes++
		return in
	}
	budgeted := d.Budgeted || d.MaxSum > 0
	sum := 0
	for i := 0; len(in.Choices) < k && i < k+len(d.Options); i++ {
		c := i
		if d.Repeatable {
			c = i % len(d.Options)
		} else if c >= len(d.Options) {
			break
		}
		if budgeted && len(in.Choices) >= d.Min && sum+d.Options[c].Value > d.MaxSum {
			continue
		}
		in.Choices = append(in.Choices, c)
		sum += d.Options[c].Value
	}
	f.res.Modes++
	return in
}

// defaultArrange answers an arrange decision the log has no line for: the
// first Max offered options (at least Min), in offered order, form pile A.
func (f *follower) defaultArrange(d *decision.Decision) decision.Intent {
	k := d.Max
	if k < d.Min {
		k = d.Min
	}
	if k > len(d.Options) {
		k = len(d.Options)
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: make([]int, k)}
	for i := range in.Choices {
		in.Choices[i] = i
	}
	f.res.Arranges++
	return in
}

// gorgeKind maps Forge's ask kind to the gorge decision kind it answers.
func gorgeKind(k string) (decision.Kind, bool) {
	switch k {
	case KindPriority:
		return decision.KPriority, true
	case KindAttack:
		return decision.KAttackers, true
	case KindBlock:
		return decision.KBlockers, true
	case KindTarget:
		return decision.KTarget, true
	}
	return "", false
}

func forgeKind(e Entry) string {
	if e.Ask != nil {
		return " " + e.Ask.Kind
	}
	return ""
}

func answerKey(a Answer) string {
	if a.Action == "pass" || a.Action == "" {
		return "pass"
	}
	return fmt.Sprintf("%s:%d", a.Action, a.Card)
}

func attackKey(card, defender, defenderCard int) string {
	if defender < 0 && defenderCard != 0 {
		return fmt.Sprintf("attack:%d:c%d", card, defenderCard)
	}
	return fmt.Sprintf("attack:%d:p%d", card, defender)
}

func refKey(r Ref) string {
	if r.Spell != 0 {
		return fmt.Sprintf("spell:%d", r.Spell)
	}
	if r.Card != 0 {
		return fmt.Sprintf("card:%d", r.Card)
	}
	if r.Player != nil {
		return fmt.Sprintf("player:%d", *r.Player)
	}
	return "none"
}

func endString(e *End) string {
	if e.Winner == nil {
		return "no winner (" + e.Reason + ")"
	}
	return fmt.Sprintf("winner %d (%s)", *e.Winner, e.Reason)
}

// cardsInDiff pulls card names out of a permanents diff line ("c0 o0 Name [..]").
func cardsInDiff(s string) []string {
	var out []string
	for _, part := range strings.Split(s, "; ") {
		fs := strings.Fields(part)
		if len(fs) < 3 || !strings.HasPrefix(fs[0], "c") || !strings.HasPrefix(fs[1], "o") {
			continue
		}
		rest := strings.Join(fs[2:], " ")
		if i := strings.Index(rest, " ["); i > 0 {
			out = append(out, rest[:i])
		}
	}
	return dedupe(out)
}

func diff(a, b []string) (onlyA, onlyB []string) {
	inA, inB := map[string]bool{}, map[string]bool{}
	for _, x := range a {
		inA[x] = true
	}
	for _, x := range b {
		inB[x] = true
	}
	for _, x := range a {
		if !inB[x] {
			onlyA = append(onlyA, x)
		}
	}
	for _, x := range b {
		if !inA[x] {
			onlyB = append(onlyB, x)
		}
	}
	return onlyA, onlyB
}

func dedupe(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	out := append([]string(nil), s...)
	sort.Strings(out)
	k := 0
	for i, x := range out {
		if i == 0 || x != out[k-1] {
			out[k] = x
			k++
		}
	}
	return out[:k]
}

func sortIDs(s []state.ObjID) { sort.Slice(s, func(i, j int) bool { return s[i] < s[j] }) }

func equalIDs(a, b []state.ObjID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
