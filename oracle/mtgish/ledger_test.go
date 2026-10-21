package mtgish_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/mtgish"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// repoRoot walks up from the package directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package directory")
		}
		dir = parent
	}
}

// maxNormalizerRows caps the ledger's `normalizer` rows: unmapped tokens
// carried as mapping debt. It may only shrink. The program drives it to 0,
// after which any normalizer row fails (and the class is deleted). When the
// test logs fewer rows than this, lower it to the logged count.
const maxNormalizerRows = 0

// maxPendingRows caps the ledger's `pending` rows: disagreements the
// program has not triaged yet (the initial corpus-wide generation). It may
// only shrink, to 0, like maxNormalizerRows.
const maxPendingRows = 411

// maxListed caps the NEW/STALE lines printed per kind; the counts are
// always given in full.
const maxListed = 60

// TestMtgishLedger is the cross-check's ratchet, corpus-wide: the live
// differences between gorge's compiled cards and mtgish, over every card both
// sides know, must equal oracle/mtgish/known/T1.tsv .. T4.tsv in both
// directions. A new difference fails (fix the normalizer, or triage the row
// into its tier's file); a ledger row that no longer differs fails as stale
// (delete it). This is the shape of gorge's TestEveryRepoDeckIsFullySupported.
//
// known/excluded.tsv names the deliberate holes: excluded cards are not
// compared, and a row whose unmapped tokens are all excluded tokens is class
// excluded, needs no ledger line and is not a gap. A malformed or stale
// exclusion fails like a stale row.
//
// Two classes are debts with shrink-only caps: normalizer
// (maxNormalizerRows) and pending (maxPendingRows). The kernel sets
// (KernelSets: FDN, BLB) log their own per-tier agreement lines.
//
// It needs the gorge corpus (.cards) and the pinned mtgish data
// (`make mtgish-fetch`); without either it SKIPS, loudly.
func TestMtgishLedger(t *testing.T) {
	root := repoRoot(t)
	ref, err := mtgish.ReadLockRef(filepath.Join(root, mtgish.LockFile))
	if err != nil {
		t.Fatal(err)
	}
	data := mtgish.DataPath(root, ref)
	if _, err := os.Stat(data); err != nil {
		t.Skipf("SKIP: no mtgish data at %s (run `make mtgish-fetch`): the ledger ratchet did not run", data)
	}
	reg := testutil.CorpusRegistry(t)

	ex, err := mtgish.ReadExclusions(filepath.Join(root, mtgish.ExclusionsFile))
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := mtgish.ReadLedgers(root)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := mtgish.Load(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := mtgish.RunWith(mtgish.SortedNames(ms), reg, ms, ex)

	for _, u := range ex.Unused(res) {
		t.Errorf("STALE exclusion (matches nothing live; delete it from %s):\n\t%s", mtgish.ExclusionsFile, u)
	}

	byClass := map[string]int{}
	for _, r := range ledger {
		byClass[r.Class]++
		if !mtgish.ValidClass(r.Class) {
			t.Errorf("%s: %s %s %s: class %q is not one of %v", mtgish.LedgerPath("", r.Tier), r.Card, r.Tier, r.Field, r.Class, mtgish.Classes)
		}
		if strings.TrimSpace(r.Note) == "" {
			t.Errorf("%s: %s %s %s: empty note", mtgish.LedgerPath("", r.Tier), r.Card, r.Tier, r.Field)
		}
	}

	added, stale := mtgish.LedgerDiff(res.Rows, ledger)
	for i, r := range added {
		if i == maxListed {
			t.Errorf("... and %d more NEW differences", len(added)-maxListed)
			break
		}
		t.Errorf("NEW difference (fix the normalizer or triage it into %s):\n\t%s", mtgish.LedgerPath("", r.Tier), r.Key())
	}
	for i, r := range stale {
		if i == maxListed {
			t.Errorf("... and %d more STALE rows", len(stale)-maxListed)
			break
		}
		t.Errorf("STALE ledger row (no longer differs; delete it from %s):\n\t%s", mtgish.LedgerPath("", r.Tier), r.Key())
	}
	if len(added)+len(stale) > 0 {
		t.Logf("%d new, %d stale; refresh with: go run ./cmd/mtgishxc -all -write-ledger (keeps existing triage), then triage the new rows", len(added), len(stale))
	}

	for _, c := range []struct {
		class string
		max   int
		name  string
	}{{"normalizer", maxNormalizerRows, "maxNormalizerRows"}, {"pending", maxPendingRows, "maxPendingRows"}} {
		n := byClass[c.class]
		switch {
		case n > c.max:
			t.Errorf("%d %s rows in the ledger, over %s = %d (it only shrinks: map or triage them)", n, c.class, c.name, c.max)
		case n < c.max:
			t.Logf("%d %s rows, under %s = %d: lower the constant to %d", n, c.class, c.name, c.max, n)
		default:
			t.Logf("%d %s rows = %s", n, c.class, c.name)
		}
	}

	excl := map[string]int{}
	for _, r := range res.Excluded {
		excl[r.Tier.String()]++
	}
	t.Logf("ALL: %d cards compared, %d excluded cards, %d excluded rows %v; %d ledger rows %v",
		len(res.Pairs), len(res.ExcludedCards), len(res.Excluded), excl, len(ledger), classCounts(byClass))
	logTiers(t, res)

	for _, s := range mtgish.KernelSets {
		names, err := mtgish.ManifestNames(root, s)
		if err != nil {
			t.Fatal(err)
		}
		sub := res.Subset(names)
		if len(sub.NoGorge)+len(sub.NoMtgish) > 0 {
			t.Errorf("%s: cards missing a side (gorge: %v; mtgish: %v)", s, sub.NoGorge, sub.NoMtgish)
		}
		kc := map[string]int{}
		for _, r := range mtgish.Merge(sub.Rows, ledger) {
			kc[r.Class]++
		}
		t.Logf("%s (kernel set): %d cards compared, %d rows %v", s, len(sub.Pairs), len(sub.Rows), classCounts(kc))
		logTiers(t, sub)
	}
}

func logTiers(t *testing.T, res mtgish.Result) {
	t.Helper()
	st := res.Stats()
	for _, tier := range mtgish.Tiers {
		x := st[tier]
		t.Logf("  %s: %d/%d faces agree, %d disagree, %d gap-only, %d excluded-only", tier, x.Agree, x.Faces, x.Disagree, x.Gap, x.Excluded)
	}
}

func classCounts(m map[string]int) string {
	var b strings.Builder
	for _, c := range append(append([]string(nil), mtgish.Classes...), "untriaged", mtgish.ClassExcluded) {
		if m[c] > 0 {
			if b.Len() > 0 {
				b.WriteString(" ")
			}
			b.WriteString(c + "=" + strconv.Itoa(m[c]))
		}
	}
	return "(" + b.String() + ")"
}
