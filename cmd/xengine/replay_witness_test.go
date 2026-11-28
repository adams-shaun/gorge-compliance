package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
)

// witnessFile writes play's items as xproof-witness-1 lines (keys from each
// item's tape) and returns the path, the ids and each witness's keys.
func witnessFile(t *testing.T, reg *cards.Registry, play string, corrupt int) (string, []string, [][]kpUse) {
	t.Helper()
	items := map[string]oraclegen.Item{}
	if err := readLines(filepath.Join(play, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		items[it.ID] = it
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	var ids []string
	var keys [][]kpUse
	if err := readLines(filepath.Join(play, "gorge.jsonl"), func(b []byte) error {
		var r gorgeRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		tb, err := os.ReadFile(filepath.Join(play, "tapes", classify.Sanitise(r.ID)+".json"))
		if err != nil {
			return err
		}
		tp, err := game.ReadTape(bytes.NewReader(tb))
		if err != nil {
			return err
		}
		w := kpWitness{Format: "xproof-witness-1", ID: r.ID, Refs: "FX", GorgeSHA: "oldsha", Item: items[r.ID], Gorge: r.Result, Keys: []kpUse{}}
		for _, u := range classify.Sorted(classify.Uses(reg, tp, 0, len(tp.Entries)-1, true)) {
			w.Keys = append(w.Keys, kpUse{u.Card, u.Key})
		}
		if len(ids) == corrupt {
			// a deep copy, then one snapshot's p0 life + 1
			raw, _ := json.Marshal(w.Gorge)
			json.Unmarshal(raw, &w.Gorge)
			w.Gorge.Snapshots[len(w.Gorge.Snapshots)-1].Players[0].Life++
		}
		line, err := jsonl(w)
		if err != nil {
			return err
		}
		out.Write(line)
		ids = append(ids, r.ID)
		keys = append(keys, w.Keys)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(ids) < 2 {
		t.Fatalf("play kept %d items, want 2", len(ids))
	}
	path := filepath.Join(t.TempDir(), "witness.jsonl")
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, ids, keys
}

func tsvRows(t *testing.T, path string) [][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	for _, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		rows = append(rows, strings.Split(l, "\t"))
	}
	return rows
}

func TestReplayWitness(t *testing.T) {
	reg, play := playTwo(t)
	path, ids, keys := witnessFile(t, reg, play, 1)
	out := t.TempDir()
	if err := replayWitness(reg, []string{path}, out, "2026-11-24", "newsha123456"); err != nil {
		t.Fatal(err)
	}
	hdr := tsvRows(t, filepath.Join(out, "keyproof.tsv"))
	if strings.Join(hdr[0], "\t") != keyproofHeader {
		t.Fatalf("header %q", hdr[0])
	}
	state := map[kpUse]string{}
	for _, r := range hdr[1:] {
		if r[14] != "newsha123456" || r[19] != "2026-11-24" || r[20] != "xproof" {
			t.Errorf("row %v: stamps", r)
		}
		u := kpUse{r[0], r[2]}
		state[u] = r[4]
		if r[4] == "diverged" && r[5] != "witness-changed" {
			t.Errorf("row %v: reason", r)
		}
	}
	second := map[kpUse]bool{}
	for _, u := range keys[1] {
		second[u] = true
		if state[u] != "diverged" {
			t.Errorf("second witness key %v: %q, want diverged", u, state[u])
		}
	}
	nFirstOnly := 0
	for _, u := range keys[0] {
		if second[u] {
			continue
		}
		nFirstOnly++
		if state[u] != "agreed" {
			t.Errorf("first witness key %v: %q, want agreed", u, state[u])
		}
	}
	t.Logf("%s: %d keys; first-only %d, second %d", ids, len(state), nFirstOnly, len(second))
	reg2 := tsvRows(t, filepath.Join(out, "regressions.tsv"))
	if strings.Join(reg2[0], "\t") != "card\tkey\twitness\tcheckpoint\tfield\tprio" {
		t.Fatalf("regressions header %q", reg2[0])
	}
	if len(reg2)-1 != len(second) {
		t.Errorf("%d regression rows, want %d", len(reg2)-1, len(second))
	}
	for _, r := range reg2[1:] {
		if r[5] != "P1" || r[2] != ids[1] {
			t.Errorf("regression row %v", r)
		}
	}
}

func TestReplayWitnessResults(t *testing.T) {
	reg, play := playTwo(t)
	path, ids, keys := witnessFile(t, reg, play, 1)
	out := t.TempDir()
	if err := replayWitness(reg, []string{path}, out, "2026-11-24", "newsha123456"); err != nil {
		t.Fatal(err)
	}
	rows := tsvRows(t, filepath.Join(out, "replay.tsv"))
	if strings.Join(rows[0], "\t") != replayHeader {
		t.Fatalf("replay header %q", rows[0])
	}
	got := map[string]map[kpUse]replayResult{}
	for _, r := range rows[1:] {
		if len(r) != 6 {
			t.Fatalf("replay row %v: %d columns, want 6", r, len(r))
		}
		u := kpUse{r[0], r[1]}
		m := got[r[2]]
		if m == nil {
			m = map[kpUse]replayResult{}
			got[r[2]] = m
		}
		if _, dup := m[u]; dup {
			t.Errorf("replay row %v: duplicate (witness, key)", r)
		}
		m[u] = replayResult{r[0], r[1], r[2], r[3], r[4], r[5]}
	}
	if want := len(keys[0]) + len(keys[1]); len(rows)-1 != want {
		t.Errorf("%d replay rows, want %d (one per witness, key)", len(rows)-1, want)
	}
	for _, u := range keys[0] {
		r, ok := got[ids[0]][u]
		if !ok {
			t.Errorf("first witness key %v: no replay row", u)
			continue
		}
		if r.result != "held" || r.checkpoint != "-" || r.field != "-" {
			t.Errorf("first witness key %v: %+v, want held with no checkpoint or field", u, r)
		}
	}
	for _, u := range keys[1] {
		r, ok := got[ids[1]][u]
		if !ok {
			t.Errorf("second witness key %v: no replay row", u)
			continue
		}
		if r.result != "changed" || r.checkpoint == "-" || r.field == "-" {
			t.Errorf("second witness key %v: %+v, want changed with its checkpoint and field", u, r)
		}
	}
}

func TestReplayWitnessDir(t *testing.T) {
	reg, play := playTwo(t)
	path, _, _ := witnessFile(t, reg, play, 1)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("%s has %d lines, want 2", path, len(lines))
	}
	dir := t.TempDir()
	f1 := filepath.Join(dir, "01-first.jsonl")
	f2 := filepath.Join(dir, "02-second.jsonl")
	if err := os.WriteFile(f1, []byte(lines[0]+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte(lines[1]+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Not a witness: the directory expansion reads *.jsonl only.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(out string, args ...string) {
		t.Helper()
		argv := []string{"-cards", "../../.cards", "-measured", "2026-11-24", "-gorge-sha", "newsha123456", "-out", out}
		if err := runReplayWitness(append(argv, args...)); err != nil {
			t.Fatal(err)
		}
	}
	outDir, outFlags := t.TempDir(), t.TempDir()
	run(outDir, "-witness-dir", dir)
	run(outFlags, "-witness", f1, "-witness", f2)
	for _, name := range []string{"keyproof.tsv", "regressions.tsv", "replay.tsv"} {
		fromDir, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatal(err)
		}
		fromFlags, err := os.ReadFile(filepath.Join(outFlags, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fromDir, fromFlags) {
			t.Errorf("%s differs between -witness-dir and -witness runs", name)
		}
	}
	err = runReplayWitness([]string{"-cards", "../../.cards", "-measured", "2026-11-24", "-gorge-sha", "newsha123456", "-witness-dir", t.TempDir(), "-out", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "-witness or -witness-dir") {
		t.Fatalf("empty dir err %v, want one naming both flags", err)
	}
}

func TestReplayWitnessBadFormat(t *testing.T) {
	reg, play := playTwo(t)
	path, _, _ := witnessFile(t, reg, play, -1)
	b, _ := os.ReadFile(path)
	bad := strings.Replace(string(b), `"format":"xproof-witness-1"`, `"format":"x"`, 1)
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	err := replayWitness(reg, []string{path}, t.TempDir(), "2026-11-24", "newsha123456")
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("err %v, want one naming line 1", err)
	}
}

func TestReplayWitnessDeterministic(t *testing.T) {
	reg, play := playTwo(t)
	path, _, _ := witnessFile(t, reg, play, 1)
	var got [2]map[string]string
	for i := range got {
		out := t.TempDir()
		if err := replayWitness(reg, []string{path}, out, "2026-11-24", "newsha123456"); err != nil {
			t.Fatal(err)
		}
		got[i] = map[string]string{}
		for _, n := range []string{"keyproof.tsv", "regressions.tsv"} {
			b, err := os.ReadFile(filepath.Join(out, n))
			if err != nil {
				t.Fatal(err)
			}
			got[i][n] = string(b)
		}
	}
	for n := range got[0] {
		if got[0][n] != got[1][n] {
			t.Errorf("%s differs between runs", n)
		}
	}
}
