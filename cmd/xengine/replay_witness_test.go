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
