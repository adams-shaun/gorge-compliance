package oraclegen

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
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

// Throw a Line is an Adventure spell face: put two +1/+1 counters distributed
// among one or two target creatures. A lone target takes both counters and
// gorge asks no division (CR 601.2d), but XMage's TargetAmount still scripts
// the share as "<ref>^X=2"; a plain target fails "Must be target amount".
func TestDividedCastLoneTargetPutCounterFromAnswersScriptsShare(t *testing.T) {
	got := dividedAnswerTargets(t, `{
  "name": "ondu-knotmaster-throw-a-line-lone", "cr": ["601.2d"], "why": "lone divided PutCounter target from answers",
  "setup": {"p0": {"hand": ["Ondu Knotmaster"]}, "p1": {"battlefield": ["Phyrexian Walker"]}},
  "steps": [
    {"op": "cast", "seat": 0, "card": "p0:Ondu Knotmaster", "cast_mode": "adventure_alt", "mana": "WB", "answers": [{"kind": "target", "pick": ["p1:Phyrexian Walker"]}]},
    {"op": "resolve"}
  ]
}`, false)
	want := []string{"target p1:Phyrexian Walker^X=2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lone answer PutCounter target: got %v want %v", got, want)
	}
}

// TestDividedSpellFaceFollowsCastMode pins that spellDivision reads the face
// the cast selects, not whichever face carries a divided spell ability: an
// alternate-face cast mode flips to the second face, an explicit ref may name
// it, and every other cast reads the main face.
func TestDividedSpellFaceFollowsCastMode(t *testing.T) {
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{AlternateMode: "Adventure", Faces: []*cards.Face{
		{Name: "Front", Abilities: []*cards.SA{{Kind: "SP", API: "DealDamage", Params: map[string]string{
			"ValidTgts": "Any", "DividedAsYouChoose": "3"}}}},
		{Name: "Back", Types: []string{"Sorcery", "Adventure"}, Abilities: []*cards.SA{{Kind: "SP", API: "PutCounter", Params: map[string]string{
			"ValidTgts": "Creature", "DividedAsYouChoose": "2"}}}},
	}})
	for _, tc := range []struct {
		name  string
		st    Step
		api   string
		total int
	}{
		{"front by ref", Step{Card: "p0:Front"}, "DealDamage", 3},
		{"adventure_alt flips to the back", Step{Card: "p0:Front", CastMode: "adventure_alt"}, "PutCounter", 2},
		{"disturb flips to the back", Step{Card: "p0:Front", CastMode: "disturb"}, "PutCounter", 2},
		{"converted flips to the back", Step{Card: "p0:Front", CastMode: "converted"}, "PutCounter", 2},
		{"back named by ref", Step{Card: "p0:Back"}, "PutCounter", 2},
		{"prepared copy names its spell face", Step{Card: "p0:Back", CastMode: "prepared_copy"}, "PutCounter", 2},
		{"non-flip mode reads the front", Step{Card: "p0:Front", CastMode: "kicked"}, "DealDamage", 3},
		{"adventure_recast returns to the front", Step{Card: "p0:Back", CastMode: "adventure_recast"}, "DealDamage", 3},
	} {
		api, total, ok := spellDivision(reg, tc.st)
		if !ok || api != tc.api || total != tc.total {
			t.Errorf("%s: spellDivision = (%q, %d, %v), want (%q, %d, true)", tc.name, api, total, ok, tc.api, tc.total)
		}
	}
}
