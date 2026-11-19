package lower

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/state"
)

// Template is the item template of a lowered tape.
const Template = "xengine-tape"

// Truncation reasons. The list is closed: Lowered.Truncated is one of these
// or "unsupported_kind:" plus the tape entry kind (or the named feature) the
// lowering has no step for.
const (
	ReasonAttackSplit     = "attack_split"     // one attack at more than one defender
	ReasonAttackNonPlayer = "attack_nonplayer" // an attack at a planeswalker or battle
	ReasonUnbindableRef   = "unbindable_ref"   // an object the runner cannot name
	ReasonLogOnlyCast     = "log_only_cast:"   // a window cast only the event log entitled
	ReasonStall           = "stall"            // the tape ends in a watchdog, not a game end
	ReasonUnsupported     = "unsupported_kind:"
)

// Lowered is a tape lowered to one oracle scenario.
type Lowered struct {
	// Item is the scenario. Ignore stays empty; Compare holds CompareCommand on a
	// Commander item and is empty otherwise (the classifier sets the rest,
	// and the follower-specific fields stay empty).
	Item oraclegen.Item `json:"item"`
	// StepEntry is parallel to Item.Steps: the index of the last tape entry
	// the step carries out, or -1 for a step the live game has no state
	// after (an explicit mana tap before a cast). The runner's checkpoint
	// after step i is checked against the digest of entry StepEntry[i]+1.
	StepEntry []int `json:"step_entry"`
	// Truncated is why the lowering stopped before the end of the tape ("" if
	// it did not) and TruncatedAt the index of the first tape entry it did
	// not lower.
	Truncated   string `json:"truncated,omitempty"`
	TruncatedAt int    `json:"truncated_at,omitempty"`
	// FirstEntry is the tape entry whose state the scenario's setup
	// checkpoint stands for: the first priority of turn 1's first main phase.
	FirstEntry int `json:"first_entry"`
	// Swap is true when the tape's starting player was seat 1, so every seat
	// in the scenario is the opposite of the tape's.
	Swap bool `json:"swap,omitempty"`
	// Prelude is the number of leading attach steps of a window: the setup
	// checkpoint precedes them, so it carries no attachment, and the state
	// the cut's digest stands for is the one after the last of them.
	Prelude int `json:"prelude,omitempty"`
	// HasRNG and RNGStep: a window entry consumed engine randomness (a
	// shuffle), and RNGStep is the first step that carries one. From its
	// checkpoint on the library order is the runner's, not the live game's, so
	// the self-check leaves library_top out.
	HasRNG  bool `json:"has_rng,omitempty"`
	RNGStep int  `json:"rng_step,omitempty"`
	// Against is the tape a window is self-checked against: its entries
	// rebased so the cut is turn 1 (see windowTape). nil for a whole-game tape.
	Against *game.Tape `json:"-"`
}

var deckName = regexp.MustCompile(`-c(\d+)-g(\d+)-`)

// deckNameOf is the name of seat i's deck, "" for a tape that lists none (a
// hand-built one).
func deckNameOf(t *game.Tape, i int) string {
	if i < len(t.Decks) {
		return t.Decks[i].Name
	}
	return ""
}

// itemID is xe:<set>:<chunk>:<game>:tape; chunk and game come from the deck
// name decks.Pair gave the tape's first deck (the seed stands in for both when
// the name is not one of those).
func itemID(t *game.Tape) string {
	if m := deckName.FindStringSubmatch(deckNameOf(t, 0)); m != nil {
		return fmt.Sprintf("xe:%s:%s:%s:tape", t.Set, m[1], m[2])
	}
	return fmt.Sprintf("xe:%s:s%d:%d:tape", t.Set, t.Seed, t.Seed)
}

// truncation is why a group of entries could not be lowered.
type truncation struct{ reason string }

func unsupported(k string) *truncation { return &truncation{ReasonUnsupported + k} }

// isAsk is true for a decision that is neither a priority nor a declaration:
// the kinds that become answers.
func isAsk(e game.Entry) bool {
	switch e.Kind {
	case "priority", "attackers", "blockers":
		return false
	}
	return true
}

func isPass(e game.Entry) bool {
	return e.Kind == "priority" && len(e.Picks) == 1 && e.Picks[0].Kind == "pass"
}

// ReasonSeats is the refusal reason of a game with more than two seats:
// "seats:<n>". The lowering (a scenario's seats p0/p1, the cut's Setup and
// Answers) is two-seat throughout, so a pod is refused whole.
const ReasonSeats = "seats:"

// seatRefusal is the one seat-count check of the lowering: the reasons a game
// of n seats is refused, none for a 1v1 game.
func seatRefusal(n int) []string {
	if n > 2 {
		return []string{ReasonSeats + strconv.Itoa(n)}
	}
	return nil
}

// RefusedError is Tape's error for a game it refuses by name: a game the
// lowering does not express at all, not one it cannot read.
type RefusedError struct{ Reasons []string }

func (e *RefusedError) Error() string {
	return "lower: refused: " + strings.Join(e.Reasons, ", ")
}

// Tape lowers t. A game with more than two seats is refused with a
// *RefusedError (reason seats:<n>). A tape the lowering cannot express in full yields the
// longest lowerable prefix with Truncated set; only a tape that cannot be
// read as a game at all (an unknown card) is an error. The result is a pure
// function of t.
func Tape(reg *cards.Registry, t *game.Tape) (Lowered, error) {
	if r := seatRefusal(max(len(t.Decks), len(t.Open))); r != nil {
		return Lowered{}, &RefusedError{Reasons: r}
	}
	m := seatMap{swap: t.Starter == 1}
	for s := range t.Open {
		for _, n := range slices.Concat(t.Open[s].Hand, t.Open[s].Command, t.Open[s].Library) {
			if _, ok := reg.Lookup(n); !ok {
				return Lowered{}, fmt.Errorf("lower: seat %d card %q not in the corpus", s, n)
			}
		}
	}
	commander := t.GameFormat == oraclegen.FormatCommander
	sc := oraclegen.Scenario{
		Name:  "xengine tape " + deckNameOf(t, 0) + " vs " + deckNameOf(t, 1),
		Turn:  1,
		CR:    []string{},
		Why:   "A recorded gorge game between two explore bots, replayed from turn 1.",
		Setup: map[string]oraclegen.Seat{},
	}
	for s := range t.Open {
		seat := oraclegen.Seat{
			Hand:       append([]string{}, t.Open[s].Hand...),
			LibraryTop: append([]string{}, t.Open[s].Library...),
		}
		if commander {
			seat.Command = append([]string{}, t.Open[s].Command...)
		}
		sc.Setup["p"+strconv.Itoa(m.seat(s))] = seat
	}
	if commander {
		sc.Format = oraclegen.FormatCommander
	}
	l := Lowered{
		Item: oraclegen.Item{ID: itemID(t), Template: Template, Scenario: sc},
		Swap: m.swap, StepEntry: []int{},
	}
	l.Item.Steps = []oraclegen.Step{}
	if commander {
		l.Item.Compare = []string{oraclediff.CompareCommand}
	}

	first := -1
	for i, e := range t.Entries {
		if e.Kind == "priority" && e.Turn == 1 && e.Step == "main1" {
			first = i
			break
		}
		switch {
		case isPass(e):
		case isAsk(e):
			// A decision posed while the runner drives to turn 1's first
			// main phase (an opening-hand "begin the game" ask).
			ans, why := answersOf(m, []game.Entry{e})
			if why != nil {
				l.stop(i, why)
				return l, nil
			}
			l.Item.SetupAnswers = append(l.Item.SetupAnswers, ans...)
		default:
			l.stop(i, unsupported("prelude_"+e.Kind))
			return l, nil
		}
	}
	if first < 0 {
		l.stop(len(t.Entries), unsupported("no_main1"))
		return l, nil
	}
	l.FirstEntry = first

	l.lowerEntries(reg, m, t.Entries, first, t.Outcome.Stall != "")
	l.markDieRolls(m, t.Entries)
	l.compactPayment()
	return l, nil
}

// compactPayment leaves Payment nil when no step recorded any spend, so an
// item with no payment keeps its pre-payment bytes (the field is a parallel
// array, not a sparse map, and omitempty must omit the all-empty slice).
func (l *Lowered) compactPayment() {
	for _, p := range l.Item.Payment {
		if p != "" {
			return
		}
	}
	l.Item.Payment = nil
}

// lowerEntries lowers entries[first:] into l's steps. stalled marks entries
// that end in a watchdog, not a game end or the end of a window.
func (l *Lowered) lowerEntries(reg *cards.Registry, m seatMap, entries []game.Entry, first int, stalled bool) {
	// A pass that posed asks owes its answers to the next step created; the
	// pass is not final until that step exists, so its place is kept.
	var owed []oraclegen.Answer
	owedAt, owedSteps := -1, 0 // first entry of the pass group, steps before it
	// quiet is true when the group recorded no mana activation, RNG draw or
	// coin flip; the measured no-op groups are all quiet. Keep a noisy group
	// even when the digest is equal: dropping it would move RNG/mana
	// bookkeeping.
	quiet := func(g []game.Entry) bool {
		for _, e := range g {
			if len(e.Mana) > 0 || e.RNG > 0 || len(e.Flips) > 0 || len(e.Rolls) > 0 {
				return false
			}
		}
		return true
	}
	for i := first; i < len(entries); {
		j := i + 1
		for j < len(entries) && isAsk(entries[j]) {
			j++
		}
		if why := l.logOnlyCast(m, entries[i]); why != nil {
			l.refuse(i, why, owedAt, owedSteps)
			return
		}
		steps, stepEntry, payments, answers, why := lowerGroup(reg, m, entries[i:j])
		if why != nil {
			l.refuse(i, why, owedAt, owedSteps)
			return
		}
		if len(owed) == 0 && len(steps) > 0 && quiet(entries[i:j]) {
			if isPaidStep(steps[len(steps)-1].Op) {
				if next, ok := l.afterEntry(j, entries); ok &&
					len(entries[i].Digest.Diff(next.Digest)) == 0 &&
					entries[i].StackTop == next.StackTop {
					// A cast or activation the live game left the whole state
					// and the stack unchanged on did not happen: it is not a
					// replayable step (a refused Spree cast, for example). A
					// real action pushes an object, so the stack top changes.
					i = j
					continue
				}
			}
		}
		mainStep := len(steps) - 1
		var resolve *oraclegen.Step
		resolveEnd := 0
		resolveKeepsPass := false
		if steps[mainStep].Op == "pass" && len(answers) > 0 {
			if why := unsettled(entries[i:j]); why != nil {
				res, end, ok := resolvePass(m, entries, i, j)
				if !ok {
					res, end, ok = resolveEmptyPass(m, entries, i, j)
					resolveKeepsPass = ok
				}
				if !ok {
					l.stop(i, why)
					return
				}
				resolve, resolveEnd = res, end
			}
		}
		hadOwed := len(owed) > 0
		fund := l.fundingTaps(reg, m, entries, i, j, resolve, resolveEnd, resolveKeepsPass, hadOwed)
		if fund.refused {
			l.refuse(i, unsupported("ask_mana"), owedAt, owedSteps)
			return
		}
		if len(owed) > 0 {
			// The pass before this group was waiting for the next step's
			// answers. The resolve step takes them only when it replaces the
			// group's pass; when the pass is kept (resolveKeepsPass) the pass
			// itself is the next step, so the answers must land on it.
			dst := &steps[0]
			if resolve != nil && !resolveKeepsPass {
				dst = resolve
			}
			dst.Answers = append(append([]oraclegen.Answer{}, owed...), dst.Answers...)
			owed, owedAt = nil, -1
		}
		if resolve != nil {
			if resolveKeepsPass {
				// The pass is its own step: a resolve starting on an
				// empty-stack priority returns before passing.
				stepEntry[mainStep] = i
				l.markShuffles(m, entries[i:j], len(l.Item.Steps)+mainStep)
				l.Item.Steps = append(l.Item.Steps, steps...)
				l.StepEntry = append(l.StepEntry, stepEntry...)
				l.Item.Payment = append(l.Item.Payment, payments...)
			}
			l.pushFunding(fund)
			l.Item.Steps = append(l.Item.Steps, *resolve)
			l.StepEntry = append(l.StepEntry, resolveEnd-1)
			l.Item.Payment = append(l.Item.Payment, "")
			l.markShuffles(m, entries[i:resolveEnd], len(l.Item.Steps)-1)
			i = resolveEnd
			continue
		}
		l.pushFunding(fund)
		if steps[mainStep].Op == "pass" && len(answers) > 0 && !settlesTwice(entries[i:j]) {
			owed, owedAt, owedSteps = answers, i, len(l.Item.Steps)
		} else if len(answers) > 0 {
			attach(&steps[mainStep], answers, entries[i:j])
		}
		stepEntry[mainStep] = j - 1
		l.markShuffles(m, entries[i:j], len(l.Item.Steps)+mainStep)
		l.Item.Steps = append(l.Item.Steps, steps...)
		l.StepEntry = append(l.StepEntry, stepEntry...)
		l.Item.Payment = append(l.Item.Payment, payments...)
		i = j
	}
	if owedAt >= 0 {
		// The last pass posed an ask nothing after it can answer.
		l.drop(owedSteps)
		l.stop(owedAt, unsupported("ask_after_pass:"+entries[owedAt+1].Kind))
		return
	}
	if stalled {
		l.stop(len(entries), &truncation{ReasonStall})
	}
}

// refuse stops the lowering at entry i for why. A pass before it that was
// waiting for this group's first step goes too: owedAt is its first entry and
// owedSteps the steps before it (owedAt < 0 when none waits).
func (l *Lowered) refuse(i int, why *truncation, owedAt, owedSteps int) {
	at := i
	if owedAt >= 0 {
		at = owedAt
		l.drop(owedSteps)
	}
	l.stop(at, why)
}

// logOnlyCastMode reports a cast mode gorge's walk offers from a log-derived
// provenance, never from the object's own state: the Adventure recast (the
// resolved Adventure spell's exile marker and its CastInfo), the warp recast
// (the delayed trigger and its CastInfo), the foretell cast ("a later turn"
// after the foretell action) and the airbend recast (the exile move's
// airbend marker). A window's setup carries state only, so it cannot entitle
// one: the runner's walk would not offer the cast at all, and the self-check
// would fail with "cast <card> not offered" at that step.
func logOnlyCastMode(mode string) bool {
	switch mode {
	case "adventure_recast", "warp_recast", "foretell_cast", "airbend_cast":
		return true
	}
	return false
}

// logOnlyCast refuses a window's cast when the card already sits in the
// scenario setup's exile and the cast's mode is one the runner offers only
// from its own event log. The window starts at a turn's first priority, so the
// event that put the card there is before the window and no lowered step can
// recreate it. A card that enters exile inside the window (an Adventure cast
// and its recast in the same turn, an airbend) is not refused: the replay
// derives the provenance from that step's own events.
func (l *Lowered) logOnlyCast(m seatMap, en game.Entry) *truncation {
	if l.Against == nil || len(en.Picks) != 1 {
		return nil
	}
	pk := en.Picks[0]
	if pk.Kind != "cast" || !logOnlyCastMode(pk.Mode) {
		return nil
	}
	seat, rest, ok := strings.Cut(m.ref(pk.Ref), ":")
	if !ok || seat == "" {
		return nil
	}
	name, _, _ := strings.Cut(rest, "#")
	for _, n := range l.Item.Setup[seat].Exile {
		if n == name {
			return &truncation{ReasonLogOnlyCast + pk.Mode}
		}
	}
	return nil
}

// isResolvePay is true for the entry that answers an election to pay a cost
// when an object resolves or a step begins (a triggered ability's "you may
// pay", cumulative upkeep, echo) by the plain route, where gorge funds it
// itself: gorge's resolve-time planner taps the payer's sources while it
// commits the election, and logs no `activate` decision for them. The manual
// routes (..._pay_manual) are answered by explicit activations and are not
// this.
func isResolvePay(e game.Entry) bool {
	if !isAsk(e) || len(e.Picks) != 1 {
		return false
	}
	switch e.Picks[0].Kind {
	case "trigger_cost_pay", "cumulative_pay", "echo_pay":
		return true
	}
	return false
}

// funding is the explicit mana taps a resolve-time payment needs ahead of the
// pass that resolves its object. Forge and XMage pay such a cost from the
// floating pool only, so a plan whose tape carries no tap leaves them with an
// empty pool and a payment that fails where gorge's planner paid.
type funding struct {
	taps []oraclegen.Step
	// before puts the taps ahead of the last step already emitted (the
	// payer's pass), not at the end of the steps.
	before bool
	// refused is true when the group's funding cannot be placed and the group
	// is a pass that does not lower as a resolve step: it must stop the
	// lowering, as an unplaceable tap always did.
	refused bool
}

// fundingTaps plans the taps for the resolve-time payments in the group
// entries[i:j] (entries[i:resolveEnd] when it lowers as a resolve step). The
// taps are the mana abilities gorge's planner fired on the election's own
// entry, so they name the same sources. They are floated while the payer holds
// priority with the object still on the stack: at the payer's own pass (the
// group's first entry), or ahead of the step already emitted for the payer's
// pass the group's first entry follows (the opponent's pass resolves the
// object, and the runner's `pass` leaves the opponent holding priority).
// Pool mana lasts to the end of the step, so it is still there when the trigger
// resolves and asks.
func (l *Lowered) fundingTaps(reg *cards.Registry, m seatMap, entries []game.Entry, i, j int,
	resolve *oraclegen.Step, resolveEnd int, keepsPass, hadOwed bool) funding {
	end := j
	if resolve != nil {
		end = resolveEnd
	}
	var mana []game.ManaRec
	for _, e := range entries[i:end] {
		if isResolvePay(e) {
			mana = append(mana, e.Mana...)
		}
	}
	if len(mana) == 0 {
		return funding{}
	}
	if resolve == nil && paysAfterAsks(entries[i]) {
		// A cast or ability: lowerGroup already lowers its group's taps.
		return funding{}
	}
	fail := funding{refused: resolve == nil}
	if hadOwed || keepsPass {
		return fail
	}
	payer := mana[0].Player
	for _, mr := range mana {
		if mr.Player != payer {
			return fail
		}
		if sa := manaSA(reg, mr); sa != nil && strings.Contains(sa.Params["Cost"], "Sac<") {
			return fail
		}
	}
	taps, why := manaTaps(reg, m, mana)
	if why != nil {
		return fail
	}
	openPass := func(k int) bool {
		return k >= 0 && isPass(entries[k]) && entries[k].Seat == payer && entries[k].StackTop != 0
	}
	if openPass(i) {
		return funding{taps: taps}
	}
	if n := len(l.Item.Steps); n > 0 && openPass(i-1) && l.StepEntry[n-1] == i-1 &&
		l.Item.Steps[n-1].Op == "pass" && l.Item.Steps[n-1].Seat == m.seat(payer) {
		return funding{taps: taps, before: true}
	}
	return fail
}

// pushFunding adds f's taps to the steps. A tap has no live state of its own,
// and the pass it goes ahead of now ends with the mana floating, which the live
// game never had, so that pass is not checked against the tape either.
func (l *Lowered) pushFunding(f funding) {
	if len(f.taps) == 0 {
		return
	}
	idx := make([]int, len(f.taps))
	pay := make([]string, len(f.taps))
	for k := range idx {
		idx[k] = -1
	}
	at := len(l.Item.Steps)
	if f.before {
		at--
		l.StepEntry[at] = -1
	}
	l.Item.Steps = slices.Insert(l.Item.Steps, at, f.taps...)
	l.StepEntry = slices.Insert(l.StepEntry, at, idx...)
	l.Item.Payment = slices.Insert(l.Item.Payment, at, pay...)
}

// afterDigest is the state after the group ending at j-1: the next entry in the
// slice, or a window's next-turn sentinel (l.Against) when the group is last.
func (l *Lowered) afterDigest(j int, entries []game.Entry) (game.Digest, bool) {
	if e, ok := l.afterEntry(j, entries); ok {
		return e.Digest, true
	}
	return game.Digest{}, false
}

// afterEntry is the entry after the group ending at j-1: the next entry in the
// slice, or a window's next-turn sentinel (l.Against) when the group is last.
func (l *Lowered) afterEntry(j int, entries []game.Entry) (game.Entry, bool) {
	if j < len(entries) {
		return entries[j], true
	}
	if l.Against != nil && j < len(l.Against.Entries) {
		return l.Against.Entries[j], true
	}
	return game.Entry{}, false
}

// markShuffles leaves a seat's library order out of the comparison from the
// checkpoint of step onward when the group activates an ability whose text
// shuffles ("Shuffle your graveyard into your library"). Which seat's library
// the engines mix is theirs to decide, and gorge may not draw randomness for
// it at all (an empty graveyard), so the tape's RNG count cannot tell: the
// label can. Each engine's order is its own RNG's afterwards, never a rules
// difference; the count still compares. The entry "pN.library_top@K" is
// oraclediff's "from checkpoint K" form (checkpoint 0 is the setup).
func (l *Lowered) markShuffles(m seatMap, g []game.Entry, step int) {
	for _, e := range g {
		for _, p := range e.Picks {
			if p.Kind != "ability" || !strings.Contains(strings.ToLower(p.Label), "shuffle") {
				continue
			}
			ig := fmt.Sprintf("p%d.library_top@%d", m.seat(p.Player), step+1)
			if !slices.Contains(l.Item.Ignore, ig) {
				l.Item.Ignore = append(l.Item.Ignore, ig)
			}
		}
	}
}

func (l *Lowered) stop(at int, why *truncation) { l.Truncated, l.TruncatedAt = why.reason, at }

// unsettled is why the asks a pass posed (g[0] is the pass) cannot be
// answered, nil when the runner answers each: it finishes a resolution the
// pass began (the object on top of the stack is still there) and a trigger
// order ask, from the answers of the next step, before the pass's checkpoint
// (settleResolutionAsk, settleTriggerAsks). Any other ask is left pending and
// no later step can take it: `pass` needs priority, and no op of the table
// answers one pending decision alone.
func unsettled(g []game.Entry) *truncation {
	top := g[0].StackTop
	for _, a := range g[1:] {
		if a.Kind == "trigger_order" {
			continue
		}
		if top == 0 || a.StackTop != top {
			return unsupported("ask_after_pass:" + a.Kind)
		}
	}
	return nil
}

// settlesTwice is true for a pass group (g[0] is the pass) the runner settles
// in two passes before its checkpoint: an ask of the resolution the pass began
// (the object on top of the stack is still there) and, after the resolution
// finished, a trigger-order ask. Both borrow their answers from the next step
// that has any, and the second borrows the step's full script again instead of
// what the first left, so the answers owed to that step would be consumed once
// and then be left over (the Destined Confrontation sacrifice picks followed by
// Pirate Peddlers' order: "unconsumed answer(s)" on the next pass). The group's
// answers go on the pass itself: its own answers settle each ask in tape order
// and nothing is borrowed.
func settlesTwice(g []game.Entry) bool {
	top := g[0].StackTop
	if top == 0 {
		return false
	}
	resolution, order := false, false
	for _, a := range g[1:] {
		switch {
		case a.Kind == "trigger_order":
			order = true
		case a.StackTop == top:
			resolution = true
		}
	}
	return resolution && order
}

// drop keeps the first n steps: the pass group after them found no step to
// answer its asks.
func (l *Lowered) drop(n int) {
	l.Item.CutSteps(n)
	l.StepEntry = l.StepEntry[:n]
}

// markDieRolls lowers every recorded die roll to the step it belongs to:
// the first step whose StepEntry reaches the entry that rolled it (a group's
// mana taps have StepEntry -1, so they can never claim a roll). A roll in an
// entry the lowering stopped before belongs to no step and is dropped.
// Rolls are appended in tape and log order, so the item order is the order
// the engines make them.
func (l *Lowered) markDieRolls(m seatMap, entries []game.Entry) {
	if len(l.Item.DieRolls) > 0 || len(l.StepEntry) == 0 {
		return
	}
	for i, e := range entries {
		if len(e.Rolls) == 0 || i < l.FirstEntry {
			continue
		}
		step := -1
		for k := l.Prelude; k < len(l.StepEntry); k++ {
			if l.StepEntry[k] >= i {
				step = k
				break
			}
		}
		if step < 0 {
			continue
		}
		for _, r := range e.Rolls {
			l.Item.DieRolls = append(l.Item.DieRolls, oraclegen.DieRoll{
				Step: step, Seat: m.seat(r.Seat), Sides: r.Sides, Natural: r.Natural, Result: r.Result,
			})
		}
	}
}

// attach puts a step's asks on it. A cast or activation whose every target
// ask took its full Max goes through the step's targets (the runner hands them
// to the target decisions in order); anything else is an answer, which the
// runner matches to a decision by kind.
func attach(st *oraclegen.Step, answers []oraclegen.Answer, group []game.Entry) {
	asks := group[1:]
	useTargets := isPaidStep(st.Op) && len(asks) == len(answers)
	n := 0
	for _, a := range asks {
		if a.Kind != "target" {
			continue
		}
		n++
		if a.Max == 0 || len(a.Picks) != a.Max {
			useTargets = false
		}
	}
	if !useTargets || n == 0 {
		st.Answers = append(st.Answers, answers...)
		return
	}
	for k, a := range answers {
		if a.Kind == "target" {
			st.Targets = append(st.Targets, a.Pick...)
		} else {
			st.Answers = append(st.Answers, answers[k])
		}
	}
}

// rngAccounted reports whether every engine draw an entry consumed is one
// the tape records explicitly: a die roll or a coin flip. A die or an
// ordinary flip consumes one draw (gorge's rng.IntN); a FlipCoinMod static
// can force a flip heads without drawing (Flip.Forced), and a FlipCoinDoubler
// static (Krark's Thumb) makes coinResult draw twice for one flip
// (Flip.DrawCount). RNG must equal the rolls plus every flip's recorded draw
// count. A surplus draw is a shuffle or a random pick the runner's seeded RNG
// cannot land where the live game did. One shared test keeps the three gates
// (answersOf, resolveSpanLowerable, a window's HasRNG) from drifting apart.
func rngAccounted(e game.Entry) bool {
	draws := uint64(len(e.Rolls))
	for _, f := range e.Flips {
		draws += f.DrawCount()
	}
	return e.RNG == draws
}

// answersOf is the entries' asks as answers, in tape order; the first entry
// of g is the decision that posed them when it is not itself an ask. Every
// entry must be one the runner can be made to repeat.
func answersOf(m seatMap, g []game.Entry) ([]oraclegen.Answer, *truncation) {
	var answers []oraclegen.Answer
	fired := firedSources(g)
	for _, a := range g {
		switch {
		case a.Reanswered:
			return nil, unsupported("reanswered")
		case a.RNG > 0 && !rngAccounted(a) && !m.rngOK:
			// A shuffle or a random pick: the runner's seeded RNG cannot land
			// where the live game did. A roll or flip is exempt only when it
			// accounts for every draw the entry made.
			return nil, unsupported("rng")
		case len(a.Rest) > 0:
			return nil, unsupported(a.Kind + "_rest")
		}
		if !isAsk(a) || isWindowTap(a) || isManaAbilityAsk(a, fired) {
			continue
		}
		ans := oraclegen.Answer{Kind: a.Kind, Pick: []string{}}
		for _, p := range a.Picks {
			s, ok := m.pickString(p)
			if !ok {
				if p.Obj != 0 {
					return nil, &truncation{ReasonUnbindableRef}
				}
				return nil, unsupported(a.Kind)
			}
			ans.Pick = append(ans.Pick, s)
		}
		answers = append(answers, ans)
	}
	return answers, nil
}

// isWindowTap reports a mana-window ask answered by firing a mana ability. The
// tap is lowered ahead of the cast or activation (manaTaps), so the replay pays
// from its pool and no window opens to ask.
func isWindowTap(a game.Entry) bool {
	return len(a.Picks) == 1 && a.Picks[0].Kind == "activate" && len(a.Mana) == 1
}

// firedSources is the group's mana records by source object: whether the
// ability produced a recorded colour (see colourProduced).
func firedSources(g []game.Entry) map[state.ObjID]bool {
	var out map[state.ObjID]bool
	for _, e := range g {
		for _, mr := range e.Mana {
			if out == nil {
				out = map[state.ObjID]bool{}
			}
			out[mr.Obj] = out[mr.Obj] || mr.Produced != ""
		}
	}
	return out
}

// isManaAbilityAsk reports an ask of a mana ability the group fired: the
// "activate <source> for mana" window pick whose own entry carries no mana
// record (the hook fires on the colour pick that follows it), and the colour
// pick ("Add U") of a source whose recorded colour is known (fired maps a
// source to that). manaTaps lowers every fired ability as an explicit activate
// step ahead of the decision, answering the colour ask on that step itself, so
// neither is left for a later step to consume (the runner never poses them
// again: "unconsumed answer(s)"). A mana effect of a resolving object (no hook
// record) is not this and keeps its answer.
func isManaAbilityAsk(a game.Entry, fired map[state.ObjID]bool) bool {
	if len(a.Picks) != 1 {
		return false
	}
	known, ok := fired[a.Picks[0].Obj]
	switch a.Picks[0].Kind {
	case "activate":
		return ok
	case "mana":
		return ok && known
	}
	return false
}

var addColour = regexp.MustCompile(`^Add ([WUBRGC])$`)

// colourProduced fills in the colour of a mana record that has none from the
// colour pick that chose it. The recorder closes a record when the next
// decision is committed, so an ability that asks its colour is closed before
// the pick lands and carries no Produced; the pick ("Add U", a choose ask on
// the same source, in this or a later entry) is the colour the live game made.
// g is not modified.
func colourProduced(g []game.Entry) []game.Entry {
	var out []game.Entry
	for k, e := range g {
		if !isAsk(e) || len(e.Picks) != 1 || e.Picks[0].Kind != "mana" {
			continue
		}
		c := addColour.FindStringSubmatch(e.Picks[0].Label)
		if c == nil {
			continue
		}
		for q := 0; q <= k; q++ {
			for r, mr := range g[q].Mana {
				if mr.Obj != e.Picks[0].Obj || mr.Produced != "" {
					continue
				}
				if out == nil {
					out = append([]game.Entry(nil), g...)
				}
				out[q].Mana = slices.Clone(out[q].Mana)
				out[q].Mana[r].Produced = c[1]
				goto next
			}
		}
	next:
	}
	if out == nil {
		return g
	}
	return out
}

// resolveSpanLowerable reports that the entries between a pass group's end j
// and the empty-stack pass end hold only asks the resolve op can answer: no
// priority action (a mana tap, a cast, an ability) and no attack, block, mana
// or randomness. The resolve op passes priority and answers asks; a priority
// action in the span would be silently dropped, and the checkpoint after the
// step would no longer match (xe:RAND:0:11:tape lost Soul-Guide Lantern's
// sacrifice this way, and the later Scarecrow Guide cast was then not
// offered). resolvePass and resolveEmptyPass share the one guard so the next
// span shape cannot skip it.
func resolveSpanLowerable(entries []game.Entry, j, end int) bool {
	for k := j; k < end; k++ {
		e := entries[k]
		if len(e.Mana) > 0 || (e.RNG > 0 && !rngAccounted(e)) || len(e.Flips) > 0 {
			return false
		}
		if e.Kind == "priority" && !isPass(e) || e.Kind == "attackers" || e.Kind == "blockers" {
			return false
		}
	}
	return true
}

// resolvePass lowers a pass that resolved an object and put a new object on
// the stack as one `resolve` step. An ETB trigger is put on the stack after
// its creature spell resolved, so the pass's stack top (the spell) differs
// from the ask's (the trigger) and `pass` cannot carry the target answer: a
// pass answers exactly one priority decision. The runner's resolve op passes
// priority, answers the pending asks from the step's answers, and stops at
// the next priority with an empty stack. Only a target ask of a trigger that
// appeared after a non-empty pass is lowered this way; any other unsettled
// ask keeps the truncation.
func resolvePass(m seatMap, entries []game.Entry, i, j int) (*oraclegen.Step, int, bool) {
	if entries[i].StackTop == 0 {
		return nil, 0, false
	}
	seen := false
	for _, a := range entries[i+1 : j] {
		if a.Kind == "trigger_order" {
			continue
		}
		if a.Kind != "target" || a.StackTop == entries[i].StackTop {
			return nil, 0, false
		}
		seen = true
	}
	if !seen {
		return nil, 0, false
	}
	end := -1
	for k := j; k < len(entries); k++ {
		if entries[k].Kind == "priority" && entries[k].StackTop == 0 {
			end = k
			break
		}
	}
	if end <= j {
		// The stop state must lie after the asks, so the resolve step starts
		// on the pass with a non-empty stack and does not early-return.
		return nil, 0, false
	}
	if !resolveSpanLowerable(entries, j, end) {
		return nil, 0, false
	}
	answers, why := answersOf(m, entries[i:end])
	if why != nil {
		return nil, 0, false
	}
	return &oraclegen.Step{Op: "resolve", Seat: m.seat(entries[end].Seat), Answers: answers}, end, true
}

// resolveEmptyPass lowers an empty-stack pass that ends the step and only then
// puts a trigger with a target on the stack. The pass stays its own step (a
// resolve starting on an empty-stack priority returns before passing); the
// returned resolve step answers the trigger's asks and stops at the next
// empty-stack priority.
func resolveEmptyPass(m seatMap, entries []game.Entry, i, j int) (*oraclegen.Step, int, bool) {
	if entries[i].StackTop != 0 {
		return nil, 0, false
	}
	seen := false
	for _, a := range entries[i+1 : j] {
		if a.Kind == "trigger_order" {
			continue
		}
		if a.Kind != "target" || a.StackTop == 0 || a.StackTop == entries[i].StackTop {
			return nil, 0, false
		}
		seen = true
	}
	if !seen {
		return nil, 0, false
	}
	end := -1
	for k := j; k < len(entries); k++ {
		if entries[k].Kind == "priority" && entries[k].StackTop == 0 {
			end = k
			break
		}
	}
	if end <= j {
		return nil, 0, false
	}
	// The resolve op only passes priority and answers asks; it cannot replay a
	// priority action (a mana activation, a cast, a play) taken while the
	// trigger sat on the stack, nor its mana/randomness. Reject such a range so
	// the checkpoint cannot silently lose it (xe:TDM:0:0:w29).
	if !resolveSpanLowerable(entries, j, end) {
		return nil, 0, false
	}
	answers, why := answersOf(m, entries[i+1:end])
	if why != nil {
		return nil, 0, false
	}
	return &oraclegen.Step{Op: "resolve", Seat: m.seat(entries[end].Seat), Answers: answers}, end, true
}

// lowerGroup lowers one decision and the asks it posed: the steps (the last is
// the decision's own, any before it are explicit mana taps), their entry
// indices (-1 for the taps; the caller fills the last), and the asks as
// answers, in tape order.
func lowerGroup(reg *cards.Registry, m seatMap, g []game.Entry) ([]oraclegen.Step, []int, []string, []oraclegen.Answer, *truncation) {
	g = colourProduced(g)
	en := g[0]
	var steps []oraclegen.Step
	var why *truncation
	var mana []game.ManaRec
	var spent string
	for _, e := range g {
		if !paysAfterAsks(en) && isResolvePay(e) {
			// The planner's funding of a resolve-time payment: lowerEntries
			// floats it ahead of the pass that resolves the object.
			continue
		}
		mana = append(mana, e.Mana...)
		spent += e.Spent
	}
	switch en.Kind {
	case "priority":
		steps, why = lowerPriority(reg, m, en, mana)
	case "attackers":
		steps, why = lowerAttack(m, en)
	case "blockers":
		steps, why = lowerBlock(m, en)
	}
	if why != nil {
		return nil, nil, nil, nil, why
	}
	if len(steps) == 0 {
		return nil, nil, nil, nil, unsupported("empty_" + en.Kind)
	}
	if why := sacrificeTapTrigger(reg, en, mana); why != nil {
		return nil, nil, nil, nil, why
	}
	if len(mana) > len(en.Mana) && !paysAfterAsks(en) {
		// Mana fired while an ask was being answered, for a decision that is
		// not a cast or activation: nothing to tap it ahead of.
		return nil, nil, nil, nil, unsupported("ask_mana")
	}
	answers, why := answersOf(m, g)
	if why != nil {
		return nil, nil, nil, nil, why
	}
	idx := make([]int, len(steps))
	pay := make([]string, len(steps))
	for i := range idx {
		idx[i] = -1
	}
	if spent != "" {
		// The group's spend belongs to the step that paid it, the last step:
		// the cast or the activation (including a mana ability activation,
		// whose own cost the recorded game already paid). Every earlier step is
		// an explicit mana tap that adds pool mana. The placement comes from the
		// lowered step's role, not the option kind that produced it, so a future
		// paying option kind cannot silently lose its spend.
		if last := len(steps) - 1; isPaidStep(steps[last].Op) {
			pay[last] = spent
		}
	}
	return steps, idx, pay, answers, nil
}

// isPaidStep reports whether a lowered step pays a cost from its controller's
// pool: a spell cast or a permanent/ability activation. It is the one place the
// lowering decides what a payment can attach to.
func isPaidStep(op string) bool {
	return op == "cast" || op == "activate"
}

// lowerPriority lowers a priority decision. mana is every mana ability fired
// by committing it and the asks it posed: a cast pays after its targets and
// modes are chosen, so the taps are recorded on those entries, not the cast.
// paysAfterAsks is true for a priority decision whose cost is paid after the
// asks it poses (targets, modes, X) are answered: a cast or an ability.
func paysAfterAsks(en game.Entry) bool {
	return en.Kind == "priority" && len(en.Picks) == 1 && (en.Picks[0].Kind == "cast" || en.Picks[0].Kind == "ability")
}

func lowerPriority(reg *cards.Registry, m seatMap, en game.Entry, mana []game.ManaRec) ([]oraclegen.Step, *truncation) {
	if len(en.Picks) != 1 {
		return nil, unsupported("priority_picks")
	}
	pk := en.Picks[0]
	seat := m.stepSeat(en)
	switch pk.Kind {
	case "pass":
		return []oraclegen.Step{{Op: "pass", Seat: seat}}, nil
	case "play_land":
		if pk.Ref == "" {
			return nil, &truncation{ReasonUnbindableRef}
		}
		return []oraclegen.Step{{Op: "play", Seat: seat, Card: m.ref(pk.Ref)}}, nil
	case "cast", "ability":
		taps, why := manaTaps(reg, m, mana)
		if why != nil {
			return nil, why
		}
		if pk.Ref == "" {
			return nil, &truncation{ReasonUnbindableRef}
		}
		if pk.Kind == "cast" {
			mode := pk.Mode
			if (mode == "" || mode == "mayplay") && pk.AltCost == 1 {
				mode = "alternative"
			}
			return append(taps, oraclegen.Step{Op: "cast", Seat: seat, Card: m.ref(pk.Ref), CastMode: mode}), nil
		}
		k := pk.Ability
		return append(taps, oraclegen.Step{Op: "activate", Seat: seat, Card: m.ref(pk.Ref), AbilityIndex: &k}), nil
	case "activate":
		// "Activate <card> for mana": the entry's own mana record is the
		// ability, which the runner finds by its index.
		if len(en.Mana) != 1 || en.Mana[0].Obj != pk.Obj {
			return nil, unsupported("activate")
		}
		taps, why := manaTaps(reg, m, en.Mana)
		if why != nil {
			return nil, why
		}
		return taps, nil
	}
	return nil, unsupported(pk.Kind)
}

// manaTaps is one activate step per mana ability the entry fired, in order. A
// source whose ability asks which colour it makes (Produced$ Any or Combo) gets
// the colour the live game made as a "choose" answer, which the runner's mana
// stage reads instead of taking the first colour.
func manaTaps(reg *cards.Registry, m seatMap, mana []game.ManaRec) ([]oraclegen.Step, *truncation) {
	var out []oraclegen.Step
	for _, mr := range mana {
		if mr.Ref == "" {
			return nil, &truncation{ReasonUnbindableRef}
		}
		if mr.Ability < 0 {
			return nil, unsupported("mana_ability")
		}
		k := mr.Ability
		st := oraclegen.Step{Op: "activate", Seat: m.seat(mr.Player), Card: m.ref(mr.Ref), AbilityIndex: &k}
		if picks := colourPicks(reg, mr); picks != nil {
			st.Answers = []oraclegen.Answer{{Kind: "choose", Pick: picks}}
		}
		out = append(out, st)
	}
	return out, nil
}

// colourPicks is the colours to answer a mana ability's colour ask with, nil
// when the ability asks none (a fixed colour) or the live activation's
// colours are unknown. "Any" is one pick; a "Combo" ask takes one per unit.
func colourPicks(reg *cards.Registry, mr game.ManaRec) []string {
	if mr.Produced == "" {
		return nil
	}
	sa := manaSA(reg, mr)
	if sa == nil {
		return nil
	}
	produced := effects.ManaOf(sa).Produced
	if _, combo := effects.ComboColours(produced); combo {
		var out []string
		for _, r := range mr.Produced {
			out = append(out, string(r))
		}
		return out
	}
	if strings.EqualFold(produced, "Any") {
		return []string{mr.Produced[:1]}
	}
	return nil
}

// sacrificeTapTrigger refuses a group whose explicit mana taps sacrifice their
// source (a Treasure or Gold token) while a permanent triggers on a sacrifice.
// The live game sacrificed the source while paying, and the trigger went on the
// stack after the cast; the lowering taps ahead of the cast, so the trigger
// would sit on the stack first, blocking a sorcery-speed cast and asking its
// target out of order.
func sacrificeTapTrigger(reg *cards.Registry, en game.Entry, mana []game.ManaRec) *truncation {
	if !paysAfterAsks(en) || !sacrificeTrigger(reg, en.Digest.Field) {
		return nil
	}
	for _, mr := range mana {
		if sa := manaSA(reg, mr); sa != nil && strings.Contains(sa.Params["Cost"], "Sac<") {
			return unsupported("sacrifice_trigger_tap")
		}
	}
	return nil
}

// manaSA is the mana ability a recorded activation ran: the card's, or when no
// card has the name (a token: "Treasure Token"), the token definition's. Token
// stems can share a face name, so the token's ability is used only when every
// stem with that name and ability agrees on its Produced$ and Cost$; the answer
// does not depend on map order. nil when it cannot be told.
func manaSA(reg *cards.Registry, mr game.ManaRec) *cards.SA {
	if c, ok := reg.Lookup(mr.Name); ok {
		return faceAbility(c, mr)
	}
	var first *cards.SA
	for _, c := range reg.Tokens {
		sa := faceAbility(c, mr)
		if sa == nil {
			continue
		}
		if first == nil {
			first = sa
		} else if effects.ManaOf(sa).Produced != effects.ManaOf(first).Produced || sa.Params["Cost"] != first.Params["Cost"] {
			return nil
		}
	}
	return first
}

// sacrificeTrigger reports whether a permanent on the battlefield has a
// "whenever you sacrifice" trigger.
func sacrificeTrigger(reg *cards.Registry, field []game.PermDigest) bool {
	for _, p := range field {
		if c, ok := reg.Lookup(p.Name); ok {
			for _, f := range c.Faces {
				for _, t := range f.Triggers {
					if t.Mode == "Sacrificed" {
						return true
					}
				}
			}
		}
	}
	return false
}

// faceAbility is the ability mr.Ability of c's face named mr.Name, or nil.
func faceAbility(c *cards.Card, mr game.ManaRec) *cards.SA {
	for _, f := range c.Faces {
		if f.Name == mr.Name && mr.Ability >= 0 && mr.Ability < len(f.Abilities) {
			return f.Abilities[mr.Ability]
		}
	}
	return nil
}

func lowerAttack(m seatMap, en game.Entry) ([]oraclegen.Step, *truncation) {
	def := -1
	st := oraclegen.Step{Op: "attack", Seat: m.stepSeat(en), Attackers: []string{}}
	for _, p := range en.Picks {
		if p.Battle != 0 {
			return nil, &truncation{ReasonAttackNonPlayer}
		}
		if def >= 0 && p.Player != def {
			return nil, &truncation{ReasonAttackSplit}
		}
		def = p.Player
		if p.Ref == "" {
			return nil, &truncation{ReasonUnbindableRef}
		}
		st.Attackers = append(st.Attackers, m.ref(p.Ref))
	}
	if def < 0 {
		def = 1 - en.Seat // no attackers: the declaration still names the opponent
	}
	st.Defender = m.player(def)
	return []oraclegen.Step{st}, nil
}

func lowerBlock(m seatMap, en game.Entry) ([]oraclegen.Step, *truncation) {
	st := oraclegen.Step{Op: "block", Seat: m.stepSeat(en), Blocks: [][2]string{}}
	for _, p := range en.Picks {
		if p.Ref == "" || p.AttackerRef == "" {
			return nil, &truncation{ReasonUnbindableRef}
		}
		st.Blocks = append(st.Blocks, [2]string{m.ref(p.Ref), m.ref(p.AttackerRef)})
	}
	return []oraclegen.Step{st}, nil
}
