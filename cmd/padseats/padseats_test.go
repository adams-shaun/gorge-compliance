package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// scenarioRoot is scenarios/, from this package's directory.
var scenarioRoot = filepath.Join("..", "..", "scenarios")

// committedRecord reads scenarios/padseats.tsv, which must exist.
func committedRecord(t *testing.T) []row {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(scenarioRoot, recordName))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parseRecord(data)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestPadSeatsSampleMatchesRecord plays one shard in 64 and requires the
// committed record's rows of that shard to be exactly what it measures.
func TestPadSeatsSampleMatchesRecord(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	all, err := loadScenarios(scenarioRoot)
	if err != nil {
		t.Fatal(err)
	}
	sh := shard{0, 64}
	scs := slices.DeleteFunc(all, func(s scenario) bool { return !sh.has(s.Key) })
	if len(scs) == 0 {
		t.Fatal("shard 0/64 holds no scenario")
	}
	rows, err := play(reg, scs)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range compare(rows, committedRecord(t), sh) {
		t.Error(d)
	}
	triage(rows, committedRecord(t))
	t.Log(summary(rows))
}

// TestPadSeatsRecordIsWellFormed needs no corpus: the committed record parses
// strictly (parseRecord), every key names a scenario in the tree, and the
// parser rejects each malformed shape.
func TestPadSeatsRecordIsWellFormed(t *testing.T) {
	scs, err := loadScenarios(scenarioRoot)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, s := range scs {
		keys[s.Key] = true
	}
	for _, r := range committedRecord(t) {
		if !keys[r.Scenario] {
			t.Errorf("row %q names no scenario of an existing file", r.Scenario)
		}
	}

	good := recordHeader + "\na#x\tdiffers\teach-opponent\t102.3\td\tn\nb#x\tnot-two-seat\t\t\t\t\n"
	if rows, err := parseRecord([]byte(good)); err != nil || len(rows) != 2 {
		t.Fatalf("well-formed record: %d rows, %v", len(rows), err)
	}
	for name, body := range map[string]string{
		"five columns":       "a#x\tdiffers\t\t\td\n",
		"unsorted":           "b#x\tdiffers\t\t\td\t\na#x\tdiffers\t\t\td\t\n",
		"duplicate":          "a#x\tdiffers\t\t\td\t\na#x\tdiffers\t\t\td\t\n",
		"verdict same":       "a#x\tsame\t\t\t\t\n",
		"verdict base-fails": "a#x\tbase-fails\t\t\t\t\n",
		"unknown class":      "a#x\tdiffers\tbecause\t102.3\td\t\n",
		"class without cr":   "a#x\tdiffers\tgorge-defect\t\td\t\n",
		"differs, no diff":   "a#x\tdiffers\t\t\t\t\n",
		"no final newline":   "a#x\tdiffers\t\t\td\t",
	} {
		if _, err := parseRecord([]byte(recordHeader + "\n" + body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := parseRecord([]byte("scenario\tverdict\n")); err == nil {
		t.Error("wrong header: accepted")
	}
}

// TestPadSeatsShardsPartition: every key falls in exactly one of n shards.
func TestPadSeatsShardsPartition(t *testing.T) {
	const n = 4
	used := map[int]bool{}
	for k := range 200 {
		key := fmt.Sprintf("oracle/family/card-%d#scenario-%d", k, k%7)
		var in []int
		for i := range n {
			sh, err := parseShard(fmt.Sprintf("%d/%d", i, n))
			if err != nil {
				t.Fatal(err)
			}
			if sh.has(key) {
				in = append(in, i)
				used[i] = true
			}
		}
		if len(in) != 1 {
			t.Errorf("%s is in shards %v, want exactly one", key, in)
		}
	}
	if len(used) != n {
		t.Errorf("200 keys used %d of %d shards", len(used), n)
	}
	for _, bad := range []string{"", "1", "4/4", "-1/4", "0/0", "a/4", "0/4/2"} {
		if _, err := parseShard(bad); err == nil {
			t.Errorf("-shard %q: accepted", bad)
		}
	}
}

// TestPadSeatsRecordDrift: each way the record can depart from a run is
// reported, only within the shards run, and -write keeps a triage only while
// the difference it explains is unchanged.
func TestPadSeatsRecordDrift(t *testing.T) {
	whole := shard{0, 1}
	record := []row{
		{Scenario: "changed", Verdict: verdictDiffers, Class: "harness", CR: "1", FirstDiff: "old"},
		{Scenario: "fixed", Verdict: verdictDiffers, FirstDiff: "d"},
		{Scenario: "gone", Verdict: verdictDiffers, FirstDiff: "d"},
		{Scenario: "kept", Verdict: verdictDiffers, Class: "each-opponent", CR: "102.3", FirstDiff: "d", Note: "n"},
	}
	measured := []row{
		{Scenario: "changed", Verdict: verdictDiffers, FirstDiff: "new"},
		{Scenario: "fixed", Verdict: verdictSame},
		{Scenario: "kept", Verdict: verdictDiffers, FirstDiff: "d"},
		{Scenario: "new", Verdict: verdictNotTwoSeat},
	}
	triage(measured, record)
	got := compare(measured, record, whole)
	want := []string{"changed row: changed\t", "missing row: new\t", "stale row: fixed\tnow same", "stale row: gone\tno such scenario"}
	if len(got) != len(want) {
		t.Fatalf("compare: %q, want %d mismatches", got, len(want))
	}
	for i, w := range want {
		if !strings.HasPrefix(got[i], w) {
			t.Errorf("mismatch %d: %q, want prefix %q", i, got[i], w)
		}
	}
	next := rewrite(measured, record, whole)
	if d := compare(measured, next, whole); len(d) != 0 {
		t.Errorf("after rewrite: %q", d)
	}
	if want := []row{measured[0], record[3], measured[3]}; !slices.Equal(next, want) {
		t.Errorf("rewrite: %+v, want %+v", next, want)
	}
	if got, want := summary(measured), "padseats: scenarios=4 same=1 explained=1 unexplained=1 base-fails=0 not-two-seat=1"; got != want {
		t.Errorf("summary %q, want %q", got, want)
	}

	// A run of one shard neither reports nor rewrites another's rows.
	other := shard{0, 2}
	if other.has("gone") {
		other.I = 1
	}
	if d := compare(nil, record[2:3], other); len(d) != 0 {
		t.Errorf("another shard's row reported: %q", d)
	}
	if got := rewrite(nil, record[2:3], other); !slices.Equal(got, record[2:3]) {
		t.Errorf("another shard's row rewritten: %+v", got)
	}
}
