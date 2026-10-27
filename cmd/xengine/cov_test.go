package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
)

func writeLedger(t *testing.T, path string, rows map[[2]string]cov.Counts) {
	t.Helper()
	l := cov.New()
	for k, c := range rows {
		l.Add(k[0], k[1], c)
	}
	var b bytes.Buffer
	if err := l.WriteTSV(&b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readLedger(t *testing.T, path string) *cov.Ledger {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := cov.ReadTSV(f)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestCovSub(t *testing.T) {
	dir := t.TempDir()
	p := func(n string) string { return filepath.Join(dir, n) }
	k := func(card, key string) [2]string { return [2]string{card, key} }

	// Snapshot L; two chunks played from it, each exercised = L + E_i.
	writeLedger(t, p("base.tsv"), map[[2]string]cov.Counts{
		k("A", "cast"): {Exercised: 2, Compared: 1},
		k("D", "k"):    {Exercised: 4},
	})
	writeLedger(t, p("ex1.tsv"), map[[2]string]cov.Counts{
		k("A", "cast"): {Exercised: 3, Compared: 1}, // L + E1 (+1 exercised)
		k("D", "k"):    {Exercised: 4},
		k("N", "new"):  {Exercised: 2}, // key not in base
	})
	writeLedger(t, p("ex2.tsv"), map[[2]string]cov.Counts{
		k("A", "cast"): {Exercised: 3, Compared: 1}, // L + E2 (+1 exercised)
		k("D", "k"):    {Exercised: 4},
	})
	writeLedger(t, p("cmp1.tsv"), map[[2]string]cov.Counts{k("A", "cast"): {Compared: 1}})
	writeLedger(t, p("cmp2.tsv"), map[[2]string]cov.Counts{k("A", "cast"): {Compared: 2}})

	// Plain form: exercised + compared - base.
	if err := runCov([]string{"-ledger", p("ex1.tsv"), "-ledger", p("cmp1.tsv"), "-sub", p("base.tsv"), "-write", p("out1.tsv")}); err != nil {
		t.Fatal(err)
	}
	out1 := readLedger(t, p("out1.tsv"))
	for _, w := range []struct {
		card, key string
		want      cov.Counts
	}{
		{"A", "cast", cov.Counts{Exercised: 1, Compared: 1}},
		{"D", "k", cov.Counts{}},
		{"N", "new", cov.Counts{Exercised: 2}},
	} {
		if got := out1.Get(w.card, w.key); got != w.want {
			t.Errorf("%s/%s: %+v, want %+v", w.card, w.key, got, w.want)
		}
	}

	// Saturation: the base exceeds the total, so counters clamp at 0.
	writeLedger(t, p("small.tsv"), map[[2]string]cov.Counts{k("A", "cast"): {Exercised: 1}})
	if err := runCov([]string{"-ledger", p("small.tsv"), "-sub", p("base.tsv"), "-write", p("out2.tsv")}); err != nil {
		t.Fatal(err)
	}
	if got := readLedger(t, p("out2.tsv")).Get("A", "cast"); got != (cov.Counts{}) {
		t.Errorf("saturation: %+v", got)
	}

	// The sweep's one-call form, run for chunk 1 and then chunk 2 against the
	// published ledger: current + E_i + C_i, the base subtracted once from the
	// total (not once per -ledger).
	writeLedger(t, p("cur0.tsv"), map[[2]string]cov.Counts{
		k("A", "cast"): {Exercised: 2, Compared: 1},
		k("D", "k"):    {Exercised: 4},
	})
	if err := runCov([]string{"-ledger", p("cur0.tsv"), "-ledger", p("ex1.tsv"), "-sub", p("base.tsv"), "-ledger", p("cmp1.tsv"), "-write", p("cur1.tsv")}); err != nil {
		t.Fatal(err)
	}
	if err := runCov([]string{"-ledger", p("cur1.tsv"), "-ledger", p("ex2.tsv"), "-sub", p("base.tsv"), "-ledger", p("cmp2.tsv"), "-write", p("cur2.tsv")}); err != nil {
		t.Fatal(err)
	}
	cur := readLedger(t, p("cur2.tsv"))
	// base A/cast {2,1}; E1 +1 exercised, E2 +1 exercised; C1 +1, C2 +2 compared.
	if got := cur.Get("A", "cast"); got != (cov.Counts{Exercised: 4, Compared: 4}) {
		t.Errorf("A/cast after both chunks: %+v, want {4 4 0}", got)
	}
	if got := cur.Get("D", "k"); got != (cov.Counts{Exercised: 4}) {
		t.Errorf("D/k after both chunks: %+v", got)
	}
	if got := cur.Get("N", "new"); got != (cov.Counts{Exercised: 2}) {
		t.Errorf("N/new after both chunks: %+v", got)
	}

	if err := runCov([]string{"-ledger", p("ex1.tsv"), "-sub", p("missing.tsv")}); err == nil {
		t.Error("missing -sub file did not fail")
	}
}
