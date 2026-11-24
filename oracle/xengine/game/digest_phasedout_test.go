package game

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestDigestOfGameOmitsPhasedOut: a phased-out permanent is on the battlefield
// but treated as though it does not exist (CR 702.26b), and gorge's oracle
// snapshot omits it. DigestOfGame must omit it too, or the tape digest keeps a
// permanent the replayed snapshot never has and lower.SelfCheck reports
// field.len. DigestOfSnapshot of the same position is the oracle the live
// digest is held to, so it must agree with no diff.
func TestDigestOfGameOmitsPhasedOut(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card := func(name string) *cards.Card {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("card %q not in the corpus", name)
		}
		return c
	}
	// A staged turn-5 main phase keeps every placement rule out of the way; the
	// two permanents are all the position needs.
	e, ids, err := rules.NewStaged(rules.Config{
		Seed: 1, Names: []string{"p0", "p1"}, Tokens: reg.Tokens,
	}, rules.Stage{
		Turn: 5, Active: 0, Starting: 0, Step: state.StepMain1, Enter: rules.StagePriorityFresh,
		Players: []rules.StagedPlayer{{Life: 20}, {Life: 20}},
		Permanents: []rules.StagedPermanent{
			{Card: card("Blink Dog"), Owner: 0, Controller: 0},
			{Card: card("Plains"), Owner: 0, Controller: 0},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(DigestOfGame(e).Field); got != 2 {
		t.Fatalf("before phase-out the field digest has %d permanents, want 2", got)
	}

	e.G.Obj(ids.Permanents[0]).PhasedOut = true
	live := DigestOfGame(e)
	if len(live.Field) != 1 || live.Field[0].Name != "Plains" {
		t.Fatalf("phased-out permanent leaked into the digest: %+v", live.Field)
	}

	// The oracle snapshot of the same position names the visible Plains only
	// (gorge's snapshot skips PhasedOut itself). DigestOfSnapshot must agree.
	snap := rules.OracleSnapshot{
		Turn: e.G.Turn, Step: e.G.Step.String(), Active: int(e.G.Active), Priority: int(e.G.Priority),
		Players: []rules.OracleSnapPlayer{
			{Seat: 0, Life: e.G.Players[0].Life},
			{Seat: 1, Life: e.G.Players[1].Life},
		},
		Permanents: []rules.OracleSnapPerm{{
			Ref: "p0:Plains", Name: "Plains", Controller: 0, Owner: 0,
		}},
	}
	if d := DigestOfSnapshot(snap).Diff(live); len(d) != 0 {
		t.Fatalf("live and snapshot digests differ: %v", d)
	}
}
