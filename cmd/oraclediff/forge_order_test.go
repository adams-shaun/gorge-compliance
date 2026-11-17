package main

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
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

// TestForgeGraveyardOrderDecisions: a step/seat whose checkpoints show two or
// more differently named cards entering that graveyard at once gets one order
// decision, in the new zone order, and a same-named pair does not (the order
// of identical cards is unobservable). An order already present is kept, not
// duplicated.
func TestForgeGraveyardOrderDecisions(t *testing.T) {
	it := orderItems(3)
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		orderSnap(nil, nil),
		orderSnap([]string{"A"}, nil),
		orderSnap([]string{"A", "C", "A"}, nil),
		orderSnap([]string{"A", "C", "A"}, []string{"B", "B"}),
	}}
	ds := []rules.OracleDecision{{Step: 1, Seat: 0, Kind: "choose_n", Picks: []string{"A", "C"}}}

	got := forgeGraveyardOrderDecisions(it, g, ds)
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

	// A pin that already reached the decisions is not added twice, and an
	// order for a single card (or copies of one) is not a pin at all.
	have := append(append([]rules.OracleDecision(nil), ds...), o)
	if again := forgeGraveyardOrderDecisions(it, g, have); len(again) != len(have) {
		t.Errorf("order decision duplicated: %+v", again)
	}
}

// TestForgeGraveyardOrderNeedsFullCheckpoints: a failed or truncated gorge run
// leaves the decisions exactly as the runner recorded them.
func TestForgeGraveyardOrderNeedsFullCheckpoints(t *testing.T) {
	it := orderItems(2)
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{orderSnap(nil, nil)}}
	ds := []rules.OracleDecision{{Step: 0, Kind: "target"}}
	if got := forgeGraveyardOrderDecisions(it, g, ds); !reflect.DeepEqual(got, ds) {
		t.Errorf("truncated snapshots changed the decisions: %+v", got)
	}
}

// TestForgeBuildRequestCarriesGraveyardOrder: the export wires the pin into
// the request the driver reads, so orderCards can consume it.
func TestForgeBuildRequestCarriesGraveyardOrder(t *testing.T) {
	it := orderItems(1)
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		orderSnap(nil, nil),
		orderSnap(nil, []string{"Akal Pakal", "First Among Equals", "Poison Dart Frog"}),
	}}
	rq := forgeBuildRequest(nil, it, g, nil)
	if len(rq.GorgeDecisions) != 1 {
		t.Fatalf("%d decisions, want one graveyard order: %+v", len(rq.GorgeDecisions), rq.GorgeDecisions)
	}
	d := rq.GorgeDecisions[0]
	if d.Kind != "order" || d.GorgeKind != "graveyard_order" || d.Step != 0 || d.Seat != 1 ||
		!reflect.DeepEqual(d.Picks, []string{"Akal Pakal", "First Among Equals", "Poison Dart Frog"}) {
		t.Errorf("request order = %+v", d)
	}
}
