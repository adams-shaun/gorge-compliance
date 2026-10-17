package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// Gogo, Master of Mimicry's "{X}{X}, {T}" activation with CC offers exactly one
// X (1): the announcement is a one-option ask, but XMage's announceXMana is
// still posed, and an unscripted announce left the cost unpaid (the measured
// activate#0.0 pool divergence). It must not be dropped as a forced pick.
func TestSingleValueXAnnounceIsStillScripted(t *testing.T) {
	d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 1, Min: 1, Max: 1, Picks: []string{"X = 1"}, PickIdx: []int{0},
		PickRefs: []string{"X = 1"}, PickKinds: []string{"x"}, XMulti: true}
	if !forcedSingleOption(rules.OracleDecision{Kind: "choose_n", Options: 1, Min: 1, Max: 1, Picks: []string{"Wastes"}, PickIdx: []int{0}}) {
		t.Fatal("precondition: a plain one-option ask must still be forced")
	}
	if forcedSingleOption(d) {
		t.Fatal("a one-value X announcement is classified as a forced ask")
	}
	got := routed(t, []rules.OracleDecision{d}, 3)
	want := []XAnswer{{0, "choice", "X=1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routed = %#v, want %#v", got, want)
	}
}

// Bolt Bend's ChangeTargets redirect is a gorge choose ask but XMage's
// chooseTarget: the new target must ride the target queue, or the original
// target survives (the measured static#0.0 divergence).
func TestRetargetRedirectIsAScriptedTarget(t *testing.T) {
	ask := rules.OracleDecision{Step: 2, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "choice",
		Options: 2, Min: 1, Max: 1, Picks: []string{""}, PickIdx: []int{0},
		PickRefs: []string{"p0:Nessian Asp"}, PickKinds: []string{"card"},
		OptionRefs: []string{"p0:Nessian Asp", "p1:Grizzly Bears"}}
	// Control: the same shape WITHOUT the redirect marker stays a choice, so
	// the marker is what routes it and the assertion below can fail.
	if got, want := routed(t, []rules.OracleDecision{ask}, 3), []XAnswer{{0, "choice", "Nessian Asp"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unmarked routed = %#v, want %#v", got, want)
	}
	ask.Retarget = true
	got := routed(t, []rules.OracleDecision{ask}, 3)
	want := []XAnswer{{0, "target", "Nessian Asp"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retarget routed = %#v, want %#v", got, want)
	}
}

// A forced X of an single-X cost (Rottenmouth Viper's X = 0, Saheeli's
// Lattice's X = 1) is not scripted: the committed rows agree unscripted, so the
// repeated-X fix must leave them forced.
func TestForcedSingleXAnnounceStaysUnscripted(t *testing.T) {
	d := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 1, Min: 1, Max: 1, Picks: []string{"X = 0"}, PickIdx: []int{0},
		PickRefs: []string{"X = 0"}, PickKinds: []string{"x"}}
	if !forcedSingleOption(d) {
		t.Fatal("a forced X = 0 announcement must stay a forced (unscripted) ask")
	}
	d.Picks, d.PickRefs = []string{"X = 1"}, []string{"X = 1"}
	if !forcedSingleOption(d) {
		t.Fatal("a forced X = 1 of an single-X cost must stay a forced (unscripted) ask")
	}
	d.XMulti = true
	if forcedSingleOption(d) {
		t.Fatal("precondition: the same ask with repeated X symbols must be scripted")
	}
}
