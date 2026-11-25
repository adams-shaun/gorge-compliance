package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// Echo (CR 702.30a) and cumulative upkeep (CR 702.24a) both pose a
// pay-or-sacrifice election, then (for a non-mana cost) the object pick the
// payment makes. XMage answers the election with chooseUse, not a labelled
// choice: EchoEffect asks controller.chooseUse("Pay " + cost.getText() + "?")
// and CumulativeUpkeepEffect asks player.chooseUse(<totalCost.getText()>+"?")
// for a non-mana cost ("Pay "+...+?" for mana). The sacrifice behind the yes is
// SacrificeTargetCost's TargetSacrifice, read from the TARGET queue (the
// driver moves a cost target's answer to the choose queue). Routing these
// through the generic choose_n arm leaves the gorge label on the chooseUse
// queue and the picks as a choice alias, which chooseUse cannot read
// (measured xpt-bcbe2139: Skizzik Surger, "Sacrifice 2 land" then two
// Mountains; Polar Kraken, "Sacrifice 1 land per age ..." then an Island).

// echoCumulativeElectionRole classifies the election's picked face: "pay" for
// any pay option (including the manual mana route and a pure-mana echo),
// "sacrifice" for the decline, "" for any other decision. Every pay face maps
// to XMage's boolean regardless of whether the cost is mana: XMage asks its
// chooseUse first in both EchoEffect and CumulativeUpkeepEffect, and pays the
// mana itself when the cost is mana. The engine kind names are the closed
// election vocabulary (engine/rules/echo.go and cumulative.go), so a new face
// is a missing case here, not a text heuristic.
func echoCumulativeElectionRole(kind string) string {
	switch kind {
	case "echo_pay", "echo_pay_manual", "cumulative_pay", "cumulative_pay_manual":
		return "pay"
	case "echo_sac", "cumulative_sac":
		return "sacrifice"
	}
	return ""
}

// echoCumulativeElectionAnswer is XMage's chooseUse answer to d's election
// ("yes" to pay, "no" to sacrifice), or "" for any other decision.
func echoCumulativeElectionAnswer(d rules.OracleDecision) string {
	if d.Kind != "choose_n" || len(d.Picks) != 1 {
		return ""
	}
	switch echoCumulativeElectionRole(pickKind(d, 0)) {
	case "pay":
		return "yes"
	case "sacrifice":
		return "no"
	}
	return ""
}

// echoActionIsSacrifice reports whether the non-mana echo action ask at i pays
// a sacrifice rather than a discard. The engine uses one action kind
// (echo_action_pick) for both (engine/rules/echo.go's echoActionAsk), and the
// election's own pay phrase is the distinguishing evidence: the engine's
// cumulativeActionPhrase spells the Sac arm "sacrifice N ..." and the Discard
// arm "discard N card...". XMage's SacrificeTargetCost reads the TARGET queue
// while DiscardTargetCost reads makeChoose, so only the sacrifice arm belongs
// in echoCumulativeActionAnswers.
func (r *answerRouting) echoActionIsSacrifice(i int) bool {
	d := r.ds[i]
	if i == 0 {
		return false
	}
	e := r.ds[i-1]
	if !sameAsker(e, d) || e.Kind != "choose_n" || len(e.Picks) != 1 ||
		echoCumulativeElectionRole(pickKind(e, 0)) == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(e.Picks[0])), "sacrifice ")
}

// echoCumulativeActionPick reports whether d is the TARGET-queue object pick
// of a non-mana echo/cumulative sacrifice. cumulative_action_sac is
// unambiguously the Sac arm; echo_action_pick is shared with the discard arm,
// so its election phrase gates it. engine/rules' echoActionAsk and
// continueCumulativeAction are the only producers of these kinds.
func (r *answerRouting) echoCumulativeActionPick(i int) bool {
	d := r.ds[i]
	if d.Kind != "choose_n" || len(d.Picks) == 0 || len(d.PickKinds) < len(d.Picks) {
		return false
	}
	sac := true
	for k := range d.Picks {
		switch pickKind(d, k) {
		case "cumulative_action_sac":
		case "echo_action_pick":
			sac = sac && r.echoActionIsSacrifice(i)
		default:
			return false
		}
	}
	return sac
}

// echoCumulativeActionAnswers spells the action's picks as ONE "^"-joined
// target answer, the way XMage's TargetSacrifice consumes them: its
// SacrificeTargetCost chooses one Target object whose TestPlayer selection
// splits the record into every target. Emitting one answer per pick leaves the
// later ones on the target queue (measured leftover "(Targets of PlayerA)
// Count are not equal (found [@p0:Mountain#2])"). Each part keeps the same-name
// alias the driver binds.
func echoCumulativeActionAnswers(d rules.OracleDecision) []XAnswer {
	parts := make([]string, 0, len(d.Picks))
	for k, label := range d.Picks {
		v := targetPickValue(d, k, label)
		if isScenarioRefShaped(v) {
			// The joined record bypasses the single-ref targetName lookup,
			// so each part carries the driver's "@<ref>" alias form itself.
			v = "@" + v
		}
		parts = append(parts, v)
	}
	return []XAnswer{{d.Seat, "target", strings.Join(parts, "^")}}
}
