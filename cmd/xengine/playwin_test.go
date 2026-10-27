package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestXenginePlayWindows(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var dirs [2]string
	for i := range dirs {
		dirs[i] = t.TempDir()
		args := []string{"-set", "FDN", "-chunk", "0", "-games", "1", "-seed", "1",
			"-cards", "../../.cards", "-manifests", manifestDir, "-windows", "-out", dirs[i]}
		if err := runPlay(args); err != nil {
			t.Fatal(err)
		}
	}
	dir := dirs[0]

	itemIDs := map[string]string{} // id -> template
	if err := readLines(filepath.Join(dir, "items.jsonl"), func(b []byte) error {
		var it struct{ ID, Template string }
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		itemIDs[it.ID] = it.Template
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	gorgeIDs := map[string]bool{}
	if err := readLines(filepath.Join(dir, "gorge.jsonl"), func(b []byte) error {
		var r gorgeRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		gorgeIDs[r.ID] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var tapes, wins, refused, nonRefused int
	lowerOK := map[string]bool{}
	if err := readLines(filepath.Join(dir, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		if r.Status == statusRefused {
			refused++
			if len(r.Reasons) == 0 || r.Turn <= 0 || r.Item != nil || r.Tape != "" {
				t.Errorf("%s: malformed refusal %+v", r.ID, r)
			}
			if _, ok := itemIDs[r.ID]; ok || gorgeIDs[r.ID] {
				t.Errorf("%s: refused but has an item or gorge result", r.ID)
			}
			return nil
		}
		nonRefused++
		if r.Status != lower.StatusOK {
			return nil
		}
		lowerOK[r.ID] = true
		tmpl, ok := itemIDs[r.ID]
		if !ok || !gorgeIDs[r.ID] {
			t.Errorf("%s: lower_ok without item (%v) or gorge result (%v)", r.ID, ok, gorgeIDs[r.ID])
		}
		if tmpl == lower.Template {
			tapes++
		}
		if tmpl != lower.WindowTemplate {
			return nil
		}
		wins++
		if !windowID.MatchString(r.ID) || r.Tape == "" {
			t.Errorf("%s: window id or tape %q", r.ID, r.Tape)
			return nil
		}
		f, err := os.Open(filepath.Join(dir, r.Tape))
		if err != nil {
			return err
		}
		defer f.Close()
		tp, err := game.ReadTape(f)
		if err != nil {
			return err
		}
		if len(tp.Entries) == 0 {
			t.Errorf("%s: empty window tape", r.ID)
		}
		for i, e := range tp.Entries {
			if e.Turn != 1 {
				t.Errorf("%s: tape entry %d is turn %d, want 1 (rebased and trimmed)", r.ID, i, e.Turn)
				break
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if tapes == 0 || wins == 0 || refused == 0 {
		t.Fatalf("tapes %d, windows %d, refusals %d: want all > 0", tapes, wins, refused)
	}
	for id, tmpl := range itemIDs {
		if !lowerOK[id] {
			t.Errorf("%s (%s): item without a lower_ok row", id, tmpl)
		}
		if tmpl == lower.WindowTemplate && !windowID.MatchString(id) {
			t.Errorf("window item %s: id does not end in :w<turn>", id)
		}
	}

	// A diff skips the refusals and agrees on everything else.
	tmp := t.TempDir()
	rows := filepath.Join(tmp, "rows.jsonl")
	goldenRows(t, dir, rows, nil)
	out := filepath.Join(tmp, "agree")
	if err := diff(reg, dir, "xmage", rows, out, ""); err != nil {
		t.Fatal(err)
	}
	got := verdictClasses(t, filepath.Join(out, "verdicts.jsonl"))
	if len(got) != nonRefused {
		t.Errorf("%d verdicts, want %d (every non-refused lower.jsonl row)", len(got), nonRefused)
	}
	for id, c := range got {
		if c != classify.ClassAgree {
			t.Errorf("%s: class %s, want agree", id, c)
		}
	}

	// Determinism: a second run is byte-identical.
	for _, f := range []string{"items.jsonl", "lower.jsonl", "gorge.jsonl", "exercised.tsv"} {
		a, err := os.ReadFile(filepath.Join(dirs[0], f))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dirs[1], f))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs between two runs", f)
		}
	}
}
