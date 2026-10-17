package boundary_test

import (
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// TestForgeBoundary is the licence guard from the Forge oracle design (§11,
// design decision P1-7), ported from gorge's engine/cards/boundary_forge_test.go.
// Forge is GPL-3.0. Nothing that belongs to Forge — its Java source, its
// oracle driver tree, its result rows or request files — may be committed to
// four-yay. boundary_test.go guards the card-script corpus; this guard covers
// the four Forge-artifact shapes the design names.
//
// A candidate file (tracked, or untracked and not ignored: see
// boundary_test.go) is an offender when it is any of:
//
//  1. a *.java file declaring `package forge.`;
//  2. a path with a `forge-oracle` component (any depth);
//  3. a path with a `compliance` component immediately followed by a
//     component beginning `forge` — `oracle/compliance/forge*`, the
//     result-row / request-file surface (it was `lab/oracle/compliance/forge*`
//     while it lived in gorge; the rule is written by component so it follows
//     the tree if it moves again);
//  4. a file carrying the Forge GPL header line.
//
// forgeBoundaryAllowlist is the explicit, empty-by-default escape hatch for
// the §9 adjudication ledger. The design (§9, recommended option C) permits
// committing `compliance/adjudication/<a-z>.jsonl`, which carries only our own
// pattern vocabulary and field names — never a Forge-produced value, message
// or script fragment. That path is not under `compliance/forge*`, so it needs
// no entry; the allowlist exists for a path the design later names that a
// rule would otherwise catch. Entries are repo-relative slash-separated paths
// matched exactly. four-yay has no oracle/compliance/adjudication/ yet; keep
// this empty until something the design allows actually needs it.
var forgeBoundaryAllowlist = map[string]bool{}

// forgeGPLHeader is the distinguishing line of the Forge source header: a
// full sentence, so a file merely discussing the GPL is not mistaken for
// Forge source, assembled from two pieces so that this file — which is itself
// scanned — never literally contains it.
const forgeGPLHeader = "Forge: " + "Play Magic: the Gathering."

// hasPathComponent reports whether slash (a slash-separated path) contains
// name as a whole component.
func hasPathComponent(slash, name string) bool {
	for _, part := range strings.Split(slash, "/") {
		if part == name {
			return true
		}
	}
	return false
}

// underComplianceForge reports whether slash has a `compliance` component
// immediately followed by a component that begins with "forge".
func underComplianceForge(slash string) bool {
	parts := strings.Split(slash, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "compliance" && strings.HasPrefix(parts[i+1], "forge") {
			return true
		}
	}
	return false
}

type forgeBoundaryOffender struct {
	path   string
	reason string
}

// forgeBoundaryViolations classifies a repo-relative path and its blob (nil
// when no copy could be read; the path rules still apply).
func forgeBoundaryViolations(p string, blob []byte) []forgeBoundaryOffender {
	if forgeBoundaryAllowlist[p] {
		return nil
	}
	var out []forgeBoundaryOffender
	slash := path.Clean(filepath.ToSlash(p))

	// Rule 2.
	if hasPathComponent(slash, "forge-oracle") {
		out = append(out, forgeBoundaryOffender{p, "path under forge-oracle/"})
	}
	// Rule 3.
	if underComplianceForge(slash) {
		out = append(out, forgeBoundaryOffender{p, "path under compliance/forge*"})
	}
	if blob != nil {
		// Rule 1.
		if strings.HasSuffix(slash, ".java") {
			for _, line := range strings.Split(string(blob), "\n") {
				trimmed := strings.TrimSuffix(strings.TrimSpace(line), ";")
				if strings.HasPrefix(trimmed, "package forge.") || trimmed == "package forge" {
					out = append(out, forgeBoundaryOffender{p, "*.java declaring package forge."})
					break
				}
			}
		}
		// Rule 4.
		if strings.Contains(string(blob), forgeGPLHeader) {
			out = append(out, forgeBoundaryOffender{p, "carries the Forge GPL header"})
		}
	}
	return out
}

// checkForgeBoundaryInRepo scans every candidate file of the repository at
// repoDir. The error is non-nil only when git fails.
func checkForgeBoundaryInRepo(repoDir string) ([]forgeBoundaryOffender, error) {
	paths, err := candidateFiles(repoDir)
	if err != nil {
		return nil, err
	}
	var offenders []forgeBoundaryOffender
	for _, p := range paths {
		offenders = append(offenders, forgeBoundaryViolations(p, readCandidateBlob(repoDir, p))...)
	}
	return offenders, nil
}

// TestForgeBoundary fails if any candidate file is Forge source, a
// forge-oracle/ tree entry, a compliance/forge* artifact, or carries the
// Forge GPL header.
func TestForgeBoundary(t *testing.T) {
	root := repoRoot(t)
	offenders, err := checkForgeBoundaryInRepo(root)
	if err != nil {
		t.Fatalf("git unavailable while scanning %s: %v", root, err)
	}
	if len(offenders) > 0 {
		var b strings.Builder
		b.WriteString("Forge artifacts are tracked (or one `git add` from it), which breaks the GPL boundary:\n")
		for _, o := range offenders {
			b.WriteString("  " + o.path + "  (" + o.reason + ")\n")
		}
		t.Fatalf("%s", b.String())
	}
}

// TestForgeBoundaryDetection plants each shape the guard exists to catch in a
// throwaway repository and requires the guard to flag it; the allowlist is
// exercised too, with a control proving the allowlisted case can fail.
func TestForgeBoundaryDetection(t *testing.T) {
	gplHeader := "Forge: " + "Play Magic: the Gathering."

	tests := []struct {
		name      string
		file      string
		body      string
		allow     bool
		staged    bool // git add the file (otherwise it is left untracked-but-addable)
		wantMatch bool
		reason    string
	}{
		{"java declaring package forge", "src/main/java/forge/game/Game.java",
			"/* header */\npackage forge.game;\n\nclass Game {}\n", false, true, true,
			"*.java declaring package forge. is Forge source"},
		{"java in a non-forge package", "src/main/java/com/example/App.java",
			"package com.example;\n\nclass App {}\n", false, true, false,
			"a Java file not declaring package forge. is not Forge source"},
		{"file under forge-oracle", "forge-oracle/src/main/java/forge/oracle/ScenarioReplay.java",
			"package forge.oracle;\n", false, true, true,
			"any path under forge-oracle/ belongs to the GPL driver"},
		{"nested file under forge-oracle", "tools/forge-oracle/pom.xml",
			"<project/>\n", false, true, true, "forge-oracle/ is caught at any depth"},
		{"oracle/compliance/forge dir", "oracle/compliance/forge-verdicts/a.jsonl",
			"{}\n", false, true, true, "oracle/compliance/forge* is forbidden"},
		{"oracle/compliance/forge file", "oracle/compliance/forge.jsonl",
			"{}\n", false, true, true, "a forge* file directly under compliance/ is caught too"},
		{"old lab/oracle/compliance/forge path", "lab/oracle/compliance/forge-verdicts/a.jsonl",
			"{}\n", false, true, true, "the rule follows the compliance tree wherever it sits"},
		{"untracked compliance/forge in a repo", "oracle/compliance/forge-rows/r.jsonl",
			"{}\n", false, false, true, "an addable artifact fails before it is staged"},
		{"forge file not under compliance", "cmd/oraclediff/forge.go",
			"package main\n", false, true, false, "only compliance/forge* is a path offender"},
		{"file carrying the Forge GPL header", "internal/oracle/snapshot.go",
			"package oracle\n\n// " + gplHeader + "\n", false, true, true,
			"the Forge GPL header must never enter a four-yay file"},
		{"innocuous Go file", "internal/oracle/snapshot.go",
			"package oracle\n\n// A snapshot read from an external process.\n", false, true, false,
			"an ordinary file is not an offender"},
		{"allowlisted forge path", "oracle/compliance/forge-verdicts/a.jsonl",
			"{}\n", true, true, true,
			"control: the same path IS flagged once the allowlist entry is removed"},
		{"ledger path is allowed", "oracle/compliance/adjudication/a.jsonl",
			"{}\n", false, true, false,
			"the section 9 ledger lives under compliance/adjudication and is allowed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newTempRepo(t, true)
			writeFile(t, dir, tt.file, tt.body)
			if tt.staged {
				gitAdd(t, dir, tt.file)
			}
			// Precondition: the file really is a candidate, so a guard that
			// silently skipped it could not pass.
			paths, err := candidateFiles(dir)
			if err != nil {
				t.Fatalf("git ls-files failed: %v", err)
			}
			found := false
			for _, p := range paths {
				found = found || p == tt.file
			}
			if !found {
				t.Fatalf("precondition failed: %q is not a candidate (%v)", tt.file, paths)
			}

			if tt.allow {
				forgeBoundaryAllowlist[tt.file] = true
				allowed, err := checkForgeBoundaryInRepo(dir)
				delete(forgeBoundaryAllowlist, tt.file)
				if err != nil {
					t.Fatalf("git unavailable: %v", err)
				}
				if len(allowed) != 0 {
					t.Fatalf("allowlisted %q was still flagged: %v", tt.file, allowed)
				}
			}

			offenders, err := checkForgeBoundaryInRepo(dir)
			if err != nil {
				t.Fatalf("git should be available in temp repo: %v", err)
			}
			matched := false
			for _, o := range offenders {
				matched = matched || o.path == tt.file
			}
			if matched != tt.wantMatch {
				t.Errorf("%q flagged=%v, want %v: %v (reason: %s)", tt.file, matched, tt.wantMatch, offenders, tt.reason)
			}
		})
	}
}
