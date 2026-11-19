package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// diagnosedCorpus copies the fixture corpus into a t.TempDir() as
// cardsfolder/<rel> and returns the temp dir. The fixtures themselves stay
// outside any cardsfolder path, so the licence guard never mistakes them for a
// Forge corpus.
func diagnosedCorpus(t *testing.T) string {
	t.Helper()
	src := filepath.Join("testdata", "diagnosed", "corpus")
	dir := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, "cardsfolder", rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatalf("build fixture corpus: %v", err)
	}
	return dir
}

// checkDiagnosedFile runs diagnosed -check on a fixture register and returns
// the lines it printed plus the command error.
func checkDiagnosedFile(t *testing.T, name string) (string, error) {
	t.Helper()
	var runErr error
	out := captureStdout(t, func() {
		runErr = runDiagnosed([]string{
			"-check",
			"-file", "testdata/diagnosed/" + name,
			"-cards", diagnosedCorpus(t),
		})
	})
	return out, runErr
}

func TestDiagnosedCheckValid(t *testing.T) {
	out, err := checkDiagnosedFile(t, "valid.tsv")
	if err != nil {
		t.Fatalf("valid register rejected: %v\n%s", err, out)
	}
	if out != "" {
		t.Fatalf("valid register printed errors:\n%s", out)
	}
}

// TestDiagnosedCheckDesignation: a farm row can be attributed to a designation
// whose inherent triggered abilities have no source (CR 725.2, 726.2), so the
// card column carries a name no corpus card has. The register accepts the
// same names the stack comparer normalises.
func TestDiagnosedCheckDesignation(t *testing.T) {
	out, err := checkDiagnosedFile(t, "designation.tsv")
	if err != nil {
		t.Fatalf("designation register rejected: %v\n%s", err, out)
	}
	if out != "" {
		t.Fatalf("designation register printed errors:\n%s", out)
	}
}

func TestDiagnosedCheckRejects(t *testing.T) {
	cases := []struct {
		file    string
		row     int
		want    string
		wantErr string
	}{
		{"bad_header.tsv", 1, "header must be", ""},
		{"bad_header_cols.tsv", 1, "header must be", ""},
		{"bad_cols.tsv", 2, "want 9 columns, got 8", ""},
		{"bad_kind.tsv", 2, `unknown kind "XY|Z"`, ""},
		{"bad_wrong.tsv", 2, `unknown wrong "Z"`, ""},
		{"bad_cr.tsv", 2, `bad cr "CR601"`, ""},
		{"bad_card.tsv", 2, `unknown card "Missing"`, ""},
		{"bad_ref.tsv", 2, "needs an upstream URL or a ticket id", ""},
		{"dup.tsv", 3, "duplicate (kind, set, card, field)", ""},
		// multi.tsv's single row breaks five rules; the summary counts the row
		// once, not the printed errors.
		{"multi.tsv", 2, `unknown kind "XY|Z"`, "1 invalid row(s)"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			out, err := checkDiagnosedFile(t, c.file)
			if err == nil {
				t.Fatalf("%s: invalid register accepted", c.file)
			}
			prefix := fmt.Sprintf("testdata/diagnosed/%s:%d: ", c.file, c.row)
			if !strings.Contains(out, prefix) {
				t.Errorf("%s: output missing %q:\n%s", c.file, prefix, out)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("%s: output missing reason %q:\n%s", c.file, c.want, out)
			}
			if c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: command error %q missing %q", c.file, err, c.wantErr)
			}
		})
	}
}
