package oraclegen

import (
	"strconv"

	"github.com/adams-shaun/gorge/engine/rules"
)

// IsSetupChoice reports whether d is a setup-placed permanent's as-enters
// colour, type or card-name choice: gorge records it before step zero (Step < 0,
// Resume "etb") and XMage poses the same dialog while placing the seeded
// permanent, so the answer must be queued ahead of the first gameplay answer
// or that dialog consumes it. The pick kind is the option kind gorge's ETB ask
// builds: "color" for ChooseColor, "type" for ChooseType (the creature list,
// or another category's list), "name" for NameCard (Skyseer's Chariot's
// "choose a nonland card name"; the oracle runner's scenario-scoped name
// universe, engine/rules/oracle_name_universe.go, makes the pick a legal name XMage
// knows) -- all string-keyed XMage choices.
func IsSetupChoice(d rules.OracleDecision) bool {
	if d.Step >= 0 || d.Resume != "etb" || d.Kind != "choose_n" || len(d.Picks) != 1 || len(d.PickKinds) != 1 {
		return false
	}
	return d.PickKinds[0] == "color" || d.PickKinds[0] == "type" || d.PickKinds[0] == "name"
}

// SetupNumberChoice returns the XMage answer for a setup-placed permanent's
// as-enters "choose a number" (Talion, the Kindly Lord's ETBReplacement
// ChooseNumber): gorge records it before step zero (Step < 0, Resume "etb",
// pick kind "number") and XMage poses the same dialog through
// TestPlayer.getAmount while placing the seeded permanent, which reads an
// "X=<n>" head of the choice queue -- the same mapping the step-time
// transcriber gives a gameplay number pick. Unscripted, XMage's AI picked its
// own number and the row compared two different chosen numbers (CR 614.12:
// the choice is made as the permanent enters). ok is false for every other
// decision.
func SetupNumberChoice(d rules.OracleDecision) (XAnswer, bool) {
	if d.Step >= 0 || d.Resume != "etb" || d.Kind != "choose_n" || len(d.Picks) != 1 || len(d.PickKinds) != 1 ||
		d.PickKinds[0] != "number" {
		return XAnswer{}, false
	}
	if _, err := strconv.Atoi(d.Picks[0]); err != nil {
		return XAnswer{}, false
	}
	return XAnswer{d.Seat, "setup_choice", "X=" + d.Picks[0]}, true
}

// HoistSetupChoices moves every setup_choice answer in a step's answer stream
// to its front, preserving their relative order. XAnswers already prepends them
// to step zero, but a template reorders a step's answers afterwards -- the
// activate template hoists the activation-cost picks (Lifecraft Engine's crew
// choice) to the front of the activation step, which would put a gameplay
// answer ahead of the as-enters dialog and let XMage consume it there ("Choice
// key [Colossal Dreadmaw] not found"). Calling this at the end of any such
// reorder restores the invariant that the as-enters queue is answered first,
// whatever the setup kind.
func HoistSetupChoices(answers [][]XAnswer, step int) {
	if step < 0 || step >= len(answers) {
		return
	}
	var setup, rest []XAnswer
	for _, a := range answers[step] {
		if a.Kind == "setup_choice" {
			setup = append(setup, a)
			continue
		}
		rest = append(rest, a)
	}
	if len(setup) == 0 {
		return
	}
	answers[step] = append(setup, rest...)
}

func isSetupNumber(d rules.OracleDecision) bool {
	_, ok := SetupNumberChoice(d)
	return ok
}
