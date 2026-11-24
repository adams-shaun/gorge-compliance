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
// The source is read as the permanent's DISPLAYED card (bolsterSourceRef): a
// copy (Clone, Spark Double) keeps the original's bound ref on the stack while
// its face is the copied card. This is structural, not card- or label-based:
// every Bolster carrier that names Bolster$ on its own printed face (a spell,
// an activated ability or a trigger, including a copied one) takes this
// route. A bolster ability GRANTED by another card (Gleam of Authority's
// AddAbility$ ABBolster) lives in the grantor's SVar, not the recipient's
// face: the activate step is resolved through ResolveActivatedAbility, the
// resolver every other granted-ability routing uses, so the SA it names is the
// granted body whatever grants it. A Support$ pick and a bare Choices$/Chooser$ pick stay on
// the choice queue -- XMage poses those through Player.choose/makeChoose
// (or, for Support, as a target declared when the ability goes on the stack,
// not as a resolution-time pick).
func bolsterTiePick(reg *cards.Registry, sc Scenario, g rules.OracleResult, d rules.OracleDecision) bool {
	snaps := g.Snapshots
	if reg == nil || d.Step < 0 || pickKind(d, 0) != "counter_pick" {
		return false
	}
	// The stack snapshot at the decision's step is the authority. Its slice is
	// top-first, so walk it in order and stop at the first object whose source
	// card resolves: that is the object resolving while the ask is posed.
	if d.Step < len(snaps) {
		for _, o := range snaps[d.Step].Stack {
			bolsters, resolved := bolsterOnStackObject(reg, sc, g, d.Step, o)
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
// Bolster effect, and whether its source card resolved at all. The abilities
// are read off the source's displayed card (bolsterSourceRef); the scenario's
// activate steps still name the object by its bound ref (o.Source).
func bolsterOnStackObject(reg *cards.Registry, sc Scenario, g rules.OracleResult, step int, o rules.OracleSnapStack) (bolsters, resolved bool) {
	if reg == nil || o.Source == "" || step < 0 || step >= len(g.Snapshots) {
		return false, false
	}
	snap := g.Snapshots[step]
	src := bolsterSourceRef(reg, snap, o.Source)
	if _, ok := reg.Lookup(oraclediffRefName(src)); !ok {
		return false, false
	}
	switch o.Kind {
	case "ability":
		if o.Trigger != "" {
			idx, err := strconv.Atoi(o.Trigger)
			if err != nil {
				return bolsterOnCard(reg, src), true
			}
			return bolsterAtTrigger(reg, src, idx), true
		}
		if sa := bolsterActivatedSA(reg, sc, g, step, o.Source, src); sa != nil {
			return bolsterInChain(sa), true
		}
		return bolsterOnCard(reg, src), true
	case "spell":
		return bolsterSpellOnCard(reg, src), true
	}
	return false, false
}

// bolsterActivatedSA resolves the activated ability a stack object names
// through the most recent activate step for that source, the same activation
// identity digSourceForStep uses.
// source is the stack object's bound ref (what the activate step names); face
// is the displayed card whose printed abilities the step's index counts into.
//
// A step that names a granted, gained or keyword-granted ability (Gleam of
// Authority's AddAbility$ ABBolster) carries an AbilityIndex that aliases a
// printed one, or none at all, so it is resolved first through
// ResolveActivatedAbility. An unresolved grant fails closed to no SA: the
// printed index must never stand in for it.
func bolsterActivatedSA(reg *cards.Registry, sc Scenario, g rules.OracleResult, step int, source, face string) *cards.SA {
	it := Item{Scenario: Scenario{Setup: sc.Setup}}
	for i := step - 1; i >= 0 && i < len(sc.Steps); i-- {
		st := sc.Steps[i]
		if st.Op != "activate" || st.Card != source {
			continue
		}
		switch sa, kind := ResolveActivatedAbility(reg, it, i, st, g); kind {
		case ActivatedGranted:
			return sa
		case ActivatedGrantedUnresolved:
			return nil
		}
		if st.AbilityIndex == nil {
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

// bolsterSourceRef names the card whose printed abilities a stack object runs.
// A copy permanent (Clone, Spark Double) keeps the original's bound ref in the
// stack snapshot while its displayed face is the copied card, so the source
// permanent's Name in the same snapshot is the authority; a ref with no
// resolvable permanent (a spell, a left-the-battlefield source, a face-down
// permanent whose Name is not a card) keeps its own ref.
func bolsterSourceRef(reg *cards.Registry, snap rules.OracleSnapshot, ref string) string {
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
