package oraclegen

import (
	"strconv"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// counterPickSource resolves the source of a counter_pick decision and reports
// whether its printed ability matches. It is the one resolver the counter_pick
// routes that XMage asks on another queue share: the Bolster tie election
// (bolster_pick.go) and the Support pick (support_pick.go).
//
// The stack snapshot at the decision's own step is the authority: top object
// first, a trigger by its printed index, an activation by the preceding
// activate step, each source read as its displayed card (a copy keeps the
// original's bound ref on the stack). The step's own cast/activate card is the
// fallback when the snapshot does not resolve it; onStack is false for that
// fallback, so a caller needing the announcement step does not read a
// synthesized object as a real stack entry.
func counterPickSource(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, d rules.OracleDecision, match func(*cards.SA) bool) (obj rules.OracleSnapStack, matched, onStack bool) {
	if reg == nil || match == nil || d.Step < 0 {
		return rules.OracleSnapStack{}, false, false
	}
	// The stack snapshot at the decision's step is the authority. Its slice is
	// top-first, so walk it in order and stop at the first object whose source
	// card resolves: that is the object resolving while the ask is posed.
	if d.Step < len(snaps) {
		for _, o := range snaps[d.Step].Stack {
			if o.Source == "" {
				continue
			}
			src := counterPickSourceRef(reg, snaps[d.Step], o.Source)
			if _, ok := reg.Lookup(oraclediffRefName(src)); !ok {
				continue
			}
			switch o.Kind {
			case "ability":
				if o.Trigger != "" {
					idx, err := strconv.Atoi(o.Trigger)
					if err != nil {
						return o, counterPickOnCard(reg, src, match), true
					}
					return o, counterPickAtTrigger(reg, src, idx, match), true
				}
				if sa := counterPickActivatedSA(reg, sc, d.Step, o.Source, src); sa != nil {
					return o, counterPickInChain(sa, match), true
				}
				return o, counterPickOnCard(reg, src, match), true
			case "spell":
				return o, counterPickSpellOnCard(reg, src, match), true
			}
		}
	}
	// A cast/activate step names its own source when the snapshot does not
	// resolve it (an empty or stale snapshot); the card-level scan covers a
	// trigger whose printed index does not resolve.
	if d.Step < len(sc.Steps) {
		st := sc.Steps[d.Step]
		if st.Card != "" {
			kind := "ability"
			if st.Op == "cast" {
				kind = "spell"
			}
			o := rules.OracleSnapStack{Kind: kind, Source: st.Card, Controller: st.Seat}
			if st.AbilityIndex != nil && counterPickAtAbility(reg, st.Card, *st.AbilityIndex, match) {
				return o, true, false
			}
			return o, counterPickOnCard(reg, st.Card, match), false
		}
	}
	return rules.OracleSnapStack{}, false, false
}

// counterPickActivatedSA resolves the activated ability a stack object names
// through the most recent activate step for that source, the same activation
// identity digSourceForStep uses. source is the stack object's bound ref (what
// the activate step names); face is the displayed card whose printed abilities
// the step's index counts into.
func counterPickActivatedSA(reg *cards.Registry, sc Scenario, step int, source, face string) *cards.SA {
	for i := step - 1; i >= 0 && i < len(sc.Steps); i-- {
		st := sc.Steps[i]
		if st.Op != "activate" || st.Card != source || st.AbilityIndex == nil {
			continue
		}
		c, ok := reg.Lookup(oraclediffRefName(face))
		if !ok {
			return nil
		}
		idx := *st.AbilityIndex
		for _, f := range digFaces(c, face) {
			if f != nil && idx >= 0 && idx < len(f.Abilities) {
				return f.Abilities[idx]
			}
		}
		return nil
	}
	return nil
}

// counterPickSourceRef names the card whose printed abilities a stack object
// runs. A copy permanent (Clone, Spark Double) keeps the original's bound ref
// in the stack snapshot while its displayed face is the copied card, so the
// source permanent's Name in the same snapshot is the authority; a ref with no
// resolvable permanent (a spell, a left-the-battlefield source, a face-down
// permanent whose Name is not a card) keeps its own ref.
func counterPickSourceRef(reg *cards.Registry, snap rules.OracleSnapshot, ref string) string {
	for _, p := range snap.Permanents {
		if p.Ref != ref || p.Name == "" {
			continue
		}
		if oraclediffRefName(ref) == p.Name {
			return ref
		}
		// The seat prefix is irrelevant to the lookup; "p0:" keeps a name
		// that itself contains a colon from being split by oraclediffRefName.
		face := "p0:" + p.Name
		if _, ok := reg.Lookup(oraclediffRefName(face)); ok {
			return face
		}
		return ref
	}
	return ref
}

func counterPickAtTrigger(reg *cards.Registry, ref string, idx int, match func(*cards.SA) bool) bool {
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, ref) {
		if f != nil && idx >= 0 && idx < len(f.Triggers) && counterPickInChain(f.Triggers[idx].Effect, match) {
			return true
		}
	}
	return false
}

func counterPickAtAbility(reg *cards.Registry, ref string, idx int, match func(*cards.SA) bool) bool {
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, ref) {
		if f != nil && idx >= 0 && idx < len(f.Abilities) && counterPickInChain(f.Abilities[idx], match) {
			return true
		}
	}
	return false
}

// counterPickOnCard is the card-level fallback: every printed ability and
// trigger on the face is checked, because a stack entry without an exact
// ability or trigger index names only the card.
func counterPickOnCard(reg *cards.Registry, ref string, match func(*cards.SA) bool) bool {
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, ref) {
		if f == nil {
			continue
		}
		for _, sa := range f.Abilities {
			if counterPickInChain(sa, match) {
				return true
			}
		}
		for _, tr := range f.Triggers {
			if counterPickInChain(tr.Effect, match) {
				return true
			}
		}
	}
	return false
}

// counterPickSpellOnCard checks only a face's spell abilities: a spell on the
// stack is resolving its own effect, never one of the permanent's triggers.
func counterPickSpellOnCard(reg *cards.Registry, ref string, match func(*cards.SA) bool) bool {
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, ref) {
		if f == nil {
			continue
		}
		for _, sa := range f.Abilities {
			if counterPickInChain(sa, match) {
				return true
			}
		}
	}
	return false
}

// counterPickInChain walks an ability's SubAbility chain applying match to each
// link.
func counterPickInChain(sa *cards.SA, match func(*cards.SA) bool) bool {
	seen := map[*cards.SA]bool{}
	for ; sa != nil && !seen[sa]; sa = sa.Sub {
		seen[sa] = true
		if match(sa) {
			return true
		}
	}
	return false
}
