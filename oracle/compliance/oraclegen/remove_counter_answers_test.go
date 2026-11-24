package oraclegen

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestXAnswersRemoveCounterMultiAmountMabel replays the xproof Mabel find
// (testdata lives with the xproof windows, xprep/testdata/xproof) through
// gorge's own runner and scripts it for XMage. The two recorded counter-kind
// picks must become the one X=2 amount the loyalty message reads, not two raw
// LOYALTY choices (which TestPlayer rejects with "Missing choice in multi
// amount").
func TestXAnswersRemoveCounterMultiAmountMabel(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	raw, err := os.ReadFile("../../xengine/xprep/testdata/xproof/mabel-bitter-recluse.json")
	if err != nil {
		t.Fatal(err)
	}
	var sc Scenario
	if err := json.Unmarshal(raw, &sc); err != nil {
		t.Fatal(err)
	}
	res, err := rules.RunOracleScenarioJSON(reg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("gorge fails: %v", res.Fails)
	}
	// Precondition: the find really records the per-counter run this fix
	// folds (two LOYALTY picks on the resolve step, no Done).
	var counterPicks int
	for _, d := range res.Decisions {
		if d.Step == 3 && removeCounterKindDecision(d) {
			counterPicks += len(d.Picks)
		}
	}
	if counterPicks != 2 {
		t.Fatalf("precondition: %d counter-kind picks at step 3, want 2: %+v", counterPicks, res.Decisions)
	}
	out := XAnswersForScenario(reg, res, sc, nil, nil)
	if len(out) != len(sc.Steps) {
		t.Fatalf("answers cover %d steps, want %d", len(out), len(sc.Steps))
	}
	var amounts []XAnswer
	for _, a := range out[3] {
		if a.Kind == "choice" && a.Value == "LOYALTY" {
			t.Fatalf("raw counter kind still scripted at step 3: %+v", out[3])
		}
		if a.Kind == "amount" {
			amounts = append(amounts, a)
		}
	}
	// Ajani Resolute holds exactly one counter kind (loyalty), so XMage asks
	// one message and the whole removal is that message's amount.
	if want := []XAnswer{{0, "amount", "2"}}; !reflect.DeepEqual(amounts, want) {
		t.Fatalf("amount answers = %+v, want %+v (step answers %+v)", amounts, want, out[3])
	}
}

// TestXAnswersRemoveCounterMultiAmountZeros pins the multi-kind shape: the
// target holds +1/+1 and charge, only two +1/+1 are removed, and XMage asks
// one message per kind in HashMap order (charge, then +1/+1). The unpicked
// kind gets an explicit zero, and the picked one its count.
func TestXAnswersRemoveCounterMultiAmountZeros(t *testing.T) {
	run := []rules.OracleDecision{
		{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "counter_kind", Options: 3, Min: 1, Max: 1,
			Picks: []string{"P1P1"}, PickIdx: []int{0}, PickRefs: []string{"P1P1"}, PickKinds: []string{"counter"}},
		{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "counter_kind", Options: 2, Min: 1, Max: 1,
			Picks: []string{"P1P1"}, PickIdx: []int{0}, PickRefs: []string{"P1P1"}, PickKinds: []string{"counter"}},
	}
	snaps := []rules.OracleSnapshot{
		{Permanents: []rules.OracleSnapPerm{{
			Ref: "p1:Grizzly Bears", Name: "Grizzly Bears",
			Counters: map[string]int32{"P1P1": 2, "CHARGE": 1},
		}}},
		{Permanents: []rules.OracleSnapPerm{{
			Ref: "p1:Grizzly Bears", Name: "Grizzly Bears",
			Counters: map[string]int32{"CHARGE": 1},
		}}},
	}
	out := XAnswersWithSnapshots(run, 1, nil, snaps)
	want := []XAnswer{{0, "amount", "0"}, {0, "amount", "2"}}
	if len(out) != 1 || !reflect.DeepEqual(out[0], want) {
		t.Fatalf("multi-kind answers = %+v, want %+v", out, want)
	}
}

// TestReconstructCounterKindOrder pins the engine-order recovery the HashMap
// tie-break reads: the first pick's index places its kind among all kinds,
// and each later pick's index is read against the kinds still present.
func TestRemoveCounterKindOrder(t *testing.T) {
	run := []rules.OracleDecision{
		{Step: 0, Picks: []string{"P1P1"}, PickIdx: []int{0}},
		{Step: 0, Picks: []string{"CHARGE"}, PickIdx: []int{0}},
	}
	got, ok := reconstructCounterKindOrder([]string{"CHARGE", "P1P1"}, run)
	if !ok || !reflect.DeepEqual(got, []string{"P1P1", "CHARGE"}) {
		t.Fatalf("order = %v, %v; want [P1P1 CHARGE], true", got, ok)
	}
}

// TestDisplayCounterName pins the gorge-kind -> XMage counter-name mapping the
// HashMap order hashes.
func TestRemoveCounterDisplayName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"P1P1", "+1/+1"},
		{"M1M1", "-1/-1"},
		{"P0M1", "-0/-1"},
		{"M1P0", "-1/-0"},
		{"LOYALTY", "loyalty"},
		{"CHARGE", "charge"},
	} {
		if got := displayCounterName(tc.in); got != tc.want {
			t.Errorf("displayCounterName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestXAnswersRemoveCounterMessageOrder pins the HashMap order against a real
// XMage test: Ghost-Spider's "Remove two counters" run (GwenStacyTest) scripts
// Charge, P0P1, P1P1 in that order, which is the bucket order of
// CounterType's names, not the engine's insertion order.
func TestXAnswersRemoveCounterMessageOrder(t *testing.T) {
	got := xmageCounterMapOrder([]string{"P1P1", "P0P1", "CHARGE"})
	want := []string{"CHARGE", "P0P1", "P1P1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counter message order = %v, want %v (XMage's GwenStacyTest order)", got, want)
	}
}
