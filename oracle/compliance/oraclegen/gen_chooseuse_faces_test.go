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

// xmageChooseUseTrueLabel is the Forge label on XMage's chooseUse TRUE branch
// for each listed face, written from the XMage source at XMAGE_REF fdde07931f
// independently of xmageChooseUseFaces' polarity values, so the census below
// compares the map against something other than itself.
var xmageChooseUseTrueLabel = map[string]string{
	"abundant harvest":          "Land",
	"bane, lord of darkness":    "CARDNAME's controller draws a card.",
	"combustible gearhulk":      "CARDNAME's controller draws three cards.",
	"covenant of minds":         "You may choose to put those cards into that player's hand.",
	"decoy gambit":              "CARDNAME's controller draws a card.",
	"distant memories":          "Yes",
	"inspirit, flagship vessel": "Put a +1/+1 counter on target artifact.",
	"multiform wonder":          "-2/+2",
	"spark of creativity":       "CARDNAME deals damage to target creature.",
	"liar's pendulum":           "A card with the chosen name is in that player's hand.",
	"palantír of orthanc":       "CARDNAME's controller draws a card.",
	"sin prodder":               "Yes (take damage)",
	"soul echo":                 "For each 1 damage that would be dealt to this player until their next upkeep, they remove an echo counter from CARDNAME instead.",
	"terrapact intimidator":     "CARDNAME's controller creates two Lander tokens.",
}

// Decoy Gambit's Forge Choices$ list the "return to hand" label first, but
// XMage's chooseUse TRUE branch is "draw a card": the draw pick must be yes
// and the return pick no (the first review's polarity finding).
func TestDecoyGambitDrawIsYesReturnIsNo(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Decoy Gambit")
	if !ok {
		t.Fatal("Decoy Gambit missing from the corpus")
	}
	const (
		returnLabel = "Return targeted creature you control to its owner's hand."
		drawLabel   = "CARDNAME's controller draws a card."
	)
	modes := ModeNumbers(card.Faces[0])
	if modes[drawLabel] != ModeYesQueue || modes[returnLabel] != ModeNoQueue {
		t.Fatalf("draw/return = %d/%d, want yes/no (%#v)", modes[drawLabel], modes[returnLabel], modes)
	}
	mk := func(label string, idx int) rules.OracleDecision {
		return rules.OracleDecision{Step: 0, Seat: 1, Kind: "mode", GorgeKind: "modes", Resume: "modes",
			Options: 2, Min: 1, Max: 1, Picks: []string{label}, PickIdx: []int{idx}, PickKinds: []string{"mode"}}
	}
	got := xanswers([]rules.OracleDecision{mk(drawLabel, 1), mk(returnLabel, 0)}, 1, modes, nil)
	want := [][]XAnswer{{{Seat: 1, Kind: "choice", Value: "yes"}, {Seat: 1, Kind: "choice", Value: "no"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answers = %#v, want %#v", got, want)
	}
}

// Census: every face in xmageChooseUseFaces is a corpus face with exactly one
// two-option GenericChoice whose XMage-true-branch label (xmageChooseUseTrueLabel)
// maps to the yes queue and the other to the no queue. A new entry needs an
// XMage-source proof (see choice_dialog.go).
func TestChooseUseFacesModeNumbersCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if len(xmageChooseUseFaces) != 14 || len(xmageChooseUseTrueLabel) != 14 {
		t.Fatalf("xmageChooseUseFaces has %d entries, true-label table %d; adding one needs an XMage-source proof (see its comment)",
			len(xmageChooseUseFaces), len(xmageChooseUseTrueLabel))
	}
	seen := map[string]bool{}
	for _, c := range reg.AllCards() {
		if c == nil {
			continue
		}
		for _, f := range c.Faces {
			if f == nil || !hasKey(xmageChooseUseFaces, strings.ToLower(f.Name)) {
				continue
			}
			pairs, ok := chooseUseLabels(f)
			if !ok || len(pairs) != 1 {
				t.Errorf("%s: want exactly one two-option GenericChoice body", f.Name)
				continue
			}
			pair := pairs[0]
			if want := xmageChooseUseTrueLabel[strings.ToLower(f.Name)]; pair[0] != want {
				t.Errorf("%s: yes label %q, XMage true branch is %q", f.Name, pair[0], want)
			}
			modes := ModeNumbers(f)
			if modes[pair[0]] != ModeYesQueue || modes[pair[1]] != ModeNoQueue {
				t.Errorf("%s: %q/%q = %d/%d, want yes/no", f.Name,
					pair[0], pair[1], modes[pair[0]], modes[pair[1]])
			}
			seen[strings.ToLower(f.Name)] = true
		}
	}
	if len(seen) != len(xmageChooseUseFaces) {
		t.Errorf("listed faces found in the corpus: %v of %d", seen, len(xmageChooseUseFaces))
	}
}

func hasKey(m map[string]bool, k string) bool { _, ok := m[k]; return ok }
