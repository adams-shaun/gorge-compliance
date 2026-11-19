package oraclegen

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
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
	// lifeOnly is the shape the committed corpus actually records when the
	// pool has no coloured mana: gorge's feasibility gate trims the menu to
	// the single life face (Gut Shot's {R/P}, Dismember's {B/P}{B/P},
	// K'rrik's {B/P} payments). forcedSingleOption used to drop that ask
	// before the pay_life branch, so XMage got no queued boolean at all.
	lifeOnly := func(step int) rules.OracleDecision {
		return rules.OracleDecision{Step: step, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Options: 1, Min: 1, Max: 1, Picks: []string{"Pay 2 life"}, PickIdx: []int{0},
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
		{"activate life-only", lifeOnly(2), [][]XAnswer{nil, nil, {{0, "choice", "yes"}}, nil}},
		{"cast life-only", lifeOnly(0), [][]XAnswer{{{0, "choice", "yes"}}, nil, nil}},
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

// TestXAnswersPhyrexianCorpus pins the generator against the committed Oracle
// corpus, not a hand-built decision: the feasibility gate trims a Phyrexian
// cast's menu to the ONE life face when the pool has no coloured mana, so the
// real recorded shape is Options == 1, not the two-option activation row.
// Each recorded pay_life pick must reach XAnswersForScenario as one "yes".
func TestXAnswersPhyrexianCorpus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, c := range []struct {
		path     string
		scenario string
		pips     int
	}{
		{"../../../scenarios/oracle/spell-conditional/gut-shot.json", "life-only-pays-two-life", 1},
		{"../../../scenarios/oracle/cost-modifier/krrik-son-of-yawgmoth.json", "phyrexian-symbols-paid-with-life", 3},
		{"../../../scenarios/oracle/cost-modifier/dismember.json", "both-pips-paid-with-life", 2},
	} {
		t.Run(c.scenario, func(t *testing.T) {
			raw, err := os.ReadFile(c.path)
			if err != nil {
				t.Fatal(err)
			}
			var file struct {
				Scenarios []json.RawMessage `json:"scenarios"`
			}
			if err := json.Unmarshal(raw, &file); err != nil {
				t.Fatal(err)
			}
			var body []byte
			var sc Scenario
			for _, r := range file.Scenarios {
				var got Scenario
				if err := json.Unmarshal(r, &got); err != nil {
					t.Fatal(err)
				}
				if got.Name == c.scenario {
					body, sc = r, got
					break
				}
			}
			if body == nil {
				t.Fatalf("%s: scenario %q not found", c.path, c.scenario)
			}
			res, err := rules.RunOracleScenarioJSON(reg, body)
			if err != nil {
				t.Fatalf("run %s: %v (fails %v)", c.scenario, err, res.Fails)
			}
			recorded, oneOption := 0, true
			lifeSteps := map[int]int{}
			for _, d := range res.Decisions {
				if !phyrexianLifePicks(d) {
					continue
				}
				recorded += len(d.Picks)
				lifeSteps[d.Step] += len(d.Picks)
				oneOption = oneOption && d.Options == 1
			}
			if recorded != c.pips {
				t.Fatalf("scenario records %d pay_life picks, want %d", recorded, c.pips)
			}
			if !oneOption {
				t.Fatalf("a recorded pay_life pick has Options != 1; this test pins the life-only shape and the corpus no longer has one")
			}
			got := 0
			for step, as := range XAnswersForScenario(reg, res, sc, nil, nil) {
				if lifeSteps[step] == 0 {
					continue
				}
				for _, a := range as {
					if a.Kind == "choice" && a.Value == "yes" {
						got++
					}
				}
			}
			if got != c.pips {
				t.Fatalf("XAnswersForScenario scripts %d yes on the pay_life steps for %d recorded life picks; each Phyrexian ask needs one", got, c.pips)
			}
		})
	}
}
