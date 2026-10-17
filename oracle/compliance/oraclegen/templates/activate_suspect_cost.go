package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// A Sac cost naming a SUSPECTED creature (Rune-Brand Juggler's
// "Sacrifice a suspected creature", Sac<1/Creature.IsSuspected>) has no setup
// fixture: suspicion is a designation (CR 701.60) only an effect confers, so
// a prelude casts a creature that suspects itself as it enters and resolves
// its trigger. Both the cost gate (activationCostIn via sacFixtureSupported)
// and the prelude (withSuspectCostPrelude) read suspectCostNeeds, so what the
// gate admits is exactly what the prelude makes.

// suspectSelfMaker is the creature that suspects itself on entering: one red
// mana, its only entry trigger targets nothing.
const suspectSelfMaker = "Frantic Scapegoat"

// suspectCostNeeds reports how many suspected creatures a Sac token asks for:
// a literal count over the one base Creature.IsSuspected (no other
// qualifier). ok is false for any other token.
func suspectCostNeeds(tok string) (int, bool) {
	if !strings.HasPrefix(tok, "Sac<") {
		return 0, false
	}
	payload, ok := bracketPayload(tok)
	if !ok {
		return 0, false
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 || !strings.EqualFold(strings.TrimSpace(parts[1]), "Creature.IsSuspected") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || n != 1 {
		return 0, false
	}
	return n, true
}

// withSuspectCostPrelude folds the suspect maker's cast into every candidate
// prelude when the cost sacrifices a suspected creature; a cost with no such
// part returns the candidates unchanged.
func withSuspectCostPrelude(reg *cards.Registry, cost string, candidates []conditionPrelude) []conditionPrelude {
	need := false
	for _, tok := range costTokens(cost) {
		if _, ok := suspectCostNeeds(tok); ok {
			need = true
		}
	}
	if !need {
		return candidates
	}
	st, ok := castProbe(reg, suspectSelfMaker)
	if !ok {
		return nil
	}
	// The cast resolves, then the maker's own "suspect it" entry trigger.
	maker := conditionPrelude{hand: []string{suspectSelfMaker}, steps: []oraclegen.Step{st, {Op: "resolve"}, {Op: "resolve"}}}
	out := make([]conditionPrelude, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, mergeConditionPreludes([]conditionPrelude{maker, c}))
	}
	return out
}
