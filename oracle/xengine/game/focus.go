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

// Focus is the seat that takes the one action an xproof item exists for.
type Focus struct {
	Seat    int
	Card    string // front-face name of the focus card
	Kind    string // "ability", "cast", "land", "attack" or "" (no forced action)
	Ability int    // for "ability": the index into Face().Abilities (decision.Option.Ability)
}

// focusState is what every seat's focusBot shares with the Setup hook: the
// focus objects, read once from the live engine before the first decision, and
// whether the action has been taken.
type focusState struct {
	f    Focus
	e    *rules.Engine
	objs map[state.ObjID]bool
	done bool
}

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

// open reports whether the game has reached turn 1's first precombat main
// phase: the span a start item replays begins there, so an action taken
// earlier (an upkeep activation) would be missing from it.
func (s *focusState) open() bool {
	return s.e != nil && (s.e.G.Turn > 1 || s.e.G.Turn == 1 && s.e.G.Step >= state.StepMain1)
}

// act replaces the bot's answer with the focus action when d offers it.
func (b focusBot) act(d decision.Decision, in decision.Intent, err error) (decision.Intent, error) {
	st := b.st
	if err != nil || st.done || st.f.Kind == "" || !st.open() {
		return in, err
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
				ok = o.Kind == "cast"
			case "land":
				ok = o.Kind == "play_land"
			}
			if ok {
				st.done = true
				in.Choices = []int{i}
				return in, nil
			}
		}
	case decision.KAttackers:
		if st.f.Kind != "attack" {
			break
		}
		for i, o := range d.Options {
			if !st.objs[o.Obj] {
				continue
			}
			st.done = true
			if !slices.Contains(in.Choices, i) {
				in.Choices = append(in.Choices, i)
				slices.Sort(in.Choices)
			}
			return in, nil
		}
	}
	return in, nil
}
