package mtgish

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LockFile is the committed data pin, relative to the repository root.
const LockFile = "oracle/mtgish/MTGISH.lock"

// KnownDir holds the per-set ledgers, relative to the repository root.
const KnownDir = "oracle/mtgish/known"

// LedgerSets are the sets whose disagreements are held to a ledger.
var LedgerSets = []string{"FDN", "BLB"}

// ReadLockRef reads the pinned mtgish ref from a lock file.
func ReadLockRef(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "ref" {
			return f[1], nil
		}
	}
	return "", fmt.Errorf("%s: no ref line", path)
}

// DataPath is where `make mtgish-fetch` puts the pinned data.
func DataPath(root, ref string) string {
	return filepath.Join(root, ".mtgish", ref, "mtgish.lines.json")
}

// ledgerHeader is the TSV's column line.
const ledgerHeader = "card\ttier\tfield\tgorge\tmtgish\tclass\tnote"

// ReadLedger reads a known/<SET>.tsv. Lines starting with '#' and the
// header are skipped.
func ReadLedger(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Row
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") || line == ledgerHeader {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 7 {
			return nil, fmt.Errorf("%s:%d: %d columns, want 7", path, n, len(c))
		}
		t, ok := parseTier(c[1])
		if !ok {
			return nil, fmt.Errorf("%s:%d: tier %q", path, n, c[1])
		}
		out = append(out, Row{Card: c[0], Tier: t, Field: c[2], Gorge: c[3], Mtgish: c[4], Class: c[5], Note: c[6]})
	}
	return out, sc.Err()
}

func parseTier(s string) (Tier, bool) {
	for _, t := range Tiers {
		if t.String() == s {
			return t, true
		}
	}
	return 0, false
}

// WriteLedger writes rows sorted by key, with a header comment.
func WriteLedger(path, set string, rows []Row) error {
	SortRows(rows)
	var b strings.Builder
	fmt.Fprintf(&b, "# mtgish cross-check ledger for %s: every live disagreement and normalizer gap,\n", set)
	b.WriteString("# triaged. TestMtgishLedger holds the live diff equal to this file in both\n")
	b.WriteString("# directions. Columns hold summary tokens only, never card text.\n")
	b.WriteString("# class: forge-bug | gorge-compile | mtgish-bug | normalizer | equivalent\n")
	b.WriteString(ledgerHeader + "\n")
	for _, r := range rows {
		b.WriteString(strings.Join([]string{r.Card, r.Tier.String(), r.Field, r.Gorge, r.Mtgish, r.Class, r.Note}, "\t") + "\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// SortRows orders rows by card, tier, field, then sides.
func SortRows(rows []Row) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key() < rows[j].Key() })
}

// LedgerDiff compares live rows against a ledger: rows live but not in the
// ledger (new disagreements) and ledger rows no longer live (stale).
func LedgerDiff(live, ledger []Row) (added, stale []Row) {
	have := map[string]int{}
	for _, r := range ledger {
		have[r.Key()]++
	}
	for _, r := range live {
		if have[r.Key()] > 0 {
			have[r.Key()]--
			continue
		}
		added = append(added, r)
	}
	want := map[string]int{}
	for _, r := range live {
		want[r.Key()]++
	}
	for _, r := range ledger {
		if want[r.Key()] > 0 {
			want[r.Key()]--
			continue
		}
		stale = append(stale, r)
	}
	return added, stale
}

// Merge carries ledger triage (class, note) onto live rows by key; rows
// without triage get class "untriaged" (a gap row defaults to
// "normalizer"), which the ratchet test refuses.
func Merge(live, ledger []Row) []Row {
	tri := map[string][]Row{}
	for _, r := range ledger {
		tri[r.Key()] = append(tri[r.Key()], r)
	}
	out := make([]Row, 0, len(live))
	for _, r := range live {
		if l := tri[r.Key()]; len(l) > 0 {
			r.Class, r.Note = l[0].Class, l[0].Note
			tri[r.Key()] = l[1:]
		} else if r.Gap() {
			r.Class, r.Note = "normalizer", "unmapped token"
		} else {
			r.Class = "untriaged"
		}
		out = append(out, r)
	}
	return out
}
