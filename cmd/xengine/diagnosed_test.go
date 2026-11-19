package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// diagnosedCorpus copies the fixture corpus into a t.TempDir() and returns the
// temp dir. Most fixtures land under cardsfolder/<rel>; tok/ holds token-script
// fixtures and lands under tokenscripts/<rest>. The fixture sources stay outside
// any cardsfolder/ or tokenscripts/ directory, so the licence guard never
// mistakes them for a Forge corpus.
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
		if rest, ok := strings.CutPrefix(rel, "tok"+string(os.PathSeparator)); ok {
			dst = filepath.Join(dir, "tokenscripts", rest)
		}
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

// writeShard writes one TSV shard of a register directory.
func writeShard(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make shard dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write shard %s: %v", name, err)
	}
}

func TestDiagnosedCheckShards(t *testing.T) {
	dir := t.TempDir()
	writeShard(t, dir, "shard-000.tsv", diagnosedHeader+"\n"+
		"FX|G\tFDN\tAlpha\tcast\tX\t601.2a\tevidence\thttps://example.com/1\t\n")
	writeShard(t, dir, diagnosedActiveName, diagnosedHeader+"\n"+
		"GF|X\tBLB\tAlpha\tstatic:haste\tF\t604.2\tevidence\t\tcompliance-diagnosed-register\n")
	var out strings.Builder
	bad, err := checkDiagnosed(dir, diagnosedCorpus(t), &out)
	if err != nil {
		t.Fatalf("shard directory rejected: %v\n%s", err, out.String())
	}
	if bad != 0 {
		t.Fatalf("%d bad row(s):\n%s", bad, out.String())
	}
}

func TestDiagnosedCheckCrossShardDuplicate(t *testing.T) {
	dir := t.TempDir()
	row := "GF|X\tFDN\tAlpha\tcast\tX\t601.2\tevidence\thttps://example.com/1\t\n"
	writeShard(t, dir, "shard-000.tsv", diagnosedHeader+"\n"+row)
	writeShard(t, dir, diagnosedActiveName, diagnosedHeader+"\n"+row)
	var out strings.Builder
	bad, err := checkDiagnosed(dir, diagnosedCorpus(t), &out)
	if err != nil {
		t.Fatalf("duplicate shard directory: %v", err)
	}
	if bad != 1 {
		t.Fatalf("%d bad row(s), want the duplicate only:\n%s", bad, out.String())
	}
	want := filepath.Join(dir, diagnosedActiveName) + ":2: duplicate (kind, set, card, field)"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output missing %q:\n%s", want, out.String())
	}
}

func TestDiagnosedCheckCap(t *testing.T) {
	dir := t.TempDir()
	row := "GF|X\tFDN\tAlpha\tcast\tX\t601.2\t" + strings.Repeat("x", diagnosedMaxBytes) +
		"\thttps://example.com/1\t\n"
	writeShard(t, dir, diagnosedActiveName, diagnosedHeader+"\n"+row)
	var out strings.Builder
	bad, err := checkDiagnosed(dir, diagnosedCorpus(t), &out)
	if err != nil {
		t.Fatalf("over-cap register: %v", err)
	}
	if bad != 1 {
		t.Fatalf("%d bad row(s), want the shard cap only:\n%s", bad, out.String())
	}
	want := fmt.Sprintf("over the %d-byte cap", diagnosedMaxBytes)
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output missing %q:\n%s", want, out.String())
	}
}

// A split repacks every shard under the target, ends with the active append
// target, and preserves the row order across shards.
func TestDiagnosedCheckSplit(t *testing.T) {
	dir := t.TempDir()
	var rows []string
	for i := 0; i < 20; i++ {
		rows = append(rows, fmt.Sprintf(
			"GF|X\tFDN\tAlpha\tcast%d\tX\t601.2\tevidence-%s\thttps://example.com/%d\t",
			i, strings.Repeat("e", 40), i))
	}
	writeShard(t, dir, "shard-000.tsv", diagnosedHeader+"\n"+strings.Join(rows[:10], "\n")+"\n")
	writeShard(t, dir, diagnosedActiveName, diagnosedHeader+"\n"+strings.Join(rows[10:], "\n")+"\n")
	const target = 400
	if err := splitDiagnosed(dir, target); err != nil {
		t.Fatalf("split: %v", err)
	}
	shards, err := diagnosedShards(dir)
	if err != nil {
		t.Fatalf("split shards: %v", err)
	}
	if got := filepath.Base(shards[len(shards)-1]); got != diagnosedActiveName {
		t.Fatalf("last shard is %q, want %q", got, diagnosedActiveName)
	}
	var got []string
	for _, shard := range shards {
		b, err := os.ReadFile(shard)
		if err != nil {
			t.Fatalf("read split shard: %v", err)
		}
		if len(b) > target {
			t.Errorf("%s is %d bytes, over the %d-byte target", shard, len(b), target)
		}
		lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		if len(lines) == 0 || lines[0] != diagnosedHeader {
			t.Errorf("%s: header is %q", shard, lines[0])
			continue
		}
		got = append(got, lines[1:]...)
	}
	if strings.Join(got, "\n") != strings.Join(rows, "\n") {
		t.Fatalf("split changed the register rows:\ngot  %q\nwant %q", got, rows)
	}
	var out strings.Builder
	if bad, err := checkDiagnosed(dir, diagnosedCorpus(t), &out); err != nil || bad != 0 {
		t.Fatalf("split register invalid: %v, %d bad row(s)\n%s", err, bad, out.String())
	}
}

// Shards sort by their numeric index (so shard-1000 follows shard-999), with
// active.tsv last because it is the append target.
func TestDiagnosedCheckShardOrder(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"shard-1000.tsv", "shard-999.tsv", "zzz.tsv", diagnosedActiveName} {
		writeShard(t, dir, name, "")
	}
	shards, err := diagnosedShards(dir)
	if err != nil {
		t.Fatalf("shard order: %v", err)
	}
	want := []string{"shard-999.tsv", "shard-1000.tsv", "zzz.tsv", diagnosedActiveName}
	if len(shards) != len(want) {
		t.Fatalf("got %v, want %v", shards, want)
	}
	for i := range want {
		if filepath.Base(shards[i]) != want[i] {
			t.Fatalf("shard %d is %q, want %q (all: %v)", i, filepath.Base(shards[i]), want[i], shards)
		}
	}
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
// whose inherent triggered abilities have no source (CR 725.2, The Monarch),
// so the card column carries a name no corpus card has. The register accepts
// the same names the stack comparer normalises.
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
		{"bad_token.tsv", 2, `unknown card "token:Missing"`, ""},
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

// tempRegister writes a one-row register whose card column is card.
func tempRegister(t *testing.T, card string) string {
	t.Helper()
	row := strings.Join([]string{
		"diverge", "FDN", card, "cast", "harness", "601.2", "evidence", "", "compliance-token",
	}, "\t")
	file := filepath.Join(t.TempDir(), "register.tsv")
	if err := os.WriteFile(file, []byte(diagnosedHeader+"\n"+row+"\n"), 0o644); err != nil {
		t.Fatalf("write register: %v", err)
	}
	return file
}

// tempCardsCorpus builds a temp corpus with only cardsfolder/a/alpha.txt and no
// tokenscripts directory.
func tempCardsCorpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	folder := filepath.Join(dir, "cardsfolder", "a")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("build corpus: %v", err)
	}
	if err := os.WriteFile(filepath.Join(folder, "alpha.txt"), []byte("Name:Alpha\n"), 0o644); err != nil {
		t.Fatalf("build corpus: %v", err)
	}
	return dir
}

// A corpus with no tokenscripts directory (the pre-token fixture shape) must
// still validate card rows: the token folder is optional.
func TestDiagnosedMissingTokenScripts(t *testing.T) {
	corpus := tempCardsCorpus(t)
	file := tempRegister(t, "Alpha")
	var out strings.Builder
	bad, err := checkDiagnosed(file, corpus, &out)
	if err != nil {
		t.Fatalf("missing tokenscripts/ is not a corpus error: %v", err)
	}
	if bad != 0 {
		t.Fatalf("%d bad row(s):\n%s", bad, out.String())
	}
}

// A missing cardsfolder is one corpus error, not an "unknown card" per row:
// a wrong -cards path must fail at the root.
func TestDiagnosedMissingCardsFolder(t *testing.T) {
	corpus := t.TempDir() // deliberately no cardsfolder/
	file := tempRegister(t, "Alpha")
	var out strings.Builder
	if _, err := checkDiagnosed(file, corpus, &out); err == nil {
		t.Fatalf("missing cardsfolder accepted; output:\n%s", out.String())
	} else if !strings.Contains(err.Error(), "cardsfolder") {
		t.Fatalf("corpus error %q does not name cardsfolder", err)
	}
}
