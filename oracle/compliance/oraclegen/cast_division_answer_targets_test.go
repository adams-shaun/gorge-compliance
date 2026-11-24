package oraclegen

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// A lowered xproof item carries a cast's targets in the step's answers, not in
// its target list, so gorge records the root target ask as via "answer". The
// divided spell's shares must still be scripted as "<ref>^X=<share>" (XMage's
// TargetAmount): a bare "target p1" fails with "Must be target amount".

func dividedAnswerTargets(t *testing.T, raw string, wantDivide bool) []string {
	t.Helper()
	reg := twinBoltRegistry(t)
	res, err := rules.RunOracleScenarioJSON(reg, []byte(raw))
	if err != nil {
		t.Fatalf("scenario did not run: %v", err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	var sc Scenario
	if err := json.Unmarshal([]byte(raw), &sc); err != nil {
		t.Fatal(err)
	}
	root := castRootTarget(res.Decisions, 0)
	if root < 0 || res.Decisions[root].Via != "answer" {
		t.Fatalf("precondition: the cast's root target ask is not via answer: %+v", res.Decisions)
	}
	if wantDivide && divideAsk(res.Decisions, root) < 0 {
		t.Fatalf("precondition: gorge posed no cast-time division ask for two targets: %+v", res.Decisions)
	}
	var got []string
	for _, step := range XAnswersForScenario(reg, res, sc, nil, nil) {
		for _, a := range step {
			got = append(got, a.Kind+" "+a.Value)
		}
	}
	return got
}

func TestDividedCastLoneTargetFromAnswersScriptsShare(t *testing.T) {
	got := dividedAnswerTargets(t, `{
  "name": "arc-lightning-lone", "cr": ["601.2d"], "why": "lone divided target from answers",
  "setup": {"p0": {"hand": ["Arc Lightning"]}, "p1": {}},
  "steps": [
    {"op": "cast", "seat": 0, "card": "p0:Arc Lightning", "mana": "CCR", "answers": [{"kind": "target", "pick": ["p1"]}]},
    {"op": "resolve"}
  ]
}`, false)
	want := []string{"target p1^X=3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lone answer target: got %v want %v", got, want)
	}
}

func TestDividedCastTwoTargetsFromAnswersScriptsShares(t *testing.T) {
	got := dividedAnswerTargets(t, `{
  "name": "arc-lightning-two", "cr": ["601.2d"], "why": "two divided targets from answers",
  "setup": {"p0": {"hand": ["Arc Lightning"]}, "p1": {"battlefield": ["Grizzly Bears", "Serra Angel"]}},
  "steps": [
    {"op": "cast", "seat": 0, "card": "p0:Arc Lightning", "mana": "CCR", "answers": [{"kind": "target", "pick": ["p1:Grizzly Bears", "p1:Serra Angel"]}]},
    {"op": "resolve"}
  ]
}`, true)
	want := []string{"target p1:Grizzly Bears^X=2", "target p1:Serra Angel^X=1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("two answer targets: got %v want %v", got, want)
	}
}
