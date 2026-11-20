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

// TestXAnswersPhyrexianMixedFaces pins the per-pip answer plan: a step that
// pays its FIRST phyrexian pip with mana and its SECOND with life must script
// each pip ask's own boolean in cost order (no then yes), not one flat yes
// that lands on the earlier mana-face ask. The corpus scenario is Dismember's
// mixed row; the plan is derived from the corpus cost ({1}{B/P}{B/P}), whose
// two AnnouncePip pips match the two recorded pip decisions.
func TestXAnswersPhyrexianMixedFaces(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	path, name := "../../../scenarios/oracle/cost-modifier/dismember.json", "one-pip-with-mana-one-with-life"
	raw, err := os.ReadFile(path)
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
		if got.Name == name {
			body, sc = r, got
			break
		}
	}
	if body == nil {
		t.Fatalf("%s: scenario %q not found", path, name)
	}
	res, err := rules.RunOracleScenarioJSON(reg, body)
	if err != nil {
		t.Fatalf("run %s: %v (fails %v)", name, err, res.Fails)
	}
	// The recording: two pip decisions on the cast step, both faces offered
	// for both pips (the pool is mana-sufficient, as XMage's castability
	// check requires): pip one picked the mana face, pip two the life face.
	var pips []rules.OracleDecision
	for _, d := range res.Decisions {
		if isPipDecision(d) {
			pips = append(pips, d)
		}
	}
	if len(pips) != 2 || pips[0].Step != 1 || pips[1].Step != 1 {
		t.Fatalf("recorded %d pip decisions (%+v), want two on the cast step 1", len(pips), pips)
	}
	if k := pickKind(pips[0], 0); k != "pay_B" || pips[0].Options != 2 {
		t.Fatalf("pip one pick kind %q options %d, want pay_B with both faces offered", k, pips[0].Options)
	}
	if k := pickKind(pips[1], 0); k != "pay_life" || pips[1].Options != 2 {
		t.Fatalf("pip two pick kind %q options %d, want pay_life with both faces offered", k, pips[1].Options)
	}
	// The derived plan mirrors the faces in cost order: pip one is a mana
	// face XMage also asks (both faces affordable), pip two the life face.
	plan := phyrexianPipPlan(reg, res, sc)
	wantPlan := map[int][]pipAsk{1: {{asks: true, life: false}, {asks: true, life: true}}}
	if !reflect.DeepEqual(plan, wantPlan) {
		t.Fatalf("phyrexianPipPlan = %#v, want %#v", plan, wantPlan)
	}
	// End to end: the cast step's choice stream is the positional pair, and
	// no other step carries a leftover boolean.
	out := XAnswersForScenario(reg, res, sc, nil, nil)
	want := []XAnswer{{0, "choice", "no"}, {0, "choice", "yes"}}
	if !reflect.DeepEqual(out[1], want) {
		t.Fatalf("XAnswersForScenario step 1 = %#v, want %#v", out[1], want)
	}
	for step, as := range out {
		if step == 1 {
			continue
		}
		for _, a := range as {
			if a.Kind == "choice" {
				t.Fatalf("step %d scripts an unexpected choice %#v", step, a)
			}
		}
	}
}

// TestXAnswersPhyrexianPlanFallback pins the mis-derivation guards: a plan is
// only built when the re-derived cost's AnnouncePip count matches the
// recorded pip decisions, and a step without a life-face pip decision never
// gets one (its no-yes shape is already exact).
func TestXAnswersPhyrexianPlanFallback(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc := Scenario{Steps: []Step{{Op: "mana"}, {Op: "cast", Card: "p0:Dismember", Targets: []string{"p1:Grizzly Bears"}}}}
	// One pip decision for a two-pip cost: the count guard refuses the plan.
	one := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Pay B"}, PickIdx: []int{0},
		PickRefs: []string{"Pay B"}, PickKinds: []string{"pay_B"}, Via: "answer"}
	if plan := phyrexianPipPlan(reg, rules.OracleResult{Decisions: []rules.OracleDecision{one}}, sc); plan != nil {
		t.Fatalf("one pip decision for a two-pip cost derived a plan %#v; the count guard must fall back", plan)
	}
	// A mana-face-only step (both pips paid with mana) needs no plan: XMage
	// asks each pip and the historical routing (or nothing) covers it.
	noLife := []rules.OracleDecision{
		one, {Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Options: 2, Min: 1, Max: 1, Picks: []string{"Pay B"}, PickIdx: []int{0},
			PickRefs: []string{"Pay B"}, PickKinds: []string{"pay_B"}, Via: "answer"},
	}
	if plan := phyrexianPipPlan(reg, rules.OracleResult{Decisions: noLife}, sc); plan != nil {
		t.Fatalf("a mana-face-only step derived a plan %#v; only a life-face pip needs one", plan)
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
