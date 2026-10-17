package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// TeamworkTappedSub is the teamwork-perspective tap trigger: Mode$ Taps with
// Teamwork$ True fires only when the source becomes tapped to PAY a teamwork
// cost (Agent Maria Hill, "Whenever Agent Maria Hill becomes tapped to pay a
// teamwork cost", CR 702.194). An attack or a tap spell taps the creature
// without paying any cost, so the ordinary trigger.tapped causes can never
// fire it; the recipe casts a teamwork spell electing the source as the tapped
// body. Exported so the oracle/compliance/oraclegen/templates recipe and the
// triggerFires dispatch share one spelling.
const TeamworkTappedSub = "trigger.tapped-teamwork"

// classifyTeamworkTrigger admits the one shape the teamwork recipe serves:
// the card itself (a creature) tapped for teamwork, with no gate the turn-1
// main-phase cast cannot satisfy. Every other trigger keeps its routing:
// classifyTapCombatTrigger rejects any Teamwork$ line, so this only moves a
// requirement out of the trigger.gap set, never between served families.
func classifyTeamworkTrigger(t *cards.Trigger) (sub string, ok bool) {
	if t.ModeKind() != cards.TriggerTaps || !strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKTeamwork)), "True") {
		return "", false
	}
	if t.HasParam(cards.PKCondition) || t.HasParam(cards.PKCheckSVar) || t.HasParam(cards.PKIsPresent) {
		return "", false
	}
	if !namesSelf(t.ParamStr(cards.PKValidCard)) {
		return "", false
	}
	if zones := t.ParamStr(cards.PKTriggerZones); zones != "" && !strings.EqualFold(zones, "Battlefield") {
		return "", false
	}
	return TeamworkTappedSub, true
}
