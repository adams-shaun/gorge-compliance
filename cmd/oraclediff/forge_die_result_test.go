package main

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestForgeExportChooseDieResultIsNumber: Valiant Endeavor's choose-one-result
// is a "die k: result r" pick in gorge; the export hands the Forge driver the
// number its chooseNumber ask reads.
func TestForgeExportChooseDieResultIsNumber(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	life := int32(20)
	zero := 0
	steps := []oraclegen.Step{}
	for _, c := range []string{"p0:Plains", "p0:Plains#2", "p0:Plains#3", "p0:Plains#4", "p0:Plains#5", "p0:Plains#6"} {
		steps = append(steps, oraclegen.Step{Op: "activate", Seat: 0, Card: c, AbilityIndex: &zero})
	}
	steps = append(steps,
		oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:Valiant Endeavor"},
		oraclegen.Step{Op: "pass", Seat: 0},
		oraclegen.Step{Op: "pass", Seat: 1},
		oraclegen.Step{Op: "pass", Seat: 0, Answers: []oraclegen.Answer{{Kind: "choose", Pick: []string{"die 1: result 3"}}}})
	lib := make([]string, 20)
	for i := range lib {
		lib[i] = "Plains"
	}
	it := oraclegen.Item{ID: "xs:XP:valiant-endeavor:cast:s1", Template: "xproof-start",
		DieRolls: []oraclegen.DieRoll{
			{Step: 8, Seat: 0, Sides: 6, Natural: 3, Result: 3},
			{Step: 8, Seat: 0, Sides: 6, Natural: 6, Result: 6}},
		Scenario: oraclegen.Scenario{Name: "valiant", Turn: 1,
			Setup: map[string]oraclegen.Seat{
				"p0": {Battlefield: []string{"Plains", "Plains", "Plains", "Plains", "Plains", "Plains"}, Hand: []string{"Valiant Endeavor"}, LibraryTop: lib, Life: &life},
				"p1": {LibraryTop: lib, Life: &life}},
			Steps: steps}}
	rq, _ := forgeExportItem(reg, it, true)
	for _, d := range rq.GorgeDecisions {
		if d.Resume != "roll" {
			continue
		}
		if d.Step != 8 || d.Kind != "choose_n" || !reflect.DeepEqual(d.Picks, []string{"3"}) || !reflect.DeepEqual(d.PickKinds, []string{"number"}) {
			t.Errorf("roll decision = %+v, want step 8 choose_n [3] kind number", d)
		}
		return
	}
	t.Fatalf("no roll decision exported: %+v", rq.GorgeDecisions)
}
