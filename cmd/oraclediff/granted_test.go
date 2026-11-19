package main

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const (
	dalakosName = "Dalakos, Crafter of Wonders"
	// dalakosPumpLabel is the label the engine offers Dragon Mantle's granted
	// {R} pump under on Dalakos: legal.go's granted-ability offer loop labels
	// it "<recipient face name>: <SpellDescription>", the SVar body's own,
	// CARDNAME-unresolved text.
	dalakosPumpLabel = dalakosName + ": CARDNAME gets +1/+0 until end of turn."
	// dalakosPrintedManaLine is Dalakos's printed ability 0, which the pick's
	// index aliased before the fix (xe:RAND:1:4:w14 step 8, w22 step 9).
	dalakosPrintedManaLine = "AB$ Mana | Cost$ T | Produced$ C | Amount$ 2 | RestrictValid$ Spell.Artifact,Activated.Artifact+inZoneBattlefield | SpellDescription$ Add {C}{C}. Spend this mana only to cast artifact spells or activate abilities of artifacts."
	// dragonMantlePumpLine is Dragon Mantle's Pump SVar body, the granted
	// pump the step must export.
	dragonMantlePumpLine = "AB$ Pump | Defined$ Self | Cost$ R | NumAtt$ +1 | SpellDescription$ CARDNAME gets +1/+0 until end of turn."
)

// dalakosGrantedItem is the farm's w14 shape rebuilt from the corpus: Island,
// Mountain and a tapped Dalakos with Dragon Mantle attached, then the lands'
// mana and the granted pump. The pump's step carries the printed index 0 the
// pick aliased plus the engine's offered label, exactly as the lower's
// granted-identity branch writes it.
func dalakosGrantedItem() oraclegen.Item {
	zero := 0
	return oraclegen.Item{
		ID: "xe:RAND:1:4:w14", Template: lower.Template, Card: dalakosName,
		Scenario: oraclegen.Scenario{
			Name: "dalakos-granted-pump", CR: []string{"602.2b", "613.1f"},
			Why: "Dragon Mantle's granted {R} pump on a tapped Dalakos, not the printed {T} ability the index aliases",
			Setup: map[string]oraclegen.Seat{"p0": {
				Battlefield: []string{"Island", "Mountain", dalakosName, "Dragon Mantle"},
				Tapped:      []string{dalakosName},
				Attached:    []oraclegen.SetupAttach{{Card: "Dragon Mantle", To: "p0:" + dalakosName}},
				Library:     []string{"Plains", "Plains", "Plains", "Plains", "Plains"},
			}},
			Steps: []oraclegen.Step{
				{Op: "activate", Card: "p0:Island", AbilityIndex: &zero},
				{Op: "activate", Card: "p0:Mountain", AbilityIndex: &zero},
				{Op: "activate", Card: "p0:" + dalakosName, AbilityIndex: &zero, Ability: dalakosPumpLabel},
			},
		},
	}
}

// TestForgeExportGrantedAbilityLine: forge-export sends the Forge driver the
// GRANTED ability's script line for a step that activates it, not the printed
// ability the pick's index aliases.
func TestForgeExportGrantedAbilityLine(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := dalakosGrantedItem()
	g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
	if gerr != nil {
		t.Fatalf("gorge replay: %v", gerr)
	}
	rq := forgeBuildRequest(reg, it, g, oraclegen.CastDivisions(reg, it.Scenario, g), false)
	got, ok := rq.Abilities["2"]
	if !ok {
		t.Fatalf("no exported line for step 2: %v", rq.Abilities)
	}
	if got != dragonMantlePumpLine {
		t.Errorf("step 2 line = %q, want the granted pump %q", got, dragonMantlePumpLine)
	}
	// The printed mana ability the index aliases must not be exported, and
	// the printed-ability steps keep their own lines.
	if got == dalakosPrintedManaLine {
		t.Errorf("step 2 exported Dalakos's printed {T} ability")
	}
	if land := rq.Abilities["0"]; land == "" {
		t.Errorf("step 0 (Island mana) exported no line")
	}
}

// TestNeutralGrantedAbility: the engine-neutral identity of a granted step is
// the granted pump's class, cost and text with no ordinal (the ability is not
// on the recipient's face), not the printed mana ability's.
func TestNeutralGrantedAbility(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := dalakosGrantedItem()
	g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
	if gerr != nil {
		t.Fatalf("gorge replay: %v", gerr)
	}
	rq := forgeBuildRequest(reg, it, g, oraclegen.CastDivisions(reg, it.Scenario, g), false)
	c := census{}
	n := neutralFor(reg, rq, g, c)
	a, ok := n.Ability["2"]
	if !ok {
		t.Fatalf("no neutral ability for step 2 (census %v)", map[string]int(c))
	}
	if a.Class != "activated" {
		t.Errorf("class = %q, want activated", a.Class)
	}
	if a.Cost != "{R}" {
		t.Errorf("cost = %q, want {R}", a.Cost)
	}
	if a.Text != "~ gets +1/+0 until end of turn." {
		t.Errorf("text = %q, want the granted pump's text", a.Text)
	}
	if a.Ordinal != nil {
		t.Errorf("ordinal = %d, want none (the ability is not on Dalakos's face)", *a.Ordinal)
	}
	if a.Keyword != "" {
		t.Errorf("keyword = %q, want none", a.Keyword)
	}
}
