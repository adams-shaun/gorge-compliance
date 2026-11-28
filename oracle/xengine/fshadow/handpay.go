package fshadow

import (
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// handPayNodes bounds the intents one payByHand search submits to clones.
const handPayNodes = 512

// pickOption finds a Forge priority answer among a priority decision's
// options.
type pickOption func(*decision.Decision) (int, bool)

// payByHand answers a priority cast or activation gorge does not offer yet by
// making Forge's mana first. gorge offers such a play once the floating pool
// or its payment planner covers the cost, and the planner does not witness
// every mana ability (a filter land's "{1}, {T}: Add {U}{R}" is paid from
// mana another source has to float first). The search, on clones, finds an
// order and the choices that activate every source in want, after which pick
// finds the answer; the follower then plays those intents on its own engine
// and returns the answering one. ok false with no divergence leaves the
// engine untouched: gorge cannot make the play from Forge's sources.
func (f *follower) payByHand(want []state.ObjID, pick pickOption) (in decision.Intent, ok bool, dv *Divergence) {
	root := f.e.Pending()
	if root == nil || !slices.ContainsFunc(root.Options, func(o decision.Option) bool {
		return o.Kind == "activate" && slices.Contains(want, o.Obj)
	}) {
		return in, false, nil
	}
	s := &handPay{seat: root.Acting(), want: want, pick: pick, dead: map[string]bool{}}
	script, found := s.search(f.e, nil)
	if !found {
		return in, false, nil
	}
	failed := func(harness, detail string) (decision.Intent, bool, *Divergence) {
		return in, false, &Divergence{What: WhatError, Harness: harness, Detail: "hand payment: " + detail}
	}
	for _, step := range script {
		d := f.e.Pending()
		if err := f.e.SubmitHypothetical(step); err != nil {
			return failed(submitHarness(d, step, err), "submit: "+err.Error())
		}
		f.res.Decisions++
		if err := f.e.AdvanceHypothetical(); err != nil {
			return failed(chanceHarness(err), "advance: "+err.Error())
		}
	}
	// The answer is read off the follower's own decision, not the clone's.
	if d := f.e.Pending(); d != nil && d.Kind == decision.KPriority {
		if i, offered := pick(d); offered {
			f.res.HandPays++
			return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}, true, nil
		}
	}
	return failed(HarnessUnmapped, "the searched activations did not replay")
}

// handPay is one payByHand search.
type handPay struct {
	seat  state.PlayerID
	want  []state.ObjID
	pick  pickOption
	nodes int
	// dead holds the priority states already searched without success, so
	// the orders of independent sources are not each walked again.
	dead map[string]bool
}

// search returns the intents that lead from e's pending decision to a
// priority decision where every wanted source has been activated and pick
// finds the answer. used is the sources activated on the way to e. Branches
// are tried in offered order, so the result is deterministic.
func (s *handPay) search(e *rules.Engine, used []state.ObjID) ([]decision.Intent, bool) {
	d := e.Pending()
	if d == nil || e.G.Over || d.Acting() != s.seat {
		return nil, false
	}
	try := func(in decision.Intent, used []state.ObjID) ([]decision.Intent, bool) {
		if s.nodes >= handPayNodes {
			return nil, false
		}
		s.nodes++
		c := e.CloneHypothetical(0)
		if c.SubmitHypothetical(in) != nil || c.AdvanceHypothetical() != nil {
			return nil, false
		}
		rest, ok := s.search(c, used)
		if !ok {
			return nil, false
		}
		return append([]decision.Intent{in}, rest...), true
	}
	if d.Kind != decision.KPriority {
		// A mana ability's own ask (the colour, which ability, a cost's
		// pick): branch over single picks, else take the minimum.
		if d.Min > 1 {
			return try(firstMin(d), used)
		}
		for i := range d.Options {
			if script, ok := try(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}, used); ok {
				return script, true
			}
		}
		return nil, false
	}
	var left []state.ObjID
	for _, id := range s.want {
		o := e.G.Obj(id)
		tapped := o != nil && o.Zone == state.ZBattlefield && o.Tapped
		if !tapped && !slices.Contains(used, id) {
			left = append(left, id)
		}
	}
	if len(left) == 0 {
		_, ok := s.pick(d)
		return nil, ok
	}
	p := e.G.Players[s.seat]
	key := fmt.Sprint(left, p.Life, p.Pool, p.RestrictedMana)
	if s.dead[key] {
		return nil, false
	}
	for i, o := range d.Options {
		if o.Kind != "activate" || !slices.Contains(left, o.Obj) {
			continue
		}
		next := append(slices.Clone(used), o.Obj)
		if script, ok := try(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}, next); ok {
			return script, true
		}
	}
	s.dead[key] = true
	return nil, false
}
