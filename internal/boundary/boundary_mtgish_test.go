package boundary_test

import (
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// The mtgish cross-check (docs/design/2026-10-20-mtgish-crosscheck.md) reads
// mtgish's card data, pinned by oracle/mtgish/MTGISH.lock and fetched into
// the gitignored .mtgish/<ref>/ by `make mtgish-fetch`. mtgish's code is MIT,
// but its data is derived card by card from Oracle text, which is Wizards'
// copyright and is never committed here (the rule cmd/oraclepacket's
// .oracle-packets/ follows). The committed lock holds only a ref and
// hashes; the committed ledgers hold only summary tokens.
//
// A candidate file (tracked, or untracked and not ignored: see
// boundary_test.go) is an offender when it is any of:
//
//  1. a path with a `.mtgish` component (a copy of the fetch directory);
//  2. a file whose base name starts with `mtgish.lines.` (the data file,
//     under any extension: .json, .ron);
//  3. a file whose content starts with an mtgish card record (a copied data
//     line, renamed).

// mtgishRecordPrefix is how every line of mtgish.lines.json starts,
// assembled from two pieces so that this file never starts with it.
const mtgishRecordPrefix = `{"_Oracle` + `Card":`

func mtgishViolations(p string, blob []byte) []string {
	slash := path.Clean(filepath.ToSlash(p))
	var out []string
	if hasPathComponent(slash, ".mtgish") {
		out = append(out, "path under .mtgish/ (the fetched mtgish data)")
	}
	if strings.HasPrefix(path.Base(slash), "mtgish.lines.") {
		out = append(out, "the mtgish data file (mtgish.lines.*)")
	}
	if blob != nil && strings.HasPrefix(strings.TrimLeft(string(blob), "\xef\xbb\xbf \t\r\n"), mtgishRecordPrefix) {
		out = append(out, "content is an mtgish card record")
	}
	return out
}

func checkMtgishInRepo(repoDir string) ([]scriptOffender, error) {
	paths, err := candidateFiles(repoDir)
	if err != nil {
		return nil, err
	}
	var offenders []scriptOffender
	for _, p := range paths {
		for _, reason := range mtgishViolations(p, readCandidateBlob(repoDir, p)) {
			offenders = append(offenders, scriptOffender{p, reason})
		}
	}
	return offenders, nil
}

// TestNoMtgishDataTracked fails if mtgish's card data is tracked or one
// `git add` away from it.
func TestNoMtgishDataTracked(t *testing.T) {
	root := repoRoot(t)
	offenders, err := checkMtgishInRepo(root)
	if err != nil {
		t.Fatalf("git unavailable while scanning %s: %v", root, err)
	}
	if len(offenders) > 0 {
		var b strings.Builder
		b.WriteString("mtgish card data is tracked (or one `git add` from it); it carries Oracle text and is never committed:\n")
		for _, o := range offenders {
			b.WriteString("  " + o.path + "  (" + o.reason + ")\n")
		}
		t.Fatalf("%s", b.String())
	}
}

// TestMtgishBoundaryDetection plants each shape in a throwaway repository.
func TestMtgishBoundaryDetection(t *testing.T) {
	record := mtgishRecordPrefix + `"Card","Name":"Test Card"}` + "\n"
	tests := []struct {
		name       string
		withIgnore bool
		file       string
		body       string
		wantMatch  bool
	}{
		{"fetched data committed", false, ".mtgish/abc/mtgish.lines.json", record, true},
		{"fetched licence under .mtgish", false, ".mtgish/abc/LICENSE", "MIT\n", true},
		{"data file elsewhere", false, "testdata/mtgish.lines.json", "[]\n", true},
		{"data file in ron", false, "data/mtgish.lines.ron", "()\n", true},
		{"renamed data line", false, "oracle/mtgish/testdata/card.jsonl", record, true},
		{"ignored fetch dir", true, ".mtgish/abc/mtgish.lines.json", record, false},
		{"the lock file", false, "oracle/mtgish/MTGISH.lock", "ref abc\nsha256 00 data/mtgish.lines.json\n", false},
		{"a ledger", false, "oracle/mtgish/known/FDN.tsv", "card\ttier\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newTempRepo(t, true)
			if tt.withIgnore {
				writeFile(t, dir, ".gitignore", ".mtgish/\n")
			}
			writeFile(t, dir, tt.file, tt.body)
			offenders, err := checkMtgishInRepo(dir)
			if err != nil {
				t.Fatalf("git should be available in temp repo: %v", err)
			}
			matched := false
			for _, o := range offenders {
				matched = matched || o.path == tt.file
			}
			if matched != tt.wantMatch {
				t.Errorf("%q flagged=%v, want %v: %v", tt.file, matched, tt.wantMatch, offenders)
			}
		})
	}
}
