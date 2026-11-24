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

// TestForgeFlippedCoin: the Forge request carries every coin flip the lowered
// item recorded, on the step that made it, so the driver can force the same
// result instead of flipping from its own stream (Mana Clash, SUP window
// xe:RAND:3:19:w7). Like a die roll, a flip is not a gorge decision and must
// not be exported as one, and it is absent from Raw(), which is all gorge's
// runner reads.
func TestForgeFlippedCoin(t *testing.T) {
	flips := []oraclegen.CoinFlip{
		{Step: 1, Seat: 0, Win: false},
		{Step: 1, Seat: 1, Win: true},
		{Step: 1, Seat: 0, Win: true, Forced: true},
		{Step: 1, Seat: 1, Win: true, Draws: 2},
	}
	it := oraclegen.Item{
		ID:        "xe:coin-flip",
		Template:  lower.Template,
		CoinFlips: flips,
		Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{
			{Op: "pass", Seat: 0},
			{Op: "cast", Seat: 0, Card: "p0:Mana Clash"},
		}},
	}
	rq := forgeBuildRequest(nil, it, rules.OracleResult{}, nil, true)
	if !reflect.DeepEqual(rq.Item.CoinFlips, flips) {
		t.Fatalf("request coin flips %+v, want %+v", rq.Item.CoinFlips, flips)
	}
	if len(rq.GorgeDecisions) != 0 {
		t.Fatalf("coin flip exported as a gorge decision: %+v", rq.GorgeDecisions)
	}
	if raw := string(rq.Item.Raw()); bytes.Contains([]byte(raw), []byte("coin_flips")) {
		t.Fatalf("Raw sends the Forge side field to gorge's runner: %s", raw)
	}
	b, err := json.Marshal(rq)
	if err != nil {
		t.Fatal(err)
	}
	const want = `"coin_flips":[{"step":1,"seat":0,"win":false},{"step":1,"seat":1,"win":true},{"step":1,"seat":0,"win":true,"forced":true},{"step":1,"seat":1,"win":true,"draws":2}]`
	if !bytes.Contains(b, []byte(want)) {
		t.Fatalf("request JSON lost the forced flips:\n%s", b)
	}
}
