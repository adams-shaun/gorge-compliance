package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// TurnedFaceUpSub is the level-B sub-family of a TurnFaceUp trigger whose
// filter names the source card itself ("When this creature is turned face
// up"). TurnedFaceUpOtherSub is the family whose filter names another
// permanent ("Whenever a permanent you control is turned face up"). Both
// share the cause shape -- a disguise cast followed by the battlefield
// turn-face-up special action -- so the recipe in
// oracle/compliance/oraclegen/templates is one file for the pair. Exported so the
// recipe and this classifier share one spelling.
const (
	TurnedFaceUpSub      = "trigger.turned-face-up"
	TurnedFaceUpOtherSub = "trigger.turned-face-up-other"
)

// classifyTurnFaceUpTrigger names the two TurnFaceUp sub-families: the source
// itself (Card.Self, or a comma list that includes it) and another permanent
// (Permanent, Permanent.YouCtrl, Detective.YouCtrl, ...). A ValidCause$
// qualifier is served only as "you turn ... face up" (SpellAbility.YouCtrl),
// which the recipe's own p0 special action is; any other qualifier stays a
// mode gap. ok is false for every other trigger.
func classifyTurnFaceUpTrigger(t *cards.Trigger) (sub string, ok bool) {
	if t.ModeKind() != cards.TriggerTurnFaceUp {
		return "", false
	}
	if cause := strings.TrimSpace(t.ParamStr(cards.PKValidCause)); cause != "" {
		// "Whenever YOU turn a permanent face up" (Growing Dread: ValidCause$
		// SpellAbility.YouCtrl): the recipe's cause is p0's own turn-face-up
		// special action, which is exactly the turn-up this names. Every
		// other cause qualifier stays a gap. Static$ True bookkeeping lines
		// (Oblivious Bookworm's remember) never reach the stack.
		if !strings.EqualFold(cause, "SpellAbility.YouCtrl") || strings.EqualFold(t.ParamStr(cards.PKStatic), "True") {
			return "", false
		}
	}
	v := t.ParamStr(cards.PKValidCard)
	if v == "" {
		// A filter-less TurnFaceUp trigger names the source the way a
		// Card.Self one does; the recipe then reports a named skip if the
		// card carries no morph family.
		return TurnedFaceUpSub, true
	}
	if namesSelf(v) {
		return TurnedFaceUpSub, true
	}
	return TurnedFaceUpOtherSub, true
}
