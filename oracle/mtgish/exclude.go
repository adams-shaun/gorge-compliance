package mtgish

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ExclusionsFile is the committed exclusion list, relative to the
// repository root.
const ExclusionsFile = KnownDir + "/excluded.tsv"

// ClassExcluded is the class of a row whose unmapped tokens are all
// excluded tokens. It is assigned automatically, never written in a ledger.
const ClassExcluded = "excluded"

// Exclusions are the deliberate holes in the comparison
// (oracle/mtgish/known/excluded.tsv), each with its reason:
//
//   - a `card` exclusion keeps a card out of the comparison entirely; it is
//     counted and listed in the report header;
//   - a `token` exclusion names an `unmapped:X` spelling that is deliberately
//     not mapped (an Un-card mechanic, Attractions, ...). A row all of whose
//     unmapped tokens are excluded is classed `excluded`: it needs no ledger
//     line, is reported apart and is not counted as a gap.
type Exclusions struct {
	Card  map[string]string // card name -> reason
	Token map[string]string // unmapped:X -> reason
}

// ReadExclusions reads an exclusion file: lines `kind<TAB>key<TAB>reason`,
// kind `card` or `token`; '#' lines and blank lines are skipped. A
// malformed line, an unknown kind, a token key that is not an `unmapped:`
// spelling, an empty reason or a duplicate key is an error.
func ReadExclusions(path string) (Exclusions, error) {
	ex := Exclusions{Card: map[string]string{}, Token: map[string]string{}}
	f, err := os.Open(path)
	if err != nil {
		return ex, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 3 {
			return ex, fmt.Errorf("%s:%d: %d columns, want 3 (kind, key, reason)", path, n, len(c))
		}
		kind, key, reason := c[0], c[1], strings.TrimSpace(c[2])
		if key == "" || key != strings.TrimSpace(key) {
			return ex, fmt.Errorf("%s:%d: empty or space-padded key %q", path, n, key)
		}
		if reason == "" {
			return ex, fmt.Errorf("%s:%d: %s %q has no reason", path, n, kind, key)
		}
		var m map[string]string
		switch kind {
		case "card":
			m = ex.Card
		case "token":
			if !IsUnmapped(key) || key == "unmapped:" || strings.ContainsAny(key, " \t") {
				return ex, fmt.Errorf("%s:%d: token %q is not an unmapped:X spelling", path, n, key)
			}
			m = ex.Token
		default:
			return ex, fmt.Errorf("%s:%d: kind %q, want card or token", path, n, kind)
		}
		if _, dup := m[key]; dup {
			return ex, fmt.Errorf("%s:%d: %s %q listed twice", path, n, kind, key)
		}
		m[key] = reason
	}
	return ex, sc.Err()
}

// RowExcluded reports whether r carries at least one unmapped token and
// every unmapped token it carries is an excluded token.
func (e Exclusions) RowExcluded(r Row) bool {
	hit := false
	for _, side := range []string{r.Gorge, r.Mtgish} {
		for _, t := range strings.Fields(side) {
			if !IsUnmapped(t) {
				continue
			}
			if _, ok := e.Token[t]; !ok {
				return false
			}
			hit = true
		}
	}
	return hit
}

// Unused lists the exclusions that match nothing in a run, as
// "card\tName" and "token\tunmapped:X", sorted: a card exclusion whose card
// was not one the run would have compared (both sides present), and a token
// exclusion that no row of the run carries. Like a stale ledger row, an
// unused exclusion is to be deleted.
func (e Exclusions) Unused(res Result) []string {
	var out []string
	excl := map[string]bool{}
	for _, c := range res.ExcludedCards {
		excl[c] = true
	}
	for c := range e.Card {
		if !excl[c] {
			out = append(out, "card\t"+c)
		}
	}
	seen := map[string]bool{}
	for _, rows := range [][]Row{res.Rows, res.Excluded} {
		for _, r := range rows {
			for _, side := range []string{r.Gorge, r.Mtgish} {
				for _, t := range strings.Fields(side) {
					if _, ok := e.Token[t]; ok {
						seen[t] = true
					}
				}
			}
		}
	}
	for t := range e.Token {
		if !seen[t] {
			out = append(out, "token\t"+t)
		}
	}
	sort.Strings(out)
	return out
}
