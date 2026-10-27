package main

import (
	"bytes"
	"encoding/json"
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
	if err := play(reg, cands, cov.New(), "FDN", 0, 2, 1, dir); err != nil {
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
