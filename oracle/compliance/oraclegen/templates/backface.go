// backface.go is the shared helper that lets a face-0 level-B template serve a
// requirement on a face after 0. The runner's setup places the named
// battlefield card on its back face (engine/rules/oracle_run.go, the "back_face"
// seat list), so a template that would otherwise put the card on the
// battlefield in face 0 marks it here instead. The setup shortcut is the whole
// point: a face-1 requirement is served without casting the front face and
// transforming.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// setupBackFace marks name as a back-face (face index 1) battlefield setup
// when the requirement lives on a face after 0. A face-0 requirement is left
// untouched, so every level-A and face-0 level-B scenario keeps its exact
// bytes. The card must already be in the seat's Battlefield list; the runner
// flips it there.
func setupBackFace(p0 *oraclegen.Seat, name string, req levelb.Requirement) {
	if req.Face > 0 {
		p0.BackFace = appendFixtureUnique(p0.BackFace, name)
	}
}

// faceKeepAnswers declines, during the setup drive, the face under test's own
// optional upkeep self-transform ("At the beginning of each upkeep, you may
// transform Aang, Master of Elements"): the drive passes turn 1's upkeep, and
// its first-option fallback accepts, so the card would be back on its front
// face before step 0 and the scenario would test the wrong face (and XMage's
// attack/block commands, which name the staged back face, find nothing).
func faceKeepAnswers(f *cards.Face) []oraclegen.Answer {
	if f == nil {
		return nil
	}
	for _, tr := range f.Triggers {
		if !strings.EqualFold(tr.Mode, "Phase") || !strings.EqualFold(tr.Params["Phase"], "Upkeep") ||
			tr.Params["OptionalDecider"] == "" || tr.Effect == nil || upkeepTriggerGated(tr) {
			continue
		}
		e := tr.Effect
		if e.API == "SetState" && strings.EqualFold(e.Params["Mode"], "Transform") && strings.EqualFold(e.Params["Defined"], "Self") {
			return []oraclegen.Answer{{Kind: "any", Pick: []string{"no"}}}
		}
	}
	return nil
}

// upkeepTriggerGated reports a trigger condition the setup board may not
// meet (Emet-Selch's IsPresent$ ... GE14 graveyard cards): the decline would
// then be an unconsumed answer.
func upkeepTriggerGated(tr cards.Trigger) bool {
	for _, k := range []string{"IsPresent", "CheckSVar", "Condition", "CheckDefinedPlayer"} {
		if tr.Params[k] != "" {
			return true
		}
	}
	return false
}

// eachUpkeep reports that the face's kept self-transform also fires on an
// opponent's upkeep ("At the beginning of each upkeep"): no ValidPlayer$
// restriction to its controller.
func eachUpkeep(f *cards.Face) bool {
	for _, tr := range f.Triggers {
		if strings.EqualFold(tr.Mode, "Phase") && strings.EqualFold(tr.Params["Phase"], "Upkeep") &&
			tr.Params["OptionalDecider"] != "" && !upkeepTriggerGated(tr) && tr.Params["ValidPlayer"] == "" {
			return true
		}
	}
	return false
}
