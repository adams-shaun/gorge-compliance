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

// Which option is Forge's is settled before the search. gorge lists an
// Adventure, a split half, foretell, evoke and every other way to cast a card
// as "cast" options of the same object, several before the plain one, and
// nothing in Forge's line names the gorge option its seat took. The search
// tries colour choices until its offer appears, so an offer that accepted any
// of them would steer into the cheaper one. A cast is therefore paid by hand
// only when neither engine had a second way to cast the card (soleCast), an
// activation only when both list as many abilities (forgeAbility).

// maxPaySubmits bounds one search, in decisions submitted to clones.
const maxPaySubmits = 4096

// maxSettle bounds the asks one trial of the offer answers.
const maxSettle = 32

// floodMana is more mana of one kind than any cost asks.
const floodMana = 99

// unconstrained is the priority options gorge would offer e's pending player if
// mana were no object: every way it has now to cast a card or activate an
// ability. Asking gorge keeps the follower from listing card faces or keywords.
// e is not changed.
func unconstrained(e *rules.Engine) []decision.Option {
	c := e.Clone()
	p := &c.G.Players[c.Pending().Player]
	for i := range p.Pool {
		p.Pool[i], p.Snow[i] = floodMana, floodMana
	}
	return c.PriorityOptionsWithPaidManaCensus(true)
}

// plainCast is the index in opts of obj's plain cast (no mode, no alternative
// cost) when it is obj's only cast option, else -1.
func plainCast(opts []decision.Option, obj state.ObjID) int {
	found := -1
	for i, o := range opts {
		if o.Kind != "cast" || o.Obj != obj {
			continue
		}
		if found >= 0 || o.Mode != "" || o.AltCostIndex != 0 {
			return -1
		}
		found = i
	}
	return found
}

// soleCast reports whether Forge's cast a of obj can only be gorge's plain
// cast: Forge marks no mode on it and listed no second cast of the card, and
// gorge has no second way to cast it either, whatever the mana.
func soleCast(e *rules.Engine, a *Ask, obj state.ObjID) bool {
	listed := 0
	for _, o := range a.Options {
		if o.Action == "cast" && o.Card == a.Answer.Card {
			listed++
		}
	}
	return a.Answer.Mode == "" && listed <= 1 && plainCast(unconstrained(e), obj) >= 0
}

// forgeAbility is the gorge ability option of obj that Forge's activate answer a
// names: the one of the same rank among all gorge could offer as Forge's
// ability id has among the activations Forge listed for the card. False when
// Forge's answer is not on its list or the two lists differ in length.
func forgeAbility(e *rules.Engine, a *Ask, obj state.ObjID) (decision.Option, bool) {
	ids := forgeAbilityIDs(a)
	r := slices.Index(ids, a.Answer.Ability)
	if r < 0 {
		return decision.Option{}, false
	}
	opts := unconstrained(e)
	cands := abilityOptions(opts, obj)
	if len(ids) != len(cands) {
		return decision.Option{}, false
	}
	return opts[cands[r]], true
}

// abilityOptions is the indices of obj's "ability" options in opts.
func abilityOptions(opts []decision.Option, obj state.ObjID) []int {
	var out []int
	for i, o := range opts {
		if o.Kind == "ability" && o.Obj == obj {
			out = append(out, i)
		}
	}
	return out
}

// forgeAbilityIDs is the ability ids Forge listed as activations of the card it
// activated, in its order.
func forgeAbilityIDs(a *Ask) []int {
	var ids []int
	for _, o := range a.Options {
		if o.Action == "activate" && o.Card == a.Answer.Card {
			ids = append(ids, o.Ability)
		}
	}
	return ids
}

// optionOf is the index of d's option that is the same action as want, or -1.
func optionOf(d *decision.Decision, want decision.Option) int {
	return slices.IndexFunc(d.Options, func(o decision.Option) bool {
		return o.Kind == want.Kind && o.Obj == want.Obj && o.Ability == want.Ability &&
			o.Mode == want.Mode && o.AltCostIndex == want.AltCostIndex && o.Label == want.Label
	})
}

// paySearch is one depth-first search for the activations behind an offer.
type paySearch struct {
	f       *follower
	ask     *Ask // the line being paid
	player  state.PlayerID
	sources []state.ObjID // distinct, in Forge's tap order
	stack   int           // stack depth before the first activation
	// offer is the index of the wanted priority option, or -1.
	offer func(*decision.Decision) int
	seen  map[string]bool // priority states already searched
	left  int             // submits left
}

// payByHand answers priority decision d with the first mana activation of a
// payment by hand for ask a: every source in tapped activated, then the option
// offer picks. The rest of the choices are queued on f.pay for the decisions
// that follow. False, with nothing changed, when no activation order and colour
// choice reaches an offer the floating mana pays.
func (f *follower) payByHand(d *decision.Decision, a *Ask, tapped []state.ObjID, offer func(*decision.Decision) int) (decision.Intent, bool) {
	s := &paySearch{f: f, ask: a, player: d.Player, offer: offer, seen: map[string]bool{}, left: maxPaySubmits}
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
			if i := s.offer(d); i >= 0 && s.settles(e, i) {
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

// try submits choice i of d to a clone of e and searches on from there.
func (s *paySearch) try(e *rules.Engine, d *decision.Decision, i int, used uint64) []int {
	c := s.submit(e, decision.Intent{Choices: []int{i}})
	if c == nil {
		return nil
	}
	rest := s.walk(c, used)
	if rest == nil {
		return nil
	}
	return append([]int{i}, rest...)
}

// submit answers e's pending decision with in on a clone of e (a clone carries
// no chance planner, so the followed game's shuffle bookkeeping is untouched)
// and returns the advanced clone; nil when gorge refuses or the search's
// budget is spent.
func (s *paySearch) submit(e *rules.Engine, in decision.Intent) *rules.Engine {
	if s.left <= 0 {
		return nil
	}
	s.left--
	d := e.Pending()
	in.Seq, in.Player = d.Seq, d.Player
	c := e.Clone()
	if c.SubmitHypothetical(in) != nil || c.AdvanceHypothetical() != nil {
		return nil
	}
	return c
}

// settles reports whether taking option i of e's priority decision is paid by
// the floating mana alone: before the next priority gorge asks nothing but
// modes, targets and what the cast's triggers ask. Any other ask (X, a delve or
// convoke pick, the mana window, a sacrifice) the follow would answer by
// default, which may tap or spend what Forge did not.
func (s *paySearch) settles(e *rules.Engine, i int) bool {
	// The trial answers targets as the follow will, from this line's targets
	// remembered on a copy of the follower: the follow's own memory stays.
	trial := *s.f
	trial.castTargets = nil
	trial.rememberCastTargets(s.ask)
	in := decision.Intent{Choices: []int{i}}
	for range maxSettle {
		if e = s.submit(e, in); e == nil {
			return false
		}
		d := e.Pending()
		if d == nil || e.G.Over {
			return false
		}
		switch d.Kind {
		case decision.KPriority:
			return true
		case decision.KModes:
			in = modeIntent(d)
		case decision.KTarget:
			answered, ok, dv := trial.answerCastTargets(d)
			if dv != nil {
				return false // the follow would stop here
			}
			if !ok {
				answered = trial.defaultTargets(d)
			}
			in = answered
		case decision.KTriggerOrder, decision.KTriggerOptional:
			in = firstMin(d)
		default:
			return false
		}
	}
	return false
}
