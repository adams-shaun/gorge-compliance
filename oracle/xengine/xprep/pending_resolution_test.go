package xprep

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// stormbroodWindow is the xproof find's Disruptive Stormbrood window: five
// Forests pay for the spell, the cast and one pass end the cut, and the
// "destroy up to one target artifact or enchantment" trigger resolves only
// after it. The stored item has no answer for that ask, so XMage strict-missed
// "Missing TARGET def" with two legal candidates.
func stormbroodWindow() oraclegen.Item {
	zero := 0
	forests := make([]string, 20)
	islands := make([]string, 20)
	for i := range forests {
		forests[i] = "Forest"
		islands[i] = "Island"
	}
	return oraclegen.Item{
		ID:       "xs:XP:disruptive-stormbrood-petty-revenge:f0t0:s2",
		Template: "xproof-start",
		Scenario: oraclegen.Scenario{
			Name: "xproof start Disruptive Stormbrood // Petty Revenge f0/t0 seed 2",
			Turn: 1,
			Why:  "A gorge game replayed from turn 1's first main phase.",
			Setup: map[string]oraclegen.Seat{
				"p0": {
					Battlefield: []string{"Forest", "Forest", "Forest", "Forest", "Forest"},
					Hand:        []string{"Disruptive Stormbrood"},
					LibraryTop:  forests,
				},
				"p1": {
					Battlefield: []string{"Fountain of Youth", "Seal of Removal"},
					Graveyard:   []string{"Ugin's Conjurant"},
					LibraryTop:  islands,
				},
			},
			Steps: []oraclegen.Step{
				{Op: "activate", Seat: 0, Card: "p0:Forest", AbilityIndex: &zero},
				{Op: "activate", Seat: 0, Card: "p0:Forest#2", AbilityIndex: &zero},
				{Op: "activate", Seat: 0, Card: "p0:Forest#3", AbilityIndex: &zero},
				{Op: "activate", Seat: 0, Card: "p0:Forest#4", AbilityIndex: &zero},
				{Op: "activate", Seat: 0, Card: "p0:Forest#5", AbilityIndex: &zero},
				{Op: "cast", Seat: 0, Card: "p0:Disruptive Stormbrood"},
				{Op: "pass", Seat: 0},
			},
		},
	}
}

func replayStormbrood(t *testing.T, reg *cards.Registry, it oraclegen.Item) rules.OracleResult {
	t.Helper()
	g, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatalf("gorge replay: %v", err)
	}
	if len(g.Fails) != 0 {
		t.Fatalf("gorge replay failed: %v", g.Fails)
	}
	return g
}

// TestPendingResolutionScriptsTheDecline: the window's own result holds no
// decision for the trigger (the spell is still on the stack at the last
// checkpoint). The continuation's own decline is retimed onto the last step
// and scripted as the target skip, so the driver neither guesses a pick nor
// strict-misses.
func TestPendingResolutionScriptsTheDecline(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := stormbroodWindow()
	g := replayStormbrood(t, reg, it)
	if len(g.Decisions) != 0 {
		t.Fatalf("the cut already holds %d decisions: %+v", len(g.Decisions), g.Decisions)
	}
	if len(g.Snapshots) != len(it.Steps)+1 || len(g.Snapshots[len(g.Snapshots)-1].Stack) == 0 {
		t.Fatalf("precondition: the spell must still be on the stack at the cut, snapshots %d", len(g.Snapshots))
	}
	ds := pendingResolutionDecisions(reg, it.Scenario, g)
	if len(ds) != 1 {
		t.Fatalf("pending decisions = %d, want the trigger's one target ask: %+v", len(ds), ds)
	}
	last := len(it.Steps) - 1
	d := ds[0]
	if d.Step != last || d.Kind != "target" || d.Seat != 0 || d.Min != 0 || d.Max != 1 ||
		d.Options != 2 || len(d.PickRefs) != 0 {
		t.Fatalf("pending decision = %+v, want the retimed declined 0..1 target ask on step %d", d, last)
	}
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	prep, ref := Prepare(reg, sp, lower.Lowered{Item: it}, g)
	if ref != nil {
		t.Fatalf("Prepare refused the stored item: %+v", ref)
	}
	want := oraclegen.XAnswer{Seat: 0, Kind: "target", Value: "[target_skip]"}
	if len(prep.XAnswers) != len(it.Steps) || len(prep.XAnswers[last]) != 1 || prep.XAnswers[last][0] != want {
		t.Fatalf("xmage_answers = %+v, want one %+v on step %d", prep.XAnswers, want, last)
	}
}

// TestPendingResolutionLeavesAResolvedWindowAlone: a window that already
// carries the pass that resolves the spell holds the trigger's own decision,
// so there is nothing to complete.
func TestPendingResolutionLeavesAResolvedWindowAlone(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := stormbroodWindow()
	it.Steps = append(it.Steps,
		oraclegen.Step{Op: "pass", Seat: 1},
		oraclegen.Step{Op: "pass", Seat: 0},
		oraclegen.Step{Op: "pass", Seat: 1},
		oraclegen.Step{Op: "pass", Seat: 0},
	)
	g := replayStormbrood(t, reg, it)
	if len(g.Snapshots) != len(it.Steps)+1 || len(g.Snapshots[len(g.Snapshots)-1].Stack) != 0 {
		t.Fatalf("precondition: the continuation must have resolved the spell")
	}
	if ds := pendingResolutionDecisions(reg, it.Scenario, g); len(ds) != 0 {
		t.Fatalf("a resolved window still completed %+v", ds)
	}
}
