package lower

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
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

// TestRNGDrawsAccounted: one die roll or ordinary coin flip is exactly one
// engine draw (gorge's rng.IntN), except a flip a FlipCoinMod static forced
// heads (it emits the same Note but consumes no draw), and a flip under a
// FlipCoinDoubler static (Krark's Thumb) draws twice for one instruction.
// The gate must use each flip's recorded draw count, not just len(Flips). An
// entry that also shuffled -- Wand of Wonder's DBShuffle, Wild Endeavor's
// search and shuffle -- spends more draws than it records outcomes, so the
// seeded runner cannot reproduce it. All three call sites share rngAccounted
// so the next one cannot drift.
func TestRNGDrawsAccounted(t *testing.T) {
	roll := game.Roll{Seat: 0, Sides: 6, Natural: 3, Result: 3}
	flip := game.Flip{Seat: 0, Win: true}
	forced := game.Flip{Seat: 0, Win: true, Forced: true}
	doubled := game.Flip{Seat: 0, Win: true, Draws: 2}
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
		// Krark's Thumb: one instruction, two draws. The old one-draw count
		// refused this entry, which the live game can reproduce (the follower
		// runs the same doubler static).
		{"doubled flip", game.Entry{RNG: 2, Flips: []game.Flip{doubled}}, true},
		{"doubled flip plus die", game.Entry{RNG: 3, Rolls: []game.Roll{roll}, Flips: []game.Flip{doubled}}, true},
		{"shuffle plus doubled flip", game.Entry{RNG: 3, Flips: []game.Flip{doubled}}, false},
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

// TestLowerDieRollOnResolvingPass: Herald of Hadar's "{5}{B}: Roll a d20" is
// an activation funded by six mana-ability taps, and the d20 is made when the
// ability resolves, on the pass that lets it. The roll sits on that later pass
// entry, so the item's die_rolls must name the pass step (the first step whose
// entry reaches the roll), not the activation and not a tap: the follower
// forces the natural roll on the step it resolves in. A roll that lands on the
// wrong step (or none) leaves the followers rolling their own d20, which is
// the +2 life both of them showed against gorge on the Herald window.
func TestLowerDieRollOnResolvingPass(t *testing.T) {
	const ref = "p0:Herald of Hadar"
	obj := stateID(42)
	d := game.Digest{Turn: 1, Step: "main1", Active: 0, Priority: 0}
	var taps []game.ManaRec
	for i := 0; i < 6; i++ {
		taps = append(taps, game.ManaRec{Obj: stateID(uint32(10 + i)), Name: "Swamp", Ability: 0, Ref: "p0:Swamp"})
	}
	// A distinct digest after the activation so the cast is not dropped as a
	// no-op: the stack top is the ability.
	d2 := d
	d2.Priority = 1
	tp := &game.Tape{
		Format: game.TapeFormat, Starter: 0,
		Entries: []game.Entry{
			{Kind: "priority", Seat: 0, Turn: 1, Step: "main1", Digest: d,
				Picks: []game.Pick{{Kind: "pass", Player: 0}}},
			{Kind: "priority", Seat: 0, Turn: 1, Step: "main1", Digest: d,
				Picks: []game.Pick{{Kind: "ability", Player: 0, Obj: obj, Ref: ref, Ability: 0,
					Label: "Herald of Hadar: Roll a d20."}},
				Mana: taps, Spent: "BBBBBB"},
			{Kind: "priority", Seat: 0, Turn: 1, Step: "main1", Digest: d2, StackTop: stateID(77),
				Picks: []game.Pick{{Kind: "pass", Player: 0}}},
			{Kind: "priority", Seat: 1, Turn: 1, Step: "main1", Digest: d2, StackTop: stateID(77),
				Picks: []game.Pick{{Kind: "pass", Player: 1}},
				RNG:   1, Rolls: []game.Roll{{Seat: 0, Sides: 20, Natural: 7, Result: 7}}},
			{Kind: "priority", Seat: 0, Turn: 1, Step: "main1", Digest: d,
				Picks: []game.Pick{{Kind: "pass", Player: 0}}},
		},
	}
	l, err := Tape(nil, tp)
	if err != nil {
		t.Fatal(err)
	}
	if l.Truncated != "" {
		t.Fatalf("truncated %q at %d", l.Truncated, l.TruncatedAt)
	}
	// Steps: pass, six taps, activate, pass (p0), pass (p1, the roll), pass.
	var pass1 int
	for k, st := range l.Item.Steps {
		if k > 7 && st.Op == "pass" && st.Seat == 1 {
			pass1 = k
		}
	}
	if len(l.Item.Steps) < 10 || l.Item.Steps[7].Op != "activate" || pass1 == 0 {
		t.Fatalf("steps %v, want pass, six taps, the activation, then the passes", ops(l))
	}
	want := []oraclegen.DieRoll{{Step: pass1, Seat: 0, Sides: 20, Natural: 7, Result: 7}}
	if !reflect.DeepEqual(l.Item.DieRolls, want) {
		t.Fatalf("die rolls %+v, want %+v on the resolving pass (steps %v)", l.Item.DieRolls, want, ops(l))
	}
}

// TestSelfCheckForcesDieRolls: Herald of Hadar's {5}{B} rolls a d20 (CR 706.2).
// The self-check must replay the roll the live game made, not draw its own:
// a 10-19 roll gains the controller 2 life and a 1-9 roll does not. The test
// plays the ability at game seeds until the live game rolls 17, and requires
// the runner's own unforced roll to land in a different row, so the check
// fails when Item.Raw() leaves die_rolls out.
func TestSelfCheckForcesDieRolls(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair := []decks.Deck{
		{Name: "start-p0", Colours: "B", Cards: slices.Concat([]string{"Herald of Hadar"}, repeatCard(39, "Swamp"))},
		{Name: "start-p1", Colours: "W", Cards: repeatCard(40, "Plains")},
	}
	start := &rules.StartSpec{Seats: []rules.StartSeat{
		{Battlefield: []int{0, 1, 2, 3, 4, 5, 6}},
		{},
	}}
	// natural-17 seeds only: the runner's seeded d20 is then in another row
	// when its own life differs from the forced run's.
	var l Lowered
	found := false
	for n := 1; n <= 40 && !found; n++ {
		cand, why, _, err := Span(reg, pair, startGameSeed(0, n), game.Options{
			Set: "XP", MaxTurns: 2, Start: start,
			Focus: &game.Focus{Seat: 0, Card: "Herald of Hadar", Kind: "ability"},
		}, false, "xs:XP:herald-of-hadar:ability", "xproof start Herald of Hadar ability")
		if err != nil {
			t.Fatal(err)
		}
		if len(why) > 0 {
			t.Fatalf("span refused: %v", why)
		}
		for _, r := range cand.Item.DieRolls {
			if r.Sides == 20 && r.Natural == 17 {
				l, found = cand, true
			}
		}
	}
	if !found {
		t.Skip("no game seed in 1..40 rolls a natural 17")
	}
	// The runner's own roll (Seed 42) without the tape.
	unforced, err := json.Marshal(l.Item.Scenario)
	if err != nil {
		t.Fatal(err)
	}
	own, err := rules.RunOracleScenarioJSON(reg, unforced)
	if err != nil {
		t.Fatal(err)
	}
	forced, err := rules.RunOracleScenarioJSON(reg, l.Item.Raw())
	if err != nil {
		t.Fatal(err)
	}
	last := func(r rules.OracleResult) int32 {
		return r.Snapshots[len(r.Snapshots)-1].Players[0].Life
	}
	if last(forced) != 22 {
		t.Fatalf("forced run p0 life %d, want 22 (natural 17 is the 10-19 row)", last(forced))
	}
	if last(own) == 22 {
		t.Skipf("the runner's own d20 also lands in the 10-19 row; the case does not discriminate")
	}
	if c := CheckWindow(reg, l); c.Status != StatusOK {
		t.Fatalf("%s at step %d, %s: gorge %s, tape %s %v", c.Status, c.Step, c.Field, c.Gorge, c.Tape, c.Fails)
	}
}
