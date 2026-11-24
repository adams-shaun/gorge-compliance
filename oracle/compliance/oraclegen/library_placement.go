package oraclegen

import (
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// LibraryPlacementOrder reports whether d is gorge's CR 401.4 arrangement of
// cards one effect puts at a library position together: a forced KArrange
// (Min == Max == Options) whose options all share the "library_placement"
// destination. The first pick is nearest the top of the placed group; the
// arrangement itself does not record whether that position is the top or the
// bottom of the library.
func LibraryPlacementOrder(d rules.OracleDecision) bool {
	return d.GorgeKind == "arrange" && d.ArrangeKind == "library_placement" &&
		len(d.Picks) >= 2 && d.Min == d.Max && d.Max == d.Options
}

// FullTopReorder reports whether d is gorge's effRearrangeTopOfLibrary answer
// (CR 401.4): a forced KArrange resumed as "arrange" whose every option has
// the shared Kind "bottom" (pile B is empty), with picks that name every
// offered card. The picks are the final top-first order. A Dig's
// "dig_arrange"/"dig_bottom" remainder carries another kind and is not this
// shape. A one-card order matches too (gorge posts it when the look holds one
// card); callers that must skip it check len(d.Picks) >= 2 themselves.
func FullTopReorder(d rules.OracleDecision) bool {
	return d.GorgeKind == "arrange" && d.Resume == "arrange" && d.ArrangeKind == "bottom" &&
		d.Min == d.Max && d.Max == d.Options && len(d.Picks) == d.Options
}

// HandMoveBatch reports whether d is a multi-card hand move: a choose_n
// answered by a "hand_move" resume, with two or more picks whose recorded
// kinds are all "hand_move". The destination (library top, battlefield,
// graveyard, ...) is not in the decision; see LibraryTopPlacement.
func HandMoveBatch(d rules.OracleDecision) bool {
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

// LibraryTopPlacement reports whether d is a multi-card move gorge recorded
// top-first onto one seat's library: a hand_move batch or a library_placement
// arrange, with the checkpoint after d.Step showing the picked cards at the
// top of that seat's library in pick order.
//
// The destination is not a decision field (both a top and a bottom placement
// are the same KArrange/hand_move shape), so it is re-derived from the state
// the move produced rather than guessed from the card's script. A decision
// whose after-step checkpoint has no library top (a hand -> battlefield move),
// puts the picks below the top (a bottom placement), or leaves them in
// another zone is not a top placement. The re-derivation is what lets both
// followers share one predicate: the next multi-card library mover is covered
// without another per-card list.
//
// snaps == nil, or a decision whose after-step checkpoint is missing, is no
// evidence at all: both a top and a bottom placement have the same shape and
// the same resulting library top when the library was empty (or already
// topped by the same names), so the predicate refuses to guess and reports
// false. A caller that has the decision list should use LibraryTopPlacements,
// which lets an explicit changezone_alternative override the checkpoint.
func LibraryTopPlacement(d rules.OracleDecision, snaps []rules.OracleSnapshot) bool {
	arrange := LibraryPlacementOrder(d)
	if !arrange && !HandMoveBatch(d) {
		return false
	}
	if snaps == nil {
		return false
	}
	if d.Step < 0 || d.Step+1 >= len(snaps) {
		return false
	}
	names := pickedNames(d)
	if len(names) < 2 {
		return false
	}
	after := snaps[d.Step+1]
	if d.Seat < 0 || d.Seat >= len(after.Players) {
		return false
	}
	p := after.Players[d.Seat]
	top := p.LibraryTop
	if len(top) == 0 {
		return false
	}
	n := len(names)
	if n > len(top) {
		// The snapshot keeps at most the top five: with more picks, a full
		// window that matches their first five is the strongest evidence it
		// can carry.
		n = len(top)
		if n < 5 {
			return false
		}
	}
	if !slices.Equal(names[:n], top[:n]) {
		return false
	}
	// A picked card still in another zone after the step did not move to the
	// library at all (a hand -> battlefield/graveyard/exile move). A
	// same-named sibling in the zone keeps the check conservative: it can
	// make a real top placement undetected, but it cannot mistake one for a
	// move that never happened.
	if containsAllNames(p.Hand, names) || containsAllNames(p.Graveyard, names) ||
		containsAllNames(p.Exile, names) {
		return false
	}
	permanents := make([]string, len(after.Permanents))
	for i, perm := range after.Permanents {
		permanents[i] = perm.Name
	}
	return !containsAllNames(permanents, names)
}

// LibraryTopPlacements derives LibraryTopPlacement for every decision and lets
// the move's own changezone_alternative override the checkpoint. The
// alternative is the spell's explicit "top"/"bottom" ask: a bottom placement
// into an empty library, or onto a top already holding the same names, leaves
// exactly the same after-step top as a top placement, so the checkpoint alone
// cannot tell them apart. The decision list (and its runner-log order) is what
// pairs a hand_move batch with its alternative; a bottom ask therefore clears
// the checkpoint's false positive instead of reversing the pile.
//
// Alternatives pair by step and order of the runner log, not by seat: the
// spell controller's position ask can belong to a different seat than the
// batch. Each alternative is consumed at most once, so two batches in one step
// take successive alternatives. Only a hand_move batch has an alternative
// (changeZone's DestinationAlternative); a library_placement arrange has no
// corresponding ask and keeps the checkpoint's reading.
func LibraryTopPlacements(ds []rules.OracleDecision, snaps []rules.OracleSnapshot) []bool {
	tops := make([]bool, len(ds))
	paired := make([]bool, len(ds))
	for i := range ds {
		top := LibraryTopPlacement(ds[i], snaps)
		if HandMoveBatch(ds[i]) {
			if j := changezoneAlternativeAfter(ds, i, paired); j >= 0 {
				paired[j] = true
				switch {
				case strings.EqualFold(strings.TrimSpace(ds[j].Picks[0]), "top"):
					top = true
				case strings.EqualFold(strings.TrimSpace(ds[j].Picks[0]), "bottom"):
					top = false
				}
			}
		}
		tops[i] = top
	}
	return tops
}

// changezoneAlternativeAfter returns the index of the first unpaired
// "changezone_alternative" decision after i that shares i's step, or -1. The
// scan stops at the step boundary: an alternative in a later step is another
// move.
func changezoneAlternativeAfter(ds []rules.OracleDecision, i int, paired []bool) int {
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

// pickedNames returns the card names of d's picks, using the exact object ref
// when one is recorded so a same-named copy still names its card.
func pickedNames(d rules.OracleDecision) []string {
	names := make([]string, len(d.Picks))
	for k, label := range d.Picks {
		if k < len(d.PickRefs) && isScenarioRefShaped(d.PickRefs[k]) {
			names[k] = oraclediffRefName(d.PickRefs[k])
			continue
		}
		names[k] = label
	}
	return names
}

// containsAllNames reports whether zone holds at least one entry for every
// name in names (a multiset check, so a duplicate pick needs two entries).
func containsAllNames(zone, names []string) bool {
	need := make(map[string]int, len(names))
	for _, name := range names {
		need[name]++
	}
	for _, name := range zone {
		if need[name] > 0 {
			need[name]--
		}
	}
	for _, left := range need {
		if left > 0 {
			return false
		}
	}
	return true
}

// HandMoveOrderAsk reports whether XMage asks the "card ORDER to put … from
// the TOP" dialog for d's top hand_move batch. Only an effect whose chooser is
// the TARGETED player is implemented with putCardsOnTopOfLibrary(...,
// anyOrder=true) and so asks it (Stunted Growth, Phyrexian Chimney Imp); a
// self put-back (Brainstorm, Dream Cache) moves the cards without an order
// dialog, and scripting the order answers there leaves a choice queued for the
// next ask. The source is resolved from the step's stack checkpoint, falling
// back to the scenario's own cast/activate step, and the card IR must carry a
// hand -> library top ChangeZone with Chooser$ Targeted. No verified source
// means no order answers: never guess.
func HandMoveOrderAsk(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, d rules.OracleDecision) bool {
	if reg == nil || !HandMoveBatch(d) || d.Step < 0 {
		return false
	}
	src := handMoveSource(sc, snaps, d.Step)
	if src == "" {
		return false
	}
	c, ok := reg.Lookup(oraclediffRefName(src))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, src) {
		if f == nil {
			continue
		}
		for _, sa := range f.Abilities {
			if targetedHandToTop(sa) {
				return true
			}
		}
		for _, tr := range f.Triggers {
			if targetedHandToTop(tr.Effect) {
				return true
			}
		}
	}
	return false
}

// handMoveSource is the card/stack ref that posed the decision at step: the
// resolving object in that step's stack checkpoint, or the most recent
// cast/play/activate step naming a card.
func handMoveSource(sc Scenario, snaps []rules.OracleSnapshot, step int) string {
	if step >= 0 && step < len(snaps) {
		for _, o := range snaps[step].Stack {
			if o.Source != "" {
				return o.Source
			}
		}
	}
	for i := step; i >= 0; i-- {
		if i >= len(sc.Steps) {
			continue
		}
		st := sc.Steps[i]
		if st.Card != "" && (st.Op == "cast" || st.Op == "play" || st.Op == "activate") {
			return st.Card
		}
	}
	return ""
}

// targetedHandToTop walks one ability chain and reports whether it puts two or
// more cards from a hand on top of a library for a TARGETED chooser. The
// ChangeNum is the number the XMage order dialog moves; a variable or
// single-card count does not ask the multi-card order prompt.
func targetedHandToTop(sa *cards.SA) bool {
	seen := make(map[*cards.SA]bool)
	for sa != nil && !seen[sa] {
		seen[sa] = true
		if strings.EqualFold(strings.TrimSpace(sa.Params["Chooser"]), "Targeted") &&
			strings.EqualFold(strings.TrimSpace(sa.Params["Destination"]), "Library") &&
			strings.Contains(strings.ToLower(sa.Params["Origin"]), "hand") &&
			topLibraryPosition(sa.Params["LibraryPosition"]) {
			if n, err := strconv.Atoi(strings.TrimSpace(sa.Params["ChangeNum"])); err == nil && n >= 2 {
				return true
			}
		}
		sa = sa.Sub
	}
	return false
}

// topLibraryPosition reports whether a LibraryPosition$ value places at the
// top: "0" and Forge's absent default.
func topLibraryPosition(raw string) bool {
	switch strings.TrimSpace(raw) {
	case "", "0":
		return true
	default:
		return false
	}
}
