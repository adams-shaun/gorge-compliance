package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// xmageTapThenUntapFaces names the TapOrUntap faces whose XMage effect asks
// "Tap <name>?" first and "Untap <name>?" only on a no, whatever the target's
// state, instead of MayTapOrUntapTargetEffect's single state-dependent
// chooseUse. A single "yes" there taps the target, so an untap pick must be
// scripted "no, yes".
//
// Membership is proven against the XMage source at XMAGE_REF fdde07931f. Of
// the corpus's TapOrUntap faces, every one XMage implements uses
// MayTapOrUntapTargetEffect or a hand-written state-dependent single ask
// (Hidden Strings, Curse of Inertia, Sword of the Paruns), except:
//
//	ToilsOfNightAndDay  ToilsOfNightAndDayEffect: chooseUse("Tap X?") then, on no, chooseUse("Untap X?"), per target
//
// Dee Kay, Faerie Dragon, Sewer-veillance Cam and Twiddlestick Charger have no
// XMage card at the pin. TestTapThenUntapFacesCensus counts the set, so a new
// entry is a deliberate, source-proven edit.
var xmageTapThenUntapFaces = map[string]bool{
	"toils of night and day": true,
}

// tapThenUntapStep reports whether the TapOrUntap election posed at step
// comes from a face in xmageTapThenUntapFaces. The resolving object is the top
// of the step's before-checkpoint stack (snaps[step], top first); the step's
// own card is the fallback for a step whose snapshot shows no stack.
func tapThenUntapStep(sc Scenario, snaps []rules.OracleSnapshot, step int) bool {
	if step < 0 {
		return false
	}
	if step < len(snaps) && len(snaps[step].Stack) > 0 {
		return xmageTapThenUntapFaces[strings.ToLower(oraclediffRefName(snaps[step].Stack[0].Source))]
	}
	if step < len(sc.Steps) && sc.Steps[step].Card != "" {
		return xmageTapThenUntapFaces[strings.ToLower(oraclediffRefName(sc.Steps[step].Card))]
	}
	return false
}

// tapThenUntapChoices is the XMage answer sequence for a TapOrUntap pick on a
// tap-then-untap face: the state-changing tap is "yes"; the state-changing
// untap is "no" to "Tap X?" and "yes" to "Untap X?"; the no-op (option 1) is
// "no, no", which leaves the target as it is.
func tapThenUntapChoices(d rules.OracleDecision) []string {
	if len(d.PickIdx) == 0 || d.PickIdx[0] != 0 {
		return []string{"no", "no"}
	}
	if pickKind(d, 0) == "tap" {
		return []string{"yes"}
	}
	return []string{"no", "yes"}
}
