package main

import (
	"fmt"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
)

// forgeGraveyardOrderDecisions adds one "order" decision per step and seat
// whose gorge checkpoints show two or more differently named cards entering
// that seat's graveyard between the two checkpoints. Forge asks "order to
// Graveyard" separately from the selection (a board wipe or mass sacrifice
// moves the cards together and the owner arranges them, CR 404.3), and
// orderCards consumes the picks in that order, so the driver reproduces
// gorge's graveyard exactly.
//
// The decision is separate from the selection decision because that decision
// was already consumed when the move started: a sacrifice's choose_n answers
// which cards, not the order they are put down in. It is derived from the
// checkpoints, not from the picks, so a move with no choice at all (a -X/-X
// wipe's state-based deaths) is covered too. The role is gorge_kind
// "graveyard_order"; DecisionQueue keeps an unasked pin out of the leftover
// list, since an order Forge never asks about is unobservable.
//
// Limitation: one pin per step and seat is the finest granularity gorge's
// checkpoints expose; they do not say which cards moved in the same event. If
// one step contains two separate simultaneous multi-card moves, the pin lists
// both groups in the final zone order. Forge's first "order to Graveyard" ask
// in that step consumes the pin and orders its offered subset by it (extra
// picks are skipped), so the second ask still has no pin and misses. That is
// no worse than the previous unconditional miss, but it is not a pin per
// event; splitting the delta would need event data the runner does not
// record.
func forgeGraveyardOrderDecisions(it oraclegen.Item, g rules.OracleResult, ds []rules.OracleDecision) []rules.OracleDecision {
	if len(g.Snapshots) < len(it.Steps)+1 {
		return ds
	}
	have := make(map[[2]int]bool, len(ds))
	for _, d := range ds {
		if d.Kind == "order" && d.GorgeKind == "graveyard_order" {
			have[[2]int{d.Step, d.Seat}] = true
		}
	}
	out := ds
	for step := 0; step < len(it.Steps); step++ {
		from, to := g.Snapshots[step].Players, g.Snapshots[step+1].Players
		for seat := 0; seat < len(from) && seat < len(to); seat++ {
			if have[[2]int{step, seat}] {
				continue
			}
			added := graveyardAddedInOrder(from[seat].Graveyard, to[seat].Graveyard)
			distinct := make(map[string]bool, len(added))
			for _, name := range added {
				distinct[name] = true
			}
			// A single card, or copies of one card, leave the order
			// unobservable; orderCards returns the offered list as is then.
			if len(added) < 2 || len(distinct) < 2 {
				continue
			}
			refs := make([]string, len(added))
			for i, name := range added {
				refs[i] = fmt.Sprintf("p%d:%s", seat, name)
			}
			out = append(out, rules.OracleDecision{
				Step: step, Seat: seat, Kind: "order", GorgeKind: "graveyard_order",
				Options: len(added), Picks: added, PickRefs: refs,
				Min: len(added), Max: len(added),
			})
		}
	}
	return out
}

// forgeDropGraveyardOrderDecisions removes the Forge-only role from decisions,
// for a caller (follower-export) whose engine queue has no consumer for it.
// It applies to the whole list, not just the pins this file adds, so a role
// that ever reaches the decisions from the runner cannot leak either. The
// result is non-nil so the request still encodes gorge_decisions as [].
func forgeDropGraveyardOrderDecisions(ds []rules.OracleDecision) []rules.OracleDecision {
	out := make([]rules.OracleDecision, 0, len(ds))
	for _, d := range ds {
		if d.Kind == "order" && d.GorgeKind == "graveyard_order" {
			continue
		}
		out = append(out, d)
	}
	return out
}

// graveyardAddedInOrder is the suffix of newly added cards between two
// graveyard listings, in the new listing's zone order (oldest first). It is a
// multiset difference, not a name set: the same card can be milled twice.
func graveyardAddedInOrder(old, next []string) []string {
	counts := make(map[string]int, len(old))
	for _, name := range old {
		counts[name]++
	}
	var added []string
	for _, name := range next {
		if counts[name] > 0 {
			counts[name]--
		} else {
			added = append(added, name)
		}
	}
	return added
}
