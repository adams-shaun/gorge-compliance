package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/follower"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/xprep"
)

// TestDiffReadsFollowerPrep: for a follower-export follower, an item in the
// play dir's <engine>-prep.jsonl with no row is class not_sent (the refusal's
// reason as its message), as xprep.jsonl is for XMage; an item with no row
// and no refusal stays harness. Forge's forge-export refuses nothing, so a
// forge-prep.jsonl changes nothing for Forge.
func TestDiffReadsFollowerPrep(t *testing.T) {
	reg, play := playTwo(t)
	defer follower.RegisterForTest(follower.Follower{
		Name: "fakefol", Display: "FakeFol", Runner: "scripts/fakefol-oracle-run.sh", Prep: follower.PrepFollowerExport,
	})()
	tmp := t.TempDir()
	rows := filepath.Join(tmp, "rows.jsonl")
	var ids []string
	goldenRows(t, play, rows, func(i int, x *oraclediff.XResult) { ids = append(ids, x.ID) })
	if len(ids) < 2 {
		t.Fatalf("play kept %d items, want at least 2", len(ids))
	}
	refused, missing := ids[0], ids[1]
	// Drop both items' rows; refuse only the first.
	var kept []byte
	if err := readLines(rows, func(b []byte) error {
		var x oraclediff.XResult
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		if x.ID != refused && x.ID != missing {
			kept = append(append(kept, b...), '\n')
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rows, kept, 0o644); err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(xprep.Refusal{ID: refused, Reason: "fakefol_lacks_card", Detail: "Some Card: not in the fake database"})
	for _, name := range []string{"fakefol-prep.jsonl", "forge-prep.jsonl"} {
		if err := os.WriteFile(filepath.Join(play, name), append(line, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(tmp, "fakefol")
	if err := diff(reg, play, "fakefol", rows, out, ""); err != nil {
		t.Fatal(err)
	}
	classes := verdictClasses(t, filepath.Join(out, "verdicts.jsonl"))
	for _, id := range ids {
		want := classify.ClassAgree
		switch id {
		case refused:
			want = classify.ClassNotSent
		case missing:
			want = classify.ClassHarness
		}
		if classes[id] != want {
			t.Errorf("%s: class %s, want %s", id, classes[id], want)
		}
	}
	var v classify.Finding
	b, err := os.ReadFile(filepath.Join(out, "finds", classify.ClassNotSent, classify.Sanitise(refused), "verdict.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v.Engine != "fakefol" || v.Msg != "fakefol_lacks_card" {
		t.Errorf("not_sent verdict: %+v", v)
	}
	md, _ := os.ReadFile(filepath.Join(out, "finds", classify.ClassNotSent, classify.Sanitise(refused), "REPRO.md"))
	if !strings.Contains(string(md), "./cmd/oraclediff follower-export -engine fakefol") {
		t.Errorf("REPRO.md does not re-export through follower-export:\n%s", md)
	}

	// Forge: the same rows, the same prep file, but nothing is not_sent.
	forgeRows := filepath.Join(tmp, "forge-rows.jsonl")
	var stamped []byte
	if err := readLines(rows, func(b []byte) error {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		m["engine"] = json.RawMessage(`"forge"`)
		nb, _ := json.Marshal(m)
		stamped = append(append(stamped, nb...), '\n')
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(forgeRows, stamped, 0o644)
	fout := filepath.Join(tmp, "forge")
	if err := diff(reg, play, "forge", forgeRows, fout, ""); err != nil {
		t.Fatal(err)
	}
	if c := verdictClasses(t, filepath.Join(fout, "verdicts.jsonl"))[refused]; c != classify.ClassHarness {
		t.Errorf("forge read forge-prep.jsonl: %s is %s, want harness", refused, c)
	}
}
