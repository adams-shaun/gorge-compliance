package oraclegen_test

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestSetupDriveKWChoiceScriptsChoice pins the setup-drive KWChoice class on
// the production generator, not on a synthetic decision: Gabriel Angelfire's
// setup-posed trigger/trigger.phase picks "Flying" while the drive runs before
// step zero, and XMage's GainsChoiceOfAbilitiesEffect asks that pick as a
// CHOICE ("Choose an ability to gain") over the keyword's lower-case rule
// text. Scripted on the numeric mode queue a setup_mode "-3" answers nothing
// and leaves the invalid sentinel for the next chooseMode.
func TestSetupDriveKWChoiceScriptsChoice(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Gabriel Angelfire")
	if !ok {
		t.Fatal("precondition: Gabriel Angelfire is not in the corpus")
	}
	var req levelb.Requirement
	found := false
	for _, r := range levelb.Requirements(c) {
		if r.Sub == "trigger.phase" {
			req, found = r, true
			break
		}
	}
	if !found {
		t.Fatalf("precondition: no trigger.phase requirement: %+v", levelb.Requirements(c))
	}
	it, skip := templates.GenerateB(reg, "Gabriel Angelfire", req)
	if skip != nil {
		t.Fatalf("GenerateB(%s): %s", req.Key, skip.Reason)
	}
	if len(it.XAnswers) == 0 || len(it.XAnswers[0]) == 0 {
		t.Fatalf("precondition: the generated item scripts no setup answers: %+v", it.XAnswers)
	}
	want := oraclegen.XAnswer{Seat: 0, Kind: "setup_choice", Value: "flying"}
	if got := it.XAnswers[0][0]; got != want {
		t.Fatalf("setup KWChoice answer = %+v, want %+v (xmage_answers = %+v)", got, want, it.XAnswers)
	}
	for _, step := range it.XAnswers {
		for _, a := range step {
			if a.Kind == "setup_mode" || a.Kind == "mode" {
				t.Fatalf("a KWChoice pick must not script a numeric mode: %+v", it.XAnswers)
			}
		}
	}
}
