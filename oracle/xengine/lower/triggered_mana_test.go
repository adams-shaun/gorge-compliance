package lower

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestLowerTriggeredManaAbility: tapping a Swamp under Nirkana Revenant fires
// the triggered mana ability "add an additional {B}" (CR 605.1b), which the
// recorder marks Triggered. It resolves with the tap (CR 605.4a), so the lowering
// emits the Swamp's activate step and none for the trigger, does not truncate
// the window as unsupported_kind:mana_ability, and self-checks OK.
func TestLowerTriggeredManaAbility(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d0 := decks.Deck{Name: "p0", Colours: "B", Cards: []string{"Nirkana Revenant", "Swamp", "Swamp", "Walking Corpse"}}
	d1 := decks.Deck{Name: "p1", Colours: "W"}
	for len(d0.Cards) < 40 {
		d0.Cards = append(d0.Cards, "Swamp")
	}
	for len(d1.Cards) < 40 {
		d1.Cards = append(d1.Cards, "Plains")
	}
	start := &rules.StartSpec{Seats: []rules.StartSeat{{Battlefield: []int{0, 1, 2}, Hand: []int{3}}, {}}}
	pair := []decks.Deck{d0, d1}
	l, why, tp, err := Span(reg, pair, rules.SeedForStarter(1, 2, 0), game.Options{Set: "BFZ", MaxTurns: 3, Start: start}, false,
		"xs:BFZ:nirkana-revenant:f0a0:s1", "xproof start Nirkana Revenant seed 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(why) > 0 {
		t.Fatalf("span refused: %v", why)
	}
	var taps, trig int
	for _, e := range tp.Entries {
		for _, mr := range e.Mana {
			switch {
			case mr.Triggered:
				trig++
			case mr.Name == "Swamp":
				taps++
			}
		}
	}
	if taps == 0 || trig == 0 {
		t.Fatalf("%d Swamp taps, %d triggered records: nothing to lower", taps, trig)
	}
	if l.Truncated != "" && l.Truncated != "stall" {
		t.Fatalf("truncated %s at entry %d, want no truncation", l.Truncated, l.TruncatedAt)
	}
	if c := CheckWindow(reg, l); c.Status != StatusOK {
		t.Fatalf("CheckWindow: %+v", c)
	}
	// One activate step per Swamp tap and none for the trigger. (The bot's
	// other activations, Nirkana's own {B} pump, are real activations.)
	var swamp int
	for _, st := range l.Item.Steps {
		if st.Op == "activate" && refName(st.Card) == "Swamp" {
			swamp++
		}
	}
	if swamp != taps {
		t.Errorf("%d Swamp activate steps, want one per tap (%d)", swamp, taps)
	}

	// An activated mana record the lowering cannot resolve stays refused.
	steps, bad := manaTaps(reg, seatMap{}, []game.ManaRec{{Name: "Swamp", Ability: -1, Ref: "p0:Swamp"}}, nil)
	if bad == nil || bad.reason != "unsupported_kind:mana_ability" || steps != nil {
		t.Errorf("unresolvable activated record: steps %v, why %v, want unsupported_kind:mana_ability", steps, bad)
	}
}
