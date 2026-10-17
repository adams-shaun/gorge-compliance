package adopt

import "testing"

// TestRatchetVerdictClassifiesOnlyStaleGrowth pins the classification the
// per-ticket gate's stale-only path depends on for TestCertificationRatchet
// (ops/scripts/gate_affected.sh reads the GORGE-CERT-RESULT line from
// ratchetVerdict): only a per-set outstanding GROWTH whose set is entirely
// stale verdicts is "stale-only" (a moved scenario sha a host XMage replay
// clears, not a fix round). A structural ratchet fail -- a set no longer
// committed, a declared level that shrank, a committed set missing from the
// ratchet, a floor not recorded -- must read "mixed" even when every measured
// set is stale, and a growth on a set with any non-stale problem must too.
func TestRatchetVerdictClassifiesOnlyStaleGrowth(t *testing.T) {
	staleSet := SetStatus{Set: "BLB", Outstanding: 1, Buckets: map[string]int{BucketStale: 1}}
	realSet := SetStatus{Set: "OTJ", Outstanding: 1, Buckets: map[string]int{BucketUnsupported: 1}}
	mixedSet := SetStatus{Set: "FDN", Outstanding: 2, Buckets: map[string]int{BucketStale: 1, BucketExpectation: 1}}
	measured := []SetStatus{staleSet, realSet, mixedSet}

	growthStale := "BLB: 1 outstanding, ratchet 0 -- per-set outstanding only shrinks (oraclediff status -set BLB)"
	growthReal := "OTJ: 1 outstanding, ratchet 0 -- per-set outstanding only shrinks (oraclediff status -set OTJ)"
	growthMixed := "FDN: 2 outstanding, ratchet 0 -- per-set outstanding only shrinks (oraclediff status -set FDN)"
	growthUnknown := "XXX: 1 outstanding, ratchet 0 -- per-set outstanding only shrinks (oraclediff status -set XXX)"
	notCommitted := "BLB: in the ratchet but no longer a committed set"
	levelShrank := "BLB: was declared at B, now \"A\" -- a declared set or level only grows"
	missing := "BLB: a committed set missing from oracle/compliance/ratchet.json (oraclediff status -all -write-ratchet)"

	cases := []struct {
		name  string
		fails []string
		want  string
	}{
		{"clean", nil, "clean"},
		{"stale-growth", []string{growthStale}, "stale-only"},
		{"stale-growth-many", []string{growthStale, growthStale}, "stale-only"},
		{"growth-on-real", []string{growthReal}, "mixed"},
		{"growth-on-mixed", []string{growthMixed}, "mixed"},
		{"growth-on-unmeasured-set", []string{growthUnknown}, "mixed"},
		{"structural-not-committed", []string{notCommitted}, "mixed"},
		{"structural-level-shrank", []string{levelShrank}, "mixed"},
		{"structural-missing", []string{missing}, "mixed"},
		{"stale-growth-plus-structural", []string{growthStale, levelShrank}, "mixed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ratchetVerdict(measured, tc.fails); got != tc.want {
				t.Fatalf("ratchetVerdict(%v) = %q, want %q", tc.fails, got, tc.want)
			}
		})
	}
}
