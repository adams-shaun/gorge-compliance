// Command testselect prints the Go test packages a pre-merge gate must run
// for a set of changed files. It is the executable form of
// internal/testselect; scripts/test-select.sh and the agentctl
// "go test (touched)" gate call it.
//
//	testselect -base "$(git merge-base origin/develop HEAD)"
//	git diff --name-only origin/develop... | testselect
//	testselect -files changed.txt
//
// The output is one package per line, sorted. A pin change prints every
// package in the module (go list ./...), not the ./... pattern, so the
// caller can split the slow packages out before running go test.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/internal/testselect"
)

func main() {
	base := flag.String("base", "", "git ref to diff against (default: read changed paths on stdin)")
	head := flag.String("head", "HEAD", "git ref for the changed side of -base")
	root := flag.String("root", ".", "module root")
	files := flag.String("files", "", `read changed paths from this file ("-" for stdin) instead of git`)
	flag.Parse()

	changed, err := changedFiles(*root, *base, *head, *files)
	if err != nil {
		fail(err)
	}
	pkgs, err := testselect.Packages(os.DirFS(*root), changed)
	if err != nil {
		fail(err)
	}
	if len(pkgs) == 1 && pkgs[0] == testselect.All {
		pkgs, err = allPackages(*root)
		if err != nil {
			fail(err)
		}
	}
	for _, pkg := range pkgs {
		fmt.Println(pkg)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "testselect:", err)
	os.Exit(2)
}

// changedFiles returns the changed repo-relative paths from -files, -base, or
// stdin, in that order.
func changedFiles(root, base, head, files string) ([]string, error) {
	switch {
	case files == "-":
		return scanLines(os.Stdin)
	case files != "":
		f, err := os.Open(files)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return scanLines(f)
	case base != "":
		cmd := exec.Command("git", "-C", root, "diff", "--name-only", base, head)
		out, err := cmd.Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return nil, fmt.Errorf("git diff --name-only %s %s: %s", base, head, strings.TrimSpace(string(ee.Stderr)))
			}
			return nil, err
		}
		return splitLines(string(out)), nil
	default:
		return scanLines(os.Stdin)
	}
}

func scanLines(f *os.File) ([]string, error) {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

func splitLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// allPackages expands testselect.All with go list, so the caller's slow/fast
// split still sees concrete package paths.
func allPackages(root string) ([]string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list ./...: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var pkgs []string
	for _, dir := range splitLines(string(out)) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		rel, err := filepath.Rel(abs, dir)
		if err != nil {
			return nil, err
		}
		if rel == "." {
			pkgs = append(pkgs, ".")
			continue
		}
		pkgs = append(pkgs, "./"+filepath.ToSlash(rel))
	}
	sort.Strings(pkgs)
	return pkgs, nil
}
