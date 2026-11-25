package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestChangedFilesKeepsBothSidesOfARename pins --no-renames. With rename
// detection on, a producer moved out of its prefix is reported only under its
// new path, so the ratchet packages the old path selected are silently dropped
// and an oraclegen move would run no verdict replay.
func TestChangedFilesKeepsBothSidesOfARename(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	src := filepath.Join(dir, "oracle", "compliance", "oraclegen", "gen.go")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("package oraclegen\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-qm", "base")
	base := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	dst := filepath.Join(dir, "internal", "foo", "gen.go")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "mv", "oracle/compliance/oraclegen/gen.go", "internal/foo/gen.go")
	git(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-qm", "rename")

	// Prove the fixture really is a rename: plain --name-only would hide the
	// producer side, which is the bug this test guards.
	if renamed := git(t, dir, "-c", "diff.renames=true", "diff", "--name-only", base, "HEAD"); strings.Contains(renamed, "oracle/compliance/oraclegen/gen.go") {
		t.Fatalf("fixture did not rename the producer file:\n%s", renamed)
	}

	got, err := changedFiles(dir, base, "HEAD", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"oracle/compliance/oraclegen/gen.go",
		"internal/foo/gen.go",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("changedFiles = %v, missing %s", got, want)
		}
	}
}
