package main

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
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
				Battlefield:      []string{"Island", "Mountain", dalakosName, "Dragon Mantle"},
				TappedAfterUntap: []string{dalakosName},
				Attached:         []oraclegen.SetupAttach{{Card: "Dragon Mantle", To: "p0:" + dalakosName}},
				Library:          []string{"Plains", "Plains", "Plains", "Plains", "Plains"},
			}},
			Steps: []oraclegen.Step{
				{Op: "activate", Card: "p0:Island", AbilityIndex: &zero},
				{Op: "activate", Card: "p0:Mountain", AbilityIndex: &zero},
				{Op: "activate", Card: "p0:" + dalakosName, AbilityIndex: &zero, Ability: dalakosPumpLabel},
			},
		},
	}
}

// replayItem plays it in gorge and requires the run to succeed. A failed
// step lives in g.Fails, not in the returned error, so a test that checks only
// gerr would accept an item whose labelled option the engine never offered
// (RunOracleScenarioJSON returns (result, nil) on a step failure).
func replayItem(t *testing.T, reg *cards.Registry, it oraclegen.Item) rules.OracleResult {
	t.Helper()
	g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
	if gerr != nil {
		t.Fatalf("gorge replay: %v", gerr)
	}
	if len(g.Fails) != 0 {
		t.Fatalf("gorge replay failed: %v", g.Fails)
	}
	return g
}

// TestForgeExportGrantedAbilityLine: forge-export sends the Forge driver the
// GRANTED ability's script line for a step that activates it, not the printed
// ability the pick's index aliases.
func TestForgeExportGrantedAbilityLine(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := dalakosGrantedItem()
	g := replayItem(t, reg, it)
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

// TestForgeExportGrantedAbilityFromRecordedPick: a pre-fix window carries no
// Step.Ability label, only the aliased index. gorge's own recorded activate
// pick still names the granted option, so forge-export recovers the identity
// without re-lowering -- Done-means item 5 on the farm's cut item shape.
func TestForgeExportGrantedAbilityFromRecordedPick(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := dalakosGrantedItem()
	it.Steps[2].Ability = ""
	g := replayItem(t, reg, it)
	rq := forgeBuildRequest(reg, it, g, oraclegen.CastDivisions(reg, it.Scenario, g), false)
	got, ok := rq.Abilities["2"]
	if !ok {
		t.Fatalf("no exported line for step 2: %v", rq.Abilities)
	}
	if got != dragonMantlePumpLine {
		t.Errorf("step 2 line = %q, want the granted pump %q", got, dragonMantlePumpLine)
	}
	if got == dalakosPrintedManaLine {
		t.Errorf("step 2 exported Dalakos's printed {T} ability")
	}
}

// TestForgeExportGrantedAbilityRequiresOfferedGrant proves the positive test's
// replay really depends on the granted option being offered: with Dragon
// Mantle absent the pump is not offered, gorge's step fails, and replayItem
// rejects the item rather than exporting a line from setup's possible grantors
// alone.
func TestForgeExportGrantedAbilityRequiresOfferedGrant(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := dalakosGrantedItem()
	it.Scenario.Setup["p0"] = oraclegen.Seat{
		Battlefield:      []string{"Island", "Mountain", dalakosName},
		TappedAfterUntap: []string{dalakosName},
		Library:          []string{"Plains", "Plains", "Plains", "Plains", "Plains"},
	}
	g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
	if gerr != nil {
		t.Fatalf("gorge replay: %v", gerr)
	}
	if len(g.Fails) == 0 {
		t.Fatalf("gorge replay of an unattached Dragon Mantle succeeded; the granted option was not actually required")
	}
}

// TestNeutralGrantedAbility: the engine-neutral identity of a granted step is
// the granted pump's class, cost and text with no ordinal (the ability is not
// on the recipient's face), not the printed mana ability's.
func TestNeutralGrantedAbility(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := dalakosGrantedItem()
	g := replayItem(t, reg, it)
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
	if !a.Granted {
		t.Errorf("granted = false, want true (the ability is not on Dalakos's face)")
	}
	if a.Keyword != "" {
		t.Errorf("keyword = %q, want none", a.Keyword)
	}
}

// TestForgeExportGrantedAbilityIgnoresUnrelatedSameTextCard: an unrelated
// card that prints the same SpellDescription (Vengeful Firebrand prints
// "CARDNAME gets +1/+0 until end of turn.") is not a grant source for
// Dalakos. Resolution is by the grant relationship, not the text, so it must
// neither shadow the granted pump nor leave forge/neutral with no answer.
// The old every-card scan found two distinct lines and silently fell back to
// Dalakos's printed {T} ability.
func TestForgeExportGrantedAbilityIgnoresUnrelatedSameTextCard(t *testing.T) {
	for _, zone := range []string{"battlefield", "hand", "graveyard"} {
		t.Run(zone, func(t *testing.T) {
			reg := testutil.CorpusRegistry(t)
			it := dalakosGrantedItem()
			seat := it.Scenario.Setup["p0"]
			switch zone {
			case "battlefield":
				seat.Battlefield = append(seat.Battlefield, "Vengeful Firebrand")
			case "hand":
				seat.Hand = append(seat.Hand, "Vengeful Firebrand")
			case "graveyard":
				seat.Graveyard = append(seat.Graveyard, "Vengeful Firebrand")
			}
			it.Scenario.Setup["p0"] = seat
			g := replayItem(t, reg, it)
			rq := forgeBuildRequest(reg, it, g, oraclegen.CastDivisions(reg, it.Scenario, g), false)
			got, ok := rq.Abilities["2"]
			if !ok {
				t.Fatalf("no exported line for step 2 with an unrelated same-text card in the %s: %v", zone, rq.Abilities)
			}
			if got != dragonMantlePumpLine {
				t.Errorf("step 2 line = %q, want the granted pump %q (not the printed index)", got, dragonMantlePumpLine)
			}
			c := census{}
			n := neutralFor(reg, rq, g, c)
			a, ok := n.Ability["2"]
			if !ok {
				t.Fatalf("no neutral ability for step 2 with an unrelated same-text card in the %s (census %v)", zone, map[string]int(c))
			}
			if a.Cost != "{R}" || !a.Granted {
				t.Errorf("neutral step 2 = %+v, want the granted {R} pump", a)
			}
		})
	}
}

// TestForgeExportOmitsUnresolvedGrantedLabel: a label-bearing granted step
// whose grant does not resolve must be omitted, never exported as the printed
// ability its aliased AbilityIndex points at. Here Dragon Mantle is absent, so
// step 2's pump label has no grant source.
func TestForgeExportOmitsUnresolvedGrantedLabel(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := dalakosGrantedItem()
	seat := it.Scenario.Setup["p0"]
	seat.Battlefield = []string{"Island", "Mountain", dalakosName}
	seat.Attached = nil
	it.Scenario.Setup["p0"] = seat
	lines := forgeAbilityLines(reg, it, rules.OracleResult{})
	if line, ok := lines["2"]; ok {
		t.Fatalf("step 2 exported %q for an unresolved granted label, want omitted (never the printed index's %q)", line, dalakosPrintedManaLine)
	}
	if _, ok := lines["0"]; !ok {
		t.Errorf("step 0 (Island mana, no grant label) lost its printed line: %v", lines)
	}
}

// dalakosTwoGrantItem is the pre-fix shape the reviewer reproduced: Dalakos
// carries two Auras whose granted pumps share the same SpellDescription
// ("CARDNAME gets +1/+0 until end of turn.") but cost {R} and {1}. The tape
// predates Step.Ability, so the step records only the aliased printed index 0;
// the label is identical for both grants, so the recovery cannot choose one.
func dalakosTwoGrantItem() oraclegen.Item {
	it := dalakosGrantedItem()
	seat := it.Scenario.Setup["p0"]
	seat.Battlefield = append(seat.Battlefield, "Draconic Destiny")
	seat.Attached = append(seat.Attached, oraclegen.SetupAttach{Card: "Draconic Destiny", To: "p0:" + dalakosName})
	it.Scenario.Setup["p0"] = seat
	it.Steps[2].Ability = ""
	return it
}

// TestForgeExportOmitsUnresolvedUnlabelledGrant is the reviewer's MAJOR: a
// pre-fix item carries no Step.Ability label, and two genuine grants with the
// same text are ambiguous. Both forge and neutral must omit the step rather
// than fall back to the aliased printed ability (Dalakos's {T} mana line).
func TestForgeExportOmitsUnresolvedUnlabelledGrant(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := dalakosTwoGrantItem()
	g := replayItem(t, reg, it)
	if !hasTranscriptLine(g.Transcript, "Dalakos, Crafter of Wonders: CARDNAME gets +1/+0 until end of turn.") {
		t.Fatalf("fixture did not activate a granted pump; transcript %q", g.Transcript)
	}
	lines := forgeAbilityLines(reg, it, g)
	if line, ok := lines["2"]; ok {
		t.Fatalf("step 2 exported %q for an ambiguous unlabelled grant, want omitted (never the printed index's %q)", line, dalakosPrintedManaLine)
	}
	if _, ok := lines["0"]; !ok {
		t.Errorf("step 0 (Island mana) lost its printed line: %v", lines)
	}
	c := census{}
	n := neutralFor(reg, forgeBuildRequest(reg, it, g, nil, false), g, c)
	if a, ok := n.Ability["2"]; ok {
		t.Fatalf("neutral step 2 = %+v for an ambiguous unlabelled grant, want omitted", a)
	}
	if c["ability.omit:granted_unresolved"] == 0 {
		t.Errorf("neutral did not count the omitted grant: %v", map[string]int(c))
	}
}

// hasTranscriptLine reports whether the runner's transcript contains line.
func hasTranscriptLine(lines []string, line string) bool {
	for _, l := range lines {
		if strings.Contains(l, line) {
			return true
		}
	}
	return false
}
