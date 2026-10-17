package shape

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
)

// TestXMageManaAbilityOnStackRuling pins the shape ruling for Bolg's Company
// (activate#0.0): its "{T}, Sacrifice another Goblin: Add {B}{R}." is a mana
// ability (CR 605.1a) that resolves without the stack (CR 605.3b), so gorge's
// pool of BR is right; XMage models it as a stack-using activated ability and
// leaves its pool empty.
func TestXMageManaAbilityOnStackRuling(t *testing.T) {
	const rulingID = "xmage-scripts-mana-ability-on-stack"
	const wantKey = "activate | activate | p0.pool | gorge=BR xmage="

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

	row := compliance.VerdictRow{
		Card:     "Bolg's Company",
		Template: "activate#0.0",
		Status:   compliance.StatusDiverge,
		Detail:   `step 0 (activate) p0.pool: gorge "BR", xmage ""`,
	}
	s, ok := Of(row)
	if !ok {
		t.Fatalf("no shape from detail %q", row.Detail)
	}
	if s.Key() != wantKey {
		t.Fatalf("shape %q, want %q", s.Key(), wantKey)
	}
	m, overlap, changed := Classify(&row, rs, "-")
	if m == nil || !changed {
		t.Fatalf("ruling did not apply: %+v", row)
	}
	if len(overlap) != 0 {
		t.Fatalf("overlapping rulings %v", overlap)
	}
	if row.Status != compliance.StatusXMageWrong || row.RulingID != rulingID {
		t.Fatalf("status %q ruling %q, want xmage_wrong/%s", row.Status, row.RulingID, rulingID)
	}
	for _, cr := range []string{"CR 605.1a", "605.3b"} {
		if !strings.Contains(row.Ruling, cr) {
			t.Errorf("ruling text %q does not cite %s", row.Ruling, cr)
		}
	}
}
