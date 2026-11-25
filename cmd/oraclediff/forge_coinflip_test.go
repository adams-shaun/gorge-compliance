package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/testkit/testutil"
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

// forgeOpsOf lists the step ops of a request, for failure messages.
func forgeOpsOf(rq forgeRequest) []string {
	var out []string
	for _, s := range rq.Item.Steps {
		out = append(out, fmt.Sprintf("%s/p%d", s.Op, s.Seat))
	}
	return out
}

// wantUpkeepFlipStep fails unless the request's only flip is the lost flip on
// the resolving seat-1 pass that follows two mana activations and the seat-0
// pass in the same priority window (xproof rng xs:XP:goblin-kaboomist:f0t0:s2:
// Goblin Kaboomist's upkeep FlipCoin | LoseSubAbility$ DmgSelf, activations at
// steps 34-35, the passes at 36-37, the lost flip on step 37). The driver arms
// a flip by step index, so a flip on any other step is Forge's own to make.
func wantUpkeepFlipStep(t *testing.T, rq forgeRequest, wantStep int) {
	t.Helper()
	if len(rq.Item.CoinFlips) != 1 {
		t.Fatalf("request coin flips %+v, want the one lost flip", rq.Item.CoinFlips)
	}
	f := rq.Item.CoinFlips[0]
	if f.Step != wantStep {
		t.Fatalf("flip names step %d, want %d (steps %v)", f.Step, wantStep, forgeOpsOf(rq))
	}
	if f.Win || f.Seat != 0 || f.Draws != 1 {
		t.Fatalf("flip %+v, want a lost one-draw flip of seat 0", f)
	}
	steps := rq.Item.Steps
	if f.Step < 3 || f.Step >= len(steps) {
		t.Fatalf("flip step %d of %d", f.Step, len(steps))
	}
	for k, want := range []struct {
		op   string
		seat int
	}{{"activate", 0}, {"activate", 0}, {"pass", 0}, {"pass", 1}} {
		if st := steps[f.Step-3+k]; st.Op != want.op || st.Seat != want.seat {
			t.Fatalf("step %d is %s/p%d, want %s/p%d: the flip rides the resolving pass after two activations (steps %v)",
				f.Step-3+k, st.Op, st.Seat, want.op, want.seat, forgeOpsOf(rq))
		}
	}
	if len(rq.GorgeDecisions) != 0 {
		t.Fatalf("coin flip exported as a gorge decision: %+v", rq.GorgeDecisions)
	}
	b, err := json.Marshal(rq)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`"coin_flips":[{"step":%d,"seat":0,"win":false,"draws":1}]`, wantStep)
	if !bytes.Contains(b, []byte(want)) {
		t.Fatalf("request JSON lost the lost flip, want %s:\n%s", want, b)
	}
}

// TestForgeFlippedCoinOnTriggerResolutionAfterManaActivations lowers a tape
// shaped like Goblin Kaboomist's upkeep (two mana activations, then the pass
// pair that resolves the trigger, the flip recorded on the second pass) and
// follows it through the exporter: the flip must stay on the resolving pass,
// at the step index the request's step list has it, after the rewrites that
// run around activations. This is the Go half of the find: lower.markCoinFlips
// stamps the step and forgeBuildRequest must not move it.
func TestForgeFlippedCoinOnTriggerResolutionAfterManaActivations(t *testing.T) {
	// Every entry carries its own digest (the library count differs), so the
	// lowering sees each activation change the state and keeps it as a step.
	dg := func(n int) game.Digest {
		return game.Digest{Turn: 1, Step: "main1", Players: []game.PlayerDigest{{Seat: 0, LibraryCount: n}}}
	}
	pass := func(seat, n int, top state.ObjID) game.Entry {
		return game.Entry{Kind: "priority", Seat: seat, Turn: 1, Step: "main1", Digest: dg(n), StackTop: top,
			Picks: []game.Pick{{Kind: "pass", Player: seat}}}
	}
	mana := func(n int, obj state.ObjID, ref string) game.Entry {
		return game.Entry{Kind: "priority", Seat: 0, Turn: 1, Step: "main1", Digest: dg(n),
			Picks: []game.Pick{{Kind: "ability", Player: 0, Obj: obj, Ref: ref, Ability: 0, Label: "{T}: Add {R}."}}}
	}
	const trig = state.ObjID(77)
	resolve := pass(1, 5, trig)
	resolve.RNG = 1
	resolve.Flips = []game.Flip{{Seat: 0, Win: false, Draws: 1}}
	tp := &game.Tape{
		Format: game.TapeFormat, Starter: 0,
		Entries: []game.Entry{
			pass(0, 1, 0),
			mana(2, 10, "p0:Mountain"),
			mana(3, 11, "p0:Mountain#2"),
			pass(0, 4, trig),
			resolve,
			pass(0, 6, 0),
		},
	}
	l, err := lower.Tape(nil, tp)
	if err != nil {
		t.Fatal(err)
	}
	if l.Truncated != "" {
		t.Fatalf("truncated %q at %d", l.Truncated, l.TruncatedAt)
	}
	rq := forgeBuildRequest(testutil.CorpusRegistry(t), l.Item, rules.OracleResult{}, nil, true)
	// The lowered steps: pass, activate, activate, pass (p0), pass (p1, the flip), pass.
	wantUpkeepFlipStep(t, rq, 4)
	if want := l.Item.CoinFlips[0].Step; rq.Item.CoinFlips[0].Step != want {
		t.Fatalf("exporter moved the lowered flip from step %d to %d", want, rq.Item.CoinFlips[0].Step)
	}
}

// TestForgeFlippedCoinKaboomistUpkeepFind is the recorded find itself
// (testdata/forge/goblin-kaboomist-upkeep-flip.jsonl is its scenario.json
// verbatim: 38 steps, the lost flip on step 37, activations at 34-35). The
// request keeps the flip on step 37, the seat-1 pass that resolves the upkeep
// trigger, with the two activations and the seat-0 pass in front of it.
func TestForgeFlippedCoinKaboomistUpkeepFind(t *testing.T) {
	raw, err := os.ReadFile("testdata/forge/goblin-kaboomist-upkeep-flip.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var it oraclegen.Item
	if err := json.Unmarshal(bytes.TrimSpace(raw), &it); err != nil {
		t.Fatal(err)
	}
	if it.Card != "Goblin Kaboomist" || len(it.Steps) != 38 {
		t.Fatalf("fixture is %q with %d steps, want Goblin Kaboomist with 38", it.Card, len(it.Steps))
	}
	rq := forgeBuildRequest(testutil.CorpusRegistry(t), it, rules.OracleResult{}, nil, true)
	if len(rq.Item.Steps) != 38 {
		t.Fatalf("the rewrites changed the step count to %d: the driver arms the flip by index", len(rq.Item.Steps))
	}
	wantUpkeepFlipStep(t, rq, 37)
}
