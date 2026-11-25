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
		{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "counter_kind", Options: 3, Min: 1, Max: 1,
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

// TestXAnswersRemoveCounterMultiAmountDoneOnly pins the early-Done shape: the
// controller removes nothing, but XMage still asks the multi-amount, so every
// kind message needs an explicit X=0 (the target decision still identifies
// the carrier even though the checkpoint delta is empty).
func TestXAnswersRemoveCounterMultiAmountDoneOnly(t *testing.T) {
	run := []rules.OracleDecision{
		{Step: 0, Seat: 0, Kind: "target", GorgeKind: "target",
			Picks: []string{"p1:Ajani Resolute"}, PickRefs: []string{"p1:Ajani Resolute"}, PickKinds: []string{"card"}},
		{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "counter_kind", Options: 2, Min: 1, Max: 1,
			Picks: []string{"Done"}, PickIdx: []int{1}, PickRefs: []string{"Done"}, PickKinds: []string{"remove_counter_done"}},
	}
	snaps := []rules.OracleSnapshot{
		{Permanents: []rules.OracleSnapPerm{{Ref: "p1:Ajani Resolute", Name: "Ajani Resolute",
			Counters: map[string]int32{"LOYALTY": 2}}}},
		{Permanents: []rules.OracleSnapPerm{{Ref: "p1:Ajani Resolute", Name: "Ajani Resolute",
			Counters: map[string]int32{"LOYALTY": 2}}}},
	}
	out := XAnswersWithSnapshots(run, 1, nil, snaps)
	var amounts []XAnswer
	for _, a := range out[0] {
		if a.Kind == "amount" {
			amounts = append(amounts, a)
		}
	}
	if want := []XAnswer{{0, "amount", "0"}}; !reflect.DeepEqual(amounts, want) {
		t.Fatalf("amount answers = %+v, want %+v (step answers %+v)", amounts, want, out[0])
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

// TestXAnswersMultiAmountPutCounterKindStaysChoice pins that a PutCounter
// "choose a kind to put" pick is not folded into a multi-amount: it shares the
// engine's "counter_kind" ResumeKind but XMage asks a Choice, so the recorded
// kind must stay on the generic choice route. Both a kind the carrier lacks
// (the up-to lift cannot offer it) and an existing kind (shape-coincident, but
// the carrier gains a counter) are covered.
func TestXAnswersMultiAmountPutCounterKindStaysChoice(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pick   string
		idx    int
		before map[string]int32
		after  map[string]int32
	}{
		{"new kind", "FLYING", 1, map[string]int32{"P1P1": 1}, map[string]int32{"P1P1": 1, "FLYING": 1}},
		{"existing kind", "P1P1", 0, map[string]int32{"P1P1": 1}, map[string]int32{"P1P1": 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := []rules.OracleDecision{
				{Step: 0, Seat: 0, Kind: "target", GorgeKind: "target",
					Picks: []string{"p1:Grizzly Bears"}, PickRefs: []string{"p1:Grizzly Bears"}, PickKinds: []string{"card"}},
				{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "counter_kind", Options: 2, Min: 1, Max: 1,
					Picks: []string{tc.pick}, PickIdx: []int{tc.idx}, PickRefs: []string{tc.pick}, PickKinds: []string{"counter"}},
			}
			snaps := []rules.OracleSnapshot{
				{Permanents: []rules.OracleSnapPerm{{Ref: "p1:Grizzly Bears", Name: "Grizzly Bears", Counters: tc.before}}},
				{Permanents: []rules.OracleSnapPerm{{Ref: "p1:Grizzly Bears", Name: "Grizzly Bears", Counters: tc.after}}},
			}
			out := XAnswersWithSnapshots(run, 1, nil, snaps)
			assertNoCounterAmount(t, out)
			assertChoice(t, out, tc.pick)
		})
	}
}

// TestXAnswersMultiAmountExistingCounterKindStaysChoice pins the ExistingCounter arm
// ("choose a kind of counter" over the kinds the recipient already carries):
// the carrier gains counters, so it is not a RemoveCounter run even when the
// option count could be read as an up-to ask.
func TestXAnswersMultiAmountExistingCounterKindStaysChoice(t *testing.T) {
	run := []rules.OracleDecision{
		{Step: 0, Seat: 0, Kind: "target", GorgeKind: "target",
			Picks: []string{"p1:Grizzly Bears"}, PickRefs: []string{"p1:Grizzly Bears"}, PickKinds: []string{"card"}},
		{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "counter_kind", Options: 2, Min: 1, Max: 1,
			Picks: []string{"CHARGE"}, PickIdx: []int{0}, PickRefs: []string{"CHARGE"}, PickKinds: []string{"counter"}},
	}
	snaps := []rules.OracleSnapshot{
		{Permanents: []rules.OracleSnapPerm{{Ref: "p1:Grizzly Bears", Name: "Grizzly Bears",
			Counters: map[string]int32{"P1P1": 1, "CHARGE": 1}}}},
		{Permanents: []rules.OracleSnapPerm{{Ref: "p1:Grizzly Bears", Name: "Grizzly Bears",
			Counters: map[string]int32{"P1P1": 1, "CHARGE": 2}}}},
	}
	out := XAnswersWithSnapshots(run, 1, nil, snaps)
	assertNoCounterAmount(t, out)
	assertChoice(t, out, "CHARGE")
}

// TestXAnswersFixedRemoveCounterKindStaysChoice pins fixed-count RemoveCounter
// Any on a multi-kind carrier: the engine asks (len(kinds) > 1) but XMage's
// RemoveCounterTargetEffect asks a Choice, so only the up-to form folds.
func TestXAnswersFixedRemoveCounterKindStaysChoice(t *testing.T) {
	run := []rules.OracleDecision{
		{Step: 0, Seat: 0, Kind: "target", GorgeKind: "target",
			Picks: []string{"p1:Grizzly Bears"}, PickRefs: []string{"p1:Grizzly Bears"}, PickKinds: []string{"card"}},
		{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "counter_kind", Options: 2, Min: 1, Max: 1,
			Picks: []string{"CHARGE"}, PickIdx: []int{0}, PickRefs: []string{"CHARGE"}, PickKinds: []string{"counter"}},
	}
	snaps := []rules.OracleSnapshot{
		{Permanents: []rules.OracleSnapPerm{{Ref: "p1:Grizzly Bears", Name: "Grizzly Bears",
			Counters: map[string]int32{"P1P1": 1, "CHARGE": 1}}}},
		{Permanents: []rules.OracleSnapPerm{{Ref: "p1:Grizzly Bears", Name: "Grizzly Bears",
			Counters: map[string]int32{"P1P1": 1}}}},
	}
	out := XAnswersWithSnapshots(run, 1, nil, snaps)
	assertNoCounterAmount(t, out)
	assertChoice(t, out, "CHARGE")
}

func assertNoCounterAmount(t *testing.T, out [][]XAnswer) {
	t.Helper()
	for _, step := range out {
		for _, a := range step {
			if a.Kind == "amount" {
				t.Fatalf("counter_kind ask folded into an amount answer: %+v", step)
			}
		}
	}
}

func assertChoice(t *testing.T, out [][]XAnswer, want string) {
	t.Helper()
	for _, step := range out {
		for _, a := range step {
			if a.Kind == "choice" && a.Value == want {
				return
			}
		}
	}
	t.Fatalf("no %q choice answer in %+v", want, out)
}

// TestRemoveCounterCostMultiAmount pins a "Remove a counter from {this}"
// ACTIVATION COST, the other XMage multi-amount. XMage's
// RemoveCountersSourceCost always messages every counter kind on the source
// (even one), while gorge picks a lone kind unasked, so the scripted answers
// have to come from the source's checkpoint delta, not from a decision.
// Loch Mare is the measured card (xproof xs:XP:spawning-kraken:cast:s2).
func TestRemoveCounterCostMultiAmount(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	idx := 0
	for _, tc := range []struct {
		name   string
		before map[string]int32
		after  map[string]int32
		run    []rules.OracleDecision
		want   []XAnswer
	}{
		{
			// Loch Mare has entered with three -1/-1 counters; the cost
			// removes one, which XMage still asks as its own multi-amount
			// message.
			name:   "one kind",
			before: map[string]int32{"M1M1": 3},
			after:  map[string]int32{"M1M1": 2},
			want:   []XAnswer{{0, "amount", "1"}},
		},
		{
			// The second ability ("Remove two counters") has the same single
			// message; the delta, not the text, supplies the amount.
			name:   "one kind two counters",
			before: map[string]int32{"M1M1": 3},
			after:  map[string]int32{"M1M1": 1},
			want:   []XAnswer{{0, "amount", "2"}},
		},
		{
			// A second counter kind present adds a message even though it
			// removes nothing (X=0). -1/-1 hashes to HashMap bucket 14 and
			// charge to bucket 3, so XMage messages charge first; the
			// recorded cost pick recovers the engine's counter order for any
			// same-bucket tie.
			name:   "two kinds",
			before: map[string]int32{"M1M1": 2, "CHARGE": 1},
			after:  map[string]int32{"M1M1": 1, "CHARGE": 1},
			run: []rules.OracleDecision{{
				Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "counter_kind",
				Options: 2, Min: 1, Max: 1,
				Picks: []string{"M1M1"}, PickIdx: []int{1}, PickRefs: []string{"M1M1"}, PickKinds: []string{"counter"},
			}},
			want: []XAnswer{{0, "amount", "0"}, {0, "amount", "1"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := Scenario{Steps: []Step{{
				Op: "activate", Seat: 0, Card: "p0:Loch Mare", AbilityIndex: &idx,
			}}}
			res := rules.OracleResult{
				Decisions: tc.run,
				Snapshots: []rules.OracleSnapshot{
					{Permanents: []rules.OracleSnapPerm{{
						Ref: "p0:Loch Mare", Name: "Loch Mare", Controller: 0, Counters: tc.before,
					}}},
					{Permanents: []rules.OracleSnapPerm{{
						Ref: "p0:Loch Mare", Name: "Loch Mare", Controller: 0, Counters: tc.after,
					}}},
				},
			}
			out := XAnswersForScenario(reg, res, sc, nil, nil)
			if len(out) != 1 {
				t.Fatalf("answers cover %d steps, want 1", len(out))
			}
			var amounts []XAnswer
			for _, a := range out[0] {
				if a.Kind == "amount" {
					amounts = append(amounts, a)
				}
				if a.Kind == "choice" && canonicalCounterKind(a.Value) == "M1M1" {
					t.Fatalf("raw counter kind still scripted at step 0: %+v", out[0])
				}
			}
			if !reflect.DeepEqual(amounts, tc.want) {
				t.Fatalf("amount answers = %+v, want %+v (step answers %+v)", amounts, tc.want, out[0])
			}
		})
	}
}
