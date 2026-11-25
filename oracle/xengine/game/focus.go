package game

import (
	"context"
	"errors"
	"slices"

	"github.com/adams-shaun/gorge/bots/botpolicy"
	"github.com/adams-shaun/gorge/bots/heuristic"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/engine/view"
)

// Focus is the seat that takes the one action an xproof item exists for. The
// seat acts at its first chance from turn 1's first precombat main priority on
// (an earlier action, an upkeep activation, would be missing from the span a
// start item replays, so a key usable only before main 1 cannot be focused).
type Focus struct {
	Seat    int
	Card    string // front-face name of the focus card
	Kind    string // "ability", "cast", "land", "attack", "block" or "" (no forced action)
	Ability int    // for "ability": the index into Face().Abilities (decision.Option.Ability)
	// Mode, for "cast", is the cast option's payment kind (decision.Option.Mode,
	// e.g. "kicked"); "" takes any cast option of the card.
	Mode string
	// Times is how many times the seat takes the action (0: once), each at its
	// first chance after the last.
	Times int
}

// focusState is what every seat's focusBot shares with the Setup hook: the
// focus objects, read once from the live engine before the first decision, and
// whether the action has been taken.
type focusState struct {
	f    Focus
	e    *rules.Engine
	objs map[state.ObjID]bool
	done bool
	n    int // actions taken
}

// took records one focus action, done once the seat has taken Times of them.
func (s *focusState) took() {
	s.n++
	if s.n >= max(s.f.Times, 1) {
		s.done = true
	}
}

// focusSeen, when set (by a test), is handed each Play's focusState as it is
// made, so the test can read what the Setup hook found and whether the focus
// acted. It is nil outside tests.
var focusSeen func(*focusState)

// read fills the focus objects: every object of the focus seat, in any zone,
// whose front face is the focus card.
func (s *focusState) read(e *rules.Engine) {
	s.e = e
	s.objs = map[state.ObjID]bool{}
	if s.f.Seat < 0 || s.f.Seat >= len(e.G.Players) {
		return
	}
	p := state.PlayerID(s.f.Seat)
	for _, z := range []state.Zone{state.ZHand, state.ZBattlefield, state.ZLibrary, state.ZGraveyard, state.ZExile} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == s.f.Card {
				s.objs[id] = true
			}
		}
	}
}

// Kind "block": the focus seat is the defender. Its first block declaration
// blocks with the focus card (the first option naming it), and every other
// seat's attackers decision is answered with an attack by at least one creature
// (the bot's own pick, or the first option), so the block is offered and taken
// whatever the bots' policies.

// focusBot is a seat that takes the focus action once. It lets the bot it wraps
// decide first (its random stream is spent exactly as before, as bandBot does),
// replaces that answer when the focus action is on offer, and from then on
// defers. The bot is a field, not embedded: an embedded bot's DecideBoard
// would answer in Decide's place.
type focusBot struct {
	bot seatBot
	st  *focusState
}

var _ heuristic.BoardSeat = focusBot{}

func (b focusBot) WantsPaymentActions() bool { return b.bot.WantsPaymentActions() }

func (b focusBot) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	in, err := b.bot.Decide(ctx, v, d)
	return b.act(d, in, err)
}

func (b focusBot) DecideBoard(ctx context.Context, brd botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	hb, ok := b.bot.(heuristic.BoardSeat)
	if !ok {
		return decision.Intent{}, errors.New("game: focus seat's bot has no board path")
	}
	in, err := hb.DecideBoard(ctx, brd, d)
	return b.act(d, in, err)
}

// holdPass is the focus seat's answer to a priority decision before the focus
// opens: pass, whatever the bot chose. Any other seat or decision keeps the
// bot's answer.
func (s *focusState) holdPass(d decision.Decision, in decision.Intent) decision.Intent {
	if d.Kind != decision.KPriority || int(d.Player) != s.f.Seat {
		return in
	}
	for i, o := range d.Options {
		if o.Kind == "pass" {
			in.Choices = []int{i}
			return in
		}
	}
	return in
}

// manaFor is, for a cast focus, the focus seat's first activation of a land,
// or -1. A seat whose bot pays through plans is offered a cast option only once
// the pool covers it (an instant or a spell the bot has no reason to cast leaves
// its lands untapped, and the cast is never offered; a kicked cast is never a
// planned payment either, as the planner plans the card's own cost), so the
// focus taps its lands (a forced start's mana sources: basics) one by one until
// the option, the mode's when it has one, is offered.
func (s *focusState) manaFor(d decision.Decision) int {
	if s.f.Kind != "cast" || int(d.Player) != s.f.Seat {
		return -1
	}
	for i, o := range d.Options {
		if o.Kind != "activate" || s.e == nil {
			continue
		}
		if ob := s.e.G.Obj(o.Obj); ob != nil && ob.Face() != nil && ob.Face().IsLand() {
			return i
		}
	}
	return -1
}

// open reports whether the game has reached turn 1's first precombat main
// phase: the span a start item replays begins there, so an action taken
// earlier (an upkeep activation) would be missing from it.
func (s *focusState) open() bool {
	return s.e != nil && (s.e.G.Turn > 1 || s.e.G.Turn == 1 && s.e.G.Step >= state.StepMain1)
}

// act replaces the bot's answer with the focus action when d offers it.
// Before turn 1's first precombat main phase the focus seat passes priority
// instead (holdPass): a land it tapped for mana in the upkeep would empty
// before main 1 and leave the forced line unaffordable.
func (b focusBot) act(d decision.Decision, in decision.Intent, err error) (decision.Intent, error) {
	st := b.st
	if err != nil || st.done || st.f.Kind == "" {
		return in, err
	}
	if st.f.Kind == "block" {
		return st.block(d, in), nil
	}
	if !st.open() {
		return st.holdPass(d, in), nil
	}
	switch d.Kind {
	case decision.KPriority:
		for i, o := range d.Options {
			if !st.objs[o.Obj] {
				continue
			}
			ok := false
			switch st.f.Kind {
			case "ability":
				ok = o.Kind == "ability" && o.Ability == st.f.Ability
			case "cast":
				ok = o.Kind == "cast" && (st.f.Mode == "" || o.Mode == st.f.Mode)
			case "land":
				ok = o.Kind == "play_land"
			}
			if ok {
				st.took()
				in.Choices = []int{i}
				in.Payment = nil
				return in, nil
			}
		}
		if i := st.manaFor(d); i >= 0 {
			in.Choices, in.Payment = []int{i}, nil
			return in, nil
		}
	case decision.KAttackers:
		if st.f.Kind != "attack" {
			break
		}
		for i, o := range d.Options {
			if !st.objs[o.Obj] {
				continue
			}
			st.took()
			if !slices.Contains(in.Choices, i) {
				in.Choices = append(in.Choices, i)
				slices.Sort(in.Choices)
			}
			return in, nil
		}
	}
	return in, nil
}

// block is act for a "block" focus: the defending focus seat blocks with the
// focus card; any other seat attacks (the focus seat's opponent, whose bot
// might otherwise decline to).
func (s *focusState) block(d decision.Decision, in decision.Intent) decision.Intent {
	if !s.open() {
		return s.holdPass(d, in)
	}
	switch d.Kind {
	case decision.KAttackers:
		if int(d.Player) == s.f.Seat || len(in.Choices) > 0 || len(d.Options) == 0 {
			return in
		}
		in.Choices = []int{0}
	case decision.KBlockers:
		if int(d.Player) != s.f.Seat {
			return in
		}
		for i, o := range d.Options {
			if s.objs[o.Obj] {
				s.took()
				in.Choices = []int{i}
				return in
			}
		}
	}
	return in
}
