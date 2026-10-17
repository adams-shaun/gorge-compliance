package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// lb-xpending: a ChoiceRestriction$ Charm (Zuko, Conflicted, ThisGame) is
// offered without the modes already chosen, and XMage's scripted mode number
// indexes the modes still on offer. Zuko's second ask offered three modes and
// picked the first of them ("Put a +1/+1 counter"): the printed position (2)
// made XMage take the second REMAINING mode (Add {R}), the verdict's
// `p0.pool: gorge "", xmage "R"`. The restricted charm's labels therefore stay
// out of the numbering so the answer is the pick's position in the offered
// list; an unrestricted charm keeps its Choices$ position.
func TestChoiceRestrictionCharmAnswersByOfferedPosition(t *testing.T) {
	svars := map[string]string{
		"DBDraw":       "DB$ Draw | SpellDescription$ Draw a card.",
		"DBPutCounter": "DB$ PutCounter | SpellDescription$ Put a +1/+1 counter on NICKNAME.",
		"DBMana":       "DB$ Mana | SpellDescription$ Add {R}.",
	}
	restricted := &cards.Face{SVars: map[string]string{
		"TrigCharm": "DB$ Charm | Choices$ DBDraw,DBPutCounter,DBMana | ChoiceRestriction$ ThisGame",
	}}
	plain := &cards.Face{SVars: map[string]string{
		"TrigCharm": "DB$ Charm | Choices$ DBDraw,DBPutCounter,DBMana",
	}}
	for k, v := range svars {
		restricted.SVars[k] = v
		plain.SVars[k] = v
	}
	const label = "Put a +1/+1 counter on NICKNAME."
	if m, ok := modeNumbers(plain)[label]; !ok || m != 2 {
		t.Fatalf("unrestricted charm numbering = %d,%v want 2 (precondition: the label is a charm label)", m, ok)
	}
	modes := modeNumbers(restricted)
	if _, ok := modes[label]; ok {
		t.Fatalf("restricted charm still numbers %q: %#v", label, modes)
	}
	// Second ask: Draw is used up, three modes were four, gorge picks index 0.
	d := rules.OracleDecision{Step: 2, Seat: 0, Kind: "mode", Options: 2, Min: 1, Max: 1,
		Picks: []string{label}, PickIdx: []int{0}, PickKinds: []string{"mode"}, Resume: "modes"}
	got := xanswers([]rules.OracleDecision{d}, 3, modes, nil)
	want := [][]XAnswer{nil, nil, {{Seat: 0, Kind: "mode", Value: "1"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answers = %#v, want %#v", got, want)
	}
	// The unrestricted face would have scripted the printed position.
	got = xanswers([]rules.OracleDecision{d}, 3, modeNumbers(plain), nil)
	if got[2][0].Value != "2" {
		t.Fatalf("unrestricted answer = %#v, want mode 2", got[2])
	}
}
