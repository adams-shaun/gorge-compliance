package mtgish

import "strings"

// Normaliser fixes for the actions and rules the mtgish head at 1553384
// spells with new, more general constructors (a "puttable" operand where
// there were one action per zone pair, ForEachPlayer variants, the
// once-each-turn wrappers). Each says what the older spelling said.
func init() {
	// A permanent that does not untap during its untap step.
	d2RuleSynonyms["DoesntUntapDuringUntapStep"] = "rule:doesntuntapduringcontrollersuntap"
	extRule["DecreaseBuybackCosts"] = staticRule("costreduction")
	extRule["PermanentsEnteringTheBattlefieldDontCauseAbilitiesOfPermanentsToTrigger"] = staticRule(tokDisableTriggers)
	// "Infinity" wraps one Rule (the Infinity Stones' own ability).
	wrapperRules["Infinity"] = true

	extAction["ForEachPlayerCreateTokens"] = c1Tokens(1)
	extAction["ForEachPlayerMayCreateTokens"] = c1Tokens(1)
	// "Once each turn, you may pay <cost>. If you do, ...": a MayCost.
	extAction["MayCostOnceEachTurn"] = func(e *effects, n node) { e.p2cMayCost(argN(n, 0)) }
	wrapperActions["MayActionsOnceEachTurn"] = true

	extAction["PutOnBattlefield"] = p3aMove("battlefield", 0)
	extAction["PutInHand"] = p3aMove("hand", 0)
	extAction["PutInOwnersHand"] = p3aMove("hand", 0)
	extAction["PutInGraveyard"] = p3aMove("graveyard", 0)
	extAction["PutAllCardsFromHandOnTopOfLibraryInAnyOrder"] = c1Tok("move:hand>library")
	extAction["ChooseNumberCardsOfTypeInHand"] = c1None
	extAction["PutNumberSpecificCardsFromHandOnTopOfLibraryAnyOrder"] = func(e *effects, n node) {
		e.add("move:hand>library:" + gameNumber(argN(n, 0)))
	}
	extAction["RevealHandAndPlayerMayChooseACardToPutOnBattlefield"] = c1Tok("move:hand>battlefield")
	extAction["LookAtPlayersHandAndMayChooseACardToPutOnBattlefield"] = c1Tok("move:hand>battlefield")
	extAction["RevealHandAndPlayerChoosesNumberCardsOfTypeToDiscard"] = c1Tok("discard:1")
	extAction["PutACardOfTypeAndOrACardOfTypeMilledThisWayIntoHand"] = c1Tok("move:graveyard>hand")
	extAction["CopyEachSpellNumberTimesAndMayChooseNewTargets"] = c1Tok("copyspell")
	extAction["CopySpellForEachPlayerAndMustChooseThemOrPermanentTheyControlAsNewTarget"] = c1Tok("copyspell")
	extAction["GainControlOfAPermanentControlledByEachPlayerUntil"] = c1Tok("gaincontrol")
	extAction["OnlyAllowedAttackingPlayersUntil"] = c1Tok("rule:cantattack")
	extAction["OnlyAllowedCastingPlayersUntil"] = c1Tok("player:cantcastspells")
	extAction["CastAnyNumberOfGroupSpellsFromAmongCopiedCardsWithoutPaying"] = c1Tok("play")
	extAction["PermanentDealsDamageCantBePrevented"] = c1Tok("damage:*")
	extAction["ChooseACardOfTypeFromAmongThoseFormerCardsInGraveyards"] = c1None
	extAction["EachPlayerChoosesACardInHand"] = c1None
	extAction["RevealCardsFromTheTopOfPlanarDeckUntilRevealNumberPlaneCardsAndPutAPlaneCardFromAmongOnTopAndTheRestOnBottomInARandomOrder"] = c1Tok(
		"diguntil", "move:library>planardeck", "move:planardeck>planardeck", "move:planardeck>planardeck", "planeswalk")
	extLayer["AddAbilityFromEachExiledHasable"] = func(any) []string { return []string{"grant:*"} }
}

// p3aMove maps a "put <puttables> into <dest>" action: one move per operand,
// from the zone the operand names.
func p3aMove(dest string, i int) func(e *effects, n node) {
	return func(e *effects, n node) {
		for _, x := range list(argN(n, i)) {
			e.add("move:" + p3aZone(x) + ">" + dest)
		}
	}
}

// p3aZone is the zone a puttable operand names.
func p3aZone(x any) string {
	s := strings.ToLower(variantOf(x) + " " + p3aVariants(argsOf(x)))
	switch {
	case strings.Contains(s, "librar"):
		return "library"
	case strings.Contains(s, "graveyard"), strings.Contains(s, "milled"), strings.Contains(s, "discarded"), strings.Contains(s, "sacrificed"):
		return "graveyard"
	case strings.Contains(s, "hand"):
		return "hand"
	case strings.Contains(s, "exile"):
		return "exile"
	case strings.Contains(s, "permanent"):
		return "battlefield"
	}
	return "*"
}

func p3aVariants(a []any) string {
	var sb strings.Builder
	for _, x := range a {
		sb.WriteString(variantOf(x))
		sb.WriteByte(' ')
		sb.WriteString(p3aVariants(argsOf(x)))
		sb.WriteByte(' ')
	}
	return sb.String()
}

// p3aEachRuleToken is ruleToken for a rule an action or static gives to a
// group of permanents. "All creatures able to block it do so" is spelled
// mtgish-side as every creature getting MustBlockAttacker; the script's
// hidden keyword (and gorge's token) names it as one rule.
func p3aEachRuleToken(action string, perms any, rule string) string {
	if rule == "MustBlockAttacker" && strings.Contains(action, "Each") {
		return "rule:allcreaturesmustblockit"
	}
	return ruleToken(rule)
}
