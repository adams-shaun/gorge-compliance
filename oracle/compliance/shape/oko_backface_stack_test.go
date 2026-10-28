package shape

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
)

// TestOkoBackFaceStackSourceRuling pins the gorge_wrong ruling for Oko,
// Lorwyn Liege activate#1.1 / activate#1.2: the -3 and -6 abilities take Oko
// to 0 loyalty, so it dies (CR 704.5i) before the step-1 checkpoint, and
// gorge's snapshot names the stack source by the front-face setup ref
// (CR 712.8a) instead of the back-face last known information (CR 113.7a,
// 712.8e). activate#1.0 (-1, Oko survives) is the negative control.
func TestOkoBackFaceStackSourceRuling(t *testing.T) {
	const rulingID = "oko-backface-stack-last-known-information"
	const wantKey = "activate | activate | stack | gorge=[ability:$CARD@p0] xmage=[ability:Oko, Shadowmoor Scion@p0]"
	const front = "[ability:Oko, Lorwyn Liege@p0]"
	const back = "[ability:Oko, Shadowmoor Scion@p0]"

	rs, err := LoadRulings(filepath.Join("..", "..", "..", RulingDir))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rs {
		if r.ID == rulingID {
			found = true
		}
	}
	if !found {
		t.Fatalf("ruling %s not loaded from %s", rulingID, RulingDir)
	}

	row := func(tmpl, gorge string) compliance.VerdictRow {
		return compliance.VerdictRow{
			Card:     "Oko, Lorwyn Liege",
			Template: tmpl,
			Status:   compliance.StatusDiverge,
			Detail:   fmt.Sprintf("step 1 (activate) stack: gorge %q, xmage %q", gorge, back),
		}
	}

	for _, tmpl := range []string{"activate#1.1", "activate#1.2"} {
		r := row(tmpl, front)
		s, ok := Of(r)
		if !ok {
			t.Fatalf("%s: no shape from detail %q", tmpl, r.Detail)
		}
		if s.Key() != wantKey {
			t.Fatalf("%s: shape %q, want %q", tmpl, s.Key(), wantKey)
		}
		m, overlap, changed := Classify(&r, rs, "-")
		if m == nil || !changed {
			t.Fatalf("%s: ruling did not apply: %+v", tmpl, r)
		}
		if len(overlap) != 0 {
			t.Fatalf("%s: overlapping rulings %v", tmpl, overlap)
		}
		if r.Status != compliance.StatusGorgeWrong || r.RulingID != rulingID {
			t.Fatalf("%s: status %q ruling %q, want gorge_wrong/%s", tmpl, r.Status, r.RulingID, rulingID)
		}
		for _, cr := range []string{"113.7a", "712.8e"} {
			if !strings.Contains(r.Ruling, cr) {
				t.Errorf("%s: ruling text %q does not cite CR %s", tmpl, r.Ruling, cr)
			}
		}
	}

	// Negative control: activate#1.0 keeps Oko on the battlefield and both
	// sides agree on the back-face name, so the ruling must not match.
	r := row("activate#1.0", back)
	if s, ok := Of(r); ok && s.Key() == wantKey {
		t.Fatalf("activate#1.0: shape %q must not equal the ruling shape", s.Key())
	}
	if m, _, changed := Classify(&r, rs, "-"); m != nil && m.ID == rulingID || changed && r.RulingID == rulingID {
		t.Fatalf("activate#1.0: ruling matched the control row: %+v", r)
	}
}
