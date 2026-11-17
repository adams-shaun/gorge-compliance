package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestRandomLibraryPickUsesOneTopCard: Getaway Barrel's trigger reveals the
// top thirteen cards and puts a RANDOM creature among them onto the
// battlefield. A varied library top made gorge and XMage pick different
// creatures with their own RNGs; the scenario now seeds one repeated creature
// so every random pick is the same object.
func TestRandomLibraryPickUsesOneTopCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it, _ := generateServed(t, reg, "Getaway Barrel", "trigger#0.0")
	top := it.Scenario.Setup["p0"].LibraryTop
	if len(top) == 0 {
		t.Fatal("Getaway Barrel scenario seeds no library top; the reveal has no random-pick candidates")
	}
	for i, name := range top {
		if name != top[0] {
			t.Fatalf("library_top = %v: entry %d %q differs from %q, so the random pick is not deterministic",
				top, i, name, top[0])
		}
	}
}

// TestModalCastFallbackTargetIsAStepTarget: Choreographed Sparks' mode target
// (the Shock spell on the stack) came from the runner's cast fallback, so the
// generator left it in xmage_answers. XMage's target queue then carried
// "Shock" past the cast until a later any-target ask rejected it. The target
// must be the cast step's own Targets, like every other cast step's.
func TestModalCastFallbackTargetIsAStepTarget(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it, _ := generateServed(t, reg, "Choreographed Sparks", "static#0.0")
	if len(it.Scenario.Steps) < 3 || it.Scenario.Steps[2].Op != "cast" {
		t.Fatalf("precondition: steps = %+v, want the Shock/Choreographed Sparks/Reverberate casts", it.Scenario.Steps)
	}
	cast := it.Scenario.Steps[2]
	if len(cast.Targets) != 1 || cast.Targets[0] != "p0:Shock" {
		t.Fatalf("Choreographed Sparks cast targets = %v, want [p0:Shock] on the step", cast.Targets)
	}
	if len(it.XAnswers) > 2 {
		for _, a := range it.XAnswers[2] {
			if a.Kind == "target" {
				t.Errorf("xmage_answers[2] still scripts target %q; the step's own Targets must carry it", a.Value)
			}
		}
	}
}
