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
// them on top), which can disagree with gorge's fallback.
//
// Gorge's answer names only pile A -- the cards KEPT on top, in the order the
// player keeps them; every card it does not name is pile B, sent to the
// arrange's shared destination (ArrangeKind). XMage's dialog is the other way
// round: the leading names are pile B (the cards selected to the graveyard or
// the bottom) and the names after the skip are pile A's top order. So a
// partial partition queues the UNPICKED labels in offered order, then the
// skip that ends selection, then the PICKED labels in PickIdx order. Keeping
// every picked card where it is is the skip alone; sending every card away is
// every label with no skip (a skip would read as keep-all).
//
// Only the two shapes whose XMage consumer is the same library-selection
// dialog are scripted: "graveyard" (Surveil) and "bottom" (Scry). A forced
// ordering (Min == Max == Options) is a different dialog and is left alone,
// as are the dig/hand/exile arrange kinds, which the setup drive has never
// measured. Keeping every card in an order other than the offered one cannot
// be expressed through this queue (the driver reads the skip as keep-in-place
// and has no selection labels to order), so it is left unscripted rather than
// silently mis-scripted until the driver grows that shape.
func setupLibraryArrange(d rules.OracleDecision) []XAnswer {
	if d.Step >= 0 || d.GorgeKind != "arrange" ||
		(d.ArrangeKind != "graveyard" && d.ArrangeKind != "bottom") ||
		len(d.ArrangeLabels) == 0 || len(d.ArrangeLabels) != d.Options ||
		(d.Min == d.Max && d.Max == d.Options) {
		return nil
	}
	binned, kept, ok := libraryArrangePartition(d.ArrangeLabels, d.PickIdx, d.Options)
	if !ok {
		return nil
	}
	if len(kept) == len(d.ArrangeLabels) {
		// The skip keeps every card in place; it cannot express a different
		// kept order (the driver has no selection labels to order), so that
		// shape stays unscripted instead of silently keeping the wrong order.
		for i, label := range d.ArrangeLabels {
			if kept[i] != label {
				return nil
			}
		}
		return []XAnswer{{d.Seat, "setup_choice", "[choice_skip]"}}
	}
	out := make([]XAnswer, 0, len(d.ArrangeLabels)+1)
	for _, label := range binned {
		out = append(out, XAnswer{d.Seat, "setup_choice", label})
	}
	if len(kept) > 0 {
		// The skip separates pile B (the selection already named) from pile
		// A (the kept cards' top order). Without it the driver would read the
		// kept labels as more cards to send away; an empty pile A has no
		// selection to end, so it queues no skip.
		out = append(out, XAnswer{d.Seat, "setup_choice", "[choice_skip]"})
		for _, label := range kept {
			out = append(out, XAnswer{d.Seat, "setup_choice", label})
		}
	}
	return out
}

// libraryArrangePartition splits a KArrange answer into the two piles XMage's
// library dialog names separately: binned is pile B in offered order (every
// label the answer did not pick) and kept is pile A in PickIdx order (the
// cards that stay on top, topmost first). ok is false when the decision does
// not carry the full offered label list or a pick index is out of range, so a
// caller never scripts a guessed split. Every library-arrange transcription
// goes through this one split, so the next caller cannot re-derive the two
// piles backwards.
func libraryArrangePartition(labels []string, pickIdx []int, options int) (binned, kept []string, ok bool) {
	if len(labels) == 0 || len(labels) != options {
		return nil, nil, false
	}
	picked := make(map[int]bool, len(pickIdx))
	for _, i := range pickIdx {
		if i < 0 || i >= len(labels) {
			return nil, nil, false
		}
		picked[i] = true
	}
	binned = make([]string, 0, len(labels)-len(picked))
	for i, label := range labels {
		if !picked[i] {
			binned = append(binned, label)
		}
	}
	kept = make([]string, 0, len(pickIdx))
	for _, i := range pickIdx {
		kept = append(kept, labels[i])
	}
	return binned, kept, true
}

// setupModeAnswers is the setup-drive form of the step-time "mode" arm for a
// MODAL pick whose label resolves to a known charm position (or a
// GenericChoice / KWChoice queue sentinel): the same numeric/choice mapping,
// but the answers ride the pre-build setup queue (kind "setup_mode" for a
// numeric mode, "setup_choice" for a GenericChoice label, a KWChoice rule
// text or a yes/no boolean) so the
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
		case ModeKWChoiceQueue:
			// A Pump KWChoice$ ("gains your choice of ...") the setup drive
			// answered: XMage's GainsChoiceOfAbilitiesEffect reads the
			// keyword's lower-case rule text from the CHOICE queue, exactly
			// as the step-time arm maps it. Without this arm the sentinel
			// falls through to setup_mode "-3", which answers nothing and
			// leaves an invalid -3 on the mode queue.
			out = append(out, XAnswer{d.Seat, "setup_choice", xmageKeywordRuleText(d.Picks[k])})
		case ModeYesQueue, ModeNoQueue:
			// A two-way GenericChoice XMage asks as chooseUse (a per-player
			// "may shuffle" or an xmageChooseUseFaces face).
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
