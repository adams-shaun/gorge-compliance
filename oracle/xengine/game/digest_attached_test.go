package game

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestDigestOfSnapshotAttachedToCopiedHost: a snapshot's AttachedTo is the
// host's scenario ref (gorge's oracle_snapshot.go sets it from objRef), and a
// ref is identity, not a name. A setup-bound ref keeps the printed name after
// its permanent copies something else (Secret Invasion; CR 707.2), so
// refName's spelling is "Ornithopter" while the host's own digest entry says
// "Memnite". DigestOfSnapshot must resolve the ref to the carrying permanent's
// Name, and DigestOfGame must name the host the same way, so the two digests
// of one position agree.
func TestDigestOfSnapshotAttachedToCopiedHost(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card := func(name string) *cards.Card {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("card %q not in the corpus", name)
		}
		return c
	}
	e, ids, err := rules.NewStaged(rules.Config{
		Seed: 1, Names: []string{"p0", "p1"}, Tokens: reg.Tokens,
	}, rules.Stage{
		Turn: 5, Active: 0, Starting: 0, Step: state.StepMain1, Enter: rules.StagePriorityFresh,
		Players: []rules.StagedPlayer{{Life: 20}, {Life: 20}},
		Permanents: []rules.StagedPermanent{
			{Card: card("Ornithopter"), Owner: 0, Controller: 0},
			{Card: card("Pacifism"), Owner: 0, Controller: 0, AttachTo: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	host := e.G.Obj(ids.Permanents[0])
	if host.AttachedTo != 0 {
		t.Fatalf("host starts attached to %d, want no attachment", host.AttachedTo)
	}
	if e.G.Obj(ids.Permanents[1]).AttachedTo != host.ID {
		t.Fatalf("staged Aura is not attached to the host")
	}
	// The copy Secret Invasion makes (CR 707.2): the host's face is now
	// Memnite, while its scenario ref stays the setup-bound "p0:Ornithopter".
	host.SetCopyFace(card("Memnite").Faces[0])

	live := DigestOfGame(e)
	var liveHost, liveAura *PermDigest
	for i := range live.Field {
		switch live.Field[i].Name {
		case "Memnite":
			liveHost = &live.Field[i]
		case "Pacifism":
			liveAura = &live.Field[i]
		}
	}
	if liveHost == nil || liveAura == nil {
		t.Fatalf("live field does not name the copied host and its Aura: %+v", live.Field)
	}
	if liveAura.AttachedTo != "Memnite" {
		t.Fatalf("live attached_to = %q, want the host's current name Memnite", liveAura.AttachedTo)
	}

	snap := rules.OracleSnapshot{
		Turn: e.G.Turn, Step: e.G.Step.String(), Active: int(e.G.Active), Priority: int(e.G.Priority),
		Players: []rules.OracleSnapPlayer{{Seat: 0, Life: 20}, {Seat: 1, Life: 20}},
		Permanents: []rules.OracleSnapPerm{
			{Ref: "p0:Ornithopter", Name: "Memnite", Controller: 0, Owner: 0, PT: "1/1"},
			{Ref: "p0:Pacifism", Name: "Pacifism", Controller: 0, Owner: 0, AttachedTo: "p0:Ornithopter"},
		},
	}
	got := DigestOfSnapshot(snap)
	var snapAura *PermDigest
	for i := range got.Field {
		if got.Field[i].Name == "Pacifism" {
			snapAura = &got.Field[i]
		}
	}
	if snapAura == nil {
		t.Fatalf("snapshot field does not name the Aura: %+v", got.Field)
	}
	if snapAura.AttachedTo != "Memnite" {
		t.Fatalf("snapshot attached_to = %q, want the host's current name Memnite", snapAura.AttachedTo)
	}
	if diff := got.Diff(live); len(diff) != 0 {
		t.Fatalf("snapshot and live digests of the same position differ: %v", diff)
	}
}
