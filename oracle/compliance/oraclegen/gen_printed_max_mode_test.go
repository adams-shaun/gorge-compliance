package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// Sentinel of Lost Lore is "choose one or more" (CharmNum 3). gorge clamps a
// mode decision's Max to the modes with a legal target (CR 700.2a), so with
// only mode 3 eligible the decision is Min 1, Max 1 -- but XMage's
// Modes.choose keeps asking until the PRINTED maximum, so the one pick must be
// closed by a mode skip.
func TestModeSkipFollowsPrintedMaxWhenGorgeClampsMax(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Sentinel of Lost Lore")
	if !ok {
		t.Fatal("Sentinel of Lost Lore missing from the corpus")
	}
	modes := ModeNumbers(card.Faces[0])
	var third string
	for label, n := range modes {
		if n == 3 {
			third = label
		}
	}
	if third == "" {
		t.Fatalf("no third mode label in %v", modes)
	}
	d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "mode", GorgeKind: "modes", Resume: "modes",
		Options: 1, Min: 1, Max: 1, Picks: []string{third}, PickIdx: []int{0}, PickKinds: []string{"mode"},
		PickRefs: []string{"p0:Sentinel of Lost Lore"}}
	got := xanswers([]rules.OracleDecision{d}, 2, modes, nil)
	want := [][]XAnswer{nil, {{0, "mode", "3"}, {0, "mode", "[mode_skip]"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
	// A card whose printed maximum is reached closes nothing.
	d.Max = 3
	d.Picks, d.PickIdx = []string{third, third, third}, []int{0, 0, 0}
	for _, a := range xanswers([]rules.OracleDecision{d}, 2, modes, nil)[1] {
		if a.Value == "[mode_skip]" {
			t.Fatalf("a full pick set must not be skipped: %#v", a)
		}
	}
}
