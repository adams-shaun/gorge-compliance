package adjudicate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge/engine/rules"
)

// result is a two-checkpoint result in which seat 0 has `life` at the second.
func result(life int32) rules.OracleResult {
	snap := func(name string, l int32) rules.OracleSnapshot {
		pl := func(seat int, life int32) rules.OracleSnapPlayer {
			return rules.OracleSnapPlayer{Seat: seat, Life: life, LibraryCount: 30}
		}
		return rules.OracleSnapshot{Checkpoint: name, Turn: 1, Step: "main1",
			Players: []rules.OracleSnapPlayer{pl(0, l), pl(1, 20)}}
	}
	return rules.OracleResult{Snapshots: []rules.OracleSnapshot{snap("setup", 20), snap("step 0 (cast)", life)}}
}

func row(id string, life int32, harness string) oraclediff.XResult {
	return oraclediff.XResult{ID: id, Harness: harness, Snapshots: result(life).Snapshots}
}

func item(id, card string) oraclegen.Item {
	return oraclegen.Item{ID: id, Card: card, Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{{Op: "cast", Card: card}}}}
}

func TestAdjudicateLabels(t *testing.T) {
	it := item("xe:T:0:0:w0", "Shock")
	g := result(18)
	for _, c := range []struct {
		name    string
		x, f    oraclediff.XResult
		pattern oraclediff.Pattern
		label   string
	}{
		{"FX|G", row("i", 20, ""), row("i", 20, ""), oraclediff.ForgeXMageG, LabelGorgeSuspect},
		{"GF|X", row("i", 20, ""), row("i", 18, ""), oraclediff.GorgeForgeX, LabelXMageSuspect},
		{"GX|F", row("i", 18, ""), row("i", 20, ""), oraclediff.GorgeXMageF, LabelForgeSuspect},
		{"G|F|X", row("i", 20, ""), row("i", 17, ""), oraclediff.AllDiffer, LabelThreeWay},
		{"GF|-", row("i", 18, "no such card"), row("i", 18, ""), oraclediff.GorgeForgeNoX, LabelGorgeForgeOnly},
		{"G|F|-", row("i", 18, "no such card"), row("i", 20, ""), oraclediff.GorgeNoForgeNoX, LabelGorgeForgeOnly},
		{"F harness", row("i", 20, ""), row("i", 18, "driver gap"), oraclediff.ForgeHarness, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, label := Item(it, g, c.x, c.f)
			if p != c.pattern || label != c.label {
				t.Fatalf("got %s/%q, want %s/%q", p, label, c.pattern, c.label)
			}
		})
	}

	// The same table through Run: GF|X is a note, never a ruling, a verdict row
	// or the word xmage_wrong, anywhere in the output.
	in := Input{
		Items: map[string]oraclegen.Item{"a": item("a", "Shock")},
		Gorge: map[string]rules.OracleResult{"a": g},
		XRows: map[string]oraclediff.XResult{"a": row("a", 20, "")},
		FRows: map[string]oraclediff.XResult{"a": row("a", 18, "")},
		XFind: map[string]classify.Finding{"a": {ID: "a", Engine: "xmage", Class: classify.ClassDiverge, Step: 0}},
		FFind: map[string]classify.Finding{"a": {ID: "a", Engine: "forge", Class: classify.ClassAgree, Step: -1}},
	}
	res := Run(in)
	if len(res.Rows) != 1 || res.Rows[0].Label != LabelXMageSuspect || len(res.Notes) != 1 || len(res.Drafts) != 0 {
		t.Fatalf("GF|X run: %+v", res)
	}
	out := filepath.Join(t.TempDir(), "out")
	if err := Write(out, res); err != nil {
		t.Fatal(err)
	}
	err := filepath.Walk(out, func(p string, st os.FileInfo, err error) error {
		if err != nil || st.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), "xmage_wrong") {
			t.Errorf("%s mentions xmage_wrong", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAdjudicateSkipsNonDiverge(t *testing.T) {
	for _, c := range []struct {
		x, f string
		want bool
	}{
		{classify.ClassRNG, classify.ClassAgree, false},
		{classify.ClassAgree, classify.ClassAgree, false},
		{classify.ClassDiverge, classify.ClassAgree, true},
		{classify.ClassAgree, classify.ClassDiverge, true},
		{classify.ClassDiverge, classify.ClassDiverge, true},
		{classify.ClassHarness, classify.ClassDiverge, true},
		{classify.ClassXMageLacks, classify.ClassDiverge, true},
		{classify.ClassDiverge, classify.ClassHarness, true},
		{classify.ClassRNG, classify.ClassDiverge, false},
		{classify.ClassDiverge, classify.ClassProjectionGap, false},
		{classify.ClassDecisionShape, classify.ClassDiverge, false},
		{classify.ClassDiverge, classify.ClassLowerGap, false},
	} {
		if got := Eligible(c.x, c.f); got != c.want {
			t.Errorf("Eligible(%s, %s) = %v, want %v", c.x, c.f, got, c.want)
		}
	}
	// Through Run: rng for XMage, agree for Forge, even with rows that would
	// compare as FX|G if looked at, is not adjudicated.
	in := Input{
		Items: map[string]oraclegen.Item{"a": item("a", "Shock")},
		Gorge: map[string]rules.OracleResult{"a": result(18)},
		XRows: map[string]oraclediff.XResult{"a": row("a", 20, "")},
		FRows: map[string]oraclediff.XResult{"a": row("a", 20, "")},
		XFind: map[string]classify.Finding{"a": {ID: "a", Class: classify.ClassRNG}},
		FFind: map[string]classify.Finding{"a": {ID: "a", Class: classify.ClassAgree}},
	}
	if res := Run(in); len(res.Rows) != 0 || len(res.Findings) != 0 {
		t.Fatalf("adjudicated a non-diverge item: %+v", res.Rows)
	}
}

// clusterInput is n gorge_suspect items on Shock, the i-th with n-1-i extra
// steps before the diverging one: repro length runs against id order, so the
// shortest repro is the last id and cannot be found by taking the first.
func clusterInput(t *testing.T, diffs string, n int) Input {
	in := Input{
		Items: map[string]oraclegen.Item{}, Gorge: map[string]rules.OracleResult{},
		XRows: map[string]oraclediff.XResult{}, FRows: map[string]oraclediff.XResult{},
		XFind: map[string]classify.Finding{}, FFind: map[string]classify.Finding{},
		XDiff: filepath.Join(diffs, "x"), FDiff: filepath.Join(diffs, "f"),
	}
	for i := range n {
		id := fmt.Sprintf("xe:T:0:%d:w0", i)
		it := item(id, "Shock")
		it.Steps = nil
		extra := n - 1 - i
		for range extra {
			it.Steps = append(it.Steps, oraclegen.Step{Op: "pass"})
		}
		it.Steps = append(it.Steps, oraclegen.Step{Op: "cast", Card: "Shock"})
		in.Items[id] = it
		in.Gorge[id] = result(18)
		in.XRows[id] = row(id, 20, "")
		in.FRows[id] = row(id, 20, "")
		xf := classify.Finding{ID: id, Engine: "xmage", Class: classify.ClassDiverge, Checkpoint: "step 0 (cast)", Field: fmt.Sprintf("p%d.life", i), Gorge: "18", Other: "20", Step: extra}
		ff := xf
		ff.Engine = "forge"
		in.XFind[id], in.FFind[id] = xf, ff
		for _, f := range []classify.Finding{xf, ff} {
			dir := classify.Dir(in.XDiff, f)
			if f.Engine == "forge" {
				dir = classify.Dir(in.FDiff, f)
			}
			os.MkdirAll(dir, 0o755)
			os.WriteFile(filepath.Join(dir, "REPRO.md"), []byte("# repro\n\n```\nrun-"+f.Engine+" "+id+"\n```\n"), 0o644)
		}
	}
	return in
}

func TestClusterDraft(t *testing.T) {
	diffs := t.TempDir()
	in := clusterInput(t, diffs, 3)
	// Every item diverges at p<i>.life: the indices strip to one field class.
	res := Run(in)
	if len(res.Rows) != 3 || len(res.Clusters) != 1 || len(res.Drafts) != 1 {
		t.Fatalf("rows %d clusters %d drafts %d", len(res.Rows), len(res.Clusters), len(res.Drafts))
	}
	c := res.Drafts[0]
	if c.Card != "Shock" || c.FieldClass != "p.life" || len(c.Findings) != 3 || c.Smallest.ID != "xe:T:0:2:w0" {
		t.Fatalf("cluster %+v", c)
	}
	out := filepath.Join(t.TempDir(), "out")
	if err := Write(out, res); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "tickets", c.SHA12+".md"))
	if err != nil {
		t.Fatal(err)
	}
	md := string(b)
	for _, want := range []string{
		"# gorge: Shock", "findings: 3", "xe:T:0:0:w0", "xe:T:0:1:w0",
		"## Smallest repro\n\n`xe:T:0:2:w0`, 1 step(s)", "run-xmage xe:T:0:2:w0", "run-forge xe:T:0:2:w0",
		"## Oracle text + CR",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("draft lacks %q:\n%s", want, md)
		}
	}
	for _, longer := range []string{"xe:T:0:0:w0", "xe:T:0:1:w0"} {
		if strings.Contains(md, "run-xmage "+longer) || strings.Contains(md, "run-forge "+longer) {
			t.Errorf("draft quotes the commands of the longer repro %s", longer)
		}
	}

	// Equal-length repros tie-break to the lowest id.
	tie := clusterInput(t, t.TempDir(), 2)
	a, b2 := "xe:T:0:0:w0", "xe:T:0:1:w0"
	tie.Items[b2] = tie.Items[a]
	for _, m := range []map[string]classify.Finding{tie.XFind, tie.FFind} {
		f := m[b2]
		f.Step = m[a].Step
		m[b2] = f
	}
	if r := Run(tie); len(r.Clusters) != 1 || r.Clusters[0].Smallest.ID != a {
		t.Errorf("tie did not pick the lowest id: %+v", r.Clusters)
	}

	// One finding on a card with a committed agree row is no draft; without
	// one it is.
	one := clusterInput(t, t.TempDir(), 1)
	one.HasAgree = func(card string) bool { return card == "Shock" }
	if r := Run(one); len(r.Clusters) != 1 || len(r.Drafts) != 0 {
		t.Errorf("single finding on an agreeing card: %d clusters, %d drafts", len(r.Clusters), len(r.Drafts))
	}
	one.HasAgree = func(string) bool { return false }
	if r := Run(one); len(r.Drafts) != 1 {
		t.Errorf("single finding on a card with no agree row: %d drafts", len(r.Drafts))
	}
}

func TestLedgerHasNoForgeValues(t *testing.T) {
	diffs := t.TempDir()
	in := clusterInput(t, diffs, 2)
	const secret = "FORGE-SECRET-VALUE"
	for id, f := range in.FFind {
		f.Other, f.Msg = secret, secret
		in.FFind[id] = f
		r := in.FRows[id]
		r.Harness = ""
		r.Snapshots[1].Players[0].Hand = []string{secret}
		in.FRows[id] = r
	}
	in.XRows["xe:T:0:1:w0"] = row("xe:T:0:1:w0", 20, "")
	res := Run(in)
	if len(res.Rows) != 2 {
		t.Fatalf("rows %+v", res.Rows)
	}
	dir := filepath.Join(t.TempDir(), "ledger")
	if err := WriteLedger(dir, res.Rows, "311f123e6abc"); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != "s.jsonl" {
		t.Fatalf("shards %v", ents)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "s.jsonl"))
	got := string(b)
	for _, bad := range []string{secret, "Players", "snapshots", "msg", "harness"} {
		if strings.Contains(got, bad) {
			t.Errorf("ledger contains %q:\n%s", bad, got)
		}
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("ledger:\n%s", got)
	}
	want0 := `{"id":"xe:T:0:0:w0","card":"Shock","forge_ref":"311f123e6abc","pattern":"G|F|X","label":"three_way","gx_field":"p0.life","gf_field":"p0.life","fx_field":"p0.hand"}`
	if lines[0] != want0 {
		t.Errorf("row 0 = %s", lines[0])
	}
	// The ledger is never written inside the repository.
	if err := WriteLedger("ledger-in-repo", res.Rows, ""); err == nil {
		t.Errorf("WriteLedger accepted a path inside the repository")
		os.RemoveAll("ledger-in-repo")
	}
}

// TestLoadRoundTrip reads a play and two diffs from disk the way the CLI does.
func TestLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	put := func(name string, vs ...any) {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		var sb strings.Builder
		for _, v := range vs {
			b, _ := json.Marshal(v)
			sb.Write(b)
			sb.WriteByte('\n')
		}
		if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	id := "xe:T:0:0:w0"
	put("play/items.jsonl", item(id, "Shock"))
	put("play/gorge.jsonl", map[string]any{"id": id, "result": result(18)})
	x, f := row(id, 20, ""), row(id, 20, "")
	put("play/xmage.jsonl", x)
	put("play/forge.jsonl", struct {
		oraclediff.XResult
		Engine string `json:"engine"`
	}{f, "forge"})
	fnd := classify.Finding{ID: id, Class: classify.ClassDiverge, Checkpoint: "step 0 (cast)", Field: "p0.life", Step: 0}
	put("dx/verdicts.jsonl", fnd)
	put("df/verdicts.jsonl", fnd)
	in, err := Load(filepath.Join(dir, "play"), filepath.Join(dir, "dx"), filepath.Join(dir, "df"),
		filepath.Join(dir, "play/xmage.jsonl"), filepath.Join(dir, "play/forge.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	res := Run(in)
	if len(res.Rows) != 1 || res.Rows[0].Pattern != "FX|G" || res.Rows[0].Card != "Shock" || len(res.Drafts) != 1 {
		t.Fatalf("%+v", res)
	}
	// A Forge row handed to the XMage side is refused.
	if _, err := Load(filepath.Join(dir, "play"), filepath.Join(dir, "dx"), filepath.Join(dir, "df"),
		filepath.Join(dir, "play/forge.jsonl"), filepath.Join(dir, "play/forge.jsonl")); err == nil {
		t.Errorf("a forge row was read as an xmage row")
	}
}
