package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// The gorge picks XMage poses as ONE boolean chooseUse (TestPlayer reads
// "yes"/"no" from the CHOICE queue; a label is never consumed there and the
// ask strict-misses "Missing CHOICE def ... Use Yes/No in unit tests for
// text", xp-xmage-choice-def-residual2).

// unleashDecision reports gorge's as-enters Unleash election (CR 702.98a:
// "you may have this permanent enter with a +1/+1 counter"). XMage's
// UnleashReplacementEffect asks controller.chooseUse("Unleash <name>?"):
// "Enter with a +1/+1 counter" is yes, "Enter without a counter" is no.
func unleashDecision(d rules.OracleDecision) bool {
	return len(d.Picks) == 1 && pickKind(d, 0) == "unleash"
}

// unleashAnswer is the chooseUse answer of an unleashDecision.
func unleashAnswer(d rules.OracleDecision) string {
	if strings.EqualFold(strings.TrimSpace(d.Picks[0]), "Enter with a +1/+1 counter") {
		return "yes"
	}
	return "no"
}

// booleanDialog names the two gorge picks of a two-face choice XMage poses as
// one chooseUse: yes is the pick XMage's TRUE branch (its trueText) takes.
type booleanDialog struct {
	// ask is XMage's chooseUse message, for the census and the reader.
	ask string
	yes string
	no  string
}

// xmageBooleanDialogFaces maps a face (lower-cased) whose gorge two-way pick
// XMage asks as a chooseUse (message, trueText, falseText) to the pick labels
// of each branch. The gorge side of each is NOT a GenericChoice, so
// xmageChooseUseFaces (which keys a Choices$ pair) cannot carry them:
//
//	Extinction Event      ChooseEvenOdd; chooseUse("Odd or even?", "Odd", "Even")
//	Winding Way           ChooseType Creature,Land; chooseUse("Creature or Land?", "Creature", "Land")
//	Domri, Anarch of Bolas  Produced$ Combo R G; chooseUse("Choose a color of mana to add", "Red", "Green")
//
// Membership is proven against the XMage source at XMAGE_REF 7aeb547a86 (each
// class's chooseUse, quoted above). The gorge labels are the engine's own:
// ChooseEvenOdd's options "odd"/"even" (engine/effects/choose.go), the card
// type names of ChooseType, and the Combo mana ask's "Add R"/"Add G". The
// land-or-nonland election of Abundant Harvest IS a GenericChoice and is an
// xmageChooseUseFaces entry (TRUE = "Land", the first Choices$ label).
// TestXMageBooleanDialogsCensus counts the table against the corpus faces.
var xmageBooleanDialogFaces = map[string]booleanDialog{
	"extinction event":       {"Odd or even?", "odd", "even"},
	"winding way":            {"Creature or Land?", "Creature", "Land"},
	"domri, anarch of bolas": {"Choose a color of mana to add", "Add R", "Add G"},
}

// stepSourceNames lists, lower-cased, the cards that can be resolving or
// acting at a decision's step: the stack snapshot's sources (top first, the
// object that resolves) then the step's own cast/activate card. The decision
// carries no card of its own, so a face-keyed table keys off this.
func stepSourceNames(sc Scenario, snaps []rules.OracleSnapshot, step int) []string {
	var out []string
	seen := map[string]bool{}
	add := func(ref string) {
		if ref == "" {
			return
		}
		name := strings.ToLower(oraclediffRefName(ref))
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	if step >= 0 && step < len(snaps) {
		for _, o := range snaps[step].Stack {
			add(o.Source)
		}
	}
	if step >= 0 && step < len(sc.Steps) {
		add(sc.Steps[step].Card)
	}
	return out
}

// booleanDialogAnswer answers a one-pick decision as the chooseUse of a
// xmageBooleanDialogFaces face resolving at its step, or "" when none applies.
func (r *answerRouting) booleanDialogAnswer(i int) string {
	d := r.ds[i]
	if len(d.Picks) != 1 {
		return ""
	}
	for _, name := range r.sources[d.Step] {
		bd, ok := xmageBooleanDialogFaces[name]
		if !ok {
			continue
		}
		switch {
		case strings.EqualFold(d.Picks[0], bd.yes):
			return "yes"
		case strings.EqualFold(d.Picks[0], bd.no):
			return "no"
		}
	}
	return ""
}

// xmageDigMayAskFaces names the faces whose "you may put a <kind> card from
// among them into your hand" Dig XMage poses as a chooseUse BEFORE the card
// pick, then the pick as LookLibraryAndPickControllerEffect's chooseTarget
// (the TARGET queue). Gorge records one optional pick (Min 0), so the answers
// are yes + the pick, or a lone no for a declined one. The value is XMage's
// chooseUse message, from the class at XMAGE_REF 7aeb547a86:
//
//	AdventurousImpulse, BondOfFlourishing, CommuneWithDinosaurs,
//	AncientStirrings, SeekTheWilds
//	    LookLibraryAndPickControllerEffect(n, 1, filter, HAND, BOTTOM_ANY):
//	    "Reveal a <kind> card and put it into your hand?"
//	GrislySalvage, ScoutTheBorders, MalevolentRumble
//	    RevealLibraryPickControllerEffect(n, 1, filter, HAND, GRAVEYARD):
//	    "Put a <kind> card from among them into your hand?"
//
// A Dig of the same IR shape is NOT listed by shape: Sage of Days' "you may
// put one back on top" is the same Forge Optional$ Dig but XMage's up-to
// target with no chooseUse (measured), so membership is per proven class
// (TestChoiceDialogFacesCensus pins the table against the corpus).
var xmageDigMayAskFaces = map[string]string{
	"adventurous impulse":    "Reveal a creature or land card and put it into your hand?",
	"ancient stirrings":      "Reveal a colorless card and put it into your hand?",
	"bond of flourishing":    "Reveal a permanent card and put it into your hand?",
	"commune with dinosaurs": "Reveal a Dinosaur or land card and put it into your hand?",
	"seek the wilds":         "Reveal a creature or land card and put it into your hand?",
	"grisly salvage":         "Put a creature or land card from among them into your hand?",
	"scout the borders":      "Put a creature or land card from among them into your hand?",
	"malevolent rumble":      "Put a permanent card from among them into your hand?",
}

// digMayAnswers is the may-ask + pick of an optional Dig take on a
// xmageDigMayAskFaces face resolving at the decision's step.
func (r *answerRouting) digMayAnswers(i int) ([]XAnswer, bool) {
	d := r.ds[i]
	if d.Resume != "dig" || d.Min != 0 || d.Max < 1 || d.Kind != "choose_n" {
		return nil, false
	}
	listed := false
	for _, name := range r.sources[d.Step] {
		if _, ok := xmageDigMayAskFaces[name]; ok {
			listed = true
			break
		}
	}
	if !listed {
		return nil, false
	}
	for k, p := range d.Picks {
		if pickKind(d, k) != "dig" || digBottomName(p) != "" {
			return nil, false
		}
	}
	if len(d.Picks) == 0 {
		return []XAnswer{{d.Seat, "choice", "no"}}, true
	}
	return []XAnswer{{d.Seat, "choice", "yes"}, {d.Seat, "target", strings.Join(d.Picks, "^")}}, true
}

func (r *answerRouting) digMayOwned(i int) bool {
	_, ok := r.digMayAnswers(i)
	return ok
}
