package lower

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
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
	ReasonStall           = "stall"            // the tape ends in a watchdog, not a game end
	ReasonUnsupported     = "unsupported_kind:"
)

// Lowered is a tape lowered to one oracle scenario.
type Lowered struct {
	// Item is the scenario. Ignore and Compare stay empty (the classifier
	// sets them); so do the follower-specific fields.
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

// itemID is xe:<set>:<chunk>:<game>:tape; chunk and game come from the deck
// name decks.Pair gave the tape's first deck (the seed stands in for both when
// the name is not one of those).
func itemID(t *game.Tape) string {
	if m := deckName.FindStringSubmatch(t.Decks[0].Name); m != nil {
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

// Tape lowers t. A tape the lowering cannot express in full yields the
// longest lowerable prefix with Truncated set; only a tape that cannot be
// read as a game at all (an unknown card) is an error. The result is a pure
// function of t.
func Tape(reg *cards.Registry, t *game.Tape) (Lowered, error) {
	m := seatMap{swap: t.Starter == 1}
	for s := range t.Open {
		for _, n := range append(append([]string{}, t.Open[s].Hand...), t.Open[s].Library...) {
			if _, ok := reg.Lookup(n); !ok {
				return Lowered{}, fmt.Errorf("lower: seat %d card %q not in the corpus", s, n)
			}
		}
	}
	sc := oraclegen.Scenario{
		Name:  "xengine tape " + t.Decks[0].Name + " vs " + t.Decks[1].Name,
		Turn:  1,
		CR:    []string{},
		Why:   "A recorded gorge game between two explore bots, replayed from turn 1.",
		Setup: map[string]oraclegen.Seat{},
	}
	for s := range t.Open {
		sc.Setup["p"+strconv.Itoa(m.seat(s))] = oraclegen.Seat{
			Hand:       append([]string{}, t.Open[s].Hand...),
			LibraryTop: append([]string{}, t.Open[s].Library...),
		}
	}
	l := Lowered{
		Item: oraclegen.Item{ID: itemID(t), Template: Template, Scenario: sc},
		Swap: m.swap, StepEntry: []int{},
	}
	l.Item.Steps = []oraclegen.Step{}

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
			if len(e.Mana) > 0 || e.RNG > 0 || len(e.Flips) > 0 {
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
		steps, stepEntry, payments, answers, why := lowerGroup(reg, m, entries[i:j])
		if why != nil {
			at := i
			if owedAt >= 0 {
				// The pass before it was waiting for this group's first step.
				at = owedAt
				l.drop(owedSteps)
			}
			l.stop(at, why)
			return
		}
		if len(owed) == 0 && len(steps) > 0 && quiet(entries[i:j]) {
			if op := steps[len(steps)-1].Op; op == "cast" || op == "activate" {
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
		if len(owed) > 0 {
			// The pass before this group was waiting for a step's answers;
			// this group's first step or the resolve step takes them.
			dst := &steps[0]
			if resolve != nil {
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
			l.Item.Steps = append(l.Item.Steps, *resolve)
			l.StepEntry = append(l.StepEntry, resolveEnd-1)
			l.Item.Payment = append(l.Item.Payment, "")
			l.markShuffles(m, entries[i:resolveEnd], len(l.Item.Steps)-1)
			i = resolveEnd
			continue
		}
		if steps[mainStep].Op == "pass" && len(answers) > 0 {
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

// drop keeps the first n steps: the pass group after them found no step to
// answer its asks.
func (l *Lowered) drop(n int) {
	l.Item.Steps = l.Item.Steps[:n]
	l.StepEntry = l.StepEntry[:n]
	l.Item.Payment = l.Item.Payment[:n]
}

// attach puts a step's asks on it. A cast or activation whose every target
// ask took its full Max goes through the step's targets (the runner hands them
// to the target decisions in order); anything else is an answer, which the
// runner matches to a decision by kind.
func attach(st *oraclegen.Step, answers []oraclegen.Answer, group []game.Entry) {
	asks := group[1:]
	useTargets := (st.Op == "cast" || st.Op == "activate") && len(asks) == len(answers)
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

// answersOf is the entries' asks as answers, in tape order; the first entry
// of g is the decision that posed them when it is not itself an ask. Every
// entry must be one the runner can be made to repeat.
func answersOf(m seatMap, g []game.Entry) ([]oraclegen.Answer, *truncation) {
	var answers []oraclegen.Answer
	for _, a := range g {
		switch {
		case a.Reanswered:
			return nil, unsupported("reanswered")
		case a.RNG > 0 && len(a.Flips) == 0 && !m.rngOK:
			// A shuffle or a random pick: the runner's seeded RNG cannot land
			// where the live game did.
			return nil, unsupported("rng")
		case len(a.Rest) > 0:
			return nil, unsupported(a.Kind + "_rest")
		}
		if !isAsk(a) {
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
	for k := j; k < end; k++ {
		e := entries[k]
		if len(e.Mana) > 0 || e.RNG > 0 || len(e.Flips) > 0 {
			return nil, 0, false
		}
		if e.Kind == "priority" && !isPass(e) || e.Kind == "attackers" || e.Kind == "blockers" {
			return nil, 0, false
		}
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
	en := g[0]
	var steps []oraclegen.Step
	var why *truncation
	var mana []game.ManaRec
	var spent string
	for _, e := range g {
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
	if paysAfterAsks(en) && spent != "" {
		// The group's spend belongs to its cast or activation, the last step;
		// every earlier step is an explicit mana tap that adds pool mana.
		pay[len(steps)-1] = spent
	}
	return steps, idx, pay, answers, nil
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
			if mode == "" && pk.AltCost == 1 {
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
	c, ok := reg.Lookup(mr.Name)
	if !ok {
		return nil
	}
	var sa *cards.SA
	for _, f := range c.Faces {
		if f.Name == mr.Name && mr.Ability < len(f.Abilities) {
			sa = f.Abilities[mr.Ability]
			break
		}
	}
	if sa == nil || mr.Produced == "" {
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
