package xprep

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestPrepareCraftMaterialTargetQueue pins the Prepare path, not just the
// activate template: Prepare recomputes XAnswersForScenario from the observed
// decisions, so the Craft material's queue must be derived there too. XMage's
// CraftAbility pays the material through chooseTarget (the TARGET queue), while
// every other fixed-size exile cost pays through makeChoose (the choice
// queue). A template-only fix is discarded here; this is the lane the
// compliance pass replays (xp-xmage-exilecost-choice-queue, Jade Seedstones).
func TestPrepareCraftMaterialTargetQueue(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	it, skip := templates.ItemFor(reg, "Jade Seedstones", "activate#0.0")
	if skip != nil {
		t.Fatalf("ItemFor(Jade Seedstones activate#0.0): %s", skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("gorge fails: %v", res.Fails)
	}
	prep, ref := Prepare(reg, sp, lower.Lowered{Item: it}, res)
	if ref != nil {
		t.Fatalf("refused: %s: %s", ref.Reason, ref.Detail)
	}
	want := []oraclegen.XAnswer{{Seat: 0, Kind: "target", Value: "Colossal Dreadmaw"}}
	if len(prep.XAnswers) == 0 || !reflect.DeepEqual(prep.XAnswers[0], want) {
		t.Fatalf("prepared craft material answers = %+v, want %+v", prep.XAnswers, want)
	}
}

// TestPrepareGroupExilePerCardChoice pins the other exiling-cost shape Prepare
// must get right: an open-ended group exile (The Capitoline Triad's "one or
// more historic cards ... total mana value 30 or greater") is paid through
// getTargets().choose, the CHOICE queue, but one card per choose iteration
// rather than one joined dialog. The template handled this for generated
// items; Prepare's recomputation needs the same answer shape.
func TestPrepareGroupExilePerCardChoice(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	it, skip := templates.ItemFor(reg, "The Capitoline Triad", "activate#0.0")
	if skip != nil {
		t.Fatalf("ItemFor(The Capitoline Triad activate#0.0): %s", skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("gorge fails: %v", res.Fails)
	}
	prep, ref := Prepare(reg, sp, lower.Lowered{Item: it}, res)
	if ref != nil {
		t.Fatalf("refused: %s: %s", ref.Reason, ref.Detail)
	}
	want := []oraclegen.XAnswer{
		{Seat: 0, Kind: "choice", Value: "Blightsteel Colossus"},
		{Seat: 0, Kind: "choice", Value: "Darksteel Colossus"},
		{Seat: 0, Kind: "choice", Value: "Myr Battlesphere"},
	}
	if len(prep.XAnswers) == 0 || !reflect.DeepEqual(prep.XAnswers[0], want) {
		t.Fatalf("prepared group exile answers = %+v, want %+v", prep.XAnswers, want)
	}
}
