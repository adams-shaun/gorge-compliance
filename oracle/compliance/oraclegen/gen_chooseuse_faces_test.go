package oraclegen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestTerrapactIntimidatorChoiceAnswersOnTheYesNoQueue pins the punisher
// shape's routing: the targeted opponent's pick is XMage's one boolean
// chooseUse ("Have <controller> create two Lander tokens?"), so the first
// Choices$ label must answer yes on the seat-1 CHOICE queue and the second no
// -- never a numeric mode on the MODE queue (xpt-78514199).
func TestTerrapactIntimidatorChoiceAnswersOnTheYesNoQueue(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Terrapact Intimidator")
	if !ok {
		t.Fatal("Terrapact Intimidator missing from the corpus")
	}
	const (
		yesLabel = "CARDNAME's controller creates two Lander tokens."
		noLabel  = "CARDNAME's controller puts two +1/+1 counters on it."
	)
	modes := ModeNumbers(card.Faces[0])
	if modes[yesLabel] != ModeYesQueue {
		t.Fatalf("ModeNumbers[%q] = %d, want ModeYesQueue (%#v)", yesLabel, modes[yesLabel], modes)
	}
	if modes[noLabel] != ModeNoQueue {
		t.Fatalf("ModeNumbers[%q] = %d, want ModeNoQueue (%#v)", noLabel, modes[noLabel], modes)
	}
	mk := func(label string, idx int) rules.OracleDecision {
		return rules.OracleDecision{Step: 0, Seat: 1, Kind: "mode", GorgeKind: "modes", Resume: "modes",
			Options: 2, Min: 1, Max: 1, Picks: []string{label}, PickIdx: []int{idx}, PickKinds: []string{"mode"}}
	}
	got := xanswers([]rules.OracleDecision{mk(yesLabel, 0), mk(noLabel, 1)}, 1, modes, nil)
	want := [][]XAnswer{{{Seat: 1, Kind: "choice", Value: "yes"}, {Seat: 1, Kind: "choice", Value: "no"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answers = %#v, want %#v", got, want)
	}
}

// Census: every face in xmageChooseUseFaces is a corpus face with a two-option
// GenericChoice whose first label maps to the yes queue and second to the no
// queue. A new entry needs an XMage-source proof (see choice_dialog.go).
func TestChooseUseFacesModeNumbersCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if len(xmageChooseUseFaces) != 11 {
		t.Fatalf("xmageChooseUseFaces has %d entries; adding one needs an XMage-source proof (see its comment)", len(xmageChooseUseFaces))
	}
	seen := map[string]bool{}
	for _, c := range reg.AllCards() {
		if c == nil {
			continue
		}
		for _, f := range c.Faces {
			if f == nil || !xmageChooseUseFaces[strings.ToLower(f.Name)] {
				continue
			}
			pairs, ok := chooseUseLabels(f)
			if !ok || len(pairs) == 0 {
				t.Errorf("%s: no two-option GenericChoice body", f.Name)
				continue
			}
			modes := ModeNumbers(f)
			for _, pair := range pairs {
				if modes[pair[0]] != ModeYesQueue || modes[pair[1]] != ModeNoQueue {
					t.Errorf("%s: %q/%q = %d/%d, want yes/no", f.Name,
						pair[0], pair[1], modes[pair[0]], modes[pair[1]])
				}
			}
			seen[strings.ToLower(f.Name)] = true
		}
	}
	if len(seen) != len(xmageChooseUseFaces) {
		t.Errorf("listed faces found in the corpus: %v of %d", seen, len(xmageChooseUseFaces))
	}
}
