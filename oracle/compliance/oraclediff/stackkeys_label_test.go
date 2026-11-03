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
