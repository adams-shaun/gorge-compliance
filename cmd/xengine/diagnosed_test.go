package main

import (
	"fmt"
	"strings"
	"testing"
)

// checkDiagnosedFile runs diagnosed -check on a fixture register and returns
// the lines it printed plus the command error.
func checkDiagnosedFile(t *testing.T, name string) (string, error) {
	t.Helper()
	var runErr error
	out := captureStdout(t, func() {
		runErr = runDiagnosed([]string{
			"-check",
			"-file", "testdata/diagnosed/" + name,
			"-cards", "testdata/diagnosed",
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

func TestDiagnosedCheckRejects(t *testing.T) {
	cases := []struct {
		file string
		row  int
		want string
	}{
		{"bad_header.tsv", 1, "header must be"},
		{"bad_cols.tsv", 2, "want 9 columns, got 8"},
		{"bad_kind.tsv", 2, `unknown kind "XY|Z"`},
		{"bad_wrong.tsv", 2, `unknown wrong "Z"`},
		{"bad_cr.tsv", 2, `bad cr "CR601"`},
		{"bad_card.tsv", 2, `unknown card "Missing"`},
		{"bad_ref.tsv", 2, "needs an upstream URL or a ticket id"},
		{"dup.tsv", 3, "duplicate (kind, set, card, field)"},
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
		})
	}
}
