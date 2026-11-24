package oraclegen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// answersWithSources scripts ds as one step 0 with the resolving card names
// the face-keyed tables read.
func answersWithSources(ds []rules.OracleDecision, sources ...string) []XAnswer {
	extra := xanswersExtra{sources: map[int][]string{}}
	for _, d := range ds {
		extra.sources[d.Step] = sources
	}
	out := xanswersSetup(ds, 1, nil, nil, nil, nil, extra)
	if out == nil {
		return nil
	}
	return out[0]
}

func pickDecision(kind, label, resume string) rules.OracleDecision {
	return rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: resume,
		Options: 2, Min: 1, Max: 1, Picks: []string{label}, PickIdx: []int{0}, PickRefs: []string{label},
		PickKinds: []string{kind}}
}

// The shape census of the XMage chooseUse dialogs gorge poses as a labelled
// pick (xp-xmage-choice-def-residual2): Unleash, clash placement and the
// two-face choices. Each entry names the gorge picks and the yes/no they
// script.
func TestXMageBooleanDialogsCensus(t *testing.T) { checkBooleanDialogCensus(t) }

// checkBooleanDialogCensus is also run by TestChoiceDialogFacesCensus, the
// census of every XMage dialog the generator routes by face.
func checkBooleanDialogCensus(t *testing.T) {
	t.Helper()
	yes := []XAnswer{{0, "choice", "yes"}}
	no := []XAnswer{{0, "choice", "no"}}
	for _, c := range []struct {
		name    string
		d       rules.OracleDecision
		sources []string
		want    []XAnswer
	}{
		{"unleash with the counter", pickDecision("unleash", "Enter with a +1/+1 counter", "etb"), nil, yes},
		{"unleash without", pickDecision("unleash", "Enter without a counter", "etb"), nil, no},
		{"clash put back on top", pickDecision("top", "Put it on top", "clash_placement"), nil, yes},
		{"clash put on the bottom", pickDecision("bottom", "Put it on the bottom", "clash_placement"), nil, no},
		{"Odd or even: odd", pickDecision("odd", "odd", "choice"), []string{"extinction event"}, yes},
		{"Odd or even: even", pickDecision("even", "even", "choice"), []string{"extinction event"}, no},
		{"Creature or land: creature", pickDecision("card_type", "Creature", "choice"), []string{"winding way"}, yes},
		{"Creature or land: land", pickDecision("card_type", "Land", "choice"), []string{"winding way"}, no},
		{"Domri red", pickDecision("mana", "Add R", "mana_color"), []string{"domri, anarch of bolas"}, yes},
		{"Domri green", pickDecision("mana", "Add G", "mana_color"), []string{"domri, anarch of bolas"}, no},
		// Land or nonland (Abundant Harvest, a GenericChoice) rides the mode
		// sentinels: see TestXMageBooleanDialogFacesAgainstCorpus.
	} {
		got := answersWithSources([]rules.OracleDecision{c.d}, c.sources...)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: answers %#v, want %#v", c.name, got, c.want)
		}
	}
	// A face-keyed pick on a step whose resolving card is not listed keeps the
	// generic label route.
	if got := answersWithSources([]rules.OracleDecision{pickDecision("card_type", "Creature", "choice")}, "some other card"); len(got) == 1 && got[0].Value == "yes" {
		t.Errorf("an unlisted card's Creature pick scripted yes: %#v", got)
	}
}

// Land or nonland (Abundant Harvest) is a GenericChoice: its modes ride the
// yes/no queue sentinels, the first Choices$ label (Land) being XMage's true
// branch. The corpus faces behind each face-keyed entry carry the gorge shape
// the labels were read from.
func TestXMageBooleanDialogFacesAgainstCorpus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	face := func(name string) *cards.Face {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s missing from the corpus", name)
		}
		return c.Faces[0]
	}
	harvest := face("Abundant Harvest")
	if pairs, ok := chooseUseLabels(harvest); !ok || len(pairs) != 1 || pairs[0] != [2]string{"Land", "Nonland"} {
		t.Fatalf("Abundant Harvest chooseUse labels = %v, %v; want [Land Nonland]", pairs, ok)
	}
	if m := ModeNumbers(harvest); m["Land"] != ModeYesQueue || m["Nonland"] != ModeNoQueue {
		t.Fatalf("Abundant Harvest modes = %v", m)
	}
	if sa := face("Extinction Event").Abilities[0]; sa.API != "ChooseEvenOdd" {
		t.Errorf("Extinction Event API = %s", sa.API)
	}
	if sa := face("Winding Way").Abilities[0]; sa.API != "ChooseType" || sa.Params["ValidTypes"] != "Creature,Land" {
		t.Errorf("Winding Way = %s %v", sa.API, sa.Params)
	}
	domri := face("Domri, Anarch of Bolas")
	combo := false
	for _, sa := range domri.Abilities {
		combo = combo || sa.API == "Mana" && strings.HasPrefix(sa.Params["Produced"], "Combo R G")
	}
	if !combo {
		t.Errorf("Domri has no Combo R G mana ability")
	}
	for name := range xmageBooleanDialogFaces {
		found := false
		for _, c := range reg.AllCards() {
			for _, f := range c.Faces {
				found = found || f != nil && strings.EqualFold(f.Name, name)
			}
		}
		if !found {
			t.Errorf("xmageBooleanDialogFaces lists %q, absent from the corpus", name)
		}
	}
	for name := range xmageDigMayAskFaces {
		if _, ok := reg.Lookup(name); !ok {
			t.Errorf("xmageDigMayAskFaces lists %q, absent from the corpus", name)
		}
	}
	if len(xmageBooleanDialogFaces) != 3 || len(xmageDigMayAskFaces) != 8 {
		t.Errorf("tables hold %d and %d entries; a new entry needs an XMage-source proof", len(xmageBooleanDialogFaces), len(xmageDigMayAskFaces))
	}
}

// An optional Dig take on a listed look-and-pick face is XMage's chooseUse
// then the TARGET-queue pick; a declined one is the lone no.
func TestDigMayAskScriptsYesThenTargetPick(t *testing.T) {
	take := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "dig",
		Options: 3, Min: 0, Max: 1, Picks: []string{"Forest"}, PickIdx: []int{0}, PickRefs: []string{"p0:Forest#2"},
		PickKinds: []string{"dig"}}
	want := []XAnswer{{0, "choice", "yes"}, {0, "target", "Forest"}}
	if got := answersWithSources([]rules.OracleDecision{take}, "adventurous impulse"); !reflect.DeepEqual(got, want) {
		t.Fatalf("take = %#v, want %#v", got, want)
	}
	declined := take
	declined.Picks, declined.PickIdx, declined.PickRefs, declined.PickKinds = nil, nil, nil, nil
	if got := answersWithSources([]rules.OracleDecision{declined}, "grisly salvage"); !reflect.DeepEqual(got, []XAnswer{{0, "choice", "no"}}) {
		t.Fatalf("declined = %#v, want a lone no", got)
	}
	// Sage of Days' Optional$ Dig is XMage's up-to target with no chooseUse.
	if got := answersWithSources([]rules.OracleDecision{take}, "sage of days"); len(got) > 0 && got[0].Value == "yes" {
		t.Fatalf("an unlisted card's take scripted a chooseUse: %#v", got)
	}
}
