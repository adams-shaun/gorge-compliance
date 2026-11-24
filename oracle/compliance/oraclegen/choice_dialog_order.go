package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/rules"
)

// chooseUseBottomOrders scripts the card-order picks XMage asks AFTER a
// chooseUse whose true branch puts the whole looked-at window on the bottom
// of the library in any order. Petals of Insight is the shape: its Forge
// GenericChoice's ReturnPetals branch is
//
//	DB$ Dig | DigNum$ 3 | ChangeNum$ All | DestinationZone$ Library |
//	LibraryPosition$ -1
//
// and XMage's PetalsOfInsightEffect calls
// putCardsOnBottomOfLibrary(cards, game, source, true), which asks "Select a
// card ORDER to put on the BOTTOM" once per card but the last (the last one
// chosen is bottommost). Gorge's Dig bottom arm poses no order ask, so the
// mode decision carries only the boolean: scripting just the yes leaves the
// order dialog to TestPlayer's AI and the strict replay reports a second
// "Missing CHOICE def" at the same ability (measured on the find).
//
// The looked-at cards are the before-step checkpoint's library top (the Dig's
// DigNum$). Only an all-identical window is answered: the order is free, so
// any picks leave gorge's library unchanged and the snapshot agrees. A
// distinct-card window would need gorge's own order, which it never recorded;
// it stays unscripted rather than guessing a placement the comparator would
// read as a divergence.
//
// The answers are keyed by step because the mode arm has no decision index of
// its own (its pick loop shadows it), and a step poses at most one such
// chooseUse.
func chooseUseBottomOrders(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, ds []rules.OracleDecision) map[int][]XAnswer {
	out := map[int][]XAnswer{}
	if reg == nil {
		return out
	}
	for _, d := range ds {
		if d.Resume != "modes" || len(d.Picks) != 1 || d.Step < 0 || d.Step >= len(snaps) {
			continue
		}
		for _, name := range decisionSourceNames(sc, snaps, d) {
			card, ok := reg.Lookup(name)
			if !ok {
				continue
			}
			for _, f := range card.Faces {
				if f == nil || !xmageChooseUseFaces[strings.ToLower(f.Name)] {
					continue
				}
				n, ok := chooseUseBottomOrderSize(f, d.Picks[0])
				if !ok {
					continue
				}
				if as := bottomOrderAnswers(snaps[d.Step], d.Seat, n); len(as) > 0 {
					out[d.Step] = as
				}
			}
		}
	}
	return out
}

// chooseUseBottomOrderSize reports how many cards the picked Choices$ label
// moves to the bottom: the label's SVar must be a Dig of the whole window
// (ChangeNum$ All) to a library bottom (LibraryPosition$ -1, Forge's default
// when omitted). ok is false for any other branch or face shape, so a
// chooseUse whose true branch draws, returns to hand or exiles is untouched.
func chooseUseBottomOrderSize(f *cards.Face, label string) (int, bool) {
	if f == nil {
		return 0, false
	}
	for _, body := range sortedSVarBodies(f) {
		p := svarParams(body)
		if p["DB"] != "GenericChoice" {
			continue
		}
		for _, name := range strings.Split(p["Choices"], ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name) != label {
				continue
			}
			dig := svarParams(f.SVars[name])
			if dig["DB"] != "Dig" || !strings.EqualFold(strings.TrimSpace(dig["ChangeNum"]), "All") {
				return 0, false
			}
			switch strings.TrimSpace(dig["DestinationZone"]) {
			case "", "Library":
			default:
				return 0, false
			}
			if pos := strings.TrimSpace(dig["LibraryPosition"]); pos != "" && pos != "-1" {
				return 0, false
			}
			n, err := strconv.Atoi(strings.TrimSpace(dig["DigNum"]))
			if err != nil || n < 2 {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}

// bottomOrderAnswers is the N-1 card-order choices XMage asks for an
// all-identical window of n cards, or nil when the before-step library top
// does not hold n cards of one name (the free order would then be observable
// and gorge recorded no order to match).
func bottomOrderAnswers(s rules.OracleSnapshot, seat, n int) []XAnswer {
	if n < 2 {
		return nil
	}
	var top []string
	for _, p := range s.Players {
		if p.Seat == seat {
			top = p.LibraryTop
			break
		}
	}
	if len(top) < n {
		return nil
	}
	name := top[0]
	for _, got := range top[:n] {
		if got != name {
			return nil
		}
	}
	out := make([]XAnswer, 0, n-1)
	for i := 0; i < n-1; i++ {
		out = append(out, XAnswer{seat, "choice", name})
	}
	return out
}

// sortedSVarBodies returns the face's SVar bodies in sorted key order, so a
// scan can never depend on map iteration order.
func sortedSVarBodies(f *cards.Face) []string {
	keys := svarKeys(f)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, f.SVars[k])
	}
	return out
}
