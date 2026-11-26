package main

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestForgeExportCipherDeclineIsNo: Writ of Return's cipher encode (CR
// 702.99a) is one KModes decision in gorge; the export hands the Forge driver
// the confirm it asks, answered "no" for a decline.
func TestForgeExportCipherDeclineIsNo(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	life := int32(20)
	zero := 0
	steps := []oraclegen.Step{}
	for _, c := range []string{"p0:Swamp", "p0:Swamp#2", "p0:Swamp#3", "p0:Swamp#4", "p0:Swamp#5"} {
		steps = append(steps, oraclegen.Step{Op: "activate", Seat: 0, Card: c, AbilityIndex: &zero})
	}
	steps = append(steps,
		oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:Writ of Return", Targets: []string{"p0:Memnite"}},
		oraclegen.Step{Op: "pass", Seat: 0},
		oraclegen.Step{Op: "pass", Seat: 1},
		oraclegen.Step{Op: "pass", Seat: 0, Answers: []oraclegen.Answer{{Kind: "modes", Pick: []string{}}}})
	it := oraclegen.Item{ID: "xs:XP:writ-of-return:cast:s1", Template: "xproof-start", Scenario: oraclegen.Scenario{
		Name: "writ", Turn: 1,
		Setup: map[string]oraclegen.Seat{
			"p0": {Battlefield: []string{"Swamp", "Swamp", "Swamp", "Swamp", "Swamp"}, Hand: []string{"Writ of Return"},
				Graveyard: []string{"Swamp", "Memnite"}, LibraryTop: []string{"Swamp", "Swamp", "Swamp"}, Life: &life},
			"p1": {Battlefield: []string{"Crookshank Kobolds"}, Life: &life}},
		Steps: steps}}
	rq, _ := forgeExportItem(reg, it, true)
	for _, d := range rq.GorgeDecisions {
		if d.Resume != "cipher" {
			continue
		}
		if d.Step != 7 || d.Kind != "choose_n" || !reflect.DeepEqual(d.PickKinds, []string{"no"}) {
			t.Errorf("cipher decision = %+v, want step 7 choose_n [no]", d)
		}
		return
	}
	t.Fatalf("no cipher decision exported: %+v", rq.GorgeDecisions)
}
