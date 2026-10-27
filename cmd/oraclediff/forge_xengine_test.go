package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestForgeExportXengineItem: forge-export takes the items an xengine play
// writes unchanged. Two FDN games are played and lowered the way `xengine
// play` does; every request line decodes, carries gorge_decisions (a list, not
// null) and has an abilities entry for every activate step whose card resolves
// to a corpus ability.
func TestForgeExportXengineItem(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cands, _, err := decks.Candidates(reg, "../../oracle/compliance/manifests", "FDN")
	if err != nil {
		t.Fatal(err)
	}
	led := cov.New()
	var items bytes.Buffer
	for g := 0; g < 2; g++ {
		pair := decks.Pair(cands, led, "FDN", 0, g, 1)
		tp, err := game.Play(reg, pair, 1_000_003+uint64(g), game.Options{Set: "FDN"})
		if err != nil {
			t.Fatal(err)
		}
		l, err := lower.Tape(reg, tp)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(l.Item)
		items.Write(append(b, '\n'))
	}
	dir := t.TempDir()
	scen, out := filepath.Join(dir, "items.jsonl"), filepath.Join(dir, "req.jsonl")
	if err := os.WriteFile(scen, items.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := forgeExport(reg, scen, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("exported %d requests, want 2", n)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	reqs := decodeRequests(t, raw)
	activates := 0
	for i, line := range bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n")) {
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(line, &keys); err != nil {
			t.Fatal(err)
		}
		if gd := string(keys["gorge_decisions"]); gd == "" || gd == "null" {
			t.Errorf("request %d: gorge_decisions = %q, want a list", i, gd)
		}
		rq := reqs[i]
		if rq.Item.Template != lower.Template || rq.Abilities == nil {
			t.Errorf("request %d: template %q, abilities %v", i, rq.Item.Template, rq.Abilities)
		}
		for s, st := range rq.Item.Steps {
			if st.Op != "activate" || st.AbilityIndex == nil {
				continue
			}
			activates++
			c, ok := reg.Lookup(refCardName(st.Card))
			if !ok || len(c.Faces) == 0 {
				continue
			}
			f := c.Faces[0]
			if k := *st.AbilityIndex; k >= 0 && k < len(f.Abilities) && f.Abilities[k] != nil {
				if rq.Abilities[fmt.Sprint(s)] == "" {
					t.Errorf("request %d step %d (%s ability %d): no abilities entry", i, s, st.Card, k)
				}
			}
		}
	}
	if activates == 0 {
		t.Fatal("no activate step in the exported tapes: the test exercises nothing")
	}
}

// TestXengineForgeExportEmptyDeclaration: an xengine item's empty attack or
// block step reaches the Forge driver as a pass_to decision "priority" (which
// the driver completes without consuming a priority), the pass before a
// stated attack or block becomes a pass_to of that decision (which completes
// inside Forge's declaration callback), the scenario SHA still names the
// unrewritten item, and a non-xengine item and the other steps are left alone.
func TestXengineForgeExportEmptyDeclaration(t *testing.T) {
	steps := []oraclegen.Step{
		{Op: "pass_to", Step: "main1"},
		{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{}, Expect: []oraclegen.Expect{{}}},
		{Op: "block", Seat: 0, Blocks: [][2]string{}},
		{Op: "pass", Seat: 1},
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:Bear"}},
		{Op: "pass", Seat: 0},
		{Op: "block", Seat: 1, Blocks: [][2]string{{"p1:Bear", "p0:Lion"}}},
		{Op: "attack", Seat: 0, Defender: "p1"},
	}
	ops := []string{"pass_to", "attack", "block", "pass", "attack", "pass", "block", "attack"}
	mk := func(tmpl string) oraclegen.Item {
		return oraclegen.Item{ID: "xe:t", Template: tmpl, Scenario: oraclegen.Scenario{Steps: steps}}
	}
	got := forgeDeclarations(mk(lower.Template))
	if len(got.Steps) != len(steps) {
		t.Fatalf("step count %d, want %d", len(got.Steps), len(steps))
	}
	for _, i := range []int{1, 2, 7} {
		if g := got.Steps[i]; g.Op != "pass_to" || g.Decision != "priority" || g.Step != "" || g.Active != "" {
			t.Errorf("empty declaration %d: %+v", i, g)
		}
	}
	if g := got.Steps[1]; len(g.Expect) != 1 {
		t.Errorf("empty attack lost its expectations: %+v", g)
	}
	for i, want := range map[int]string{3: "attackers", 5: "blockers"} {
		if g := got.Steps[i]; g.Op != "pass_to" || g.Decision != want {
			t.Errorf("pass %d before a stated declaration: %+v, want pass_to %s", i, g, want)
		}
	}
	for _, i := range []int{0, 4, 6} {
		if got.Steps[i].Op != ops[i] {
			t.Errorf("step %d changed: %+v", i, got.Steps[i])
		}
	}
	for i, op := range ops {
		if steps[i].Op != op {
			t.Fatalf("the input item's step %d was mutated: %+v", i, steps[i])
		}
	}
	if g := forgeDeclarations(mk(lower.WindowTemplate)); g.Steps[1].Op != "pass_to" {
		t.Errorf("window item not rewritten: %+v", g.Steps[1])
	}
	for i, st := range forgeDeclarations(mk("other")).Steps {
		if st.Op != ops[i] {
			t.Errorf("non-xengine item rewritten at %d: %+v", i, st)
		}
	}

	reg := testutil.CorpusRegistry(t)
	dir := t.TempDir()
	scen, out := filepath.Join(dir, "items.jsonl"), filepath.Join(dir, "req.jsonl")
	it := mk(lower.Template)
	b, _ := json.Marshal(it)
	if err := os.WriteFile(scen, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := forgeExport(reg, scen, out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	rq := decodeRequests(t, raw)[0]
	if rq.Item.Steps[1].Op != "pass_to" || rq.Item.Steps[1].Decision != "priority" || rq.Item.Steps[5].Decision != "blockers" {
		t.Errorf("exported steps %+v", rq.Item.Steps)
	}
	if rq.ScenarioSHA != gate.ItemSHA(it) {
		t.Errorf("scenario_sha %s, want the unrewritten item's %s", rq.ScenarioSHA, gate.ItemSHA(it))
	}
}

// TestForgeDropsEmptyDeclarationDecisions: an empty attack or block step is
// rewritten to a pass_to "priority" the driver never poses as a declaration,
// so gorge's attackers/blockers decision for that step must not travel in the
// request (the driver would report it unconsumed). A stated declaration's
// decision, other kinds at the same step, and non-xengine items are kept.
func TestForgeDropsEmptyDeclarationDecisions(t *testing.T) {
	steps := []oraclegen.Step{
		{Op: "pass_to", Step: "main1"},
		{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{}},
		{Op: "block", Seat: 0, Blocks: [][2]string{}},
		{Op: "pass", Seat: 0},
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:Bear"}},
	}
	ds := []rules.OracleDecision{
		{Step: 1, Kind: "attackers"},
		{Step: 1, Kind: "target"},
		{Step: 2, Kind: "blockers"},
		{Step: 2, Kind: "attackers"},
		{Step: 4, Kind: "attackers", Picks: []string{"Attack with Bear at b"}},
	}
	mk := func(tmpl string) oraclegen.Item {
		return oraclegen.Item{ID: "xe:t", Template: tmpl, Scenario: oraclegen.Scenario{Steps: steps}}
	}
	got := forgeDropEmptyDeclarations(mk(lower.Template), ds)
	want := []rules.OracleDecision{ds[1], ds[3], ds[4]}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decisions %+v, want %+v", got, want)
	}
	if len(ds) != 5 || ds[0].Kind != "attackers" {
		t.Errorf("the input decisions were mutated: %+v", ds)
	}
	if got := forgeDropEmptyDeclarations(mk("other"), ds); len(got) != len(ds) {
		t.Errorf("non-xengine item lost decisions: %+v", got)
	}

	reg := testutil.CorpusRegistry(t)
	dir := t.TempDir()
	scen, out := filepath.Join(dir, "items.jsonl"), filepath.Join(dir, "req.jsonl")
	b, _ := json.Marshal(mk(lower.Template))
	if err := os.WriteFile(scen, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := forgeExport(reg, scen, out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range decodeRequests(t, raw)[0].GorgeDecisions {
		if d.Step == 1 && d.Kind == "attackers" || d.Step == 2 && d.Kind == "blockers" {
			t.Errorf("exported a leftover decision for an empty declaration: %+v", d)
		}
	}
}
