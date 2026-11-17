package oraclegen

import (
	"fmt"

	"github.com/adams-shaun/gorge/engine/rules"
)

// setupDriveAnswers maps a decision the runner answered while it drove from
// genesis to turn N's first main phase (Step < 0) to the XMage answers that
// answer the same dialogs, or nil when XMage poses no ask this decision
// matches. Gorge's fallback answers such an ask in place, but the generator's
// transcriber used to drop every Step<0 decision that was not an as-enters
// colour/type choice (IsSetupChoice), a declined optional target
// (IsSetupTargetDecline) or a setup-placed trigger order
// (triggerOrderSetupPosed); an as-enters "choose a number" is scripted by
// SetupNumberChoice. XMage then answered the dropped ask with its own
// AI, so the two engines diverged before the card's real behaviour was ever
// judged (Zuko, Conflicted's turn-1 charm; Gathering Stone's mill/reveal
// pair).
//
// This covers the classes those predicates leave: a modal pick
// (GorgeKind "modes"), a bare two-way boolean (yesNo) -- the optional
// mill/reveal booleans Gathering Stone's triggers pose -- and a library
// arrange (GorgeKind "arrange"), the upkeep Surveil/Scry that sends every
// looked-at card to the graveyard (Essence Anchor). Each answer is the
// fallback's own pick, mapped to XMage's dialog vocabulary the same way the
// step-time transcriber maps the identical decision (the numeric mode queue,
// the boolean choice, the labelled choice queue), so scripting it cannot
// change gorge's side: xmage_answers are invisible to the runner.
//
// The kinds deliberately left unscripted, all of which XMage answers without
// the setup drive's help or whose ask this generator cannot yet place: an
// "opening_*" pregame ask (a Leyline's "begin with this?" -- XMage's own
// start-of-game handling; not a bare yesNo), and a "mode" whose label
// resolves to no charm position (a plain GenericChoice on XMage's CHOICE
// dialog, or an unless-pay XMage does not pose at all).
func setupDriveAnswers(d rules.OracleDecision, modes map[string]int) []XAnswer {
	if d.GorgeKind == "modes" {
		return setupModeAnswers(d, modes)
	}
	if v, ok := yesNo(d); ok {
		return []XAnswer{{d.Seat, "setup_choice", v}}
	}
	if as := setupLibraryArrange(d); len(as) > 0 {
		return as
	}
	if v := triggerCostAnswer(d); v != "" {
		return []XAnswer{{d.Seat, "setup_choice", v}}
	}
	return nil
}

// setupLibraryArrange scripts a setup-drive library-arrange ask (an upkeep
// Surveil/Scry's card-by-card selection) into XMage's library-selection
// queue. XMage poses it during build(), before any step, and consumes the
// head of the choice queue; unscripted it answers with its own default (the
// old build sent every looked-at card to the graveyard, the new one keeps
// them on top), which can disagree with gorge's fallback. The answers are
// the SELECTED cards first (the ones the runner sent to the graveyard or the
// bottom), then the kept cards in the runner's order, because XMage reads the
// remaining labels as the top-card ordering. Keeping every card is the one
// shape XMage spells as its skip token. A forced ordering (Min == Max ==
// Options) is a different dialog and is left alone.
func setupLibraryArrange(d rules.OracleDecision) []XAnswer {
	if d.Step >= 0 || d.GorgeKind != "arrange" || d.ArrangeKind == "" ||
		len(d.ArrangeLabels) == 0 || len(d.ArrangeLabels) != d.Options ||
		(d.Min == d.Max && d.Max == d.Options) {
		return nil
	}
	picked := make(map[int]bool, len(d.PickIdx))
	for _, i := range d.PickIdx {
		if i >= 0 && i < len(d.ArrangeLabels) {
			picked[i] = true
		}
	}
	if len(picked) == len(d.ArrangeLabels) {
		// Keep every looked-at card where it is.
		return []XAnswer{{d.Seat, "setup_choice", "[choice_skip]"}}
	}
	out := make([]XAnswer, 0, len(d.ArrangeLabels))
	for i, label := range d.ArrangeLabels {
		if !picked[i] {
			out = append(out, XAnswer{d.Seat, "setup_choice", label})
		}
	}
	for _, i := range d.PickIdx {
		if i >= 0 && i < len(d.ArrangeLabels) {
			out = append(out, XAnswer{d.Seat, "setup_choice", d.ArrangeLabels[i]})
		}
	}
	return out
}

// setupModeAnswers is the setup-drive form of the step-time "mode" arm for a
// MODAL pick whose label resolves to a known charm position (or a
// GenericChoice queue sentinel): the same numeric/choice mapping, but the
// answers ride the pre-build setup queue (kind "setup_mode" for a numeric
// mode, "setup_choice" for a GenericChoice label or a yes/no boolean) so the
// driver queues them before XMage places the seeded permanents. An unmapped
// label returns nil: a plain GenericChoice's label reaches XMage's CHOICE
// dialog, not chooseMode, so scripting the step-time arm's positional
// fallback would answer the wrong queue. The unless-pay and play "mode"
// decisions are likewise left to XMage, which poses their chooseUse itself.
func setupModeAnswers(d rules.OracleDecision, modes map[string]int) []XAnswer {
	var out []XAnswer
	for k := range d.PickIdx {
		m, ok := modeNumberFor(d, k, modes)
		if !ok {
			return nil
		}
		switch m {
		case ModeChoiceQueue:
			// A DB$ GenericChoice | SetChosenMode$ True body: XMage reads the
			// pick through its CHOICE queue, showing the option LABEL.
			out = append(out, XAnswer{d.Seat, "setup_choice", d.Picks[k]})
		case ModeYesQueue, ModeNoQueue:
			// A per-player "may shuffle" GenericChoice: XMage asks chooseUse.
			out = append(out, XAnswer{d.Seat, "setup_choice", map[bool]string{true: "yes", false: "no"}[m == ModeYesQueue]})
		default:
			out = append(out, XAnswer{d.Seat, "setup_mode", fmt.Sprint(m)})
		}
	}
	if len(out) > 0 && d.Max > len(d.Picks) {
		// XMage keeps choosing modes up to Max; stop it with the mode queue's
		// skip token.
		out = append(out, XAnswer{d.Seat, "setup_mode", "[mode_skip]"})
	}
	return out
}
