// Package testselect maps a pull request's changed files to the Go test
// packages a pre-merge gate must run.
//
// The gate used to run only the nearest enclosing package of every changed
// file. That is enough for ordinary code, but the compliance verdicts are
// data, not an import edge: oracle/compliance/verdicts/*.jsonl are hashed
// from oraclegen's XMage answers, and the XMage driver writes the snapshots
// they are compared against. An oraclegen or driver change therefore ran the
// generator's own tests and left the committed verdicts stale; develop went
// red on 2026-11-24 with 27 re-hashed rows that no PR gate had replayed.
//
// The fix matches the role, not the one path. Any change under a verdict
// producer selects the packages whose tests emit the merge gate's
// GORGE-CERT-RESULT ratchet verdict (adopt and gate today). Those packages are
// found by that contract rather than by a hand-kept list, so a new ratchet
// that follows the same contract is selected automatically.
package testselect

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// All is the selection for a pin bump: every package in the module, as
// `go test` takes the pattern.
const All = "./..."

// certResultMarker is the line the verdict-ratchet tests log for the merge
// gate's stale-only classifier. It is the contract that identifies the
// packages a verdict-producer change must run.
const certResultMarker = "GORGE-CERT-RESULT"

// complianceRoot is the tree that holds the committed verdicts and the
// ratchets that check them.
const complianceRoot = "oracle/compliance"

// verdictProducerPrefixes are the trees whose changes can re-hash or rewrite
// a committed verdict: the generator, the XMage replay driver, and the tools
// that compute and write the rows. The verdict files are data, so the import
// graph cannot discover this dependency; the prefix is the role.
var verdictProducerPrefixes = []string{
	complianceRoot + "/",
	"oracle/xmageoracle/",
	"cmd/oraclediff/",
	"cmd/compliance/",
	"oracle/validation/oracle/", // full-replay.sh, std-write.sh: write verdict rows
}

// verdictProducerFiles are the regen recipe's scripts and the build file that
// pins its replay inputs. A change here can change which scenarios are
// replayed, how a verdict is written, or which XMage replay produced it.
//
// Makefile is the whole class: it carries XMAGE_REF (the design names it as a
// verdict-validity input: "the generator, gorge's head or XMAGE_REF changes"),
// and it may carry another replay pin tomorrow. Treating the file as a producer
// keeps a pin bump from running no tests, without a per-pin list to forget.
var verdictProducerFiles = map[string]bool{
	"Makefile":                      true,
	"scripts/compliance-pass.sh":    true,
	"scripts/xmage-oracle-run.sh":   true,
	"scripts/xmage-oracle-setup.sh": true,
	"scripts/forge-pass.sh":         true, // --verdicts writes Forge-only rows
	"scripts/forge-oracle-run.sh":   true,
	"scripts/forge-oracle-setup.sh": true,
}

// forgeDriverFixturePrefix names scripts/forge-oracle-test-*.sh, the Forge
// driver regression fixtures. cmd/oraclediff's
// TestForgeScriptFixturesCarryTheirItemSHA reads every one of them.
const forgeDriverFixturePrefix = "scripts/forge-oracle-test-"

// Packages returns the Go test packages for changed repo-relative paths.
// fsys must be rooted at the module root. The result is sorted and unique;
// it is []string{All} when a pin file changed, because that re-links every
// package against a new gorge.
func Packages(fsys fs.FS, changed []string) ([]string, error) {
	set := map[string]struct{}{}
	producer := false
	for _, raw := range changed {
		f := normalize(raw)
		if f == "" {
			continue
		}
		if isPin(f) {
			return []string{All}, nil
		}
		if strings.HasPrefix(f, "scenarios/") {
			// The hand-authored and XMage-port corpora are consumed by ./audit,
			// whatever their own directory contains, and replayed at four seats
			// by ./cmd/padseats, whose record scenarios/padseats.tsv names them.
			set["./audit"] = struct{}{}
			set["./cmd/padseats"] = struct{}{}
		}
		if strings.HasPrefix(f, forgeDriverFixturePrefix) {
			// The Forge driver fixtures hold hand-authored requests whose
			// identity cmd/oraclediff checks; the script is no Go file, so
			// the role selects the package.
			set["./cmd/oraclediff"] = struct{}{}
		}
		if isVerdictProducer(f) {
			producer = true
		}
		pkg, err := enclosingPackage(fsys, f)
		if err != nil {
			return nil, err
		}
		if pkg != "" {
			set[pkg] = struct{}{}
		}
	}
	if producer {
		ratchets, err := ratchetPackages(fsys)
		if err != nil {
			return nil, err
		}
		for _, pkg := range ratchets {
			set[pkg] = struct{}{}
		}
	}
	return sorted(set), nil
}

// normalize turns a changed path into a clean, slash-separated path relative
// to the module root, or "" for the root itself.
func normalize(f string) string {
	f = strings.TrimSpace(f)
	f = strings.TrimPrefix(f, "./")
	f = path.Clean(f)
	if f == "." || f == "/" {
		return ""
	}
	return f
}

func isPin(f string) bool {
	switch f {
	case "go.mod", "go.sum", "GORGE_PIN":
		return true
	}
	return false
}

func isVerdictProducer(f string) bool {
	for _, prefix := range verdictProducerPrefixes {
		if strings.HasPrefix(f, prefix) {
			return true
		}
	}
	return verdictProducerFiles[f]
}

// enclosingPackage is the nearest ancestor directory of f that holds a Go
// source file: the package a test for that file belongs to. A file whose
// ancestors have no Go package (docs, the Java driver) selects nothing beyond
// the role rules.
func enclosingPackage(fsys fs.FS, f string) (string, error) {
	for dir := path.Dir(f); dir != "." && dir != "/"; dir = path.Dir(dir) {
		ok, err := hasGoFiles(fsys, dir)
		if err != nil {
			return "", err
		}
		if ok {
			return "./" + dir, nil
		}
	}
	return "", nil
}

func hasGoFiles(fsys fs.FS, dir string) (bool, error) {
	ents, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil // the diff deleted the directory too: keep walking up
		}
		return false, err
	}
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true, nil
		}
	}
	return false, nil
}

// ratchetPackages finds the packages under the compliance oracle that emit
// the merge gate's ratchet verdict. This is the structural half of the fix:
// a new ratchet package that logs GORGE-CERT-RESULT is selected without
// editing a package list here.
func ratchetPackages(fsys fs.FS) ([]string, error) {
	dirs := map[string]struct{}{}
	err := fs.WalkDir(fsys, complianceRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte(certResultMarker)) {
			dirs["./"+path.Dir(p)] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s for %s: %w", complianceRoot, certResultMarker, err)
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no package under %s emits %s: the verdict ratchets cannot be identified", complianceRoot, certResultMarker)
	}
	return sorted(dirs), nil
}

func sorted(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for pkg := range set {
		out = append(out, pkg)
	}
	sort.Strings(out)
	return out
}
