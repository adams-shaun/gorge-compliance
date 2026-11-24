package oraclegen

import (
	"slices"

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
// snaps == nil is the shape-only case (a caller with no scenario, e.g. the
// XAnswers unit surface). A library_placement arrange is then the measured top
// arrangement; a hand_move has no destination evidence and is left alone.
func LibraryTopPlacement(d rules.OracleDecision, snaps []rules.OracleSnapshot) bool {
	arrange := LibraryPlacementOrder(d)
	if !arrange && !HandMoveBatch(d) {
		return false
	}
	if snaps == nil {
		return arrange
	}
	if d.Step < 0 || d.Step+1 >= len(snaps) {
		// No after-step checkpoint: keep the shape-only reading that the
		// measured library_placement is a top arrangement.
		return arrange
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
	// only make a real top placement undetected, never invent one.
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
