package fshadow

import (
	"fmt"
	"math/bits"
	"slices"

	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// Paying by hand. gorge offers a cast or an activation at priority when it can
// plan the payment or the mana pool already covers it, and its planner plans no
// mana ability whose own cost includes mana (a filter land, a Signet). Forge
// activates those while it pays (CR 601.2g, 605.3a) and logs only the sources
// it tapped. gorge's player reaches the same state by activating those sources
// at priority (CR 605.3a) and then taking the offer the pool now pays, so the
// follower does that: it searches engine clones for the activations, then
// submits them on the followed engine.
//
// One difference remains between the two orders: an ability a tap triggers (City
// of Brass) goes on the stack above the spell when the tap pays for it (CR
// 603.3), below it when the tap came first. A path that grows the stack before
// the offer is therefore rejected, and the follow diverges instead.

// maxPaySubmits bounds one search, in decisions submitted to clones.
const maxPaySubmits = 4096

// paySearch is one depth-first search for the activations behind an offer.
type paySearch struct {
	player  state.PlayerID
	sources []state.ObjID // distinct, in Forge's tap order
	stack   int           // stack depth before the first activation
	// offer is the index of the wanted priority option, or -1.
	offer func(*decision.Decision) int
	seen  map[string]bool // priority states already searched
	left  int             // submits left
}

// payByHand answers priority decision d with the first mana activation of a
// payment by hand: every source in tapped activated, then the option offer
// picks. The rest of the choices are queued on f.pay for the decisions that
// follow. False, with nothing changed, when no activation order and colour
// choice reaches the offer.
func (f *follower) payByHand(d *decision.Decision, tapped []state.ObjID, offer func(*decision.Decision) int) (decision.Intent, bool) {
	s := &paySearch{player: d.Player, offer: offer, seen: map[string]bool{}, left: maxPaySubmits}
	for _, id := range tapped {
		if !slices.Contains(s.sources, id) {
			s.sources = append(s.sources, id)
		}
	}
	if len(s.sources) == 0 || len(s.sources) > 64 {
		return decision.Intent{}, false
	}
	s.stack = len(f.e.G.Stack)
	choices := s.walk(f.e, 0)
	if choices == nil {
		return decision.Intent{}, false
	}
	f.res.HandPayments++
	f.pay = choices[1:]
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices[:1]}, true
}

// payStep answers d with the next queued choice of a payment by hand. The last
// one takes the offer itself, which consumes the priority line e.
func (f *follower) payStep(d *decision.Decision, e Entry) decision.Intent {
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{f.pay[0]}}
	f.pay = f.pay[1:]
	if len(f.pay) == 0 {
		f.rememberCastTargets(e.Ask)
		f.pos++
	}
	return in
}

// walk returns the choices, one per decision from e's pending one, that
// activate the sources not yet in used and then take the offer; nil if none
// do. e is not changed.
func (s *paySearch) walk(e *rules.Engine, used uint64) []int {
	d := e.Pending()
	if d == nil || e.G.Over || d.Player != s.player {
		return nil
	}
	switch {
	case d.Kind == decision.KPriority:
		if len(e.G.Stack) != s.stack {
			return nil // an activation triggered something
		}
		if bits.OnesCount64(used) == len(s.sources) {
			if i := s.offer(d); i >= 0 {
				return []int{i}
			}
			return nil
		}
		p := e.G.Players[s.player]
		key := fmt.Sprint(used, p.Pool, p.Snow, p.TypedMana, p.ArtifactTyped, p.SnowTyped, p.PersistentMana, p.CombatMana, p.RestrictedMana)
		if s.seen[key] {
			return nil
		}
		s.seen[key] = true
		for k, src := range s.sources {
			if used&(1<<k) != 0 {
				continue
			}
			for i, o := range d.Options {
				if o.Kind != "activate" || o.Obj != src {
					continue
				}
				if rest := s.try(e, d, i, used|1<<k); rest != nil {
					return rest
				}
			}
		}
	case d.Kind == decision.KChoose && d.Min == 1 && d.Max == 1:
		// An activation's own ask: which ability, which colour.
		for i := range d.Options {
			if rest := s.try(e, d, i, used); rest != nil {
				return rest
			}
		}
	}
	return nil
}

// try submits choice i of d to a clone of e (a clone carries no chance
// planner, so the followed game's shuffle bookkeeping is untouched) and
// searches on from there.
func (s *paySearch) try(e *rules.Engine, d *decision.Decision, i int, used uint64) []int {
	if s.left <= 0 {
		return nil
	}
	s.left--
	c := e.Clone()
	if c.SubmitHypothetical(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}) != nil || c.AdvanceHypothetical() != nil {
		return nil
	}
	rest := s.walk(c, used)
	if rest == nil {
		return nil
	}
	return append([]int{i}, rest...)
}
