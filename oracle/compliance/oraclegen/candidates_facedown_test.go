package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// isFaceDownCand reports a candidate made by the face-down cast prelude: a
// cast step carrying a cast mode, paying the face-down pool.
func isFaceDownCand(c cand) bool {
	for _, st := range c.pre {
		if st.Op == "cast" && st.CastMode != "" && st.Mana == FaceDownCastPool {
			return true
		}
	}
	return false
}

// TestCandidatesFaceDownFilterMatrix pins which faceDown filters the
// candidate picker serves with a face-down cast prelude and which fail closed
// (Hauntwoods Shrieker "Card.faceDown", Nosy Goblin "Creature.faceDown", Skirk
// Alarmist "Creature.YouCtrl+faceDown"; Break Open's OppCtrl and Kaust's
// attacking shapes are named gaps).
func TestCandidatesFaceDownFilterMatrix(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	SetXMageKnown(nil)
	t.Cleanup(func() { SetXMageKnown(nil) })

	probe := FaceDownProbe(reg, "")
	if probe == "" {
		t.Fatal("precondition: no Disguise probe in the corpus")
	}

	for _, filter := range []string{"Card.faceDown", "Creature.faceDown", "Creature.YouCtrl+faceDown"} {
		cs := candidatesFor(reg, filter, "")
		if len(cs) != 1 {
			t.Fatalf("%s: %d candidates, want exactly the one face-down probe: %+v", filter, len(cs), cs)
		}
		c := cs[0]
		if !isFaceDownCand(c) {
			t.Fatalf("%s: candidate %+v has no face-down cast prelude", filter, c)
		}
		if c.seat != "p0" || c.zone != "battlefield" || c.card != probe || c.ref != "p0:"+probe {
			t.Fatalf("%s: candidate = %+v, want p0 battlefield %q ref p0:%s", filter, c, probe, probe)
		}
		if len(c.pre) != 2 || c.pre[0].Seat != 0 || c.pre[0].Card != "p0:"+probe || c.pre[1].Op != "resolve" {
			t.Fatalf("%s: prelude = %+v, want p0 cast of the probe then resolve", filter, c.pre)
		}
	}

	// Fails closed: another player's face-down permanent (a p1 probe needs a
	// p1 turn), and an attacking one (the cast probe is summoning sick).
	for _, filter := range []string{
		"Creature.faceDown+OppCtrl",
		"Creature.faceDown+attacking+YouCtrl",
		"Land.faceDown",
	} {
		for _, c := range candidatesFor(reg, filter, "") {
			if isFaceDownCand(c) {
				t.Errorf("%s: served a face-down candidate %+v, want none", filter, c)
			}
		}
	}
	if cs := candidatesFor(reg, "Creature.faceDown+OppCtrl", ""); len(cs) != 0 {
		t.Errorf("Creature.faceDown+OppCtrl served %+v, want none (fail closed)", cs)
	}

	// A negated faceDown is a restriction, not a demand: the ordinary face-up
	// stand-ins serve it. The list is non-empty, so "no face-down candidate"
	// below is not satisfied vacuously.
	neg := candidatesFor(reg, "Creature.!faceDown", "")
	if len(neg) == 0 {
		t.Fatal("precondition: Creature.!faceDown has no candidates at all")
	}
	for _, c := range neg {
		if isFaceDownCand(c) {
			t.Errorf("Creature.!faceDown served the face-down probe %+v", c)
		}
	}
}

// TestCandidatesFaceDownMirrorStagesProbeInP0Hand covers a Mirror slot (the
// second pick of a TargetsWithDifferentControllers$ pair): an unqualified
// "Creature.faceDown" has no controller word, so the slot flips the candidate
// to p1, yet the probe is only ever cast by p0. The cast is staged in p0's
// hand and the target ref still names p0's probe.
func TestCandidatesFaceDownMirrorStagesProbeInP0Hand(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	SetXMageKnown(nil)
	t.Cleanup(func() { SetXMageKnown(nil) })
	probe := FaceDownProbe(reg, "")
	if probe == "" {
		t.Fatal("precondition: no Disguise probe in the corpus")
	}
	if controllerQualified("Creature.faceDown") {
		t.Fatal("precondition: Creature.faceDown must be unqualified for the Mirror flip to apply")
	}

	for _, mirror := range []bool{false, true} {
		fxs := fixtures(reg, []Slot{{Filter: "Creature.faceDown", Mirror: mirror}})
		if len(fxs) != 1 {
			t.Fatalf("mirror=%v: %d fixtures, want 1", mirror, len(fxs))
		}
		fx := fxs[0]
		if len(fx.targets) != 1 || fx.targets[0] != "p0:"+probe {
			t.Fatalf("mirror=%v: targets = %v, want [p0:%s]", mirror, fx.targets, probe)
		}
		if len(fx.p0.Hand) != 1 || fx.p0.Hand[0] != probe {
			t.Errorf("mirror=%v: p0 hand = %v, want [%s]", mirror, fx.p0.Hand, probe)
		}
		if len(fx.p1.Hand) != 0 || len(fx.p1.Battlefield) != 0 || len(fx.p0.Battlefield) != 0 {
			t.Errorf("mirror=%v: probe also staged elsewhere: p1 %+v p0 bf %v", mirror, fx.p1, fx.p0.Battlefield)
		}
		if len(fx.pre) != 2 || fx.pre[0].CastMode == "" {
			t.Errorf("mirror=%v: fixture prelude = %+v, want the face-down cast + resolve", mirror, fx.pre)
		}
	}
}
