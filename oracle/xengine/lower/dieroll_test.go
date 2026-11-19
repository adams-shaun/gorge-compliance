package lower

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
)

// TestLowerDieRollResult: a die roll recorded on a tape entry lowers into the
// item on the step that made it. The tape carries both the natural and the
// modified result; a follower must force the natural roll, so the item keeps
// both and the step index is the scenario's, not the tape entry's.
func TestLowerDieRollResult(t *testing.T) {
	d := game.Digest{Turn: 1, Step: "main1", Active: 0, Priority: 0}
	tp := &game.Tape{
		Format: game.TapeFormat, Starter: 0,
		Entries: []game.Entry{
			{Kind: "priority", Seat: 0, Turn: 1, Step: "main1", Digest: d,
				Picks: []game.Pick{{Kind: "pass", Player: 0}}},
			{Kind: "priority", Seat: 0, Turn: 1, Step: "main1", Digest: d,
				Picks: []game.Pick{{Kind: "ability", Player: 0, Obj: stateID(42), Ref: "p0:Ebony Fly", Ability: 0,
					Label: "Ebony Fly: Roll a d6."}},
				Rolls: []game.Roll{{Seat: 0, Sides: 6, Natural: 3, Result: 3}}},
			{Kind: "priority", Seat: 1, Turn: 1, Step: "main1", Digest: d,
				Picks: []game.Pick{{Kind: "pass", Player: 1}}},
		},
	}
	l, err := Tape(nil, tp)
	if err != nil {
		t.Fatal(err)
	}
	if l.Truncated != "" {
		t.Fatalf("truncated %q at %d", l.Truncated, l.TruncatedAt)
	}
	want := []oraclegen.DieRoll{{Step: 1, Seat: 0, Sides: 6, Natural: 3, Result: 3}}
	if !reflect.DeepEqual(l.Item.DieRolls, want) {
		t.Fatalf("die rolls %+v, want %+v (steps %+v)", l.Item.DieRolls, want, ops(l))
	}
	if len(l.Item.Steps) != 3 || l.Item.Steps[1].Op != "activate" {
		t.Fatalf("steps %+v, want the roll on an activate step", l.Item.Steps)
	}
}

// TestRNGDrawsAccounted: one die roll or coin flip is exactly one engine
// draw (gorge's rng.IntN), except a flip a FlipCoinMod static forced heads:
// it emits the same Note but consumes no draw, so the gate must count only
// the flips marked Forced=false. An entry that also shuffled -- Wand of
// Wonder's DBShuffle, Wild Endeavor's search and shuffle -- spends more draws
// than it records outcomes, so the seeded runner cannot reproduce it. The
// gate must count every draw, not just look for a recorded roll; all three
// call sites share rngAccounted so the next one cannot drift.
func TestRNGDrawsAccounted(t *testing.T) {
	roll := game.Roll{Seat: 0, Sides: 6, Natural: 3, Result: 3}
	flip := game.Flip{Seat: 0, Win: true}
	forced := game.Flip{Seat: 0, Win: true, Forced: true}
	for _, c := range []struct {
		name string
		e    game.Entry
		want bool
	}{
		{"no draws", game.Entry{}, true},
		{"one die", game.Entry{RNG: 1, Rolls: []game.Roll{roll}}, true},
		{"one flip", game.Entry{RNG: 1, Flips: []game.Flip{flip}}, true},
		{"forced flip alone", game.Entry{RNG: 0, Flips: []game.Flip{forced}}, true},
		{"die and flip", game.Entry{RNG: 2, Rolls: []game.Roll{roll}, Flips: []game.Flip{flip}}, true},
		{"die and forced flip", game.Entry{RNG: 1, Rolls: []game.Roll{roll}, Flips: []game.Flip{forced}}, true},
		{"shuffle plus die", game.Entry{RNG: 7, Rolls: []game.Roll{roll}}, false},
		{"shuffle plus flip", game.Entry{RNG: 7, Flips: []game.Flip{flip}}, false},
		// The finding's case: the flip was forced so the entry's one draw is
		// the shuffle, not the flip. Counting len(Flips) would accept it.
		{"shuffle plus forced flip", game.Entry{RNG: 1, Flips: []game.Flip{forced}}, false},
		{"shuffle only", game.Entry{RNG: 7}, false},
	} {
		if got := rngAccounted(c.e); got != c.want {
			t.Errorf("%s: rngAccounted(%+v) = %v, want %v", c.name, c.e, got, c.want)
		}
	}

	// answersOf refuses the shuffle-plus-roll entry and accepts the die-only
	// one: the roll explains the single draw, so the tape can force it.
	bad := game.Entry{Kind: "choose", Seat: 0, RNG: 7, Rolls: []game.Roll{roll}}
	if _, why := answersOf(seatMap{}, []game.Entry{bad}); why == nil || why.reason != ReasonUnsupported+"rng" {
		t.Fatalf("shuffle plus roll: why = %v, want %s", why, ReasonUnsupported+"rng")
	}
	good := game.Entry{Kind: "choose", Seat: 0, RNG: 1, Rolls: []game.Roll{roll}}
	if _, why := answersOf(seatMap{}, []game.Entry{good}); why != nil {
		t.Fatalf("die-only entry refused: %v", why)
	}

	// resolveSpanLowerable is the second gate: a resolved span containing a
	// shuffle-plus-roll entry is not lowerable, while the same span with the
	// roll alone is.
	if resolveSpanLowerable([]game.Entry{bad}, 0, 1) {
		t.Error("resolve span with a shuffle plus a roll is lowerable")
	}
	if !resolveSpanLowerable([]game.Entry{good}, 0, 1) {
		t.Error("resolve span with only a forced roll is not lowerable")
	}
}
