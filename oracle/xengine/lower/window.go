package lower

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// WindowTemplate is the item template of a lowered window.
const WindowTemplate = "xengine-window"

// SpanTemplate is the item template of a lowered span: a game from a scripted
// start (xproof), lowered from turn 1's first main phase to the end of the tape.
const SpanTemplate = "xproof-start"

// IsRecorded reports whether template names an item lowered from a recorded
// game (a tape, a window or a span): its steps are tape entries, so a follower
// that cannot replay a tape entry as-is rewrites them. Every consumer that
// branches on "is this a recorded game" goes through here, so a new recorded
// template is added to this one list.
func IsRecorded(template string) bool {
	switch template {
	case Template, WindowTemplate, SpanTemplate:
		return true
	}
	return false
}

// Refusal is a turn whose window was not lowered, and why: every reason that
// applies, sorted.
type Refusal struct {
	ID      string   `json:"id"`
	Turn    int32    `json:"turn"`
	Reasons []string `json:"reasons"`
}

// Windows plays pair with game.Play and lowers each turn of the game to a
// one-turn scenario: setup is the live engine at the turn's first precombat
// main priority (cut while the game plays, hence the hook), the steps are the
// tape's entries from there to the end of the turn. A turn whose cut state setup
// cannot place (the expressibility gate, gate.go) is a Refusal instead. The
// tape is returned for the caller's use; a window is self-checked against its
// own rebased tape (Lowered.Against, CheckWindow). xmage says XMage replays the
// windows, so a setup token the XMage driver cannot place is refused; a
// Forge-only run places any token by its script stem. The result is a pure
// function of (pair, seed, opt, xmage). A game of more than two seats is played
// and refused turn by turn, reason seats:<n>, with no cut read.
func Windows(reg *cards.Registry, pair []decks.Deck, seed uint64, opt game.Options, xmage bool) ([]Lowered, []Refusal, *game.Tape, error) {
	var cuts []*cut
	user := opt.OnCut
	opt.OnCut = func(e *rules.Engine, entry int) {
		if r := seatRefusal(len(pair)); r != nil {
			// capture reads two seats; the pod's windows are refused unread.
			cuts = append(cuts, &cut{Entry: entry, Turn: e.G.Turn, Reasons: r})
		} else {
			cuts = append(cuts, capture(e, entry, xmage))
		}
		if user != nil {
			user(e, entry)
		}
	}
	t, err := game.Play(reg, pair, seed, opt)
	if err != nil {
		return nil, nil, t, err
	}
	var out []Lowered
	var refused []Refusal
	for _, c := range cuts {
		id := windowID(t, c.Turn)
		if len(c.Reasons) > 0 {
			refused = append(refused, Refusal{ID: id, Turn: c.Turn, Reasons: c.refusalReasons(reg, t, id, windowEnd(t, c), false)})
			continue
		}
		if c.setupSpeedLatch() {
			refused = append(refused, Refusal{ID: id, Turn: c.Turn, Reasons: []string{reasonSpeedLatch}})
			continue
		}
		l, err := lowerWindow(reg, t, c, id, windowEnd(t, c), false)
		if err != nil {
			return nil, nil, t, err
		}
		if why := setupAnswerReasons(reg, l.Item); len(why) > 0 {
			refused = append(refused, Refusal{ID: id, Turn: c.Turn, Reasons: why})
			continue
		}
		out = append(out, l)
	}
	return out, refused, t, nil
}

// refusalReasons is c.Reasons, except that a window refused for a placed Saga
// alone (reasonSagaEntry, the coarse gate: the runner's placement re-fires the
// chapters whatever they say) reports the setup drive's own unscripted asks
// when it has any, since those name the decision the cut cannot state. The
// window is refused either way; a probe that cannot run keeps the Saga reason.
//
// end and span are the lowering's own (windowEnd and false for a window, the
// tape's length and true for a span), so the probe lowers what would have been.
func (c *cut) refusalReasons(reg *cards.Registry, t *game.Tape, id string, end int, span bool) []string {
	if len(c.Reasons) != 1 || c.Reasons[0] != reasonSagaEntry {
		return c.Reasons
	}
	l, err := lowerWindow(reg, t, c, id, end, span)
	if err != nil {
		return c.Reasons
	}
	if why := setupAnswerReasons(reg, l.Item); len(why) > 0 {
		return why
	}
	return c.Reasons
}

// windowID is xe:<set>:<chunk>:<game>:w<turn>.
func windowID(t *game.Tape, turn int32) string {
	return strings.TrimSuffix(itemID(t), ":tape") + ":w" + strconv.Itoa(int(turn))
}

// windowEnd is the index of the first entry after the cut's turn.
func windowEnd(t *game.Tape, c *cut) int {
	end := c.Entry
	for end < len(t.Entries) && t.Entries[end].Turn == c.Turn {
		end++
	}
	return end
}

// setupSpeedLatch reports why the runner's setup drive consumes the window's
// CR 702.179d speed trigger: the non-active seat's setup life is below 20, so
// it is placed with a life-loss LifeChange, and the active seat already has
// speed 1..3, so the inherent trigger is queued, resolved and latched at
// turn 1. The decision is setup state alone: the setup's own LifeChange is the
// trigger, so there need not be a life change in the tape (and the tape cannot
// show the speed at all). c.Active is the live active seat.
func (c *cut) setupSpeedLatch() bool {
	if c.Setup[c.Active].Speed <= 0 || c.Setup[c.Active].Speed >= 4 {
		return false
	}
	life := c.Setup[1-c.Active].Life
	return life != nil && *life < startLife(c)
}

// windowTape is the tape a window's self-check holds it to: the entries from the
// cut to the first entry of the next turn (end is windowEnd's, or the tape's
// length for a span, which has no next turn) (the digest the last step is checked
// against), with the turn numbers rebased so the cut is turn 1 and every ref
// the way the window's setup binds it. The window's own entries are the ones of
// its turn; the last one is only read for its digest.
func windowTape(t *game.Tape, c *cut, end int) *game.Tape {
	hi := min(end+1, len(t.Entries))
	wt := &game.Tape{Format: t.Format, Set: t.Set, Seed: t.Seed, Decks: t.Decks, Open: make([]game.Opening, len(t.Decks)), Entries: make([]game.Entry, 0, hi-c.Entry)}
	rebase := c.Turn - 1
	for _, en := range t.Entries[c.Entry:hi] {
		en.Turn -= rebase
		en.Digest.Turn -= rebase
		en.Picks = append([]game.Pick(nil), en.Picks...)
		for k := range en.Picks {
			p := &en.Picks[k]
			p.Ref = c.remap(p.Obj, p.Ref)
			p.AttackerRef = c.remap(p.Attacker, p.AttackerRef)
			p.BattleRef = c.remap(p.Battle, p.BattleRef)
		}
		en.Mana = append([]game.ManaRec(nil), en.Mana...)
		for k := range en.Mana {
			en.Mana[k].Ref = c.remap(en.Mana[k].Obj, en.Mana[k].Ref)
		}
		wt.Entries = append(wt.Entries, en)
	}
	wt.Outcome = t.Outcome
	return wt
}

// remap is the runner ref of object id in the window: the ref its setup binds
// (every card alive at the cut), or the live ref of an object made after it when
// that ref does not depend on the objects before the cut (a token, an
// ability). Anything else is "", which the lowering reads as unbindable.
func (c *cut) remap(id state.ObjID, live string) string {
	if id == 0 {
		return live
	}
	if r, ok := c.Refs[id]; ok {
		// A setup token is found by its current face name, not bound: a Clone
		// renames it, so the live ref is the one the runner resolves.
		if strings.Contains(r, ":token:") && strings.Contains(live, ":token:") {
			return live
		}
		return r
	}
	if id <= c.MaxID {
		return ""
	}
	if strings.Contains(live, ":token:") || strings.Contains(live, ":ability:") {
		return live
	}
	return ""
}

// item is the window's scenario, with the seats relabelled by m.
func (c *cut) item(m seatMap, id, name string) oraclegen.Item { return c.itemOf(m, id, name, false) }

// itemOf is item, or with span the item of a span (SpanTemplate).
func (c *cut) itemOf(m seatMap, id, name string, span bool) oraclegen.Item {
	why, tmpl := "One turn of a recorded gorge game between two explore bots, replayed from its first precombat main phase.", WindowTemplate
	if span {
		why, tmpl = "A gorge game from a scripted start, played on by explore bots and a focus seat, replayed from turn 1's first main phase.", SpanTemplate
	}
	sc := oraclegen.Scenario{
		Name:  name,
		Turn:  1,
		CR:    []string{},
		Why:   why,
		Setup: map[string]oraclegen.Seat{},
	}
	for s := range c.Setup {
		seat := c.Setup[s]
		// Card is seat-relative; To is a full ref, so it follows the swap.
		seat.Attached = nil
		for _, a := range c.Setup[s].Attached {
			seat.Attached = append(seat.Attached, oraclegen.SetupAttach{Card: a.Card, To: m.ref(a.To)})
		}
		// CommanderDamage keys are full refs, so they follow the swap too.
		seat.CommanderDamage = nil
		for k, n := range c.Setup[s].CommanderDamage {
			if seat.CommanderDamage == nil {
				seat.CommanderDamage = map[string]int32{}
			}
			seat.CommanderDamage[m.ref(k)] = n
		}
		sc.Setup["p"+strconv.Itoa(m.seat(s))] = seat
	}
	// The runner places seat p0's cards first, so p0's asks come first.
	for k := 0; k < len(c.Answers); k++ {
		sc.SetupAnswers = append(sc.SetupAnswers, c.Answers[m.seat(k)]...)
	}
	it := oraclegen.Item{ID: id, Template: tmpl, Scenario: sc}
	if c.Commander {
		it.Scenario.Format = oraclegen.FormatCommander
		it.Compare = []string{oraclediff.CompareCommand}
	}
	return it
}

// lowerWindow lowers the tape's entries from the cut to end (exclusive). A
// window passes windowEnd; a span (span true, named by name) passes the tape's
// length.
func lowerWindow(reg *cards.Registry, t *game.Tape, c *cut, id string, end int, span bool) (Lowered, error) {
	return lowerFrom(reg, t, c, id, end, span, fmt.Sprintf("xengine window %s vs %s turn %d", deckNameOf(t, 0), deckNameOf(t, 1), c.Turn))
}

func lowerFrom(reg *cards.Registry, t *game.Tape, c *cut, id string, end int, span bool, name string) (Lowered, error) {
	wt := windowTape(t, c, end)
	n := end - c.Entry
	m := seatMap{swap: c.Active == 1, rngOK: true}
	l := Lowered{
		Item: c.itemOf(m, id, name, span),
		Swap: m.swap, StepEntry: []int{}, Against: wt,
	}
	l.Item.Steps = []oraclegen.Step{}
	for _, a := range c.Attach {
		a.Seat = m.seat(a.Seat)
		a.Card, a.AttachedTo = m.ref(a.Card), m.ref(a.AttachedTo)
		l.Item.Steps = append(l.Item.Steps, a)
		l.StepEntry = append(l.StepEntry, -1)
		l.Item.Payment = append(l.Item.Payment, "")
	}
	l.Prelude = len(c.Attach)
	// A window ends at the last entry of its turn; the entry after it only
	// serves as the digest the last step is checked against.
	stalled := end == len(t.Entries) && t.Outcome.Stall != ""
	l.lowerEntries(reg, m, wt.Entries[:n], 0, stalled)
	l.markDieRolls(m, wt.Entries[:n])
	l.markCoinFlips(m, wt.Entries[:n])
	l.compactPayment()
	limit := n
	if l.Truncated != "" {
		limit = min(limit, l.TruncatedAt)
	}
	for k, e := range wt.Entries[:limit] {
		if e.RNG == 0 || rngAccounted(e) {
			continue
		}
		// A window's last entry (and a span's final tape entry) has no after
		// digest, so a draw there cannot be told from a shuffle: cut it. That
		// fails safe (the item never carries the unpinned draw), where HasRNG
		// would have carried it.
		after, afterOK := game.Digest{}, k+1 < len(wt.Entries)
		if afterOK {
			after = wt.Entries[k+1].Digest
		}
		if afterOK && rngShuffle(e, after) {
			if !l.HasRNG {
				l.HasRNG = true
				l.RNGStep = l.stepReaching(k)
				l.Item.Ignore = []string{"library_top"}
			}
			continue
		}
		// A draw the tape cannot account for that is not a library shuffle:
		// the runner's seeded RNG would land somewhere else, and the outcome
		// can be visible on the board (Crystalline Giant's counter kind), not
		// just in the library order HasRNG leaves out. Cut the window before
		// the entry so neither a follower nor the self-check is handed it.
		l.cutEntry(k, &truncation{ReasonRNG})
		return l, nil
	}
	return l, nil
}

// stepReaching is the first step past the prelude whose StepEntry reaches tape
// entry k, or len(StepEntry) when no step does.
func (l *Lowered) stepReaching(k int) int {
	for i := l.Prelude; i < len(l.StepEntry); i++ {
		if l.StepEntry[i] >= k {
			return i
		}
	}
	return len(l.StepEntry)
}

// cutEntry keeps only the steps before the one carrying tape entry k: that
// step and every later step go, and so do the group's mana taps (StepEntry -1)
// that sit immediately before it. The cut is the first tape entry the lowering
// did not replay.
func (l *Lowered) cutEntry(k int, why *truncation) {
	if l.Truncated != "" && l.TruncatedAt <= k {
		return
	}
	at := l.stepReaching(k)
	for at > l.Prelude && l.StepEntry[at-1] == -1 {
		at--
	}
	l.drop(at)
	l.stop(l.firstUnlowered(at), why)
}

// firstUnlowered is the first tape entry no kept step carries out once the
// steps from at onward are dropped: the entry after the last kept step's last
// entry, or the window's first lowered entry when no kept gameplay step
// remains. A step's StepEntry is the last entry of its group (a pass with its
// asks, or a resolve span), so a draw inside a group stops at the group's
// first entry: entries from there on are not lowered, and TruncatedAt must
// name that first entry rather than the draw's own.
func (l *Lowered) firstUnlowered(at int) int {
	for i := at - 1; i >= l.Prelude; i-- {
		if l.StepEntry[i] >= 0 {
			return l.StepEntry[i] + 1
		}
	}
	return 0
}

// setupAnswerReasons is why the window's setup drive cannot place the cut: it
// runs the runner's own setup (the same xmage-fixture drive the self-check
// uses) with no gameplay steps and reports every ask no scenario answer
// scripts, so the runner took its fallback. A fallback that picks nothing (a
// declined target, an optional trigger's empty choice) leaves the state as
// placed, and a trigger order only orders triggers the drive was going to
// resolve anyway; every other pick is a decision the cut cannot state, and the
// lowering would report it at a later gameplay step instead (HOB Down, Down to
// Goblin-town's chapter I discarding the active seat's hand). A probe that
// cannot run is left to the self-check's own harness verdict.
func setupAnswerReasons(reg *cards.Registry, it oraclegen.Item) []string {
	res, err := rules.RunOracleScenarioJSON(reg, setupProbeRaw(it))
	if err != nil {
		return nil
	}
	why := map[string]bool{}
	for _, d := range res.Decisions {
		if d.Via != "setup fallback" || len(d.Picks) == 0 {
			continue
		}
		if d.Kind == "order" {
			// A trigger order: the fallback keeps the offered order, and the
			// drive resolves the same triggers either way.
			continue
		}
		why[reasonSetupAnswer+d.Kind] = true
	}
	return sortedKeys(why)
}

// setupProbeRaw is the runner JSON of the setup-only probe: the item's
// scenario with no gameplay steps. The recorded die rolls are gameplay rolls
// on gameplay steps (markDieRolls never records a setup roll), so the probe
// leaves them out: gorge's runner rejects a roll whose step is outside the
// probe's zero-step range, and the guard would then be silently skipped.
func setupProbeRaw(it oraclegen.Item) []byte {
	probe := it
	probe.Steps = nil
	probe.DieRolls = nil
	return probe.Raw()
}

// CheckWindow is SelfCheck for a window, against its own tape.
func CheckWindow(reg *cards.Registry, l Lowered) Check { return SelfCheck(reg, l, l.Against) }
