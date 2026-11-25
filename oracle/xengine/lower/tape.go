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
	"github.com/adams-shaun/gorge/engine/rules"
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
	ReasonRNG             = "rng"              // an engine draw that is neither a recorded outcome nor a library shuffle
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
	l.markCoinFlips(m, t.Entries)
	l.compactPayment()
	return l, nil
}

// withoutTriggeredMana is entries with every triggered mana record (see
// game.ManaRec.Triggered) dropped. The lowering indexes an entry's records
// against its tap steps, spend and funding, one step per record; a triggered
// record has no step, so it is removed once here, ahead of all of them,
// instead of in each. The mana it adds is the tap's own outcome (CR 605.4a):
// the follower gets it from the tap step, and the recorded spend is split by
// the Cost$ of the activated records, which a triggered record has none of.
// entries is not modified; the same slice returns when nothing is dropped.
func withoutTriggeredMana(entries []game.Entry) []game.Entry {
	var out []game.Entry
	for i, e := range entries {
		if !slices.ContainsFunc(e.Mana, func(mr game.ManaRec) bool { return mr.Triggered }) {
			if out != nil {
				out[i] = e
			}
			continue
		}
		if out == nil {
			out = slices.Clone(entries)
		}
		e.Mana = slices.DeleteFunc(slices.Clone(e.Mana), func(mr game.ManaRec) bool { return mr.Triggered })
		out[i] = e
	}
	if out == nil {
		return entries
	}
	return out
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
	entries = withoutTriggeredMana(entries)
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
	// shuffleZone is the pre-action digest of the latest entry whose engine
	// draws are not accounted for (a shuffle or a random pick), and
	// shuffleMoved the card names that entry's own picks named (a search puts
	// the fetched card in hand before the shuffle). The runner's seeded RNG
	// cannot land where the live game did (CR 701.24a), so a later play or cast
	// that names a card the shuffle could have drawn into hand cannot be
	// replayed; one the seat already held at the shuffle, or one this
	// resolution fetched, is still there.
	var shuffleZone *game.Digest
	shuffleMoved := map[string]bool{}
	// shuffleZones is true when the shuffle also moves cards into the library
	// (Weftwalking: "shuffle your hand and graveyard into your library"), so a
	// card the seat held beforehand is not safe either: the live game shuffled
	// it away and every redraw is the runner's own.
	shuffleZones := false
	// passes is the live game's consecutive priority passes since its last
	// reset, which the digest does not carry: a resolution, a step advance or
	// any other priority action resets it to zero. -1 means a resolve step hid
	// the exact count; an unknown non-zero count is never safe to drop across.
	passes := 0
	counted := func(op string, taps bool) {
		if taps || op != "pass" {
			passes = 0
			return
		}
		if passes < 0 {
			return
		}
		passes++
		if passes >= 2 {
			passes = 0
		}
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
		// A play or cast after an unaccounted shuffle names the live game's
		// exact object ("p0:Island#34"). The runner's seeded shuffle draws a
		// different Island into the hand, so the step it names is not offered
		// (Weftwalking: "play p0:Island#34 not offered" while seven Play Island
		// options were offered). Cutting here keeps every emitted step
		// replayable and lets the followers use the prefix.
		if shuffleZone != nil && groupsDependOnShuffle(entries[i:j], *shuffleZone, shuffleMoved, shuffleZones) {
			l.refuse(i, unsupported("shuffled_ref"), owedAt, owedSteps)
			return
		}
		if d, moved, ok := groupShuffle(entries[i:j]); ok {
			shuffleZone, shuffleMoved = &d, moved
			shuffleZones = zoneMovingShuffle(reg, entries, j)
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
					// Committing it still reset the pass count the digest
					// cannot show, so dropping it is only safe when the count
					// is already zero; otherwise the next pass would look like
					// the second in the chain and advance the step early
					// (Hypnotic Sprite's refused cast).
					if passes != 0 {
						l.refuse(i, unsupported("pass_chain"), owedAt, owedSteps)
						return
					}
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
			if fund.before {
				passes = 1 // the funding tap reset the count before the pass it precedes
			}
			l.Item.Steps = append(l.Item.Steps, *resolve)
			l.StepEntry = append(l.StepEntry, resolveEnd-1)
			l.Item.Payment = append(l.Item.Payment, "")
			l.markShuffles(m, entries[i:resolveEnd], len(l.Item.Steps)-1)
			// The resolve op passes priority and may answer several consecutive
			// passes; the count it leaves is not recoverable from its steps.
			passes = -1
			i = resolveEnd
			continue
		}
		l.pushFunding(fund)
		if fund.before {
			passes = 1 // the funding tap reset the count before the pass it precedes
		}
		if steps[mainStep].Op == "pass" && len(answers) > 0 && !settlesTwice(entries[i:j]) {
			owed, owedAt, owedSteps = answers, i, len(l.Item.Steps)
		} else if len(answers) > 0 {
			attach(&steps[mainStep], answers, entries[i:j])
		}
		stepEntry[mainStep] = j - 1
		counted(steps[mainStep].Op, len(steps) > 1 || len(fund.taps) > 0 && !fund.before)
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
	// pay is parallel to taps: the pool mana each funding activation's own
	// cost spent (see fundingTaps). "" for a free one.
	pay []string
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
	// fundSpend is one isResolvePay entry's recorded spend: the pool mana its
	// funding activations cost in activation order, followed by the
	// resolve-time charge itself (the recorder flushes the commit's whole
	// spend onto the election's own entry), and the index in mana of the
	// entry's first record. The election's commit can fire several funding
	// activations (`resPayAutoStep` runs the planner's activations one at a
	// time), so the spend is split per record, in activation order.
	type fundSpend struct {
		recs  []game.ManaRec
		first int
		spent string
	}
	var spends []fundSpend
	// The slice may hold a mana colour pick for a funding activation that asked
	// one: fold it into the record's Produced the same way lowerGroup does for
	// its own group. Read the unanswered records first: colourProduced erases
	// the "the activation stopped for a colour decision" signal.
	asked := colourAskedSources(entries[i:end])
	scan := colourProduced(entries[i:end])
	for _, e := range scan {
		if !isResolvePay(e) {
			continue
		}
		first := len(mana)
		mana = append(mana, e.Mana...)
		if len(e.Mana) > 0 && e.Spent != "" {
			spends = append(spends, fundSpend{e.Mana, first, e.Spent})
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
	taps, why := manaTaps(reg, m, mana, asked)
	if why != nil {
		return fail
	}
	// Each entry's spend is one flush: its funding activations' own costs in
	// activation order (a Signet's {1}), then the resolve-time charge. Walk
	// the records in that order and give each the mana units its own Cost$
	// names, taken from the front of what remains; that prefix rides the tap
	// step the activation is lowered as — the only step kind a follower
	// reads a payment from (oraclegen's Payment is "the pool mana a cast or
	// activate step must spend"), so a follower that honours Item.Payment
	// pays the recorded split there. Whatever remains is the resolve-time
	// charge itself: the follower engine pays it from its floating pool at
	// the resolve step, so spending it early would leave the pool short of
	// exactly those units at the resolve's checkpoint.
	pay := make([]string, len(taps))
	for _, sp := range spends {
		rest := sp.spent
		for k, mr := range sp.recs {
			n := manaCostUnit(reg, mr)
			if n > len(rest) {
				n = len(rest)
			}
			pay[sp.first+k] += rest[:n]
			rest = rest[n:]
		}
	}
	openPass := func(k int) bool {
		return k >= 0 && isPass(entries[k]) && entries[k].Seat == payer && entries[k].StackTop != 0
	}
	if openPass(i) {
		return funding{taps: taps, pay: pay}
	}
	if n := len(l.Item.Steps); n > 0 && openPass(i-1) && l.StepEntry[n-1] == i-1 &&
		l.Item.Steps[n-1].Op == "pass" && l.Item.Steps[n-1].Seat == m.seat(payer) {
		return funding{taps: taps, pay: pay, before: true}
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
	l.Item.Payment = slices.Insert(l.Item.Payment, at, f.pay...)
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
		step := l.stepOfEntry(i)
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

// markCoinFlips lowers every recorded coin flip to the step it belongs to,
// the same way markDieRolls does (one shared stepOfEntry, so the two cannot
// place a result on different steps). Forge's FlipCoinEffect draws its result
// from its own seeded stream, so a follower only agrees with gorge when the
// driver forces the tape's result before the step. Forced and Draws ride
// along: a flip a FlipCoinMod static forced heads takes no result from the
// driver, and a doubler flip (Krark's Thumb) takes two.
func (l *Lowered) markCoinFlips(m seatMap, entries []game.Entry) {
	if len(l.Item.CoinFlips) > 0 || len(l.StepEntry) == 0 {
		return
	}
	for i, e := range entries {
		if len(e.Flips) == 0 || i < l.FirstEntry {
			continue
		}
		step := l.stepOfEntry(i)
		if step < 0 {
			continue
		}
		for _, f := range e.Flips {
			l.Item.CoinFlips = append(l.Item.CoinFlips, oraclegen.CoinFlip{
				Step: step, Seat: m.seat(f.Seat), Win: f.Win, Forced: f.Forced, Draws: f.Draws,
			})
		}
	}
}

// stepOfEntry is the first step (past the prelude) whose StepEntry reaches
// tape entry i, or -1 when the lowering stopped before it. A group's mana
// taps have StepEntry -1, so they can never claim an entry's results.
func (l *Lowered) stepOfEntry(i int) int {
	for k := l.Prelude; k < len(l.StepEntry); k++ {
		if l.StepEntry[k] >= i {
			return k
		}
	}
	return -1
}

// attach puts a step's asks on it. A cast or activation whose every target
// ask took its full Max goes through the step's targets (the runner hands them
// to the target decisions in order); anything else is an answer, which the
// runner matches to a decision by kind. A target ask posed after a triggered
// ability went on the stack is that trigger's own target (CR 603.3d), not the
// step's: the cast step has no such target to name, so the ask stays an answer
// and never joins the step's targets, which the drivers hand to the spell.
func attach(st *oraclegen.Step, answers []oraclegen.Answer, group []game.Entry) {
	asks := group[1:]
	own := ownAsks(group)
	useTargets := isPaidStep(st.Op) && len(asks) == len(answers)
	n := 0
	for k, a := range asks {
		if a.Kind != "target" || k >= own {
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
		if a.Kind == "target" && k < own {
			st.Targets = append(st.Targets, a.Pick...)
		} else {
			st.Answers = append(st.Answers, answers[k])
		}
	}
}

// ownAsks is how many of a group's asks (group[1:]) belong to the group's own
// decision: the ones posed before any entry's commit put a triggered ability
// on the stack. The recorder hands a trigger to the entry committed before
// its ask is posed, so every ask after the first entry with Triggers belongs
// to a trigger (a cast trigger's target, CR 603.3d), never to the spell or
// ability being put on the stack.
func ownAsks(group []game.Entry) int {
	for k, e := range group {
		if len(e.Triggers) > 0 {
			return k
		}
	}
	return len(group) - 1
}

// recordedDraws is the number of engine draws an entry's recorded outcomes
// account for: one per die roll, and every flip's own count (0 for a flip a
// FlipCoinMod static forced, 2 under a FlipCoinDoubler static).
func recordedDraws(e game.Entry) uint64 {
	draws := uint64(len(e.Rolls))
	for _, f := range e.Flips {
		draws += f.DrawCount()
	}
	return draws
}

// rngAccounted reports whether every engine draw an entry consumed is one
// the tape records explicitly: a die roll or a coin flip. A die or an
// ordinary flip consumes one draw (gorge's rng.IntN); a FlipCoinMod static
// can force a flip heads without drawing (Flip.Forced), and a FlipCoinDoubler
// static (Krark's Thumb) makes coinResult draw twice for one flip
// (Flip.DrawCount). RNG must equal the rolls plus every flip's recorded draw
// count. A surplus draw is a shuffle or a random pick the runner's seeded RNG
// cannot land where the live game did. One shared test keeps the gates
// (answersOf, resolveSpanLowerable, a window's RNG cut) from drifting apart.
func rngAccounted(e game.Entry) bool { return e.RNG == recordedDraws(e) }

// rngShuffle reports whether e's surplus draws are a library shuffle. gorge's
// only random operation that consumes more than one value is the Fisher-Yates
// Shuffle over one seat's library (rules.rng.Shuffle), which draws
// len(library)-1 values; after is the digest of the state the entry left, so a
// seat's library_count names the size of the library it shuffled, and e.Digest
// (taken before the intent) must show the top of that same library moved.
// Every other surplus draw -- Crystalline Giant's random counter kind, a
// random discard, a random target -- draws for the pick it made, and that
// outcome can land on the board rather than only in the library order a
// window's HasRNG leaves out. A shuffle that also drew or moved a card after
// its pass no longer matches the count and is cut too: the window loses a
// step, never agreement.
func rngShuffle(e game.Entry, after game.Digest) bool {
	draws := recordedDraws(e)
	if e.RNG <= draws {
		return false
	}
	surplus := e.RNG - draws
	for i, p := range after.Players {
		if p.LibraryCount <= 1 || surplus != uint64(p.LibraryCount-1) {
			continue
		}
		// A shuffle reorders the library, so the top five cannot stay put. A
		// matching count with an unchanged top is a pick that drew the same
		// number as a shuffle would (a one-draw pick at a two-card library),
		// not a shuffle, and must be cut.
		if i < len(e.Digest.Players) && !slices.Equal(e.Digest.Players[i].LibraryTop, p.LibraryTop) {
			return true
		}
	}
	return false
}

// groupShuffle reports a group that consumed engine randomness the tape does
// not account for, with the state before its decision and the card names its
// own picks named. answersOf's rng gate is bypassed in a window or span, so
// the group is lowered; the runner's seeded order is still its own.
func groupShuffle(g []game.Entry) (game.Digest, map[string]bool, bool) {
	shuffles := false
	for _, e := range g {
		if e.RNG > 0 && !rngAccounted(e) {
			shuffles = true
			break
		}
	}
	if !shuffles || len(g) == 0 {
		return game.Digest{}, nil, false
	}
	moved := map[string]bool{}
	for _, e := range g {
		for _, p := range e.Picks {
			if n := refName(p.Ref); n != "" {
				moved[n] = true
			}
		}
	}
	return g[0].Digest, moved, true
}

// groupsDependOnShuffle reports a group whose play or cast names a card the
// shuffle could have moved into hand. A card the seat already held at a pure
// library shuffle, or one this resolution moved (a search puts the fetched
// card into hand before it shuffles), is still there in the runner; a card
// drawn after the shuffle is the runner's own draw (CR 701.24a), so the live
// object it names may not be in hand. zones says the shuffle also moved the
// hand/graveyard into the library, which invalidates every held card.
func groupsDependOnShuffle(g []game.Entry, before game.Digest, moved map[string]bool, zones bool) bool {
	if len(g) == 0 {
		return false
	}
	for _, p := range g[0].Picks {
		if p.Kind != "play_land" && p.Kind != "cast" {
			continue
		}
		seat, name := refSeatName(p.Ref)
		if name == "" || !moved[name] && (zones || !seatHeld(before, seat, name)) {
			return true
		}
	}
	return false
}

// zoneMovingShuffle reports whether a trigger recorded up to entry j shuffles
// cards from a non-library zone into its owner's library, so the shuffle moved
// cards the pre-shuffle digest still lists as held. The trigger's own SA chain
// carries the parsed parameters (Origin$, Destination$, Shuffle$), not a text
// heuristic.
func zoneMovingShuffle(reg *cards.Registry, entries []game.Entry, j int) bool {
	if reg == nil {
		return false
	}
	for k := 0; k < j && k < len(entries); k++ {
		for _, tr := range entries[k].Triggers {
			c, ok := reg.Lookup(tr.Name)
			if !ok || tr.Face < 0 || tr.Face >= len(c.Faces) {
				continue
			}
			f := c.Faces[tr.Face]
			if tr.Index < 0 || tr.Index >= len(f.Triggers) {
				continue
			}
			for sa := f.Triggers[tr.Index].Effect; sa != nil; sa = sa.Sub {
				if !strings.EqualFold(sa.Params["Shuffle"], "True") ||
					!strings.Contains(sa.Params["Destination"], "Library") {
					continue
				}
				if o := sa.Params["Origin"]; strings.Contains(o, "Hand") || strings.Contains(o, "Graveyard") {
					return true
				}
			}
		}
	}
	return false
}

// seatHeld reports whether seat's pre-shuffle digest lists name in a zone a
// library shuffle does not move: hand, graveyard, exile or command. The deck's
// own order is the engine's, but a card the seat already held stays put.
//
// The comparison is by name: a Digest carries names, not the setup-bound refs
// ("p0:Island#2") the runner resolves, so a hand holding a different object of
// the same name is not distinguished. Closing that would need the setup
// binder's refs, which the tape does not carry; a span that could expose it
// either stops at the window's rng cut (a shuffle-plus-draw entry) or has
// every same-name object interchangeable to the digest, and the self-check
// still rejects a step the runner cannot offer.
func seatHeld(d game.Digest, seat int, name string) bool {
	for _, p := range d.Players {
		if p.Seat != seat {
			continue
		}
		for _, z := range [][]string{p.Hand, p.Graveyard, p.Exile, p.Command} {
			if slices.Contains(z, name) {
				return true
			}
		}
		return false
	}
	return false
}

// refName is the card name in a runner ref ("p1:Island#34" -> "Island").
func refName(r string) string {
	_, name := refSeatName(r)
	return name
}

// refSeatName splits a runner card ref into its owner seat and card name. ok is
// false for an empty or non-seat ref.
func refSeatName(r string) (seat int, name string) {
	if len(r) < 3 || r[0] != 'p' {
		return 0, ""
	}
	i := strings.IndexByte(r, ':')
	if i < 2 {
		return 0, ""
	}
	n, err := strconv.Atoi(r[1:i])
	if err != nil {
		return 0, ""
	}
	name = r[i+1:]
	if j := strings.IndexByte(name, '#'); j >= 0 {
		name = name[:j]
	}
	return n, name
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

// addColour is the colour-wheel label shape: a bare "Add R", or the same
// production behind the ability's cost prefix ("Pay 1 life: Add G"). The
// prefix is named on the colour options too (gorge's askManaColor), so an
// exact bare match dropped every costed carrier.
var addColour = regexp.MustCompile(`^(?:.*: )?Add ([WUBRGC])$`)

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
	// Read which activation stopped for a colour ask before colourProduced
	// fills its Produced from the pick that answered it.
	asked := colourAskedSources(g)
	g = colourProduced(g)
	en := g[0]
	var steps []oraclegen.Step
	var why *truncation
	var mana []game.ManaRec
	// recAt is, per entry, where its mana records sit in the group's
	// aggregate: the lowering lowers each record as the tap step at that
	// index of the group's steps. An entry skipped for a resolve-time
	// payment has -1.
	recAt := make([]int, len(g))
	for k, e := range g {
		recAt[k] = -1
		if !paysAfterAsks(en) && isResolvePay(e) {
			// The planner's funding of a resolve-time payment: lowerEntries
			// floats it ahead of the pass that resolves the object.
			continue
		}
		recAt[k] = len(mana)
		mana = append(mana, e.Mana...)
	}
	switch en.Kind {
	case "priority":
		steps, why = lowerPriority(reg, m, en, mana, asked)
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
	if len(mana) > len(en.Mana) && !paysAfterAsks(en) && !priorityManaTap(en, mana) {
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
	// Each entry's Spent is the cost its own commit paid, so it belongs to
	// the step that commit lowered as: a mana window ask pays its
	// activation's own cost on the tap step that activation is lowered as
	// (its record's step, the same index the record sits at in the group's
	// steps), and everything else — the decision itself, the asks it posed,
	// the payment they completed — pays the decision's cost on the
	// decision's step. An activate ask whose record is not on it (the hook
	// fires on the colour pick that follows it, isManaAbilityAsk) carries
	// the spend to that record's tap step by object. Merging the entries
	// into one aggregate instead put a nested mana activation's own cost (a
	// Ferrous Lake's {1}, CR 107.4b) on the later cast or ability, which
	// replayed a pool split the live game never had (xe:RAND:1:13:w14).
	//
	// allTaps marks a decision that is itself a mana ability activation:
	// every step is a record's tap step and the records sit at their own
	// step indexes; for a cast or ability the last step is the decision, so
	// only the indexes before it are taps.
	//
	// parentPays marks a decision whose own cost is paid only after its asks
	// are answered (a cast or an ability). The recorder flushes that final
	// payment onto the latest entry, so in a mana window it lands on the
	// group's last tap ask, not on the decision: the last mana ask's Spent
	// can then hold its own activation's cost followed by the decision's.
	// The activation's part is the mana units its Cost$ names (T, sacrifice
	// and the other non-mana parts are not mana); the remainder stays with
	// the decision.
	allTaps := en.Kind == "priority" && len(en.Picks) == 1 && en.Picks[0].Kind == "activate"
	tapLimit := len(steps)
	if !allTaps {
		tapLimit--
	}
	parentPays := paysAfterAsks(en)
	var decSpent string
	type deferPay struct {
		obj   state.ObjID
		start int
		spent string
	}
	var deferred []deferPay
	claimed := make([]bool, len(steps))
	for k, e := range g {
		if recAt[k] < 0 || e.Spent == "" {
			continue
		}
		last := recAt[k] + len(e.Mana) - 1
		if pk := manaAsk(e); pk != nil {
			own, rest := e.Spent, ""
			if k == len(g)-1 && parentPays {
				n := manaCostUnits(reg, e.Mana)
				if n > len(own) {
					n = len(own)
				}
				own, rest = own[:n], own[n:]
			}
			switch {
			case len(e.Mana) > 0 && last < tapLimit:
				pay[last] += own
				claimed[last] = true
				decSpent += rest
			case len(e.Mana) > 0:
				decSpent += own + rest
			default:
				deferred = append(deferred, deferPay{pk.Obj, recAt[k], own + rest})
			}
			continue
		}
		decSpent += e.Spent
	}
	// The deferred spends: the first unclaimed tap step of a record of that
	// object at or after the ask's own position, in activation order, so a
	// source activated twice gives each activation its own (an earlier free
	// activation's record cannot be claimed by a later paid one).
	for _, d := range deferred {
		for i := d.start; i < len(mana); i++ {
			mr := mana[i]
			if i >= tapLimit || claimed[i] || mr.Obj != d.obj {
				continue
			}
			pay[i] += d.spent
			claimed[i] = true
			d.spent = ""
			break
		}
		if d.spent != "" {
			// No record of that object was lowered as a tap: keep the spend
			// with the decision's, the aggregate the group lowered before.
			decSpent += d.spent
		}
	}
	if decSpent != "" {
		// The decision's spend belongs to the step that paid it, the last
		// step: the cast or the activation (including a mana ability
		// activation, whose own cost the recorded game already paid). Every
		// earlier step is an explicit mana tap that adds pool mana. The
		// placement comes from the lowered step's role, not the option kind
		// that produced it, so a future paying option kind cannot silently
		// lose its spend.
		if last := len(steps) - 1; isPaidStep(steps[last].Op) {
			pay[last] += decSpent
		}
	}
	return steps, idx, pay, answers, nil
}

// manaAsk is the pick of an entry whose commit is part of a mana activation:
// the "activate <source> for mana" window ask, the priority decision that
// activated a mana ability directly, or the colour pick of a source whose
// record this entry carries. nil when the commit is not one.
func manaAsk(e game.Entry) *game.Pick {
	if len(e.Picks) != 1 {
		return nil
	}
	pk := &e.Picks[0]
	switch pk.Kind {
	case "activate":
		return pk
	case "mana":
		for _, mr := range e.Mana {
			if mr.Obj == pk.Obj {
				return pk
			}
		}
	}
	return nil
}

// manaCostUnit is how many pool units one mana ability activation costs, read
// from its Cost$ (the tap, sacrifice, discard and life parts are not mana). A
// source the registry cannot tell contributes zero: its spend stays with the
// decision's, where the final flush put it.
func manaCostUnit(reg *cards.Registry, mr game.ManaRec) int {
	if reg == nil {
		return 0
	}
	if sa := manaSA(reg, mr); sa != nil {
		return int(rules.ParseCost(sa.Params["Cost"]).CMC())
	}
	return 0
}

// manaCostUnits is how many pool units the mana abilities of an entry cost:
// the sum of their own costs (see manaCostUnit).
func manaCostUnits(reg *cards.Registry, mana []game.ManaRec) int {
	n := 0
	for _, mr := range mana {
		n += manaCostUnit(reg, mr)
	}
	return n
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

func lowerPriority(reg *cards.Registry, m seatMap, en game.Entry, mana []game.ManaRec, asked map[manaAbilityKey]bool) ([]oraclegen.Step, *truncation) {
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
		taps, why := manaTaps(reg, m, mana, asked)
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
		st := oraclegen.Step{Op: "activate", Seat: seat, Card: m.ref(pk.Ref), AbilityIndex: &k}
		// A granted, gained or keyword-granted ability's pick is not the
		// card's printed Face.Abilities[k] (for an SVar grant k aliases
		// printed index 0, for a keyword grant k is -1), so the step also
		// carries the engine's offered label: the runner matches it with
		// oracleLabelMatches beside the index, and the follower exporters
		// resolve the ability by it instead of exporting the printed
		// index's line (Dalakos's {T} mana ability aliasing Dragon
		// Mantle's granted {R} pump). The label is the engine's own text,
		// so a printed pick keeps its index-only step byte-identical.
		if pk.SVar != "" || pk.Keyword != "" || pk.GrantSource != 0 || pk.GainedSource != 0 {
			st.Ability = pk.Label
		}
		return append(taps, st), nil
	case "activate":
		// "Activate <card> for mana": the entry's own mana record is the
		// ability, which the runner finds by its index. A source with several
		// mana abilities asks which one first, and its record sits on that
		// ask (isManaAbilityAsk): the group's one record is then the ability.
		if !priorityManaTap(en, mana) ||
			len(en.Mana) == 0 && colourPicks(reg, mana[0]) != nil && colourAskPosed(reg, mana[0], asked) {
			// The runner would answer the ability choice with the colour
			// (harness: answer "U" not offered for choose).
			return nil, unsupported("activate")
		}
		taps, why := manaTaps(reg, m, mana, asked)
		if why != nil {
			return nil, why
		}
		return taps, nil
	}
	return nil, unsupported(pk.Kind)
}

// priorityManaTap reports a priority "activate <source> for mana" decision
// whose group fired exactly one mana ability, of that source: on the entry
// itself, or on the ask of which of the source's mana abilities it is.
func priorityManaTap(en game.Entry, mana []game.ManaRec) bool {
	return en.Kind == "priority" && len(en.Picks) == 1 && en.Picks[0].Kind == "activate" &&
		len(mana) == 1 && mana[0].Obj == en.Picks[0].Obj
}

// manaTaps is one activate step per mana ability the entry fired, in order. A
// source whose ability asks which colour it makes (Produced$ Any, Combo or a
// ManaReflected set with more than one colour) gets the colour the live game
// made as a "choose" answer, which the runner's mana stage reads instead of
// taking the first colour. asked is the group's colour-asked sources (see
// colourAskedSources): a single-colour reflection asks nothing and stays
// answer-free.
func manaTaps(reg *cards.Registry, m seatMap, mana []game.ManaRec, asked map[manaAbilityKey]bool) ([]oraclegen.Step, *truncation) {
	var out []oraclegen.Step
	for _, mr := range mana {
		if mr.Triggered {
			// No step of its own: a triggered mana ability resolves with the
			// tap that triggered it (CR 605.1b, 605.4a), in the follower too.
			continue
		}
		if mr.Ref == "" {
			return nil, &truncation{ReasonUnbindableRef}
		}
		if mr.Ability < 0 {
			return nil, unsupported("mana_ability")
		}
		k := mr.Ability
		st := oraclegen.Step{Op: "activate", Seat: m.seat(mr.Player), Card: m.ref(mr.Ref), AbilityIndex: &k}
		if picks := colourPicks(reg, mr); picks != nil && colourAskPosed(reg, mr, asked) {
			st.Answers = []oraclegen.Answer{{Kind: "choose", Pick: picks}}
		}
		out = append(out, st)
	}
	return out, nil
}

// manaAbilityKey identifies one mana activation record: the source and the
// index into its face's Abilities. The ability index is part of the key so a
// multi-ability source's wheel-selected plain ability cannot stand in for a
// reflected ability of the same object.
type manaAbilityKey struct {
	obj     state.ObjID
	ability int
}

// colourAskedSources is the mana activations that stopped for a colour
// decision: the recorder closed their record before the colour was added, so
// it carries no Produced. That is the live game's own evidence that it posed
// the ask; a reflection whose reflected set has one colour adds its mana in the
// activation's commit and records Produced, and an ability wheel's "mana" pick
// precedes the record and names the ability, not a produced colour. g must be
// the group before colourProduced fills the record from the following pick.
func colourAskedSources(g []game.Entry) map[manaAbilityKey]bool {
	var out map[manaAbilityKey]bool
	for _, e := range g {
		for _, mr := range e.Mana {
			if mr.Produced != "" {
				continue
			}
			if out == nil {
				out = map[manaAbilityKey]bool{}
			}
			out[manaAbilityKey{mr.Obj, mr.Ability}] = true
		}
	}
	return out
}

// colourAskPosed reports whether the runner will pose this activation's colour
// ask. Produced$ Any and Combo abilities always pose one when colourPicks has
// an answer; a ManaReflected ability poses one only when the live game did
// (its record closed with no Produced), which colourAskedSources reads.
func colourAskPosed(reg *cards.Registry, mr game.ManaRec, asked map[manaAbilityKey]bool) bool {
	sa := manaSA(reg, mr)
	if sa == nil || sa.API != "ManaReflected" {
		return true
	}
	return asked[manaAbilityKey{mr.Obj, mr.Ability}]
}

// colourPicks is the colours to answer a mana ability's colour ask with, nil
// when the ability asks none (a fixed colour) or the live activation's
// colours are unknown. "Any" is one pick; a "Combo" ask takes one per unit;
// a ManaReflected set is one colour (Amount$ repeats it).
func colourPicks(reg *cards.Registry, mr game.ManaRec) []string {
	if mr.Produced == "" {
		return nil
	}
	sa := manaSA(reg, mr)
	if sa == nil {
		return nil
	}
	// A mana ability whose Produced$ sits on a DB$ Mana sub-ability (Empowered
	// Autogenerator: AB$ PutCounter | SubAbility$ DBMana) carries the colour
	// ask there; the head's own Params name no colour.
	if !cards.IsManaAbilityAPI(sa.API) {
		if sub := cards.ManaChainProduction(sa); sub != nil {
			sa = sub
		}
	}
	// A ManaReflected ability has no Produced$: the colour is the one the
	// reflected permanent offered, and the live activation recorded it as the
	// mana it added. One pick answers the one colour decision; Amount$ then
	// adds that colour once per unit.
	if sa.API == "ManaReflected" {
		return []string{mr.Produced[:1]}
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
