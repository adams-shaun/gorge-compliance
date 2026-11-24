package main

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
)

// forgeLibraryTopOrder reverses every multi-card library move that ends on TOP
// of its owner's library, so Forge's one-card-at-a-time "put it on top" asks
// rebuild gorge's top-first order.
//
// Two shapes carry such a move. A hand -> library put-back is a "choose_n"
// batch with resume "hand_move" (Dream Cache's "put two cards from your hand
// both on top"; Stunted Growth's "chooses three cards ... puts them on top"),
// whose picks are in the order the cards end up in from the top down. A CR
// 401.4 arrangement is a "library_placement" arrange over the cards one effect
// puts at the same position (Harmonic Convergence's "put all enchantments on
// top"), whose first pick is likewise nearest the top. Forge's
// ChangeZoneEffect asks for one card at a time and puts each successive pick on
// top, so the last pick ends topmost: feeding either shape in gorge's order
// reverses it (CR 401.4 allows either order, so both engines are rules-correct;
// only the export convention differs). A third shape, gorge's
// effRearrangeTopOfLibrary (Sensei's Divining Top, Ponder, Index, Portent: a
// forced "arrange", resume "arrange", every option Kind "bottom"), is the same
// case: Forge's RearrangeTopOfLibraryEffect moves each scripted card to index
// 0, so the last ends topmost (oraclegen.FullTopReorder, two or more cards).
// Reversing the picks makes the successive placements reproduce gorge's order,
// with the first card topmost again. A
// bottom placement needs no reversal: successive "put on the bottom" appends
// leave the first pick above the second, which is gorge's bottom order.
//
// The destination is re-derived from the checkpoint after the decision (see
// oraclegen.LibraryTopPlacements), because the decision alone does not say
// whether a hand_move/library_placement went to the top or the bottom. A
// hand_move batch can instead be paired with the spell's following
// "changezone_alternative" ask (its single pick is "top" or "bottom"; it is
// that ask, not the batch's own "first", which is option 0's label, "top" for
// both alternatives). The batch and the alternative can belong to different
// seats, so they are paired by step and order of the runner log, not by seat.
// Each alternative is paired at most once, so two hand_move batches in one step
// take successive alternatives. The alternative overrides the checkpoint: a
// bottom placement into an empty library (or onto a top already holding the
// same names) leaves the same after-step top as a top placement, and only the
// explicit ask can tell them apart.
//
// Only the parallel pick arrays reverse, and only the ones as long as Picks
// (every one is parallel by contract, but a rewrite must not corrupt a
// decision whose array is short): Picks, PickKinds, PickRefs, PickIdx,
// PickRefsInexact and ObjectPicks. First stays (it is option 0's label);
// OptionRefs, OptionGroups, ArrangeLabels and ArrangeKind are offered state,
// not pick order, and stay too. The input is never mutated: the returned
// slice is a copy when anything changes.
func forgeLibraryTopOrder(ds []rules.OracleDecision, snaps []rules.OracleSnapshot) []rules.OracleDecision {
	out := ds
	cloned := false
	tops := oraclegen.LibraryTopPlacements(ds, snaps)
	for i := range ds {
		if !tops[i] && !(len(ds[i].Picks) >= 2 && oraclegen.FullTopReorder(ds[i])) {
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
