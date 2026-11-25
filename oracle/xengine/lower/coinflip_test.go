package lower

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
)

// TestLowerCoinFlipResult: every coin flip recorded on a tape entry lowers
// into the item on the step that made it, in log order, keeping the Forced and
// Draws marks a driver needs to know how many results the flip takes. Mana
// Clash's repeat loop flips several coins on one step.
func TestLowerCoinFlipResult(t *testing.T) {
	d := game.Digest{Turn: 1, Step: "main1", Active: 0, Priority: 0}
	tp := &game.Tape{
		Format: game.TapeFormat, Starter: 0,
		Entries: []game.Entry{
			{Kind: "priority", Seat: 0, Turn: 1, Step: "main1", Digest: d,
				Picks: []game.Pick{{Kind: "pass", Player: 0}}},
			{Kind: "priority", Seat: 0, Turn: 1, Step: "main1", Digest: d, RNG: 4,
				Picks: []game.Pick{{Kind: "ability", Player: 0, Obj: stateID(42), Ref: "p0:Ebony Fly", Ability: 0,
					Label: "Ebony Fly: Roll a d6."}},
				Flips: []game.Flip{
					{Seat: 0, Win: false}, {Seat: 1, Win: true},
					{Seat: 0, Win: true, Forced: true}, {Seat: 1, Win: true, Draws: 2},
				}},
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
	want := []oraclegen.CoinFlip{
		{Step: 1, Seat: 0, Win: false},
		{Step: 1, Seat: 1, Win: true},
		{Step: 1, Seat: 0, Win: true, Forced: true},
		{Step: 1, Seat: 1, Win: true, Draws: 2},
	}
	if !reflect.DeepEqual(l.Item.CoinFlips, want) {
		t.Fatalf("coin flips %+v, want %+v (steps %+v)", l.Item.CoinFlips, want, ops(l))
	}
	// Raw sends the flips in gorge's runner shape: no Forced field (the runner
	// decodes with DisallowUnknownFields), an absent draws for the ordinary
	// one-draw flip, 0 for the forced flip its hook never asks about, and 2
	// for the doubler.
	raw := l.Item.Raw()
	if strings.Contains(string(raw), `"forced"`) {
		t.Fatalf("Raw sends the follower side Forced field to gorge's runner: %s", raw)
	}
	var runner struct {
		CoinFlips []struct {
			Step  int  `json:"step"`
			Seat  int  `json:"seat"`
			Win   bool `json:"win"`
			Draws *int `json:"draws"`
		} `json:"coin_flips"`
	}
	if err := json.Unmarshal(raw, &runner); err != nil {
		t.Fatalf("Raw is not the runner's coin_flips shape: %v (%s)", err, raw)
	}
	if len(runner.CoinFlips) != len(want) {
		t.Fatalf("Raw coin flips %+v, want %d records (%s)", runner.CoinFlips, len(want), raw)
	}
	zero, two := 0, 2
	draws := []*int{nil, nil, &zero, &two}
	for i, got := range runner.CoinFlips {
		if got.Step != want[i].Step || got.Seat != want[i].Seat || got.Win != want[i].Win {
			t.Fatalf("Raw coin flip %d = %+v, want %+v", i, got, want[i])
		}
		switch {
		case draws[i] == nil && got.Draws != nil:
			t.Fatalf("Raw coin flip %d draws %d, want absent (%s)", i, *got.Draws, raw)
		case draws[i] != nil && (got.Draws == nil || *got.Draws != *draws[i]):
			t.Fatalf("Raw coin flip %d draws %v, want %d (%s)", i, got.Draws, *draws[i], raw)
		}
	}
	// The setup-only probe has no steps, so it must leave the gameplay flips
	// out the way it leaves the die rolls out.
	if probe := string(setupProbeRaw(l.Item)); strings.Contains(probe, `"coin_flips"`) {
		t.Fatalf("setup probe sends gameplay coin flips to gorge's runner: %s", probe)
	}
	cut := l.Item
	cut.CutSteps(1)
	if len(cut.CoinFlips) != 0 {
		t.Fatalf("CutSteps(1) kept flips of cut steps: %+v", cut.CoinFlips)
	}
	cut = l.Item
	cut.CutSteps(2)
	if !reflect.DeepEqual(cut.CoinFlips, want) {
		t.Fatalf("CutSteps(2) lost flips of a kept step: %+v", cut.CoinFlips)
	}
}
