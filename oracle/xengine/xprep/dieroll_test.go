package xprep

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// TestAddDieRollAnswers: each recorded die roll becomes one kind "die" answer
// on its step, carrying the NATURAL result (the driver applies its own
// modifier). A roll outside the item's steps is not scripted, and an answer
// list shorter than the step count grows to it.
func TestAddDieRollAnswers(t *testing.T) {
	rolls := []oraclegen.DieRoll{
		{Step: 1, Seat: 0, Sides: 6, Natural: 3, Result: 4},
		{Step: 0, Seat: 1, Sides: 20, Natural: 17, Result: 17},
		{Step: 2, Seat: 0, Sides: 6, Natural: 5, Result: 5}, // past the steps
	}
	got := addDieRollAnswers([][]oraclegen.XAnswer{{}}, rolls, 2)
	want := [][]oraclegen.XAnswer{
		{{Seat: 1, Kind: "die", Value: "17"}},
		{{Seat: 0, Kind: "die", Value: "3"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answers %+v, want %+v", got, want)
	}
	if nothing := addDieRollAnswers(nil, nil, 2); nothing != nil {
		t.Fatalf("no rolls changed the answers: %+v", nothing)
	}
}
