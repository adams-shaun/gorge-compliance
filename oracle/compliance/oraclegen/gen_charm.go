package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/rules"
)

type charmMode struct{ svar, label string }

// CharmCombination is one legal, ordered set of mode picks and their target
// chains. Combinations follow Choices$ order; repeated picks appear only when
// CanRepeatModes$ is true.
type CharmCombination struct {
	Modes []CharmMode
	Slots []Slot
}

// CharmCombinations enumerates legal combinations from the minimum pick count
// upward, in Choices$ order within each size. Ordinary charms default to one
// pick, preserving their historical ordering.
// optionalCostCharmNum reads a CharmNum$ that names an SVar of the form
// Count$OptionalGenericCostPaid.<paid>.<unpaid> with literal branches, and
// returns the two counts in ascending order.
func optionalCostCharmNum(f *cards.Face, charmNum string) (int, int, bool) {
	body, ok := f.SVars[strings.TrimSpace(charmNum)]
	if !ok {
		return 0, 0, false
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(body), "Count$OptionalGenericCostPaid.")
	if !ok {
		return 0, 0, false
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 2 {
		return 0, 0, false
	}
	a, errA := strconv.Atoi(parts[0])
	b, errB := strconv.Atoi(parts[1])
	if errA != nil || errB != nil || a <= 0 || b <= 0 {
		return 0, 0, false
	}
	return min(a, b), max(a, b), true
}

func CharmCombinations(f *cards.Face) []CharmCombination {
	modes := charmModes(f)
	if len(modes) == 0 {
		return nil
	}
	pickCount, minCount, repeat := 1, 1, false
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" || sa.API != "Charm" {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(sa.Params["CharmNum"])); err == nil && n > 0 {
			pickCount = n
		}
		if n, err := strconv.Atoi(strings.TrimSpace(sa.Params["MinCharmNum"])); err == nil && n > 0 {
			minCount = n
		} else if strings.TrimSpace(sa.Params["MinCharmNum"]) == "" {
			minCount = pickCount
		}
		if lo, hi, ok := optionalCostCharmNum(f, sa.Params["CharmNum"]); ok {
			// The count depends on the cast's optional-cost election (Pyrrhic
			// Strike's "if this spell's additional cost was paid, choose both
			// instead"): offer both counts, the smaller first, so a declined
			// cast keeps its historical first plan and a paid one finds a
			// plan of its own size.
			minCount, pickCount = lo, hi
		}
		repeat = strings.EqualFold(strings.TrimSpace(sa.Params["CanRepeatModes"]), "True")
		break
	}
	var out []CharmCombination
	var selected []CharmMode
	// emit appends the current selection as one combination.
	emit := func() {
		combo := CharmCombination{Modes: append([]CharmMode(nil), selected...)}
		for _, mode := range selected {
			combo.Slots = append(combo.Slots, ChainSlotSpecs(f, mode.svar)...)
		}
		out = append(out, combo)
	}
	// allDistinct reports whether the current selection repeats no mode.
	allDistinct := func() bool {
		seen := make(map[string]bool, len(selected))
		for _, mode := range selected {
			if seen[mode.svar] {
				return false
			}
			seen[mode.svar] = true
		}
		return true
	}
	// visit enumerates combinations of count modes from start. distinct
	// constrains the walk to strictly increasing positions (repeat-free
	// combinations only); otherwise positions may repeat, and the leaf
	// skips any all-distinct selection so the two walks do not overlap.
	var visit func(start, count int, distinct bool)
	visit = func(start, count int, distinct bool) {
		if len(selected) == count {
			if !distinct && allDistinct() {
				return
			}
			emit()
			return
		}
		for i := start; i < len(modes); i++ {
			selected = append(selected, modes[i])
			next := i
			if distinct {
				next++
			}
			visit(next, count, distinct)
			selected = selected[:len(selected)-1]
		}
	}
	// Within each pick count the repeat-free combinations come first, in
	// Choices$ order; only a CanRepeatModes$ charm then adds the repeating
	// ones. A non-repeat charm takes the single distinct walk, so its output
	// is byte-identical to the historical one.
	for count := minCount; count <= pickCount; count++ {
		visit(0, count, true)
		if repeat {
			visit(0, count, false)
		}
	}
	return out
}

// charmModes lists the spell's charm modes in Choices$ order.
func charmModes(f *cards.Face) []charmMode {
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" {
			continue
		}
		if sa.API != "Charm" {
			return nil
		}
		var out []charmMode
		for _, name := range strings.Split(sa.Params["Choices"], ",") {
			if name = strings.TrimSpace(name); name != "" {
				out = append(out, charmMode{name, effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)})
			}
		}
		return out
	}
	return nil
}

// modeNumberFor resolves the k-th pick's 1-based XMage mode number from its
// label. ok is false when the pick's label names no charm mode -- a
// non-charm modal or a discard-style picker sharing the "mode" kind; the
// caller then falls back to the option's own 1-based position. A label that
// names a SetChosenMode$ True GenericChoice returns ModeChoiceQueue, telling
// the caller to answer on XMage's choice queue instead of the mode queue.
func modeNumberFor(d rules.OracleDecision, k int, modes map[string]int) (int, bool) {
	if k < 0 || k >= len(d.Picks) {
		return 0, false
	}
	m, ok := modes[d.Picks[k]]
	return m, ok
}

// tokenChoiceNames returns a GenericChoice's Choices$ names when there are at
// least two and every one names a DB$ Token body on the face, else nil.
func tokenChoiceNames(f *cards.Face, choices string) []string {
	var names []string
	for _, name := range strings.Split(choices, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if svarParams(f.SVars[name])["DB"] != "Token" {
			return nil
		}
		names = append(names, name)
	}
	if len(names) < 2 {
		return nil
	}
	return names
}

// ModeChoiceQueue is the sentinel position modeNumbers stores for a mode
// label that reaches XMage through its CHOICE queue (controller.choose)
// rather than the numeric mode queue: the Choices$ label of a
// DB$ GenericChoice | SetChosenMode$ True body (the Theros-style Sieges).
// XMage's ChooseModeEffect does controller.choose(Outcome.Neutral, Choice,
// game), which shows the option LABEL; chooseMode/setModeChoice (the numeric
// queue) is never posed for it. A real charm position is 1-based, so 0 is
// never a valid mode number and is unambiguous.
const ModeChoiceQueue = 0

// ModeYesQueue and ModeNoQueue are the sentinels for the two labels of a
// GenericChoice XMage poses as player.chooseUse (the yes/no choice queue),
// never as a mode: a DB$ GenericChoice | AILogic$ Timetwister body (Turtles in
// Time, a per-player "may shuffle your hand and graveyard" ask) and every
// two-option face in xmageChooseUseFaces (choice_dialog.go, e.g. Terrapact
// Intimidator's "target opponent may have you create two Lander tokens").
// Choices$ lists the yes label first, the no label second. Negative, so
// neither collides with a real 1-based charm position nor with
// ModeChoiceQueue.
const (
	ModeYesQueue = -1
	ModeNoQueue  = -2
)

// modeNumbers maps each charm mode's label (as gorge's mode decision
// shows it) to its 1-based position in its Choices$ list, for every Charm
// on the face -- the spell's own and any modal trigger's. It ALSO carries
// every DB$ GenericChoice | SetChosenMode$ True label and every source-proven
// Pump KWChoice$ label, mapped to the ModeChoiceQueue / ModeKWChoiceQueue
// sentinels: those picks are the same "mode" decision kind to gorge but a
// different XMage queue, and no in-band discriminator on the decision
// separates them (a mid-resolution Charm shares Resume "modes"), so the face
// shape is the authority.
// printedMaxPrefix marks the ModeNumbers entries that carry a charm's PRINTED
// maximum rather than a mode number (see printedMaxKey).
const printedMaxPrefix = "\x00printed_max\x00"

// printedMaxKey is the ModeNumbers key under which a charm mode label carries
// its printed CharmNum$ (stored negated, so the value can never be mistaken
// for a 1-based mode number or a queue sentinel). gorge clamps a mode
// decision's Max to the modes it can offer (CR 700.2a), but XMage's
// Modes.choose keeps asking up to the printed maximum, so the closing
// mode skip must be keyed on the printed bound. The label is the only handle
// a decision gives back to its card, hence one entry per mode label.
func printedMaxKey(label string) string { return printedMaxPrefix + label }

// IsPrintedMaxKey reports whether a ModeNumbers key is a printed-maximum
// entry rather than a mode label.
func IsPrintedMaxKey(k string) bool { return strings.HasPrefix(k, printedMaxPrefix) }

// printedCharmMax is the literal CharmNum$ of a non-repeating Charm (0 when
// absent, non-literal or repeatable: those keep the decision's own Max).
func printedCharmMax(params map[string]string) int {
	if strings.EqualFold(strings.TrimSpace(params["CanRepeatModes"]), "True") {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(params["CharmNum"]))
	if err != nil || n < 2 {
		return 0
	}
	return n
}

// printedModeMax is the printed maximum XMage will loop a mode ask to for the
// decision's picked cards (0 when none of the picks carries one).
func printedModeMax(d rules.OracleDecision, modes map[string]int) int {
	for _, label := range d.Picks {
		if n, ok := modes[printedMaxKey(label)]; ok && n < 0 {
			return -n
		}
	}
	return 0
}

func modeNumbers(f *cards.Face) map[string]int {
	out := map[string]int{}
	// A ChoiceRestriction$ Charm (Zuko, Conflicted's "choose one that hasn't
	// been chosen") is offered WITHOUT the modes already used, by gorge and
	// by XMage alike, and XMage's scripted mode number indexes the modes still
	// on offer, not Choices$: Zuko's second ask offers three modes and XMage
	// resolved the scripted absolute "2" as the second REMAINING mode (Add
	// {R}), not the second printed one. Such a charm's labels stay out of the
	// map, so the answer falls to the pick's 1-based position in the offered
	// list -- the number XMage's chooseMode reads.
	addCharm := func(choices, restriction string, params map[string]string) {
		if restriction != "" {
			return
		}
		printed := printedCharmMax(params)
		for i, name := range strings.Split(choices, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			label := effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)
			out[label] = i + 1
			if printed > 0 {
				out[printedMaxKey(label)] = -printed
			}
		}
	}
	for _, body := range f.SVars {
		p := svarParams(body)
		if p["DB"] != "GenericChoice" || !strings.EqualFold(p["AILogic"], "Timetwister") {
			continue
		}
		for i, name := range strings.Split(p["Choices"], ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			sentinel := ModeNoQueue
			if i == 0 {
				sentinel = ModeYesQueue
			}
			out[effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)] = sentinel
		}
	}
	// "Create your choice of a <token> or a <token>" (a DB$ GenericChoice
	// whose every Choices$ entry is a DB$ Token body, no SetChosenMode$ and
	// no AILogic$ Timetwister): XMage writes the card's own OneShotEffect,
	// never a modal ability. Measured over the 20 Standard sets' four
	// carriers: the two-token form is player.chooseUse with the first token
	// as the yes label (Tireless Provisioner, Ant-Man's Army: "Food" /
	// "Treasure"), the three-token form a ChoiceImpl over the token names
	// (Transmutation Font's Blood/Clue/Food, The Third Doctor's
	// Clue/Food/Treasure), whose labels are the bodies' SpellDescription$.
	// The choice is an activated ability's own body (Transmutation Font's
	// AB$ GenericChoice) or a trigger's DB$ SVar body (Tireless Provisioner).
	var tokenChoices []map[string]string
	for _, sa := range f.Abilities {
		if sa.API == "GenericChoice" {
			tokenChoices = append(tokenChoices, sa.Params)
		}
	}
	for _, body := range f.SVars {
		if p := svarParams(body); p["DB"] == "GenericChoice" {
			tokenChoices = append(tokenChoices, p)
		}
	}
	for _, p := range tokenChoices {
		if p["SetChosenMode"] != "" || strings.EqualFold(p["AILogic"], "Timetwister") {
			continue
		}
		names := tokenChoiceNames(f, p["Choices"])
		for i, name := range names {
			label := effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)
			switch {
			case len(names) > 2:
				out[label] = ModeChoiceQueue
			case i == 0:
				out[label] = ModeYesQueue
			default:
				out[label] = ModeNoQueue
			}
		}
	}
	// A two-option GenericChoice XMage poses as ONE boolean chooseUse
	// (choice_dialog.go): the first Choices$ label is the true branch and the
	// second the false branch, answered yes/no on the CHOICE queue for the
	// seat that chose (Terrapact Intimidator's targeted opponent). The seat
	// is already the decision's Seat; only the queue and the value change.
	if pairs, ok := chooseUseLabels(f); ok {
		for _, pair := range pairs {
			out[pair[0]] = ModeYesQueue
			out[pair[1]] = ModeNoQueue
		}
	}
	// The SetChosenMode labels first, so a real Charm mode of the same
	// label (a corpus impossibility) would keep its numeric position below.
	for _, body := range f.SVars {
		p := svarParams(body)
		if p["DB"] != "GenericChoice" || !strings.EqualFold(p["SetChosenMode"], "True") {
			continue
		}
		for _, name := range strings.Split(p["Choices"], ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out[effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)] = ModeChoiceQueue
		}
	}
	// A plain GenericChoice XMage poses as a Choice dialog over the labels
	// (choice_dialog.go): the choice queue, by label, like SetChosenMode.
	for _, label := range choiceDialogLabels(f) {
		if _, taken := out[label]; !taken {
			out[label] = ModeChoiceQueue
		}
	}
	// A Pump KWChoice$ ("gains your choice of X, Y or Z") is XMage's
	// GainsChoiceOfAbilitiesEffect: one ChoiceImpl over the candidate
	// abilities' RULE TEXTS, read from the CHOICE queue by label
	// (choice_dialog.go). The answer is the keyword's lower-case rule text,
	// not gorge's KWChoice$ spelling ("Flying" -> "flying", "Rampage:3" ->
	// "rampage 3"), so it gets its own sentinel.
	for _, label := range kwChoiceDialogLabels(f) {
		if _, taken := out[label]; !taken {
			out[label] = ModeKWChoiceQueue
		}
	}
	// The bespoke-dialog KWChoice$ faces: same queue, XMage's own labels.
	for _, label := range kwChoiceBespokeLabels(f) {
		if _, taken := out[label]; !taken {
			out[label] = ModeKWBespokeChoiceQueue
		}
	}
	// The bespoke-dialog GenericChoice faces: same queue, XMage's own labels.
	for label := range genericChoiceBespokeLabels(f) {
		if _, taken := out[label]; !taken {
			out[label] = ModeGenericBespokeChoiceQueue
		}
	}
	for _, sa := range f.Abilities {
		if sa.API == "Charm" {
			addCharm(sa.Params["Choices"], sa.Params["ChoiceRestriction"], sa.Params)
		}
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "Charm") {
			if c := svarParams(body)["Choices"]; c != "" {
				addCharm(c, svarParams(body)["ChoiceRestriction"], svarParams(body))
			}
		}
	}
	return out
}
