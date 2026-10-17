package templates

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestActivateFaceDownTargetRows serves the four level-B activate rows whose
// target filter names faceDown (they used to skip "activate no fixture gorge
// can activate"). The fixture casts a Disguise probe face down for {3}
// BEFORE the activation, and the activation targets that permanent.
func TestActivateFaceDownTargetRows(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	type finalState int
	const (
		faceUp        finalState = iota // the effect turns the target face up
		stillFaceDown                   // the effect leaves it face down on the battlefield
		gone                            // the effect destroys it
	)
	for _, tc := range []struct {
		name  string
		final finalState
	}{
		{"Hauntwoods Shrieker", faceUp},      // DSK: SetState TurnFaceUp, Card.faceDown
		{"Ixidor, Reality Sculptor", faceUp}, // ONS: TurnFaceUp, Creature.faceDown
		{"Nosy Goblin", gone},                // ONS: Destroy, Creature.faceDown
		{"Skirk Alarmist", stillFaceDown},    // LGN: SubAbility pump, Creature.YouCtrl+faceDown
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := activateRowRequirement(t, reg, tc.name, "activate#0.0")
			c, _ := reg.Lookup(tc.name)
			slot, err := strconv.Atoi(req.Slot)
			if err != nil {
				t.Fatalf("precondition: slot %q: %v", req.Slot, err)
			}
			if line := c.Faces[req.Face].Abilities[slot].Line; !strings.Contains(line, "faceDown") {
				t.Fatalf("precondition: %s ability line names no faceDown target: %s", tc.name, line)
			}

			it, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("%s skipped: %s", tc.name, skip.Reason)
			}

			steps := it.Scenario.Steps
			act := -1
			for i, st := range steps {
				if st.Op == "activate" {
					act = i
					break
				}
			}
			if act < 0 {
				t.Fatalf("no activate step in %+v", steps)
			}
			// The face-down cast precedes the activation.
			cast := -1
			for i := 0; i < act; i++ {
				if steps[i].Op == "cast" && steps[i].CastMode != "" && steps[i].Mana == faceDownCastPool {
					cast = i
					break
				}
			}
			if cast < 0 {
				t.Fatalf("no face-down cast (CastMode set, Mana %q) before the activate step: %+v", faceDownCastPool, steps[:act])
			}
			ref := steps[cast].Card
			if got := steps[act].Targets; len(got) != 1 || got[0] != ref {
				t.Fatalf("activate targets = %v, want [%s] (the face-down probe)", got, ref)
			}
			if steps[act].Seat != 0 || !strings.HasPrefix(ref, "p0:") {
				t.Fatalf("activation seat %d / probe ref %q: want p0 on both", steps[act].Seat, ref)
			}

			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			// The snapshot taken at the activation step (after the step that
			// resolved the cast): the probe is a face-down p0 permanent, and
			// the source (a face-up control) is not.
			before := snapshotAfter(t, res, steps, act-1)
			probe, okp := permByRef(before, ref)
			if !okp {
				t.Fatalf("probe %s is not on the battlefield at the activate step: %+v", ref, before.Permanents)
			}
			if !probe.FaceDown || probe.Controller != 0 {
				t.Fatalf("probe = %+v, want a face-down permanent p0 controls", probe)
			}
			src, oks := permByRef(before, steps[act].Card)
			if !oks || src.FaceDown {
				t.Fatalf("source %s = %+v (found %v): want a face-up control that differs from the probe", steps[act].Card, src, oks)
			}

			last := res.Snapshots[len(res.Snapshots)-1]
			after, still := permByRef(last, ref)
			switch tc.final {
			case faceUp:
				if !still || after.FaceDown {
					t.Errorf("final: probe on battlefield=%v %+v, want turned face up", still, after)
				}
			case gone:
				if still {
					t.Errorf("final: probe still on the battlefield %+v, want destroyed", after)
				}
			}
		})
	}
}

// snapshotAfter returns the snapshot the runner took after step i.
func snapshotAfter(t *testing.T, res rules.OracleResult, steps []oraclegen.Step, i int) rules.OracleSnapshot {
	t.Helper()
	want := fmt.Sprintf("step %d (%s)", i, steps[i].Op)
	for _, s := range res.Snapshots {
		if s.Checkpoint == want {
			return s
		}
	}
	t.Fatalf("no snapshot %q among %d", want, len(res.Snapshots))
	return rules.OracleSnapshot{}
}

func permByRef(s rules.OracleSnapshot, ref string) (rules.OracleSnapPerm, bool) {
	for _, p := range s.Permanents {
		if p.Ref == ref {
			return p, true
		}
	}
	return rules.OracleSnapPerm{}, false
}
