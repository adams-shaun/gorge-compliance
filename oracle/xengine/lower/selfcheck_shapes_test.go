package lower

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// The three fixtures below are the xproof starts that produced the
// lower_gap:fail finds: the scripted position `xengine scen` writes, with the
// deck startDeck builds (hand, battlefield, then basics to 40), played at the
// seed search `xengine play -start` uses (seed 1 for the first game, the next
// starter-matching seed for the second).

func repeatCard(n int, name string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = name
	}
	return out
}

// startGameSeed is game n (1-based) of a start file whose active seat is
// active: gameSeeds' search, one starter-matching seed per game.
func startGameSeed(active, n int) uint64 {
	next := uint64(1)
	var seed uint64
	for k := 1; k <= n; k++ {
		seed = rules.SeedForStarter(next, 2, active)
		next = seed + 1
	}
	return seed
}

// spanShape plays one shape fixture and returns its lowered span, failing on a
// refusal or a self-check gap. Done means: every emitted step is one gorge's
// runner replays identically; a shape it cannot replay must be cut instead.
func spanShape(t *testing.T, reg *cards.Registry, pair []decks.Deck, start *rules.StartSpec, seed uint64, card, id, name string) Lowered {
	t.Helper()
	l, why, _, err := Span(reg, pair, seed, game.Options{
		Set: "XP", MaxTurns: 2, Start: start,
		Focus: &game.Focus{Seat: 0, Card: card, Kind: "cast"},
	}, false, id, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(why) > 0 {
		t.Fatalf("span refused: %v", why)
	}
	if c := CheckWindow(reg, l); c.Status != StatusOK {
		t.Fatalf("%s: %s at step %d, %s: gorge %s, tape %s %v", id, c.Status, c.Step, c.Field, c.Gorge, c.Tape, c.Fails)
	}
	return l
}

// TestSpanWeftwalkingETBShuffleSelfChecks: Weftwalking's ETB shuffles the
// hand and graveyard into the library and draws seven. The live game then plays
// Island#34 out of the drawn hand; the runner's seeded shuffle draws different
// Island objects, so the exact ref is not offered (lower_gap step 11). CR
// 701.24a: the shuffle's result is the engine's own, so the lowering must cut
// before the play rather than name the live object.
func TestSpanWeftwalkingETBShuffleSelfChecks(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair := []decks.Deck{
		{Name: "start-p0", Colours: "U", Cards: slices.Concat([]string{"Weftwalking"}, repeatCard(6, "Island"), repeatCard(33, "Island"))},
		{Name: "start-p1", Colours: "W", Cards: repeatCard(40, "Plains")},
	}
	start := &rules.StartSpec{Seats: []rules.StartSeat{
		{Hand: []int{0}, Battlefield: []int{1, 2, 3, 4, 5, 6}},
		{},
	}}
	l := spanShape(t, reg, pair, start, startGameSeed(0, 1), "Weftwalking",
		"xs:XP:weftwalking:f0t0:s1", "xproof start Weftwalking f0/t0 seed 1")
	// The shuffle moved the hand and graveyard in and then drew seven, so its
	// surplus does not match a pure library shuffle: the window's rng cut
	// stops it before the play. Either cut satisfies the brief (the item must
	// not carry the exact drawn object); accept both so the test does not pin
	// which guard fires first.
	shuffleAt := -1
	for i, e := range l.Against.Entries {
		if e.RNG > 0 && !rngAccounted(e) {
			shuffleAt = i
			break
		}
	}
	if shuffleAt < 0 {
		t.Fatalf("no unaccounted RNG entry in the lowered tape: the ETB shuffle did not lower")
	}
	switch l.Truncated {
	case ReasonRNG, unsupported("shuffled_ref").reason:
	default:
		t.Fatalf("truncated %q at %d, want %q (a random shuffle the runner cannot reproduce) or %q (the play after it)",
			l.Truncated, l.TruncatedAt, ReasonRNG, unsupported("shuffled_ref").reason)
	}
	// The write-up's failure is a later play naming Island#34, an object the
	// runner's seeded draw may not have put in hand. No play or cast after the
	// shuffle entry may survive.
	for i, st := range l.Item.Steps {
		if l.StepEntry[i] <= shuffleAt {
			continue
		}
		if st.Op == "play" || st.Op == "cast" {
			t.Fatalf("play/cast step %d (%s) survives after shuffle entry %d: the runner may not offer the live object", i, st.Card, shuffleAt)
		}
	}
}

// TestSpanPyrotechnicsDivide: Pyrotechnics divides 5 damage between both
// players. CR 601.2d keys the division by target, not by the live game's
// seat-facing label ("W-1"/"R-0"); the runner's players are a and b (`p0`,
// `p1`), so the lowered choose must name the seat. The self-check replays it.
func TestSpanPyrotechnicsDivide(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair := []decks.Deck{
		{Name: "start-p0", Colours: "R", Cards: slices.Concat([]string{"Pyrotechnics"}, repeatCard(39, "Mountain"))},
		{Name: "start-p1", Colours: "W", Cards: repeatCard(40, "Plains")},
	}
	start := &rules.StartSpec{Seats: []rules.StartSeat{
		{Hand: []int{0}, Battlefield: []int{1, 2, 3, 4, 5}},
		{},
	}}
	l := spanShape(t, reg, pair, start, startGameSeed(0, 1), "Pyrotechnics",
		"xs:XP:pyrotechnics:cast:s1", "xproof start Pyrotechnics cast seed 1")
	cast := -1
	for i, st := range l.Item.Steps {
		if st.Op == "cast" && st.Card == "p0:Pyrotechnics" {
			cast = i
			break
		}
	}
	if cast < 0 {
		t.Fatalf("no Pyrotechnics cast step in %+v", l.Item.Steps)
	}
	var divide []string
	for _, a := range l.Item.Steps[cast].Answers {
		if a.Kind == "choose" {
			divide = a.Pick
		}
	}
	if !slices.Equal(divide, []string{"p1", "p0"}) {
		t.Fatalf("divide picks %v, want the seats [p1 p0] (not the live labels W-1/R-0)", divide)
	}
}

// TestSpanHypnoticSpritePassChainSelfChecks: p1 refuses a Spree cast (Smuggler's
// Surprise) twice in a row at its priority. Each refused commit still reset the
// pass count gorge does not put in the digest. Dropping the two no-op casts
// while keeping the passes made the next p1 pass look like the second pass of
// the chain and advanced main1 early (lower_gap step 10: "p0 does not hold
// priority"). The lowering must cut at the first dropped action instead.
func TestSpanHypnoticSpritePassChainSelfChecks(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair := []decks.Deck{
		{Name: "start-p0", Colours: "U", Cards: slices.Concat([]string{"Hypnotic Sprite"}, repeatCard(39, "Island"))},
		{Name: "start-p1", Colours: "G", Cards: slices.Concat([]string{"Smuggler's Surprise", "Forest"}, repeatCard(38, "Forest"))},
	}
	start := &rules.StartSpec{Seats: []rules.StartSeat{
		{Hand: []int{0}, Battlefield: []int{1, 2}},
		{Hand: []int{0}, Battlefield: []int{1}},
	}}
	// Seed 2 is the repro (the tape records p1's refused casts); seed 1 has no
	// refused cast and lowers whole, so this is also the pass-chain guard.
	l := spanShape(t, reg, pair, start, startGameSeed(0, 2), "Hypnotic Sprite",
		"xs:XP:hypnotic-sprite:cast:s2", "xproof start Hypnotic Sprite cast seed 2")
	if want := unsupported("pass_chain").reason; l.Truncated != want {
		t.Fatalf("truncated %q at %d, want %q (the refused cast's pass reset cannot be dropped)", l.Truncated, l.TruncatedAt, want)
	}
}
