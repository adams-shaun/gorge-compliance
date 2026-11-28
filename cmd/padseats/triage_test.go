package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPadGroupKey(t *testing.T) {
	for in, want := range map[string]string{
		"snap 3 p1.life: 17 != 20":                         "snap N pN.life",
		"snapshots: 5 != 6":                                "snapshots",
		"expect: p0 life 20, want 22":                      "expect",
		"snap 2 inert: p2 controls Soldier":                "snap N inert",
		"snap 12 perm p0:Progenitor Mimic#2.ref: a != b":   "snap N perm pN:Progenitor Mimic#N.ref",
		"snap 10 p0.library_top: [Wastes] != [Wastes]":     "snap N pN.library_top",
		"snap 4 p23.hand without a value":                  "snap N pN.hand without a value",
		"expectation in snap 3: only an expect: prefix is": "expectation in snap N",
	} {
		if got := padGroupKey(in); got != want {
			t.Errorf("padGroupKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPadSeatsUnclassedShare counts every scenario the tool loads and the
// record's differs rows with no class. The 2% bound is report-only until the
// operator's follow-up flips it (decision 12's new-gate standard): a breach is
// logged, never failed.
func TestPadSeatsUnclassedShare(t *testing.T) {
	scs, err := loadScenarios(scenarioRoot)
	if err != nil {
		t.Fatal(err)
	}
	perClass := map[string]int{}
	differs, unclassed := 0, 0
	for _, r := range committedRecord(t) {
		if r.Verdict != verdictDiffers {
			continue
		}
		differs++
		if r.Class == "" {
			unclassed++
			t.Logf("unclassed: %s\t%s", r.Scenario, r.FirstDiff)
			continue
		}
		perClass[r.Class]++
	}
	t.Logf("scenarios=%d differs=%d unclassed=%d", len(scs), differs, unclassed)
	names := make([]string, 0, len(perClass))
	for c := range perClass {
		names = append(names, c)
	}
	slices.Sort(names)
	for _, c := range names {
		t.Logf("class %s=%d", c, perClass[c])
	}
	if unclassedOverBound(unclassed, len(scs)) {
		t.Logf("REPORT-ONLY: unclassed=%d is over 1 in %d of scenarios=%d (the 2%% bound); not failed until the operator flips the gate",
			unclassed, padUnclassedPer, len(scs))
	}

	// The bound itself: at most one unclassed row in 50 scenarios.
	for _, c := range []struct {
		unclassed, scenarios int
		over                 bool
	}{{0, 0, false}, {2, 100, false}, {3, 100, true}, {119, 5968, false}, {120, 5968, true}} {
		if got := unclassedOverBound(c.unclassed, c.scenarios); got != c.over {
			t.Errorf("unclassedOverBound(%d, %d) = %v, want %v", c.unclassed, c.scenarios, got, c.over)
		}
	}
}

// TestPadSeatsDefectTableMatchesRecord: scenarios/padseats-defects.tsv parses
// strictly and is two-way with the record's gorge-defect rows; then each
// malformed table or record shape is rejected.
func TestPadSeatsDefectTableMatchesRecord(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(scenarioRoot, defectsName))
	if err != nil {
		t.Fatal(err)
	}
	table, err := parseDefects(data)
	if err != nil {
		t.Fatal(err)
	}
	scs, err := loadScenarios(scenarioRoot)
	if err != nil {
		t.Fatal(err)
	}
	steps, err := scenarioSteps(scs)
	if err != nil {
		t.Fatal(err)
	}
	stepsOf := func(k string) (int, bool) { n, ok := steps[k]; return n, ok }
	for _, e := range checkDefects(committedRecord(t), table, stepsOf) {
		t.Error(e)
	}
	t.Logf("defects=%d", len(table))

	// The fixture: two rows cite d800-4a, one cites d102-3; a is the shortest.
	fixSteps := map[string]int{"a#x": 1, "b#x": 3, "c#x": 2, "d#x": 1}
	fixStepsOf := func(k string) (int, bool) { n, ok := fixSteps[k]; return n, ok }
	rec := func(rows ...string) []row {
		r, err := parseRecord([]byte(recordHeader + "\n" + strings.Join(rows, "\n") + "\n"))
		if err != nil {
			t.Fatalf("fixture record: %v", err)
		}
		return r
	}
	gd := func(key, cr, diff, id string) string {
		return fmt.Sprintf("%s\tdiffers\tgorge-defect\t%s\t%s\tdefect:%s The rule is broken.", key, cr, diff, id)
	}
	goodRecord := []string{
		gd("a#x", "800.4a", "snap 1 p0.life: 1 != 2", "d800-4a"),
		gd("b#x", "800.4a", "snap 3 p1.life: 5 != 7", "d800-4a"),
		gd("c#x", "102.3", "snapshots: 2 != 3", "d102-3"),
		"d#x\tdiffers\teach-opponent\t102.3\tsnap 2 p0.life: 1 != 2\tEach opponent loses life.",
	}
	goodTable := defectsHeader + "\n" +
		"d102-3\t102.3\tsnapshots\t1\tc#x\tOne sentence.\n" +
		"d800-4a\t800.4a\tsnap N pN.life\t2\ta#x\tOne sentence.\n"
	tbl, err := parseDefects([]byte(goodTable))
	if err != nil {
		t.Fatalf("well-formed table: %v", err)
	}
	if errs := checkDefects(rec(goodRecord...), tbl, fixStepsOf); len(errs) != 0 {
		t.Fatalf("well-formed fixture: %v", errs)
	}

	for name, body := range map[string]string{
		"wrong header":     "defect\tcr\n",
		"five columns":     defectsHeader + "\nd1\t102.3\tg\t1\ta#x\n",
		"unsorted":         defectsHeader + "\nd2\t102.3\tg\t1\ta#x\ts.\nd1\t102.3\tg\t1\ta#x\ts.\n",
		"duplicate":        defectsHeader + "\nd1\t102.3\tg\t1\ta#x\ts.\nd1\t102.3\tg\t1\ta#x\ts.\n",
		"uppercase id":     defectsHeader + "\nD1\t102.3\tg\t1\ta#x\ts.\n",
		"long id":          defectsHeader + "\n" + strings.Repeat("d", 41) + "\t102.3\tg\t1\ta#x\ts.\n",
		"two rule ids":     defectsHeader + "\nd1\t102.2,102.3\tg\t1\ta#x\ts.\n",
		"empty cr":         defectsHeader + "\nd1\t\tg\t1\ta#x\ts.\n",
		"zero rows":        defectsHeader + "\nd1\t102.3\tg\t0\ta#x\ts.\n",
		"long summary":     defectsHeader + "\nd1\t102.3\tg\t1\ta#x\t" + strings.Repeat("s", 121) + "\n",
		"no final newline": defectsHeader + "\nd1\t102.3\tg\t1\ta#x\ts.",
	} {
		if _, err := parseDefects([]byte(body)); err == nil {
			t.Errorf("table %s: accepted", name)
		}
	}

	replace := func(i int, line string) []string {
		out := slices.Clone(goodRecord)
		out[i] = line
		return out
	}
	for name, c := range map[string]struct {
		record []row
		table  string
	}{
		"note without prefix": {rec(replace(0, "a#x\tdiffers\tgorge-defect\t800.4a\tsnap 1 p0.life: 1 != 2\tThe rule is broken.")...), goodTable},
		"id not in table":     {rec(replace(0, gd("a#x", "800.4a", "snap 1 p0.life: 1 != 2", "d999"))...), goodTable},
		"cr differs":          {rec(replace(1, gd("b#x", "800.4b", "snap 3 p1.life: 5 != 7", "d800-4a"))...), goodTable},
		"group differs":       {rec(replace(1, gd("b#x", "800.4a", "snap 3 p1.hand: 5 != 7", "d800-4a"))...), goodTable},
		"rows count":          {rec(goodRecord...), strings.Replace(goodTable, "\t2\ta#x", "\t3\ta#x", 1)},
		"repro not citing":    {rec(goodRecord...), strings.Replace(goodTable, "\t2\ta#x", "\t2\tc#x", 1)},
		"repro not shortest":  {rec(goodRecord...), strings.Replace(goodTable, "\t2\ta#x", "\t2\tb#x", 1)},
		"table row uncited":   {rec(goodRecord[:2]...), goodTable},
		"other class cites":   {rec(replace(3, "d#x\tdiffers\teach-opponent\t102.3\tsnap 2 p0.life: 1 != 2\tdefect:d102-3 x")...), goodTable},
		"gorge-defect no cr":  {[]row{{Scenario: "c#x", Verdict: verdictDiffers, Class: classGorgeDefect, FirstDiff: "snapshots: 2 != 3", Note: "defect:d102-3 x"}}, defectsHeader + "\nd102-3\t102.3\tsnapshots\t1\tc#x\ts.\n"},
	} {
		tbl, err := parseDefects([]byte(c.table))
		if err != nil {
			t.Fatalf("%s: table: %v", name, err)
		}
		if errs := checkDefects(c.record, tbl, fixStepsOf); len(errs) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
}
