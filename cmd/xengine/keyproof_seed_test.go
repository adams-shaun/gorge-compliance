package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
)

// seedCorpus is the diagnosed fixture corpus plus the seed fixture card: one
// card holding an activated ability, a trigger, a static and a replacement.
func seedCorpus(t *testing.T) string {
	t.Helper()
	dir := diagnosedCorpus(t)
	b, err := os.ReadFile(filepath.Join("testdata", "seed", "matrix.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cardsfolder", "a", "matrix.txt"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeScope writes the tab-separated oracle_id<TAB>name file keyproof -seed
// reads, and returns its path.
func writeScope(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scope.tsv")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const seedGorgeSHA = "8f7095353a64"
const seedMeasured = "2026-11-27"

// runSeed runs seedKeyproof and returns the printed summary and the parsed
// seed file rows.
func runSeed(t *testing.T, corpus, scope, out string) (string, []map[string]string) {
	t.Helper()
	reg, err := loadReg(corpus)
	if err != nil {
		t.Fatal(err)
	}
	var runErr error
	out2 := captureStdout(t, func() {
		runErr = seedKeyproof(reg, scope, corpus, seedMeasured, seedGorgeSHA, out)
	})
	if runErr != nil {
		t.Fatalf("seedKeyproof: %v", runErr)
	}
	_, rows := readTSV(t, filepath.Join(out, "keyproof-seed.tsv"))
	return out2, rows
}

func TestKeyproofSeedCoversEveryFamily(t *testing.T) {
	corpus := seedCorpus(t)
	reg, err := loadReg(corpus)
	if err != nil {
		t.Fatal(err)
	}
	var scope []string
	oidOf := map[string]string{}
	for i, c := range reg.AllCards() {
		if len(c.Faces) == 0 || c.Faces[0] == nil {
			continue
		}
		name := c.Faces[0].Name
		oid := "aa" + strings.Repeat("0", 28) + string(rune('a'+i)) + "0"
		scope = append(scope, oid+"\t"+name)
		oidOf[oid] = name
	}
	if len(oidOf) < 2 {
		t.Fatalf("fixture corpus holds %d card(s), want at least Alpha and Matrix Warden", len(oidOf))
	}
	if _, ok := reg.Lookup("Matrix Warden"); !ok {
		t.Fatal("the seed fixture card did not compile into the registry")
	}
	scope = append([]string{"oracle_id\tname", "# a comment line"}, scope...)
	out := t.TempDir()
	summary, rows := runSeed(t, corpus, writeScope(t, scope), out)

	// The header is keyproof's v1 column order without the legacy card column.
	hdr, _ := readTSV(t, filepath.Join(out, "keyproof-seed.tsv"))
	if want := strings.Split(keyproofSeedColumns, "\t"); len(hdr) != len(want) {
		t.Fatalf("header has %d columns, want %d", len(hdr), len(want))
	}

	// One pair of rows per key of every scope card: 2 * len(cov.Universe(c)).
	byOID := map[string][]map[string]string{}
	for _, r := range rows {
		byOID[r["oracle_id"]] = append(byOID[r["oracle_id"]], r)
	}
	for oid, name := range oidOf {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s: not in the registry", name)
		}
		want := 2 * len(cov.Universe(c))
		if got := len(byOID[oid]); got != want {
			t.Errorf("%s: %d seeded rows, want %d (2 per key of %d)", name, got, want, len(cov.Universe(c)))
		}
	}
	if !strings.Contains(summary, "0 not in corpus") {
		t.Errorf("summary %q counts a not-in-corpus card where none exists", summary)
	}
	for _, r := range rows {
		if r["state"] != "owed" || r["source"] != "seed" || r["writer"] != "seed" {
			t.Errorf("row %+v: not an owed seed row", r)
		}
		if r["reason"] != "untried" {
			t.Errorf("row %+v: reason %q, want untried", r, r["reason"])
		}
		if r["leg"] != "xmage" && r["leg"] != "lockstep" {
			t.Errorf("row %+v: leg %q", r, r["leg"])
		}
	}

	// Every family the fixture card's IR holds has a seeded key of that
	// family's id form.
	c, _ := reg.Lookup("Matrix Warden")
	seeded := map[string]bool{}
	for _, r := range rows {
		if oidOf[r["oracle_id"]] == "Matrix Warden" {
			seeded[cov.Family(r["key"])] = true
		}
	}
	present := map[string]bool{}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, a := range f.Abilities {
			if a != nil && a.Kind == "AB" {
				present["activated"] = true
			}
		}
		if len(f.Triggers) > 0 {
			present["trigger"] = true
		}
		if len(f.Statics) > 0 {
			present["static"] = true
		}
		if len(f.Repls) > 0 {
			present["repl"] = true
		}
	}
	for fam := range present {
		if !seeded[fam] {
			t.Errorf("the fixture card's IR holds a %s and nothing of family %s was seeded (seeded: %v)", fam, fam, keysOf(seeded))
		}
	}
	for _, want := range []string{"activated", "trigger", "static", "repl"} {
		if !present[want] {
			t.Errorf("the fixture card's IR holds no %s; the fixture must hold one of each seeded family", want)
		}
	}
}

func keysOf(m map[string]bool) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestKeyproofSeedNotInCorpus(t *testing.T) {
	corpus := seedCorpus(t)
	out := t.TempDir()
	oid := "ffff" + strings.Repeat("0", 28)
	summary, rows := runSeed(t, corpus, writeScope(t, []string{oid + "\tNoSuch Card"}), out)
	if len(rows) != 2 {
		t.Fatalf("%d rows, want the two cast rows:\n%v", len(rows), rows)
	}
	if !strings.Contains(summary, "1 not in corpus") {
		t.Errorf("summary %q does not count the miss", summary)
	}
	for i, r := range rows {
		want := map[string]string{
			"oracle_id": oid, "key": "cast", "leg": []string{"lockstep", "xmage"}[i], "desc": "cast",
			"state": "owed", "reason": "not-in-corpus", "refs": "-", "source": "seed",
			"agree_items": "0", "diff_items": "0", "witness": "-", "sig": "-", "cite": "-", "ticket": "-",
			"seats": "0", "multiplayer": "false", "gorge_sha": seedGorgeSHA, "corpus_pin": "-", "script_sha": "-",
			"xmage_ref": "-", "forge_ref": "-", "measured": seedMeasured, "replayed": "-", "replayed_sha": "-",
			"writer": "seed",
		}
		for k, v := range want {
			if r[k] != v {
				t.Errorf("row %d column %s: %q, want %q", i, k, r[k], v)
			}
		}
	}
}
