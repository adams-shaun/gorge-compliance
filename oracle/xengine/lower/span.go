package lower

import (
	"slices"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// reasonNoTurn1Cut is why a span has nothing to lower: the game never reached
// the first priority of turn 1's precombat main phase.
const reasonNoTurn1Cut = "span:no-turn1-cut"

// Span plays pair with game.Play like Windows, but lowers the whole game
// from turn 1's first precombat main priority (the first cut) to the end of the
// tape, as one scenario of template SpanTemplate: the game of an xproof start
// item, which a follower replays unchanged. Only turn 1's cut is read. A game
// with no such cut, or a cut the lowering refuses (the expressibility gate, the
// speed latch, a setup answer no scenario scripts), returns its reasons, and
// the zero Lowered. The tape is returned for the caller's use; the span is
// self-checked against its own rebased tape (CheckWindow). The result is a pure
// function of (pair, seed, opt, xmage, id, name). Span is two-seat only: a
// larger pod's reasons are seats:<n>.
func Span(reg *cards.Registry, pair []decks.Deck, seed uint64, opt game.Options, xmage bool, id, name string) (Lowered, []string, *game.Tape, error) {
	var first *cut
	user := opt.OnCut
	opt.OnCut = func(e *rules.Engine, entry int) {
		if first == nil {
			if r := seatRefusal(len(pair)); r != nil {
				first = &cut{Entry: entry, Turn: e.G.Turn, Reasons: r}
			} else {
				first = captureStart(e, entry, xmage)
			}
		}
		if user != nil {
			user(e, entry)
		}
	}
	t, err := game.Play(reg, pair, seed, opt)
	if err != nil {
		return Lowered{}, nil, t, err
	}
	c := first
	if c == nil || c.Turn != 1 {
		return Lowered{}, []string{reasonNoTurn1Cut}, t, nil
	}
	if len(c.Reasons) > 0 {
		return Lowered{}, c.refusalReasons(reg, t, id, len(t.Entries), true), t, nil
	}
	if c.setupSpeedLatch() {
		return Lowered{}, []string{reasonSpeedLatch}, t, nil
	}
	l, err := lowerFrom(reg, t, c, id, len(t.Entries), true, name)
	if err != nil {
		return Lowered{}, nil, t, err
	}
	if why := setupAnswerReasons(reg, l.Item); len(why) > 0 {
		// The drive walks turn 1's upkeep, so a placed permanent's optional
		// upkeep trigger asks again after the cut already carries its result.
		// Decline it (the trigger's state is in the setup); only a probe that
		// then has no fallback left accepts the span.
		if slices.Contains(why, reasonSetupAnswer+"yesno") {
			if ans := startPhaseTriggerDeclines(reg, l.Item); len(ans) > 0 {
				l.Item.Scenario.SetupAnswers = append(l.Item.Scenario.SetupAnswers, ans...)
				why = setupAnswerReasons(reg, l.Item)
			}
		}
		if len(why) > 0 {
			return Lowered{}, why, t, nil
		}
	}
	return l, nil, t, nil
}
