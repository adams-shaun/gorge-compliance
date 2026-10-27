package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const manifestDir = "../../oracle/compliance/manifests"

// playTwo plays two FDN games (the smoke's first two) into a fresh temp dir.
func playTwo(t *testing.T) (*cards.Registry, string) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cands, _, err := decks.Candidates(reg, manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := play(reg, cands, cov.New(), "FDN", 0, 2, 1, dir, false); err != nil {
		t.Fatal(err)
	}
	return reg, dir
}

// goldenRows writes a follower row per item whose snapshots are gorge's own
// (so they agree), after edit has had its way with the row. It returns the
// item ids in order.
func goldenRows(t *testing.T, playDir, path string, edit func(i int, x *oraclediff.XResult)) []string {
	t.Helper()
	var ids []string
	var out bytes.Buffer
	if err := readLines(filepath.Join(playDir, "gorge.jsonl"), func(b []byte) error {
		var r gorgeRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		x := oraclediff.XResult{ID: r.ID, Name: r.ID, Snapshots: r.Result.Snapshots}
		// A deep copy, so an edit never reaches gorge's stored result.
		raw, _ := json.Marshal(x)
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		if edit != nil {
			edit(len(ids), &x)
		}
		line, _ := jsonl(x)
		out.Write(line)
		ids = append(ids, r.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Fatal("play kept no lower_ok item")
	}
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return ids
}

func verdictClasses(t *testing.T, path string) map[string]string {
	t.Helper()
	got := map[string]string{}
	if err := readLines(path, func(b []byte) error {
		var f classify.Finding
		if err := json.Unmarshal(b, &f); err != nil {
			return err
		}
		got[f.ID] = f.Class
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestXenginePlayDiffRoundTrip(t *testing.T) {
	reg, play := playTwo(t)
	tmp := t.TempDir()
	rows := filepath.Join(tmp, "rows.jsonl")
	ids := goldenRows(t, play, rows, nil)

	// Rows that are gorge's own snapshots: everything agrees, nothing is found.
	out := filepath.Join(tmp, "agree")
	if err := diff(reg, play, "xmage", rows, out, ""); err != nil {
		t.Fatal(err)
	}
	for id, c := range verdictClasses(t, filepath.Join(out, "verdicts.jsonl")) {
		if c != classify.ClassAgree {
			t.Errorf("%s: class %s, want agree", id, c)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "finds")); err == nil {
		t.Error("an all-agree diff wrote finds")
	}

	// One life total changed in the first item's last checkpoint: exactly one
	// diverge finding with a complete repro directory.
	rows2 := filepath.Join(tmp, "rows2.jsonl")
	goldenRows(t, play, rows2, func(i int, x *oraclediff.XResult) {
		if i == 0 {
			last := len(x.Snapshots) - 1
			x.Snapshots[last].Players[0].Life--
		}
	})
	out2 := filepath.Join(tmp, "diverge")
	if err := diff(reg, play, "xmage", rows2, out2, ""); err != nil {
		t.Fatal(err)
	}
	classes := verdictClasses(t, filepath.Join(out2, "verdicts.jsonl"))
	nonAgree := 0
	for id, c := range classes {
		if c != classify.ClassAgree {
			nonAgree++
			if id != ids[0] || c != classify.ClassDiverge {
				t.Errorf("%s: class %s, want only %s diverge", id, c, ids[0])
			}
		}
	}
	if nonAgree != 1 || len(classes) != len(ids) {
		t.Fatalf("%d non-agree of %d items (%v), want 1 of %d", nonAgree, len(classes), classes, len(ids))
	}
	repro := filepath.Join(out2, "finds", classify.ClassDiverge, classify.Sanitise(ids[0]))
	for _, n := range []string{"scenario.json", "gorge.json", "xmage.json", "verdict.json", "REPRO.md", "input.jsonl"} {
		if _, err := os.Stat(filepath.Join(repro, n)); err != nil {
			t.Errorf("repro lacks %s", n)
		}
	}
	var v classify.Finding
	b, _ := os.ReadFile(filepath.Join(repro, "verdict.json"))
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v.Class != classify.ClassDiverge || v.Engine != "xmage" || !strings.HasSuffix(v.Field, ".life") || v.Gorge == v.Other {
		t.Errorf("verdict.json: %+v", v)
	}
	md, _ := os.ReadFile(filepath.Join(repro, "REPRO.md"))
	for _, want := range []string{"./cmd/xengine diff -play " + play, "scripts/xmage-oracle-run.sh", "-only " + ids[0]} {
		if !strings.Contains(string(md), want) {
			t.Errorf("REPRO.md lacks %q:\n%s", want, md)
		}
	}

	// The repro's command: a rerun of the cut scenario (rows with the cut's
	// checkpoints) is classified against gorge's stored result, not replayed.
	cut := filepath.Join(tmp, "cut.jsonl")
	var x oraclediff.XResult
	b, _ = os.ReadFile(filepath.Join(repro, "xmage.json"))
	if err := json.Unmarshal(b, &x); err != nil {
		t.Fatal(err)
	}
	line, _ := jsonl(x)
	os.WriteFile(cut, line, 0o644)
	out3 := filepath.Join(tmp, "only")
	if err := diff(reg, play, "xmage", cut, out3, ids[0]); err != nil {
		t.Fatal(err)
	}
	if got := verdictClasses(t, filepath.Join(out3, "verdicts.jsonl")); got[ids[0]] != classify.ClassDiverge {
		t.Errorf("-only rerun: %v", got)
	}

	// Follower output never lands in the repository.
	if err := runDiff([]string{"-play", play, "-engine", "xmage", "-rows", rows, "-out", "finds-inside"}); err == nil {
		os.RemoveAll("finds-inside")
		t.Error("diff wrote inside the repository")
	}
}

func TestXengineCompareCredit(t *testing.T) {
	reg, play := playTwo(t)
	tmp := t.TempDir()
	rows := filepath.Join(tmp, "rows.jsonl")
	ids := goldenRows(t, play, rows, func(i int, x *oraclediff.XResult) {
		if i == 1 {
			x.Harness = "the driver cannot express this step"
		}
	})
	if len(ids) < 2 {
		t.Skip("play kept fewer than two lower_ok items")
	}
	out := filepath.Join(tmp, "out")
	if err := diff(reg, play, "xmage", rows, out, ""); err != nil {
		t.Fatal(err)
	}
	if got := verdictClasses(t, filepath.Join(out, "verdicts.jsonl")); got[ids[0]] != classify.ClassAgree || got[ids[1]] != classify.ClassHarness {
		t.Fatalf("classes %v", got)
	}
	f, err := os.Open(filepath.Join(out, "compared.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	led, err := cov.ReadTSV(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	usesOf := func(id string) map[classify.Use]int {
		var tp *game.Tape
		b, err := os.ReadFile(filepath.Join(play, "tapes", classify.Sanitise(id)+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if tp, err = game.ReadTape(bytes.NewReader(b)); err != nil {
			t.Fatal(err)
		}
		return classify.Uses(reg, tp, 0, len(tp.Entries)-1, true)
	}
	agreed := usesOf(ids[0])
	if len(agreed) == 0 {
		t.Fatal("the agreeing game exercised no key")
	}
	// Every key of the agreeing item is compared (a lowering truncation can
	// only shrink the range, so allow a key beyond it to be missing but never
	// a key the harness item alone exercised).
	credited := 0
	for _, u := range classify.Sorted(usesOf(ids[0])) {
		if led.Get(u.Card, u.Key).Compared > 0 {
			credited++
		}
	}
	if credited == 0 {
		t.Error("the agreeing item credited no key")
	}
	for u := range usesOf(ids[1]) {
		if _, shared := agreed[u]; shared {
			continue
		}
		if c := led.Get(u.Card, u.Key).Compared; c != 0 {
			t.Errorf("%s %s: compared %d, but only the harness item exercised it", u.Card, u.Key, c)
		}
	}
}

// mkChunk makes root/set/chunk, with a .done marker when done.
func mkChunk(t *testing.T, root, set string, chunk int, done bool) string {
	t.Helper()
	dir := filepath.Join(root, set, fmt.Sprint(chunk))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if done {
		if err := os.WriteFile(filepath.Join(dir, doneFile), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(filepath.Join(dir, "items.jsonl"), []byte("{half"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSweepPlanResume(t *testing.T) {
	root := t.TempDir()
	mkChunk(t, root, "FDN", 0, true)
	half := mkChunk(t, root, "FDN", 1, false)
	mkChunk(t, root, "FDN", 2, true)
	want := "FDN/1 FDN/3"
	plan, err := sweepPlan(root, []string{"FDN"}, 4, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(plan, " "); got != want {
		t.Fatalf("plan %q, want %q", got, want)
	}
	if _, err := os.Stat(half); err != nil {
		t.Fatalf("a plan without -stale must leave %s: %v", half, err)
	}
	// A stale directory from an earlier attempt takes n=1; the next is n=2.
	if err := os.MkdirAll(half+".stale.1", 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err = sweepPlan(root, []string{"FDN"}, 4, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(plan, " "); got != want {
		t.Fatalf("-stale plan %q, want %q", got, want)
	}
	if _, err := os.Stat(half); !os.IsNotExist(err) {
		t.Fatalf("%s should have moved aside: %v", half, err)
	}
	if b, err := os.ReadFile(filepath.Join(half+".stale.2", "items.jsonl")); err != nil || string(b) != "{half" {
		t.Fatalf("stale.2 does not hold the half-written chunk: %q %v", b, err)
	}
	// Done chunks stay in place and a forced plan redoes them all.
	if _, err := os.Stat(filepath.Join(root, "FDN", "0", doneFile)); err != nil {
		t.Fatal(err)
	}
	plan, err = sweepPlan(root, []string{"FDN"}, 4, false, true)
	if err != nil || strings.Join(plan, " ") != "FDN/0 FDN/1 FDN/2 FDN/3" {
		t.Fatalf("forced plan %v %v", plan, err)
	}
}

func TestSweepPlanOrder(t *testing.T) {
	root := t.TempDir()
	mkChunk(t, root, "BLB", 0, true)
	var first []string
	for i := 0; i < 3; i++ {
		plan, err := sweepPlan(root, []string{"FDN", "BLB"}, 2, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = plan
			continue
		}
		if strings.Join(plan, "\n") != strings.Join(first, "\n") {
			t.Fatalf("run %d: %v differs from %v", i, plan, first)
		}
	}
	if got := strings.Join(first, " "); got != "FDN/0 FDN/1 BLB/1" {
		t.Fatalf("plan %q: sets must keep the given order and chunks ascend", got)
	}
}

func TestSweepPlanRefusesRepoRoot(t *testing.T) {
	root := filepath.Join("..", "..", ".sweep-plan-test-root")
	defer os.RemoveAll(root)
	err := runSweepPlan([]string{"-root", root, "-sets", "FDN", "-chunks", "1"})
	if err == nil || !strings.Contains(err.Error(), "inside the repository") {
		t.Fatalf("a root inside the module must be refused, got %v", err)
	}
	if _, serr := os.Stat(root); serr == nil {
		t.Fatal("the refused root was created")
	}
}

// TestXprepPlay: xprep splits a play dir's items into the served ones (xitems)
// and the refused ones (xprep.jsonl), loses none, and writes the same bytes
// twice.
func TestXprepPlay(t *testing.T) {
	reg, dir := playTwo(t)
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	run := func() (served, refused []byte) {
		out := filepath.Join(t.TempDir(), "xitems.jsonl")
		if err := xprepPlay(reg, sp, dir, out); err != nil {
			t.Fatal(err)
		}
		served, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		refused, err = os.ReadFile(filepath.Join(filepath.Dir(out), xprepRefusalsFile))
		if err != nil {
			t.Fatal(err)
		}
		return served, refused
	}
	served, refused := run()
	served2, refused2 := run()
	if !bytes.Equal(served, served2) || !bytes.Equal(refused, refused2) {
		t.Error("xprep is not deterministic")
	}
	in := map[string]bool{}
	if err := readLines(filepath.Join(dir, "items.jsonl"), func(b []byte) error {
		var it struct{ ID string }
		json.Unmarshal(b, &it)
		in[it.ID] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(in) == 0 {
		t.Fatal("play wrote no lower_ok item")
	}
	seen := map[string]bool{}
	for _, part := range [][]byte{served, refused} {
		for _, line := range bytes.Split(bytes.TrimSpace(part), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var it struct {
				ID      string
				Reason  string
				Answers [][]any `json:"xmage_answers"`
			}
			if err := json.Unmarshal(line, &it); err != nil {
				t.Fatal(err)
			}
			if seen[it.ID] || !in[it.ID] {
				t.Errorf("item %s is duplicated or not in items.jsonl", it.ID)
			}
			seen[it.ID] = true
		}
	}
	if len(seen) != len(in) {
		t.Errorf("%d of %d items accounted for", len(seen), len(in))
	}
}

// TestCalibReport: canned rows (two windows agree, one harness, a lower_gap
// window; tape A followed to turn 2 and harness at turn 4, tape B harness from
// turn 2) give the class counts, the harness rate, the prefix curve grouped by
// turns kept and the gate.
func TestCalibReport(t *testing.T) {
	play, diff := t.TempDir(), t.TempDir()
	type row = map[string]any
	var lower, verdicts bytes.Buffer
	add := func(id, status, class string, step int, msg string) {
		b, _ := jsonl(row{"id": id, "status": status})
		lower.Write(b)
		b, _ = jsonl(row{"id": id, "class": class, "step": step, "msg": msg})
		verdicts.Write(b)
	}
	add("xe:FDN:0:0:w1", "lower_ok", "agree", -1, "")
	add("xe:FDN:0:0:w2", "lower_ok", "agree", -1, "")
	add("xe:FDN:0:0:w3", "lower_ok", "harness", 4, "no such card: Shock #12 at step 7")
	add("xe:FDN:0:0:w4", "lower_gap", "lower_gap", 2, "")
	for _, p := range []struct {
		id, class string
		step      int
	}{
		{"xe:FDN:0:0:tape:t1", "agree", -1}, {"xe:FDN:0:0:tape:t2", "agree", -1}, {"xe:FDN:0:0:tape:t4", "harness", 9},
		{"xe:FDN:0:1:tape:t1", "agree", -1}, {"xe:FDN:0:1:tape:t2", "harness", 3}, {"xe:FDN:0:1:tape:t4", "harness", 3},
	} {
		add(p.id, "lower_ok", p.class, p.step, "x")
	}
	files := map[string][]byte{
		filepath.Join(play, "lower.jsonl"):    lower.Bytes(),
		filepath.Join(diff, "verdicts.jsonl"): verdicts.Bytes(),
		filepath.Join(play, "exercised.tsv"):  []byte("card\tkey\texercised\tcompared\toffered\nA\tk\t1\t0\t0\nB\tk\t2\t0\t0\nC\tk\t0\t0\t0\n"),
		filepath.Join(diff, "compared.tsv"):   []byte("card\tkey\texercised\tcompared\toffered\nA\tk\t0\t1\t0\n"),
	}
	for p, b := range files {
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := calibrate(play, diff, "xmage")
	if err != nil {
		t.Fatal(err)
	}
	w := c.Windows
	if w.Total != 4 || w.LowerOK != 3 || w.ByClass["agree"] != 2 || w.ByClass["harness"] != 1 || w.ByClass["lower_gap"] != 1 {
		t.Errorf("window counts: %+v", w)
	}
	if w.Harness != 1 || w.NotFollowed != 1 || w.HarnessRate < 0.333 || w.HarnessRate > 0.334 {
		t.Errorf("window rates: %+v", w)
	}
	if len(w.TopHarness) != 1 || w.TopHarness[0].Msg != "no such card: Shock N at step N" || w.TopHarness[0].Count != 1 {
		t.Errorf("top harness: %+v", w.TopHarness)
	}
	curve := map[string][2]int{}
	for k, p := range c.Tapes.Curve {
		curve[k] = [2]int{p.Followed, p.Items}
	}
	want := map[string][2]int{"1": {2, 2}, "2": {1, 2}, "4": {0, 2}}
	if len(curve) != len(want) || curve["1"] != want["1"] || curve["2"] != want["2"] || curve["4"] != want["4"] {
		t.Errorf("prefix curve %v, want %v", curve, want)
	}
	// Tape A's earliest harness row is step 9, tape B's step 3.
	if c.Tapes.MedianFirstHarness != 6 {
		t.Errorf("median first harness step %v, want 6", c.Tapes.MedianFirstHarness)
	}
	if c.Gate.Windows || c.Gate.Tape2 == nil || !*c.Gate.Tape2 {
		t.Errorf("gate %+v: 2 of 3 windows followed fails the 80%% gate, 1 of 2 tape prefixes passes the 50%% one", c.Gate)
	}
	if c.Cover.Exercised != 2 || c.Cover.Compared != 1 {
		t.Errorf("coverage %+v", c.Cover)
	}
	b1, _ := json.Marshal(c)
	c2, _ := calibrate(play, diff, "xmage")
	if b2, _ := json.Marshal(c2); !bytes.Equal(b1, b2) {
		t.Error("calibrate is not deterministic")
	}
}
