package templates

import "testing"

// TestStaticGrantedReplacementMore: the printed AddReplacementEffect$ carriers
// whose body is a ReplaceDamage prevention (Hedron-Field Purists, one row per
// LEVEL band) or an Untap-replacing RemoveCounter (Bewitching Leechcraft)
// generate a scenario that replays and whose own observation holds. The
// generator's control run (counters absent / no Aura) proves the grant did
// the work.
func TestStaticGrantedReplacementMore(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		card, key string
		life      int32 // p0 life after Shock (Purists)
		level     int32 // LEVEL counters the fixture must hold (Purists)
	}{
		{"Hedron-Field Purists", "static#0.0", 19, 1},
		{"Hedron-Field Purists", "static#0.1", 20, 5},
		{"Bewitching Leechcraft", "static#0.0", 0, 0},
	} {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			req := counterReq(t, reg, tc.card, tc.key)
			it, skip := GenerateB(reg, tc.card, req)
			if skip != nil {
				t.Fatalf("GenerateB skipped: %s", skip.Reason)
			}
			if len(it.Scenario.Steps) == 0 {
				t.Fatal("served item has no observation steps")
			}
			res, ok := runStatic(reg, it.Scenario)
			if !ok {
				t.Fatal("served scenario does not replay in gorge")
			}
			if len(res.Fails) != 0 {
				t.Fatalf("served scenario's own expectations fail: %v", res.Fails)
			}
			if len(res.Snapshots) == 0 {
				t.Fatal("served scenario produced no snapshot")
			}
			last := res.Snapshots[len(res.Snapshots)-1]
			if tc.level > 0 {
				// Precondition: the gate's LEVEL counters are really placed.
				if got := it.Setup["p0"].Counters[tc.card]["LEVEL"]; got != tc.level {
					t.Fatalf("fixture LEVEL counters = %d, want %d", got, tc.level)
				}
				// Shock's printed 2 would leave 18; the prevention differs.
				if life, ok := snapshotLife(last, 0); !ok || life != tc.life {
					t.Fatalf("p0 life = %d (ok=%v), want %d", life, ok, tc.life)
				}
				return
			}
			var bears, aura bool
			for _, p := range last.Permanents {
				switch p.Name {
				case staticProbe:
					bears = true
					if p.Tapped {
						t.Errorf("%s stayed tapped; the untap should remove its counter and untap it", p.Name)
					}
					if n := p.Counters["P1P1"]; n != 0 {
						t.Errorf("%s has %d +1/+1 counters, want 0 (the untap removed it)", p.Name, n)
					}
				case tc.card:
					aura = p.AttachedTo != ""
				}
			}
			if !bears || !aura {
				t.Fatalf("precondition: creature on battlefield=%v, Aura attached=%v", bears, aura)
			}
		})
	}
}
