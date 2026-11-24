package lower

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestSelfCheckPhasedOutPermanent: Blink Dog's Teleport ability phases it out
// (CR 702.26b). The live tape digest must drop it the way gorge's oracle
// snapshot does, so a lowered span that activates the ability and then crosses
// the phase-out replays with no phantom field.len gap. Before the digest
// omitted phased-out permanents this span self-checked as lower_gap:field.len
// at the first checkpoint after the activation.
func TestSelfCheckPhasedOutPermanent(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair := []decks.Deck{
		{Name: "start-p0", Colours: "W", Cards: slices.Concat([]string{"Blink Dog"}, repeatCard(39, "Plains"))},
		{Name: "start-p1", Colours: "W", Cards: repeatCard(40, "Plains")},
	}
	start := &rules.StartSpec{Seats: []rules.StartSeat{
		{Battlefield: []int{0, 1, 2, 3, 4, 5, 6}},
		{},
	}}
	l, why, _, err := Span(reg, pair, startGameSeed(0, 1), game.Options{
		Set: "XP", MaxTurns: 2, Start: start,
		Focus: &game.Focus{Seat: 0, Card: "Blink Dog", Kind: "ability"},
	}, false, "xs:XP:blink-dog:phase-out:s1", "xproof start Blink Dog Teleport seed 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(why) > 0 {
		t.Fatalf("span refused: %v", why)
	}
	hasDog := func(d game.Digest) bool {
		for _, p := range d.Field {
			if p.Name == "Blink Dog" {
				return true
			}
		}
		return false
	}
	act := -1
	for i, st := range l.Item.Steps {
		if st.Op == "activate" && st.Card == "p0:Blink Dog" {
			act = i
			break
		}
	}
	if act < 0 {
		t.Fatalf("no Blink Dog activation step in %+v", l.Item.Steps)
	}
	before := l.StepEntry[act]
	if before < 0 || before >= len(l.Against.Entries) || !hasDog(l.Against.Entries[before].Digest) {
		t.Fatalf("Blink Dog is not on the battlefield at its activation entry %d", before)
	}
	// The activation's payment and resolution take their own entries, so the
	// phase-out shows up at the first later digest that no longer names it.
	after := before + 1
	for after < len(l.Against.Entries) && hasDog(l.Against.Entries[after].Digest) {
		after++
	}
	if after >= len(l.Against.Entries) {
		t.Fatalf("tape never drops the phased-out Blink Dog after its activation entry %d", before)
	}
	if c := CheckWindow(reg, l); c.Status != StatusOK {
		t.Fatalf("%s: %s at step %d, %s: gorge %s, tape %s %v", l.Item.ID, c.Status, c.Step, c.Field, c.Gorge, c.Tape, c.Fails)
	}
}
