package main

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// forgeLibraryTopOrder reverses a multi-card "hand_move" batch that the
// resolving spell then puts on TOP of its owner's library.
//
// gorge records a hand -> library move ("Draw three cards, then put two cards
// from your hand both on top of your library", Dream Cache) as a "choose_n"
// batch with resume "hand_move", whose picks are in the order the cards end up
// in from the top down. Forge's ChangeZoneEffect asks for one card at a time
// and puts each successive pick on top, so the last pick ends topmost: feeding
// the batch in gorge's order would reverse it (CR 401.4 allows either, so both
// engines are rules-correct; only the export convention differs). Reversing
// the batch makes the successive picks place gorge's order, with the first
// card topmost again. A bottom placement needs no reversal: successive "put on
// the bottom" appends leave the first pick above the second, which is gorge's
// bottom order.
//
// The destination comes from the decision that follows the batch in the same
// step: the spell's "changezone_alternative" ask, whose single pick is "top"
// or "bottom". It is the alternative ask, not the batch's own "first" (option
// 0's label, "top" for both alternatives). The batch and the alternative can
// belong to different seats, so they are paired by step and order of the
// runner log, not by seat. Each alternative is paired at most once, so two
// hand_move batches in one step take successive alternatives; a batch with no
// alternative left in its step (a hand -> battlefield move, Brainstorm's
// plain top-only put-back) is left alone, because the decision alone does not
// say where the cards go.
//
// Only the parallel pick arrays reverse, and only the ones as long as Picks
// (every one is parallel by contract, but a rewrite must not corrupt a
// decision whose array is short): Picks, PickKinds, PickRefs, PickIdx,
// PickRefsInexact and ObjectPicks. First stays (it is option 0's label);
// OptionRefs, OptionGroups, ArrangeLabels and ArrangeKind are offered state,
// not pick order, and stay too. The input is never mutated: the returned
// slice is a copy when anything changes.
func forgeLibraryTopOrder(ds []rules.OracleDecision) []rules.OracleDecision {
	out := ds
	cloned := false
	paired := make([]bool, len(ds))
	for i := range ds {
		if !forgeHandMoveBatch(ds[i]) {
			continue
		}
		j := forgeAlternativeAfter(ds, i, paired)
		if j < 0 {
			continue
		}
		paired[j] = true
		if !strings.EqualFold(ds[j].Picks[0], "top") {
			continue
		}
		if !cloned {
			out = append([]rules.OracleDecision(nil), ds...)
			cloned = true
		}
		out[i] = forgeReversedHandMove(ds[i])
	}
	return out
}

// forgeHandMoveBatch reports whether d is a multi-card hand -> library batch:
// a "choose_n" answered by a "hand_move" resume, with two or more picks whose
// recorded kinds are all "hand_move". The pick-kind check is what tells a
// library placement from any other two-pick choose; the resume alone names the
// continuation, not the move.
func forgeHandMoveBatch(d rules.OracleDecision) bool {
	if d.Kind != "choose_n" || d.Resume != "hand_move" || len(d.Picks) < 2 {
		return false
	}
	for _, k := range d.PickKinds {
		if k != "hand_move" {
			return false
		}
	}
	return true
}

// forgeAlternativeAfter returns the index of the first unpaired
// "changezone_alternative" decision after i that shares i's step, or -1. The
// scan stops at the step boundary: an alternative in a later step is another
// move.
func forgeAlternativeAfter(ds []rules.OracleDecision, i int, paired []bool) int {
	step := ds[i].Step
	for j := i + 1; j < len(ds) && ds[j].Step == step; j++ {
		d := ds[j]
		if paired[j] || d.Kind != "choose_n" || d.Resume != "changezone_alternative" || len(d.Picks) != 1 {
			continue
		}
		return j
	}
	return -1
}

// forgeReversedHandMove returns a copy of d whose parallel pick arrays are in
// reverse order. Arrays that are not as long as Picks are left as they are.
func forgeReversedHandMove(d rules.OracleDecision) rules.OracleDecision {
	n := len(d.Picks)
	d.Picks = forgeReversed(d.Picks)
	if len(d.PickKinds) == n {
		d.PickKinds = forgeReversed(d.PickKinds)
	}
	if len(d.PickRefs) == n {
		d.PickRefs = forgeReversed(d.PickRefs)
	}
	if len(d.PickIdx) == n {
		d.PickIdx = forgeReversed(d.PickIdx)
	}
	if len(d.PickRefsInexact) == n {
		d.PickRefsInexact = forgeReversed(d.PickRefsInexact)
	}
	if len(d.ObjectPicks) == n {
		d.ObjectPicks = forgeReversed(d.ObjectPicks)
	}
	return d
}

// forgeReversed returns a new slice holding xs in reverse order.
func forgeReversed[T any](xs []T) []T {
	out := make([]T, len(xs))
	for i, x := range xs {
		out[len(xs)-1-i] = x
	}
	return out
}
