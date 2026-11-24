package certdeck

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestCounterNeedWildcards: a Forge CounterType$ All (Honest Work) or Any
// (Mabel) names every kind, not a kind, so the need is the commonest kind
// P1P1 (CR 122.1) rather than the literal wildcard the followers refuse.
func TestCounterNeedWildcards(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	types := CreatureTypes(reg)
	for _, name := range []string{"Honest Work", "Mabel, Bitter Recluse"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s is not in the corpus", name)
		}
		var got []string
		for _, n := range Derive(c, types) {
			if n.Class == Counters {
				got = append(got, n.Arg)
			}
		}
		if len(got) != 1 || got[0] != "P1P1" {
			t.Errorf("%s counter needs = %v, want one P1P1", name, got)
		}
	}
	// The whole corpus, not just the two cards: no other CounterType$ value
	// reaches a need as a wildcard or an empty kind.
	for _, c := range reg.AllCards() {
		for _, n := range Derive(c, types) {
			if n.Class != Counters {
				continue
			}
			switch strings.ToLower(n.Arg) {
			case "", "all", "any", "eachfromsource", "existingcounter":
				t.Errorf("%s: counter need %q is not a kind", c.Faces[0].Name, n.Arg)
			}
		}
	}
}

// TestCounterSeat: the placed counter follows the ability that reads it. An
// Aura enchanting an opponent's creature (Honest Work), a target that is not
// yours (Mabel, Blitz Leech) and a generic target (Vampire Hexmage) put it on
// the opponent's seat; an Aura enchanting any creature, a target of yours
// (Hapatra's Mark), a move from your own creatures (Slippery Bogbonder) and a
// replacement (Winding Constrictor) keep it on the card's own.
func TestCounterSeat(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name string
		seat int
	}{
		{"Honest Work", 1},
		{"Mabel, Bitter Recluse", 1},
		{"Blitz Leech", 1},
		{"Vampire Hexmage", 1},
		{"Rikku, Resourceful Guardian", 1},
		{"Enchanted River's Grasp", 0},
		{"Hapatra's Mark", 0},
		{"Slippery Bogbonder", 0},
		{"Afiya Grove", 0},
		{"Explorer's Cache", 0},
		{"Black Panther, Wakandan King", 0},
		{"Nesting Grounds", 0},
		{"Winding Constrictor", 0},
		{"Badgermole", 0},
	} {
		c, ok := reg.Lookup(tc.name)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("%s is not in the corpus", tc.name)
		}
		if got := CounterSeat(c); got != tc.seat {
			t.Errorf("CounterSeat(%s) = %d, want %d", tc.name, got, tc.seat)
		}
	}
}
