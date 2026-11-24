package oraclegen

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestUnlessPaidEnergyIsYes pins the Lathnu Hellion find: its beginning-of-end-
// step trigger is SacrificeSourceUnlessPaysEffect(new PayEnergyCost(2)), which
// asks XMage's chooseUse "Pay {E}{E}?" for the energy cost. gorge pays (CR
// 118.12), so the one scripted answer must be the boolean yes; a numeric mode
// answer leaves chooseUse to TestPlayer's AI, which declines and keeps the
// energy.
func TestUnlessPaidEnergyIsYes(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	raw, err := os.ReadFile("../../../oracle/xengine/xprep/testdata/xproof/lathnu-hellion.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := rules.RunOracleScenarioJSON(reg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("gorge fails: %v", res.Fails)
	}
	var sc Scenario
	if err := json.Unmarshal(raw, &sc); err != nil {
		t.Fatal(err)
	}

	// Precondition: the scenario really poses Lathnu Hellion's paid
	// unless-pay decision, and gorge paid it. Exactly one such decision.
	step := -1
	for _, d := range res.Decisions {
		if d.Seat != 0 || d.Kind != "mode" || d.Resume != "unless_pay" {
			continue
		}
		if d.Options != 2 || len(d.Picks) != 1 || d.Picks[0] != "Pay the cost" {
			continue
		}
		if !pickRefsContain(d.PickRefs, "p0:Lathnu Hellion") {
			continue
		}
		if step >= 0 {
			t.Fatalf("more than one paid Lathnu unless-pay decision: steps %d and %d", step, d.Step)
		}
		step = d.Step
	}
	if step < 0 {
		t.Fatalf("scenario posed no paid Lathnu Hellion unless-pay decision: %+v", res.Decisions)
	}

	got := XAnswersForScenario(reg, res, sc, nil, nil)
	want := []XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}}
	if step >= len(got) || !reflect.DeepEqual(got[step], want) {
		t.Fatalf("xmage_answers[%d] = %+v, want %+v", step, got[step], want)
	}

	// The payment must actually resolve: p0 has both energy counters before
	// the trigger, spends them at the decision step, and keeps Lathnu Hellion.
	if step+1 >= len(res.Snapshots) {
		t.Fatalf("decision step %d has no after-snapshot (%d snapshots)", step, len(res.Snapshots))
	}
	before, after := res.Snapshots[step], res.Snapshots[step+1]
	if e, ok := seatEnergy(before, 0); !ok || e != 2 {
		t.Fatalf("p0 energy before the end-step trigger = %d (found %v), want 2", e, ok)
	}
	if e, ok := seatEnergy(after, 0); !ok || e != 0 {
		t.Fatalf("p0 energy after the end-step trigger = %d (found %v), want 0", e, ok)
	}
	last := res.Snapshots[len(res.Snapshots)-1]
	for _, p := range last.Permanents {
		if p.Name == "Lathnu Hellion" && p.Controller == 0 {
			return
		}
	}
	t.Fatalf("Lathnu Hellion not on the battlefield after paying: %+v", last.Permanents)
}

func seatEnergy(s rules.OracleSnapshot, seat int) (int32, bool) {
	for _, p := range s.Players {
		if p.Seat == seat {
			return p.Counters["ENERGY"], true
		}
	}
	return 0, false
}

func pickRefsContain(refs []string, want string) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}
