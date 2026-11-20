package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestBandAnswersScriptJoinThenDecline pins the measured xengine band decision
// shape (xe:SC15:s1000003:1000003:tape step 269): one KChoose per band answer,
// with the accepted member as an object ref and the decline as the option
// label. XMage's Combat.handleBanding asks a chooseUse ("Form a band ...?")
// and then a TargetControlledPermanent for the member. The member selection
// reaches TestPlayer.makeChoose on the CHOICE queue (Target.choose ->
// Player.choose), so the accepted pick needs yes + its ref there, and a decline
// needs no. Scripting the gorge label (the generic choice route) leaves
// chooseUse unanswered and desyncs the rest of the step (CR 702.22c).
func TestBandAnswersScriptJoinThenDecline(t *testing.T) {
	join := rules.OracleDecision{
		Step: 2, Seat: 1, Kind: "choose_n", Options: 3, GorgeKind: "choose", Via: "answer",
		Picks: []string{"Band Copper Gnomes with Timber Wolves"}, PickIdx: []int{1},
		PickRefs: []string{"p1:Copper Gnomes"}, PickRefsInexact: []bool{true},
		OptionRefs:  []string{"p1:Copper Gnomes", "p1:Timber Wolves#2"},
		ObjectPicks: []string{"p1:Copper Gnomes"}, PickKinds: []string{"band"}, Min: 1, Max: 1,
	}
	decline := rules.OracleDecision{
		Step: 2, Seat: 1, Kind: "choose_n", Options: 2, GorgeKind: "choose", Via: "answer",
		Picks: []string{"No more creatures in Timber Wolves's band"}, PickIdx: []int{0},
		PickRefs:   []string{"No more creatures in Timber Wolves's band"},
		OptionRefs: []string{"p1:Timber Wolves#2"},
		PickKinds:  []string{"band"}, Min: 1, Max: 1,
	}
	got := routed(t, []rules.OracleDecision{join, decline}, 3)
	want := []XAnswer{
		{Seat: 1, Kind: "choice", Value: "yes"},
		{Seat: 1, Kind: "choice", Value: "p1:Copper Gnomes"},
		{Seat: 1, Kind: "choice", Value: "no"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("band answers = %#v, want %#v", got, want)
	}
	// The routing is keyed on the engine's pick kind, not the label: an
	// ordinary permanent pick keeps its own target-queue route.
	generic := rules.OracleDecision{
		Step: 2, Seat: 1, Kind: "choose_n", Options: 3, GorgeKind: "choose", Via: "answer",
		Picks: []string{"Copper Gnomes"}, PickIdx: []int{1},
		PickRefs: []string{"p1:Copper Gnomes"}, ObjectPicks: []string{"p1:Copper Gnomes"},
		PickKinds: []string{"permanent"}, Min: 1, Max: 1,
	}
	if got := routed(t, []rules.OracleDecision{generic}, 3); len(got) != 1 || got[0].Kind != "target" {
		t.Fatalf("a permanent pick must keep its own route, got %#v", got)
	}
}
