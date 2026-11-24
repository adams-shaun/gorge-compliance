package oraclegen

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// Aurora Awakener's Vivid DigUntil (OptionalFoundMove$ True, X = 1 colour)
// reveals one Forest and gorge puts it onto the battlefield. XMage's
// TargetCardInLibrary(0, MAX) over the revealed cards is complete once the
// lone permanent is chosen, so a closing [choice_skip] is never read and
// strict XMage fails "(Choices of PlayerA) Count are not equal (found
// [[choice_skip]])" (xp-xmage-choices-count-leftover, triage xpt-b02019c3).
func TestDigUntilFoundMoveScriptsNoTrailingSkip(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	zero := 0
	forests := func(n int) []string { return strings.Fields(strings.Repeat("Forest ", n)) }
	sc := Scenario{
		Name: "aurora awakener found move",
		Setup: map[string]Seat{
			"p0": {Battlefield: forests(7), Hand: []string{"Aurora Awakener"}, LibraryTop: forests(20)},
			"p1": {LibraryTop: strings.Fields(strings.Repeat("Plains ", 20))},
		},
	}
	for _, ref := range []string{"p0:Forest", "p0:Forest#2", "p0:Forest#3", "p0:Forest#4", "p0:Forest#5", "p0:Forest#6", "p0:Forest#7"} {
		sc.Steps = append(sc.Steps, Step{Op: "activate", Seat: 0, Card: ref, AbilityIndex: &zero})
	}
	sc.Steps = append(sc.Steps,
		Step{Op: "cast", Seat: 0, Card: "p0:Aurora Awakener"},
		Step{Op: "pass", Seat: 0},
		Step{Op: "pass", Seat: 1},
		Step{Op: "pass", Seat: 0},
		Step{Op: "pass", Seat: 1, Answers: []Answer{{Kind: "choose", Pick: []string{"yes"}}}},
	)
	raw, _ := json.Marshal(sc)
	res, err := rules.RunOracleScenarioJSON(reg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("gorge rejects the scenario: %v", res.Fails)
	}
	step := -1
	for _, d := range res.Decisions {
		if d.Resume == "diguntil_move" && len(d.Picks) == 1 && pickKind(d, 0) == "yes" {
			step = d.Step
		}
	}
	if step < 0 {
		t.Fatalf("no answered diguntil_move decision: %+v", res.Decisions)
	}
	got := XAnswersForScenario(reg, res, sc, nil, nil)
	want := []XAnswer{{0, "choice", "Forest"}}
	if !reflect.DeepEqual(got[step], want) {
		t.Fatalf("found-move answers = %+v, want %+v", got[step], want)
	}
}
