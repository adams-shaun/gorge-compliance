package dside

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestAnswer(t *testing.T) {
	diverged := Row{Status: StatusDiverged, FromTurn: 4, ToTurn: 5}
	stalled := Row{Status: StatusCapacity, FromTurn: 4, ToTurn: 4}
	for _, c := range []struct {
		row  Row
		id   string
		want string
	}{
		{Row{Status: StatusClean}, "xe:T:0:0:w9", Agree},
		{Row{Status: StatusClean}, "xe:T:0:0:tape", Agree},
		{Row{Status: StatusNA}, "xe:T:0:0:w1", NotAppl},
		{Row{Status: StatusHang}, "xe:T:0:0:w1", NotAppl},
		{diverged, "xe:T:0:0:w3", Agree},
		{diverged, "xe:T:0:0:w4", Diverge},
		{diverged, "xe:T:0:0:w5", Diverge},
		{diverged, "xe:T:0:0:w6", NotAppl},
		{diverged, "xe:T:0:0:tape", Diverge},
		{diverged, "xe:T:0:0:tape:t2", Diverge},
		{stalled, "xe:T:0:0:w3", Agree},
		{stalled, "xe:T:0:0:w4", NotAppl},
		{stalled, "xe:T:0:0:tape", NotAppl},
		{Row{Status: StatusClean}, "not an item", NotAppl},
	} {
		if got := c.row.Answer(c.id); got != c.want {
			t.Errorf("%+v %s: got %s, want %s", c.row, c.id, got, c.want)
		}
	}
}

func TestLocate(t *testing.T) {
	tp := &game.Tape{Entries: []game.Entry{{Turn: 1}, {Turn: 1}, {Turn: 2}}}
	r := Row{Status: StatusDiverged, Intents: 2}
	r.Locate(tp)
	if r.FromTurn != 1 || r.ToTurn != 2 {
		t.Fatalf("a difference at a turn's edge: %+v", r)
	}
	r = Row{Status: StatusCapacity, Intents: 2}
	r.Locate(tp)
	if r.FromTurn != 2 || r.ToTurn != 2 {
		t.Fatalf("a stall: %+v", r)
	}
}

// TestScriptReplaysTheLockstepGame plays the game the sweep plays from a lockstep's
// answers: the script holds, so the tape's entries are the lockstep's decisions.
func TestScriptReplaysTheLockstepGame(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tables := NewTables(reg)
	cands, _, err := decks.Candidates(reg, "../../compliance/manifests", "FDN")
	if err != nil {
		t.Fatal(err)
	}
	playable, err := tables.Playable("FDN", cands)
	if NoTables(err) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(playable) == 0 || len(playable) > len(cands) {
		t.Fatalf("%d playable of %d", len(playable), len(cands))
	}
	played := 0
	for g := 0; g < 6; g++ {
		pair := decks.Pair(playable, nil, "FDN", 0, g, 1)
		seed := uint64(1000 + g)
		row, script := tables.Lockstep(GameID("FDN", 0, g), "FDN", pair, seed, game.DefaultMaxTurns)
		if row.Status == StatusNA {
			continue
		}
		played++
		tp, err := game.Play(reg, pair, seed, game.Options{Set: "FDN", Script: script})
		if err != nil {
			t.Fatalf("game %d: %v", g, err)
		}
		if len(tp.Entries) < row.Intents {
			t.Fatalf("game %d: %d entries for %d answers", g, len(tp.Entries), row.Intents)
		}
		for i, in := range script {
			if tp.Entries[i].Seq != in.Seq {
				t.Fatalf("game %d entry %d: seq %d, the script's %d", g, i, tp.Entries[i].Seq, in.Seq)
			}
		}
	}
	if played == 0 {
		t.Fatal("no pair played")
	}
}
