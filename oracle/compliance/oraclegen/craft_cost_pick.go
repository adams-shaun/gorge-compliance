package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// craftMaterialPick reports whether decision d is the material exile selection
// of a Craft keyword ability (CR 702.167a). It is the IR-aware counterpart of
// the activate template's craftCost/costAnswerKind: the routed answer must use
// the same queue the template scripts, because xprep.Prepare recomputes the
// answers through XAnswersForScenario and never runs the template.
//
// Craft's material is a TargetCardInGraveyardBattlefieldOrStack chosen by
// CraftAbility.CraftCost.pay -> player.chooseTarget, the TARGET queue. Every
// other exile cost (ExileFromGraveCost, ExileTargetCost, ExileFromHandCost and
// ExileXFromYourGraveCost's generated ExileFromGraveCost, fixed or open-ended)
// pays through getTargets().choose -> player.choose, the CHOICE queue. The
// distinction is the resolving ability, not the decision shape: gorge emits an
// "exilecost" pick either way, so a shape-only gate would wrongly route Craft
// material to the choice queue.
//
// The source ability is resolved structurally, not by card name or cost text:
// the step's recorded activate ability (including a granted or gained one,
// through ResolveActivatedAbility) must carry the Keyword$ Craft that
// gorge's kwCraft installer writes. A card with Craft on one ability and an
// unrelated exilecost cost on another can therefore never be confused.
func craftMaterialPick(reg *cards.Registry, sc Scenario, g rules.OracleResult, d rules.OracleDecision) bool {
	if reg == nil || d.Step < 0 || d.Step >= len(sc.Steps) || !hasExileCostPick(d) {
		return false
	}
	st := sc.Steps[d.Step]
	if st.Op != "activate" || st.Card == "" {
		return false
	}
	it := Item{Scenario: Scenario{Setup: sc.Setup}}
	switch sa, kind := ResolveActivatedAbility(reg, it, d.Step, st, g); kind {
	case ActivatedGranted:
		return craftMaterialAbility(sa)
	case ActivatedGrantedUnresolved:
		// A granted ability the resolver cannot place must fail closed: the
		// printed index may alias a different ability.
		return false
	}
	if st.AbilityIndex == nil {
		return false
	}
	c, ok := reg.Lookup(oraclediffRefName(st.Card))
	if !ok {
		return false
	}
	idx := *st.AbilityIndex
	for _, f := range digFaces(c, st.Card) {
		if f != nil && idx >= 0 && idx < len(f.Abilities) {
			return craftMaterialAbility(f.Abilities[idx])
		}
	}
	return false
}

// craftMaterialAbility reports whether sa is a Craft keyword's activated
// ability. kwCraft stamps Params["Keyword"] = "Craft" on the ability it
// installs, so this matches the expanded keyword for every carrier (uniform,
// announced-X and multi-slot) without re-parsing ExileCtrlOrGrave.
func craftMaterialAbility(sa *cards.SA) bool {
	return sa != nil && strings.EqualFold(strings.TrimSpace(sa.Params["Keyword"]), "Craft")
}

// hasExileCostPick reports whether any of d's picks is the engine's
// exilecost option kind.
func hasExileCostPick(d rules.OracleDecision) bool {
	for _, kind := range d.PickKinds {
		if kind == "exilecost" {
			return true
		}
	}
	return false
}
