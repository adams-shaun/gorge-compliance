package templates

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestLevelBGeneratorResidueRowsServed pins the wave-4 generator-residue rows
// lb4w2-gen serves, one row per mechanism. Each must generate, replay through
// gorge with no fail, and show its mechanism's observable, so a row is never
// served by a scenario that does not exercise it.
func TestLevelBGeneratorResidueRowsServed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key, mechanism string
		check                func(*testing.T, oraclegen.Item, rules.OracleResult)
	}{
		{"Coveted Falcon", "trigger#0.0", "Permanent.YouOwn+YouDontCtrl: the donated-permanent prelude",
			func(t *testing.T, it oraclegen.Item, res rules.OracleResult) {
				// The prelude hands p1 the p0-owned probe; the trigger hands
				// it back. Both control changes must be observed.
				donated := false
				for _, s := range res.Snapshots {
					for _, p := range s.Permanents {
						if p.Name == oraclegen.DonatedProbe && p.Owner == 0 && p.Controller == 1 {
							donated = true
						}
					}
				}
				if !donated {
					t.Fatalf("%s was never controlled by p1 (steps %+v)", oraclegen.DonatedProbe, it.Steps)
				}
				p, ok := permByNameIn(res, oraclegen.DonatedProbe)
				if !ok || p.Controller != 0 {
					t.Fatalf("%s after the trigger: %+v, want p0 controlling it again", oraclegen.DonatedProbe, p)
				}
			}},
		{"Mikey & Don, Party Planners", "trigger#0.0", "MayPlay rider over creature subtypes: a Mutant/Ninja/Turtle probe cast from the library top",
			func(t *testing.T, it oraclegen.Item, res rules.OracleResult) {
				top := it.Scenario.Setup["p0"].LibraryTop
				if len(top) != 1 {
					t.Fatalf("p0 library top %v, want the one subtype probe", top)
				}
				p, ok := permByNameIn(res, top[0])
				if !ok || p.Counters["P1P1"] != 1 {
					t.Fatalf("%s entered with %v, want the rider's one +1/+1 counter", top[0], p.Counters)
				}
			}},
		{"Smaug, Wicked Worm", "trigger#0.1", "Spell.ManaFromTreasure: a nontoken Treasure's mana spent on the probe",
			func(t *testing.T, _ oraclegen.Item, res rules.OracleResult) {
				// "you draw a card and lose 1 life": both halves observed.
				if life := lifeOf(res, 0); life != 19 {
					t.Fatalf("p0 at %d life, want 19 (the trigger's life loss)", life)
				}
				for _, p := range res.Snapshots[len(res.Snapshots)-1].Players {
					if p.Seat == 0 && len(p.Hand) != 1 {
						t.Fatalf("p0 hand %v, want the one drawn card", p.Hand)
					}
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := requirementFor(t, reg, tc.name, tc.key)
			it, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("%s %s (%s) skipped: %s", tc.name, tc.key, tc.mechanism, skip.Reason)
			}
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil || len(res.Fails) != 0 {
				t.Fatalf("%s %s does not replay: err=%v fails=%v", tc.name, tc.key, err, res.Fails)
			}
			if tc.check != nil {
				tc.check(t, it, res)
			}
		})
	}
}
