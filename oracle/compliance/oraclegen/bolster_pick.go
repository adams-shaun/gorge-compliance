package oraclegen

import (
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
// The source is resolved by counterPickSource (counter_pick_source.go): the
// stack snapshot at the decision's own step is the authority, with the step's
// own cast/activate card as the fallback. The resolved ability's effect chain
// must carry PutCounter's Bolster$ parameter. This is structural, not card- or
// label-based: every Bolster carrier that names Bolster$ on its own printed
// face (a spell, an activated ability or a trigger, including a copied one)
// takes this route. A bolster ability GRANTED by another card (Gleam of
// Authority's AddAbility$ ABBolster) lives in the grantor's SVar, not the
// recipient's face, and is not seen here. A Support$ pick is XMage's target at
// the source's ANNOUNCEMENT, not at resolution, and takes the separate
// support_pick.go route; a bare Choices$/Chooser$ pick stays on the choice
// queue.
func bolsterTiePick(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, d rules.OracleDecision) bool {
	if reg == nil || d.Step < 0 || pickKind(d, 0) != "counter_pick" {
		return false
	}
	_, matched, _ := counterPickSource(reg, sc, snaps, d, bolsterMatch)
	return matched
}

// bolsterMatch reports whether one ability is PutCounter's Bolster$ arm (the
// one primitive putCounterBolster serves).
func bolsterMatch(sa *cards.SA) bool {
	return sa.API == "PutCounter" && strings.TrimSpace(sa.Params["Bolster"]) != ""
}
