package main

import (
	"fmt"
	"strings"
)

// Optional keyword costs paid as a spell is cast. XMage asks each one as a
// string of booleans after the cast (setChoice(true) once per payment, then
// false to stop; Conspire and Buyback one yes/no per instance). Gorge offers
// the payment as its own cast option (Option.Mode), so the translated cast
// step carries cast_mode and the extra mana, and a counted cost's number is
// answered by the cast-flow count ask ("Pay squad 2 times"), not by booleans.

// countedCost is a keyword whose cost is paid any number of times.
type countedCost struct {
	head string // Forge keyword head: K:<head>:<cost>
	mode string // gorge cast option Mode
	noun string // the word in the count ask's labels ("Pay <noun> once")
}

var countedCosts = [...]countedCost{
	{"Multikicker", "multikicked", "multikicker"},
	{"Replicate", "replicated", "replicate"},
	{"Squad", "squadded", "squad"},
}

// label is the count ask's option for n payments (engine/rules cast_asks.go
// replicateAsk, multikickAsk, squadAsk).
func (c countedCost) label(n int) string {
	if n == 1 {
		return "Pay " + c.noun + " once"
	}
	return fmt.Sprintf("Pay %s %d times", c.noun, n)
}

// keywordCost returns the cost parameter of a printed "Head:cost" keyword of
// the named card.
func (em *emitter) keywordCost(name, head string) (string, bool) {
	c, ok := em.t.lookup(name)
	if !ok {
		return "", false
	}
	for _, f := range c.Faces {
		if f == nil || !sameName(f.Name, name) && len(c.Faces) > 1 {
			continue
		}
		for _, k := range f.Keywords {
			if cost, ok := strings.CutPrefix(k, head+":"); ok {
				return cost, true
			}
		}
	}
	return "", false
}

// isBoolChoice reports a plain yes/no setChoice.
func isBoolChoice(ch choice) bool {
	return !ch.target && ch.x < 0 && ch.mode == 0 && ch.kind == "" && (ch.pick == "yes" || ch.pick == "no")
}

// electionPrefix reads the booleans at the front of a cast's setChoice queue
// (XMage keeps addTarget picks in their own queue, so target choices between
// them are skipped): how many are yes, and the indices into cs they span. With
// counted, a no ends the run (it is the "stop" of a counted payment);
// otherwise every leading boolean is one instance's own election.
func electionPrefix(cs []choice, counted bool) (yes int, idx []int) {
	for i, ch := range cs {
		if ch.target {
			continue
		}
		if !isBoolChoice(ch) {
			break
		}
		idx = append(idx, i)
		if ch.pick == "yes" {
			yes++
		} else if counted {
			break
		}
	}
	return yes, idx
}

// consume drops the choices at idx (ascending) from a, keeping the originals
// for the XMage driver cross-check.
func (a *action) consume(idx []int) {
	var rest []choice
	for i, ch := range a.choices {
		if len(idx) > 0 && idx[0] == i {
			a.xlead = append(a.xlead, xans{Seat: ch.seat, Kind: "choice", Value: ch.pick})
			idx = idx[1:]
			continue
		}
		rest = append(rest, ch)
	}
	a.choices = rest
}

// aiElects arms the shape refusal for a cast whose optional keyword cost the
// test states no election for while XMage's AI is left to make it (no
// setStrictChooseMode(true)): the runner plays the plain cast, which says
// nothing about gorge when the AI would have paid.
func (em *emitter) aiElects(a *action, cost string) {
	if !em.t.strict && em.t.electShape == nil {
		em.t.electShape = refuse("optional-cost-ai-election", "%s: XMage's AI elects %s, the test states no choice", a.card, cost)
	}
}

// optionalKeywordCasts maps the cast's leading booleans onto the cast option
// the keyword's payment is in gorge: Multikicker, Replicate and Squad (a count
// ask), Conspire and Buyback (a plain election). A granted keyword is left
// alone except Conspire, which gorge reads from grants like a printed one.
func (em *emitter) optionalKeywordCasts(a *action, s *step) *refusal {
	if s.CastMode != "" || s.Kicked {
		return nil
	}
	for _, cc := range countedCosts {
		cost, ok := em.keywordCost(a.card, cc.head)
		if !ok || em.t.setupMayGrantKeyword(cc.head, a) {
			continue
		}
		yes, used := electionPrefix(a.choices, true)
		if len(used) == 0 {
			em.aiElects(a, cc.head)
			return nil
		}
		per, r := manaFromCostX(cost, 0)
		if r != nil {
			return r
		}
		a.consume(used)
		if yes == 0 {
			return nil
		}
		s.Mana += strings.Repeat(per, yes)
		s.CastMode = cc.mode
		a.choices = append([]choice{{seat: a.seat, x: -1, pick: cc.label(yes), kind: "choose", synth: true}}, a.choices...)
		return nil
	}
	if cost, ok := em.keywordCost(a.card, "Buyback"); ok {
		// one yes/no: only the first boolean is the election, a later yes may
		// answer a "you may" on resolution
		yes, used := electionPrefix(a.choices, true)
		if len(used) > 1 {
			used = used[:1]
			yes = 0
			if a.choices[used[0]].pick == "yes" {
				yes = 1
			}
		}
		if len(used) == 0 {
			em.aiElects(a, "Buyback")
			return nil
		}
		per, r := manaFromCostX(cost, 0)
		if r != nil {
			return r
		}
		a.consume(used)
		if yes > 0 {
			s.Mana += per
			s.CastMode = "buyback"
		}
		return nil
	}
	if !em.conspireMayApply(a) {
		return nil
	}
	yes, used := electionPrefix(a.choices, false)
	if len(used) == 0 {
		em.aiElects(a, "Conspire")
		return nil
	}
	a.consume(used)
	if yes >= 1 {
		s.CastMode = "conspired"
	}
	return nil
}

// conspireMayApply reports whether the cast spell has Conspire: printed, or
// granted by a setup permanent (Wort, the Raidmother; Raiding Schemes) in any
// zone, since a test may cast or copy the granter into play first.
func (em *emitter) conspireMayApply(a *action) bool {
	if _, ok := em.keywordCost(a.card, "Conspire"); ok || em.hasKeywordPrefix(a.card, "Conspire") {
		return true
	}
	return em.t.keywordGrants("Conspire", a, true, true)
}

// armCastShapes arms the shape refusals of a cast the runner cannot play
// faithfully. They convert a failing run only (a passing run of the same
// shape stays covered), so a harness failure of another row is never hidden.
//
//   - kicker-and-or: a card with two kicker costs ("Kicker:2 U:1 G") is
//     elected by XMage one boolean per cost, in the order the card is
//     written, while gorge offers kicked1, kicked2 and kickedboth and the
//     translator reads only one cost.
//   - convoke-tap-target: a spell that has convoke is cast with an explicit
//     target and an addTarget, which XMage answers the convoke tap with; the
//     runner seeds the whole cost into the pool and cannot place that tap.
func (em *emitter) armCastShapes(a *action) {
	if em.t.electShape != nil {
		return
	}
	if em.hasTwoKickerCosts(a.card) {
		if _, idx := electionPrefix(a.choices, false); len(idx) > 0 {
			em.t.electShape = refuse("kicker-and-or", "%s has two kicker costs and XMage elects each by a boolean", a.card)
			return
		}
	}
	if len(a.targets) > 0 && em.mentionsConvoke(a) {
		for _, ch := range a.choices {
			if ch.target {
				em.t.electShape = refuse("convoke-tap-target", "%s: an addTarget taps a creature for convoke", a.card)
				return
			}
		}
	}
}

// hasTwoKickerCosts reports a printed "Kicker:cost:cost" keyword.
func (em *emitter) hasTwoKickerCosts(name string) bool {
	cost, ok := em.keywordCost(name, "Kicker")
	return ok && strings.Contains(cost, ":")
}

// mentionsConvoke reports whether the cast spell has convoke: printed, or a
// setup permanent's text grants it ("The next spell you cast this turn has
// convoke").
func (em *emitter) mentionsConvoke(a *action) bool {
	if _, ok := em.keywordCost(a.card, "Convoke"); ok || em.hasKeywordPrefix(a.card, "Convoke") {
		return true
	}
	for _, ac := range em.t.setup[a.seat] {
		if ac.zone != "battlefield" {
			continue
		}
		if c, ok := em.t.lookup(ac.name); ok {
			for _, f := range c.Faces {
				if f != nil && strings.Contains(strings.ToLower(f.Oracle), "convoke") {
					return true
				}
			}
		}
	}
	return false
}
