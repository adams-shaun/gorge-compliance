package boundary_test

import (
	"crypto/sha256"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// This file is the port of gorge's engine/cards/boundary_test.go: Forge card
// and token scripts are GPL-3.0 and four-yay never commits one.
//
// WHICH FILES ARE SCANNED. gorge scans `git ls-files` (the index). four-yay
// scans the index UNION the untracked files git would pick up on the next
// `git add` (`git ls-files --cached --others --exclude-standard`), for two
// reasons: the repository was created by an export and had no commit and an
// empty index, where an index-only scan passes vacuously; and a GPL file that
// is one `git add` away from being committed is worth failing on now. A file
// matched by .gitignore (the .cards symlink, bin/, status.md) is not a
// candidate: git will never commit it without -f.

// A Forge card script is identifiable by content, not path: it begins with a
// Name: line and carries an Oracle: line (every card and token script in the
// corpus does). A UTF-8 byte-order mark is tolerated. Checking content rather
// than directory means the guard survives someone vendoring the corpus
// somewhere unexpected, under any extension.
func looksLikeForgeScript(blob []byte) bool {
	s := strings.TrimPrefix(string(blob), "\xef\xbb\xbf")
	return strings.HasPrefix(s, "Name:") && strings.Contains(s, "\nOracle:")
}

// corpusDirComponents are the directory names the Forge corpus lives under
// (res/cardsfolder and res/tokenscripts upstream; .cards/cardsfolder and
// .cards/tokenscripts after `make fetch-cards`). A path with either as a
// whole component is an offender whatever it holds.
var corpusDirComponents = []string{"cardsfolder", "tokenscripts"}

// gitCmd builds a git command running with cards.GitEnv()'s environment:
// os.Environ() with every inherited GIT_* variable stripped, so a hook that
// exported GIT_DIR or GIT_INDEX_FILE cannot redirect the throwaway
// repositories these tests create into the repository the test runs under.
func gitCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Env = cards.GitEnv()
	return cmd
}

// repoRoot resolves the root of the git repository containing the test's
// working directory (the package directory), whatever depth it lives at.
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := gitCmd("-C", ".", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("could not resolve git repo root from working directory: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// candidateFiles lists every repo-relative, slash-separated path git tracks
// or would track on the next `git add`: the index plus untracked,
// non-ignored files (see the file comment). Works in a repository with no
// commit. The error is non-nil only when git itself fails.
func candidateFiles(repoDir string) ([]string, error) {
	out, err := gitCmd("-C", repoDir, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var paths []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	return paths, nil
}

// readCandidateBlob returns a candidate's content, preferring the working
// tree, then the staged index, then HEAD (absent in a repository with no
// commit). A symlink in the working tree is not followed: git commits the
// link, never its target, so the index/HEAD blob (the link text) is what
// counts. nil means no copy could be read; path rules still apply.
func readCandidateBlob(repoDir, p string) []byte {
	full := filepath.Join(repoDir, filepath.FromSlash(p))
	if fi, err := os.Lstat(full); err == nil && fi.Mode().IsRegular() {
		if data, err := os.ReadFile(full); err == nil {
			return data
		}
	}
	if data, err := gitCmd("-C", repoDir, "show", ":"+p).Output(); err == nil {
		return data
	}
	if data, err := gitCmd("-C", repoDir, "show", "HEAD:"+p).Output(); err == nil {
		return data
	}
	return nil
}

// scriptOffender is one candidate that breaches the card-script boundary,
// with the rule that caught it.
type scriptOffender struct {
	path   string
	reason string
}

// corpusHashes is the sha256 of every file in a live Forge corpus, so a
// verbatim copy is caught even if it somehow fails the content signature.
type corpusHashes map[[sha256.Size]byte]string

// loadCorpusHashes hashes cardsfolder/ and tokenscripts/ under corpusDir. It
// returns nil when no corpus is present (a fresh clone); the content and path
// rules still run.
func loadCorpusHashes(t *testing.T, corpusDir string) corpusHashes {
	t.Helper()
	if _, err := os.Stat(cards.CorpusDir(corpusDir)); err != nil {
		return nil
	}
	h := corpusHashes{}
	for _, sub := range corpusDirComponents {
		root := filepath.Join(corpusDir, sub)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(corpusDir, p)
			h[sha256.Sum256(data)] = filepath.ToSlash(rel)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("hashing corpus %s: %v", root, err)
		}
	}
	return h
}

// scriptViolations classifies one candidate path and its blob.
func scriptViolations(p string, blob []byte, corpus corpusHashes) []scriptOffender {
	var out []scriptOffender
	slash := path.Clean(p)
	for _, c := range corpusDirComponents {
		if hasPathComponent(slash, c) {
			out = append(out, scriptOffender{p, "path under a Forge corpus directory (" + c + "/)"})
		}
	}
	if blob != nil {
		if looksLikeForgeScript(blob) {
			out = append(out, scriptOffender{p, "content is a Forge card/token script (Name: ... Oracle:)"})
		}
		if src, ok := corpus[sha256.Sum256(blob)]; ok && len(blob) > 0 {
			out = append(out, scriptOffender{p, "byte-identical to corpus file " + src})
		}
	}
	return out
}

// checkForgeScriptsInRepo scans every candidate file of the repository at
// repoDir. corpus may be nil. The error is non-nil only when git fails.
func checkForgeScriptsInRepo(repoDir string, corpus corpusHashes) ([]scriptOffender, error) {
	paths, err := candidateFiles(repoDir)
	if err != nil {
		return nil, err
	}
	var offenders []scriptOffender
	for _, p := range paths {
		offenders = append(offenders, scriptViolations(p, readCandidateBlob(repoDir, p), corpus)...)
	}
	return offenders, nil
}

// TestNoForgeScriptsTracked enforces the GPL boundary: four-yay builds on the
// corpus `make fetch-cards` puts in gorge's gitignored .cards/, and never
// carries a script itself.
func TestNoForgeScriptsTracked(t *testing.T) {
	root := repoRoot(t)

	var corpus corpusHashes
	if dir, ok := testutil.SharedCorpusDir(root); ok {
		corpus = loadCorpusHashes(t, dir)
	}
	if corpus == nil {
		t.Logf("no .cards corpus present: the byte-identity rule is skipped; content and path rules still run")
	} else {
		t.Logf("byte-identity rule armed with %d corpus files", len(corpus))
	}

	paths, err := candidateFiles(root)
	if err != nil {
		t.Fatalf("git unavailable while scanning %s: %v", root, err)
	}
	// A guard over zero files passes vacuously; four-yay always has some.
	if len(paths) == 0 {
		t.Fatalf("no candidate files under %s: the guard would scan nothing", root)
	}
	t.Logf("scanning %d candidate files", len(paths))

	offenders, err := checkForgeScriptsInRepo(root, corpus)
	if err != nil {
		t.Fatalf("git unavailable while scanning %s: %v", root, err)
	}
	if len(offenders) > 0 {
		var b strings.Builder
		b.WriteString("Forge card scripts are tracked (or one `git add` from it), which breaks the GPL boundary:\n")
		for _, o := range offenders {
			b.WriteString("  " + o.path + "  (" + o.reason + ")\n")
		}
		t.Fatalf("%s", b.String())
	}
}

// newTempRepo initialises an empty git repository; withCommit adds an empty
// initial commit (four-yay itself started without one, so both shapes are
// exercised).
func newTempRepo(t *testing.T, withCommit bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := gitCmd("-C", dir, "init", "-q").Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	if withCommit {
		commit(t, dir, "--allow-empty", "-m", "initial")
	}
	return dir
}

func commit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.email=test@test.com", "-c", "user.name=Test",
		"-c", "commit.gpgsign=false", "commit", "-q"}, args...)
	if err := gitCmd(full...).Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}
}

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func gitAdd(t *testing.T, dir string, paths ...string) {
	t.Helper()
	if err := gitCmd(append([]string{"-C", dir, "add", "--"}, paths...)...).Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
}

// TestForgeScriptDetection verifies the guard catches a script in every state
// git can hold it in, including the untracked-but-addable state four-yay
// adds, and does not flag ignored or innocuous files.
func TestForgeScriptDetection(t *testing.T) {
	forgeSignature := "Name:Test Card\nManaCost:1 U\nTypes:Creature Test\nPT:1/1\nOracle:Draw a card.\n"
	innocentText := "This is just a normal text file\nwith multiple lines\nbut no Forge signature"
	// A file that fails the content signature but is byte-identical to a
	// (fake) corpus file: only the identity rule can catch it.
	corpusOnly := "Types:Creature Test\nPT:2/2\n"
	fakeCorpus := corpusHashes{sha256.Sum256([]byte(corpusOnly)): "cardsfolder/t/test.txt"}

	tests := []struct {
		name           string
		withCommit     bool
		setup          func(t *testing.T, dir string)
		expectOffender bool
		reason         string
	}{
		{
			name: "script staged but not committed", withCommit: true,
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, "card.txt", forgeSignature)
				gitAdd(t, dir, "card.txt")
			},
			expectOffender: true, reason: "guard must catch scripts in the staging area",
		},
		{
			name: "script committed", withCommit: true,
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, "card.txt", forgeSignature)
				gitAdd(t, dir, "card.txt")
				commit(t, dir, "-m", "test")
			},
			expectOffender: true, reason: "guard must catch committed scripts",
		},
		{
			name: "committed script deleted from working tree", withCommit: true,
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, "card.txt", forgeSignature)
				gitAdd(t, dir, "card.txt")
				commit(t, dir, "-m", "test")
				if err := os.Remove(filepath.Join(dir, "card.txt")); err != nil {
					t.Fatal(err)
				}
			},
			expectOffender: true, reason: "the index/HEAD copy is still committed",
		},
		{
			name: "script untracked in a repo with no commit", withCommit: false,
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, "data/card.txt", forgeSignature)
			},
			expectOffender: true, reason: "an addable script must fail before the first commit",
		},
		{
			name: "script under a non-txt name", withCommit: true,
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, "fixtures/card.script", forgeSignature)
				gitAdd(t, dir, "fixtures/card.script")
			},
			expectOffender: true, reason: "content, not extension, identifies a script",
		},
		{
			name: "script ignored by .gitignore", withCommit: true,
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, ".gitignore", ".cards/\n")
				writeFile(t, dir, ".cards/cardsfolder/t/test.txt", forgeSignature)
			},
			expectOffender: false, reason: "git will never commit an ignored file without -f",
		},
		{
			name: "path under tokenscripts", withCommit: true,
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, "vendor/res/tokenscripts/x.txt", "anything\n")
				gitAdd(t, dir, "vendor/res/tokenscripts/x.txt")
			},
			expectOffender: true, reason: "a Forge corpus directory is forbidden whatever it holds",
		},
		{
			name: "byte-identical to a corpus file", withCommit: true,
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, "notes/x.dat", corpusOnly)
				gitAdd(t, dir, "notes/x.dat")
			},
			expectOffender: true, reason: "a verbatim corpus copy is caught by identity",
		},
		{
			name: "innocuous text file committed", withCommit: true,
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, "readme.txt", innocentText)
				gitAdd(t, dir, "readme.txt")
				commit(t, dir, "-m", "test")
			},
			expectOffender: false, reason: "no Name:/Oracle: signature, no corpus path, no identity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newTempRepo(t, tt.withCommit)
			tt.setup(t, dir)
			offenders, err := checkForgeScriptsInRepo(dir, fakeCorpus)
			if err != nil {
				t.Fatalf("git should be available in temp repo: %v", err)
			}
			if tt.expectOffender && len(offenders) == 0 {
				t.Errorf("expected an offender, got none (reason: %s)", tt.reason)
			}
			if !tt.expectOffender && len(offenders) > 0 {
				t.Errorf("expected no offender, got %v (reason: %s)", offenders, tt.reason)
			}
		})
	}
}
