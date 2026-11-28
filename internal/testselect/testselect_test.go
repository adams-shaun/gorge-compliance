package testselect

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fixture builds a temporary module tree for a selection case.
func fixture(t *testing.T, files map[string]string) fs.FS {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return os.DirFS(dir)
}

// ratchetFixture is the minimum a verdict-producer case needs: the two
// packages that emit the merge gate's ratchet verdict.
func ratchetFixture(t *testing.T, extra map[string]string) fs.FS {
	t.Helper()
	files := map[string]string{
		"oracle/compliance/adopt/ratchet_test.go":            "package adopt\n// GORGE-CERT-RESULT\n",
		"oracle/compliance/gate/declared_chunks_test.go":     "package gate\n// GORGE-CERT-RESULT\n",
		"oracle/compliance/oraclegen/answer_routing.go":      "package oraclegen\n",
		"oracle/compliance/oraclegen/answer_routing_test.go": "package oraclegen\n",
		"oracle/compliance/oraclegen/templates/template.go":  "package templates\n",
		"oracle/compliance/verdicts.go":                      "package compliance\n",
		"oracle/compliance/verdicts/b.jsonl":                 "{}\n",
		"oracle/compliance/ratchet.json":                     "{}\n",
	}
	for name, body := range extra {
		files[name] = body
	}
	return fixture(t, files)
}

func repoFS(t *testing.T) fs.FS {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return os.DirFS(root)
}

func TestOraclegenChangeSelectsRatchetPackages(t *testing.T) {
	got, err := Packages(ratchetFixture(t, nil), []string{
		"oracle/compliance/oraclegen/answer_routing.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"./oracle/compliance/adopt",
		"./oracle/compliance/gate",
		"./oracle/compliance/oraclegen",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("oraclegen change selected %v, want %v", got, want)
	}
}

func TestOraclegenChangeOnRealTreeSelectsRatchetPackages(t *testing.T) {
	got, err := Packages(repoFS(t), []string{
		"oracle/compliance/oraclegen/driver_plot_cast_test.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"./oracle/compliance/adopt", "./oracle/compliance/gate"} {
		if !slices.Contains(got, pkg) {
			t.Errorf("oraclegen change selected %v, missing %s", got, pkg)
		}
	}
}

func TestXMageDriverChangeSelectsRatchetPackages(t *testing.T) {
	got, err := Packages(ratchetFixture(t, map[string]string{
		"oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java": "// java\n",
	}), []string{
		"oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"./oracle/compliance/adopt", "./oracle/compliance/gate"}
	if !slices.Equal(got, want) {
		t.Fatalf("XMage driver change selected %v, want %v", got, want)
	}
}

// TestVerdictPipelineChangesSelectRatchetPackages covers the other files that
// can re-hash or rewrite a committed verdict. The regression was one of
// these producers, but the guard must not need another patch for the next.
func TestVerdictPipelineChangesSelectRatchetPackages(t *testing.T) {
	fsys := repoFS(t)
	paths := []string{
		"oracle/compliance/oraclediff/compare.go",
		"oracle/compliance/levelb/levelb.go",
		"oracle/compliance/verdicts/b.jsonl",
		"oracle/compliance/ratchet.json",
		"oracle/compliance/declared.json",
		"oracle/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java",
		"cmd/oraclediff/main.go",
		"cmd/compliance/main.go",
		"Makefile",
		"scripts/compliance-pass.sh",
		"scripts/xmage-oracle-run.sh",
		"scripts/xmage-oracle-setup.sh",
		"scripts/forge-pass.sh",
		"scripts/forge-oracle-run.sh",
		"scripts/forge-oracle-setup.sh",
		"oracle/validation/oracle/full-replay.sh",
		"oracle/validation/oracle/std-write.sh",
	}
	for _, p := range paths {
		t.Run(strings.ReplaceAll(p, "/", "_"), func(t *testing.T) {
			got, err := Packages(fsys, []string{p})
			if err != nil {
				t.Fatal(err)
			}
			for _, pkg := range []string{"./oracle/compliance/adopt", "./oracle/compliance/gate"} {
				if !slices.Contains(got, pkg) {
					t.Errorf("%s selected %v, missing %s", p, got, pkg)
				}
			}
		})
	}
}

// TestEveryComplianceChildSelectsTheRatchets is the sibling guard: every
// current entry under oracle/compliance is a verdict producer, and a future
// sibling is covered by the same prefix without editing this test.
func TestEveryComplianceChildSelectsTheRatchets(t *testing.T) {
	fsys := repoFS(t)
	ents, err := fs.ReadDir(fsys, complianceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) == 0 {
		t.Fatal("oracle/compliance has no entries")
	}
	for _, e := range ents {
		p := complianceRoot + "/" + e.Name()
		if e.IsDir() {
			p += "/probe.go"
		}
		got, err := Packages(fsys, []string{p})
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		for _, pkg := range []string{"./oracle/compliance/adopt", "./oracle/compliance/gate"} {
			if !slices.Contains(got, pkg) {
				t.Errorf("%s selected %v, missing %s", p, got, pkg)
			}
		}
	}
}

func TestNearestPackageAndNonGoDataMapping(t *testing.T) {
	fsys := ratchetFixture(t, nil)
	cases := []struct {
		path string
		want string
	}{
		{"oracle/compliance/oraclegen/templates/template.go", "./oracle/compliance/oraclegen/templates"},
		{"oracle/compliance/verdicts/b.jsonl", "./oracle/compliance"},
		{"oracle/compliance/ratchet.json", "./oracle/compliance"},
	}
	for _, c := range cases {
		got, err := Packages(fsys, []string{c.path})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(got, c.want) {
			t.Errorf("%s selected %v, missing nearest package %s", c.path, got, c.want)
		}
	}
}

func TestScenariosMapToAudit(t *testing.T) {
	fsys := fixture(t, map[string]string{
		"audit/audit.go":       "package audit\n",
		"cmd/padseats/main.go": "package main\n",
	})
	for _, f := range []string{
		"scenarios/oracle/bolt/lightning-bolt.json",
		"scenarios/xmageport/some/test.json",
		"scenarios/padseats.tsv",
	} {
		got, err := Packages(fsys, []string{f})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"./audit", "./cmd/padseats"}
		if !slices.Equal(got, want) {
			t.Errorf("%s selected %v, want %v", f, got, want)
		}
	}
}

func TestForgeDriverFixtureScriptsMapToOraclediff(t *testing.T) {
	fsys := fixture(t, map[string]string{"cmd/oraclediff/main.go": "package main\n"})
	got, err := Packages(fsys, []string{"scripts/forge-oracle-test-counter-any-done.sh"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"./cmd/oraclediff"}
	if !slices.Equal(got, want) {
		t.Fatalf("forge driver fixture change selected %v, want %v", got, want)
	}
}

func TestPinSelectsEveryPackage(t *testing.T) {
	fsys := ratchetFixture(t, nil)
	for _, p := range []string{"go.mod", "go.sum", "GORGE_PIN"} {
		got, err := Packages(fsys, []string{p})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{All}
		if !slices.Equal(got, want) {
			t.Errorf("%s selected %v, want %v", p, got, want)
		}
	}
}

func TestUnrelatedPathsSelectNothing(t *testing.T) {
	fsys := ratchetFixture(t, map[string]string{
		"oracle/xengine/sweep.go": "package xengine\n",
	})
	got, err := Packages(fsys, []string{
		"README.md",
		"docs/PINNING.md",
		"oracle/xengine/sweep.go", // a different oracle subsystem: no verdict edge
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"./oracle/xengine"}
	if !slices.Equal(got, want) {
		t.Fatalf("unrelated changes selected %v, want %v", got, want)
	}
}

func TestDeletedCompliancePathWalksUpToTheCompliancePackage(t *testing.T) {
	fsys := ratchetFixture(t, nil)
	got, err := Packages(fsys, []string{"oracle/compliance/removed/gone.go"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"./oracle/compliance",
		"./oracle/compliance/adopt",
		"./oracle/compliance/gate",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("deleted compliance path selected %v, want %v", got, want)
	}
}

func TestDuplicateFilesSelectOneSortedPackage(t *testing.T) {
	fsys := ratchetFixture(t, nil)
	got, err := Packages(fsys, []string{
		"oracle/compliance/oraclegen/answer_routing.go",
		"./oracle/compliance/oraclegen/answer_routing.go",
		"oracle/compliance/oraclegen/answer_routing_test.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"./oracle/compliance/adopt",
		"./oracle/compliance/gate",
		"./oracle/compliance/oraclegen",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("duplicate changes selected %v, want %v", got, want)
	}
}

func TestRatchetScannerRefusesAnEmptyContract(t *testing.T) {
	fsys := fixture(t, map[string]string{
		"oracle/compliance/oraclegen/answer_routing.go": "package oraclegen\n",
	})
	if _, err := Packages(fsys, []string{"oracle/compliance/oraclegen/answer_routing.go"}); err == nil {
		t.Fatal("a verdict producer with no GORGE-CERT-RESULT package selected silently")
	}
}

// verdictWriter matches a shell script that writes or records committed
// verdict rows: `oraclediff diff -write oracle/compliance/verdicts`, the
// forge pass's `--verdicts` mode, or the ratchet recording command.
var verdictWriter = regexp.MustCompile(`compliance/verdicts|-write-ratchet|--verdicts`)

// knownNonProducers are scripts that mention a verdict path but never write a
// row. Adding one here is a deliberate statement that its change cannot make a
// committed verdict stale.
var knownNonProducers = map[string]bool{}

// TestEveryVerdictWritingScriptIsAProducer keeps the producer list honest: it
// is a hand-kept contract, so a new shell script that writes verdict rows must
// either select the ratchet packages or be named as a known non-producer here.
func TestEveryVerdictWritingScriptIsAProducer(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fsys := repoFS(t)
	found := 0
	for _, top := range []string{"scripts", "oracle", "ops"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(p string, d os.DirEntry, werr error) error {
			if werr != nil || d.IsDir() || !strings.HasSuffix(p, ".sh") {
				return werr
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "ops/scripts/tests/") || strings.HasPrefix(rel, "oracle/xmageoracle/") {
				return nil // smokes and driver tests: covered by their own prefix or by stubs
			}
			body, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			if !verdictWriter.Match(body) || knownNonProducers[rel] {
				return nil
			}
			found++
			got, perr := Packages(fsys, []string{rel})
			if perr != nil {
				return perr
			}
			if !slices.Contains(got, "./oracle/compliance/adopt") || !slices.Contains(got, "./oracle/compliance/gate") {
				t.Errorf("%s writes verdict rows but selects %v: add it to verdictProducerFiles/Prefixes or to knownNonProducers", rel, got)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if found == 0 {
		t.Fatal("scanner found no verdict-writing script: the pattern no longer matches the tree")
	}
}
