package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// lowerKeyStarts writes the start scen makes for each (card, key) into a
// directory, plays them with playStart, and returns the lowered items by card
// slug (the first seed's) with the gorge rows by item id.
func lowerKeyStarts(t *testing.T, keys [][2]string) (map[string]oraclegen.Item, map[string]rules.OracleResult) {
	t.Helper()
	reg, b := certdeckBuilder(t)
	dir, out := t.TempDir(), t.TempDir()
	for _, k := range keys {
		res, err := scen.Generate(reg, b, k[0], k[1], scen.Options{Prefer: "start"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Path != scen.PathStart {
			t.Fatalf("%s %s: path %q reason %q, want a start", k[0], k[1], res.Path, res.Reason)
		}
		raw, err := res.Start.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, scen.Slug(k[0])+"-"+scen.KeyID(k[1])+".json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := playStart(reg, []string{dir}, "XP", out, false); err != nil {
		t.Fatal(err)
	}
	items, gorge := map[string]oraclegen.Item{}, map[string]rules.OracleResult{}
	if err := readLines(filepath.Join(out, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		items[it.ID] = it
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := readLines(filepath.Join(out, "gorge.jsonl"), func(b []byte) error {
		var r gorgeRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		gorge[r.ID] = r.Result
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return items, gorge
}

func stepOps(it oraclegen.Item, op string) []oraclegen.Step {
	var out []oraclegen.Step
	for _, s := range it.Steps {
		if s.Op == op {
			out = append(out, s)
		}
	}
	return out
}

// TestPlayStartCombatStatic lowers a start of each new family and holds the
// item to the key: an attack key's item attacks with the card, a block key's
// item blocks the fixture's attacker with the card (forced, whatever the
// defender's bot would do), and a static start's item carries the comparison
// that sees its effect.
func TestPlayStartCombatStatic(t *testing.T) {
	items, gorge := lowerKeyStarts(t, [][2]string{
		{"Magnigoth Sentry", "combat#0.attack"},
		{"Magnigoth Sentry", "combat#0.block"},
		{"Suntail Hawk", "combat#0.block"},
		{"Aggressive Mammoth", "static#0.0"},
		{"Primal Rage", "static#0.0"},
		{"Glorious Anthem", "static#0.0"},
	})
	for _, id := range []string{
		"xs:XP:magnigoth-sentry:combat0.attack:s1", "xs:XP:magnigoth-sentry:combat0.block:s1", "xs:XP:suntail-hawk:combat0.block:s1",
		"xs:XP:aggressive-mammoth:static0.0:s1", "xs:XP:primal-rage:static0.0:s1", "xs:XP:glorious-anthem:static0.0:s1",
	} {
		if _, ok := items[id]; !ok {
			t.Fatalf("no lowered item %s (have %d items)", id, len(items))
		}
	}

	atk := stepOps(items["xs:XP:magnigoth-sentry:combat0.attack:s1"], "attack")
	if len(atk) != 1 || atk[0].Seat != 0 || !slices.Equal(atk[0].Attackers, []string{"p0:Magnigoth Sentry"}) {
		t.Errorf("attack key: attack steps %+v, want seat 0 attacking with the card", atk)
	}
	for _, id := range []string{"xs:XP:magnigoth-sentry:combat0.block:s1", "xs:XP:suntail-hawk:combat0.block:s1"} {
		it := items[id]
		card := "Magnigoth Sentry"
		if id == "xs:XP:suntail-hawk:combat0.block:s1" {
			card = "Suntail Hawk"
		}
		blk := stepOps(it, "block")
		if len(blk) != 1 || blk[0].Seat != 1 || len(blk[0].Blocks) != 1 || blk[0].Blocks[0] != [2]string{"p1:" + card, "p0:Grizzly Bears"} {
			t.Errorf("%s: block steps %+v, want seat 1 blocking Grizzly Bears with %s", id, blk, card)
		}
		if a := stepOps(it, "attack"); len(a) != 1 || !slices.Equal(a[0].Attackers, []string{"p0:Grizzly Bears"}) {
			t.Errorf("%s: attack steps %+v, want the fixture attacking", id, a)
		}
	}

	// a keyword grant is seen only with the keyword comparison
	for _, id := range []string{"xs:XP:aggressive-mammoth:static0.0:s1", "xs:XP:primal-rage:static0.0:s1"} {
		it := items[id]
		if !slices.Equal(it.Compare, []string{oraclediff.CompareKeywords}) {
			t.Errorf("%s: compare %v, want [keywords]", id, it.Compare)
			continue
		}
		g, err := rules.RunOracleScenarioJSON(testutil.CorpusRegistry(t), it.Raw())
		if err != nil {
			t.Fatal(err)
		}
		// the other engine's snapshots: the same, with every Trample dropped
		x := stripKeyword(t, gorge[id].Snapshots, "Trample")
		if v := oraclediff.CompareOpts(g, nil, oraclediff.XResult{ID: id, Snapshots: x}, it.Compare); v.Status == oraclediff.Agree {
			t.Errorf("%s: a missing trample agrees under compare %v", id, it.Compare)
		}
		if v := oraclediff.CompareOpts(g, nil, oraclediff.XResult{ID: id, Snapshots: x}, nil); v.Status != oraclediff.Agree {
			t.Errorf("%s: without the keyword compare the missing trample is already seen (%s): the test shows nothing", id, v.Status)
		}
	}
	// a pump is seen in P/T: no opt-in
	if c := items["xs:XP:glorious-anthem:static0.0:s1"].Compare; len(c) != 0 {
		t.Errorf("Glorious Anthem: compare %v, want none", c)
	}
}

// stripKeyword is snaps with keyword kw removed from every permanent.
func stripKeyword(t *testing.T, snaps []rules.OracleSnapshot, kw string) []rules.OracleSnapshot {
	t.Helper()
	b, err := json.Marshal(snaps)
	if err != nil {
		t.Fatal(err)
	}
	var out []rules.OracleSnapshot
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	n := 0
	for i := range out {
		for j := range out[i].Permanents {
			p := &out[i].Permanents[j]
			if k := slices.Index(p.Keywords, kw); k >= 0 {
				p.Keywords = slices.Delete(p.Keywords, k, k+1)
				n++
			}
		}
	}
	if n == 0 {
		t.Fatalf("no permanent has %s: the start does not show the grant", kw)
	}
	return out
}
