package gate

import "testing"

// TestCertVerdictClassifiesStaleOnly pins the classification the per-ticket
// gate's stale-only path depends on (ops/scripts/gate_affected.sh reads the
// GORGE-CERT-RESULT line the chunk tests log from CertVerdict): a set whose
// only problems are stale verdicts is "stale-only" (a host XMage replay
// clears it, so the gate must not charge a fix round), any non-stale problem
// makes it "mixed" (a real regression the gate must still fail), and no
// problems is "clean". A regression that is not a stale sha must never read as
// stale-only.
func TestCertVerdictClassifiesStaleOnly(t *testing.T) {
	stale := Problem{Card: "Thundertrap Trainer", Reason: StaleVerdictReason + " (Thundertrap Trainer/cast-resolve/v1); re-run the XMage pass"}
	nonstale := Problem{Card: "Luxurious Locomotive", Reason: "unsupported [count:CrewSize]"}
	expect := Problem{Card: "Taurean Mauler", Reason: "gorge no longer meets the frozen expectation: life (id)"}

	cases := []struct {
		name  string
		probs []Problem
		want  string
	}{
		{"clean", nil, "clean"},
		{"clean-empty", []Problem{}, "clean"},
		{"stale-only", []Problem{stale}, "stale-only"},
		{"stale-only-many", []Problem{stale, stale}, "stale-only"},
		{"non-stale-unsupported", []Problem{nonstale}, "mixed"},
		{"non-stale-expectation", []Problem{expect}, "mixed"},
		{"stale-and-non-stale", []Problem{stale, nonstale}, "mixed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CertVerdict(tc.probs); got != tc.want {
				t.Fatalf("CertVerdict(%v) = %q, want %q", tc.probs, got, tc.want)
			}
		})
	}
	if !Stale(stale) {
		t.Fatal("Stale(stale) = false")
	}
	if Stale(nonstale) || Stale(expect) {
		t.Fatal("Stale classified a non-stale problem as stale")
	}
}
