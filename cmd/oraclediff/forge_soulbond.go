package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// forgeSoulbondPairSource rewrites the answer to a Soulbond pairing ask so the
// Forge driver pairs the same two creatures gorge did.
//
// Soulbond (CR 702.103a) has two trigger halves. For "when this creature
// enters" (gorge's Soulbond#self) Forge's BondEffect asks for the partner:
// ValidCards$ Creature.Other+YouCtrl+!Paired. For "when another unpaired
// creature you control enters" (Soulbond#other) Forge's BondEffect asks for
// the SOURCE creature: ValidCards$ Creature.Self+!Paired, the unpaired
// Soulbond permanent, while gorge's $ Pair effect offers the entering creature
// it remembered. Both halves produce an identical gorge decision -- choose_n,
// one "pair" pick, resume "soulbond" -- so the export has to recover the
// trigger's role, not the card.
//
// The role comes from the trigger itself. The stack checkpoint entering the
// decision's step records the resolving ability's source and its printed
// trigger slot (OracleSnapStack.Trigger), which names Soulbond#self or
// Soulbond#other on the source card. When the checkpoint still shows the
// creature spell being resolved, the entrant is the source for #self (it is
// not among the offered partners) and is the offered partner for #other (the
// lone Soulbond permanent already on the battlefield is the source). Only when
// neither relation can place the trigger half does the export fall back to the
// source named by the preceding trigger_order decision and the two creatures'
// entry order, read only through the checkpoint entering the decision so a
// later blink or return cannot retroactively change an earlier ask. An ask
// whose half cannot be established is left as gorge answered it rather than
// guessed.
//
// The input slice is not modified; a list with no Soulbond ask is returned as
// is.
func forgeSoulbondPairSource(reg *cards.Registry, g rules.OracleResult, ds []rules.OracleDecision) []rules.OracleDecision {
	out := ds
	cloned := false
	for i, d := range ds {
		if !isSoulbondPairAsk(d) {
			continue
		}
		trig := forgeSoulbondTrigger(reg, g, ds, i, d)
		if trig.source == "" || soulbondPickLabel(trig.source) == "" {
			continue
		}
		switch {
		case trig.known:
			// Soulbond#self already asks the partner, exactly as gorge
			// answered; only the "another creature enters" half asks for the
			// source.
			if !trig.other {
				continue
			}
		case slices.Contains(d.OptionRefs, trig.source):
			// The source is itself an offered partner: this is the
			// "this creature enters" ask (or an unrelated ability's source),
			// and Forge offers that creature as a partner too. Rewriting to it
			// could pair the wrong creature, so leave the answer alone.
			continue
		case !forgeSoulbondSourceIsForgeAnswer(g, trig.source, d):
			continue
		}
		if !cloned {
			out = append([]rules.OracleDecision(nil), ds...)
			cloned = true
		}
		out[i] = forgeSoulbondPairAnswer(d, trig.source)
	}
	return out
}

// isSoulbondPairAsk reports whether d is gorge's Soulbond pairing choice: a
// single-pick choose_n with the "pair" option kind and the "soulbond" resume.
// An empty pick is a decline and is not an answer to rewrite.
func isSoulbondPairAsk(d rules.OracleDecision) bool {
	return d.Kind == "choose_n" && d.Resume == "soulbond" &&
		len(d.PickKinds) == 1 && d.PickKinds[0] == "pair" &&
		len(d.Picks) == 1 && len(d.PickRefs) == 1
}

// forgeSoulbondPairAnswer returns d with its one pick naming src: every
// parallel pick array is rewritten when it has the pick's length, so the
// driver's ref resolution (which prefers pick_refs only while object_picks is
// a subset of it) cannot fall back to the partner.
func forgeSoulbondPairAnswer(d rules.OracleDecision, src string) rules.OracleDecision {
	n := len(d.PickRefs)
	d.Picks = []string{soulbondPickLabel(src)}
	d.PickRefs = []string{src}
	if len(d.PickIdx) == n {
		d.PickIdx = []int{0}
	}
	if len(d.PickRefsInexact) == n {
		d.PickRefsInexact = []bool{false}
	}
	if len(d.ObjectPicks) == n {
		d.ObjectPicks = []string{src}
	}
	return d
}

// soulbondPickLabel is the printed name of a source ref ("p0:token:Soldier#2"
// is "Soldier"), so a token source's label is the name Forge prints, not the
// scenario ref.
func soulbondPickLabel(ref string) string {
	return strings.TrimPrefix(refCardName(ref), "token:")
}

// soulbondTrigger is the Soulbond trigger that posed a pair ask: the ref of
// its source permanent and, when the checkpoint named the printed half, which
// half it was. known is false when only a source could be recovered (a granted
// or token trigger the snapshot's Trigger slot cannot name, or the
// trigger_order fallback); the caller then falls back to the entry-order
// relation.
type soulbondTrigger struct {
	source string
	other  bool
	known  bool
}

// forgeSoulbondTrigger identifies the Soulbond trigger that posed d: the stack
// checkpoint entering d.Step first (an ability's source and printed trigger
// slot, or the creature spell being resolved), then the trigger_order decision
// that preceded d, then the lone Soulbond permanent on the battlefield. A zero
// trigger means the snapshot could not place it.
func forgeSoulbondTrigger(reg *cards.Registry, g rules.OracleResult, ds []rules.OracleDecision, at int, d rules.OracleDecision) soulbondTrigger {
	if d.Step >= 0 && d.Step < len(g.Snapshots) {
		s := g.Snapshots[d.Step]
		if len(s.Stack) > 0 {
			top := s.Stack[0]
			switch top.Kind {
			case "ability":
				if top.Source != "" {
					if other, ok := forgeSoulbondHalf(reg, top.Source, top.Trigger); ok {
						return soulbondTrigger{source: top.Source, other: other, known: true}
					}
					// The trigger slot names no printed Soulbond half (a
					// granted or token trigger, or an unrelated ability that
					// happens to be resolving, such as a blink). Keep its
					// source for the entry-order fallback only when it is a
					// Soulbond permanent; any other ability says nothing about
					// the pair ask.
					if forgeHasSoulbond(s, top.Source) {
						return soulbondTrigger{source: top.Source}
					}
					// An ability that is not a Soulbond trigger is on top,
					// so the checkpoint cannot place the pair ask's trigger
					// (a blinked Soulbond creature re-enters after this
					// snapshot). Guessing a source from the battlefield could
					// pair the wrong creature; leave the answer as gorge made
					// it.
					return soulbondTrigger{}
				}
			case "spell":
				// A resolving creature spell is the entrant. For Soulbond#self
				// the entrant is the Soulbond source; for Soulbond#other it is
				// the offered partner and the source is the lone Soulbond
				// permanent already on the battlefield.
				if top.Source != "" {
					if !slices.Contains(d.OptionRefs, top.Source) {
						return soulbondTrigger{source: top.Source, known: true}
					}
					if src := forgeLoneSoulbondPermanent(s, d.Seat); src != "" {
						return soulbondTrigger{source: src, other: true, known: true}
					}
				}
			}
		}
		if src := forgeLoneSoulbondPermanent(s, d.Seat); src != "" {
			return soulbondTrigger{source: src}
		}
	}
	for j := at - 1; j >= 0; j-- {
		e := ds[j]
		if e.Kind != "order" || e.GorgeKind != "trigger_order" {
			continue
		}
		var found []string
		for k, label := range e.Picks {
			if !strings.Contains(label, "Soulbond") || k >= len(e.PickRefs) || e.PickRefs[k] == "" {
				continue
			}
			found = append(found, e.PickRefs[k])
		}
		if len(found) == 1 {
			return soulbondTrigger{source: found[0]}
		}
	}
	return soulbondTrigger{}
}

// forgeSoulbondHalf reports which Soulbond half the printed trigger at slot is,
// read off the source card's face: true means Soulbond#other, the half whose
// Forge BondEffect asks for the source. ok is false when the slot is empty, out
// of range, ambiguous across a double-faced card's faces, or not a Soulbond
// trigger at all.
func forgeSoulbondHalf(reg *cards.Registry, src, slot string) (other, ok bool) {
	idx, err := strconv.Atoi(slot)
	if err != nil || idx < 0 {
		return false, false
	}
	c, found := reg.Lookup(soulbondPickLabel(src))
	if !found || c == nil {
		return false, false
	}
	half, found := false, false
	for _, f := range c.Faces {
		if idx >= len(f.Triggers) {
			continue
		}
		o, isSoulbond := soulbondHalfOfTrigger(f.Triggers[idx])
		if !isSoulbond {
			continue
		}
		if found {
			// Both faces carry a Soulbond trigger at this slot, so the slot
			// alone cannot say which face was up. Decline rather than guess.
			return false, false
		}
		half, found = o, true
	}
	return half, found
}

// soulbondHalfOfTrigger classifies one printed trigger as a Soulbond half. It
// reads the keyword expansion's own tag first, then falls back to the trigger's
// shape (a ChangesZone-to-battlefield trigger whose effect is the Pair body)
// for a printed or alternate expansion that carries no KeywordLine.
func soulbondHalfOfTrigger(t cards.Trigger) (other, ok bool) {
	switch t.Params["KeywordLine"] {
	case "Soulbond#self":
		return false, true
	case "Soulbond#other":
		return true, true
	}
	if t.Mode != "ChangesZone" || t.Params["Destination"] != "Battlefield" {
		return false, false
	}
	if t.Effect == nil || !strings.EqualFold(t.Effect.API, "Pair") {
		return false, false
	}
	switch t.Params["ValidCard"] {
	case "Creature.Self":
		return false, true
	case "Creature.YouCtrl+Other":
		return true, true
	}
	return false, false
}

// forgeHasSoulbond reports whether ref is a Soulbond permanent in s.
func forgeHasSoulbond(s rules.OracleSnapshot, ref string) bool {
	for _, p := range s.Permanents {
		if p.Ref == ref {
			return slices.Contains(p.Keywords, "Soulbond")
		}
	}
	return false
}

// forgeLoneSoulbondPermanent returns the only Soulbond permanent of seat in s,
// or "" when there is none or more than one (the snapshot cannot say which
// one's trigger fired).
func forgeLoneSoulbondPermanent(s rules.OracleSnapshot, seat int) string {
	src := ""
	for _, p := range s.Permanents {
		if p.Controller != seat || !slices.Contains(p.Keywords, "Soulbond") {
			continue
		}
		if src != "" {
			return ""
		}
		src = p.Ref
	}
	return src
}

// forgeSoulbondSourceIsForgeAnswer is the fallback role test for a trigger the
// checkpoint could not name: Soulbond#other's source was already on the
// battlefield when the answered partner entered. The two creatures' entry
// order is read only through the checkpoint entering the decision, so a blink
// or return after the ask cannot retroactively change it; an equal or
// unplaceable pair is left alone rather than guessed.
func forgeSoulbondSourceIsForgeAnswer(g rules.OracleResult, src string, d rules.OracleDecision) bool {
	sourceAt := forgeArrivalThrough(g, src, d.Step)
	pickAt := forgeArrivalThrough(g, d.PickRefs[0], d.Step)
	return sourceAt >= 0 && pickAt >= 0 && sourceAt < pickAt
}

// forgeArrivalThrough is the latest checkpoint at or before step in which ref
// became present on the battlefield (a permanent, or the source of a spell on
// the stack, counting a spell that then resolves as one continuous entry), and
// -1 when those checkpoints do not show it. Scanning stops at step, the
// checkpoint entering the decision's step.
func forgeArrivalThrough(g rules.OracleResult, ref string, step int) int {
	if step >= len(g.Snapshots) {
		step = len(g.Snapshots) - 1
	}
	if step < 0 {
		return -1
	}
	arrival := -1
	present := false
	for i := 0; i <= step; i++ {
		s := g.Snapshots[i]
		now := false
		for _, p := range s.Permanents {
			if p.Ref == ref {
				now = true
				break
			}
		}
		if !now {
			for _, st := range s.Stack {
				if st.Kind == "spell" && st.Source == ref {
					now = true
					break
				}
			}
		}
		if now && !present {
			arrival = i
		}
		present = now
	}
	return arrival
}
