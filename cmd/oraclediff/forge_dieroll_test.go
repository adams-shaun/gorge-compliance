package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
)

// TestForgeRolledDie: the Forge request carries every die roll the lowered
// item recorded, on the step that made it, so the driver can force the same
// natural roll instead of rolling its own. The roll is not a gorge decision
// (Forge poses no decision for a die), so it must not be exported as one: a
// decision the driver never consumes would be an unconsumed leftover. The
// side field is also absent from Raw(), which is all gorge's runner reads.
// The driver-side half of the behaviour -- that MyRandom.setRandom's wrapper
// forwards the bounded draw to the installed generator -- is pinned by
// forge-oracle's ForcedDieRollTest.productionSetRandomForwardsTheBound.
func TestForgeRolledDie(t *testing.T) {
	roll := oraclegen.DieRoll{Step: 1, Seat: 0, Sides: 6, Natural: 3, Result: 3}
	it := oraclegen.Item{
		ID:       "xe:die-roll",
		Template: lower.Template,
		DieRolls: []oraclegen.DieRoll{roll},
		Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{
			{Op: "pass", Seat: 0},
			{Op: "activate", Seat: 0, Card: "p0:Ebony Fly"},
		}},
	}
	rq := forgeBuildRequest(nil, it, rules.OracleResult{}, nil, true)
	if !reflect.DeepEqual(rq.Item.DieRolls, []oraclegen.DieRoll{roll}) {
		t.Fatalf("request die rolls %+v, want %+v", rq.Item.DieRolls, roll)
	}
	if len(rq.GorgeDecisions) != 0 {
		t.Fatalf("die roll exported as a gorge decision: %+v", rq.GorgeDecisions)
	}
	if raw := string(rq.Item.Raw()); bytes.Contains([]byte(raw), []byte("die_rolls")) {
		t.Fatalf("Raw sends the Forge side field to gorge's runner: %s", raw)
	}
	b, err := json.Marshal(rq)
	if err != nil {
		t.Fatal(err)
	}
	const want = `"die_rolls":[{"step":1,"seat":0,"sides":6,"natural":3,"result":3}]`
	if !bytes.Contains(b, []byte(want)) {
		t.Fatalf("request JSON lost the forced roll:\n%s", b)
	}
}
