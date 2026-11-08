package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
)

// followerRow builds one ordinary Forge follower row.
func followerRow(id string, ms int) []byte {
	b, err := json.Marshal(struct {
		oraclediff.XResult
		Engine string `json:"engine"`
	}{
		XResult: oraclediff.XResult{ID: id, Name: id, Strict: true, MS: ms},
		Engine:  "forge",
	})
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}

// TestReadFollowerRowsOversizedRow: one row over the cap is returned as a
// harness row naming its size, and the rows around it are still read.
func TestReadFollowerRowsOversizedRow(t *testing.T) {
	old := followerRowCap
	followerRowCap = 4 << 10
	t.Cleanup(func() { followerRowCap = old })

	const bigID = "row-big"
	prefix := `{"name":"` + bigID + `","id":"` + bigID + `","engine":"forge","padding":"`
	bigLine := prefix + strings.Repeat("x", followerRowCap) + `"}`
	if len(bigLine) <= followerRowCap {
		t.Fatalf("oversized fixture is %d bytes, not over the %d-byte cap", len(bigLine), followerRowCap)
	}

	path := filepath.Join(t.TempDir(), "rows.jsonl")
	data := append(followerRow("row-a", 1), []byte(bigLine+"\n")...)
	data = append(data, followerRow("row-c", 3)...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	rows, err := readFollowerRows(path, "forge")
	if err != nil {
		t.Fatalf("readFollowerRows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("read %d rows, want 3 (%v)", len(rows), rows)
	}
	for id, ms := range map[string]int{"row-a": 1, "row-c": 3} {
		x, ok := rows[id]
		if !ok {
			t.Errorf("row %q was not read", id)
			continue
		}
		if x.Harness != "" || x.MS != ms {
			t.Errorf("row %q: %+v, want ms %d and no harness", id, x, ms)
		}
	}

	big, ok := rows[bigID]
	if !ok {
		t.Fatalf("oversized %q is not in %v", bigID, rows)
	}
	if big.Harness == "" {
		t.Fatalf("oversized %q is not a harness row: %+v", bigID, big)
	}
	if !strings.Contains(big.Harness, strconv.Itoa(len(bigLine))) {
		t.Errorf("harness reason %q does not name the %d-byte row", big.Harness, len(bigLine))
	}
	if !strings.Contains(big.Harness, strconv.Itoa(followerRowCap)) {
		t.Errorf("harness reason %q does not name the %d-byte cap", big.Harness, followerRowCap)
	}
}

// TestDiffOversizedFollowerRow: xengine diff on a rows file with one row over
// the cap succeeds and classifies that item as harness, not as a lost chunk.
func TestDiffOversizedFollowerRow(t *testing.T) {
	reg, play := playTwo(t)

	var (
		ids   []string
		lines [][]byte
	)
	if err := readLines(filepath.Join(play, "gorge.jsonl"), func(b []byte) error {
		var r gorgeRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		raw, err := json.Marshal(struct {
			oraclediff.XResult
			Engine string `json:"engine"`
		}{
			XResult: oraclediff.XResult{ID: r.ID, Name: r.ID, Strict: true, Snapshots: r.Result.Snapshots},
			Engine:  "forge",
		})
		if err != nil {
			return err
		}
		ids = append(ids, r.ID)
		lines = append(lines, raw)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(ids) < 2 {
		t.Fatalf("play kept %d items, want at least 2", len(ids))
	}

	// Cap the reader just above every ordinary row: the fixture stays a few
	// KiB instead of 256 MiB, and the capped row is still the only one over.
	maxLine := 0
	for _, line := range lines {
		if len(line) > maxLine {
			maxLine = len(line)
		}
	}
	old := followerRowCap
	followerRowCap = maxLine + 1024
	t.Cleanup(func() { followerRowCap = old })

	target := ids[0]
	quote := func(s string) string {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	bigLine := `{"name":` + quote(target) + `,"id":` + quote(target) +
		`,"engine":"forge","padding":"` + strings.Repeat("x", followerRowCap) + `"}`
	if len(bigLine) <= followerRowCap {
		t.Fatalf("oversized fixture is %d bytes, not over the %d-byte cap", len(bigLine), followerRowCap)
	}

	var rows bytes.Buffer
	for i, id := range ids {
		if id == target {
			rows.WriteString(bigLine)
		} else {
			rows.Write(lines[i])
		}
		rows.WriteByte('\n')
	}
	rowsPath := filepath.Join(t.TempDir(), "forge.jsonl")
	if err := os.WriteFile(rowsPath, rows.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "diff-forge")
	if err := diff(reg, play, "forge", rowsPath, out, ""); err != nil {
		t.Fatalf("diff with one oversized follower row: %v", err)
	}

	findings := map[string]classify.Finding{}
	if err := readLines(filepath.Join(out, "verdicts.jsonl"), func(b []byte) error {
		var f classify.Finding
		if err := json.Unmarshal(b, &f); err != nil {
			return err
		}
		findings[f.ID] = f
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(findings) != len(ids) {
		t.Fatalf("classified %d of %d items", len(findings), len(ids))
	}
	for _, id := range ids {
		f, ok := findings[id]
		if !ok {
			t.Errorf("item %q is missing from verdicts.jsonl", id)
			continue
		}
		if id != target {
			if f.Class != classify.ClassAgree {
				t.Errorf("item %q: class %s, want agree", id, f.Class)
			}
			continue
		}
		if f.Class != classify.ClassHarness {
			t.Errorf("oversized item %q: class %s, want harness", id, f.Class)
		}
		if !strings.Contains(f.Msg, strconv.Itoa(len(bigLine))) {
			t.Errorf("harness reason %q does not name the %d-byte row", f.Msg, len(bigLine))
		}
		if !strings.Contains(f.Msg, strconv.Itoa(followerRowCap)) {
			t.Errorf("harness reason %q does not name the %d-byte cap", f.Msg, followerRowCap)
		}
	}
}
