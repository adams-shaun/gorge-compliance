package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExitCodes(t *testing.T) {
	const schema = "../../carddb/schema"
	committed := "../../carddb"
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "README.md"), []byte("A:"+"AB$ Draw | Cost$ T\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "sets.tsv"), []byte("#carddb sets v1\nset_code\tname\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		code int
		out  string
	}{
		{[]string{"validate", "-schema", schema, "-tree", committed}, 0, "validate: ok"},
		{[]string{"boundary", "-schema", schema, "-tree", committed}, 0, "boundary: ok"},
		{[]string{"boundary", "-schema", schema, "-tree", bad}, 1, "README.md:1: Forge-script-shaped line"},
		{[]string{"validate", "-schema", schema, "-tree", bad}, 1, "sets.tsv:2: columns differ"},
		{[]string{"validate", "-schema", schema}, 2, ""},
		{[]string{"validate", "-schema", schema, "-tree", committed, "-scope", "x"}, 2, ""},
		{[]string{"validate", "-schema", "nowhere", "-tree", committed}, 2, ""},
		{[]string{"bogus"}, 2, ""},
		{nil, 2, ""},
	} {
		var o, e bytes.Buffer
		code := run(tc.args, &o, &e)
		if code != tc.code || !strings.Contains(o.String(), tc.out) {
			t.Errorf("%v: exit %d, want %d; stdout %q stderr %q", tc.args, code, tc.code, o.String(), e.String())
		}
	}
}
