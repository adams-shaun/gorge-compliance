package lower

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestLowerRandomPickCounterIsCut: Crystalline Giant's beginning-of-combat
// trigger picks a counter kind at random (RandomType$ True). The pick lands on
// the board, so a window whose runner draws its own value hands the self-check
// a different kind and reports a lower_gap. The window must instead stop
// before the entry that made the pick: no step at or after it, and the
// self-check agrees with every step that remains.
func TestLowerRandomPickCounterIsCut(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d0 := decks.Deck{Name: "start-p0", Colours: "G", Cards: []string{"Crystalline Giant", "Forest", "Forest", "Forest", "Forest", "Forest"}}
	for len(d0.Cards) < 40 {
		d0.Cards = append(d0.Cards, "Forest")
	}
	d1 := decks.Deck{Name: "start-p1", Colours: "W"}
	for len(d1.Cards) < 40 {
		d1.Cards = append(d1.Cards, "Plains")
	}
	// Seat 0 starts with the Giant and five Forests; the rest of its deck is
	// the library. Seed 2 makes seat 0 the starting player, so the Giant's
	// "on your turn" trigger resolves inside turn 1's window.
	start := &rules.StartSpec{Seats: []rules.StartSeat{
		{Battlefield: []int{0, 1, 2, 3, 4, 5}, Library: []int{6, 7, 8, 9}},
		{},
	}}
	pair := []decks.Deck{d0, d1}
	l, why, tp, err := Span(reg, pair, rules.SeedForStarter(2, 2, 0),
		game.Options{Set: "IKO", MaxTurns: 2, Start: start}, false,
		"xs:IKO:crystalline-giant:f0t0:s1", "xproof start Crystalline Giant f0/t0 seed 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(why) > 0 {
		t.Fatalf("span refused: %v", why)
	}
	pick := -1
	for i, e := range tp.Entries {
		if e.RNG > 0 && !rngAccounted(e) {
			pick = i
			break
		}
	}
	if pick < 0 {
		t.Fatal("the scripted start made no unpinned random draw")
	}
	if pick+1 >= len(tp.Entries) {
		t.Fatalf("the pick is the tape's last entry %d", pick)
	}
	// The draw chose a counter kind: its state change is on the battlefield,
	// not only in the library order HasRNG leaves out.
	diff := tp.Entries[pick].Digest.Diff(tp.Entries[pick+1].Digest)
	if !slices.ContainsFunc(diff, func(s string) bool { return strings.Contains(s, ".counters.") }) {
		t.Fatalf("pick entry %d diff %v: want a counter kind on the board", pick, diff)
	}
	// Lowered.TruncatedAt and StepEntry index the window's rebased tape
	// (l.Against), not the whole game, so map the pick onto it by sequence.
	rel := -1
	for i, e := range l.Against.Entries {
		if e.Seq == tp.Entries[pick].Seq {
			rel = i
			break
		}
	}
	if rel < 0 {
		t.Fatalf("pick entry %d is not in the lowered tape", pick)
	}
	if l.Truncated != ReasonRNG || l.TruncatedAt != rel {
		t.Fatalf("truncated %q at %d, want %q at %d", l.Truncated, l.TruncatedAt, ReasonRNG, rel)
	}
	if l.HasRNG {
		t.Errorf("HasRNG is set: a non-shuffle draw must be cut, not only left out of library_top")
	}
	for i, se := range l.StepEntry {
		if se >= rel {
			t.Errorf("step %d still carries entry %d, at or after the pick %d", i, se, rel)
		}
	}
	if len(l.Item.Steps) == 0 {
		t.Fatal("every step was cut")
	}
	if c := CheckWindow(reg, l); c.Status != StatusOK {
		t.Errorf("check: %s at step %d, %s: gorge %s, tape %s", c.Status, c.Step, c.Field, c.Gorge, c.Tape)
	}
}

// TestRNGShuffleDraw: a surplus draw is a library shuffle only when it matches
// a Fisher-Yates pass (len(library)-1 values) over a library whose top the
// entry moved. Every other surplus -- one draw for a random counter kind or
// discard, a shuffle plus a pick, a shuffle whose post-pass draw breaks the
// count -- is a draw the lowering must cut, never carry.
func TestRNGShuffleDraw(t *testing.T) {
	seat := func(count int, top ...string) game.PlayerDigest {
		return game.PlayerDigest{LibraryCount: count, LibraryTop: top}
	}
	dig := func(ps ...game.PlayerDigest) game.Digest { return game.Digest{Players: ps} }
	before := dig(seat(34, "Forest", "Forest", "Forest"), seat(40, "Plains"))
	cases := []struct {
		name  string
		e     game.Entry
		after game.Digest
		want  bool
	}{
		{"one library shuffle", game.Entry{RNG: 33, Digest: before},
			dig(seat(34, "Forest", "Mountain", "Forest"), seat(40, "Plains")), true},
		{"shuffle and a counted die", game.Entry{RNG: 34, Rolls: []game.Roll{{Seat: 0, Sides: 6}},
			Digest: before}, dig(seat(34, "Mountain", "Forest"), seat(40, "Plains")), true},
		{"doubled flip plus shuffle", game.Entry{RNG: 35, Flips: []game.Flip{{Seat: 0, Draws: 2}},
			Digest: before}, dig(seat(34, "Mountain", "Forest"), seat(40, "Plains")), true},
		{"forced flip draws nothing", game.Entry{RNG: 33, Flips: []game.Flip{{Seat: 0, Forced: true}},
			Digest: before}, dig(seat(34, "Mountain", "Forest"), seat(40, "Plains")), true},
		// Crystalline Giant: one draw for the counter kind.
		{"random pick", game.Entry{RNG: 1, Digest: before},
			dig(seat(34, "Forest", "Forest", "Forest"), seat(40, "Plains")), false},
		// A one-draw pick at a two-card library has the shuffle's count, but it
		// did not move the top: the outcome is the pick, not an order.
		{"pick at a two-card library", game.Entry{RNG: 1, Digest: dig(seat(2, "Forest", "Forest"))},
			dig(seat(2, "Forest", "Forest")), false},
		// A shuffle plus a random pick spends one draw more than the pass.
		{"shuffle plus pick", game.Entry{RNG: 34, Digest: before},
			dig(seat(34, "Mountain", "Forest"), seat(40, "Plains")), false},
		// A shuffle that draws a card after its pass leaves one fewer than the
		// count names, so it is cut too (the safe direction).
		{"shuffle then draw", game.Entry{RNG: 33, Digest: dig(seat(34, "Forest", "Forest"))},
			dig(seat(33, "Mountain", "Forest")), false},
	}
	for _, c := range cases {
		if got := rngShuffle(c.e, c.after); got != c.want {
			t.Errorf("%s: rngShuffle = %v, want %v", c.name, got, c.want)
		}
	}
}
