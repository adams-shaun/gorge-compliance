package shape

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
)

// TestCrewedVehicleChosenTypeRuling pins the ruling for Lifecraft Engine's
// activate#0.0 row: after the crew resolves, gorge orders the layer-4
// chosen-type static after the crew effect that makes the Vehicle a creature
// (CR 613.8 dependency), so the Vehicle is a Dinosaur; XMage orders by
// timestamp and leaves it without the chosen type.
func TestCrewedVehicleChosenTypeRuling(t *testing.T) {
	const rulingID = "crewed-vehicle-takes-chosen-type-after-crew-dependency"
	const wantKey = "activate | resolve | permanents | $CARD types gorge+[dinosaur] xmage+[]"

	rs, err := LoadRulings(filepath.Join("..", "..", "..", RulingDir))
	if err != nil {
		t.Fatal(err)
	}
	row := compliance.VerdictRow{
		Card:     "Lifecraft Engine",
		Template: "activate#0.0",
		Status:   compliance.StatusDiverge,
		Detail:   `step 1 (resolve) permanents: gorge "c0 o0 Lifecraft Engine [artifact creature dinosaur vehicle] {} 4/4", xmage "c0 o0 Lifecraft Engine [artifact creature vehicle] {} 4/4"`,
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
	if !strings.Contains(row.Ruling, "613.8") {
		t.Errorf("ruling text %q does not cite 613.8", row.Ruling)
	}
}
