package lower

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// WindowTemplate is the item template of a lowered window.
const WindowTemplate = "xengine-window"

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
// own rebased tape (Lowered.Against, CheckWindow). The result is a pure function of (pair, seed, opt).
func Windows(reg *cards.Registry, pair [2]decks.Deck, seed uint64, opt game.Options) ([]Lowered, []Refusal, *game.Tape, error) {
	var cuts []*cut
	user := opt.OnCut
	opt.OnCut = func(e *rules.Engine, entry int) {
		cuts = append(cuts, capture(e, entry))
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
			refused = append(refused, Refusal{ID: id, Turn: c.Turn, Reasons: c.Reasons})
			continue
		}
		l, err := lowerWindow(reg, t, c, id)
		if err != nil {
			return nil, nil, t, err
		}
		out = append(out, l)
	}
	return out, refused, t, nil
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

// windowTape is the tape a window's self-check holds it to: the entries from the
// cut to the first entry of the next turn (the digest the last step is checked
// against), with the turn numbers rebased so the cut is turn 1 and every ref
// the way the window's setup binds it. The window's own entries are the ones of
// its turn; the last one is only read for its digest.
func windowTape(t *game.Tape, c *cut) *game.Tape {
	end := windowEnd(t, c)
	hi := min(end+1, len(t.Entries))
	wt := &game.Tape{Format: t.Format, Set: t.Set, Seed: t.Seed, Decks: t.Decks, Entries: make([]game.Entry, 0, hi-c.Entry)}
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
func (c *cut) item(m seatMap, id, name string) oraclegen.Item {
	sc := oraclegen.Scenario{
		Name:  name,
		Turn:  1,
		CR:    []string{},
		Why:   "One turn of a recorded gorge game between two explore bots, replayed from its first precombat main phase.",
		Setup: map[string]oraclegen.Seat{},
	}
	for s := range c.Setup {
		sc.Setup["p"+strconv.Itoa(m.seat(s))] = c.Setup[s]
	}
	// The runner places seat p0's cards first, so p0's asks come first.
	for k := 0; k < len(c.Answers); k++ {
		sc.SetupAnswers = append(sc.SetupAnswers, c.Answers[m.seat(k)]...)
	}
	return oraclegen.Item{ID: id, Template: WindowTemplate, Scenario: sc}
}

func lowerWindow(reg *cards.Registry, t *game.Tape, c *cut, id string) (Lowered, error) {
	wt := windowTape(t, c)
	n := windowEnd(t, c) - c.Entry
	m := seatMap{swap: c.Active == 1, rngOK: true}
	name := fmt.Sprintf("xengine window %s vs %s turn %d", t.Decks[0].Name, t.Decks[1].Name, c.Turn)
	l := Lowered{
		Item: c.item(m, id, name),
		Swap: m.swap, StepEntry: []int{}, Against: wt,
	}
	l.Item.Steps = []oraclegen.Step{}
	for _, a := range c.Attach {
		a.Seat = m.seat(a.Seat)
		a.Card, a.AttachedTo = m.ref(a.Card), m.ref(a.AttachedTo)
		l.Item.Steps = append(l.Item.Steps, a)
		l.StepEntry = append(l.StepEntry, -1)
	}
	l.Prelude = len(c.Attach)
	// A window ends at the last entry of its turn; the entry after it only
	// serves as the digest the last step is checked against.
	stalled := windowEnd(t, c) == len(t.Entries) && t.Outcome.Stall != ""
	l.lowerEntries(reg, m, wt.Entries[:n], 0, stalled)
	for k, e := range wt.Entries[:n] {
		if e.RNG > 0 && len(e.Flips) == 0 {
			l.HasRNG = true
			l.RNGStep = len(l.StepEntry)
			for i, se := range l.StepEntry {
				if i >= l.Prelude && se >= k {
					l.RNGStep = i
					break
				}
			}
			l.Item.Ignore = []string{"library_top"}
			break
		}
	}
	return l, nil
}

// CheckWindow is SelfCheck for a window, against its own tape.
func CheckWindow(reg *cards.Registry, l Lowered) Check { return SelfCheck(reg, l, l.Against) }
