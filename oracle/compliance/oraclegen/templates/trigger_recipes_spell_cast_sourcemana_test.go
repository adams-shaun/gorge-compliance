package templates

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestSpellCastSourceManaCauseServesAndControls pins the producing-source
// provenance cause (spellCastSourceManaCause) on every corpus carrier of
// `ValidSA$ Spell.ManaFromCard.StrictlySelf`: the served item taps the card
// under test for mana and casts the probe with it, and gorge puts the
// trigger on the stack. The control replays the same item with the card's
// tap replaced by a Wastes' {T}: Add {C} -- the probe is paid from a land, so
// the trigger must stay silent (CR 106.3/601.2h: only mana THIS permanent
// produced counts).
func TestSpellCastSourceManaCauseServesAndControls(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, backFace string }{
		{"Brass's Tunnel-Grinder", "Tecutlan, the Searing Rift"},
		{"The Everflowing Well", "The Myriad Pools"},
		{"Thousand Moons Smithy", "Barracks of the Thousand"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := requirementFor(t, reg, tc.name, "trigger#1.0")
			it, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("skipped: %s", skip.Reason)
			}
			if len(it.Steps) < 2 || it.Steps[0].Op != "activate" || it.Steps[0].Card != "p0:"+tc.name || it.Steps[1].Op != "cast" {
				t.Fatalf("steps %+v, want the card's own mana activation then the probe cast", it.Steps)
			}
			if len(it.XAbility) == 0 || it.XAbility[0] == "" {
				t.Fatalf("xmage_ability %v, want the mana ability's rule text on the activate step", it.XAbility)
			}
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil || len(res.Fails) != 0 {
				t.Fatalf("does not replay: err=%v fails=%v", err, res.Fails)
			}
			if cast, fired := sourceTriggerAfterCast(res, tc.backFace); !cast || !fired {
				t.Fatalf("no %s trigger on the stack after the card-paid cast", tc.backFace)
			}

			// Control: the same cast paid from a land.
			var sc oraclegen.Scenario
			if err := json.Unmarshal(it.Raw(), &sc); err != nil {
				t.Fatal(err)
			}
			p0 := sc.Setup["p0"]
			p0.Battlefield = append(p0.Battlefield, "Wastes")
			sc.Setup["p0"] = p0
			zero := 0
			sc.Steps = []oraclegen.Step{
				{Op: "activate", Seat: 0, Card: "p0:Wastes", AbilityIndex: &zero},
				sc.Steps[1],
			}
			raw, _ := json.Marshal(sc)
			ctl, err := rules.RunOracleScenarioJSON(reg, raw)
			if err != nil || len(ctl.Fails) != 0 {
				t.Fatalf("control does not replay: err=%v fails=%v", err, ctl.Fails)
			}
			if cast, fired := sourceTriggerAfterCast(ctl, tc.backFace); !cast || fired {
				t.Fatalf("control: probe cast observed=%v, %s fired=%v; want the cast and no trigger", cast, tc.backFace, fired)
			}
		})
	}
}

// sourceTriggerAfterCast reads the snapshot after step 1: cast reports that
// the probe spell is on the stack there (the cast really happened), fired
// that a triggered ability of the named permanent is too.
func sourceTriggerAfterCast(res rules.OracleResult, face string) (cast, fired bool) {
	for _, s := range res.Snapshots {
		if s.Checkpoint != "step 1 (cast)" {
			continue
		}
		for _, it := range s.Stack {
			switch {
			case it.Kind == "spell" && it.Source == "p0:"+sourceManaProbe:
				cast = true
			case it.Kind == "ability" && it.Source == "p0:"+face:
				fired = true
			}
		}
	}
	return cast, fired
}
