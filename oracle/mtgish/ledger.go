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

// KnownDir holds the ledgers (one per tier, corpus-wide: T1.tsv .. T4.tsv)
// and the exclusion list, relative to the repository root.
const KnownDir = "oracle/mtgish/known"

// KernelSets are the sets the dirty-dirac kernel plays. The ledger covers
// the whole corpus; these keep their own per-tier agreement lines in the
// ratchet's log and their own headline tables in the -all report.
var KernelSets = []string{"FDN", "BLB"}

// LedgerPath is a tier's ledger file under root.
func LedgerPath(root string, t Tier) string {
	return filepath.Join(root, KnownDir, t.String()+".tsv")
}

// ReadLedgers reads the four tier ledgers under root. A row filed under
// the wrong tier's file is an error.
func ReadLedgers(root string) ([]Row, error) {
	var out []Row
	for _, t := range Tiers {
		path := LedgerPath(root, t)
		rows, err := ReadLedger(path)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r.Tier != t {
				return nil, fmt.Errorf("%s: %s: a %s row in the %s ledger", path, r.Card, r.Tier, t)
			}
		}
		out = append(out, rows...)
	}
	return out, nil
}

// WriteLedgers writes rows into the four tier ledgers under root.
func WriteLedgers(root string, rows []Row) (map[Tier]int, error) {
	by := map[Tier][]Row{}
	for _, r := range rows {
		by[r.Tier] = append(by[r.Tier], r)
	}
	n := map[Tier]int{}
	for _, t := range Tiers {
		if err := WriteLedger(LedgerPath(root, t), t.String(), by[t]); err != nil {
			return nil, err
		}
		n[t] = len(by[t])
	}
	return n, nil
}

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

// WriteLedger writes rows sorted by key, with a header comment naming what
// the file covers (a tier).
func WriteLedger(path, name string, rows []Row) error {
	SortRows(rows)
	var b strings.Builder
	fmt.Fprintf(&b, "# mtgish cross-check ledger, %s, whole corpus: every live disagreement and\n", name)
	b.WriteString("# normalizer gap, triaged. TestMtgishLedger holds the live diff equal to the\n")
	b.WriteString("# four tier files in both directions. Columns hold summary tokens only, never\n")
	b.WriteString("# card text. Rebuild with `go run ./cmd/mtgishxc -all -write-ledger` (keeps triage).\n")
	b.WriteString("# class: forge-bug | gorge-compile | mtgish-bug | equivalent | digital-variant\n")
	b.WriteString("#   | normalizer (a mapping debt; at most maxNormalizerRows, which only shrinks)\n")
	b.WriteString("#   | pending (not yet triaged; at most maxPendingRows, which only shrinks)\n")
	b.WriteString("# Rows whose unmapped tokens are all in excluded.tsv are class excluded, automatic, never here.\n")
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
// "normalizer"), which the ratchet test refuses. See MergeTriage.
func Merge(live, ledger []Row) []Row {
	out, _ := MergeTriage(live, false, ledger)
	return out
}

// MergeTriage carries triage onto live rows by key from several sources in
// priority order (the -triage files first, then the ledger): each live row
// takes its class and note from the first source holding its key, and
// consumes that key once from every later source too (superseded, not
// stale). A row no source triages gets "normalizer" when it is a gap, else
// "pending" when pending is set, else "untriaged" (refused by the ratchet).
// unused holds, per source, the rows that matched nothing live.
func MergeTriage(live []Row, pending bool, sources ...[]Row) (out []Row, unused [][]Row) {
	type queue struct{ rows []Row }
	tri := make([]map[string]*queue, len(sources))
	for i, src := range sources {
		tri[i] = map[string]*queue{}
		for _, r := range src {
			q := tri[i][r.Key()]
			if q == nil {
				q = &queue{}
				tri[i][r.Key()] = q
			}
			q.rows = append(q.rows, r)
		}
	}
	out = make([]Row, 0, len(live))
	for _, r := range live {
		k := r.Key()
		found := false
		for i := range sources {
			q := tri[i][k]
			if q == nil || len(q.rows) == 0 {
				continue
			}
			if !found {
				r.Class, r.Note = q.rows[0].Class, q.rows[0].Note
				found = true
			}
			q.rows = q.rows[1:]
		}
		if !found {
			switch {
			case r.Gap():
				r.Class, r.Note = "normalizer", "unmapped token"
			case pending:
				r.Class, r.Note = "pending", "not yet triaged"
			default:
				r.Class = "untriaged"
			}
		}
		out = append(out, r)
	}
	unused = make([][]Row, len(sources))
	for i, src := range sources {
		// Walk the source in its own order so the report is stable.
		left := map[string]int{}
		for k, q := range tri[i] {
			left[k] = len(q.rows)
		}
		for _, r := range src {
			if left[r.Key()] > 0 {
				left[r.Key()]--
				unused[i] = append(unused[i], r)
			}
		}
	}
	return out, unused
}
