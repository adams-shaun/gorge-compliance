package classify

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestUsesCreditsTriggers: Llanowar Elves' mana tap credits f0/a0, and the
// ETB trigger of a creature that entered credits f0/t0.
func TestUsesCreditsTriggers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tp := &game.Tape{Entries: []game.Entry{
		{Kind: "priority", Mana: []game.ManaRec{{Name: "Llanowar Elves", Ability: 0}}},
		{Kind: "priority", Triggers: []game.Trig{{Name: "Elvish Visionary", Index: 0}}},
		{Kind: "priority", Triggers: []game.Trig{{Name: "Elvish Visionary", Index: 0}}},
	}}
	got := Uses(reg, tp, 0, 1, false)
	want := map[Use]int{{"Llanowar Elves", "f0/a0"}: 1, {"Elvish Visionary", "f0/t0"}: 1}
	if len(got) != len(want) {
		t.Fatalf("Uses = %v, want %v", got, want)
	}
	for u, n := range want {
		if got[u] != n {
			t.Errorf("Uses[%v] = %d, want %d (all: %v)", u, got[u], n, got)
		}
	}
	if all := Uses(reg, tp, 0, 2, false); all[Use{"Elvish Visionary", "f0/t0"}] != 2 {
		t.Errorf("both trigger entries: %v", all)
	}
}

// TestUsesCreditsBackFace: an MDFC back face's mana ability, an activated pick
// and a back-face trigger credit f1/aN and f1/tN, never f0.
func TestUsesCreditsBackFace(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tp := &game.Tape{Entries: []game.Entry{{
		Kind: "priority",
		Mana: []game.ManaRec{{Name: "Kazandu Valley", Card: "Kazandu Mammoth", Face: 1, Ability: 0}},
	}, {
		Kind:  "priority",
		Picks: []game.Pick{{Kind: "ability", Ref: "p0:Kazandu Valley", Card: "Kazandu Mammoth", Face: 1, Ability: 0}},
	}, {
		Kind:     "priority",
		Triggers: []game.Trig{{Name: "Kazandu Mammoth", Face: 0, Index: 0}},
	}}}
	got := Uses(reg, tp, 0, 2, false)
	if got[Use{"Kazandu Mammoth", "f1/a0"}] != 2 || got[Use{"Kazandu Mammoth", "f0/t0"}] != 1 || len(got) != 2 {
		t.Errorf("Uses = %v", got)
	}
}
