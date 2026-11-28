package audit_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
)

// LEDGER.tsv holds one row per worked example of the Comprehensive Rules text
// the pinned scenarios restate (277 examples for the 2026-08-07 text). Each
// row's scenario column is "<file stem>#<scenario name>" under
// scenarios/oracle/cr-examples/ for a covered row; scenarios no row can name
// (extra demonstrations of an already-claimed example) are listed, with a
// note, in LEDGER-UNANCHORED.tsv next to it. The ledger holds rule ids and
// counts only: no sentence of the rules text is ever copied into this
// repository. scripts/cr_example_ledger.py diffs it against the rules text by
// hand when the pinned text changes.
//
// This test needs no corpus and runs no scenario: it reads the two TSV files
// and the scenario heads (loadOracleFiles/headOf) and checks the ledger in
// both directions.

const (
	crExamplesFamily = "cr-examples"
	crLedgerName     = "LEDGER.tsv"
	crUnclaimedName  = "LEDGER-UNANCHORED.tsv"
)

var (
	crLedgerStatuses  = map[string]bool{"covered": true, "open": true, "not-scenario": true, "out-of-scope": true}
	crLedgerRuleRe    = regexp.MustCompile(`^[0-9]+\.[0-9]+[a-z]*$`)
	crLedgerHeader    = []string{"rule", "n", "status", "scenario", "note"}
	crUnclaimedHeader = []string{"scenario", "note"}
)

// crLedgerRow is one data row of LEDGER.tsv.
type crLedgerRow struct {
	rule     string
	n        int
	status   string
	scenario string
	note     string
}

// crSplitRow splits one TSV line into exactly len(header) columns, reporting
// a problem when the column count does not match.
func crSplitRow(src string, line string, cols int) ([]string, []string) {
	got := strings.Split(line, "\t")
	if len(got) != cols {
		return nil, []string{src + ": expected " + strconv.Itoa(cols) + " tab-separated columns, got " + strconv.Itoa(len(got)) + ": " + line}
	}
	return got, nil
}

// crParseLedger parses the ledger's lines (header first). It reports every
// problem it can: header, column count, column syntax. Rows that fail to
// parse are dropped from the returned rows; the caller must treat any
// problem as fatal for the ledger.
func crParseLedger(lines []string) ([]crLedgerRow, []string) {
	var problems []string
	if len(lines) == 0 || !equalStrings(lines[0], crLedgerHeader) {
		return nil, []string{crLedgerName + ": header must be rule\\tn\\tstatus\\tscenario\\tnote"}
	}
	var rows []crLedgerRow
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		src := crLedgerName + ": " + line
		cols, bad := crSplitRow(src, line, len(crLedgerHeader))
		if bad != nil {
			problems = append(problems, bad...)
			continue
		}
		n, err := strconv.Atoi(cols[1])
		if err != nil || n < 1 {
			problems = append(problems, src+": n must be a positive integer")
			continue
		}
		if !crLedgerRuleRe.MatchString(cols[0]) {
			problems = append(problems, src+": rule must look like 603.7a")
			continue
		}
		rows = append(rows, crLedgerRow{rule: cols[0], n: n, status: cols[2], scenario: cols[3], note: cols[4]})
	}
	return rows, problems
}

// crParseUnclaimed parses the LEDGER-UNANCHORED.tsv lines (header first).
func crParseUnclaimed(lines []string) (map[string]string, []string) {
	var problems []string
	out := map[string]string{}
	if len(lines) == 0 {
		return out, nil // no registry lines at all: an empty registry
	}
	if !equalStrings(lines[0], crUnclaimedHeader) {
		return nil, []string{crUnclaimedName + ": header must be scenario\\tnote"}
	}
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		src := crUnclaimedName + ": " + line
		cols, bad := crSplitRow(src, line, len(crUnclaimedHeader))
		if bad != nil {
			problems = append(problems, bad...)
			continue
		}
		if cols[1] == "" {
			problems = append(problems, src+": every unclaimed scenario carries a note")
		}
		if _, dup := out[cols[0]]; dup {
			problems = append(problems, src+": unclaimed scenario listed twice")
		}
		out[cols[0]] = cols[1]
	}
	return out, problems
}

// crRuleSortKey orders rule ids the way the rules text does: major number,
// minor number, then subrule letters.
func crRuleSortKey(rule string) (int, int, string, bool) {
	m := regexp.MustCompile(`^([0-9]+)\.([0-9]+)([a-z]*)$`).FindStringSubmatch(rule)
	if m == nil {
		return 0, 0, "", false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major, minor, m[3], true
}

// crLedgerCheck is the pure check function behind TestCRExampleLedger and
// TestCRExampleLedgerProblems. rows are the ledger's parsed data rows;
// scenarioCR maps every scenario reference "<file stem>#<scenario name>"
// under scenarios/oracle/cr-examples/ to the rule ids the scenario's head
// cites; unclaimed is the LEDGER-UNANCHORED.tsv registry (reference to note)
// of scenarios no row can name. It returns one message per problem, checking
// in order: statuses in the closed set; (rule, n) uniqueness; a covered
// row's scenario exists and cites the row's rule; every scenario appears in
// at least one row or is registered unclaimed (and registered scenarios
// exist, are unnamed, and are stale-proof); no open row's rule is cited by a
// scenario no covered row of that rule names; not-scenario rows have notes.
func crLedgerCheck(rows []crLedgerRow, scenarioCR map[string][]string, unclaimed map[string]string) []string {
	var problems []string
	seen := map[[2]string]bool{}
	for _, r := range rows {
		src := crLedgerName + ": " + r.rule + " " + strconv.Itoa(r.n)
		if !crLedgerStatuses[r.status] {
			problems = append(problems, src+": status "+strconv.Quote(r.status)+" is not covered, open, not-scenario or out-of-scope")
			continue
		}
		key := [2]string{r.rule, strconv.Itoa(r.n)}
		if seen[key] {
			problems = append(problems, src+": duplicate row")
		}
		seen[key] = true
		if r.status == "covered" {
			if r.scenario == "" {
				problems = append(problems, src+": a covered row names a scenario")
			} else if crs, ok := scenarioCR[r.scenario]; !ok {
				problems = append(problems, src+": scenario "+r.scenario+" does not exist under "+crExamplesFamily+"/")
			} else if !contains(crs, r.rule) {
				problems = append(problems, src+": scenario "+r.scenario+" does not cite "+r.rule)
			}
		} else if r.scenario != "" {
			problems = append(problems, src+": only covered rows name a scenario")
		}
	}
	// Every scenario appears in at least one row, or is registered.
	named := map[string]bool{}
	namedByRule := map[string]map[string]bool{}
	for _, r := range rows {
		if r.status == "covered" && r.scenario != "" {
			named[r.scenario] = true
			if namedByRule[r.rule] == nil {
				namedByRule[r.rule] = map[string]bool{}
			}
			namedByRule[r.rule][r.scenario] = true
		}
	}
	for _, ref := range sortedKeys(scenarioCR) {
		if !named[ref] {
			if _, ok := unclaimed[ref]; !ok {
				problems = append(problems, "scenario "+ref+" appears in no ledger row and is not registered in "+crUnclaimedName)
			}
		}
	}
	for _, ref := range sortedKeys(unclaimed) {
		if _, ok := scenarioCR[ref]; !ok {
			problems = append(problems, crUnclaimedName+": "+ref+" is not a scenario under "+crExamplesFamily+"/")
		}
		if named[ref] {
			problems = append(problems, crUnclaimedName+": "+ref+" is named by a covered row and must be struck from the registry")
		}
	}
	// No open row whose rule is cited by a scenario no covered row names.
	for _, r := range rows {
		if r.status != "open" {
			continue
		}
		for _, ref := range sortedKeys(scenarioCR) {
			if contains(scenarioCR[ref], r.rule) && !namedByRule[r.rule][ref] {
				problems = append(problems, crLedgerName+": open row "+r.rule+" "+strconv.Itoa(r.n)+" is cited by scenario "+ref+", which no covered row of "+r.rule+" names")
			}
		}
	}
	for _, r := range rows {
		if r.status == "not-scenario" && r.note == "" {
			problems = append(problems, crLedgerName+": "+r.rule+" "+strconv.Itoa(r.n)+": a not-scenario row states why no game can show the example")
		}
	}
	return problems
}

func equalStrings(line string, header []string) bool {
	return equalStringSlices(strings.Split(line, "\t"), header)
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// crExampleScenarioHeads returns the scenario references under this
// repository's cr-examples family, mapped to the rules their heads cite.
func crExampleScenarioHeads(t *testing.T, files map[string]oracleFile) map[string][]string {
	t.Helper()
	own := filepath.ToSlash(gate.HandOracleRel) + "/" + crExamplesFamily + "/"
	out := map[string][]string{}
	for label, f := range files {
		if !strings.HasPrefix(label, own) || f.Family != crExamplesFamily {
			continue
		}
		stem := strings.TrimSuffix(filepath.Base(label), ".json")
		for _, raw := range f.Scenarios {
			h := headOf(t, label, raw)
			if h.Name == "" {
				t.Fatalf("%s: a scenario needs a name", label)
			}
			ref := stem + "#" + h.Name
			if _, dup := out[ref]; dup {
				t.Fatalf("%s: duplicate scenario reference %s", label, ref)
			}
			out[ref] = h.CR
		}
	}
	return out
}

func TestCRExampleLedger(t *testing.T) {
	dir := filepath.Join("..", "scenarios", "oracle", crExamplesFamily)
	raw, err := os.ReadFile(filepath.Join(dir, crLedgerName))
	if err != nil {
		t.Fatal(err)
	}
	un, err := os.ReadFile(filepath.Join(dir, crUnclaimedName))
	if err != nil {
		t.Fatal(err)
	}
	rows, problems := crParseLedger(strings.Split(string(raw), "\n"))
	unreg, unProblems := crParseUnclaimed(strings.Split(string(un), "\n"))
	problems = append(problems, unProblems...)
	files := loadOracleFiles(t)
	scenarioCR := crExampleScenarioHeads(t, files)
	problems = append(problems, crLedgerCheck(rows, scenarioCR, unreg)...)
	// The file is sorted by rule (rule order, then ordinal).
	for i := 1; i < len(rows); i++ {
		amaj, amin, alet, aok := crRuleSortKey(rows[i-1].rule)
		bmaj, bmin, blet, bok := crRuleSortKey(rows[i].rule)
		if !aok || !bok {
			continue // syntax problems already reported
		}
		outOfOrder := amaj > bmaj || (amaj == bmaj && (amin > bmin || (amin == bmin && alet > blet)))
		if outOfOrder || (amaj == bmaj && amin == bmin && alet == blet && rows[i-1].n > rows[i].n) {
			problems = append(problems, crLedgerName+": rows must be sorted by rule in rule order, then n: "+rows[i-1].rule+" "+strconv.Itoa(rows[i-1].n)+" before "+rows[i].rule+" "+strconv.Itoa(rows[i].n))
		}
	}
	for _, p := range problems {
		t.Error(p)
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.status]++
	}
	t.Logf("examples=%d covered=%d open=%d not-scenario=%d out-of-scope=%d", len(rows), counts["covered"], counts["open"], counts["not-scenario"], counts["out-of-scope"])
}

// crLedgerCase is one synthetic ledger (TSV lines, header first) with the
// scenario heads and the unclaimed registry it is checked against.
type crLedgerCase struct {
	name       string
	lines      []string
	scenarioCR map[string][]string
	unclaimed  []string // TSV lines, header first
	want       []string // substrings each expected problem must contain
}

func TestCRExampleLedgerProblems(t *testing.T) {
	base := []string{
		"rule\tn\tstatus\tscenario\tnote",
		"603.7a\t1\tcovered\tfoo#one\t",
		"603.7a\t2\topen\t\t",
	}
	baseScen := map[string][]string{"foo#one": {"603.7a"}}
	cases := []crLedgerCase{
		{name: "clean", lines: base, scenarioCR: baseScen},
		{name: "bad header", lines: []string{"n\trule\tstatus\tscenario\tnote", "603.7a\t1\topen\t\t"}, scenarioCR: map[string][]string{}, want: []string{"header must be"}},
		{name: "wrong columns", lines: []string{"rule\tn\tstatus\tscenario\tnote", "603.7a\t1\topen"}, scenarioCR: map[string][]string{}, want: []string{"expected 5 tab-separated columns"}},
		{name: "bad status", lines: append(append([]string{}, base...), "603.8\t1\tdone\t\t"), scenarioCR: baseScen, want: []string{`status "done" is not covered`}},
		{name: "duplicate row", lines: append(append([]string{}, base...), "603.7a\t1\topen\t\t"), scenarioCR: baseScen, want: []string{"duplicate row"}},
		{name: "bad rule id", lines: append(append([]string{}, base...), "6037a\t1\topen\t\t"), scenarioCR: baseScen, want: []string{"rule must look like 603.7a"}},
		{name: "bad n", lines: append(append([]string{}, base...), "603.9\t0\topen\t\t"), scenarioCR: baseScen, want: []string{"n must be a positive integer"}},
		{name: "covered scenario missing", lines: []string{"rule\tn\tstatus\tscenario\tnote", "603.7a\t1\tcovered\tfoo#ghost\t"}, scenarioCR: map[string][]string{}, want: []string{"scenario foo#ghost does not exist under"}},
		{name: "covered scenario does not cite rule", lines: []string{"rule\tn\tstatus\tscenario\tnote", "603.7a\t1\tcovered\tfoo#one\t"}, scenarioCR: map[string][]string{"foo#one": {"603.7b"}}, want: []string{"scenario foo#one does not cite 603.7a"}},
		{name: "non-covered names a scenario", lines: append(append([]string{}, base...), "700.1\t1\topen\tfoo#one\t"), scenarioCR: baseScen, want: []string{"only covered rows name a scenario"}},
		{name: "unregistered scenario", lines: base, scenarioCR: map[string][]string{"foo#one": {"603.7a"}, "bar#two": {"701.3"}}, want: []string{"scenario bar#two appears in no ledger row"}},
		{name: "registered scenario is exempt", lines: base, scenarioCR: map[string][]string{"foo#one": {"603.7a"}, "bar#two": {"701.3"}}, unclaimed: []string{"scenario\tnote", "bar#two\textra demonstration of another example."}},
		{name: "registry entry missing scenario", lines: base, scenarioCR: baseScen, unclaimed: []string{"scenario\tnote", "bar#two\tnote."}, want: []string{"bar#two is not a scenario under"}},
		{name: "registry entry already named", lines: base, scenarioCR: baseScen, unclaimed: []string{"scenario\tnote", "foo#one\tstale."}, want: []string{"foo#one is named by a covered row"}},
		{name: "registry entry no note", lines: base, scenarioCR: baseScen, unclaimed: []string{"scenario\tnote", "bar#two\t"}, want: []string{"carries a note"}},
		{name: "open row rule cited by unnamed scenario", lines: base, scenarioCR: map[string][]string{"foo#one": {"603.7a"}, "baz#three": {"603.7a"}}, want: []string{"open row 603.7a 2 is cited by scenario baz#three"}},
		{name: "not-scenario without note", lines: append(append([]string{}, base...), "604.1\t1\tnot-scenario\t\t"), scenarioCR: baseScen, want: []string{"states why no game can show"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, problems := crParseLedger(c.lines)
			unreg, unProblems := crParseUnclaimed(c.unclaimed)
			problems = append(problems, unProblems...)
			problems = append(problems, crLedgerCheck(rows, c.scenarioCR, unreg)...)
			if len(c.want) == 0 {
				for _, p := range problems {
					t.Errorf("unexpected problem: %s", p)
				}
				return
			}
			for _, want := range c.want {
				found := false
				for _, p := range problems {
					if strings.Contains(p, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no problem contains %q; problems: %v", want, problems)
				}
			}
		})
	}
}
