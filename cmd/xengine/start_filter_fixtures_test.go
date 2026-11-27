package main

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
)

// TestStartFilterFixtureKeysFires plays the start files the :filter fixture
// lanes (xproof top-owed no-generator:trigger:<mode>:filter, census 11-27)
// now generate and reads the tape: the trigger the key names actually reached
// the stack in the played game. The scen package's structural tests pin the
// file's shape (focus, targets, attach, counters); this proves the engine
// honours those placements, one row per fixture shape:
//
//   - a land the filter takes dies to the opponent's Stone Rain (Akki Raider);
//   - a countered creature an opponent controls dies to Murder, the
//     HasCounters gate placed (Village Pillagers);
//   - a creature the filter takes enters as a fixture cast (a Detective for
//     Case of the Pilfered Proof, an Equipment for Barret, a Cartouche Aura
//     cast attached at its bearer for Trial of Ambition);
//   - a creature you cast this turn enters, the cast itself making the
//     wasCastByYou history (The Sibsig Ceremony);
//   - the Aura's bearer dies to Murder while enchanted (Necrotic Plague);
//   - an Aura that shrinks its bearer (+2/-1) is placed on one that survives
//     it, so the bearer dies to Murder and not to setup (Infectious Rage);
//   - a countered creature dies, the gate placed (The Ozolith);
//   - a counter-gated creature attacks, its +1/+1 counter placed (Elite
//     Scaleguard);
//   - the Aura's attached bearer blocks and is dealt damage (Cracked Skull);
//   - a flying carrier blocks the flying attacker the fixture cannot pair with
//     (Gustcloak Harrier).
func TestStartFilterFixtureKeysFires(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Akki Raider", "f0/t0"},
		{"Village Pillagers", "f0/t1"},
		{"Case of the Pilfered Proof", "f0/t0"},
		{"Barret, Avalanche Leader", "f0/t0"},
		{"Trial of Ambition", "f0/t1"},
		{"The Sibsig Ceremony", "f0/t0"},
		{"Necrotic Plague", "f0/t0"},
		{"Infectious Rage", "f0/t0"},
		{"The Ozolith", "f0/t0"},
		{"Elite Scaleguard", "f0/t1"},
		{"Cracked Skull", "f0/t1"},
		{"Gustcloak Harrier", "f0/t0"},
	} {
		t.Run(c.card, func(t *testing.T) {
			uses := startTapeUses(t, reg, b, c.card, c.key)
			if uses[classify.Use{Card: c.card, Key: c.key}] == 0 {
				t.Errorf("%s %s: the played tape never put the trigger on the stack; keys %v", c.card, c.key, uses)
			}
		})
	}
}
