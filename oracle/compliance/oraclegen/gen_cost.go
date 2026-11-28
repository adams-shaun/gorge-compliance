package oraclegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// hasCostPick reports whether any pick pays a cost through a chooser XMage
// poses even when the choice is forced: a tap (Crew/Saddle), a triggered
// optional tap (Spider-Man, To the Rescue's "you may tap it"), a sacrifice or
// an exile cost. These picks name object identities, so they can never be the
// lone-label ask forcedSingleOption drops.
func hasCostPick(d rules.OracleDecision) bool {
	for _, k := range d.PickKinds {
		switch k {
		case "tapcost", "trigger_cost_tap", "sacrifice", "exilecost":
			return true
		}
	}
	return false
}

// announcesX reports whether d is the single positive "X = <n>" announcement
// of a cost that names X more than once ({X}{X}, the runner's XMulti mark).
// XMage's unscripted announce does not pay the repeated X when the pool leaves
// one legal value (Gogo's unspent CC), so it is scripted. A forced single-X
// announce is owned by castAnnouncesX/activationAnnouncesX (a real
// VariableManaCost step) or left to XMage's own default (a Craft keyword's
// XMin count: Saheeli's Lattice, The Enigma Jewel). A forced announce that
// sizes a sacrifice cost is the exception (announcesSacrificeX).
func announcesX(d rules.OracleDecision) bool {
	if !d.XMulti || len(d.Picks) != 1 || pickKind(d, 0) != "x" {
		return false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(d.Picks[0], "X = "))
	return err == nil && n > 0 && strings.HasPrefix(d.Picks[0], "X = ")
}

// castAnnouncesX reports a forced "X = <n>" announcement made while casting a
// spell (a cast step of the scenario). XMage announces a spell's X over 0..any
// (VariableManaCost's max is unbounded; TestPlayer.announceX shows "(any
// value)"), so it poses the ask however few values gorge's pool allowed: a
// forced X = 0 cast with no spare mana (The Goose Mother, Lost in the Maze,
// Meathook Massacre II's {X}{X}) strict-misses "Announce the value for {X}"
// when it is left unscripted. A triggered cost's X stays with announcesX /
// announcesSacrificeX/xTriggerCostGroup; nor is a spell whose X also sizes a
// non-mana cost part (castXSizesCost: the step is left out of casts).
func castAnnouncesX(d rules.OracleDecision, casts map[int]bool) bool {
	return announcesXAtStep(d, casts)
}

// activationAnnouncesX reports the same forced "X = <n>" announcement on an
// ACTIVATION whose own cost names {X} (Talon of Pain's "{X}, {T}, Remove X
// charge counters": gorge offered only X = 0 with no charge counters, and
// XMage's announceX still strict-missed "Announce the value for {X} (any
// value)"). The mapped count is one per announcing cost: a VariableManaCost
// and a separate VariableCostImpl each ask (Ludevic, Necrogenius's {X} plus
// ExileXFromYourGraveCost), while a counter-removal cost that reuses the mana
// X (Talon) counts once. Craft/XMin is a keyword, not an activation cost, and
// stays out.
func activationAnnouncesX(d rules.OracleDecision, activates map[int]int) bool {
	return activates[d.Step] > 0 && announcesXShape(d)
}

// announcesXAtStep is the shared shape of a forced single-pick X announcement
// on an owned step.
func announcesXAtStep(d rules.OracleDecision, steps map[int]bool) bool {
	return steps[d.Step] && announcesXShape(d)
}

// announcesXShape is the forced single-pick X announcement, never the forced
// one-option label asks these predicates exist to protect.
func announcesXShape(d rules.OracleDecision) bool {
	return d.Kind == "choose_n" && len(d.Picks) == 1 && pickKind(d, 0) == "x" &&
		strings.HasPrefix(d.Picks[0], "X = ")
}

// announcesSacrificeX reports a forced positive "X = <n>" announcement that
// sizes a Sac<X/...> additional cost: the same seat's next decision in the
// step picks exactly n objects to sacrifice (Rottenmouth Viper's "sacrifice
// any number of nonland permanents", X = the count). Gorge offers only the X
// the pool can then afford, but XMage's announce ranges over every
// sacrificeable count and its AI picks its own (measured: X = 0, the cost
// paid with Sol Ring's mana and nothing sacrificed), leaving the scripted
// sacrifice unused. Such an announce is scripted.
func announcesSacrificeX(ds []rules.OracleDecision, i int) bool {
	d := ds[i]
	if len(d.Picks) != 1 || pickKind(d, 0) != "x" || !strings.HasPrefix(d.Picks[0], "X = ") {
		return false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(d.Picks[0], "X = "))
	if err != nil || n <= 0 || i+1 >= len(ds) {
		return false
	}
	next := ds[i+1]
	if next.Step != d.Step || next.Seat != d.Seat || len(next.Picks) != n {
		return false
	}
	for k := range next.Picks {
		if pickKind(next, k) != "sacrifice" {
			return false
		}
	}
	return true
}

// unlessPolarity is the yes/no answer to an unless-pay boolean ask: the
// picked option's label says whether the cost was paid. A decline wording
// ("Don't pay", "Decline") is "no"; a payment wording is "yes".
func unlessPolarity(d rules.OracleDecision) string {
	if len(d.Picks) == 0 {
		return "no"
	}
	l := strings.ToLower(strings.TrimSpace(d.Picks[0]))
	for _, neg := range []string{"don't", "do not", "decline", "no", "refuse"} {
		if strings.HasPrefix(l, neg) {
			return "no"
		}
	}
	// Any other label names the cost being paid ("Pay {2}", "Sacrifice an
	// artifact").
	return "yes"
}

// manaColourLabel maps gorge's "Add W" mana option to the colour name
// XMage's colour chooser shows (its Choice key is "White", not "Add W"). A
// colour option of a costed mana ability names the cost first ("Pay 1: Add W",
// "Pay 2 life: Add W": pay.ManaAbilityCostPrefix), and XMage asks the same
// colour dialog for it, so the cost prefix is dropped before matching.
func manaColourLabel(label string) (string, bool) {
	if i := strings.LastIndex(label, ": "); i >= 0 {
		label = label[i+2:]
	}
	if strings.HasPrefix(label, "Add ") && len(label) == 5 {
		return manaColour(label[4])
	}
	return "", false
}

// bareColourLabel maps a bare colour word ("white", any case) to the
// capitalised colour name XMage's colour chooser shows ("White"). XMage's
// ChoiceColor keys are the names, while a resolution-time "choose a colour"
// decision records the picked option's label, which can reach a transcript as
// the bare word (Midvast Protector's ETB, Sygg, River Guide's protection
// grant). The gate is the engine's option KIND "color" -- the fixed WUBRG list
// every colour ask builds -- never the label text, so a card or object named
// "Red" is an object pick and keeps its label. A transcript written before
// PickKinds existed falls back to the ChooseColor resume kind, whose option
// list is the same fixed colour list.
func bareColourLabel(d rules.OracleDecision, k int) (string, bool) {
	if k < 0 || k >= len(d.Picks) {
		return "", false
	}
	if k < len(d.PickKinds) && d.PickKinds[k] != "" {
		if d.PickKinds[k] != "color" {
			return "", false
		}
	} else if d.Resume != "choosecolor" {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(d.Picks[k])) {
	case "white":
		return "White", true
	case "blue":
		return "Blue", true
	case "black":
		return "Black", true
	case "red":
		return "Red", true
	case "green":
		return "Green", true
	}
	return "", false
}

// colourPickCount counts the picks of a decision that name a single mana
// colour (a mana ability's colour option, with or without a cost prefix).
// When every pick of a multi-pick decision is such an option, XMage models
// the ask as a multi-amount distribution among colours, not one colour
// dialog, so the decision must not emit a joined "^" colour selection.
func colourPickCount(d rules.OracleDecision) int {
	n := 0
	for k, label := range d.Picks {
		if label == "" && k < len(d.PickRefs) {
			label = oraclediffRefName(d.PickRefs[k])
		}
		if _, ok := manaColourLabel(label); ok {
			n++
		}
	}
	return n
}

func manaColour(code byte) (string, bool) {
	switch code {
	case 'W':
		return "White", true
	case 'U':
		return "Blue", true
	case 'B':
		return "Black", true
	case 'R':
		return "Red", true
	case 'G':
		return "Green", true
	default:
		return "", false
	}
}

func payment(picks []string) bool {
	if len(picks) == 0 {
		return false
	}
	for _, p := range picks {
		if !strings.HasPrefix(p, "Pay ") {
			return false
		}
		// "Pay 1: Add W" is a costed mana ability's colour pick, a real
		// XMage colour dialog, not a hybrid/phyrexian payment half.
		if _, colour := manaColourLabel(p); colour {
			return false
		}
	}
	return true
}

// phyrexianLifePicks reports whether every pick is the two-life face of a
// Phyrexian pip (the engine's pay_life option, from pay.ManaAsk's
// announcement or a triggered cost's pipAsk). It is the ONE payment face
// XMage poses as a chooseUse ("Pay 2 life instead of {X}?"), so the generator
// must script it; every other flexible face (pay_W/pay_generic) is paid from
// the pool without a question.
func phyrexianLifePicks(d rules.OracleDecision) bool {
	if len(d.Picks) == 0 {
		return false
	}
	for k := range d.Picks {
		if pickKind(d, k) != "pay_life" {
			return false
		}
	}
	return true
}

// poolFor turns a Forge mana cost ("2 R R", "W/U", "X G") into the exact
// pool letters that pay it: generic as colorless, hybrid as its first half.
func poolFor(cost string) (string, string) {
	cost = strings.TrimSpace(cost)
	if cost == "" || strings.EqualFold(cost, "no cost") {
		return "", "no mana cost"
	}
	var b strings.Builder
	for _, sym := range strings.Fields(cost) {
		switch {
		case sym == "0":
		case strings.Trim(sym, "0123456789") == "":
			var n int
			fmt.Sscanf(sym, "%d", &n)
			b.WriteString(strings.Repeat("C", n))
		case len(sym) == 1 && strings.Contains("WUBRGC", sym):
			b.WriteString(sym)
		case len(sym) == 2 && strings.Contains("WUBRG", sym[:1]) && strings.Contains("WUBRG", sym[1:]):
			// Forge spells hybrid {W/B} as "WB": pay the first half.
			b.WriteString(sym[:1])
		case len(sym) == 2 && sym[0] == '2' && strings.Contains("WUBRG", sym[1:]):
			// {2/W}: pay the coloured half.
			b.WriteString(sym[1:])
		case strings.Contains(sym, "/"):
			h := strings.Split(sym, "/")
			switch {
			case strings.EqualFold(h[1], "P"):
				b.WriteString(h[0])
			case len(h[0]) == 1 && strings.Contains("WUBRGC", h[0]):
				b.WriteString(h[0])
			case strings.Trim(h[0], "0123456789") == "":
				// {2/W}: pay the coloured half.
				b.WriteString(h[1])
			default:
				return "", "mana symbol " + sym
			}
		case sym == "X":
			// X = xValue, scripted for both engines (Generate).
			b.WriteString(strings.Repeat("C", xValue))
		default:
			return "", "mana symbol " + sym
		}
	}
	return b.String(), ""
}
