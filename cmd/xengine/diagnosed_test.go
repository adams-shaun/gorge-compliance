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
	rows := diagnosedRows(20)
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
	if got := diagnosedShardRows(t, shards, target); strings.Join(got, "\n") != strings.Join(rows, "\n") {
		t.Fatalf("split changed the register rows:\ngot  %q\nwant %q", got, rows)
	}
	var out strings.Builder
	if bad, err := checkDiagnosed(dir, diagnosedCorpus(t), &out); err != nil || bad != 0 {
		t.Fatalf("split register invalid: %v, %d bad row(s)\n%s", err, bad, out.String())
	}
}

// A split of the committed file-plus-archive layout keeps
// oracle/xengine/diagnosed.tsv as the active (farm append) shard and packs the
// archives into its sibling directory. -check reads both. A canonical stale
// archive is removed; a stray *.tsv is rejected and left alone.
func TestDiagnosedCheckSplitFile(t *testing.T) {
	root := t.TempDir()
	active := filepath.Join(root, "diagnosed.tsv")
	archive := filepath.Join(root, "diagnosed")
	rows := diagnosedRows(20)
	writeShard(t, root, "diagnosed.tsv", diagnosedHeader+"\n"+strings.Join(rows, "\n")+"\n")
	const target = 400
	if err := splitDiagnosed(active, target); err != nil {
		t.Fatalf("split file register: %v", err)
	}
	shards, err := diagnosedShards(active)
	if err != nil {
		t.Fatalf("split file shards: %v", err)
	}
	if shards[len(shards)-1] != active {
		t.Fatalf("last shard is %q, want the active file %q (all: %v)", shards[len(shards)-1], active, shards)
	}
	if len(shards) < 2 {
		t.Fatalf("split kept %d shard(s); want archive shards beside the active file", len(shards))
	}
	for _, shard := range shards[:len(shards)-1] {
		if filepath.Dir(shard) != archive {
			t.Errorf("archive shard %q is not under %s", shard, archive)
		}
	}
	if got := diagnosedShardRows(t, shards, target); strings.Join(got, "\n") != strings.Join(rows, "\n") {
		t.Fatalf("split changed the register rows:\ngot  %q\nwant %q", got, rows)
	}
	var out strings.Builder
	if bad, err := checkDiagnosed(active, diagnosedCorpus(t), &out); err != nil || bad != 0 {
		t.Fatalf("split file register invalid: %v, %d bad row(s)\n%s", err, bad, out.String())
	}

	// A canonical stale archive is removed by the next split.
	writeShard(t, archive, "shard-999.tsv", diagnosedHeader+"\n")
	if err := splitDiagnosed(active, target); err != nil {
		t.Fatalf("resplit file register: %v", err)
	}
	if _, err := os.Stat(filepath.Join(archive, "shard-999.tsv")); !os.IsNotExist(err) {
		t.Fatalf("stale shard-999.tsv survived the split: %v", err)
	}

	// An appended row survives as the active file's last row.
	appended := "GF|X\tFDN\tAlpha\tcast-appended\tX\t601.2\tevidence\thttps://example.com/appended\t"
	f, err := os.OpenFile(active, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open active shard: %v", err)
	}
	if _, err := f.WriteString(appended + "\n"); err != nil {
		f.Close()
		t.Fatalf("append active shard: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close active shard: %v", err)
	}
	if err := splitDiagnosed(active, target); err != nil {
		t.Fatalf("split after append: %v", err)
	}
	shards, err = diagnosedShards(active)
	if err != nil {
		t.Fatalf("post-append shards: %v", err)
	}
	rows = append(rows, appended)
	if got := diagnosedShardRows(t, shards, target); strings.Join(got, "\n") != strings.Join(rows, "\n") {
		t.Fatalf("split after append changed the register rows:\ngot  %q\nwant %q", got, rows)
	}
	if shards[len(shards)-1] != active {
		t.Fatalf("last shard after append is %q, want %q", shards[len(shards)-1], active)
	}
	if bad, err := checkDiagnosed(active, diagnosedCorpus(t), &out); err != nil || bad != 0 {
		t.Fatalf("post-append register invalid: %v, %d bad row(s)\n%s", err, bad, out.String())
	}
}

// A stray *.tsv beside the register is a configuration error, not a shard:
// -check and -split must not absorb it and -split must not delete it.
func TestDiagnosedCheckStrayShard(t *testing.T) {
	const stray = "GF|X\tFDN\tAlpha\tcast\tX\t601.2\tevidence\thttps://example.com/1\t"
	dir := t.TempDir()
	writeShard(t, dir, "shard-000.tsv", diagnosedHeader+"\n")
	writeShard(t, dir, "notes.tsv", diagnosedHeader+"\n"+stray+"\n")
	var out strings.Builder
	if _, err := checkDiagnosed(dir, diagnosedCorpus(t), &out); err == nil || !strings.Contains(err.Error(), "notes.tsv") {
		t.Fatalf("check accepted a stray notes.tsv: %v\n%s", err, out.String())
	}
	if err := splitDiagnosed(dir, 4096); err == nil {
		t.Fatalf("split accepted a stray notes.tsv")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.tsv")); err != nil {
		t.Fatalf("split removed the stray notes.tsv: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "shard-000.tsv")); err != nil || string(b) != diagnosedHeader+"\n" {
		t.Fatalf("split rewrote a shard before failing: %q, %v", b, err)
	}

	root := t.TempDir()
	active := filepath.Join(root, "diagnosed.tsv")
	writeShard(t, root, "diagnosed.tsv", diagnosedHeader+"\n")
	archive := filepath.Join(root, "diagnosed")
	writeShard(t, archive, "notes.tsv", diagnosedHeader+"\n"+stray+"\n")
	if _, err := diagnosedShards(active); err == nil || !strings.Contains(err.Error(), "notes.tsv") {
		t.Fatalf("file register accepted a stray notes.tsv: %v", err)
	}
	if err := splitDiagnosed(active, 4096); err == nil {
		t.Fatalf("file split accepted a stray notes.tsv")
	}
	if _, err := os.Stat(filepath.Join(archive, "notes.tsv")); err != nil {
		t.Fatalf("file split removed the stray notes.tsv: %v", err)
	}
}

// Shards sort by their numeric index (so shard-1000 follows shard-999), with
// active.tsv last because it is the append target.
func TestDiagnosedCheckShardOrder(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"shard-1000.tsv", "shard-999.tsv", diagnosedActiveName} {
		writeShard(t, dir, name, "")
	}
	shards, err := diagnosedShards(dir)
	if err != nil {
		t.Fatalf("shard order: %v", err)
	}
	want := []string{"shard-999.tsv", "shard-1000.tsv", diagnosedActiveName}
	if len(shards) != len(want) {
		t.Fatalf("got %v, want %v", shards, want)
	}
	for i := range want {
		if filepath.Base(shards[i]) != want[i] {
			t.Fatalf("shard %d is %q, want %q (all: %v)", i, filepath.Base(shards[i]), want[i], shards)
		}
	}
}

// The committed file-plus-archive layout reads the archive shards in numeric
// order, then the active file that report.py appends to.
func TestDiagnosedCheckFileArchiveOrder(t *testing.T) {
	root := t.TempDir()
	active := filepath.Join(root, "diagnosed.tsv")
	writeShard(t, root, "diagnosed.tsv", "")
	archive := filepath.Join(root, "diagnosed")
	writeShard(t, archive, "shard-001.tsv", "")
	writeShard(t, archive, "shard-000.tsv", "")
	shards, err := diagnosedShards(active)
	if err != nil {
		t.Fatalf("file register order: %v", err)
	}
	want := []string{
		filepath.Join(archive, "shard-000.tsv"),
		filepath.Join(archive, "shard-001.tsv"),
		active,
	}
	if len(shards) != len(want) {
		t.Fatalf("got %v, want %v", shards, want)
	}
	for i := range want {
		if shards[i] != want[i] {
			t.Fatalf("shard %d is %q, want %q (all: %v)", i, shards[i], want[i], shards)
		}
	}
}

// diagnosedRows builds n valid register rows in order.
func diagnosedRows(n int) []string {
	rows := make([]string, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, fmt.Sprintf(
			"GF|X\tFDN\tAlpha\tcast%d\tX\t601.2\tevidence-%s\thttps://example.com/%d\t",
			i, strings.Repeat("e", 40), i))
	}
	return rows
}

// diagnosedShardRows reads every shard, checks it is at most target bytes, and
// returns the data rows in register order.
func diagnosedShardRows(t *testing.T, shards []string, target int) []string {
	t.Helper()
	var rows []string
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
		rows = append(rows, lines[1:]...)
	}
	return rows
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
