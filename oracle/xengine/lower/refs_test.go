package lower

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestLowerPlayStepUnlessAnswer: a played shock land's as-enters "pay 2 life"
// election is a modes ask whose options share Obj (the land). The answer must
// name the picked option by its label; a ref matches the first option and
// makes gorge's replay pay 2 life instead of declining.
func TestLowerPlayStepUnlessAnswer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tp := &game.Tape{
		Format: game.TapeFormat, Starter: 0,
		Open: []game.Opening{{Hand: []string{"Steam Vents"}}, {}},
		Entries: []game.Entry{
			{Kind: "priority", Seat: 0, Turn: 1, Step: "main1",
				Picks: []game.Pick{{Kind: "play_land", Ref: "p0:Steam Vents"}}},
			{Kind: "modes", Seat: 0, Turn: 1, Step: "main1",
				Picks: []game.Pick{{Kind: "mode", Obj: 33, Mode: "unless_decline",
					Label: "Don't pay", Ref: "p0:Steam Vents"}}},
		},
	}
	l, err := Tape(reg, tp)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Item.Steps) != 1 || l.Item.Steps[0].Op != "play" {
		t.Fatalf("steps %+v, want one play", l.Item.Steps)
	}
	if got := l.Item.Steps[0].Answers; len(got) != 1 || got[0].Kind != "modes" ||
		len(got[0].Pick) != 1 || got[0].Pick[0] != "Don't pay" {
		t.Fatalf("play answers %+v, want the decline label", got)
	}
	res, err := rules.RunOracleScenarioJSON(reg, l.Item.Raw())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("runner fails: %v", res.Fails)
	}
	for _, s := range res.Snapshots {
		if s.Checkpoint != "step 0 (play)" {
			continue
		}
		if s.Players[0].Life != 20 {
			t.Fatalf("p0 life %d, want 20 (the decline pays nothing)", s.Players[0].Life)
		}
		found := false
		for _, o := range s.Permanents {
			if o.Name != "Steam Vents" {
				continue
			}
			found = true
			if !o.Tapped {
				t.Fatalf("Steam Vents untapped, want the decline's entry tap")
			}
		}
		if !found {
			t.Fatalf("Steam Vents not on the battlefield at step 0")
		}
		return
	}
	t.Fatalf("no step 0 (play) snapshot")
}

// TestPickStringDividePlayer: a CR 601.2d division option over a player carries
// no object (the engine keys it by Option.Player) and its label is the live
// game's seat-facing name ("W-1"), which the runner's a/b seats do not offer.
// The seat is the one spelling both engines agree on.
func TestPickStringDividePlayer(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    seatMap
		p    game.Pick
		want string
	}{
		{"player 1", seatMap{}, game.Pick{Kind: "divide", Player: 1, Label: "W-1"}, "p1"},
		{"player 0", seatMap{}, game.Pick{Kind: "divide", Player: 0, Label: "R-0"}, "p0"},
		{"swapped seats", seatMap{swap: true}, game.Pick{Kind: "divide", Player: 1, Label: "W-1"}, "p0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.m.pickString(tc.p)
			if !ok || got != tc.want {
				t.Fatalf("pickString(%+v) = %q, %v; want %q, true", tc.p, got, ok, tc.want)
			}
		})
	}
	// A divide over a permanent still names the object, not its owner.
	p := game.Pick{Kind: "divide", Obj: 42, Player: 0, Label: "Grizzly Bears", Ref: "p0:Grizzly Bears"}
	if got, ok := (seatMap{}).pickString(p); !ok || got != "p0:Grizzly Bears" {
		t.Fatalf("divide over an object = %q, %v; want the ref", got, ok)
	}
}
