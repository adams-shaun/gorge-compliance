package compliance

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestVerdictRowXMageDriverJSON pins the serialization tag: the recorded
// driver must round-trip as "xmage_driver", or every committed row is stale
// under a driver-aware plan. A row without a driver keeps its old bytes
// (omitempty).
func TestVerdictRowXMageDriverJSON(t *testing.T) {
	row := VerdictRow{
		Card: "Shock", Template: "cast-resolve", ID: "cast-resolve",
		ScenarioSHA: "s", XMageRef: "ref1", XMageDriver: "0123456789ab",
		Status: StatusAgree,
	}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"xmage_driver":"0123456789ab"`) {
		t.Errorf("row JSON lacks the xmage_driver field: %s", b)
	}
	var got VerdictRow
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.XMageDriver != "0123456789ab" {
		t.Errorf("XMageDriver = %q after round trip, want 0123456789ab", got.XMageDriver)
	}

	legacy := row
	legacy.XMageDriver = ""
	lb, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(lb), `"xmage_driver"`) {
		t.Errorf("driver-less row carries xmage_driver: %s", lb)
	}
}
