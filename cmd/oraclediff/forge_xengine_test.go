package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
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
// block step reaches the Forge driver as a pass_to the declaration step (the
// XMage port's skipEmptyDeclaration), the scenario SHA still names the
// unrewritten item, and a stated attack, a non-xengine item and the other
// steps are left alone.
func TestXengineForgeExportEmptyDeclaration(t *testing.T) {
	steps := []oraclegen.Step{
		{Op: "pass_to", Step: "main1"},
		{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{}, Expect: []oraclegen.Expect{{}}},
		{Op: "block", Seat: 0, Blocks: [][2]string{}},
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:Bear"}},
		{Op: "block", Seat: 1, Blocks: [][2]string{{"p1:Bear", "p0:Lion"}}},
	}
	mk := func(tmpl string) oraclegen.Item {
		return oraclegen.Item{ID: "xe:t", Template: tmpl, Scenario: oraclegen.Scenario{Steps: steps}}
	}
	got := skipEmptyDeclarations(mk(lower.Template))
	if g := got.Steps[1]; g.Op != "pass_to" || g.Step != "declare-attackers" || g.Active != "p1" || len(g.Expect) != 1 {
		t.Errorf("empty attack: %+v", g)
	}
	if g := got.Steps[2]; g.Op != "pass_to" || g.Step != "declare-blockers" || g.Active != "p1" {
		t.Errorf("empty block: %+v", g)
	}
	for _, i := range []int{0, 3, 4} {
		if got.Steps[i].Op != steps[i].Op {
			t.Errorf("step %d changed: %+v", i, got.Steps[i])
		}
	}
	if steps[1].Op != "attack" || steps[2].Op != "block" {
		t.Error("the input item's steps were mutated")
	}
	if g := skipEmptyDeclarations(mk(lower.WindowTemplate)); g.Steps[1].Op != "pass_to" {
		t.Errorf("window item not rewritten: %+v", g.Steps[1])
	}
	if g := skipEmptyDeclarations(mk("other")); g.Steps[1].Op != "attack" || g.Steps[2].Op != "block" {
		t.Errorf("non-xengine item rewritten: %+v", g.Steps)
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
	if rq.Item.Steps[1].Op != "pass_to" || rq.Item.Steps[2].Op != "pass_to" {
		t.Errorf("exported steps %+v", rq.Item.Steps)
	}
	if rq.ScenarioSHA != gate.ItemSHA(it) {
		t.Errorf("scenario_sha %s, want the unrewritten item's %s", rq.ScenarioSHA, gate.ItemSHA(it))
	}
}
