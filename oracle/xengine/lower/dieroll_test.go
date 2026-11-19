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
