package adopt

import (
	"fmt"
	"testing"
)

// logRecorder captures the Logf lines of the embedded TB so the emitted
// GORGE-CERT-RESULT line itself can be asserted.
type logRecorder struct {
	testing.TB
	lines []string
}

func (r *logRecorder) Name() string { return "TestLevelBRatchetChunk00" }
func (r *logRecorder) Helper()      {}
func (r *logRecorder) Logf(format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

// TestLogCertResultEmitsGateLine pins the exact line the merge gate's
// stale-only classifier reads; the level-A and level-B ratchet tests both emit
// it through logCertResult, so dropping the call or changing the format is
// caught here rather than silently disabling the gate's stale-only signal.
func TestLogCertResultEmitsGateLine(t *testing.T) {
	stale := []SetStatus{{Set: "BLB", Outstanding: 1, Buckets: map[string]int{BucketStale: 1}}}
	growth := "BLB: 1 outstanding at level B, ratchet 0 -- per-set outstanding only shrinks (oraclediff status -set BLB -level B)"
	for _, c := range []struct {
		name  string
		fails []string
		want  string
	}{
		{"stale", []string{growth}, "GORGE-CERT-RESULT TestLevelBRatchetChunk00 stale-only"},
		{"clean", nil, "GORGE-CERT-RESULT TestLevelBRatchetChunk00 clean"},
	} {
		rec := &logRecorder{}
		logCertResult(rec, stale, c.fails)
		if len(rec.lines) != 1 || rec.lines[0] != c.want {
			t.Errorf("%s: got %q, want exactly [%q]", c.name, rec.lines, c.want)
		}
	}
}
