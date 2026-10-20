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

// TestMtgishLedger is the cross-check's ratchet: for each ledger set (FDN,
// BLB) the live differences between gorge's compiled cards and mtgish must
// equal oracle/mtgish/known/<SET>.tsv in both directions. A new difference
// fails (triage it: fix the normalizer, or add the row with its class); a
// ledger row that no longer differs fails as stale (delete it). This is the
// shape of gorge's TestEveryRepoDeckIsFullySupported.
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

	perSet := map[string][]string{}
	want := map[string]bool{}
	for _, s := range mtgish.LedgerSets {
		names, err := mtgish.ManifestNames(root, s)
		if err != nil {
			t.Fatal(err)
		}
		perSet[s] = names
		for _, n := range names {
			want[n] = true
		}
	}
	ms, err := mtgish.Load(data, func(n string) bool { return want[n] })
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range mtgish.LedgerSets {
		t.Run(s, func(t *testing.T) {
			res := mtgish.Run(perSet[s], reg, ms)
			if len(res.NoGorge)+len(res.NoMtgish) > 0 {
				t.Errorf("%s: cards missing a side (gorge: %v; mtgish: %v)", s, res.NoGorge, res.NoMtgish)
			}
			path := filepath.Join(root, mtgish.KnownDir, s+".tsv")
			ledger, err := mtgish.ReadLedger(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range ledger {
				if !mtgish.ValidClass(r.Class) {
					t.Errorf("%s: %s %s %s: class %q is not one of %v", path, r.Card, r.Tier, r.Field, r.Class, mtgish.Classes)
				}
				if strings.TrimSpace(r.Note) == "" {
					t.Errorf("%s: %s %s %s: empty note", path, r.Card, r.Tier, r.Field)
				}
			}
			added, stale := mtgish.LedgerDiff(res.Rows, ledger)
			for _, r := range added {
				t.Errorf("NEW difference (fix the normalizer or triage it into %s):\n\t%s", path, r.Key())
			}
			for _, r := range stale {
				t.Errorf("STALE ledger row (no longer differs; delete it from %s):\n\t%s", path, r.Key())
			}
			if len(added)+len(stale) > 0 {
				t.Logf("refresh with: go run ./cmd/mtgishxc -set %s -write-ledger (keeps existing triage)", s)
			}

			st := res.Stats()
			byClass := map[string]int{}
			for _, r := range ledger {
				byClass[r.Class]++
			}
			t.Logf("%s: %d cards compared, %d ledger rows %v", s, len(res.Pairs), len(ledger), classCounts(byClass))
			for _, tier := range mtgish.Tiers {
				x := st[tier]
				t.Logf("  %s: %d/%d faces agree, %d disagree, %d gap-only", tier, x.Agree, x.Faces, x.Disagree, x.Gap)
			}
		})
	}
}

func classCounts(m map[string]int) string {
	var b strings.Builder
	for _, c := range mtgish.Classes {
		if m[c] > 0 {
			if b.Len() > 0 {
				b.WriteString(" ")
			}
			b.WriteString(c + "=" + strconv.Itoa(m[c]))
		}
	}
	return "(" + b.String() + ")"
}
