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

// TestForgeFlippedCoinAfterManaActivations: a trigger's flip resolves on the
// pass that lets it resolve, after the mana activations spent in the same
// priority window (xproof rng xs:XP:goblin-kaboomist:f0t0:s2: Goblin
// Kaboomist's upkeep FlipCoin | LoseSubAbility$ DmgSelf, activations at steps
// 34-35, the passes at 36-37, the lost flip on step 37). The request must keep
// the flip on that last pass, at the same index the step list has it, and the
// rewrites around activations must not move it: the driver arms the flip by
// that step index, and a flip armed on any other step is Forge's own
// (Kaboomist survives a flip gorge lost).
func TestForgeFlippedCoinAfterManaActivations(t *testing.T) {
	flips := []oraclegen.CoinFlip{{Step: 4, Seat: 0, Win: false, Draws: 1}}
	it := oraclegen.Item{
		ID:        "xs:XP:goblin-kaboomist:coin",
		Template:  lower.Template,
		CoinFlips: flips,
		Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{
			{Op: "pass", Seat: 1},
			{Op: "activate", Seat: 0, Card: "p0:Mountain"},
			{Op: "activate", Seat: 0, Card: "p0:Mountain#2"},
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
		}},
	}
	rq := forgeBuildRequest(nil, it, rules.OracleResult{}, nil, true)
	if !reflect.DeepEqual(rq.Item.CoinFlips, flips) {
		t.Fatalf("request coin flips %+v, want %+v", rq.Item.CoinFlips, flips)
	}
	f := rq.Item.CoinFlips[0]
	if f.Step < 0 || f.Step >= len(rq.Item.Steps) {
		t.Fatalf("flip names step %d of %d", f.Step, len(rq.Item.Steps))
	}
	if st := rq.Item.Steps[f.Step]; st.Op != "pass" || st.Seat != 1 {
		t.Fatalf("flip step %d is %+v, want the resolving seat-1 pass after the activations", f.Step, st)
	}
	for i, want := range []string{"pass", "activate", "activate", "pass", "pass"} {
		if got := rq.Item.Steps[i].Op; got != want {
			t.Fatalf("step %d is %q after the rewrites, want %q: the driver arms the flip by index", i, got, want)
		}
	}
	if len(rq.GorgeDecisions) != 0 {
		t.Fatalf("coin flip exported as a gorge decision: %+v", rq.GorgeDecisions)
	}
	b, err := json.Marshal(rq)
	if err != nil {
		t.Fatal(err)
	}
	const want = `"coin_flips":[{"step":4,"seat":0,"win":false,"draws":1}]`
	if !bytes.Contains(b, []byte(want)) {
		t.Fatalf("request JSON lost the lost flip:\n%s", b)
	}
}
