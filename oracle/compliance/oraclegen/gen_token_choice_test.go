package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// TestTokenChoiceAnswersOnXMageChoiceQueues pins the "create your choice of a
// <token> ..." GenericChoice shape: XMage's own OneShotEffect asks chooseUse
// for two tokens (Tireless Provisioner, first token = yes) and a ChoiceImpl
// over the token names for three (Transmutation Font), never a numeric mode.
func TestTokenChoiceAnswersOnXMageChoiceQueues(t *testing.T) {
	three := &cards.Face{SVars: map[string]string{
		"Choose": "DB$ GenericChoice | Defined$ You | Choices$ Blood,Clue,Food",
		"Blood":  "DB$ Token | TokenScript$ blood | SpellDescription$ Blood",
		"Clue":   "DB$ Token | TokenScript$ clue | SpellDescription$ Clue",
		"Food":   "DB$ Token | TokenScript$ food | SpellDescription$ Food",
	}}
	two := &cards.Face{SVars: map[string]string{
		"Choose":   "DB$ GenericChoice | Defined$ You | Choices$ Food,Treasure",
		"Food":     "DB$ Token | TokenScript$ food | SpellDescription$ Food",
		"Treasure": "DB$ Token | TokenScript$ treasure | SpellDescription$ Treasure",
	}}
	mixed := &cards.Face{SVars: map[string]string{
		"Choose": "DB$ GenericChoice | Defined$ You | Choices$ Food,Draw",
		"Food":   "DB$ Token | TokenScript$ food | SpellDescription$ Food",
		"Draw":   "DB$ Draw | SpellDescription$ Draw",
	}}
	if m := modeNumbers(mixed); len(m) != 0 {
		t.Fatalf("a non-token choice must stay a plain mode: %#v", m)
	}
	pick := func(label string, idx, opts int) rules.OracleDecision {
		return rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", Options: opts, Min: 1, Max: 1,
			Picks: []string{label}, PickIdx: []int{idx}, PickKinds: []string{"mode"}, Resume: "modes"}
	}
	for _, tc := range []struct {
		face *cards.Face
		d    rules.OracleDecision
		want XAnswer
	}{
		{three, pick("Blood", 0, 3), XAnswer{Seat: 0, Kind: "choice", Value: "Blood"}},
		{three, pick("Food", 2, 3), XAnswer{Seat: 0, Kind: "choice", Value: "Food"}},
		{two, pick("Food", 0, 2), XAnswer{Seat: 0, Kind: "choice", Value: "yes"}},
		{two, pick("Treasure", 1, 2), XAnswer{Seat: 0, Kind: "choice", Value: "no"}},
	} {
		got := xanswers([]rules.OracleDecision{tc.d}, 1, modeNumbers(tc.face), nil)
		if want := [][]XAnswer{{tc.want}}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: answers = %#v, want %#v", tc.d.Picks[0], got, want)
		}
	}
}

// TestTokenChoiceOnAnActivatedAbility pins the A: carrier (Transmutation
// Font's AB$ GenericChoice): the face's ability list is scanned as well as
// its SVar bodies.
func TestTokenChoiceOnAnActivatedAbility(t *testing.T) {
	f := &cards.Face{
		Abilities: []*cards.SA{{API: "GenericChoice", Params: map[string]string{"AB": "GenericChoice", "Defined": "You", "Choices": "Blood,Clue,Food"}}},
		SVars: map[string]string{
			"Blood": "DB$ Token | TokenScript$ blood | SpellDescription$ Blood",
			"Clue":  "DB$ Token | TokenScript$ clue | SpellDescription$ Clue",
			"Food":  "DB$ Token | TokenScript$ food | SpellDescription$ Food",
		},
	}
	modes := modeNumbers(f)
	for _, label := range []string{"Blood", "Clue", "Food"} {
		if m, ok := modes[label]; !ok || m != ModeChoiceQueue {
			t.Fatalf("modes[%q] = %d, %v; want the choice-queue sentinel (%#v)", label, m, ok, modes)
		}
	}
}
