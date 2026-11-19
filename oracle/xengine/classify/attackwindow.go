package classify

import (
	"encoding/json"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
)

// An attackWindow is one declare attackers step of gorge's in which no
// creature attacks: gorge's checkpoints Entry..Close-1 are in it. Entry is the
// checkpoint of the pass that ends beginning of combat (the non-active seat's,
// on an empty stack), when no creature could attack, or of the attack step
// that declares no attacker, when one could. phase.rs gives no priority in
// the step either way: it goes to end of combat at once. Close is the first checkpoint after it, the end of
// combat (CR 508.8 skips declare blockers and combat damage when no creature
// attacks), or len(g.Snapshots) when the item ends inside the step. The steps
// gorge plays in it are Entry..Close-1. Passes is true when every one of them
// is a pass, on an empty stack, with no creature attacking: no player did
// anything there, so an engine that never gave the step (phase.rs, when no
// creature can attack) has missed nothing but the priority passes.
type attackWindow struct {
	Entry, Close int
	Passes       bool
}

// attackWindows lists gorge's empty declare attackers steps in steps' order.
func attackWindows(steps []oraclegen.Step, g rules.OracleResult) []attackWindow {
	var out []attackWindow
	for k := 1; k < len(g.Snapshots); k++ {
		gs, prev := g.Snapshots[k], g.Snapshots[k-1]
		if gs.Step != "declare-attackers" || len(prev.Stack) != 0 || k > len(steps) {
			continue
		}
		switch st := steps[k-1]; {
		case prev.Step == "begin-combat" && st.Op == "pass" && st.Seat != gs.Active:
			// the pass that ends beginning of combat: nothing could attack
		case prev.Step == "declare-attackers" && st.Op == "attack" && len(st.Attackers) == 0:
			// the declaration of no attackers
		default:
			continue
		}
		w := attackWindow{Entry: k, Close: k, Passes: true}
		for w.Close < len(g.Snapshots) && g.Snapshots[w.Close].Step == "declare-attackers" && g.Snapshots[w.Close].Turn == gs.Turn {
			i := w.Close
			if len(g.Snapshots[i].Stack) != 0 || attacking(g.Snapshots[i]) || (i < len(steps) && steps[i].Op != "pass") {
				w.Passes = false
			}
			w.Close++
		}
		if w.Close < len(g.Snapshots) && g.Snapshots[w.Close].Step != "end-combat" {
			w.Passes = false // a declaration after all: not an empty step
		}
		// No skip to Close: a step entered by the pass ending beginning of combat can hold the
		// empty declaration that is the next window's entry.
		out = append(out, w)
	}
	return out
}

func attacking(s rules.OracleSnapshot) bool {
	for _, p := range s.Permanents {
		if p.Attacking {
			return true
		}
	}
	return false
}

// EmptyAttackPasses is the indices of the steps gorge plays as passes in an
// empty declare attackers step (attackWindows, Passes): follower-export hands
// them to a SkipsEmptyAttackStep driver (phase.rs), which steps over each one
// it reaches at end of combat, where its engine went without giving the step,
// instead of passing in the wrong step. nil when there are none.
func EmptyAttackPasses(steps []oraclegen.Step, g rules.OracleResult) []int {
	var out []int
	for _, w := range attackWindows(steps, g) {
		if !w.Passes {
			continue
		}
		for i := w.Entry; i < w.Close && i < len(steps); i++ {
			out = append(out, i)
		}
	}
	return out
}

// StepOver is what StepOverAttackWindows did to a follower row.
type StepOver struct {
	// Stepped is the number of empty declare attackers steps the follower
	// skipped and the comparison stepped over.
	Stepped int
	// Unproven is the entry checkpoint of the first skipped step the row
	// gives no evidence of having stepped over (a driver that passed on in
	// the wrong step), when every checkpoint before it agrees: the old
	// phase_skips_attack_step finding. 0 if none.
	Unproven int
	// Acted is the entry checkpoint of the first skipped declare attackers
	// step in which gorge did something besides pass (an activation, a
	// cast, an attack). It is left to compare, so it is a difference. 0 if
	// none.
	Acted int
}

// StepOverAttackWindows is x with the declare attackers steps the follower
// skipped stepped over: where gorge gives an empty declare attackers step
// (attackWindows) and the follower's row is already at end of combat at its
// entry checkpoint, the follower's checkpoints inside the step are replaced by
// gorge's, and the comparison resumes at the end of combat. That is sound
// only when the follower did nothing there either, so it is done only when
// every step gorge played in the window is a pass (Passes) and the row shows
// the driver stood still through them (each checkpoint in the window equals
// the entry's, the label aside: the driver steps over the passes
// follower-export marked, EmptyAttackPasses). The end of combat itself is
// compared as usual, and so is an entry gorge's item ends in, with only the
// step name set aside. A window gorge acted in is never stepped over: its
// first checkpoint differs, and that is a difference (Acted names it).
// Windows are taken in order; the first that is not stepped over ends the
// walk, since the row is out of step with gorge from there.
func StepOverAttackWindows(item oraclegen.Item, g rules.OracleResult, x oraclediff.XResult) (oraclediff.XResult, StepOver) {
	var so StepOver
	var snaps []rules.OracleSnapshot // x's, copied on the first change
	cur := func() []rules.OracleSnapshot {
		if snaps != nil {
			return snaps
		}
		return x.Snapshots
	}
	for _, w := range attackWindows(item.Steps, g) {
		k := w.Entry
		xs := cur()
		if k >= len(xs) || xs[k].Step != "end-combat" {
			if k < len(xs) && xs[k].Step == "declare-attackers" {
				continue // the follower gave the step too: nothing to step over
			}
			break
		}
		if !w.Passes {
			so.Acted = k
			break
		}
		last := min(w.Close, len(xs)) // x's checkpoints k..last-1 are in gorge's step
		still := true
		for i := k + 1; i < last; i++ {
			if !sameBut(xs[k], xs[i]) {
				still = false
				break
			}
		}
		// Unless the end of combat is compared (the window closes inside x),
		// the entry itself must agree with gorge's but for the step name.
		closes := w.Close < len(xs)
		entryAgrees := sameButStep(item, g.Snapshots[k], xs[k])
		if !still || (!closes && !entryAgrees) {
			if still || !agreeUpTo(item, g, xs, k) {
				break
			}
			so.Unproven = k
			break
		}
		if snaps == nil {
			snaps = append([]rules.OracleSnapshot(nil), x.Snapshots...)
		}
		copy(snaps[k:last], g.Snapshots[k:last])
		so.Stepped++
	}
	if snaps != nil {
		x.Snapshots = snaps
	}
	return x, so
}

// sameBut reports whether two snapshots are equal but for their checkpoint
// label.
func sameBut(a, b rules.OracleSnapshot) bool {
	a.Checkpoint, b.Checkpoint = "", ""
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// sameButStep reports whether the follower's snapshot xs agrees with gorge's
// gs once the step name is set aside, under the item's compare options.
func sameButStep(item oraclegen.Item, gs, xs rules.OracleSnapshot) bool {
	xs.Step = gs.Step
	return oraclediff.CompareOpts(rules.OracleResult{Snapshots: []rules.OracleSnapshot{gs}}, nil,
		oraclediff.XResult{Snapshots: []rules.OracleSnapshot{xs}}, item.Compare, item.Ignore...).Status == oraclediff.Agree
}

// agreeUpTo reports whether everything but the step name agrees up to and
// including checkpoint i.
func agreeUpTo(item oraclegen.Item, g rules.OracleResult, xs []rules.OracleSnapshot, i int) bool {
	upto := append([]rules.OracleSnapshot(nil), g.Snapshots[:i+1]...)
	upto[i].Step = xs[i].Step
	return oraclediff.CompareOpts(rules.OracleResult{Snapshots: upto}, nil,
		oraclediff.XResult{Snapshots: xs[:i+1]}, item.Compare, item.Ignore...).Status == oraclediff.Agree
}
