// The crew-dependency fixture: a static that gives the creatures you control
// a new creature type and a base power/toughness has to be observed on a
// Vehicle that becomes a creature after the static entered. A Vehicle is not
// a creature until crewed, so the static's layer-4 type addition and layer-7b
// base P/T depend (CR 613.8a) on the crew effect that makes it an artifact
// creature: applying the crew effect changes which objects "creatures you
// control" covers, so the crew effect is applied first whatever the
// timestamps. A scenario that only holds a creature already on the
// battlefield never exercises the dependency, and both follower engines miss
// it (docs/upstream-bugs.md).
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// staticCrewDependencyFixture builds the fixture for a static whose affected
// filter is the plain "creatures you control" and whose effect adds a
// creature type and sets a literal base P/T, and names the Vehicle probe it
// crews: the Vehicle is placed beside the ordinary probe, crewed by the
// fixture's afterSteps once the static has resolved, so the final checkpoint
// holds it as a creature the static must have changed.
func staticCrewDependencyFixture(reg *cards.Registry, name string, st cards.Static, affected string) (staticFixture, string, bool) {
	if !plainCreaturesYouControl(affected) || st.ParamStr(cards.PKAddType) == "" {
		return staticFixture{}, "", false
	}
	if _, err := strconv.Atoi(st.ParamStr(cards.PKSetPower)); err != nil {
		return staticFixture{}, "", false
	}
	if _, err := strconv.Atoi(st.ParamStr(cards.PKSetToughness)); err != nil {
		return staticFixture{}, "", false
	}
	vehicle, vf, ok := staticCrewDependencyVehicle(reg)
	if !ok || strings.EqualFold(vehicle, name) {
		return staticFixture{}, "", false
	}
	steps, xab, taps, cost, ok := crewPrelude(vf, vehicle)
	if !ok {
		return staticFixture{}, "", false
	}
	return staticFixture{
		conditionPrelude: conditionPrelude{battlefield: taps},
		afterSteps:       steps,
		afterXab:         xab,
		reanswers:        true,
		activationCost:   cost,
	}, vehicle, true
}

// plainCreaturesYouControl reports that affected is exactly "Creature" plus
// "YouCtrl": no counter gate, subtype, state or controller qualifier narrows
// it, so every creature you control -- including one that only becomes a
// creature after the static resolves -- is affected.
func plainCreaturesYouControl(affected string) bool {
	words := affectedWords(affected)
	if len(words) != 2 {
		return false
	}
	creature, you := false, false
	for _, w := range words {
		switch strings.ToLower(w) {
		case "creature":
			creature = true
		case "youctrl":
			you = true
		default:
			return false
		}
	}
	return creature && you
}

// staticCrewDependencyVehicle returns the table's uncrewed Vehicle probe and
// its face. It is the same probe staticPlanFor uses for a filter that names
// "Vehicle", so the fixture adds no card the corpus does not already hold.
func staticCrewDependencyVehicle(reg *cards.Registry) (string, *cards.Face, bool) {
	for _, row := range staticProbeTable {
		if row.word != "Vehicle" {
			continue
		}
		c, ok := reg.Lookup(row.card)
		if !ok || len(c.Faces) == 0 {
			return "", nil, false
		}
		if crewAbilityIndex(c.Faces[0]) < 0 {
			return "", nil, false
		}
		return row.card, c.Faces[0], true
	}
	return "", nil, false
}

// reanswerAfterSteps re-derives XMage's answers for a fixture whose afterSteps
// ask questions the cast path's own XAnswers do not cover (an activate step's
// cost), so the crew dialog is scripted from the runner's observed picks.
func reanswerAfterSteps(reg *cards.Registry, f *cards.Face, name string, base *oraclegen.Item, res rules.OracleResult, cond *staticFixture) {
	if !cond.reanswers {
		return
	}
	base.XAnswers = oraclegen.XAnswersForScenario(reg, res, base.Scenario,
		oraclegen.ModeNumbers(f), castStepIndices(base.Scenario.Steps))
	if cond.beholdPick != "" {
		beholdFromHand(base.XAnswers, base.Scenario.Steps, name, cond.beholdPick)
	}
	if cond.activationCost == "" {
		return
	}
	for i := len(base.Scenario.Steps) - len(cond.afterSteps); i < len(base.Scenario.Steps); i++ {
		if i < 0 || i >= len(base.Scenario.Steps) || base.Scenario.Steps[i].Op != "activate" {
			continue
		}
		// The generic derivation already scripts an observed material exile
		// pick; the catalogue fixture appended beside it would be an unused
		// second answer on the same target ask (XMage consumes the queue
		// FIFO), so the cost selector is scripted only when the runner
		// recorded no exile pick.
		if !exileCostPickObserved(res.Decisions, i) {
			addActivationCostAnswers(base.XAnswers, i, cond.activationCost, name, res.Decisions)
		}
	}
}
