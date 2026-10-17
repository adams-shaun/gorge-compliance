package oraclegen

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// xanswers is xanswersSetup without a scenario's setup: a decision recorded
// before step zero is retimed as setup_choice only when the caller knows the
// setup-placed permanents (see triggerOrderSetupPosed), so callers with no
// scenario keep the pre-retiming behaviour. XAnswers and the direct tests use
// this form.
func xanswers(ds []rules.OracleDecision, steps int, modes map[string]int, castSteps map[int]bool) [][]XAnswer {
	return xanswersSetup(ds, steps, modes, castSteps, nil, nil)
}

// triggerOrderSetupPosed reports whether a trigger-order decision is posed by
// XMage during the setup placement, before the recorded step's answers are
// read: gorge recorded it before the first gameplay step (Step < 0) and every
// pick's source is a permanent the scenario's setup put on the battlefield.
// Such an order pends while build() places the seeded permanents, and XMage's
// chooseTriggeredAbility ask fires before any gameplay answer is consumed, so
// the name answers must be queued as setup_choice (xmage_answers[0], before
// build()) or the ask falls to XMage's pending-list fallback and the stack is
// ordered by luck.
//
// A same-source order (two triggers off one permanent) cannot be answered by
// name, and Forge's verbatim rule text is not XMage's rule wording: queued as
// setup_choice it is never consumed and derails the as-enters dialogs that
// share the queue (measured on Ashling, Rekindled and Gathering Stone). Such
// an order is retimed only when xmageTriggerOrderKeys can key its picks in
// XMage's rule-text form, which the order ask itself consumes. A pick without
// a scenario ref cannot be placed either, so it fails the predicate.
func triggerOrderSetupPosed(d rules.OracleDecision, setup map[string]Seat) bool {
	if d.Step >= 0 || d.GorgeKind != "trigger_order" || len(d.Picks) < 2 {
		return false
	}
	if !triggerOrderNamesDistinct(d) {
		if _, ok := xmageTriggerOrderKeys(d); !ok {
			return false
		}
	}
	if len(d.PickRefs) < len(d.Picks) {
		return false
	}
	for k := range d.Picks {
		ref := d.PickRefs[k]
		if !isScenarioRefShaped(ref) {
			return false
		}
		seat, ok := setup[ref[:strings.IndexByte(ref, ':')]]
		if !ok || !slices.Contains(seat.Battlefield, oraclediffRefName(ref)) {
			return false
		}
	}
	return true
}

// setupTriggerOrderKeys is the setup_choice queue of a setup-posed order
// (triggerOrderSetupPosed): each pick's source name when the names are
// distinct, else the XMage rule-text keys. The last pick is never consumed.
func setupTriggerOrderKeys(d rules.OracleDecision) []string {
	if !triggerOrderNamesDistinct(d) {
		keys, _ := xmageTriggerOrderKeys(d)
		return keys
	}
	keys := make([]string, 0, len(d.Picks))
	for k := 0; k+1 < len(d.Picks); k++ {
		keys = append(keys, triggerOrderChoice(d, k))
	}
	return keys
}
