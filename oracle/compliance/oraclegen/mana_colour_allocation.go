package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// manaColourAllocation answers a Produced$ "Combo <colours>" mana ask. The
// decision carries the ask's offered colour set (OracleDecision.ManaColours,
// read off the ask's own options), and XMage's AddManaInAnyCombinationEffect
// poses one multi-amount message per colour of that set, in the set's own
// order, whatever the unit count -- a count-1 restricted set included
// (Muerra, Trash Tactician's Add R / Add G pair). Each message consumes one "X=<n>" amount answer in order, so
// the answers are one per offered colour carrying the number of units the
// engine allocated to it. The dialog's message order is the set's option
// order, which is what ManaColours records; a wrong order shows up as a
// stable harness or divergent row on the XMage replay, not a flake.
//
// A produced-Any ask (XMage's DynamicManaEffect: a colour dialog at one
// unit, a WUBRG multi-amount above it) never reaches here -- it carries no
// ManaColours and keeps the routing it already agrees with.
func manaColourAllocation(d rules.OracleDecision) []XAnswer {
	counts := make(map[string]int, len(d.ManaColours))
	for k := range d.Picks {
		if c, ok := manaAskPickColour(d, k); ok {
			counts[c]++
		}
	}
	as := make([]XAnswer, 0, len(d.ManaColours))
	for _, c := range d.ManaColours {
		as = append(as, XAnswer{d.Seat, "amount", strconv.Itoa(counts[c])})
	}
	return as
}

// manaAskPickColour names the mana symbol one picked option allocates: the
// pick's label ("Add R", with or without a cost prefix) is the authority,
// falling back to the option's index into the recorded set (the options are
// laid out unit-major over the set).
func manaAskPickColour(d rules.OracleDecision, k int) (string, bool) {
	if k < len(d.Picks) {
		label := d.Picks[k]
		if i := strings.LastIndex(label, ": "); i >= 0 {
			label = label[i+2:]
		}
		if strings.HasPrefix(label, "Add ") && len(label) == 5 {
			return label[4:5], true
		}
	}
	if k < len(d.PickIdx) && len(d.ManaColours) > 0 {
		return d.ManaColours[d.PickIdx[k]%len(d.ManaColours)], true
	}
	return "", false
}

// stagedManaAllocationAnswers answers a cost-staged "any combination of
// colors" allocation (Conversion Apparatus's "{T}, Pay {E}{E}{E}: Add three
// mana in any combination of colors"). The activation's cost stage poses the
// allocation as a plain choose -- no "mana_color" resume (rules' askManaChoice
// is not the asker; the staged ability chooser is) -- so the mana_color arms
// never see it, and every pick's label carries the cost prefix
// ("Pay 3 energy: Add W", pay.ManaAbilityCostPrefix). The option layout is the
// same unit-major walk over the produced set as the bare-resume ask
// (engine/effects/mana_effect.go: unit*len(colours) + colourIndex), so the
// open set (Options == units*5, gorge's manaChoiceColours) is recoverable, and
// XMage's AddManaInAnyCombinationEffect poses the same "Distribute mana among
// colors" multi-amount for it as for the bare-resume allocation: five WUBRG
// "X=<n>" amounts, zeroes included. Its AI answer otherwise distributes
// wherever it likes (xproof 11251811: gorge WWU, xmage WUB), so the class the
// plain "emit nothing" rule served only agreed when the two engines' AI
// distributions happened to coincide (Baxter Building's WUBR).
//
// A restricted staged set (Produced$ "Combo <some colours>" behind a staged
// cost) records only picked labels, so the set's own order is not recoverable
// here and its multi-amount message order with it: it keeps the plain rule
// rather than scripting amounts against a guessed order.
func stagedManaAllocationAnswers(d rules.OracleDecision) ([]XAnswer, bool) {
	if d.Kind != "choose_n" || d.Resume != "" || d.Min != d.Max || d.Max != len(d.Picks) || d.Max < 2 {
		return nil, false
	}
	if d.Options != d.Max*5 {
		return nil, false
	}
	counts := map[byte]int{}
	for k := range d.Picks {
		if pickKind(d, k) != "mana" {
			return nil, false
		}
		code, ok := stagedAllocationColour(d, k)
		if !ok {
			return nil, false
		}
		counts[code]++
	}
	as := make([]XAnswer, 0, 5)
	for _, code := range []byte("WUBRG") {
		as = append(as, XAnswer{d.Seat, "amount", strconv.Itoa(counts[code])})
	}
	return as, true
}

// stagedAllocationColour names the WUBRG colour one picked option of a
// cost-staged allocation allocates: the pick's label with its cost prefix
// stripped ("Add W", the same read the Forge driver's manaColor applies with
// its lastIndexOf(": Add ")) is the authority, the option's index into the
// WUBRG layout (unit*5 + colourIndex) the fallback for an unlabelled pick.
func stagedAllocationColour(d rules.OracleDecision, k int) (byte, bool) {
	if k < len(d.Picks) {
		label := d.Picks[k]
		if label == "" && k < len(d.PickRefs) {
			label = oraclediffRefName(d.PickRefs[k])
		}
		if label != "" {
			i := strings.LastIndex(label, ": ")
			if i < 0 {
				// A bare "Add W" is a resolution-time colour ask, whose
				// resume kind this gate requires to be absent: never
				// second-guess it from the label alone.
				return 0, false
			}
			label = label[i+2:]
			if strings.HasPrefix(label, "Add ") && len(label) == 5 && strings.IndexByte("WUBRG", label[4]) >= 0 {
				return label[4], true
			}
			return 0, false
		}
	}
	if k < len(d.PickIdx) && d.PickIdx[k] >= 0 {
		return "WUBRG"[d.PickIdx[k]%5], true
	}
	return 0, false
}
