package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// answerAt is the answer list of step i, or nil.
func answerAt(it oraclegen.Item, i int) []oraclegen.XAnswer {
	if i < 0 || i >= len(it.XAnswers) {
		return nil
	}
	return it.XAnswers[i]
}

// TestLevelBPerPlayerTargetAsk: Kaya, Spirits' Justice's -2 takes its
// per-player "exile up to one target creature that player controls" targets
// when it is activated (CR 602.2b), one per controller, so the generated
// item names both on the activate step and poses no resolve-time ask for
// XMage to close with a skip. The item leads with the CR 606.3 empty-stack
// resolve every loyalty setup carries (the source's own entry trigger may be
// pending).
func TestLevelBPerPlayerTargetAsk(t *testing.T) {
	it := levelBItem(t, "Kaya, Spirits' Justice", "activate#0.2")
	if len(it.Scenario.Steps) != 3 ||
		it.Scenario.Steps[0].Op != "resolve" ||
		it.Scenario.Steps[1].Op != "activate" || it.Scenario.Steps[2].Op != "resolve" {
		t.Fatalf("steps = %v, want the empty-stack resolve, activate, resolve", it.Scenario.Steps)
	}
	cast := it.Scenario.Steps[1]
	if strings.Join(cast.Targets, "|") != "p0:Grizzly Bears|p1:Grizzly Bears" {
		t.Fatalf("activate targets = %v, want one creature per controller", cast.Targets)
	}
	if as := answerAt(it, 2); len(as) != 0 {
		t.Fatalf("resolve answers = %+v, want none: every target was taken on activation", as)
	}
	for i, list := range it.XAnswers {
		for _, a := range list {
			if a.Kind == "choice" && a.Value == "[choice_skip]" {
				t.Fatalf("step %d still scripts a choice skip: %+v", i, list)
			}
		}
	}
}

// TestLevelBTransformedTargetRef: Jill, Shiva's Dominant's activation exiles
// her and returns her transformed (Shiva, Warden of Ice), whose Saga chapter
// targets a creature. The pick's option label names the current face while
// its ref is the front name. The returned permanent is a new XMage object, so
// the setup alias the exact ref binds no longer names it (XMage left the
// "@p0:Jill, Shiva's Dominant" target unused); the current face's name,
// unique among the options, does.
func TestLevelBTransformedTargetRef(t *testing.T) {
	it := levelBItem(t, "Jill, Shiva's Dominant", "activate#0.0")
	as := answerAt(it, 1)
	if len(as) != 1 || as[0].Kind != "target" {
		t.Fatalf("resolve answers = %+v, want one target", as)
	}
	if as[0].Value != "Shiva, Warden of Ice" {
		t.Fatalf("target answer = %q, want the current face's name Shiva, Warden of Ice", as[0].Value)
	}
}

// TestLevelBTapOrUntapAnswer: Inverted Iceberg's back-face attack trigger
// targets an artifact or creature and then asks XMage's chooseUse to tap or
// untap it. The target is the transformed permanent (answered by its current
// face's name, unique among the options) and the election is the boolean XMage asks, not gorge's option label.
func TestLevelBTapOrUntapAnswer(t *testing.T) {
	it := levelBItem(t, "Inverted Iceberg", "trigger#1.0")
	first := answerAt(it, 0)
	if len(first) != 1 || first[0].Kind != "target" || first[0].Value != "Iceberg Titan" {
		t.Fatalf("attack answers = %+v, want the transformed permanent's current (unique) name", first)
	}
	second := answerAt(it, 1)
	if len(second) != 1 || second[0].Kind != "choice" || (second[0].Value != "yes" && second[0].Value != "no") {
		t.Fatalf("resolve answers = %+v, want the tap/untap boolean", second)
	}
	for _, a := range second {
		if strings.HasPrefix(a.Value, "Untap ") || strings.HasPrefix(a.Value, "Tap ") {
			t.Fatalf("tap/untap answer is gorge's label, not XMage's boolean: %+v", a)
		}
	}
}

// TestLevelBSurveyMechanQueueOrder pins the generator input the driver's
// later-player hiding fixes: the activation names the creature (the any-target
// damage slot) before the player (the TargetPlayer draw slot), and scripts no
// separate answers.
func TestLevelBSurveyMechanQueueOrder(t *testing.T) {
	it := levelBItem(t, "Survey Mechan", "activate#0.0")
	if len(it.Scenario.Steps) != 2 || it.Scenario.Steps[0].Op != "activate" {
		t.Fatalf("steps = %v, want activate then resolve", it.Scenario.Steps)
	}
	if got := strings.Join(it.Scenario.Steps[0].Targets, "|"); got != "p1:Grizzly Bears|p1" {
		t.Fatalf("activate targets = %q, want the creature then the player in slot order", got)
	}
	for i, list := range it.XAnswers {
		if len(list) != 0 {
			t.Fatalf("step %d carries answers %+v; the driver queues the step targets itself", i, list)
		}
	}
}
