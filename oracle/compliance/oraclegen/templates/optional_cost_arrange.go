package templates

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// paidArrangeToGraveyard re-plays the paid optional-cost cast with every
// look-at-the-library arrange its resolution poses (a surveil or scry rider
// the paid branch adds) answered with the empty kept pile, which sends the
// looked-at cards to the graveyard. The Wastes filler makes keeping them on
// top indistinguishable from never looking; graveyarding them makes the paid
// branch visible. ok is false when the paid replay poses no arrange. The XMage
// answers for the arrange step are re-derived from the replay
// (oraclegen's arrange arm scripts one name selection per looked-at card); the
// other steps keep the answers the cast pipeline derived.
func paidArrangeToGraveyard(reg *cards.Registry, f *cards.Face, paid oraclegen.Item) (oraclegen.Item, bool) {
	res, ok := oraclegen.PlaysThrough(reg, paid.Scenario)
	if !ok {
		return oraclegen.Item{}, false
	}
	steps := map[int]bool{}
	for _, d := range res.Decisions {
		if d.GorgeKind == "arrange" && d.Step >= 0 && d.Step < len(paid.Scenario.Steps) {
			steps[d.Step] = true
		}
	}
	if len(steps) == 0 {
		return oraclegen.Item{}, false
	}
	out := paid
	out.Scenario.Steps = append([]oraclegen.Step(nil), paid.Scenario.Steps...)
	for i := range out.Scenario.Steps {
		if !steps[i] || out.Scenario.Steps[i].Op != "resolve" {
			continue
		}
		st := out.Scenario.Steps[i]
		st.Answers = append(append([]oraclegen.Answer(nil), st.Answers...), oraclegen.Answer{Kind: "arrange", Pick: []string{}})
		out.Scenario.Steps[i] = st
	}
	moved, ok := oraclegen.PlaysThrough(reg, out.Scenario)
	if !ok || len(moved.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	derived := oraclegen.XAnswersForScenario(moved, out.Scenario, oraclegen.ModeNumbers(f), castStepIndices(out.Scenario.Steps))
	xa := make([][]oraclegen.XAnswer, len(out.Scenario.Steps))
	copy(xa, paid.XAnswers)
	for i := range steps {
		if i < len(derived) && out.Scenario.Steps[i].Op == "resolve" {
			xa[i] = derived[i]
		}
	}
	out.XAnswers = xa
	return out, true
}
