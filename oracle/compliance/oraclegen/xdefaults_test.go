package oraclegen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// Transmutation Font's plain GenericChoice is one XMage Choice dialog over the
// labels: the pick must reach the choice queue by label, not the numeric mode
// queue the positional fallback used (lb-xdefaults).
func TestPlainGenericChoiceDialogScriptsLabel(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Transmutation Font")
	if !ok {
		t.Fatal("Transmutation Font missing from the corpus")
	}
	modes := ModeNumbers(card.Faces[0])
	if modes["Clue"] != ModeChoiceQueue {
		t.Fatalf("ModeNumbers = %v, want Clue on the choice queue", modes)
	}
	// Precondition: the pick is the NON-first label, so a positional "mode 1"
	// and the label answer cannot coincide.
	d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "mode", GorgeKind: "modes", Resume: "modes",
		Options: 3, Min: 1, Max: 1, Picks: []string{"Clue"}, PickIdx: []int{1}, PickKinds: []string{"mode"},
		PickRefs: []string{"p0:Transmutation Font"}}
	got := xanswers([]rules.OracleDecision{d}, 2, modes, nil)
	want := [][]XAnswer{nil, {{0, "choice", "Clue"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
	// A face outside the proven set keeps the positional mode answer.
	other, _ := reg.Lookup("Grizzly Bears")
	if m := ModeNumbers(other.Faces[0]); len(m) != 0 {
		t.Fatalf("Grizzly Bears ModeNumbers = %v, want none", m)
	}
}

// Census: every face in xmageChoiceDialogFaces is a corpus face whose
// GenericChoice labels all map to the choice queue.
func TestChoiceDialogFacesCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if len(xmageChoiceDialogFaces) != 5 {
		t.Fatalf("xmageChoiceDialogFaces has %d entries; adding one needs an XMage-source proof (see its comment)", len(xmageChoiceDialogFaces))
	}
	seen := map[string]bool{}
	for _, c := range reg.AllCards() {
		if c == nil {
			continue
		}
		for _, f := range c.Faces {
			if f == nil || !xmageChoiceDialogFaces[strings.ToLower(f.Name)] {
				continue
			}
			labels := choiceDialogLabels(f)
			if len(labels) < 2 {
				t.Errorf("%s: %d GenericChoice labels", f.Name, len(labels))
			}
			m := ModeNumbers(f)
			for _, l := range labels {
				if m[l] != ModeChoiceQueue {
					t.Errorf("%s: label %q not on the choice queue", f.Name, l)
				}
			}
			seen[strings.ToLower(f.Name)] = true
		}
	}
	if len(seen) != len(xmageChoiceDialogFaces) {
		t.Errorf("listed faces found in the corpus: %v of %d", seen, len(xmageChoiceDialogFaces))
	}
}

// A gift PROMISE is chooseUse yes whatever option index lists it (Jolly
// Gerbils' Blooming Blast offers "Don't promise" first, so the promise is
// option 1 and the old "option 0 is yes" rule sent no).
func TestGiftPromiseAtSecondOptionIsYes(t *testing.T) {
	d := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Min: 1, Max: 1,
		Picks: []string{"Promise an opponent a gift"}, PickRefs: []string{"Promise an opponent a gift"},
		PickIdx: []int{1}, PickKinds: []string{"gift_promise"}}
	if d.PickIdx[0] != 1 {
		t.Fatal("precondition: the promise must be listed second")
	}
	if got := routed(t, []rules.OracleDecision{d}, 1); !reflect.DeepEqual(got, []XAnswer{{0, "choice", "yes"}}) {
		t.Fatalf("promise at option 1 = %#v, want yes", got)
	}
}

// A decline-only triggered optional cost is still XMage's DoIfCostPaid
// chooseUse; its AI would pay what gorge declined (Byway Barterer, Esoteric
// Duplicator).
func TestLoneTriggerCostDeclineScriptsNo(t *testing.T) {
	d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 1, Min: 1, Max: 1,
		Picks: []string{"Do not pay"}, PickIdx: []int{0}, PickRefs: []string{"p0:Byway Barterer"}, CostPosed: true,
		PickKinds: []string{"trigger_cost_decline"}, Via: "answer"}
	if !loneTriggerCostDecline(d) {
		t.Fatal("precondition: fixture must be a lone trigger-cost decline")
	}
	got := xanswers([]rules.OracleDecision{d}, 2, nil, nil)
	want := [][]XAnswer{nil, {{0, "choice", "no"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xanswers = %#v, want %#v", got, want)
	}
}

// Whiskervale Forerunner: a declined one-card chooser after a look is
// XMage's mandatory 1..1 makeChoose, so the rerun takes the eligible card.
func TestDeclinedLookedOnePickIsForced(t *testing.T) {
	ack := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "look_ack",
		Options: 1, Min: 1, Max: 1, Picks: []string{"Continue"}, PickIdx: []int{0}, PickKinds: []string{"yes"}, First: "Continue", Via: "answer"}
	pick := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "choice",
		Options: 1, Min: 0, Max: 1, OptionRefs: []string{"p0:Grizzly Bears"}, Via: "resolve fallback"}
	sc := Scenario{Steps: []Step{{Op: "cast"}, {Op: "resolve"}}}
	ds := []rules.OracleDecision{ack, pick}
	if _, changed := searchPicks(sc, ds, false); changed {
		t.Fatal("without the card-side look proof nothing is forced")
	}
	out, changed := searchPicks(sc, ds, true)
	if !changed || len(out.Steps[1].Answers) != 1 || out.Steps[1].Answers[0].Pick[0] != "p0:Grizzly Bears" {
		t.Fatalf("searchPicks look = %+v changed=%v, want the Grizzly Bears pick forced", out.Steps, changed)
	}
}
