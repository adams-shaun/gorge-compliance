package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
)

// TestTapeTurnsPrefixes: -tape-turns 1,2 writes, per tape that outlasts the
// turn, one extra item per N that is a step-prefix of the full tape item, ends
// at the boundary of turn N (the tape file's last entry is the first of turn
// N+1, everything before it turn <= N) and passes its self-check.
func TestTapeTurnsPrefixes(t *testing.T) {
	dir := t.TempDir()
	args := []string{"-set", "FDN", "-chunk", "0", "-games", "2", "-seed", "1", "-cards", "../../.cards",
		"-manifests", manifestDir, "-tape-turns", "2,1,2", "-out", dir}
	if err := runPlay(args); err != nil {
		t.Fatal(err)
	}
	items := map[string]oraclegen.Item{}
	if err := readLines(filepath.Join(dir, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		items[it.ID] = it
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rows := map[string]lowerRow{}
	if err := readLines(filepath.Join(dir, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		rows[r.ID] = r
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	prefixes := 0
	for id, full := range items {
		if full.Template != lower.Template {
			continue
		}
		var prev int
		for _, n := range []int{1, 2} {
			pid := id + ":t" + strconv.Itoa(n)
			p, have := items[pid]
			r, row := rows[pid]
			if !have {
				if row && r.Status == lower.StatusOK {
					t.Errorf("%s: lower_ok row without an item", pid)
				}
				continue
			}
			prefixes++
			if !row || r.Status != lower.StatusOK || r.Truncated != reasonTurnPrefix {
				t.Errorf("%s: row %+v, want lower_ok and truncated %q", pid, r, reasonTurnPrefix)
			}
			if len(p.Steps) == 0 || len(p.Steps) >= len(full.Steps) || len(p.Steps) <= prev {
				t.Errorf("%s: %d steps of %d (previous prefix %d)", pid, len(p.Steps), len(full.Steps), prev)
				continue
			}
			prev = len(p.Steps)
			for i, st := range p.Steps {
				a, _ := json.Marshal(st)
				b, _ := json.Marshal(full.Steps[i])
				if string(a) != string(b) {
					t.Errorf("%s: step %d is not the full item's", pid, i)
					break
				}
			}
			f, err := os.Open(filepath.Join(dir, r.Tape))
			if err != nil {
				t.Fatal(err)
			}
			tp, err := game.ReadTape(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			last := len(tp.Entries) - 1
			if int(tp.Entries[last].Turn) <= n || int(tp.Entries[last-1].Turn) > n || r.TruncatedAt != last {
				t.Errorf("%s: tape cut at entry %d (turns %d, %d), want the first entry past turn %d", pid, last, tp.Entries[last-1].Turn, tp.Entries[last].Turn, n)
			}
			if e := r.StepEntry[len(p.Steps)-1]; e != last-1 {
				t.Errorf("%s: last step carries entry %d, want %d (the end of turn %d)", pid, e, last-1, n)
			}
		}
	}
	if prefixes == 0 {
		t.Fatal("no tape prefix written: the test exercises nothing")
	}
	if _, err := parseTapeTurns("0"); err == nil {
		t.Error("-tape-turns 0 accepted")
	}
}
