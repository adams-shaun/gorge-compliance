// Command padseats runs gorge's pad-to-four relation (rules.RunOracleScenarioPadded)
// over this repository's scenario corpora: each two-seat scenario is played as
// written and again with two inert seats added, and anything the two original
// seats see differently is reported. It needs gorge and the card corpus only.
//
//	go run ./cmd/padseats                       # every scenario, TSV on stdout
//	go run ./cmd/padseats -shard 0/64           # one shard
//	go run ./cmd/padseats -write                # rewrite scenarios/padseats.tsv
//
// The committed record, <root>/padseats.tsv, holds every scenario that differs
// or does not seat two, with the triage of why (class, cr, note). The TSV goes
// to -out; the summary line and any mismatch with the record go to stderr.
//
// Exit status: 0 when the record matches the run (always after a successful
// -write), 1 when a record row is missing, stale or changed, 2 on a usage or
// input error.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

const (
	recordName   = "padseats.tsv"
	recordHeader = "scenario\tverdict\tclass\tcr\tfirst_diff\tnote"
)

// The runner's verdicts (rules.OraclePadResult.Verdict).
const (
	verdictSame       = "same"
	verdictDiffers    = "differs"
	verdictBaseFails  = "base-fails"
	verdictNotTwoSeat = "not-two-seat"
)

// classGorgeDefect marks a difference no rule makes correct; cr names the rule
// gorge breaks.
const classGorgeDefect = "gorge-defect"

// classes is the closed set of triage classes: gorge's reasons a difference is
// correct (its testdata/oracle/seat-sensitive.tsv) and gorge-defect.
var classes = map[string]bool{
	"each-opponent": true, "each-player": true, "player-count": true, "each-turn": true,
	"turn-order": true, "opponent-choice": true, "first-draw": true, "game-end": true,
	"harness": true, classGorgeDefect: true,
}

// corpora are the scenario trees under -root. fixture is the runner's mode:
// false for the hand Oracle-text audit, true for a generated port.
var corpora = []struct {
	dir     string
	fixture bool
}{
	{"oracle", false},
	{"xmageport", true},
}

// ratchetDirs hold per-card ratchet rows beside the scenario files; their
// files are not scenarios.
var ratchetDirs = map[string]bool{"known-divergent": true, "known-unconsumed": true}

// portFields are the xmageport scenario fields that are not the runner's,
// whose schema rejects unknown fields.
var portFields = []string{"xmage", "known_bug"}

// row is one TSV line, of the committed record or of the full output.
type row struct {
	Scenario, Verdict, Class, CR, FirstDiff, Note string
}

// recorded says whether the row belongs in the committed record.
func (r row) recorded() bool { return r.Verdict == verdictDiffers || r.Verdict == verdictNotTwoSeat }

// scenario is one scenario object ready for the runner.
type scenario struct {
	Key     string // <path under root without .json>#<scenario name>
	Raw     []byte
	Fixture bool
}

// shard selects the scenarios with fnv32a(key) % N == I.
type shard struct{ I, N int }

func parseShard(s string) (shard, error) {
	a, b, ok := strings.Cut(s, "/")
	i, errI := strconv.Atoi(a)
	n, errN := strconv.Atoi(b)
	if !ok || errI != nil || errN != nil || n < 1 || n > 1<<30 || i < 0 || i >= n {
		return shard{}, fmt.Errorf("-shard %q: want i/n with 0 <= i < n", s)
	}
	return shard{i, n}, nil
}

func (s shard) has(key string) bool {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()%uint32(s.N) == uint32(s.I)
}

// loadScenarios reads every scenario of every corpus under root, sorted by key.
// A missing corpus, an empty file or a repeated key is an error: the run never
// passes over fewer scenarios than the tree holds.
func loadScenarios(root string) ([]scenario, error) {
	var out []scenario
	for _, c := range corpora {
		before := len(out)
		err := filepath.WalkDir(filepath.Join(root, c.dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if ratchetDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(p) != ".json" {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			scs, err := loadFile(p, strings.TrimSuffix(filepath.ToSlash(rel), ".json"), c.fixture)
			out = append(out, scs...)
			return err
		})
		if err != nil {
			return nil, err
		}
		if len(out) == before {
			return nil, fmt.Errorf("no scenarios under %s", filepath.Join(root, c.dir))
		}
	}
	slices.SortFunc(out, func(a, b scenario) int { return strings.Compare(a.Key, b.Key) })
	for i := 1; i < len(out); i++ {
		if out[i].Key == out[i-1].Key {
			return nil, fmt.Errorf("scenario key %q is used twice", out[i].Key)
		}
	}
	return out, nil
}

// loadFile reads one scenario file. The file level differs between the corpora;
// only "scenarios" is read, and each object stays raw for the runner.
func loadFile(path, stem string, fixture bool) ([]scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f struct {
		Scenarios []json.RawMessage `json:"scenarios"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(f.Scenarios) == 0 {
		return nil, fmt.Errorf("%s: no scenarios", path)
	}
	out := make([]scenario, 0, len(f.Scenarios))
	for _, raw := range f.Scenarios {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		var name string
		if err := json.Unmarshal(fields["name"], &name); err != nil || name == "" || strings.ContainsAny(name, "\t\r\n") {
			return nil, fmt.Errorf("%s: every scenario needs a one-line name", path)
		}
		body := []byte(raw)
		if fixture {
			for _, k := range portFields {
				delete(fields, k)
			}
			if body, err = json.Marshal(fields); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
		}
		out = append(out, scenario{Key: stem + "#" + name, Raw: body, Fixture: fixture})
	}
	return out, nil
}

// oneLine keeps a runner message inside its TSV cell.
var oneLine = strings.NewReplacer("\t", `\t`, "\r", `\r`, "\n", `\n`)

// play runs every scenario on GOMAXPROCS workers and returns one untriaged row
// per scenario, in the order given.
func play(reg *cards.Registry, scs []scenario) ([]row, error) {
	rows := make([]row, len(scs))
	errs := make([]error, len(scs))
	next := make(chan int)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				res, err := rules.RunOracleScenarioPadded(reg, scs[i].Raw, scs[i].Fixture)
				if err != nil {
					errs[i] = fmt.Errorf("%s: %w", scs[i].Key, err)
					continue
				}
				rows[i] = row{Scenario: scs[i].Key, Verdict: res.Verdict, FirstDiff: oneLine.Replace(res.FirstDiff)}
			}
		}()
	}
	for i := range scs {
		next <- i
	}
	close(next)
	wg.Wait()
	return rows, errors.Join(errs...)
}

// parseRecord reads the committed record strictly: the header, six columns,
// keys sorted and unique, the closed verdict and class sets, a first_diff
// exactly on a differs row, and a cr on every classed row.
func parseRecord(data []byte) ([]row, error) {
	text, ok := strings.CutSuffix(string(data), "\n")
	lines := strings.Split(text, "\n")
	if !ok || lines[0] != recordHeader {
		return nil, fmt.Errorf("want the header %q and newline-terminated rows", recordHeader)
	}
	var out []row
	var errs []error
	for i, line := range lines[1:] {
		bad := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("line %d: "+format, append([]any{i + 2}, args...)...))
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			bad("%d columns, want 6", len(f))
			continue
		}
		r := row{Scenario: f[0], Verdict: f[1], Class: f[2], CR: f[3], FirstDiff: f[4], Note: f[5]}
		if len(out) > 0 && r.Scenario <= out[len(out)-1].Scenario {
			bad("%q is not sorted after %q", r.Scenario, out[len(out)-1].Scenario)
		}
		if !r.recorded() {
			bad("verdict %q, want %s or %s", r.Verdict, verdictDiffers, verdictNotTwoSeat)
		}
		if (r.Verdict == verdictDiffers) != (r.FirstDiff != "") {
			bad("first_diff is set exactly on a %s row", verdictDiffers)
		}
		if r.Class != "" && !classes[r.Class] {
			bad("unknown class %q", r.Class)
		}
		if r.Class != "" && strings.TrimSpace(r.CR) == "" {
			bad("class %q needs a cr", r.Class)
		}
		out = append(out, r)
	}
	return out, errors.Join(errs...)
}

// readRecord is parseRecord of a file; a file that does not exist is an empty
// record.
func readRecord(path string) ([]row, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := parseRecord(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rows, nil
}

func formatTSV(rows []row) []byte {
	var b strings.Builder
	b.WriteString(recordHeader + "\n")
	for _, r := range rows {
		b.WriteString(strings.Join([]string{r.Scenario, r.Verdict, r.Class, r.CR, r.FirstDiff, r.Note}, "\t") + "\n")
	}
	return []byte(b.String())
}

func byKey(rows []row) map[string]row {
	m := make(map[string]row, len(rows))
	for _, r := range rows {
		m[r.Scenario] = r
	}
	return m
}

// triage copies the record's class, cr and note onto each measured row that
// still has the recorded verdict and first_diff. A changed row gets none: its
// triage explained a different difference.
func triage(measured, record []row) {
	rec := byKey(record)
	for i, m := range measured {
		if r, ok := rec[m.Scenario]; ok && r.Verdict == m.Verdict && r.FirstDiff == m.FirstDiff {
			measured[i].Class, measured[i].CR, measured[i].Note = r.Class, r.CR, r.Note
		}
	}
}

// compare lists how the record's rows of shard sh depart from the measured
// rows of that shard: missing, stale (also a row naming no scenario) or
// changed.
func compare(measured, record []row, sh shard) []string {
	rec := byKey(record)
	var out []string
	seen := map[string]bool{}
	for _, m := range measured {
		if !m.recorded() {
			continue
		}
		seen[m.Scenario] = true
		r, ok := rec[m.Scenario]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("missing row: %s\t%s\t%s", m.Scenario, m.Verdict, m.FirstDiff))
		case r.Verdict != m.Verdict || r.FirstDiff != m.FirstDiff:
			out = append(out, fmt.Sprintf("changed row: %s\trecorded %s %q, measured %s %q", m.Scenario, r.Verdict, r.FirstDiff, m.Verdict, m.FirstDiff))
		}
	}
	got := byKey(measured)
	for _, r := range record {
		if !sh.has(r.Scenario) || seen[r.Scenario] {
			continue
		}
		now := "no such scenario"
		if m, ok := got[r.Scenario]; ok {
			now = "now " + m.Verdict
		}
		out = append(out, fmt.Sprintf("stale row: %s\t%s", r.Scenario, now))
	}
	return out
}

// rewrite is the record with shard sh's rows replaced by the measured ones;
// every other shard's rows are kept. measured must already be triaged.
func rewrite(measured, record []row, sh shard) []row {
	var out []row
	for _, r := range record {
		if !sh.has(r.Scenario) {
			out = append(out, r)
		}
	}
	for _, m := range measured {
		if m.recorded() {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b row) int { return strings.Compare(a.Scenario, b.Scenario) })
	return out
}

// summary counts a triaged run: a differs row is explained once it has a class.
func summary(rows []row) string {
	n := map[string]int{}
	explained := 0
	for _, r := range rows {
		n[r.Verdict]++
		if r.Verdict == verdictDiffers && r.Class != "" {
			explained++
		}
	}
	return fmt.Sprintf("padseats: scenarios=%d same=%d explained=%d unexplained=%d base-fails=%d not-two-seat=%d",
		len(rows), n[verdictSame], explained, n[verdictDiffers]-explained, n[verdictBaseFails], n[verdictNotTwoSeat])
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("padseats", flag.ContinueOnError)
	flags.SetOutput(stderr)
	cardsDir := flags.String("cards", ".cards", "corpus directory (holds cardsfolder/)")
	root := flags.String("root", "scenarios", "scenario tree holding oracle/, xmageport/ and "+recordName)
	shardFlag := flags.String("shard", "0/1", "run shard i/n: the scenarios with fnv32a(key) % n == i")
	out := flags.String("out", "", "write the TSV here (default stdout)")
	write := flags.Bool("write", false, "rewrite the committed record's rows for the shards run")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "padseats:", err)
		return 2
	}
	if flags.NArg() > 0 {
		return fail(fmt.Errorf("unexpected arguments %q", flags.Args()))
	}
	sh, err := parseShard(*shardFlag)
	if err != nil {
		return fail(err)
	}
	all, err := loadScenarios(*root)
	if err != nil {
		return fail(err)
	}
	scs := slices.DeleteFunc(all, func(s scenario) bool { return !sh.has(s.Key) })
	recordPath := filepath.Join(*root, recordName)
	record, err := readRecord(recordPath)
	if err != nil {
		return fail(err)
	}
	reg, err := cards.SharedCorpus(*cardsDir)
	if err != nil {
		return fail(err)
	}
	rows, err := play(reg, scs)
	if err != nil {
		return fail(err)
	}
	triage(rows, record)

	tsv := formatTSV(rows)
	if *out == "" {
		_, err = stdout.Write(tsv)
	} else if err = os.MkdirAll(filepath.Dir(*out), 0o755); err == nil {
		err = os.WriteFile(*out, tsv, 0o644)
	}
	if err != nil {
		return fail(err)
	}
	status := 0
	if *write {
		if err := os.WriteFile(recordPath, formatTSV(rewrite(rows, record, sh)), 0o644); err != nil {
			return fail(err)
		}
	} else if diffs := compare(rows, record, sh); len(diffs) > 0 {
		for _, d := range diffs {
			fmt.Fprintln(stderr, d)
		}
		fmt.Fprintf(stderr, "%d row(s) of %s do not match this run; re-run with -write and triage them\n", len(diffs), recordPath)
		status = 1
	}
	fmt.Fprintln(stderr, summary(rows))
	return status
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
