package templates_test

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/engine/rules"
)

// Fireglass Mentor's "Choose one of them" over the Dig'ed exiled cards. The
// corpus marks that ChooseCard Mandatory$ (Card-Forge/forge#12481), so gorge
// poses it 1..1 and takes the first exiled card by its own default: the
// level-B trigger item must carry no decline and no empty answer on gorge's
// side, and must script XMage the same first card -- never a skip, which
// XMage's mandatory 1..1 TargetCardInExile rejects ("Wrong skip command found
// - it can be used with up to targets, but used in TargetCardInExile, from 1
// to 1").
func TestTriggerExileLookPickForcesFirstCard(t *testing.T) {
	reg := corpusReg(t)
	c, ok := reg.Lookup("Fireglass Mentor")
	if !ok {
		t.Fatal("precondition: Fireglass Mentor is not in the corpus")
	}
	var req levelb.Requirement
	found := false
	for _, r := range levelb.Requirements(c) {
		if r.Key == "trigger#0.0" {
			req, found = r, true
		}
	}
	if !found {
		t.Fatal("precondition: Fireglass Mentor has no trigger#0.0 requirement")
	}
	it, skip := templates.GenerateB(reg, "Fireglass Mentor", req)
	if skip != nil {
		t.Fatalf("GenerateB: %s", skip.Reason)
	}

	// gorge's side: the trigger resolve step never declines the look pick or
	// answers it empty (the pre-Mandatory shape was an explicit choose of the
	// first exiled card; Mandatory leaves the first-option default to gorge).
	resolveAt := -1
	for i, s := range it.Scenario.Steps {
		if s.Op == "resolve" {
			resolveAt = i
		}
		for _, a := range s.Answers {
			if a.Kind != "choose" {
				continue
			}
			if len(a.Pick) == 0 {
				t.Fatalf("step %d: gorge answer %+v declines the Mandatory look pick", i, a)
			}
			for _, p := range a.Pick {
				if p == "" {
					t.Fatalf("step %d: gorge answer %+v picks an empty label", i, a)
				}
			}
		}
	}
	if resolveAt < 0 {
		t.Fatalf("precondition: no resolve step in %+v", it.Scenario.Steps)
	}

	// XMage's scripted answer: the looked-at card by name, never a skip.
	var gotPick, gotSkip bool
	for _, a := range it.XAnswers[resolveAt] {
		if a.Seat == 0 && (a.Kind == "choice" || a.Kind == "target") && a.Value == "Wastes" {
			gotPick = true
		}
		if a.Value == "[target_skip]" || a.Value == "[choice_skip]" {
			gotSkip = true
		}
	}
	if !gotPick {
		t.Fatalf("xmage_answers %+v do not name the exiled card", it.XAnswers[resolveAt])
	}
	if gotSkip {
		t.Fatalf("xmage_answers %+v still carry a skip", it.XAnswers[resolveAt])
	}
}

// Whiskervale Forerunner's "you may reveal a creature card ... you may put
// it onto the battlefield": XMage poses the one-card pick as a mandatory 1..1
// makeChoose over the looked-at eligible creatures (TargetCardInLibrary), which
// rejects every skip token, so the scenario is re-run with gorge taking the
// card and the choice queue names it, followed by the "put it onto the
// battlefield?" chooseUse (lb-xdefaults; the old lone target skip left
// XMage's AI to pick and moved its library count). The cast-side twin of the
// decision shape (Break Out) keeps the measured pair; only the card-side-proven
// look chooser is forced.
func TestTriggerLookedPickDeclineIsForcedChoice(t *testing.T) {
	reg := corpusReg(t)
	c, ok := reg.Lookup("Whiskervale Forerunner")
	if !ok {
		t.Fatal("precondition: Whiskervale Forerunner is not in the corpus")
	}
	var req levelb.Requirement
	found := false
	for _, r := range levelb.Requirements(c) {
		if r.Key == "trigger#0.0" {
			req, found = r, true
		}
	}
	if !found {
		t.Fatal("precondition: Whiskervale Forerunner has no trigger#0.0 requirement")
	}
	it, skip := templates.GenerateB(reg, "Whiskervale Forerunner", req)
	if skip != nil {
		t.Fatalf("GenerateB: %s", skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	picked := 0
	for _, d := range res.Decisions {
		if d.Resume != "choice" || d.Options != 1 || d.Step < 0 || d.Step >= len(it.XAnswers) {
			continue
		}
		if len(d.Picks) == 0 {
			t.Fatalf("looked pick at step %d still declined: the replay must take the card", d.Step)
		}
		picked++
		got := it.XAnswers[d.Step]
		if len(got) == 0 || got[0] != (oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: "Grizzly Bears"}) {
			t.Fatalf("looked-pick step %d answers %+v, want the choice-queue pick of Grizzly Bears first", d.Step, got)
		}
		for _, a := range got {
			if a.Value == "[target_skip]" || a.Value == "[choice_skip]" || a.Value == "no" && a.Kind == "choice" && len(got) == 1 {
				t.Fatalf("step %d answers %+v carry a skip XMage's mandatory ask rejects", d.Step, got)
			}
		}
	}
	if picked == 0 {
		t.Fatal("precondition: the replay carries no one-card look pick")
	}
}

// Bebop & Rocksteady's "sacrifice a permanent unless you discard a card":
// the decline (the pay branch is unreachable, so gorge offered only
// "Don't pay") must be scripted as the target queue's skip — XMage poses the
// cost's own ask on the target queue and never a chooseUse, so the "no"
// boolean has nothing to answer ("Found wrong choice command").
func TestTriggerSacrificeUnlessDiscardDeclineIsTargetSkip(t *testing.T) {
	reg := corpusReg(t)
	c, ok := reg.Lookup("Bebop & Rocksteady")
	if !ok {
		t.Fatal("precondition: Bebop & Rocksteady is not in the corpus")
	}
	for _, key := range []string{"trigger#0.0", "trigger#0.1"} {
		var req levelb.Requirement
		found := false
		for _, r := range levelb.Requirements(c) {
			if r.Key == key {
				req, found = r, true
			}
		}
		if !found {
			t.Fatalf("precondition: Bebop & Rocksteady has no %s requirement", key)
		}
		it, skip := templates.GenerateB(reg, "Bebop & Rocksteady", req)
		if skip != nil {
			t.Fatalf("GenerateB(%s): %s", key, skip.Reason)
		}
		res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
		if err != nil {
			t.Fatalf("%s: replay: %v", key, err)
		}

		// Precondition: the replay really carries the declined unless-pay
		// ask (the pay branch unreachable, one option offered) whose script
		// this asserts — the step's only answer is the target skip, and no
		// "no" boolean remains for XMage to reject.
		declined := 0
		for _, d := range res.Decisions {
			if d.Resume != "unless_pay" || d.Options != 1 || len(d.Picks) != 1 ||
				d.Step < 0 || d.Step >= len(it.XAnswers) {
				continue
			}
			declined++
			got := it.XAnswers[d.Step]
			if len(got) != 1 || got[0] != (oraclegen.XAnswer{Seat: 0, Kind: "target", Value: "[target_skip]"}) {
				t.Fatalf("%s: unless step %d answers %+v, want the lone target skip", key, d.Step, got)
			}
			for _, a := range got {
				if a.Kind == "choice" && a.Value == "no" {
					t.Fatalf("%s: unless step %d still scripts the chooseUse no: %+v", key, d.Step, got)
				}
			}
		}
		if declined == 0 {
			t.Fatalf("%s: the replay carries no declined unless-pay ask", key)
		}
	}
}
