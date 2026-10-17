package templates

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Token fixtures for the granted-ability offered observation (static_granted.go):
// a grant whose recipient or whose granted ability's target must be a token,
// which no setup seat can hold. The prelude casts staticTokenSpell (Dragon
// Fodder, two 1/1 Goblin tokens); the control replays the same prelude without
// the source, so an offer is still the grant's doing.

// grantTokenRecipientRef is the scenario ref of the first token the prelude
// makes: the offer probe a token-only grant lands on.
var grantTokenRecipientRef = "token:" + staticTokenKind

// affectedNamesToken reports whether a grant's Affected$ filter selects
// tokens (the positive "token" word, not "!token").
func affectedNamesToken(st cards.Static) bool {
	return hasWord(affectedWords(st.ParamStr(cards.PKAffected)), "token")
}

// grantTokenPrelude is the token fixture's prelude; nextTurn ends p0's turn
// after it and returns to p0's next main phase, so the tokens are no longer
// summoning sick (CR 302.6: a {T} ability -- a mana ability included -- needs
// a creature its controller has controlled since the turn began).
func grantTokenPrelude(reg *cards.Registry, nextTurn bool) (conditionPrelude, bool) {
	pre, ok := tokenCostPrelude(reg, []tokenNeed{{staticTokenKind, 1}})
	if !ok {
		return conditionPrelude{}, false
	}
	if nextTurn {
		pre.steps = append(pre.steps,
			oraclegen.Step{Op: "pass_to", Step: "end"},
			oraclegen.Step{Op: "pass_to", Step: "main1", Active: "p0"})
	}
	return pre, true
}

// grantTokenRecipientTry is the offered try for a mana-ability grant only
// creature tokens receive: the token the prelude made on the previous turn is
// the probe.
func grantTokenRecipientTry(reg *cards.Registry, st cards.Static, label string) (offerTry, bool) {
	if !affectedNamesToken(st) {
		return offerTry{}, false
	}
	pre, ok := grantTokenPrelude(reg, true)
	if !ok {
		return offerTry{}, false
	}
	return offerTry{probe: grantTokenRecipientRef, kind: "activate", label: label,
		prelude: pre.steps, extraHand: pre.hand}, true
}

// grantTokenTargetTries are the activated-ability tries again with a token
// made this turn, for a granted ability whose targets must be tokens.
func grantTokenTargetTries(reg *cards.Registry, sa *cards.SA, tries []offerTry) []offerTry {
	if sa == nil || !hasWord(affectedWords(sa.ParamStr(cards.PKValidTgts)), "token") {
		return nil
	}
	pre, ok := grantTokenPrelude(reg, false)
	if !ok {
		return nil
	}
	out := make([]offerTry, 0, len(tries))
	for _, t := range tries {
		t.prelude = append(append([]oraclegen.Step(nil), t.prelude...), pre.steps...)
		t.extraHand = append(append([]string(nil), t.extraHand...), pre.hand...)
		out = append(out, t)
	}
	return out
}
