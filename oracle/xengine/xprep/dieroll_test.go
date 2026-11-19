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

// TestAddDieRollAnswersSameStepOrder pins the queue order when a step already
// carries a decision and also rolls: the die leads, then the step's scripted
// decisions. XMage's CHOICE queue is FIFO: TestPlayer.rollDieResult consumes
// only a head that starts with DIE_ROLL, and Ebony Fly's ability rolls the
// d6 before it asks the animate yes/no, so ["Yes", die] strict-misses while
// [die, "Yes"] works.
func TestAddDieRollAnswersSameStepOrder(t *testing.T) {
	rolls := []oraclegen.DieRoll{{Step: 1, Seat: 0, Sides: 6, Natural: 3, Result: 3}}
	existing := [][]oraclegen.XAnswer{
		nil,
		{{Seat: 0, Kind: "choice", Value: "yes"}},
	}
	got := addDieRollAnswers(existing, rolls, 2)
	want := [][]oraclegen.XAnswer{
		nil,
		{
			{Seat: 0, Kind: "die", Value: "3"},
			{Seat: 0, Kind: "choice", Value: "yes"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answers %+v, want %+v", got, want)
	}
}

// TestAddDieRollAnswersEbonyFlyStep6 pins the real shape the farm row
// xe:RAND:0:7:w7 lowers to: the tape's natural 3 is on scenario step 6 (the
// pass that resolves Ebony Fly's activation), where gorge's own answer list
// already holds the animate "yes". The die must come first in that step's
// queue, and a multi-die step keeps its log order.
func TestAddDieRollAnswersEbonyFlyStep6(t *testing.T) {
	answers := make([][]oraclegen.XAnswer, 20)
	answers[6] = []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}}
	rolls := []oraclegen.DieRoll{
		{Step: 6, Seat: 0, Sides: 6, Natural: 3, Result: 3},
		{Step: 6, Seat: 0, Sides: 6, Natural: 5, Result: 5},
	}
	got := addDieRollAnswers(answers, rolls, 20)
	want := []oraclegen.XAnswer{
		{Seat: 0, Kind: "die", Value: "3"},
		{Seat: 0, Kind: "die", Value: "5"},
		{Seat: 0, Kind: "choice", Value: "yes"},
	}
	if !reflect.DeepEqual(got[6], want) {
		t.Fatalf("step 6 answers %+v, want %+v", got[6], want)
	}
	for i, as := range got {
		if i != 6 && len(as) != 0 {
			t.Fatalf("step %d changed: %+v", i, as)
		}
	}
}
