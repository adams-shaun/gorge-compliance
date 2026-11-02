package oraclegen

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestScriptedLibraryAnswersPinDriverConsumer couples the driver contract to
// the Java consumer: reverting the choice-queue integration or its ordered
// library reconstruction must fail even when the generator output is unchanged.
func TestScriptedLibraryAnswersPinDriverConsumer(t *testing.T) {
	source, err := os.ReadFile("../../xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, required := range []string{
		"public boolean scry(int value, Ability source, Game game)",
		"public Player.SurveilResult doSurveil(int value, Ability source, Game game)",
		"scriptedLibrarySelection(cards, game)",
		"List<String> queue = librarySelectionQueue(getChoices(), getTargets(),",
		"static List<String> librarySelectionQueue(List<String> choices, List<String> targets,",
		"TestPlayer.CHOICE_SKIP.equals(answer)",
		"getLibrary().getCards(game)",
		"if (cards.contains(card.getId()))",
		"for (Card card : lookedAtOrder)",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("ScenarioReplay.java no longer implements scripted library answer contract %q", required)
		}
	}
}

// TestArrangeAnswersMatchScriptedSurveilQueue pins the choice-queue sequence
// consumed by scry/surveil: selection, optional terminator, then order labels.
func TestArrangeAnswersMatchScriptedSurveilQueue(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange",
		Options: 3, Min: 0, Max: 2,
		Picks: []string{"Forest"}, PickIdx: []int{1}, PickKinds: []string{"graveyard"},
	}
	got := xanswers([]rules.OracleDecision{d}, 1, nil, nil)
	want := [][]XAnswer{{
		{Seat: 0, Kind: "choice", Value: "Forest"},
		{Seat: 0, Kind: "choice", Value: "[choice_skip]"},
		{Seat: 0, Kind: "choice", Value: "Forest"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("arrange xanswers = %#v, want %#v", got, want)
	}
}

// TestArrangeAllToBottomNamesTheCards: a scry that keeps nothing names the cards it
// sends away, since XMage's selection is what goes and a skip keeps them all.
func TestArrangeAllToBottomNamesTheCards(t *testing.T) {
	d := rules.OracleDecision{Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange", ArrangeKind: "bottom", Min: 0, Max: 1,
		Options: 1, ArrangeLabels: []string{"Exclusion Mage"}}
	got := xanswers([]rules.OracleDecision{d}, 1, nil, nil)
	if len(got) != 1 || len(got[0]) != 1 || got[0][0].Value != "Exclusion Mage" || got[0][0].Kind != "choice" {
		t.Fatalf("got %+v", got)
	}
}

func TestRetimeEntryTargets(t *testing.T) {
	steps := []Step{{Op: "pass"}, {Op: "resolve"}}
	snap := func(step string) rules.OracleSnapshot { return rules.OracleSnapshot{Step: step} }
	target := rules.OracleDecision{Step: 1, GorgeKind: "target"}
	moved := []rules.OracleSnapshot{snap("main1"), snap("begin-combat"), snap("begin-combat")}
	if got := RetimeEntryTargets(steps, moved, []rules.OracleDecision{target}); got[0].Step != 0 {
		t.Errorf("a trigger of the step entered: step %d", got[0].Step)
	}
	stayed := []rules.OracleSnapshot{snap("main1"), snap("main1"), snap("main1")}
	if got := RetimeEntryTargets(steps, stayed, []rules.OracleDecision{target}); got[0].Step != 1 {
		t.Errorf("a target asked as something resolves: step %d", got[0].Step)
	}
	choice := rules.OracleDecision{Step: 1, GorgeKind: "choose"}
	if got := RetimeEntryTargets(steps, moved, []rules.OracleDecision{choice}); got[0].Step != 1 {
		t.Errorf("not a target: step %d", got[0].Step)
	}
}
