package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func orderSnap(p0, p1 []string) rules.OracleSnapshot {
	return rules.OracleSnapshot{Players: []rules.OracleSnapPlayer{
		{Seat: 0, Graveyard: p0},
		{Seat: 1, Graveyard: p1},
	}}
}

func orderItems(steps int) oraclegen.Item {
	ss := make([]oraclegen.Step, steps)
	for i := range ss {
		ss[i] = oraclegen.Step{Op: "pass"}
	}
	return oraclegen.Item{ID: "order", Template: "other", Scenario: oraclegen.Scenario{Steps: ss}}
}

// orderWrathItem is a cast-resolve item that puts two differently named
// creatures into each player's graveyard in the resolving step (Wrath of God):
// the shape the pin exists for, with no selection decision to derive it from.
// The mana is in the cast step so the item does not depend on which lands the
// runner taps.
const orderWrathItem = `{"id":"Wrath of God/order/v1","card":"Wrath of God","template":"cast-resolve","name":"graveyard order","cr":["404.3"],"why":"graveyard order regression","setup":{"p0":{"battlefield":["Plains","Plains","Plains","Plains","Grizzly Bears"],"hand":["Wrath of God"]},"p1":{"battlefield":["Hill Giant","Air Elemental"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Wrath of God","mana":"WWWW"},{"op":"resolve","seat":0}]}`

// TestForgeGraveyardOrderDecisions: a step/seat whose checkpoints show two or
// more differently named cards entering that graveyard between them gets one
// order decision, in the new zone order, and a same-named pair does not (the
// order of identical cards is unobservable). An order already present is kept,
// not duplicated.
func TestForgeGraveyardOrderDecisions(t *testing.T) {
	it := orderItems(3)
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		orderSnap(nil, nil),
		orderSnap([]string{"A"}, nil),
		orderSnap([]string{"A", "C", "A"}, nil),
		orderSnap([]string{"A", "C", "A"}, []string{"B", "B"}),
	}}
	ds := []rules.OracleDecision{{Step: 1, Seat: 0, Kind: "choose_n", Picks: []string{"A", "C"}}}

	got, ends := forgeGraveyardOrderDecisions(it, g, ds)
	if len(got) != 2 {
		t.Fatalf("%d decisions, want the selection plus one order: %+v", len(got), got)
	}
	o := got[1]
	if o.Step != 1 || o.Seat != 0 || o.Kind != "order" || o.GorgeKind != "graveyard_order" ||
		!reflect.DeepEqual(o.Picks, []string{"C", "A"}) ||
		!reflect.DeepEqual(o.PickRefs, []string{"p0:C", "p0:A"}) ||
		o.Min != 2 || o.Max != 2 {
		t.Errorf("order = %+v, want step 1 seat 0 order [C A] p0 refs 2..2", o)
	}
	if !reflect.DeepEqual(got[0], ds[0]) {
		t.Errorf("the selection decision moved: %+v", got[0])
	}
	if want := map[string][]string{"1:0": {"A", "C", "A"}}; !reflect.DeepEqual(ends, want) {
		t.Errorf("graveyard_ends = %v, want the step-end graveyard under 1:0", ends)
	}

	// A pin that already reached the decisions (the runner queued it) is not
	// added twice, but it still carries the step's end graveyard: its ask can
	// offer a transit card, which strict-misses without the ends to classify
	// it by. An order for a single card (or copies of one) is not a pin at all.
	have := append(append([]rules.OracleDecision(nil), ds...), o)
	again, againEnds := forgeGraveyardOrderDecisions(it, g, have)
	if len(again) != len(have) {
		t.Errorf("order decision duplicated: %+v", again)
	}
	if want := map[string][]string{"1:0": {"A", "C", "A"}}; !reflect.DeepEqual(againEnds, want) {
		t.Errorf("graveyard_ends for a runner-produced pin = %v, want the pinned step's end graveyard", againEnds)
	}
}

// TestForgeGraveyardOrderNeedsFullCheckpoints: a failed or truncated gorge run
// leaves the decisions exactly as the runner recorded them.
func TestForgeGraveyardOrderNeedsFullCheckpoints(t *testing.T) {
	it := orderItems(2)
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{orderSnap(nil, nil)}}
	ds := []rules.OracleDecision{{Step: 0, Kind: "target"}}
	if got, ends := forgeGraveyardOrderDecisions(it, g, ds); !reflect.DeepEqual(got, ds) || ends != nil {
		t.Errorf("truncated snapshots changed the decisions: %+v %v", got, ends)
	}
}

// TestForgeGraveyardOrderOnePinPerStep keeps the documented approximation
// visible: gorge's checkpoints cannot tell one simultaneous move from two in
// the same step, so four cards entering one graveyard between two checkpoints
// produce one pin, in the final zone order, and not one pin per event. The
// driver's first ask consumes it; a second ask in the same step still misses.
func TestForgeGraveyardOrderOnePinPerStep(t *testing.T) {
	it := orderItems(1)
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		orderSnap(nil, nil),
		orderSnap([]string{"A", "B", "C", "D"}, nil),
	}}
	got, _ := forgeGraveyardOrderDecisions(it, g, nil)
	if len(got) != 1 {
		t.Fatalf("%d order decisions, want one per step/seat: %+v", len(got), got)
	}
	if !reflect.DeepEqual(got[0].Picks, []string{"A", "B", "C", "D"}) {
		t.Errorf("picks = %v, want the whole step's zone order", got[0].Picks)
	}
}

// TestForgeGraveyardOrderTransitCardIsNotPinned: a card that enters and
// leaves a graveyard between the same two checkpoints is in neither snapshot,
// so no pin can name it (Sorin, Lord of Innistrad's -6: Destroy, then
// ChangeZoneAll back to the battlefield in one resolution). The visible
// arrivals still get their pin, in zone order; the pin derivation must not
// invent one from the step's selection decision, which names the transit card
// too. StepMachine.orderCards keeps Forge's offered order for the unpinned
// ask.
func TestForgeGraveyardOrderTransitCardIsNotPinned(t *testing.T) {
	it := orderItems(1)
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		orderSnap(nil, nil),
		orderSnap(nil, []string{"Stayed A", "Stayed B"}),
	}}
	selection := []rules.OracleDecision{{
		Step: 0, Seat: 1, Kind: "choose_n",
		Picks: []string{"Transit", "Stayed A", "Stayed B"},
	}}
	got, ends := forgeGraveyardOrderDecisions(it, g, selection)
	if len(got) != 2 {
		t.Fatalf("%d decisions, want the selection plus one pin for the cards that stayed: %+v", len(got), got)
	}
	if !reflect.DeepEqual(got[0], selection[0]) {
		t.Errorf("the selection decision changed: %+v", got[0])
	}
	pin := got[1]
	if pin.Step != 0 || pin.Seat != 1 || pin.Kind != "order" || pin.GorgeKind != "graveyard_order" ||
		!reflect.DeepEqual(pin.Picks, []string{"Stayed A", "Stayed B"}) {
		t.Errorf("pin = %+v, want step 0 seat 1 naming only the visible arrivals", pin)
	}
	if want := map[string][]string{"0:1": {"Stayed A", "Stayed B"}}; !reflect.DeepEqual(ends, want) {
		t.Errorf("graveyard_ends = %v, want the step-end graveyard under 0:1", ends)
	}
	for _, pick := range pin.Picks {
		if pick == "Transit" {
			t.Errorf("pin names the pass-through card: %+v", pin)
		}
	}
}

// TestForgeBuildRequestCarriesGraveyardOrder: the export wires the pin into
// the request the driver reads, so orderCards can consume it. The same builder
// without the opt-in (the follower path) must not carry it: a phase/Manabrew
// queue has no consumer for the role and an unasked pin is a leftover.
func TestForgeBuildRequestCarriesGraveyardOrder(t *testing.T) {
	it := orderItems(1)
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		orderSnap(nil, nil),
		orderSnap(nil, []string{"Akal Pakal, First Among Equals", "Poison Dart Frog"}),
	}}
	rq := forgeBuildRequest(nil, it, g, nil, true)
	if len(rq.GorgeDecisions) != 1 {
		t.Fatalf("%d decisions, want one graveyard order: %+v", len(rq.GorgeDecisions), rq.GorgeDecisions)
	}
	d := rq.GorgeDecisions[0]
	if d.Kind != "order" || d.GorgeKind != "graveyard_order" || d.Step != 0 || d.Seat != 1 ||
		!reflect.DeepEqual(d.Picks, []string{"Akal Pakal, First Among Equals", "Poison Dart Frog"}) {
		t.Errorf("request order = %+v", d)
	}
	if want := map[string][]string{"0:1": {"Akal Pakal, First Among Equals", "Poison Dart Frog"}}; !reflect.DeepEqual(rq.GraveyardEnds, want) {
		t.Errorf("request graveyard_ends = %v, want the step-end graveyard under 0:1", rq.GraveyardEnds)
	}

	// Without the opt-in (the follower path) the role is stripped, even one
	// the runner itself produced, and the other decisions stay.
	have := []rules.OracleDecision{
		{Step: 0, Seat: 0, Kind: "order", GorgeKind: "graveyard_order", Picks: []string{"X", "Y"}},
		{Step: 0, Seat: 0, Kind: "target"},
	}
	if got := forgeBuildRequest(nil, it, g, have, false); len(got.GorgeDecisions) != 1 || got.GorgeDecisions[0].Kind != "target" {
		t.Errorf("the builder without the opt-in kept the role: %+v", got.GorgeDecisions)
	}
}

// TestForgeGraveyardOrderReachesForgeExportOnly exports a real multi-card
// graveyard move through both commands: forge-export carries the pin at the
// resolving step, so the driver can answer Forge's ask, and follower-export
// writes the same item without the role, because the follower queues have no
// consumer for it (an unasked decision would become an unconsumed leftover
// and classify a clean row as decision_shape).
func TestForgeGraveyardOrderReachesForgeExportOnly(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	dir := t.TempDir()
	scen := filepath.Join(dir, "items.jsonl")
	if err := os.WriteFile(scen, []byte(orderWrathItem+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	forgeOut := filepath.Join(dir, "forge-req.jsonl")
	if _, err := forgeExport(reg, scen, forgeOut); err != nil {
		t.Fatal(err)
	}
	forgeBytes, err := os.ReadFile(forgeOut)
	if err != nil {
		t.Fatal(err)
	}
	reqs := decodeRequests(t, forgeBytes)
	if len(reqs) != 1 {
		t.Fatalf("%d forge requests, want 1", len(reqs))
	}
	want := map[[2]int][]string{
		{1, 0}: {"Grizzly Bears", "Wrath of God"},
		{1, 1}: {"Hill Giant", "Air Elemental"},
	}
	pins := 0
	for _, d := range reqs[0].GorgeDecisions {
		if d.GorgeKind != "graveyard_order" {
			continue
		}
		pins++
		if got, ok := want[[2]int{d.Step, d.Seat}]; !ok || !reflect.DeepEqual(d.Picks, got) {
			t.Errorf("pin step %d seat %d picks %v, want one of %v", d.Step, d.Seat, d.Picks, want)
		}
	}
	if pins != 2 {
		t.Errorf("%d graveyard-order pins, want 2 (one per seat): %+v", pins, reqs[0].GorgeDecisions)
	}

	fl, runner := fakeFollower(t)
	followerOut := filepath.Join(dir, "follower-req.jsonl")
	if _, err := followerExport(reg, fl, runner, scen, followerOut); err != nil {
		t.Fatal(err)
	}
	followerBytes, err := os.ReadFile(followerOut)
	if err != nil {
		t.Fatal(err)
	}
	lines := splitLines(followerBytes)
	if len(lines) != 1 {
		t.Fatalf("%d follower requests, want 1", len(lines))
	}
	var rq followerRequest
	if err := json.Unmarshal(lines[0], &rq); err != nil {
		t.Fatal(err)
	}
	for _, d := range rq.GorgeDecisions {
		if d.GorgeKind == "graveyard_order" {
			t.Errorf("follower-export wrote the Forge-only role: %+v", d)
		}
	}
}
