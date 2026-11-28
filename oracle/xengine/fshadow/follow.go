package fshadow

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/effects"
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
	HarnessIntent   = "intent-invalid"
)

// chanceHarness classifies an engine error: a planner failure is the
// follower's chance alignment, anything else is gorge's.
func chanceHarness(err error) string {
	if strings.Contains(err.Error(), "hypothetical") {
		return HarnessChance
	}
	return ""
}

// submitHarness classifies a SubmitHypothetical failure for the intent the
// follower built. A planner failure is its chance alignment. Otherwise the
// posed decision's own wire contract (decision.Decision.Validate, the first
// fence in rules.Engine.Submit) is the re-derivation: if it refuses the
// intent, the follower constructed choices the engine never offered, so the
// fence is the follower's construction, not a rule the engine applied. Any
// other engine error is gorge's.
func submitHarness(d *decision.Decision, in decision.Intent, err error) string {
	if h := chanceHarness(err); h != "" {
		return h
	}
	if d != nil && d.Validate(in) != nil {
		return HarnessIntent
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
	// SkippedMulls counts the state-neutral mulligan answers Forge's random
	// seat can log after its hand has emptied: AbstractMulligan.mulligan()
	// returns without a redraw, and Forge asks again.
	SkippedMulls int `json:"skipped_mulls,omitempty"`
	// Modes counts the modal picks (a charm's modes, an unless-pay ask) the
	// log has no line for, which the follower answered by default.
	Modes int `json:"modes,omitempty"`
	// Chooses counts the list-picks (discard, keep, x, sacrifice ...) the
	// log has no line for, which the follower answered by default.
	Chooses int `json:"chooses,omitempty"`
	// LibraryPicks counts the Chooses that picked library cards (a search's
	// pick) from the owner's next shuffle line: the offered cards missing
	// from it are the ones Forge took.
	LibraryPicks int `json:"library_picks,omitempty"`
	// HandPicks counts the Chooses that picked hand cards (a cleanup
	// discard) from the next snapshot line: the offered cards gone from the
	// seat's hand there are the ones Forge took.
	HandPicks int `json:"hand_picks,omitempty"`
	// DiscardElections counts the Chooses that answered a discard's "unless"
	// election "ordinary" (the full count, not one card of a type) from the
	// next snapshot line.
	DiscardElections int `json:"discard_elections,omitempty"`
	// ArrivalPicks counts the Chooses that picked cards moving into a hand (a
	// dig's take, a graveyard return, a bounced land) from the next snapshot
	// line: the offered cards new in their owner's hand there are the ones
	// Forge took.
	ArrivalPicks int `json:"arrival_picks,omitempty"`
	// FieldPicks counts the Chooses that picked cards moving onto the
	// battlefield (one land of a split search, a basic put from hand) from
	// the next snapshot line: the offered cards new among their owner's
	// permanents there are the ones Forge took.
	FieldPicks int `json:"field_picks,omitempty"`
	// Targets counts the target decisions gorge posed where the log's next
	// line is an ask of another kind (Forge chose no target through its
	// logged hook), which the follower answered by default.
	Targets int `json:"targets,omitempty"`
	// CastTargets counts the target decisions gorge posed with no target
	// line of their own that the follower answered from the targets Forge's
	// cast or activate answer carried (its priority line's "targets"): Forge
	// picks a spell's targets while casting it, records them on the cast,
	// and writes no target ask.
	CastTargets int `json:"cast_targets,omitempty"`
	// TargetParts counts the target decisions answered from a later
	// targeting part of an already consumed target line (its "target_slots"):
	// Forge writes one line over a whole sub-ability chain, gorge asks each
	// targeting link its own decision.
	TargetParts int `json:"target_parts,omitempty"`
	// Arranges counts the arrange decisions (scry, surveil, ...) the log has
	// no line for.
	Arranges int `json:"arranges,omitempty"`
	// ArrangePicks counts the Arranges answered from the next snapshot line's
	// library, hand and graveyard; the rest were answered by default.
	ArrangePicks int `json:"arrange_picks,omitempty"`
	// Optionals counts the optional-trigger yes/no decisions (an
	// OptionalDecider$ trigger) the log has no line for, which the follower
	// answered by default (yes).
	Optionals int `json:"optionals,omitempty"`
	// Orders counts the trigger-order decisions (CR 603.3b: a seat's
	// simultaneously triggered abilities) the log has no line for, which the
	// follower answered by default (the offered order).
	Orders int `json:"orders,omitempty"`
	// HandPayments counts the casts and activations gorge did not offer with
	// Forge's sources, which the follower paid by activating those sources'
	// mana abilities itself before taking the offer.
	HandPayments int `json:"hand_payments,omitempty"`
	// CommanderZones counts the CR 903.9 commander-zone decisions (put the
	// commander into the command zone, or leave it) the log has no line for,
	// which the follower answered from the next snapshot (commanderZone).
	CommanderZones int `json:"commander_zones,omitempty"`
	// OtherDefaults counts the decisions of a kind the log never carries and no
	// counter above names (a CR 616.1 replacement choice ...), which the
	// follower answered by default (the first Min options).
	OtherDefaults int `json:"other_defaults,omitempty"`
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
	planned  map[int]int        // per seat, how many of its shuffles gorge has asked the planner for
	rands    []*Rand
	start    int

	setupHarness string // a header gorge's Config cannot express

	pos        int           // next unread entry
	mullBottom map[int][]int // Forge seat -> the bottom its kept mull line carried, until gorge's bottoming ask for that seat
	// mullBottomAll marks a seat whose keep had an empty hand and no recorded
	// bottom (the recorder cleared it on a no-op mulligan line): the whole
	// hand goes to the bottom.
	mullBottomAll map[int]bool
	// chain holds the targeting parts the last consumed line carried that no
	// gorge target decision has taken yet. Consuming the next log line clears
	// it, so the parts never reach a later spell's or a trigger's target.
	chain chainTargets
	// pay are the choices still to submit of a payment by hand (payByHand);
	// the priority line it answers stays unconsumed until the last.
	pay []int
	res Result
}

// chainTargets are the targeting parts one log line carried for a spell or
// ability's sub-ability chain: a priority answer's (a cast or an activation),
// or a target line's parts after the one it answered.
type chainTargets struct {
	// parts are the untaken parts in chain order, each holding the refs Forge
	// chose for that part (none, where it chose nothing).
	parts   [][]Ref
	seat    int         // the seat that chose them
	ask     int         // the line's ask number
	obj     state.ObjID // the gorge object whose asks they answer
	ability bool        // the asks carry no Source (an activation's)
	line    int         // a target line's number; 0 for a priority answer
	host    string      // a target line's host card
}

// fromLine reports whether the parts came from a target line.
func (c chainTargets) fromLine() bool { return c.line != 0 }

// Follow replays l in gorge. A setup failure (an unknown card, a binding
// that does not hold) is an error; every in-game difference is a Result.
func Follow(reg *cards.Registry, l *Log, opt Options) (*Result, error) {
	f := &follower{log: l, reg: reg, toG: map[int]state.ObjID{}, toF: map[state.ObjID]int{}, name: map[int]string{}, shuffles: map[int][]*Shuffle{}, planned: map[int]int{}, start: -1}
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
	// The header's mulligans is Forge's counted cap: CR 103.5c's free first
	// mulligan of a multiplayer game does not count toward the number of
	// mulligans a player may take, and Forge's canMulligan is derived from the
	// counted ones, so its seats can answer one more than the cap. gorge's
	// Config.Mulligans counts the free mulligan against the allowance (its
	// crcoverage 103.5c row records the deviation), so hand it the cap plus
	// the free mulligan the header declares. The extra mulligan bottoms at
	// most the seven-card hand, which gorge's bottoming ask can express.
	mulligans := h.Mulligans
	if mulligans > 0 && h.FreeMulligan {
		mulligans++
	}
	// The live game and this replay must offer the same card names: scope the
	// name asks to the seated decks (decks.NameUniverse, the shape gorge's
	// oracleNameUniverse builds) and keep NamedCorpus whole for the derived
	// vocabularies and "copy the card named N", as the live game does.
	cfg := rules.Config{Seed: h.Seed, Tokens: f.reg.Tokens, NamedCorpus: f.reg.Universe(), Mulligans: mulligans}
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
	cfg.NameUniverse = decks.NameUniverse(cfg.Decks)
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
	f.planned[int(c.Player)] = c.Ordinal + 1
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

// unloggedKind reports whether Forge's recorder writes no line for a decision
// of kind k, so the follower answers it without reading one (answer). It names
// the kinds the log does carry (mull lines and the asks gorgeKind maps), so
// every other kind of gorge's, a later one included, is unlogged. A target
// decision is a recorded kind even though Forge writes no ask for some of them.
func unloggedKind(k decision.Kind) bool {
	switch k {
	case decision.KMulligan, decision.KPriority, decision.KAttackers, decision.KBlockers, decision.KTarget:
		return false
	}
	return true
}

// snapAheadOfUnlogged reports whether snapshot entry e describes a point past
// gorge's position while gorge waits on an unlogged decision (a cleanup
// discard, a modal pick, a scry): Forge answered it with no ask line and moved
// on, so the snapshot is the state after the answer. Comparing now would read
// gorge's pre-answer state (farm cmdr4: gorge turn 2 against Forge turn 3 at
// the cleanup discard); the follower answers by default first and compares
// the snapshot at the state the answer reaches.
func snapAheadOfUnlogged(e Entry, gorge int, d *decision.Decision) bool {
	return e.Snap != nil && unloggedKind(d.Kind) && at(int(e.Snap.Snapshot.Turn), e.Snap.Snapshot.Step) > gorge
}

// snapPrecedesOwnDeclaration reports whether snapshot entry e is Forge's
// mid-declaration snapshot of a combat declaration step: the recorder writes
// it after the declaration set the attacking/blocking flags but before the
// declaration's tap, and the ask line (next, the matching attack or block ask
// of the same step) follows it. CR 508.1f taps the attackers and 508.1k marks
// them attacking in one turn-based action, so that state is no rules-legal
// checkpoint and gorge, still at its pending declaration decision, has no
// counterpart for it. A snapshot with no declared creature is the
// pre-declaration state and still compares; the follower skips only this one; the next step's snapshot carries
// the post-declaration combat state.
func snapPrecedesOwnDeclaration(e, next Entry, gorge int, d *decision.Decision) bool {
	if e.Snap == nil || next.Ask == nil {
		return false
	}
	s := e.Snap.Snapshot
	if at(int(s.Turn), s.Step) != gorge {
		return false
	}
	declared := false
	switch {
	case d.Kind == decision.KAttackers && s.Step == "declare-attackers" && next.Ask.Kind == KindAttack:
		for _, p := range s.Permanents {
			declared = declared || p.Attacking
		}
	case d.Kind == decision.KBlockers && s.Step == "declare-blockers" && next.Ask.Kind == KindBlock:
		for _, p := range s.Permanents {
			declared = declared || p.Blocking
		}
	}
	if !declared {
		return false // a pre-declaration snapshot still compares
	}
	return next.Ask.Turn == int(s.Turn) && next.Ask.Step == s.Step
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
			// A snapshot after a target line describes the state after all of
			// that line's targets were chosen.
			if d := f.e.Pending(); d != nil && f.linePartsDue(d) {
				break
			}
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
			if d := f.e.Pending(); d != nil && d.Kind == decision.KMulligan && isBottomingAsk(d) {
				// The bottoming of a kept hand reads no line (its bottom came
				// with the keep), so the snapshot that follows that keep
				// belongs to the state after the bottoming, not before it.
				break
			}
			if at(int(e.Snap.Snapshot.Turn), e.Snap.Snapshot.Step) < f.gorgeAt() && !f.e.G.Over {
				f.res.SkippedSnaps++
				f.pos++
				continue
			}
			if pend := f.e.Pending(); pend != nil && snapAheadOfUnlogged(e, f.gorgeAt(), pend) {
				break
			}
			if pend := f.e.Pending(); pend != nil && f.pos+1 < len(f.log.Entries) && snapPrecedesOwnDeclaration(e, f.log.Entries[f.pos+1], f.gorgeAt(), pend) {
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
		if f.skipNoOpMulligan(d) {
			continue
		}
		if f.res.Decisions >= max {
			f.res.Capped, f.res.End = true, "cap"
			return
		}
		var e Entry
		if f.pos < len(f.log.Entries) {
			e = f.log.Entries[f.pos]
		}
		// An ask a consumed target line still answers reads no further line,
		// so the log's end does not stop it.
		if !f.linePartsDue(d) {
			if f.pos >= len(f.log.Entries) {
				f.res.End = "log"
				return
			}
			if e.End != nil && e.End.Reason == ReasonUnfinished && e.End.Winner == nil {
				f.res.End = ReasonUnfinished
				return
			}
			if e.End != nil {
				f.hard(WhatTerminal, d, Divergence{Line: e.Line, Gorge: fmt.Sprintf("%s for seat %d", d.Kind, d.Player), Forge: endString(e.End)})
				return
			}
		}
		in, ok := f.answer(d, e)
		if !ok {
			return
		}
		if err := f.e.SubmitHypothetical(in); err != nil {
			f.hard(WhatError, d, Divergence{Line: e.Line, Detail: "submit: " + err.Error(), Harness: submitHarness(d, in, err), Cards: f.entryCards(e)})
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
	if len(f.pay) > 0 {
		return f.payStep(d, e), true
	}
	if d.Kind == decision.KMulligan {
		if isBottomingAsk(d) {
			// Forge bottoms a London mulligan's cards right after drawing, so
			// its mull line for the kept hand carries the bottom and was
			// consumed with the keep. gorge asks every seat keep-or-mulligan
			// first and the bottoming of each kept hand after, in seat order:
			// the answer is the seat's remembered bottom, and no line is read.
			if f.mullBottomAll[int(d.Player)] {
				// The recorder cleared the bottom on a no-op mulligan line, so
				// the keep's empty hand is the record: bottom the whole hand.
				if d.Min != len(d.Options) {
					f.hard(WhatAnswerMissing, d, Divergence{Line: e.Line, Field: "bottom",
						Forge: "the whole hand", Gorge: optKeys(f, d),
						Detail: fmt.Sprintf("want %d cards on the bottom, the hand holds %d", d.Min, len(d.Options))})
					return in, false
				}
				delete(f.mullBottomAll, int(d.Player))
				for _, o := range d.Options {
					in.Choices = append(in.Choices, o.Index)
				}
				return in, true
			}
			bottom, ok := f.mullBottom[int(d.Player)]
			idx, missing := f.objIndices(d, bottom)
			if !ok || missing != "" || len(idx) != d.Min {
				f.hard(WhatAnswerMissing, d, Divergence{Line: e.Line, Field: "bottom", Forge: fmt.Sprint(bottom), Gorge: optKeys(f, d), Detail: missing})
				return in, false
			}
			delete(f.mullBottom, int(d.Player))
			in.Choices = idx
			return in, true
		}
		if e.Mull == nil {
			f.hard(WhatKind, d, Divergence{Line: e.Line, Gorge: string(d.Kind), Forge: e.T + forgeKind(e)})
			return in, false
		}
		m := e.Mull
		if m.Seat != int(d.Player) {
			f.hard(WhatKind, d, Divergence{Line: e.Line, Field: "seat", Gorge: fmt.Sprint(d.Player), Forge: fmt.Sprint(m.Seat)})
			return in, false
		}
		want := "mulligan"
		if m.Keep {
			want = "keep"
		}
		for i, o := range d.Options {
			if o.Kind == want {
				in.Choices = []int{i}
				switch {
				case m.Keep && len(m.Bottom) > 0:
					if f.mullBottom == nil {
						f.mullBottom = map[int][]int{}
					}
					f.mullBottom[m.Seat] = m.Bottom
				case m.Keep && m.emptyHand():
					// A keep with no cards in hand and no recorded bottom:
					// the recorder cleared the tucked cards when Forge logged
					// a no-op mulligan after the hand had already emptied.
					// Bottom the whole hand at the bottoming ask.
					if f.mullBottomAll == nil {
						f.mullBottomAll = map[int]bool{}
					}
					f.mullBottomAll[m.Seat] = true
				}
				f.pos++
				return in, true
			}
		}
		f.hard(WhatAnswerMissing, d, Divergence{Line: e.Line, Forge: want, Gorge: optKeys(f, d)})
		return in, false
	}
	if unloggedKind(d.Kind) {
		return f.answerUnlogged(d), true
	}
	if d.Kind == decision.KTarget && (f.linePartsDue(d) || e.Ask != nil && e.Ask.Kind != KindTarget) {
		// A target line's later parts answer their chain's asks whatever
		// follows, the next trigger's target line included (linePartsDue).
		// Otherwise Forge's recorder logs a target ask only where its targeting
		// hook is asked. A target it fixes without asking (a forced single
		// candidate, a trigger's target picked inside resolution ...) writes
		// no line, so the next line is the seat's next priority, attack or
		// block ask. Answer by default -- the first Min offered options the
		// Group contract admits -- and consume no line; Forge's real choice,
		// if it differs, shows at the next snapshot.
		in, ok, dv := f.answerChainTargets(d)
		if dv != nil {
			dv.Line, dv.Ask = e.Line, f.chain.ask
			if f.chain.fromLine() {
				dv.Line = f.chain.line
			}
			f.hard(dv.What, d, *dv)
			return in, false
		}
		if ok {
			if f.chain.fromLine() {
				f.res.TargetParts++
			} else {
				f.res.CastTargets++
			}
			return in, true
		}
		f.res.Targets++
		return f.defaultTargets(d), true
	}
	if e.Ask == nil {
		f.hard(WhatKind, d, Divergence{Line: e.Line, Gorge: string(d.Kind), Forge: e.T})
		return in, false
	}
	a := e.Ask
	f.res.Asks++
	f.chain = chainTargets{} // this line is consumed (or the follow stops): earlier targets are spent
	if gk, ok := gorgeKind(a.Kind); !ok {
		f.hard(WhatError, d, Divergence{Line: e.Line, Ask: a.N, Harness: HarnessUnmapped, Detail: "unsupported ask kind " + a.Kind, Forge: a.Kind, Gorge: string(d.Kind)})
		return in, false
	} else if gk != d.Kind || a.Seat != int(d.Acting()) {
		f.hard(WhatKind, d, Divergence{Line: e.Line, Ask: a.N, Gorge: fmt.Sprintf("%s seat %d", d.Kind, d.Acting()), Forge: fmt.Sprintf("%s seat %d", a.Kind, a.Seat), Detail: f.kindContext(a)})
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
		in, err = f.targets(d, a, e.Line)
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
	if len(f.pay) > 0 {
		return in, true // a payment by hand began: its last step consumes the line
	}
	if d.Kind == decision.KPriority {
		f.rememberCastTargets(a)
	}
	f.pos++
	return in, true
}

// answerUnlogged answers decision d of a kind the log carries no line for
// (unloggedKind): from what the log shows Forge did where it shows it, else by
// default. It consumes no line, so Forge's real choice, if it differs, shows at
// the next snapshot. The follow and a payment's trial (passesOverCostlySource)
// both answer through it.
func (f *follower) answerUnlogged(d *decision.Decision) decision.Intent {
	switch d.Kind {
	case decision.KModes:
		// Forge's recorder logs no modal answer (a charm's modes, a
		// modal trigger's, a nested Charm): the AI answers through hooks that
		// write no ask line, so the next line belongs to a later decision.
		// gorge also poses some card picks as modes (an effect's discard, a
		// connive's), which the log can still answer (cardPickChoose declines
		// every modes ask that is no such pick). Otherwise answer by
		// default: the first modes the ask requires, at least one.
		if in, ok := f.cardPickChoose(d); ok {
			return in
		}
		return f.defaultModes(d)
	case decision.KArrange:
		// Forge's recorder logs no scry/surveil/dig-order answer (its
		// arrangeForScry and arrangeForSurveil hooks write no ask line), so
		// the log has nothing to match this decision against and the next
		// line belongs to a later decision. Read the order from the next
		// snapshot where it shows (a later library_top snapshot shows a
		// difference); otherwise answer by default -- the offered cards stay
		// on top in offered order, as many as the ask allows.
		if in, ok := f.arrangeFromSnapshot(d); ok {
			return in
		}
		return f.defaultArrange(d)
	case decision.KTriggerOptional:
		// Forge's recorder logs no "may" trigger's yes/no (its
		// confirmTrigger hook writes no ask line), so the next line belongs
		// to a later decision (farm cmdr4: gorge "trigger_optional seat 0"
		// against Forge's "priority seat 0"). Answer by default -- the first
		// option, "yes", as Forge's controller accepts a trigger it may
		// decline.
		f.res.Optionals++
		return firstMin(d)
	case decision.KTriggerOrder:
		// Forge's recorder logs no trigger order (its controller orders the
		// stack through a hook that writes no ask line), so the next line
		// belongs to a later decision (farm cmdr4: gorge "trigger_order seat
		// 2" against Forge's "priority seat 0"). Answer by default -- the
		// offered order, a full permutation as Min == Max == len(Options);
		// Forge's real order, if it differs, shows at the next stack
		// snapshot.
		f.res.Orders++
		return firstMin(d)
	case decision.KChoose:
		// Forge's recorder logs only priority, attack, block and target
		// asks: a list-pick (a cleanup discard, the legend rule's keep, an
		// "as this enters" choice, a cost's sacrifice ...) is answered
		// through hooks that write no ask line, so the next line belongs to
		// a later decision. A card pick the log's later lines show is
		// answered from them; otherwise answer by default -- the first Min
		// options in offered order.
		if in, ok := f.mayShuffleChoose(d); ok {
			return in
		}
		if in, ok := f.discardElection(d); ok {
			return in
		}
		if in, ok := f.cardPickChoose(d); ok {
			return in
		}
		return f.defaultChoose(d)
	case decision.KCommanderZone:
		return f.commanderZone(d)
	}
	// Every other kind the log never carries (a CR 616.1 replacement choice,
	// a kind gorge adds later): the next line belongs to a later decision.
	// Answer by default -- the first Min options in offered order.
	f.res.OtherDefaults++
	return firstMin(d)
}

// rememberCastTargets keeps the targets a priority answer carried (a cast's
// or an activation's "targets", split by "target_slots"): Forge chooses them
// while it casts and writes no target ask, so gorge's target decisions that
// follow have no line of their own and are answered from these. An answer
// whose only parts are empty is kept too: Forge chose nothing there.
func (f *follower) rememberCastTargets(a *Ask) {
	parts := a.Answer.targetParts()
	if len(parts) == 0 {
		return
	}
	f.chain = chainTargets{parts: parts, seat: a.Seat, ask: a.N, obj: f.toG[a.Answer.Card], ability: a.Answer.Action == "activate"}
}

// castPoses reports whether target decision d is one the remembered chain's
// spell or ability poses: the same seat's, with its object as Source. gorge
// leaves an activated ability's Source 0 (the permanent stays a legal target
// of its own ability). Another Source is another object's ask (a trigger's, a
// copy's).
func (f *follower) castPoses(d *decision.Decision) bool {
	c := f.chain
	if int(d.Acting()) != c.seat {
		return false
	}
	return c.obj != 0 && d.Source == c.obj || c.ability && d.Source == 0
}

// linePartsDue reports whether d is a later ask of the chain a consumed target
// line answered, with parts of that line left. Such an ask reads no further
// line: Forge writes one target line per chain, so the next line, even another
// target line (a second trigger's, put on the stack with this one), belongs to
// a later decision. A priority answer's parts do not come first: a target line
// that follows a cast wins. An ask with no Source cannot be told from another
// ability's, so it is not claimed here.
func (f *follower) linePartsDue(d *decision.Decision) bool {
	return d.Kind == decision.KTarget && d.Source != 0 && f.chain.fromLine() && len(f.chain.parts) > 0 && f.castPoses(d)
}

// combinedModeAsk reports whether d is one ask over several targeting parts,
// one target per chosen mode: gorge groups such an ask's options by mode
// ("charm-mode-<i>", or "charm-mode-player-<seat>" where the modes must name
// different players).
func combinedModeAsk(d *decision.Decision) bool {
	for _, o := range d.Options {
		if strings.HasPrefix(o.Group, "charm-mode-") {
			return true
		}
	}
	return false
}

// answerChainTargets answers target decision d from the remembered chain's
// targeting parts, in chain order: d takes its own part (partsFor) whole
// (nothing, if Forge left it empty), and a combined mode ask takes parts until
// it has Min refs. Taken and passed-over parts are spent. ok is false (answer
// by default) when d is not the chain's ask or no part is left. A divergence
// means d is the chain's ask and the parts do not answer it: the follower
// cannot tell what Forge chose there, so it stops (a harness finding) instead
// of answering by default.
func (f *follower) answerChainTargets(d *decision.Decision) (decision.Intent, bool, *Divergence) {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	if len(f.chain.parts) == 0 || !f.castPoses(d) {
		return in, false, nil
	}
	in, rest, dv := f.answerParts(d, f.chain)
	if dv != nil {
		return in, false, dv
	}
	f.chain.parts = rest
	return in, true, nil
}

// targetingLinks counts the targeting links of the sub-ability chain from sa,
// the links Forge's recorder counts one target_slots entry each for.
func targetingLinks(sa *cards.SA) int {
	n := 0
	for ; sa != nil; sa = sa.Sub {
		if effects.TargetsOf(sa).Targeted() {
			n++
		}
	}
	return n
}

// namedLink is the link of its source's ability chain that target decision d
// names: the ResumeSA of a root or "trig_sub" ask, found on the chain of the
// stack object d.Source. nil where d names none, a modal trigger's chosen mode
// included: its chain is not the ability's, so it says nothing about d's place
// among the parts.
func (f *follower) namedLink(d *decision.Decision) *cards.SA {
	if d.ResumeSA == nil || d.ResumeKind != "" && d.ResumeKind != "trig_sub" || f.e == nil {
		return nil
	}
	o := f.e.G.Obj(d.Source)
	if o == nil {
		return nil
	}
	for sa := o.Ability; sa != nil; sa = sa.Sub {
		if cards.SAEqual(sa, d.ResumeSA) {
			return sa
		}
	}
	return nil
}

// partsFor drops from c's untaken parts the leading ones gorge posed no ask
// for, so that the first part returned is d's. gorge poses no ask for an
// optional link with no candidate, while Forge still counts its empty part, so
// d is not always the first untaken part's. why says why d's part cannot be
// told.
//
// link is the chain link d names (namedLink): the targeting links from there to
// the chain's end are the parts from d's to the last. Without one, a mandatory
// ask passes over empty parts. An optional one is the first part's if it is a
// root ask (no ResumeKind); otherwise, with that part empty and a later one
// not, either could be d's.
func (c chainTargets) partsFor(d *decision.Decision, link *cards.SA) (parts [][]Ref, why string) {
	chosen := func(p []Ref) bool { return len(p) > 0 }
	parts = c.parts
	if link != nil {
		n := targetingLinks(link)
		if n < 1 || n > len(parts) {
			return nil, fmt.Sprintf("gorge asks a link with %d targeting links to the chain's end, %d targeting parts are left", n, len(parts))
		}
		skip := len(parts) - n
		if slices.ContainsFunc(parts[:skip], chosen) {
			return nil, "gorge posed no ask for a targeting part Forge chose targets for"
		}
		return parts[skip:], ""
	}
	if d.Min > 0 {
		for len(parts) > 0 && len(parts[0]) == 0 {
			parts = parts[1:]
		}
		return parts, ""
	}
	if d.ResumeKind != "" && len(parts) > 0 && len(parts[0]) == 0 && slices.ContainsFunc(parts[1:], chosen) {
		return nil, "cannot tell whether gorge's optional ask is for the empty targeting part or a later one"
	}
	return parts, ""
}

// answerParts answers target decision d from the chain's targeting parts (see
// answerChainTargets for which parts d takes) and returns the parts left. A
// divergence carries the field and card of the parts' origin.
func (f *follower) answerParts(d *decision.Decision, c chainTargets) (decision.Intent, [][]Ref, *Divergence) {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	field, what, host := "cast_targets", "the cast's", f.name[f.toF[c.obj]]
	if c.fromLine() {
		field, what, host = "target_slots", "the logged answer's", c.host
	}
	var refs []Ref
	mismatch := func(detail string) (decision.Intent, [][]Ref, *Divergence) {
		forge := targetRefKeys(refs)
		if forge == "" {
			forge = "none"
		}
		var implicated []Implicated
		if host != "" {
			implicated = []Implicated{{Name: host}}
		}
		return in, nil, &Divergence{What: WhatAnswerMissing, Harness: HarnessUnmapped, Field: field,
			Forge: forge, Gorge: strings.Join(f.gorgeOptionKeys(d), " "), Cards: implicated, Detail: detail}
	}
	parts, why := c.partsFor(d, f.namedLink(d))
	if why != "" {
		refs = slices.Concat(c.parts...)
		return mismatch(why)
	}
	if len(parts) > 0 {
		refs, parts = parts[0], parts[1:]
	}
	for combinedModeAsk(d) && len(refs) < d.Min && len(parts) > 0 {
		refs, parts = append(refs, parts[0]...), parts[1:]
	}
	if len(refs) < d.Min || d.Max > 0 && len(refs) > d.Max {
		return mismatch(fmt.Sprintf("%s next targeting part holds %d targets, gorge asks %d to %d", what, len(refs), d.Min, d.Max))
	}
	cands := make([][]int, len(refs))
	for i, r := range refs {
		cand, bound := f.targetCandidates(d, r)
		if !bound {
			dv := f.unbound(r.Card + r.Spell)
			dv.Field = field
			return in, nil, dv
		}
		if len(cand) == 0 {
			return mismatch("gorge does not offer " + what + " target " + refKey(r))
		}
		cands[i] = cand
	}
	choices, ok := assignTargetOptions(d, cands)
	if !ok {
		return mismatch("no assignment of " + what + " targets to one option per Group")
	}
	in.Choices = choices
	return in, parts, nil
}

// kindContext places a kind divergence: where Forge's ask sits (turn, step,
// what it answered) and where gorge is (turn, step, active seat, stack depth),
// so a same-kind seat mismatch (gorge "priority seat 1" against Forge's
// "priority seat 0") can be read as an ordering or a missing-line difference
// without the full log. It rides in the divergence's msg, never in its class.
func (f *follower) kindContext(a *Ask) string {
	if f.e == nil || f.e.G == nil {
		return ""
	}
	g := f.e.G
	return fmt.Sprintf("forge ask t%d:%s answer %q; gorge t%d:%s active seat %d, stack %d",
		a.Turn, a.Step, a.Answer.Action, g.Turn, g.Step, g.Active, len(g.Stack))
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
		// A land line names no face and taps nothing, so the mark is all
		// Forge's line says: one candidate way is followed, two stop.
		ws, gorge, dv := forgeWays(d.Options, "play_land", g, a)
		if dv != nil {
			return in, dv
		}
		if len(ws) > 1 {
			return in, unnamedWay(a, gorge, len(ws))
		}
		if i := ws[0].option(d, g); i >= 0 {
			in.Choices = []int{i}
			return in, nil
		}
		return in, &Divergence{What: WhatAnswerMissing, Forge: answerKey(a.Answer)}
	case "cast":
		g, ok := f.toG[a.Answer.Card]
		if !ok {
			return in, f.unbound(a.Answer.Card)
		}
		return f.cast(d, a, g)
	case "activate":
		g, ok := f.toG[a.Answer.Card]
		if !ok {
			return in, f.unbound(a.Answer.Card)
		}
		return f.activate(d, a, g)
	}
	return in, &Divergence{What: WhatError, Harness: HarnessUnmapped, Detail: "unsupported priority action " + a.Answer.Action, Forge: a.Answer.Action}
}

// activate answers a Forge "activate" with gorge's "ability" option of the
// same card (a printed activated ability that is not a mana tap: Evolving
// Wilds' sacrifice, a manland's animation). An offered ability Forge paid by
// tapping sources is paid by hand from those sources first (handActivation),
// so gorge's payment does not tap others; the direct pick is the fallback.
// The follow stops instead where gorge's own payment passes over a source
// Forge tapped whose mana can cost another card (passesOverCostlySource):
// Forge's Phyrexian Tower sacrificing a creature the line does not name. An
// ability both engines list (forgeAbility) that gorge does not offer is paid
// by hand from the sources Forge tapped, if that makes gorge offer it.
func (f *follower) activate(d *decision.Decision, a *Ask, obj state.ObjID) (decision.Intent, *Divergence) {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	offered := abilityOption(d, a, obj)
	pick, known := offered, false
	var want decision.Option
	// Every ability Forge listed is offered: its rank among them names it.
	if offered < 0 || len(forgeAbilityIDs(a)) != len(abilityOptions(d.Options, obj)) {
		want, known = forgeAbility(f.e, a, obj)
		if known {
			pick = optionOf(d, want)
		}
	}
	if pick >= 0 {
		// Forge's tapped sources, when all bound, pay it as Forge did.
		if tapped, dv := f.boundTapped(a); dv == nil && len(tapped) > 0 {
			chosen := d.Options[pick]
			offer := func(d *decision.Decision) int { return optionOf(d, chosen) }
			if paid, ok := f.handActivation(d, a, obj, tapped, offer); ok {
				return paid, nil
			}
			if f.passesOverCostlySource(d, a, pick) {
				// gorge offers the ability, but neither Forge's sources by
				// hand nor its own payment pay it as Forge did.
				return in, &Divergence{What: WhatAnswerMissing, Field: "payment", Forge: fmt.Sprintf("activate:%d tapping %v", a.Answer.Card, a.Answer.Tapped)}
			}
		}
		in.Choices = []int{pick}
		return in, nil
	}
	if known {
		// Only a payment by hand reads the tapped ids.
		tapped, dv := f.boundTapped(a)
		if dv != nil {
			return in, dv
		}
		offer := func(d *decision.Decision) int { return optionOf(d, want) }
		if paid, ok := f.handActivation(d, a, obj, tapped, offer); ok {
			return paid, nil
		}
	}
	return in, &Divergence{What: WhatAnswerMissing, Forge: answerKey(a.Answer)}
}

// boundTapped is gorge's objects for the ids Forge's answer a lists as
// tapped, in its order; the unbound divergence of the first id the header
// does not bind.
func (f *follower) boundTapped(a *Ask) ([]state.ObjID, *Divergence) {
	tapped := make([]state.ObjID, 0, len(a.Answer.Tapped))
	for _, fid := range a.Answer.Tapped {
		g, ok := f.toG[fid]
		if !ok {
			return nil, f.unbound(fid)
		}
		tapped = append(tapped, g)
	}
	return tapped, nil
}

// handActivation pays the activation of obj's ability offer picks by hand
// from tapped (payByHand). Forge's tapped list holds {T} costs too: the
// ability's own card may be on it for its cost, not for mana, so it is tried
// once more without it.
func (f *follower) handActivation(d *decision.Decision, a *Ask, obj state.ObjID, tapped []state.ObjID, offer func(*decision.Decision) int) (decision.Intent, bool) {
	if paid, ok := f.payByHand(d, a, tapped, offer); ok {
		return paid, true
	}
	if i := slices.Index(tapped, obj); i >= 0 {
		return f.payByHand(d, a, slices.Delete(slices.Clone(tapped), i, i+1), offer)
	}
	return decision.Intent{}, false
}

// abilityOption is the index of d's "ability" option matching Forge's
// activate answer a on obj, or -1. Forge's ability id is a SpellAbility id,
// not an index into the card's faces, so it cannot name gorge's option
// directly. A card with one ability option has one answer; with several, the
// ability's rank among the activate options Forge listed for the card picks
// the option of the same rank, when both sides list as many.
func abilityOption(d *decision.Decision, a *Ask, obj state.ObjID) int {
	cands := abilityOptions(d.Options, obj)
	switch {
	case len(cands) == 1:
		return cands[0]
	case len(cands) > 1:
		if ids := forgeAbilityIDs(a); len(ids) == len(cands) {
			for r, id := range ids {
				if id == a.Answer.abilityID() {
					return cands[r]
				}
			}
		}
	}
	return -1
}

// A way is one way gorge has to cast or play a card: the Kind of its priority
// option ("cast", "play_land"), the option's Mode (an Adventure's
// "adventure_alt", a modal back face's "modal_land", "kicked", ...) and its
// AltCostIndex. gorge lists every way as an option of the same object, and
// the follower takes one only when Forge's line names it (forgeWays,
// abilityWays, cast): taking the first offered would cast Stomp for
// Bonecrusher Giant, or play a Pathway's other face.
type way struct {
	kind, mode string
	alt        int
}

// wayOf is o's way.
func wayOf(o decision.Option) way { return way{o.Kind, o.Mode, o.AltCostIndex} }

// option is the index of d's first option of obj in way w, or -1.
func (w way) option(d *decision.Decision, obj state.ObjID) int {
	return slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Obj == obj && wayOf(o) == w })
}

// actionWays is the ways among ws payment action act may be: its base
// option's way, or (an action that needs floating mana first has none, and
// PlannedCast carries no alternative-cost index) every way of ws in the kind
// and mode its planned cast begins as, whatever its AltCostIndex.
func actionWays(d *decision.Decision, act decision.PaymentAction, ws []way) []way {
	if i := act.BaseOptionIndex; i != nil && *i >= 0 && *i < len(d.Options) {
		if w := wayOf(d.Options[*i]); slices.Contains(ws, w) {
			return []way{w}
		}
		return nil
	}
	var out []way
	for _, w := range ws {
		if w.kind == "cast" && w.mode == act.Cast.CastMode() {
			out = append(out, w)
		}
	}
	return out
}

// actionWay is the way of payment action act: its base option's, or the way
// its planned cast begins as, at alternative-cost index 0.
func actionWay(d *decision.Decision, act decision.PaymentAction) way {
	if i := act.BaseOptionIndex; i != nil && *i >= 0 && *i < len(d.Options) {
		return wayOf(d.Options[*i])
	}
	return way{kind: "cast", mode: act.Cast.CastMode()}
}

// forgeMark is the mode Forge's recorder writes on a cast gorge makes in mode
// m: it marks a kicked and a flashback cast and nothing else (ShadowLog's
// BridgeController.mode), so every other way, a land play's included, is
// unmarked.
func forgeMark(m string) string {
	switch m {
	case "kicked", "multikicked":
		return "kicked"
	case "flashback":
		return "flashback"
	}
	return ""
}

// forgeWays is obj's ways of kind in opts that carry the mark of Forge's
// answer a (its candidates), in option order, and every way of obj of kind
// as "mode/alt" keys for a divergence. Forge's line names a face or an
// alternative neither in its label ("Cast <card>", "Play <card>") nor in its
// mode, so the mark only narrows the ways; what else Forge's line holds (a
// cast's ability index, abilityWays; the sources it tapped) must pick one out.
// When no way carries the mark, gorge has no way to do what Forge did. opts
// must hold every way gorge has (unconstrained for a cast; a land play costs no
// mana, so the pending options for a land).
func forgeWays(opts []decision.Option, kind string, obj state.ObjID, a *Ask) ([]way, string, *Divergence) {
	var named []way
	var all []string
	for _, o := range opts {
		if o.Kind != kind || o.Obj != obj {
			continue
		}
		w := wayOf(o)
		all = append(all, fmt.Sprintf("%s/%d", w.mode, w.alt))
		if forgeMark(w.mode) == a.Answer.Mode && !slices.Contains(named, w) {
			named = append(named, w)
		}
	}
	gorge := strings.Join(all, " ")
	if len(named) == 0 {
		return nil, gorge, &Divergence{What: WhatAnswerMissing, Forge: answerKey(a.Answer), Gorge: gorge}
	}
	return named, gorge, nil
}

// frontWay is gorge's ordinary cast of a card: its front face, no mode, no
// alternative cost.
var frontWay = way{kind: "cast"}

// abilityWays narrows ws, the ways Forge's cast answer a may name, by the
// abilities of a's line. The recorder indexes a cast among the host card's own
// spell abilities: 0 is the front face's ordinary cast, and every other cast
// of the card has another index or none (an Adventure, a face-down or evoke
// cast, a split card's right half, a variant Forge copied). So when exactly
// one of the casts a lists with the answer's mark is index 0:
//   - an answer of index 0 names frontWay;
//   - any other answer is not frontWay, which is ruled out when a lists as
//     many such casts as ws holds ways. With lists of unlike length gorge's
//     other ways do not pair with Forge's: a variant Forge lists may be a pick
//     gorge asks during frontWay's cast.
//
// ws is returned as it is when it has no frontWay or Forge lists no index 0 (a
// commander's cast, a may-play's), and a narrowing never empties it.
func abilityWays(ws []way, a *Ask) []way {
	front := slices.Index(ws, frontWay)
	if front < 0 {
		return ws
	}
	listed, first := 0, 0
	for _, o := range a.Options {
		if o.Action != "cast" || o.Card != a.Answer.Card || o.Mode != a.Answer.Mode {
			continue
		}
		listed++
		if o.abilityID() == 0 {
			first++
		}
	}
	switch {
	case first != 1:
		return ws
	case a.Answer.abilityID() == 0:
		return []way{frontWay}
	case listed != len(ws) || len(ws) < 2:
		return ws
	}
	return slices.Delete(slices.Clone(ws), front, front+1)
}

// unnamedWay is the stop when n of gorge's ways match all of Forge's line: the
// follower cannot tell which one Forge's seat took (a harness divergence: the
// recorder's line, not gorge, is short).
func unnamedWay(a *Ask, gorge string, n int) *Divergence {
	return &Divergence{What: WhatError, Harness: HarnessUnmapped, Forge: answerKey(a.Answer), Gorge: gorge,
		Detail: fmt.Sprintf("forge's %s line does not say which of gorge's %d ways (mode/alt_cost_index) its seat took", a.Answer.Action, n)}
}

// cast pays a cast with the payment plan whose activations tap exactly the
// sources Forge tapped, among the plans of the ways Forge's line may name
// (forgeWays: its mark narrows them; abilityWays: so does its ability index),
// passing over a plan that picks a permanent to sacrifice (sacrificesAnother).
// It takes the cast when exactly one candidate way has such a plan, and stops
// (unnamedWay) when two or more do: on a line without an ability index to tell
// them apart, Bonecrusher Giant's creature and Stomp plans both paid from Sol
// Ring and a Mountain. A cast Forge paid with nothing is likewise the option of
// the one candidate way gorge offers as a cast option. A card with only one way
// to be cast (soleCast) and no such plan is paid by hand from Forge's sources,
// if that makes gorge offer it.
func (f *follower) cast(d *decision.Decision, a *Ask, obj state.ObjID) (decision.Intent, *Divergence) {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	tapped, dv := f.boundTapped(a)
	if dv != nil {
		return in, dv
	}
	want := slices.Clone(tapped)
	sortIDs(want)
	ws, gorge, dv := forgeWays(unconstrained(f.e), "cast", obj, a)
	if dv != nil {
		return in, dv
	}
	ws = abilityWays(ws, a)
	offered := false
	var matched []way
	var pay *decision.PaymentSelection
	for _, act := range f.e.EnsurePaymentPlanAlternatives() {
		if act.Cast.Object != obj {
			continue
		}
		aw := actionWays(d, act, ws)
		if len(aw) == 0 {
			continue
		}
		offered = true
		for _, p := range act.Plans {
			if sacrificesAnother(p) {
				continue
			}
			got := make([]state.ObjID, 0, len(p.Activations))
			for _, x := range p.Activations {
				got = append(got, x.Source)
			}
			sortIDs(got)
			if !equalIDs(got, want) {
				continue
			}
			if pay == nil {
				pay = &decision.PaymentSelection{ActionID: act.ID, Plan: decision.ClonePaymentPlan(p)}
			}
			for _, w := range aw {
				if !slices.Contains(matched, w) {
					matched = append(matched, w)
				}
			}
			break
		}
	}
	switch {
	case len(matched) > 1:
		return in, unnamedWay(a, gorge, len(matched))
	case len(matched) == 1:
		in.Payment = pay
		return in, nil
	}
	if len(want) == 0 {
		var opts []int
		for _, w := range ws {
			if i := w.option(d, obj); i >= 0 {
				opts = append(opts, i)
			}
		}
		switch len(opts) {
		case 1:
			in.Choices = opts
			return in, nil
		case 0:
		default:
			return in, unnamedWay(a, gorge, len(opts))
		}
	}
	if soleCast(f.e, a, obj) {
		offer := func(d *decision.Decision) int { return plainCast(d.Options, obj) }
		if paid, ok := f.payByHand(d, a, tapped, offer); ok {
			return paid, nil
		}
	}
	if offered {
		// gorge offers the cast, but neither a plan nor Forge's sources by
		// hand pay it: the design's payment class.
		return in, &Divergence{What: WhatAnswerMissing, Field: "payment", Forge: fmt.Sprintf("cast:%d tapping %v", a.Answer.Card, a.Answer.Tapped)}
	}
	return in, &Divergence{What: WhatAnswerMissing, Forge: answerKey(a.Answer)}
}

// sacrificesAnother reports whether a step of p sacrifices a permanent other
// than its source (Phyrexian Tower's creature). gorge's planner picks that
// permanent and Forge's line, which lists only taps, does not name the one
// Forge picked, so p is not known to be Forge's payment. A step's cost to its
// own source (a Treasure's sacrifice, a painland's damage) names nothing the
// line leaves open.
func sacrificesAnother(p decision.PaymentPlan) bool {
	return slices.ContainsFunc(p.Activations, func(x decision.PaymentActivation) bool { return len(x.Sacrifices) > 0 })
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

// targets answers d from the target line numbered line.
func (f *follower) targets(d *decision.Decision, a *Ask, line int) (decision.Intent, *Divergence) {
	if len(a.Answer.TargetSlots) > 1 {
		return f.targetParts(d, a, line)
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}
	cands := make([][]int, len(a.Answer.Targets))
	for i, r := range a.Answer.Targets {
		var ok bool
		cands[i], ok = f.targetCandidates(d, r)
		if !ok {
			return in, f.unbound(r.Card + r.Spell)
		}
		if len(cands[i]) == 0 {
			return in, &Divergence{What: WhatAnswerMissing, Forge: refKey(r)}
		}
	}
	choices, ok := assignTargetOptions(d, cands)
	if !ok {
		return in, &Divergence{What: WhatAnswerMissing, Forge: targetRefKeys(a.Answer.Targets),
			Detail: "no assignment of the answer to one option per Group"}
	}
	in.Choices = choices
	return in, nil
}

// targetParts answers a logged target line whose target_slots hold several
// targeting parts: Forge's recorder writes one line over a spell or ability's
// whole sub-ability chain, while gorge asks each targeting link its own
// decision. d takes its part as a cast's ask does, and the parts left answer
// the chain's later asks (linePartsDue; an ask with no Source, only while the
// next line is not a target line).
func (f *follower) targetParts(d *decision.Decision, a *Ask, line int) (decision.Intent, *Divergence) {
	c := chainTargets{parts: a.Answer.targetParts(), seat: a.Seat, ask: a.N, obj: d.Source,
		ability: d.Source == 0, line: line, host: f.name[a.Answer.Source]}
	in, rest, dv := f.answerParts(d, c)
	if dv != nil {
		return in, dv
	}
	c.parts = rest
	f.chain = c
	return in, nil
}

// targetCandidates is the offered option indices answer ref r names, in offer
// order: an object (a spell is its host card's object on the stack) or a
// player. ok is false when the Forge id is unbound, which is a harness gap
// (unbound), not an answer-missing.
func (f *follower) targetCandidates(d *decision.Decision, r Ref) (out []int, ok bool) {
	if id := r.Card + r.Spell; id != 0 {
		g, bound := f.toG[id]
		if !bound {
			return nil, false
		}
		for k, o := range d.Options {
			if o.Obj == g {
				out = append(out, k)
			}
		}
		return out, true
	}
	if r.Player != nil {
		for k, o := range d.Options {
			if o.Obj == 0 && int(o.Player) == *r.Player {
				out = append(out, k)
			}
		}
	}
	return out, true
}

// targetOptionsGrouped reports whether the offer carries the per-Option Group
// exclusivity contract. Both target intent builders read this one predicate,
// so a Group-less ask can never be routed through the grouped assignment.
func targetOptionsGrouped(d *decision.Decision) bool {
	for _, o := range d.Options {
		if o.Group != "" {
			return true
		}
	}
	return false
}

// assignTargetOptions maps each selected ref onto one offered target option.
// With a Group-less decision that is first-match in offer order. A decision
// with Option.Group is bound by Decision.Validate's exclusivity contract
// (at most GroupCapFor(group) picks per group) -- most visibly a combined
// charm ask's one charm-mode-N group per target-bearing mode -- and the same
// permanent can be a legal target in several modes. First-match is not enough
// there: it can put two picks in one mode's group, which the engine rejects
// with "choices N and M are mutually exclusive (group "charm-mode-0")". This
// is a bipartite assignment that keeps first-match preference but backtracks
// an earlier ref to another mode's option rather than exceed a group's cap.
func assignTargetOptions(d *decision.Decision, cands [][]int) ([]int, bool) {
	chosen := make([]int, len(cands))
	if !targetOptionsGrouped(d) {
		for i := range cands {
			chosen[i] = cands[i][0]
		}
		return chosen, true
	}
	for i := range chosen {
		chosen[i] = -1
	}
	used := map[string]int{}
	var assign func(int) bool
	assign = func(i int) bool {
		if i == len(cands) {
			return true
		}
		for _, c := range cands[i] {
			o := d.Options[c]
			if !d.GroupAdmits(used, o.Group) {
				continue
			}
			dup := false
			if !d.Repeatable {
				for _, prev := range chosen[:i] {
					if prev == c {
						dup = true
						break
					}
				}
			}
			if dup {
				continue
			}
			chosen[i] = c
			if o.Group != "" {
				used[o.Group]++
			}
			if assign(i + 1) {
				return true
			}
			if o.Group != "" {
				used[o.Group]--
			}
			chosen[i] = -1
		}
		return false
	}
	if !assign(0) {
		return nil, false
	}
	return chosen, true
}

// targetRefKeys is a target answer's refs in key form, for a divergence that
// has no single ref to name.
func targetRefKeys(refs []Ref) string {
	keys := make([]string, len(refs))
	for i, r := range refs {
		keys[i] = refKey(r)
	}
	return strings.Join(keys, " ")
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
	f.res.Modes++
	return modeIntent(d)
}

// modeIntent is defaultModes' answer, uncounted.
func modeIntent(d *decision.Decision) decision.Intent {
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
	return in
}

// defaultChoose answers a choose decision the log has no line for: the first
// Min options in offered order (none when Min is 0), capped at Max and at the
// offered count.
func (f *follower) defaultChoose(d *decision.Decision) decision.Intent {
	f.res.Chooses++
	return firstMin(d)
}

// defaultTargets answers a target decision the log has no line for. A
// Group-less ask is firstMin unchanged: its slots are not exclusive, so the
// first Min offered options are always a valid answer (and giving every slot
// the same offer list would answer all-zero, which Validate rejects as a
// duplicate choice). A grouped ask needs the Group contract: a combined
// charm's options are emitted mode by mode, so a plain prefix can lie inside
// one charm-mode Group and the engine rejects it with "choices 0 and 1 are
// mutually exclusive (group \"charm-mode-0\")". That path walks the offer and
// skips an option whose Group is already at its cap.
func (f *follower) defaultTargets(d *decision.Decision) decision.Intent {
	if !targetOptionsGrouped(d) {
		return firstMin(d)
	}
	k := d.Min
	if d.Max > 0 && k > d.Max {
		k = d.Max
	}
	if k > len(d.Options) {
		k = len(d.Options)
	}
	if k < 0 {
		k = 0
	}
	all := make([]int, len(d.Options))
	for i := range all {
		all[i] = i
	}
	cands := make([][]int, k)
	for i := range cands {
		cands[i] = all
	}
	choices, _ := assignTargetOptions(d, cands)
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
}

// firstMin is the intent picking the first Min options in offered order.
func firstMin(d *decision.Decision) decision.Intent {
	k := d.Min
	if d.Max > 0 && k > d.Max {
		k = d.Max
	}
	if k > len(d.Options) {
		k = len(d.Options)
	}
	if k < 0 {
		k = 0
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: make([]int, k)}
	for i := range in.Choices {
		in.Choices[i] = i
	}
	return in
}

// mayShuffleChoose answers a "you may shuffle" election (the resume kinds
// ending in "mayshuffle": a search or rearrange effect's optional shuffle)
// from the shuffle lines instead of by default. Forge's recorder writes no
// ask line for it, but a shuffle Forge took leaves a shuffle line and a
// shuffle it declined leaves none. Answering yes by default (the first
// option) made gorge shuffle where Forge did not, so gorge's next planned
// shuffle consumed a LATER Forge shuffle of a library a draw smaller
// (chance-misaligned, "Forge shuffled 90 cards, gorge 91"). The election is
// taken (yes) exactly when some seat's next unconsumed Forge shuffle holds
// every card now in that seat's library -- the library at the shuffle is
// that one plus whatever the effect moves in first -- and declined (no)
// when none does, the next Forge shuffle then being a later one.
func (f *follower) mayShuffleChoose(d *decision.Decision) (decision.Intent, bool) {
	if !strings.HasSuffix(d.ResumeKind, "mayshuffle") {
		return decision.Intent{}, false
	}
	yes, no := -1, -1
	for i, o := range d.Options {
		switch o.Kind {
		case "yes":
			yes = i
		case "no":
			no = i
		}
	}
	if yes < 0 || no < 0 || d.Min != 1 || d.Max != 1 {
		return decision.Intent{}, false
	}
	pick := no
	if f.shuffleLogged() {
		pick = yes
	}
	f.res.Chooses++
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}, true
}

// discardElection answers "ordinary" to gorge's "discard_unless" election
// (Thirst for Knowledge: "discard two cards unless you discard an artifact
// card"), which Forge's recorder writes no ask line for, only where the next
// snapshot rules "unless" out: at least the ordinary count of cards left the
// seat's hand, and no hand card of the ability's UnlessType$ is among them or
// new in the seat's graveyard or exile (where a discard shows even when a
// card of that name is drawn after it). The count alone is no evidence: after
// an "unless" discard a card also leaves a hand by being cycled, cast, paid
// as a cost or discarded again. Everything else keeps the default, "unless",
// including an ordinary discard that takes a card of that type, which the
// snapshot cannot tell from "unless" and one of those departures.
func (f *follower) discardElection(d *decision.Decision) (decision.Intent, bool) {
	if d.ResumeKind != "discard_unless" || d.ResumeSA == nil || f.e == nil || f.e.G == nil {
		return decision.Intent{}, false
	}
	ordinary := slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Kind == "ordinary" })
	count, err := strconv.Atoi(strings.TrimSpace(d.ResumeSA.ParamStr(cards.PKNumCards)))
	snap := f.nextSnapshot()
	if ordinary < 0 || err != nil || snap == nil {
		return decision.Intent{}, false
	}
	now := Snapshot(f.e, "")
	before, ok := snapPlayer(&now, int(d.Player))
	after, ok2 := snapPlayer(snap, int(d.Player))
	if !ok || !ok2 {
		return decision.Intent{}, false
	}
	hand := f.e.G.Zone(state.ZHand, d.Player)
	gone := surplus(before.Hand, after.Hand)
	if need := min(count, len(hand)); need < 1 || len(gone) < need {
		return decision.Intent{}, false
	}
	moved := slices.Concat(gone, surplus(after.Graveyard, before.Graveyard), surplus(after.Exile, before.Exile))
	unless := strings.Split(d.ResumeSA.ParamStr(cards.PKUnlessType), ",")
	for _, id := range hand {
		if slices.Contains(moved, objName(f.e.G.Obj(id))) && slices.ContainsFunc(unless, func(spec string) bool {
			return effects.MatchesSpecFrom(f.e.G, strings.TrimSpace(spec), id, d.Player, d.Source)
		}) {
			return decision.Intent{}, false
		}
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{ordinary}}
	if d.Validate(in) != nil {
		return decision.Intent{}, false
	}
	f.res.Chooses++
	f.res.DiscardElections++
	return in, true
}

// libraryPickChoose answers a pick of library cards (a library search's
// "choose up to N", whatever its resume kind) from the owner's next
// unconsumed Forge shuffle line. Forge's recorder writes no ask line for the
// pick, but a search shuffles afterwards and that shuffle line names the
// cards left in the library: the picked cards are exactly the offered ones
// missing from it. Answering by default (firstMin, nothing when Min is 0)
// left the land in gorge's library, so the planned shuffle met a library one
// card larger than Forge's (chance-misaligned, "Forge shuffled 89 cards,
// gorge 90"; farm cmdr4 pod 14201, Terramorphic Expanse).
//
// It applies to a pick cardPickChoose routes out of a library (pickRouteOf:
// the chosen cards leave it and the others stay) and whose every option is a
// card in one seat's library; an ask with no such route never gets here, so a
// new search-shaped pick needs its route first. It answers only when every
// card of gorge's library absent from that shuffle is offered (a shuffle that
// is a later one, after draws or other moves, misses cards not offered and
// falls back to the default) and the picks form a valid answer.
func (f *follower) libraryPickChoose(d *decision.Decision) (decision.Intent, bool) {
	if len(d.Options) == 0 || f.e == nil || f.e.G == nil {
		return decision.Intent{}, false
	}
	seat := -1
	var lib []state.ObjID
	inLib := map[state.ObjID]bool{}
	for _, o := range d.Options {
		if o.Obj == 0 {
			return decision.Intent{}, false
		}
		if seat < 0 {
			for s := range f.e.G.Players {
				if slices.Contains(f.e.G.Zone(state.ZLibrary, state.PlayerID(s)), o.Obj) {
					seat = s
					break
				}
			}
			if seat < 0 {
				return decision.Intent{}, false
			}
			lib = f.e.G.Zone(state.ZLibrary, state.PlayerID(seat))
			for _, id := range lib {
				inLib[id] = true
			}
		}
		if !inLib[o.Obj] {
			return decision.Intent{}, false
		}
	}
	ss := f.shuffles[seat]
	n := f.planned[seat]
	if n >= len(ss) {
		return decision.Intent{}, false
	}
	kept := map[state.ObjID]bool{}
	for _, fid := range ss[n].Order {
		if g, ok := f.toG[fid]; ok {
			kept[g] = true
		}
	}
	offered := map[state.ObjID]int{}
	for i, o := range d.Options {
		if _, dup := offered[o.Obj]; !dup {
			offered[o.Obj] = i
		}
	}
	var picks []int
	for _, id := range lib {
		if kept[id] {
			continue
		}
		i, ok := offered[id]
		if !ok {
			return decision.Intent{}, false // a later shuffle: something else left the library first
		}
		picks = append(picks, i)
	}
	slices.Sort(picks)
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}
	if in.Choices == nil {
		in.Choices = []int{}
	}
	if d.Validate(in) != nil {
		return decision.Intent{}, false
	}
	f.res.Chooses++
	f.res.LibraryPicks++
	return in, true
}

// handLeaveChoose answers a pick of hand cards (a CR 514.1 cleanup discard,
// an effect's or a cost's discard, any choose that moves cards out of a hand)
// from the next snapshot line. Forge's recorder writes no ask line for the
// pick, but the next snapshot names the seat's hand after it: the picked
// cards are the offered ones whose names are gone from that hand. Answering
// by default (the first Min in offered order) discarded a different card than
// Forge (farm cmdr4 TLA-s289190153 pod-28919015300 line 58: gorge kept
// Avenger of Zendikar and discarded Welcome the Dead, Forge the reverse), and
// the p0..p3.hand snapshot classes stopped the follow.
//
// It applies to a pick cardPickChoose routes out of a hand (pickRouteOf: the
// chosen cards leave it and the others stay) and whose every option is a card
// in one seat's hand; an ask with no such route (a reveal, a "keep these")
// never gets here, so a new hand-shaped pick needs its route first. It
// answers only when every card that left the hand by that snapshot, less
// those a logged cast or land play took (loggedPlays), is offered, the
// departed count is a legal pick count, and the picks validate; anything else
// (an unlogged move in between) falls back to the default. A draw of a
// departed card's name before the snapshot hides the departure: a pick onto
// the battlefield is read from the permanents first (fieldArriveChoose).
func (f *follower) handLeaveChoose(d *decision.Decision) (decision.Intent, bool) {
	if len(d.Options) == 0 || f.e == nil || f.e.G == nil {
		return decision.Intent{}, false
	}
	seat := -1
	inHand := map[state.ObjID]bool{}
	var hand []state.ObjID
	for _, o := range d.Options {
		if o.Obj == 0 {
			return decision.Intent{}, false
		}
		if seat < 0 {
			for s := range f.e.G.Players {
				if slices.Contains(f.e.G.Zone(state.ZHand, state.PlayerID(s)), o.Obj) {
					seat = s
					break
				}
			}
			if seat < 0 {
				return decision.Intent{}, false
			}
			hand = f.e.G.Zone(state.ZHand, state.PlayerID(seat))
			for _, id := range hand {
				inHand[id] = true
			}
		}
		if !inHand[o.Obj] {
			return decision.Intent{}, false
		}
	}
	snap := f.nextSnapshot()
	if snap == nil {
		return decision.Intent{}, false
	}
	var after []string
	found := false
	for _, p := range snap.Players {
		if p.Seat == seat {
			after, found = p.Hand, true
			break
		}
	}
	if !found {
		return decision.Intent{}, false
	}
	// gone counts, by name, the cards of gorge's hand now missing from
	// Forge's hand at the snapshot (a multiset difference).
	gone := map[string]int{}
	for _, id := range hand {
		gone[objName(f.e.G.Obj(id))]++
	}
	for _, n := range after {
		if gone[n] > 0 {
			gone[n]--
		}
	}
	// A card the log shows Forge casting or playing out of this hand before
	// the snapshot left it on its own, not by this pick (farm cmdr4 BIG-s148
	// pod-14803 line 642: Dream Cache's two cards, then Portent cast).
	for _, g := range f.loggedPlays() {
		if inHand[g] {
			if n := objName(f.e.G.Obj(g)); gone[n] > 0 {
				gone[n]--
			}
		}
	}
	total := 0
	for _, n := range gone {
		total += n
	}
	var picks []int
	seen := map[state.ObjID]bool{}
	for i, o := range d.Options {
		if seen[o.Obj] {
			continue
		}
		seen[o.Obj] = true
		if n := objName(f.e.G.Obj(o.Obj)); gone[n] > 0 {
			gone[n]--
			picks = append(picks, i)
		}
	}
	if len(picks) != total || len(picks) < d.Min || (d.Max > 0 && len(picks) > d.Max) {
		return decision.Intent{}, false // a card left that was not offered, or a pick count the ask forbids
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}
	if in.Choices == nil {
		in.Choices = []int{}
	}
	if d.Validate(in) != nil {
		return decision.Intent{}, false
	}
	f.res.Chooses++
	f.res.HandPicks++
	return in, true
}

// cardPickChoose answers an unlogged pick of cards from what the log shows
// Forge did with them: a search's pick from the next shuffle line, a pick out
// of a hand or into one from the next snapshot line. Each reader takes the
// offered cards that made one movement for the pick, so it is asked only
// where the ask sends the chosen cards that way and the ones left out stay
// behind (pickRouteOf): where the chosen cards stay and the others move, the
// same evidence names the complement of Forge's pick. This is the only caller
// of the readers; each also matches the offered objects by the zone they are
// in and declines what it cannot show.
func (f *follower) cardPickChoose(d *decision.Decision) (decision.Intent, bool) {
	r, ok := f.pickRouteOf(d)
	if !ok {
		return decision.Intent{}, false
	}
	if r.chosenLeaves && !r.restLeaves {
		if in, ok := f.libraryPickChoose(d); ok {
			return in, true
		}
		// Where the chosen cards land on the battlefield, the snapshot's
		// permanents show them by name whatever else moves meanwhile; the
		// hand's count does not (a later draw of the same name cancels the
		// departure), so this reader goes before handLeaveChoose.
		if r.chosenToField {
			if in, ok := f.fieldArriveChoose(d); ok {
				return in, true
			}
		}
		if in, ok := f.handLeaveChoose(d); ok {
			return in, true
		}
	}
	if r.chosenToHand && !r.restToHand {
		return f.handArriveChoose(d)
	}
	return decision.Intent{}, false
}

// pickRoute is where an ask sends the cards it offers, the ones the answer
// names (chosen) and the ones it leaves out (rest): whether they leave the
// zone they are offered from, and whether they reach a hand.
type pickRoute struct {
	chosenLeaves, restLeaves bool
	chosenToHand, restToHand bool
	// chosenToField: the chosen cards go only to the battlefield.
	chosenToField bool
}

// leavingOptionKinds are the option kinds that say by themselves that the
// chosen cards leave their zone for somewhere other than a hand and the rest
// stay. Most are costs gorge asks with no resume kind. A kind not listed here
// and not routed by pickRouteOf keeps the default, so an ask whose chosen
// cards stay (revealcost, beholdcost, splice, balance_keep) is never listed.
var leavingOptionKinds = map[string]bool{
	// Discards: the cleanup step's, an effect's, a connive's or a cost's; a
	// ward's, a mana ability's, a cumulative upkeep's.
	"discard": true, "ward_discard": true, "mana_discard": true, "cumulative_action_discard": true,
	// An echo cost's discard or sacrifice.
	"echo_action_pick": true,
	// Exiles: a cast or unless cost's, a mana ability's, a triggered cost's,
	// an opening-hand effect's (Gemstone Caverns), an imprint's.
	"exilecost": true, "mana_exile": true, "exile_cost": true, "opening_exile": true, "imprint": true,
	// A cost that puts cards on a library (Penance).
	"puttolibcost": true,
	// A manifest or cloak from hand.
	"face_down_choice": true,
}

// pickRouteOf reads d's route off the ask itself: its option kinds, else its
// resume kind and the asking ability's destination parameters. ok is false for
// every ask that does not positively say -- a ChooseCard, whose chosen cards'
// fate later abilities decide; a reveal; a pile split; modes of an ability; a
// shape gorge adds later -- so no reader infers a pick from it.
func (f *follower) pickRouteOf(d *decision.Decision) (pickRoute, bool) {
	var no pickRoute
	if len(d.Options) == 0 {
		return no, false
	}
	if !slices.ContainsFunc(d.Options, func(o decision.Option) bool { return !leavingOptionKinds[o.Kind] }) {
		return pickRoute{chosenLeaves: true}, true
	}
	sa := d.ResumeSA
	api := ""
	if sa != nil {
		api = sa.API
	}
	switch d.ResumeKind {
	case "search", "search_zones":
		// A search takes the cards found out of their zone before it
		// shuffles, wherever it then puts them.
		r := pickRoute{chosenLeaves: true}
		if api == "ChangeZone" {
			to, ok := changeZoneDestinations(sa)
			r.chosenToHand = ok && onlyHand(to)
			r.chosenToField = ok && onlyZone(to, state.ZBattlefield)
		}
		return r, true
	case "hidden_pick", "hand_move":
		if api != "ChangeZone" || f.e == nil || f.e.G == nil {
			return no, false
		}
		to, ok := changeZoneDestinations(sa)
		if !ok {
			return no, false
		}
		for _, o := range d.Options {
			obj := f.e.G.Obj(o.Obj)
			if obj == nil || slices.Contains(to, obj.Zone) {
				return no, false
			}
		}
		return pickRoute{chosenLeaves: true, chosenToHand: onlyHand(to), chosenToField: onlyZone(to, state.ZBattlefield)}, true
	case "dig":
		if api != "Dig" {
			return no, false
		}
		p := effects.DigOf(sa)
		// SkipReorder$ alone leaves the rest where it is; otherwise it goes
		// to the second destination (the library when none is named).
		rest, ok := state.ZLibrary, true
		if !p.SkipReorder || p.RestRandomOrder {
			rest, ok = effects.ParseZoneWord(p.Dest2Name)
		}
		if !ok {
			return no, false
		}
		return pickRoute{
			chosenLeaves: p.Dest != state.ZLibrary, chosenToHand: p.Dest == state.ZHand,
			restLeaves: rest != state.ZLibrary, restToHand: rest == state.ZHand,
		}, true
	case "digmultiple":
		if api != "DigMultiple" {
			return no, false
		}
		p := effects.DigMultipleOf(sa)
		if p.ChangeLater {
			return no, false // later abilities move the picked cards
		}
		// With ChooseAmount$ 1, one of the picks goes to ChosenZone$ instead.
		split := p.ChooseAmount == 1
		return pickRoute{
			chosenLeaves: p.Dest != state.ZLibrary && !(split && p.ChosenZone == state.ZLibrary),
			chosenToHand: p.Dest == state.ZHand && !(split && p.ChosenZone != state.ZHand),
			restLeaves:   p.Rest != state.ZLibrary, restToHand: p.Rest == state.ZHand,
		}, true
	case "hideaway_pick":
		// The pick is exiled; the rest go to the bottom of the library.
		return pickRoute{chosenLeaves: true}, true
	case "draw_upto":
		// "Draw up to N": the cards named are drawn.
		return pickRoute{chosenLeaves: true, chosenToHand: true}, true
	}
	return no, false
}

// changeZoneDestinations is every zone a ChangeZone ability may put a chosen
// card: its Destination$ and, where it names one, its DestinationAlternative$.
// ok is false when either is not a zone gorge models.
func changeZoneDestinations(sa *cards.SA) ([]state.Zone, bool) {
	p := effects.ChangeZoneOf(sa)
	if !p.DestinationKnown || (p.DestinationAltText != "" && !p.DestinationAltKnown) {
		return nil, false
	}
	to := []state.Zone{p.Destination}
	if p.DestinationAltText != "" {
		to = append(to, p.DestinationAlt)
	}
	return to, true
}

// onlyHand reports whether every zone of to is the hand.
func onlyHand(to []state.Zone) bool { return onlyZone(to, state.ZHand) }

// onlyZone reports whether every zone of to is z.
func onlyZone(to []state.Zone, z state.Zone) bool {
	return !slices.ContainsFunc(to, func(x state.Zone) bool { return x != z })
}

// loggedPlays is the gorge object of every card a priority answer between
// f.pos and the next snapshot casts or plays as a land: the log's own record
// of cards leaving a hand (or arriving on the battlefield) that no unlogged
// pick moved. A Forge id gorge never bound is skipped.
func (f *follower) loggedPlays() []state.ObjID {
	var out []state.ObjID
	for i := f.pos; i < len(f.log.Entries); i++ {
		e := f.log.Entries[i]
		if e.Snap != nil || e.End != nil {
			break
		}
		a := e.Ask
		if a == nil || a.Kind != KindPriority || (a.Answer.Action != "cast" && a.Answer.Action != "land") {
			continue
		}
		if g, ok := f.toG[a.Answer.Card]; ok {
			out = append(out, g)
		}
	}
	return out
}

// fieldArriveChoose answers a pick of cards that move onto the battlefield
// (one land of a split search, "put a basic land card from your hand onto
// the battlefield") from the next snapshot line: the picked cards are the
// offered ones whose names are new among their owner's nontoken permanents
// there, less those a logged land play or cast before it accounts for.
// Answering by default put the wrong land of Kodama's Reach onto the
// battlefield (farm cmdr4 TDM-s284753433 pod-28475343302 line 524: gorge
// Island, Forge Plains), and a "may" pick from hand took nothing where Forge
// put the Mountain and then drew another (EOE-s138 pod-13801 line 384, Terrain
// Generator), which the hand alone cannot show.
//
// It applies to a pick cardPickChoose routes onto the battlefield whose
// options are all elsewhere. It answers only when every unaccounted arrival
// is offered, the count is a legal pick count, and the picks validate;
// anything else falls back to the next reader.
func (f *follower) fieldArriveChoose(d *decision.Decision) (decision.Intent, bool) {
	if len(d.Options) == 0 || f.e == nil || f.e.G == nil {
		return decision.Intent{}, false
	}
	for _, o := range d.Options {
		obj := f.e.G.Obj(o.Obj)
		if o.Obj == 0 || obj == nil || obj.Zone == state.ZBattlefield {
			return decision.Intent{}, false
		}
	}
	snap := f.nextSnapshot()
	if snap == nil {
		return decision.Intent{}, false
	}
	now := Snapshot(f.e, "")
	type card struct {
		seat int
		name string
	}
	owned := func(s *rules.OracleSnapshot, seat int) []string {
		var names []string
		for _, p := range s.Permanents {
			if p.Owner == seat && !p.Token {
				names = append(names, p.Name)
			}
		}
		return names
	}
	want := map[card]int{}
	counted := map[int]bool{}
	for _, o := range d.Options {
		seat := int(f.e.G.Obj(o.Obj).Owner)
		if counted[seat] {
			continue
		}
		counted[seat] = true
		if _, ok := snapPlayer(snap, seat); !ok {
			return decision.Intent{}, false
		}
		for _, n := range surplus(owned(snap, seat), owned(&now, seat)) {
			want[card{seat, n}]++
		}
	}
	for _, g := range f.loggedPlays() {
		if obj := f.e.G.Obj(g); obj != nil {
			if c := (card{int(obj.Owner), objName(obj)}); want[c] > 0 {
				want[c]--
			}
		}
	}
	total := 0
	for _, n := range want {
		total += n
	}
	var picks []int
	seen := map[state.ObjID]bool{}
	for i, o := range d.Options {
		if seen[o.Obj] {
			continue
		}
		seen[o.Obj] = true
		obj := f.e.G.Obj(o.Obj)
		if c := (card{int(obj.Owner), objName(obj)}); want[c] > 0 {
			want[c]--
			picks = append(picks, i)
		}
	}
	if total == 0 || len(picks) != total || len(picks) < d.Min || (d.Max > 0 && len(picks) > d.Max) {
		return decision.Intent{}, false // nothing arrived, an arrival was not offered, or a pick count the ask forbids
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}
	if d.Validate(in) != nil {
		return decision.Intent{}, false
	}
	f.res.Chooses++
	f.res.FieldPicks++
	return in, true
}

// handArriveChoose answers a pick of cards that move into a hand (a dig's
// "you may put one into your hand", a graveyard return, a bounce land's
// return) from the next snapshot line: the picked cards are the offered ones
// whose names are new in their owner's hand there. Answering by default left
// the card where it was (a "may" pick takes nothing) or took another one
// (farm cmdr4 DSK-s134 pod-13402 line 209, Satyr Wayfinder; TMT-s23564447
// pod-2356444702 line 384, Gruul Turf).
//
// It applies to any pick that cardPickChoose routes into a hand and whose
// options are all outside the hands. It answers only when every card new in
// those hands is offered AND its source holds that many fewer at the
// snapshot, the count is a legal pick count, and the picks validate; anything
// else falls back to the default. A public source is counted by name; a
// library only by its size, which a draw lowers too, so an optional pick
// Forge declined, followed before the snapshot by a draw of a card named like
// an offered one, reads as that pick.
func (f *follower) handArriveChoose(d *decision.Decision) (decision.Intent, bool) {
	if len(d.Options) == 0 || f.e == nil || f.e.G == nil {
		return decision.Intent{}, false
	}
	for _, o := range d.Options {
		obj := f.e.G.Obj(o.Obj)
		if o.Obj == 0 || obj == nil || obj.Zone == state.ZHand {
			return decision.Intent{}, false
		}
	}
	snap := f.nextSnapshot()
	if snap == nil {
		return decision.Intent{}, false
	}
	// A draw_upto ask offers the library's own top cards, so a later draw
	// step takes exactly a card the ask offered: read it only from a snapshot
	// of the step gorge is asking in.
	if d.ResumeKind == "draw_upto" && at(int(snap.Turn), snap.Step) != f.gorgeAt() {
		return decision.Intent{}, false
	}
	now := Snapshot(f.e, "")
	// want counts, per owner and name, the cards new in that hand at the
	// snapshot.
	type card struct {
		seat int
		name string
	}
	want := map[card]int{}
	total := 0
	counted := map[int]bool{}
	for _, o := range d.Options {
		seat := int(f.e.G.Obj(o.Obj).Owner)
		if counted[seat] {
			continue
		}
		counted[seat] = true
		after, ok := snapPlayer(snap, seat)
		before, ok2 := snapPlayer(&now, seat)
		if !ok || !ok2 {
			return decision.Intent{}, false
		}
		for _, n := range surplus(after.Hand, before.Hand) {
			want[card{seat, n}]++
			total++
		}
	}
	// left is how many more cards of a name each source zone can have lost by
	// the snapshot. A library names only its top, so it is bounded by its
	// count, whatever the names.
	type source struct {
		zone state.Zone
		card card
	}
	left := map[source]int{}
	var picks []int
	seen := map[state.ObjID]bool{}
	for i, o := range d.Options {
		if seen[o.Obj] {
			continue
		}
		seen[o.Obj] = true
		obj := f.e.G.Obj(o.Obj)
		c := card{int(obj.Owner), objName(obj)}
		if want[c] == 0 {
			continue
		}
		src := source{zone: obj.Zone, card: c}
		if obj.Zone == state.ZLibrary {
			src.card.name = ""
		}
		n, ok := left[src]
		if !ok {
			n = zoneCount(&now, src.zone, c.seat, c.name) - zoneCount(snap, src.zone, c.seat, c.name)
		}
		if n <= 0 {
			left[src] = 0
			continue
		}
		left[src] = n - 1
		want[c]--
		picks = append(picks, i)
	}
	if total == 0 || len(picks) != total || len(picks) < d.Min || (d.Max > 0 && len(picks) > d.Max) {
		return decision.Intent{}, false // nothing arrived, an arrival was not offered, or a pick count the ask forbids
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}
	if d.Validate(in) != nil {
		return decision.Intent{}, false
	}
	f.res.Chooses++
	f.res.ArrivalPicks++
	return in, true
}

// snapPlayer is the snapshot's entry for seat.
func snapPlayer(s *rules.OracleSnapshot, seat int) (rules.OracleSnapPlayer, bool) {
	for _, p := range s.Players {
		if p.Seat == seat {
			return p, true
		}
	}
	return rules.OracleSnapPlayer{}, false
}

// zoneCount is how many cards seat owns in zone z at snapshot s: those named
// name, except in a library, whose whole count it is (a snapshot names only
// its top). A zone a snapshot does not carry counts 0.
func zoneCount(s *rules.OracleSnapshot, z state.Zone, seat int, name string) int {
	p, ok := snapPlayer(s, seat)
	if !ok {
		return 0
	}
	var names []string
	switch z {
	case state.ZLibrary:
		return p.LibraryCount
	case state.ZGraveyard:
		names = p.Graveyard
	case state.ZExile:
		names = p.Exile
	case state.ZCommand:
		names = p.Command
	case state.ZBattlefield:
		for _, perm := range s.Permanents {
			if perm.Owner == seat && !perm.Token {
				names = append(names, perm.Name)
			}
		}
	}
	n := 0
	for _, x := range names {
		if x == name {
			n++
		}
	}
	return n
}

// maxArrangeRead is the most offered cards arrangeFromSnapshot enumerates the
// orders of (6 cards have 1957 ordered subsets).
const maxArrangeRead = 6

// arrangeFromSnapshot answers a scry, surveil or top-of-library reorder from
// the next snapshot line. Forge's recorder writes no ask line for it, but the
// snapshot names the library's top and count, the hand and the graveyard
// after it. Answering by default (everything stays on top in order) kept two
// Islands on top where Forge bottomed them and drew Elvish Piper (farm cmdr4
// BLB-s131026052 pod-13102605201 line 85, Elven Farsight).
//
// It applies to an "arrange" ask over exactly the top cards of one library.
// Each ordered keep is projected onto that library; it fits when taking as
// many cards off the top as the count has dropped (draws since) leaves the
// snapshot's top, and every card new in the hand is among those taken. The
// fit whose binned cards best show as new in the graveyard answers, the
// earliest (the default first) on a tie. No fit (a shuffle or another move in
// between) falls back to the default.
func (f *follower) arrangeFromSnapshot(d *decision.Decision) (decision.Intent, bool) {
	n := len(d.Options)
	if d.ResumeKind != "arrange" || n == 0 || n > maxArrangeRead || f.e == nil || f.e.G == nil {
		return decision.Intent{}, false
	}
	away := d.Options[0].Kind // where the cards not kept on top go
	if away != "bottom" && away != "graveyard" {
		return decision.Intent{}, false
	}
	first := f.e.G.Obj(d.Options[0].Obj)
	if first == nil {
		return decision.Intent{}, false
	}
	seat := int(first.Owner)
	lib := f.e.G.Zone(state.ZLibrary, first.Owner)
	if len(lib) < n {
		return decision.Intent{}, false
	}
	for i, o := range d.Options {
		if o.Kind != away || lib[i] != o.Obj {
			return decision.Intent{}, false
		}
	}
	snap := f.nextSnapshot()
	if snap == nil {
		return decision.Intent{}, false
	}
	now := Snapshot(f.e, "")
	after, ok := snapPlayer(snap, seat)
	before, ok2 := snapPlayer(&now, seat)
	if !ok || !ok2 || len(after.LibraryTop) == 0 {
		return decision.Intent{}, false
	}
	toHand := surplus(after.Hand, before.Hand)
	toYard := surplus(after.Graveyard, before.Graveyard)
	names := make([]string, len(lib))
	for i, id := range lib {
		names[i] = objName(f.e.G.Obj(id))
	}
	// fit scores one keep, -1 when the snapshot rules it out.
	fit := func(keep []int) int {
		var order, binned []string
		for _, i := range keep {
			order = append(order, names[i])
		}
		order = append(order, names[n:]...)
		for i := range n {
			if !slices.Contains(keep, i) {
				binned = append(binned, names[i])
			}
		}
		if away == "bottom" {
			order, binned = append(order, binned...), nil
		}
		drawn := len(order) - after.LibraryCount
		if drawn < 0 || drawn+len(after.LibraryTop) > len(order) || !slices.Equal(order[drawn:drawn+len(after.LibraryTop)], after.LibraryTop) {
			return -1
		}
		if len(surplus(toHand, order[:drawn])) > 0 {
			return -1
		}
		return len(binned) - len(surplus(binned, toYard))
	}
	hi := min(d.Max, n)
	var best []int
	bestScore := -1
	var walk func(keep []int, size int)
	walk = func(keep []int, size int) {
		if len(keep) == size {
			if s := fit(keep); s > bestScore {
				best, bestScore = slices.Clone(keep), s
			}
			return
		}
		for i := range n {
			if !slices.Contains(keep, i) {
				walk(append(keep, i), size)
			}
		}
	}
	for size := hi; size >= d.Min; size-- {
		walk(make([]int, 0, size), size)
	}
	if bestScore < 0 {
		return decision.Intent{}, false
	}
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: best}
	if in.Choices == nil {
		in.Choices = []int{}
	}
	if d.Validate(in) != nil {
		return decision.Intent{}, false
	}
	f.res.Arranges++
	f.res.ArrangePicks++
	return in, true
}

// commanderZone answers a CR 903.9 commander-zone decision (the owner puts a
// commander in a graveyard or exile, or bound for the hand or library, into the
// command zone, or leaves it) from the next snapshot line. Forge's recorder
// writes no ask line for it, so the next line belongs to a later decision
// (farm cmdr4 FRA-s27490222 pod-2749022201 line 283: gorge "commander_zone seat
// 0" after Swords to Plowshares exiled Giada, against Forge's "priority seat
// 0"). The default is the command zone, gorge's first option. It is "leave"
// when the owner's command zone at that snapshot lacks the commander and no
// logged cast explains why: a cast of it by its owner before the snapshot was
// made from the command zone, unless leaving it in exile keeps a permission to
// cast it from there (the leave option's Mode). It consumes no line.
func (f *follower) commanderZone(d *decision.Decision) decision.Intent {
	f.res.CommanderZones++
	in := firstMin(d)
	leave := slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Kind == "leave" })
	if leave < 0 {
		return in
	}
	fid, bound := f.toF[d.Options[leave].Obj]
	snap := f.nextSnapshot()
	if !bound || snap == nil {
		return in
	}
	i := slices.IndexFunc(snap.Players, func(p rules.OracleSnapPlayer) bool { return p.Seat == int(d.Player) })
	if i < 0 || slices.Contains(snap.Players[i].Command, f.name[fid]) {
		return in
	}
	if d.Options[leave].Mode == "" && f.castBeforeSnapshot(int(d.Player), fid) {
		return in
	}
	in.Choices = []int{leave}
	return in
}

// castBeforeSnapshot reports whether seat casts Forge's card fid on a line at
// or after f.pos and before the next snapshot.
func (f *follower) castBeforeSnapshot(seat, fid int) bool {
	for i := f.pos; i < len(f.log.Entries); i++ {
		e := f.log.Entries[i]
		if e.Snap != nil || e.End != nil {
			return false
		}
		if a := e.Ask; a != nil && a.Seat == seat && a.Answer.Action == "cast" && a.Answer.Card == fid {
			return true
		}
	}
	return false
}

// nextSnapshot is the first snapshot line at or after f.pos: with an
// unlogged decision pending, the state after Forge's answer to it.
func (f *follower) nextSnapshot() *rules.OracleSnapshot {
	for i := f.pos; i < len(f.log.Entries); i++ {
		if s := f.log.Entries[i].Snap; s != nil {
			return &s.Snapshot
		}
		if f.log.Entries[i].End != nil {
			return nil
		}
	}
	return nil
}

// shuffleLogged reports whether some seat's next unconsumed Forge shuffle
// holds every card in that seat's current gorge library (a non-empty one).
func (f *follower) shuffleLogged() bool {
	for seat, ss := range f.shuffles {
		n := f.planned[seat]
		if n >= len(ss) || seat < 0 || seat >= len(f.log.Header.Seats) {
			continue
		}
		lib := f.e.G.Zone(state.ZLibrary, state.PlayerID(seat))
		if len(lib) == 0 {
			continue
		}
		in := map[state.ObjID]bool{}
		for _, fid := range ss[n].Order {
			if g, ok := f.toG[fid]; ok {
				in[g] = true
			}
		}
		all := true
		for _, id := range lib {
			if !in[id] {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
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

// surplus is the multiset difference a minus b, in a's order.
func surplus(a, b []string) []string {
	have := map[string]int{}
	for _, x := range b {
		have[x]++
	}
	var out []string
	for _, x := range a {
		if have[x] > 0 {
			have[x]--
			continue
		}
		out = append(out, x)
	}
	return out
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

// isBottomingAsk reports whether d is a London bottoming ask (CR 103.5) as
// opposed to a keep-or-mulligan one: its options are the cards of the hand.
func isBottomingAsk(d *decision.Decision) bool {
	return len(d.Options) > 0 && d.Options[0].Kind == "bottom"
}

// skipNoOpMulligan consumes the mulligan lines that changed nothing. Forge's
// random-policy seat can answer "mulligan" after its hand has emptied:
// AbstractMulligan.mulligan() returns without a redraw, a shuffle or an
// increment, and Forge asks the seat again, writing one mull line per answer.
// gorge's allowance is spent by then and offers only "keep", so such a line
// has no gorge counterpart. The seat's recorded empty hand is the evidence;
// the keep that follows (after any number of no-ops) answers gorge's ask.
func (f *follower) skipNoOpMulligan(d *decision.Decision) bool {
	if d == nil || d.Kind != decision.KMulligan || isBottomingAsk(d) {
		return false
	}
	skipped := false
	for f.pos < len(f.log.Entries) {
		m := f.log.Entries[f.pos].Mull
		if m == nil || m.Keep || !m.emptyHand() || m.Seat != int(d.Player) {
			return skipped
		}
		f.res.SkippedMulls++
		f.pos++
		skipped = true
	}
	return skipped
}
