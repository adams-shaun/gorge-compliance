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
// the pinned scenarios restate (277 examples for the 2026-08-07 text). A
// covered row's scenario column is one or more references
// "<file stem>#<scenario name>" under scenarios/oracle/cr-examples/, separated
// by crRefSep; several scenarios often demonstrate the same example. Every
// scenario there must be named by some covered row. The ledger holds rule ids
// and counts only: no sentence of the rules text is ever copied into this
// repository. scripts/cr_example_ledger.py diffs it against the rules text by
// hand when the pinned text changes.
//
// This test needs no corpus and runs no scenario: it reads the ledger and the
// scenario heads (loadOracleFiles/headOf) and checks the ledger in both
// directions.

const (
	crExamplesFamily = "cr-examples"
	crLedgerName     = "LEDGER.tsv"
	crRefSep         = ";"
)

var (
	crLedgerStatuses = map[string]bool{"covered": true, "open": true, "not-scenario": true, "out-of-scope": true}
	crLedgerRuleRe   = regexp.MustCompile(`^[0-9]+\.[0-9]+[a-z]*$`)
	crLedgerHeader   = []string{"rule", "n", "status", "scenario", "note"}
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

// crRuleSortKey orders rule ids the way the rules text does: major number,
// minor number, then subrule letters shortest first, so 704.5z precedes
// 704.5aa.
func crRuleSortKey(rule string) (major, minor, letters int, sub string, ok bool) {
	m := regexp.MustCompile(`^([0-9]+)\.([0-9]+)([a-z]*)$`).FindStringSubmatch(rule)
	if m == nil {
		return 0, 0, 0, "", false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, len(m[3]), m[3], true
}

// crRuleLess reports whether a comes before b in rule order.
func crRuleLess(a, b string) bool {
	amaj, amin, alen, alet, aok := crRuleSortKey(a)
	bmaj, bmin, blen, blet, bok := crRuleSortKey(b)
	if !aok || !bok {
		return false // syntax problems are reported by the parser
	}
	if amaj != bmaj {
		return amaj < bmaj
	}
	if amin != bmin {
		return amin < bmin
	}
	if alen != blen {
		return alen < blen
	}
	return alet < blet
}

// crScenarioRefs splits a covered row's scenario cell into its references.
func crScenarioRefs(cell string) []string {
	if cell == "" {
		return nil
	}
	return strings.Split(cell, crRefSep)
}

// crLedgerCheck is the pure check function behind TestCRExampleLedger and
// TestCRExampleLedgerProblems. rows are the ledger's parsed data rows;
// scenarioCR maps every scenario reference "<file stem>#<scenario name>"
// under scenarios/oracle/cr-examples/ to the rule ids the scenario's head
// cites. It returns one message per problem, checking in order: statuses in
// the closed set; (rule, n) uniqueness; a covered row's scenarios exist and
// cite the row's rule; every scenario appears in at least one covered row;
// no open row's rule is cited by a scenario no covered row of that rule
// names; not-scenario rows have notes; rows are sorted in rule order, then
// ordinal.
func crLedgerCheck(rows []crLedgerRow, scenarioCR map[string][]string) []string {
	var problems []string
	seen := map[[2]string]bool{}
	named := map[string]bool{}
	namedByRule := map[string]map[string]bool{}
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
			refs := crScenarioRefs(r.scenario)
			if len(refs) == 0 {
				problems = append(problems, src+": a covered row names a scenario")
			}
			for _, ref := range refs {
				if ref == "" {
					problems = append(problems, src+": empty scenario reference in "+strconv.Quote(r.scenario))
					continue
				}
				if crs, ok := scenarioCR[ref]; !ok {
					problems = append(problems, src+": scenario "+ref+" does not exist under "+crExamplesFamily+"/")
					continue
				} else if !contains(crs, r.rule) {
					problems = append(problems, src+": scenario "+ref+" does not cite "+r.rule)
				}
				named[ref] = true
				if namedByRule[r.rule] == nil {
					namedByRule[r.rule] = map[string]bool{}
				}
				namedByRule[r.rule][ref] = true
			}
		} else if r.scenario != "" {
			problems = append(problems, src+": only covered rows name a scenario")
		}
	}
	// Every scenario appears in at least one covered row.
	for _, ref := range sortedKeys(scenarioCR) {
		if !named[ref] {
			problems = append(problems, "scenario "+ref+" appears in no ledger row")
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
	for i := 1; i < len(rows); i++ {
		a, b := rows[i-1], rows[i]
		if crRuleLess(b.rule, a.rule) || (a.rule == b.rule && a.n > b.n) {
			problems = append(problems, crLedgerName+": rows must be sorted by rule in rule order, then n: "+a.rule+" "+strconv.Itoa(a.n)+" before "+b.rule+" "+strconv.Itoa(b.n))
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
	rows, problems := crParseLedger(strings.Split(string(raw), "\n"))
	files := loadOracleFiles(t)
	scenarioCR := crExampleScenarioHeads(t, files)
	problems = append(problems, crLedgerCheck(rows, scenarioCR)...)
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
// scenario heads it is checked against.
type crLedgerCase struct {
	name       string
	lines      []string
	scenarioCR map[string][]string
	want       []string // substrings each expected problem must contain
}

func TestCRExampleLedgerProblems(t *testing.T) {
	base := []string{
		"rule\tn\tstatus\tscenario\tnote",
		"603.7a\t1\tcovered\tfoo#one\t",
		"603.7a\t2\topen\t\t",
	}
	baseScen := map[string][]string{"foo#one": {"603.7a"}}
	header := "rule\tn\tstatus\tscenario\tnote"
	cases := []crLedgerCase{
		{name: "clean", lines: base, scenarioCR: baseScen},
		{name: "clean with several scenarios on one row", lines: []string{header, "603.7a\t1\tcovered\tfoo#one;bar#two\t", "603.7a\t2\topen\t\t"}, scenarioCR: map[string][]string{"foo#one": {"603.7a"}, "bar#two": {"603.7a"}}},
		{name: "bad header", lines: []string{"n\trule\tstatus\tscenario\tnote", "603.7a\t1\topen\t\t"}, scenarioCR: map[string][]string{}, want: []string{"header must be"}},
		{name: "wrong columns", lines: []string{header, "603.7a\t1\topen"}, scenarioCR: map[string][]string{}, want: []string{"expected 5 tab-separated columns"}},
		{name: "bad status", lines: append(append([]string{}, base...), "603.8\t1\tdone\t\t"), scenarioCR: baseScen, want: []string{`status "done" is not covered`}},
		{name: "duplicate row", lines: append(append([]string{}, base...), "603.7a\t1\topen\t\t"), scenarioCR: baseScen, want: []string{"duplicate row"}},
		{name: "bad rule id", lines: append(append([]string{}, base...), "6037a\t1\topen\t\t"), scenarioCR: baseScen, want: []string{"rule must look like 603.7a"}},
		{name: "bad n", lines: append(append([]string{}, base...), "603.9\t0\topen\t\t"), scenarioCR: baseScen, want: []string{"n must be a positive integer"}},
		{name: "covered scenario missing", lines: []string{header, "603.7a\t1\tcovered\tfoo#ghost\t"}, scenarioCR: map[string][]string{}, want: []string{"scenario foo#ghost does not exist under"}},
		{name: "covered scenario does not cite rule", lines: []string{header, "603.7a\t1\tcovered\tfoo#one\t"}, scenarioCR: map[string][]string{"foo#one": {"603.7b"}}, want: []string{"scenario foo#one does not cite 603.7a"}},
		{name: "one of several covered scenarios missing", lines: []string{header, "603.7a\t1\tcovered\tfoo#one;bar#ghost\t"}, scenarioCR: map[string][]string{"foo#one": {"603.7a"}}, want: []string{"scenario bar#ghost does not exist under"}},
		{name: "one of several covered scenarios does not cite", lines: []string{header, "603.7a\t1\tcovered\tfoo#one;bar#two\t"}, scenarioCR: map[string][]string{"foo#one": {"603.7a"}, "bar#two": {"603.7b"}}, want: []string{"scenario bar#two does not cite 603.7a"}},
		{name: "covered row names no scenario", lines: []string{header, "603.7a\t1\tcovered\t\t"}, scenarioCR: map[string][]string{}, want: []string{"a covered row names a scenario"}},
		{name: "empty scenario reference", lines: []string{header, "603.7a\t1\tcovered\tfoo#one;\t"}, scenarioCR: map[string][]string{"foo#one": {"603.7a"}}, want: []string{"empty scenario reference"}},
		{name: "non-covered names a scenario", lines: append(append([]string{}, base...), "700.1\t1\topen\tfoo#one\t"), scenarioCR: baseScen, want: []string{"only covered rows name a scenario"}},
		{name: "unnamed scenario", lines: base, scenarioCR: map[string][]string{"foo#one": {"603.7a"}, "bar#two": {"701.3"}}, want: []string{"scenario bar#two appears in no ledger row"}},
		{name: "open row rule cited by unnamed scenario", lines: base, scenarioCR: map[string][]string{"foo#one": {"603.7a"}, "baz#three": {"603.7a"}}, want: []string{"open row 603.7a 2 is cited by scenario baz#three"}},
		{name: "not-scenario without note", lines: append(append([]string{}, base...), "604.1\t1\tnot-scenario\t\t"), scenarioCR: baseScen, want: []string{"states why no game can show"}},
		{name: "clean multi-letter subrule order", lines: []string{header, "704.5z\t1\topen\t\t", "704.5aa\t1\topen\t\t"}, scenarioCR: map[string][]string{}},
		{name: "multi-letter subrule out of order", lines: []string{header, "704.5aa\t1\topen\t\t", "704.5z\t1\topen\t\t"}, scenarioCR: map[string][]string{}, want: []string{"rows must be sorted"}},
		{name: "numeric rule order not lexical", lines: []string{header, "603.7a\t1\topen\t\t", "603.10a\t1\topen\t\t"}, scenarioCR: map[string][]string{}},
		{name: "ordinal out of order", lines: []string{header, "603.7a\t2\topen\t\t", "603.7a\t1\topen\t\t"}, scenarioCR: map[string][]string{}, want: []string{"rows must be sorted"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, problems := crParseLedger(c.lines)
			problems = append(problems, crLedgerCheck(rows, c.scenarioCR)...)
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
