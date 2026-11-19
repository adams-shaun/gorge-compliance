package oraclegen

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// A recorded pay_life pick is XMage's Phyrexian chooseUse ("Pay 2 life instead
// of {W} (phyrexian cost)?"), not payment UI: XMage asks at CR 601.2b before
// it will pay a Phyrexian pip, so the step needs one "yes". The same decision
// with a hybrid or twobrid face must stay skipped: XMage pays those from the
// pool without asking, and a scripted answer would shift the later queue.
//
// Measured: NPH Blinding Souleater's {W/P} activation (xe:RAND:4:2:w18/w22)
// stopped at "Missing CHOICE def" on this ask; the sibling Immolating
// Souleater row recorded pick_kinds ["pay_life"].
func TestXAnswersPhyrexian(t *testing.T) {
	life := func(step int) rules.OracleDecision {
		return rules.OracleDecision{Step: step, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Options: 2, Min: 1, Max: 1, Picks: []string{"Pay 2 life"}, PickIdx: []int{1},
			PickRefs: []string{"Pay 2 life"}, PickKinds: []string{"pay_life"}, Via: "answer"}
	}
	// An activation (step 2) and a cast (step 0): XMage poses the same ask at
	// 601.2b for both, and a triggered cost pays through ManaCostsImpl's
	// later prompt with the same recorded kind.
	for _, c := range []struct {
		name string
		d    rules.OracleDecision
		want [][]XAnswer
	}{
		{"activate", life(2), [][]XAnswer{nil, nil, {{0, "choice", "yes"}}, nil}},
		{"cast", life(0), [][]XAnswer{{{0, "choice", "yes"}}, nil, nil}},
	} {
		if got := xanswers([]rules.OracleDecision{c.d}, len(c.want), nil, nil); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s pay_life: xanswers = %#v, want %#v", c.name, got, c.want)
		}
	}

	// A two-pip decision whose life faces were both recorded scripts one yes
	// per pip (ManaAsk poses one decision per pip; this keeps the order if a
	// future shape folds them).
	two := rules.OracleDecision{Step: 2, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Pay 2 life", "Pay 2 life"},
		PickIdx: []int{1, 1}, PickRefs: []string{"Pay 2 life", "Pay 2 life"},
		PickKinds: []string{"pay_life", "pay_life"}, Via: "answer"}
	if got, want := xanswers([]rules.OracleDecision{two}, 3, nil, nil)[2],
		[]XAnswer{{0, "choice", "yes"}, {0, "choice", "yes"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("two pay_life picks = %#v, want %#v", got, want)
	}

	// The payment() skip stays correct for every non-life face: a hybrid
	// label (the brief's "Pay {W/U} with W" shape), a bare colour face and a
	// twobrid generic face are all paid from the pool without a chooseUse.
	for _, c := range []struct {
		name string
		pick string
		kind string
	}{
		{"hybrid label", "Pay {W/U} with W", "pay_W"},
		{"hybrid colour", "Pay W", "pay_W"},
		{"twobrid generic", "Pay 2 generic", "pay_generic"},
	} {
		d := rules.OracleDecision{Step: 2, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Options: 2, Min: 1, Max: 1, Picks: []string{c.pick}, PickIdx: []int{0},
			PickRefs: []string{c.pick}, PickKinds: []string{c.kind}, Via: "answer"}
		if got := xanswers([]rules.OracleDecision{d}, 3, nil, nil); got != nil {
			t.Errorf("%s: xanswers = %#v, want nothing scripted (paid from the pool)", c.name, got)
		}
	}

	// The other half of the fix is the driver's starved-ask arm: without it a
	// colour-face pick (no queued boolean) still stops at "Missing CHOICE
	// def". Pin the arm here too so a merge that drops it fails this test,
	// not only the Java driver-asks script.
	source, err := os.ReadFile("../../xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, want := range []string{
		"if (phyrexianStarved(message, getChoices())) {",
		"static boolean phyrexianAsk(String message)",
		"static boolean phyrexianStarved(String message, List<String> choices)",
		`message.startsWith("Pay 2 life instead of ")`,
	} {
		if !strings.Contains(java, want) {
			t.Errorf("ScenarioReplay.ScriptedChoicePlayer lost its Phyrexian chooseUse arm: missing %q", want)
		}
	}
}
