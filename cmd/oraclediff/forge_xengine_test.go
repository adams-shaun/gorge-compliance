package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
