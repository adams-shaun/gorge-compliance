package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// copyAsk builds a copy's CR 707.10c new-target decision: CopyInherited
// leading keep-current options followed by the changed picks' refs. pickIdx
// are option indices into that list (0..n-1 keep the slot, >= n change it).
func copyAsk(seat, n int, pickIdx []int, pickRefs []string) rules.OracleDecision {
	return rules.OracleDecision{
		Step: 1, Seat: seat, Kind: "target", GorgeKind: "target", Resume: "copy_targets",
		CopyInherited: n, Options: n + len(pickRefs), Min: len(pickIdx), Max: len(pickIdx),
		PickIdx: pickIdx, Picks: append([]string(nil), pickRefs...),
		PickRefs: append([]string(nil), pickRefs...), Via: "target",
	}
}

// TestCopyRetargetOneTargetKeepScriptsNo: a copy whose sole inherited target is
// kept (gorge pick option 0) is XMage's "Change this 1 of 1 target?" answered
// no, with no target pick and no trailing skip.
func TestCopyRetargetOneTargetKeepScriptsNo(t *testing.T) {
	d := copyAsk(0, 1, []int{0}, []string{"p1"})
	// PRECONDITION: the decision really is the copy ask with one inherited
	// slot and a pick that stays inside it.
	if d.Resume != "copy_targets" || d.CopyInherited != 1 || d.PickIdx[0] >= d.CopyInherited {
		t.Fatalf("fixture is not a kept one-target copy ask: %+v", d)
	}
	want := []XAnswer{{0, "choice", "no"}}
	if got := routed(t, []rules.OracleDecision{d}, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("kept copy target: got %v want %v", got, want)
	}
}

// TestCopyRetargetOneTargetChangeScriptsYesThenTarget: a copy whose sole
// inherited target is replaced (gorge pick option 1) is XMage's chooseUse yes
// followed by one chooseTarget for the new object.
func TestCopyRetargetOneTargetChangeScriptsYesThenTarget(t *testing.T) {
	d := copyAsk(0, 1, []int{1}, []string{"p1:Grizzly Bears"})
	// PRECONDITION: the pick index is above the inherited count, so it really
	// is a change and not a keep.
	if d.CopyInherited != 1 || d.PickIdx[0] < d.CopyInherited {
		t.Fatalf("fixture is not a changed one-target copy ask: %+v", d)
	}
	want := []XAnswer{{0, "choice", "yes"}, {0, "target", "Grizzly Bears"}}
	if got := routed(t, []rules.OracleDecision{d}, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("changed copy target: got %v want %v", got, want)
	}
}

// TestCopyRetargetTwoSlotsOneKeptOneChanged: two inherited slots, the first
// kept and the second changed, emit one chooseUse per slot in slot order, the
// target pick only after the changed slot's yes.
func TestCopyRetargetTwoSlotsOneKeptOneChanged(t *testing.T) {
	d := copyAsk(0, 2, []int{0, 2}, []string{"p0:Old A", "p1:New B"})
	// PRECONDITION: two inherited slots; the first pick keeps slot 0 and the
	// second is above the count (slot 1 changed).
	if d.CopyInherited != 2 || d.PickIdx[0] != 0 || d.PickIdx[1] < d.CopyInherited {
		t.Fatalf("fixture is not keep-slot-0/change-slot-1: %+v", d)
	}
	want := []XAnswer{{0, "choice", "no"}, {0, "choice", "yes"}, {0, "target", "New B"}}
	if got := routed(t, []rules.OracleDecision{d}, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed copy target: got %v want %v", got, want)
	}
}

// TestCopyRetargetTwoSlotsBothChanged: two changed slots emit yes+target for
// each, in slot order.
func TestCopyRetargetTwoSlotsBothChanged(t *testing.T) {
	d := copyAsk(0, 2, []int{2, 3}, []string{"p0:New X", "p1:New Y"})
	// PRECONDITION: both picks are above the inherited count, so both slots
	// change.
	if d.CopyInherited != 2 || d.PickIdx[0] < d.CopyInherited || d.PickIdx[1] < d.CopyInherited {
		t.Fatalf("fixture is not two changed slots: %+v", d)
	}
	want := []XAnswer{
		{0, "choice", "yes"}, {0, "target", "New X"},
		{0, "choice", "yes"}, {0, "target", "New Y"},
	}
	if got := routed(t, []rules.OracleDecision{d}, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("two changed copy targets: got %v want %v", got, want)
	}
}

// TestCopyRetargetForcedSingleOptionIsScripted: a copy of a "target opponent"
// spell in a two-seat match offers one keep-current option (Min=Max=Options=1)
// yet XMage still asks the chooseUse, so forcedSingleOption must not skip it.
func TestCopyRetargetForcedSingleOptionIsScripted(t *testing.T) {
	d := copyAsk(0, 1, []int{0}, []string{"p1"})
	d.Options = 1
	d.Min, d.Max = 1, 1
	// PRECONDITION: this is the forced one-option copy shape the skip rule
	// exempts targets from; without the exemption nothing would be scripted.
	if forcedSingleOption(d) {
		t.Fatalf("forcedSingleOption unexpectedly claims a target ask: %+v", d)
	}
	if d.Options != 1 || d.Min != 1 || d.Max != 1 || d.Kind != "target" {
		t.Fatalf("fixture is not a forced one-option target ask: %+v", d)
	}
	want := []XAnswer{{0, "choice", "no"}}
	if got := routed(t, []rules.OracleDecision{d}, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("forced single-option copy ask: got %v want %v", got, want)
	}
}

// TestNonCopyTargetUnchangedByCopyRouting: a target decision whose Resume is
// not copy_targets keeps the ordinary pick-plus-skip output even when it
// carries a stray CopyInherited count, proving the routing keys on Resume.
func TestNonCopyTargetUnchangedByCopyRouting(t *testing.T) {
	d := copyAsk(0, 1, []int{1}, []string{"p1:Grizzly Bears"})
	d.Resume = "target"
	d.CopyInherited = 1
	d.Max = 2
	// PRECONDITION: not a copy ask, so the copy routing must not fire.
	if d.Resume == "copy_targets" {
		t.Fatalf("fixture is unexpectedly a copy ask: %+v", d)
	}
	want := []XAnswer{{0, "target", "Grizzly Bears"}, {0, "target", "[target_skip]"}}
	if got := routed(t, []rules.OracleDecision{d}, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("ordinary target ask changed: got %v want %v", got, want)
	}
}
