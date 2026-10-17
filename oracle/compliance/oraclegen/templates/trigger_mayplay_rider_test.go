package templates

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// TestMayPlayRiderRowsGenerate is the class table for the Static$ True
// SpellCast rider of a play-permission static (wave-4 G5 SpellCast rows):
// every row generates, plays through gorge, ends with the permitted cast's
// permanent carrying the rider's counter, and the scenario's cast step is the
// row's cause (not an assertion-only pass_to).
func TestMayPlayRiderRowsGenerate(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key, probe, counter string }{
		{"Intrepid Paleontologist", "trigger#0.0", "", "FINALITY"},
		{"Undead Sprinter", "trigger#0.0", "Undead Sprinter", "P1P1"},
		{"Hundred-Battle Veteran", "trigger#0.0", "Hundred-Battle Veteran", "FINALITY"},
		{"Noctis, Prince of Lucis", "trigger#0.0", "", "FINALITY"},
		{"Leonardo, Sewer Samurai", "trigger#0.0", "", "FINALITY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, levelb.MayPlayRiderSub)
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			var cast string
			for _, st := range it.Scenario.Steps {
				if st.Op == "cast" {
					cast = st.Card
				}
			}
			if cast == "" {
				t.Fatalf("no cast step in %+v", it.Scenario.Steps)
			}
			if tc.probe != "" && cast != "p0:"+tc.probe {
				t.Fatalf("cast %s, want p0:%s", cast, tc.probe)
			}
			found := false
			for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
				if "p0:"+p.Name == cast && p.Counters[tc.counter] > 0 {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s did not enter with a %s counter", cast, tc.counter)
			}
			if len(it.XAbility) != 0 && len(it.XAbility) != len(it.Scenario.Steps) {
				t.Fatalf("xmage_ability length %d, steps %d", len(it.XAbility), len(it.Scenario.Steps))
			}
		})
	}
}
