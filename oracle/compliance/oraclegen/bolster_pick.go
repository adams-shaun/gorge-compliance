package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// bolsterTiePick reports whether a recorded counter_pick decision is the
// tie-breaking creature election of a Bolster effect (CR 701.39a).
//
// gorge asks that election as a KChoose with ResumeKind "counter_pick"
// (engine/effects/counters_put.go's putCounterBolster), but XMage's
// BolsterEffect builds a TargetPermanent(1, 1, filter, true) and calls
// controller.chooseTarget on a tie. TestPlayer reads chooseTarget from the
// TARGET queue, so a "choice" answer for the pick is never consumed and the
// replay fails with "Missing TARGET def" (Dromoka, the Eternal).
//
// The source is resolved the way digSourceForStep/ownBounceStep resolve it:
// the stack snapshot at the decision's own step is the authority (top object
// first, a trigger by its printed index, an activation by the preceding
// activate step), with the step's own cast/activate card as the fallback. The
// resolved ability's effect chain must carry PutCounter's Bolster$ parameter.
// This is structural, not card- or label-based: every Bolster carrier takes
// this route, while a Support$ pick and a bare Choices$/Chooser$ pick stay on
// the choice queue -- XMage poses those through Player.choose/makeChoose
// (or, for Support, as a target declared when the ability goes on the stack,
// not as a resolution-time pick).
func bolsterTiePick(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, d rules.OracleDecision) bool {
	if reg == nil || d.Step < 0 || pickKind(d, 0) != "counter_pick" {
		return false
	}
	// The stack snapshot at the decision's step is the authority. Its slice is
	// top-first, so walk it in order and stop at the first object whose source
	// card resolves: that is the object resolving while the ask is posed.
	if d.Step < len(snaps) {
		for _, o := range snaps[d.Step].Stack {
			bolsters, resolved := bolsterOnStackObject(reg, sc, d.Step, o)
			if resolved {
				return bolsters
			}
		}
	}
	// A cast/activate step names its own source when the snapshot does not
	// resolve it (an empty or stale snapshot); the card-level scan covers a
	// trigger whose printed index does not resolve.
	if d.Step < len(sc.Steps) {
		st := sc.Steps[d.Step]
		if st.Card != "" {
			if st.AbilityIndex != nil && bolsterAtAbility(reg, st.Card, *st.AbilityIndex) {
				return true
			}
			return bolsterOnCard(reg, st.Card)
		}
	}
	return false
}

// bolsterOnStackObject reports whether one stack entry's source ability is a
// Bolster effect, and whether its source card resolved at all.
func bolsterOnStackObject(reg *cards.Registry, sc Scenario, step int, o rules.OracleSnapStack) (bolsters, resolved bool) {
	if reg == nil || o.Source == "" {
		return false, false
	}
	if _, ok := reg.Lookup(oraclediffRefName(o.Source)); !ok {
		return false, false
	}
	switch o.Kind {
	case "ability":
		if o.Trigger != "" {
			idx, err := strconv.Atoi(o.Trigger)
			if err != nil {
				return bolsterOnCard(reg, o.Source), true
			}
			return bolsterAtTrigger(reg, o.Source, idx), true
		}
		if sa := bolsterActivatedSA(reg, sc, step, o.Source); sa != nil {
			return bolsterInChain(sa), true
		}
		return bolsterOnCard(reg, o.Source), true
	case "spell":
		return bolsterSpellOnCard(reg, o.Source), true
	}
	return false, false
}

// bolsterActivatedSA resolves the activated ability a stack object names
// through the most recent activate step for that source, the same activation
// identity digSourceForStep uses.
func bolsterActivatedSA(reg *cards.Registry, sc Scenario, step int, source string) *cards.SA {
	for i := step - 1; i >= 0 && i < len(sc.Steps); i-- {
		st := sc.Steps[i]
		if st.Op != "activate" || st.Card != source || st.AbilityIndex == nil {
			continue
		}
		c, ok := reg.Lookup(oraclediffRefName(source))
		if !ok {
			return nil
		}
		idx := *st.AbilityIndex
		for _, f := range digFaces(c, source) {
			if f != nil && idx >= 0 && idx < len(f.Abilities) {
				return f.Abilities[idx]
			}
		}
		return nil
	}
	return nil
}

func bolsterAtTrigger(reg *cards.Registry, ref string, idx int) bool {
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, ref) {
		if f != nil && idx >= 0 && idx < len(f.Triggers) && bolsterInChain(f.Triggers[idx].Effect) {
			return true
		}
	}
	return false
}

func bolsterAtAbility(reg *cards.Registry, ref string, idx int) bool {
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, ref) {
		if f != nil && idx >= 0 && idx < len(f.Abilities) && bolsterInChain(f.Abilities[idx]) {
			return true
		}
	}
	return false
}

// bolsterOnCard is the card-level fallback: every printed ability and trigger
// on the face is checked, because a stack entry without an exact ability or
// trigger index names only the card.
func bolsterOnCard(reg *cards.Registry, ref string) bool {
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, ref) {
		if f == nil {
			continue
		}
		for _, sa := range f.Abilities {
			if bolsterInChain(sa) {
				return true
			}
		}
		for _, tr := range f.Triggers {
			if bolsterInChain(tr.Effect) {
				return true
			}
		}
	}
	return false
}

// bolsterSpellOnCard checks only a face's spell abilities: a spell on the
// stack is resolving its own effect, never one of the permanent's triggers.
func bolsterSpellOnCard(reg *cards.Registry, ref string) bool {
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, ref) {
		if f == nil {
			continue
		}
		for _, sa := range f.Abilities {
			if bolsterInChain(sa) {
				return true
			}
		}
	}
	return false
}

// bolsterInChain walks an ability's SubAbility chain for PutCounter's
// Bolster$ parameter (the one primitive putCounterBolster serves).
func bolsterInChain(sa *cards.SA) bool {
	seen := map[*cards.SA]bool{}
	for ; sa != nil && !seen[sa]; sa = sa.Sub {
		seen[sa] = true
		if sa.API == "PutCounter" && strings.TrimSpace(sa.Params["Bolster"]) != "" {
			return true
		}
	}
	return false
}
