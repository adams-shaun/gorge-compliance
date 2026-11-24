package main

import (
	"slices"
	"strings"

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
// The resolving trigger's source comes from the stack checkpoint entering the
// decision's step (an ability's Source), then from the path that put a
// creature on the battlefield (a resolving creature spell: the entrant is the
// source for Soulbond#self, and for Soulbond#other it is the option while the
// source is the Soulbond permanent already on the battlefield), then from the
// preceding trigger_order decision, then from a lone Soulbond permanent on the
// battlefield. When the source is the answer Forge wants, the single pick is
// rewritten to it; the Soulbond#self ask already names the partner and is left
// alone. Which half fired is decided by which of the two creatures entered
// most recently: #other's source is the existing one, #self's source is the
// entering one. An ask whose source the snapshots cannot place, or one whose
// two candidates entered together, is left as gorge answered it.
//
// The input slice is not modified; a list with no Soulbond#other ask is
// returned as is.
func forgeSoulbondPairSource(g rules.OracleResult, ds []rules.OracleDecision) []rules.OracleDecision {
	out := ds
	cloned := false
	for i, d := range ds {
		if !isSoulbondPairAsk(d) {
			continue
		}
		src := forgeSoulbondTriggerSource(g, ds, i, d)
		if src == "" || soulbondPickLabel(src) == "" || slices.Contains(d.OptionRefs, src) {
			continue
		}
		if !forgeSoulbondSourceIsForgeAnswer(g, src, d) {
			continue
		}
		if !cloned {
			out = append([]rules.OracleDecision(nil), ds...)
			cloned = true
		}
		out[i] = forgeSoulbondPairAnswer(d, src)
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

// forgeSoulbondTriggerSource names the permanent whose Soulbond trigger posed
// d. The stack checkpoint first (the ability's source, or the creature spell
// being resolved when it is not the offered partner, i.e. Soulbond#self),
// then the trigger_order decision that preceded d, then the lone Soulbond
// permanent on the battlefield. "" means the snapshot could not place it.
func forgeSoulbondTriggerSource(g rules.OracleResult, ds []rules.OracleDecision, at int, d rules.OracleDecision) string {
	if d.Step >= 0 && d.Step < len(g.Snapshots) {
		s := g.Snapshots[d.Step]
		if len(s.Stack) > 0 {
			top := s.Stack[0]
			switch top.Kind {
			case "ability":
				if top.Source != "" {
					return top.Source
				}
			case "spell":
				// A resolving creature spell is the entrant. For Soulbond#self
				// the entrant is the Soulbond source; for Soulbond#other it is
				// the offered partner, and the source is the permanent below.
				if top.Source != "" && !slices.Contains(d.OptionRefs, top.Source) {
					return top.Source
				}
			}
		}
		if src := forgeLoneSoulbondPermanent(s, d.Seat); src != "" {
			return src
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
			return found[0]
		}
	}
	return ""
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

// forgeSoulbondSourceIsForgeAnswer reports whether the Soulbond#other half
// posed d: its trigger source is the creature that was already on the
// battlefield, while the answered pick is the entrant. Soulbond#self's source
// is the entrant itself, so it is left for gorge's own partner answer. The
// two candidates' latest entries decide (latest, not first, so a creature
// that left and re-entered counts as the newer entrant); an equal or
// unplaceable pair is left alone rather than guessed.
func forgeSoulbondSourceIsForgeAnswer(g rules.OracleResult, src string, d rules.OracleDecision) bool {
	sourceAt := forgeLastArrival(g, src)
	pickAt := forgeLastArrival(g, d.PickRefs[0])
	return sourceAt >= 0 && pickAt >= 0 && sourceAt < pickAt
}

// forgeLastArrival is the latest checkpoint in which ref became present on the
// battlefield (a permanent, or the source of a spell on the stack, counting a
// spell that then resolves as one continuous entry), and -1 when no checkpoint
// shows it.
func forgeLastArrival(g rules.OracleResult, ref string) int {
	arrival := -1
	present := false
	for i, s := range g.Snapshots {
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
