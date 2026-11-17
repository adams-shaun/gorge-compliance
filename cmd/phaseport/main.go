// Command phaseport is the census step toward a phase.rs test translator
// (docs: backlog item 10). It reads phase.rs engine integration tests
// (crates/engine/tests/integration/*.rs) without a Rust parser, classifies
// each #[test] by how it populates the GameScenario builder, and reports which
// ones gorge could run: every real card (add_real_card) supported.
//
//	phaseport -census <tests dir> [-cards .cards]
//
// It translates nothing and commits nothing: the TSV and summary go to stdout.
// phase.rs is Apache-2.0; only counts and names are ever quoted from it.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func main() {
	census := flag.String("census", "", "phase.rs tests directory to census (crates/engine/tests/integration)")
	corpus := flag.String("cards", ".cards", "gorge .cards corpus directory")
	flag.Parse()
	if *census == "" {
		fmt.Fprintln(os.Stderr, "usage: phaseport -census <tests dir> [-cards .cards]")
		os.Exit(2)
	}
	reg, err := testutil.OpenCorpusRegistry(*corpus)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	rows, err := runCensus(*census, registrySupport(reg))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	writeTSV(os.Stdout, rows)
	writeSummary(os.Stdout, rows)
}

func registrySupport(reg *cards.Registry) cardSupport {
	supported := effects.Supported()
	cache := map[string][2]bool{}
	return func(name string) (bool, bool) {
		if v, ok := cache[name]; ok {
			return v[0], v[1]
		}
		var v [2]bool
		if c, ok := reg.Lookup(name); ok {
			v = [2]bool{len(reg.Unsupported(c, supported)) == 0, true}
		}
		cache[name] = v
		return v[0], v[1]
	}
}

// runCensus walks dir (lexical order) and classifies every .rs file's tests.
func runCensus(dir string, supp cardSupport) ([]testRow, error) {
	var rows []testRow
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".rs") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rows = append(rows, censusFile(filepath.ToSlash(rel), string(src), supp)...)
		return nil
	})
	return rows, err
}

func writeTSV(w io.Writer, rows []testRow) {
	fmt.Fprintln(w, "file\ttest\tclass\treal_cards\tgorge_ok\tbuilder_calls\tassert_kinds\tcr_cites")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%t\t%s\t%s\t%s\n", r.File, r.Name, r.Class,
			strings.Join(r.Cards, "; "), r.GorgeOK, strings.Join(r.Calls, ","),
			strings.Join(r.Asserts, ","), strings.Join(r.CRs, ","))
	}
}

const topN = 30

func writeSummary(w io.Writer, rows []testRow) {
	counts := map[string]int{}
	var ok, okReal, withReal int
	callsAll, assertsAll := map[string]int{}, map[string]int{}
	callsReal, assertsReal := map[string]int{}, map[string]int{}
	for _, r := range rows {
		counts[r.Class]++
		if len(r.Cards) > 0 {
			withReal++
		}
		if !r.GorgeOK {
			continue
		}
		ok++
		for _, c := range r.Calls {
			callsAll[c]++
		}
		for _, a := range r.Asserts {
			assertsAll[a]++
		}
		if r.Class == classRealOnly {
			okReal++
			for _, c := range r.Calls {
				callsReal[c]++
			}
			for _, a := range r.Asserts {
				assertsReal[a]++
			}
		}
	}
	fmt.Fprintln(w, "\n# summary")
	fmt.Fprintf(w, "tests\t%d\n", len(rows))
	for _, c := range classOrder {
		fmt.Fprintf(w, "class %s\t%d\n", c, counts[c])
	}
	fmt.Fprintf(w, "tests with a real card\t%d\n", withReal)
	fmt.Fprintf(w, "gorge_ok tests (any class)\t%d\n", ok)
	fmt.Fprintf(w, "gorge_ok real-only tests\t%d\n", okReal)
	rank(w, fmt.Sprintf("top %d builder calls, gorge_ok tests (tests using the call)", topN), callsAll)
	rank(w, fmt.Sprintf("top %d assert kinds, gorge_ok tests (tests using the kind)", topN), assertsAll)
	rank(w, fmt.Sprintf("top %d builder calls, gorge_ok real-only tests", topN), callsReal)
	rank(w, fmt.Sprintf("top %d assert kinds, gorge_ok real-only tests", topN), assertsReal)
}

func rank(w io.Writer, title string, m map[string]int) {
	type kv struct {
		k string
		n int
	}
	var xs []kv
	for k, n := range m {
		xs = append(xs, kv{k, n})
	}
	sort.Slice(xs, func(i, j int) bool {
		if xs[i].n != xs[j].n {
			return xs[i].n > xs[j].n
		}
		return xs[i].k < xs[j].k
	})
	fmt.Fprintf(w, "\n## %s\n", title)
	for i, x := range xs {
		if i == topN {
			break
		}
		fmt.Fprintf(w, "%d\t%s\n", x.n, x.k)
	}
}
