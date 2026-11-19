package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// A delayed effect's ability is named by Forge after an Effect object with an
// id; the id and the "'s Effect" tail are not part of the card's name.
func TestStackKeysDropsEffectObjectLabel(t *testing.T) {
	want := stackKeys([]rules.OracleSnapStack{{Kind: "ability", Source: "p0:Full Throttle"}})
	got := stackKeys([]rules.OracleSnapStack{{Kind: "ability", Source: "Full Throttle (8)'s Effect"}})
	if got != want || got != "[ability:Full Throttle@p0]" {
		t.Fatalf("got %q want %q", got, want)
	}
}

// An ability on the stack is labelled by its source's current name: a copy
// keeps the ref it was set up under but shows the copied name.
func TestStackKeysUsesSourcesCurrentName(t *testing.T) {
	perms := []rules.OracleSnapPerm{{Ref: "p0:Loan Shark", Name: "Kraum, Ludevic's Opus"}}
	g := stackKeysOf([]rules.OracleSnapStack{{Kind: "ability", Source: "p0:Loan Shark"}}, perms)
	x := stackKeys([]rules.OracleSnapStack{{Kind: "ability", Source: "Kraum, Ludevic's Opus"}})
	if g != x {
		t.Fatalf("gorge %q, follower %q", g, x)
	}
	// a spell has no permanent: its ref name stands
	if s := stackKeysOf([]rules.OracleSnapStack{{Kind: "spell", Source: "p0:Loan Shark"}}, perms); s != "[spell:Loan Shark@p0]" {
		t.Fatalf("spell %q", s)
	}
}

// An ability whose source has left the battlefield exists on its own (CR
// 113.7a) and is built from the source's last-known information (CR
// 603.10a): a Daring Thunder-Thief that died as a copy of Wanted Griffin is
// "p0:Daring Thunder-Thief" in gorge's snapshot, which keeps only the setup
// ref, and "Wanted Griffin" in Forge's. Neither is on the battlefield, so the
// two sides render the same string.
func TestStackKeysSourceLeftBattlefield(t *testing.T) {
	perms := []rules.OracleSnapPerm{{Ref: "p1:Wanted Griffin", Name: "Wanted Griffin"}}
	gs := rules.OracleSnapshot{Permanents: perms, Stack: []rules.OracleSnapStack{
		{Kind: "ability", Source: "p1:Wanted Griffin", Controller: 1},
		{Kind: "ability", Source: "p0:Daring Thunder-Thief", Controller: 0},
	}}
	// the follower's perms hold a Wanted Griffin too, so its label alone
	// cannot say that the p0 source is gone
	xs := rules.OracleSnapshot{Permanents: perms, Stack: []rules.OracleSnapStack{
		{Kind: "ability", Source: "Wanted Griffin", Controller: 1},
		{Kind: "ability", Source: "Wanted Griffin", Controller: 0},
	}}
	if a, b := stackKeysOf(gs.Stack, perms), stackKeysOf(xs.Stack, perms); a == b {
		t.Fatalf("unmasked labels already agree: %q", a)
	}
	g, x := pairedStackKeys(gs, xs)
	if g != x {
		t.Fatalf("gorge %q, follower %q", g, x)
	}
	if want := "[ability:Wanted Griffin@p1, ability:" + leftSourceLabel + "@p0]"; g != want {
		t.Fatalf("got %q want %q", g, want)
	}
	// a differing controller is still a divergence
	xs.Stack[1].Controller = 1
	if g, x := pairedStackKeys(gs, xs); g == x {
		t.Fatalf("controller difference hidden: %q", g)
	}
	// a source still on the battlefield keeps its name on both sides
	gs.Stack[1].Source = "p1:Wanted Griffin"
	xs.Stack[1].Controller = 0
	gs.Stack[1].Controller = 0
	xs.Stack[1].Source = "Other"
	if g, x := pairedStackKeys(gs, xs); g == x {
		t.Fatalf("name difference hidden for a live source: %q", g)
	}
}

// A sourceless designation beside a source that left the battlefield: the
// SUP-s148-supp shape. Both stand-ins are display values, so the two engines'
// stacks render the same and neither label is mistaken for a card.
func TestStackKeysLeftSourceBesideDesignation(t *testing.T) {
	g := rules.OracleSnapshot{Stack: []rules.OracleSnapStack{
		{Kind: "ability", Source: "p1:Brood of Cockroaches", Controller: 1},
		{Kind: "ability", Source: "", Controller: 0},
	}}
	x := rules.OracleSnapshot{Stack: []rules.OracleSnapStack{
		{Kind: "ability", Source: "Brood of Cockroaches", Controller: 1},
		{Kind: "ability", Source: "The Monarch", Controller: 0},
	}}
	gv, xv := pairedStackKeys(g, x)
	if gv != xv {
		t.Fatalf("gorge %q, xmage %q", gv, xv)
	}
	if want := "[ability:" + leftSourceLabel + "@p1, ability:@p0]"; gv != want {
		t.Fatalf("got %q want %q", gv, want)
	}
	if !IsLeftSourceLabel(leftSourceLabel) || IsLeftSourceLabel("The Monarch") || IsLeftSourceLabel("Brood of Cockroaches") {
		t.Fatal("IsLeftSourceLabel does not identify only the comparator stand-in")
	}
}
