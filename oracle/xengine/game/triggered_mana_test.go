package game

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// nirkanaPair is the xproof fixture: p0 has Nirkana Revenant and two Swamps on
// the battlefield (deck indices 0..2) and Walking Corpse in hand (3), p1
// plays Plains. Tapping a Swamp triggers Nirkana's extra {B} (CR 605.1b).
func nirkanaPair() ([2]decks.Deck, *rules.StartSpec) {
	d0 := decks.Deck{Name: "p0", Colours: "B", Cards: []string{"Nirkana Revenant", "Swamp", "Swamp", "Walking Corpse"}}
	for len(d0.Cards) < 40 {
		d0.Cards = append(d0.Cards, "Swamp")
	}
	d1 := decks.Deck{Name: "p1", Colours: "W"}
	for len(d1.Cards) < 40 {
		d1.Cards = append(d1.Cards, "Plains")
	}
	return [2]decks.Deck{d0, d1}, &rules.StartSpec{Seats: []rules.StartSeat{{Battlefield: []int{0, 1, 2}, Hand: []int{3}}, {}}}
}

// nirkanaTape plays the Nirkana fixture to turn 3.
func nirkanaTape(t *testing.T) *Tape {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	pair, start := nirkanaPair()
	return play(t, reg, pair, rules.SeedForStarter(1, 2, 0), Options{Set: "BFZ", MaxTurns: 3, Start: start})
}

// TestRecorderTriggeredManaRec: the hook is called for a triggered mana
// ability as well as for an activation (CR 605.1b), and the SA it passes is
// the trigger's effect body, which is no entry of Face().Abilities. The
// recorder must mark that record Triggered and leave an activated record (the
// Swamp's own tap, Ability 0) unmarked.
func TestRecorderTriggeredManaRec(t *testing.T) {
	tp := nirkanaTape(t)
	var activated, triggered int
	for i, e := range tp.Entries {
		for _, mr := range e.Mana {
			switch {
			case mr.Triggered:
				triggered++
				if mr.Name != "Nirkana Revenant" || mr.Ability != -1 {
					t.Errorf("entry %d: triggered rec = %+v, want Nirkana Revenant, Ability -1", i, mr)
				}
			case mr.Name == "Swamp":
				activated++
				if mr.Ability != 0 {
					t.Errorf("entry %d: Swamp activation = %+v, want Ability 0", i, mr)
				}
			}
		}
	}
	if activated == 0 || triggered == 0 {
		t.Fatalf("%d Swamp activations, %d triggered records: the fixture taps no Swamp under Nirkana Revenant", activated, triggered)
	}
	if triggered != activated {
		t.Errorf("%d triggered records for %d Swamp taps, want one each", triggered, activated)
	}
}
