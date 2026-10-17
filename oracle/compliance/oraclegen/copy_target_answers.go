package oraclegen

import "github.com/adams-shaun/gorge/engine/rules"

// copyTargetAnswers scripts a copy's CR 707.10c new-target election.
//
// XMage's chooseNewTarget loops over each INHERITED target of the copy's
// declaration and asks "Change this N of M target: X?"; `no` keeps that slot,
// `yes` runs one chooseTarget (min=max=1) and replaces just that slot, then
// moves on to the next. gorge poses ONE target decision whose leading
// CopyInherited options are those inherited slots (keep-current), followed by
// the remaining legal candidates, so a pick whose option index is below the
// count keeps that slot and any higher index is a change. The changed picks
// are assigned to the non-kept slots in option order. A slot with no changed
// pick left -- an "up to N" copy that dropped it (Min 0) -- keeps its old
// target, the only outcome XMage's per-slot replace flow can express.
//
// No trailing target skip is emitted: after a `yes` XMage's temp target is
// min=max=1 and asks exactly once, so a skip would be an unconsumed leftover.
func copyTargetAnswers(d rules.OracleDecision) []XAnswer {
	n := d.CopyInherited
	kept := make(map[int]bool, n)
	var changed []int
	for k, idx := range d.PickIdx {
		if idx >= 0 && idx < n {
			kept[idx] = true
			continue
		}
		changed = append(changed, k)
	}
	as := make([]XAnswer, 0, 2*n)
	c := 0
	for slot := 0; slot < n; slot++ {
		if kept[slot] || c >= len(changed) {
			as = append(as, XAnswer{d.Seat, "choice", "no"})
			continue
		}
		as = append(as, XAnswer{d.Seat, "choice", "yes"})
		as = append(as, singleTargetAnswer(d, changed[c]))
		c++
	}
	return as
}
