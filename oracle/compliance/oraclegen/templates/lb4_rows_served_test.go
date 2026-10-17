package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestLevelBCostAndStaticRowsServed pins the wave-4 rows the cost-gap,
// raise-recipient and static-mode mechanisms serve (lb4-gcost), one row per
// mechanism. Each row must generate (no skip), replay through gorge with no
// fail, and show the mechanism's own observable in gorge's final snapshot, so
// a row can never be served by a scenario that does not exercise it.
func TestLevelBCostAndStaticRowsServed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	poolEmpty := func(seat int) func(*testing.T, oraclegen.Item, rules.OracleResult) {
		return func(t *testing.T, _ oraclegen.Item, res rules.OracleResult) {
			last := res.Snapshots[len(res.Snapshots)-1]
			for _, p := range last.Players {
				if p.Seat == seat && p.Pool != "" {
					t.Fatalf("p%d pool %q after the exactly priced probe: the raise was not applied", seat, p.Pool)
				}
			}
		}
	}
	for _, tc := range []struct {
		name, key, mechanism string
		check                func(*testing.T, oraclegen.Item, rules.OracleResult)
	}{
		{"Key to the Side-Door", "activate#0.1", "Discard sharesNameWith Valid: namesake legendary on the battlefield",
			func(t *testing.T, it oraclegen.Item, _ rules.OracleResult) {
				p0 := it.Scenario.Setup["p0"]
				if !containsString(p0.Hand, legendaryHandCard) || !containsString(p0.Battlefield, legendaryHandCard) {
					t.Fatalf("the namesake legendary is not in both p0's hand and battlefield: %+v", p0)
				}
			}},
		{"Rune-Brand Juggler", "activate#0.0", "Sac<1/Creature.IsSuspected>: the suspect prelude",
			func(t *testing.T, it oraclegen.Item, res rules.OracleResult) {
				if !stepsCast(it.Scenario.Steps, suspectSelfMaker) {
					t.Fatalf("no suspect-maker cast in the prelude: %+v", it.Scenario.Steps)
				}
				if !graveNames(res, suspectSelfMaker) {
					t.Fatalf("the suspected %s was not sacrificed", suspectSelfMaker)
				}
			}},
		{"Transmutation Font", "activate#0.1", "Sac<3/Artifact.token+WithDifferentNames>: three distinct token makers",
			func(t *testing.T, _ oraclegen.Item, res rules.OracleResult) {
				for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
					if strings.Contains(p.Ref, ":token:") {
						t.Fatalf("token %s survived the with-different-names sacrifice", p.Ref)
					}
				}
			}},
		{"Dáin, Lord of the Iron Hills", "static#0.0", "CantAttackUnless Condition$ EnduringStory: the Storied fixture", nil},
		{"Tapestry Warden", "static#0.1", "TapPowerValue Station: charge counters equal to toughness",
			func(t *testing.T, _ oraclegen.Item, res rules.OracleResult) {
				p, ok := permByNameIn(res, stationProbe)
				if !ok || p.Counters["CHARGE"] != 4 {
					t.Fatalf("%s holds %v, want 4 CHARGE (Tapestry Warden's toughness)", stationProbe, p.Counters)
				}
			}},
		{"Grievous Wound", "static#0.0", "CantGainLife Player.EnchantedBy: the Aura cast onto p0",
			func(t *testing.T, _ oraclegen.Item, res rules.OracleResult) {
				if life := lifeOf(res, 0); life != 20 {
					t.Fatalf("enchanted p0 is at %d life after the life-gain probe, want 20", life)
				}
			}},
		{"Mimeoplasm, Revered One", "activate#0.0", "ExiledWithSource target made by the source's own cast",
			func(t *testing.T, _ oraclegen.Item, res rules.OracleResult) {
				for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
					if p.Ref == "p0:Mimeoplasm, Revered One" && p.Name != exiledWithCastCard {
						t.Fatalf("Mimeoplasm is %q, want a copy of %s", p.Name, exiledWithCastCard)
					}
				}
			}},
		{"Kaito, Bane of Nightmares", "activate#0.3", "Ninjutsu: an unblocked attacker returned",
			func(t *testing.T, _ oraclegen.Item, res rules.OracleResult) {
				p, ok := permByNameIn(res, "Kaito, Bane of Nightmares")
				if !ok || !p.Attacking || !p.Tapped {
					t.Fatalf("Kaito did not enter tapped and attacking: %+v", p)
				}
			}},
		{"A Killer Among Us", "activate#0.0", "RevealChosen: the secret choice made by the source's cast, the token attacking",
			func(t *testing.T, _ oraclegen.Item, res rules.OracleResult) {
				p, ok := permByNameIn(res, "Human Token")
				if !ok || p.Counters["P1P1"] != 3 {
					t.Fatalf("the chosen-type Human token holds %v, want P1P1=3", p.Counters)
				}
			}},
		{"Leyline of Mutation", "static#0.0", "AlternativeCost for your spells", poolEmpty(0)},
		{"Kíli the Resourceful", "static#0.0", "AlternativeCost {0} equip under an enduring story", nil},
		{"Agatha's Soul Cauldron", "static#0.1", "abilities of cards exiled with the source on countered creatures",
			func(t *testing.T, _ oraclegen.Item, res rules.OracleResult) {
				if life := lifeOf(res, 1); life != 19 {
					t.Fatalf("p1 at %d life, want 19 from the gained ping", life)
				}
			}},
		{"Elvish Refueler", "static#0.0", "Activations MinLimit$ -1: exhaust re-activated next turn",
			func(t *testing.T, _ oraclegen.Item, res rules.OracleResult) {
				p, ok := permByNameIn(res, "Elvish Refueler")
				if !ok || p.Counters["P1P1"] != 2 {
					t.Fatalf("Elvish Refueler holds %v, want two exhaust activations' P1P1=2", p.Counters)
				}
			}},
		{"Aven Interrupter", "static#0.0", "RaiseCost on an opponent's graveyard cast", poolEmpty(1)},
		{"Skyseer's Chariot", "static#0.0", "RaiseCost on the chosen name's activated abilities", poolEmpty(0)},
		{"Arachne, Psionic Weaver", "static#0.0", "RaiseCost on the chosen card type", poolEmpty(0)},
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

// TestDainEnduringStoryGatesTheTax is the Dáin row's control: without the
// Storied fixture's two artifacts the enduring story never starts, the tax
// does not apply, and the want=false attack expectation fails -- so the row
// observes the gate, not a blanket tax.
func TestDainEnduringStoryGatesTheTax(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Dáin, Lord of the Iron Hills"
	it, skip := GenerateB(reg, name, requirementFor(t, reg, name, "static#0.0"))
	if skip != nil {
		t.Fatalf("skipped: %s", skip.Reason)
	}
	sc := it.Scenario
	p1 := sc.Setup["p1"]
	var kept []string
	for _, c := range p1.Battlefield {
		if c != "Sol Ring" && c != "Arcane Signet" {
			kept = append(kept, c)
		}
	}
	if len(kept) == len(p1.Battlefield) {
		t.Fatalf("precondition: the Storied fixture is not on p1's battlefield: %v", p1.Battlefield)
	}
	p1.Battlefield = kept
	sc.Setup = map[string]oraclegen.Seat{"p0": sc.Setup["p0"], "p1": p1}
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) == 0 {
		t.Fatalf("without an enduring story the attack is still taxed (ok=%v fails=%v)", ok, res.Fails)
	}
}

func stepsCast(steps []oraclegen.Step, card string) bool {
	for _, s := range steps {
		if s.Op == "cast" && strings.TrimPrefix(s.Card, "p0:") == card {
			return true
		}
	}
	return false
}

func graveNames(res rules.OracleResult, card string) bool {
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Players {
		if p.Seat == 0 && containsString(p.Graveyard, card) {
			return true
		}
	}
	return false
}
