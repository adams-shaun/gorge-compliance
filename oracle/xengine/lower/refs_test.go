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
		Open: [2]game.Opening{{Hand: []string{"Steam Vents"}}, {}},
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
