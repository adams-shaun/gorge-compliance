package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// A setup permanent's as-enters card-name ask (Skyseer's Chariot, pick kind
// "name") is queued before setup like the colour and type asks: XMage poses
// its name dialog while placing the seeded permanent, and without the
// scripted name it consumed the step's crew pick ("Colossal Dreadmaw") and
// the Crew activation then found no tap answer. The gameplay answer keeps its
// place after it.
func TestSetupNameXAnswersAreQueuedBeforeSetup(t *testing.T) {
	setup := rules.OracleDecision{
		Step: -1, Seat: 0, Kind: "choose_n", Resume: "etb", Options: 3,
		Picks: []string{"Colossal Dreadmaw"}, PickKinds: []string{"name"},
	}
	if !IsSetupChoice(setup) {
		t.Fatalf("%+v is not recognised as a setup choice", setup)
	}
	crew := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Options: 2, Min: 1, Max: 1,
		Picks: []string{"Colossal Dreadmaw"}, PickKinds: []string{"tapcost"},
		PickRefs: []string{"p0:Colossal Dreadmaw"},
	}
	got := XAnswers([]rules.OracleDecision{setup, crew}, 1, nil)
	if len(got) != 1 || len(got[0]) == 0 {
		t.Fatalf("answers = %#v", got)
	}
	want := XAnswer{Seat: 0, Kind: "setup_choice", Value: "Colossal Dreadmaw"}
	if !reflect.DeepEqual(got[0][0], want) {
		t.Fatalf("first step-0 answer = %#v, want the setup name %#v", got[0][0], want)
	}
	gameplay := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Resume: "etb",
		Picks: []string{"Grizzly Bears"}, PickKinds: []string{"name"},
	}
	if IsSetupChoice(gameplay) {
		t.Fatalf("gameplay name decision %+v classified as a setup choice", gameplay)
	}
}
