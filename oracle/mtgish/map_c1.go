package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit C1 of the mtgish cross-check: mtgish Action variants A through L.
// Each is mapped by family to the token the gorge side emits for the same
// game action, or to nothing when the node is not itself an effect (a
// choice whose result feeds a later action, a wrapper whose nested actions
// the walk visits, bookkeeping). A mapped action's nested actions are still
// walked unless it is registered as a leaf below.

func init() {
	for v, f := range c1Actions {
		extAction[v] = f
	}
	// These summarise their own nested actions (alternatives, or a payload
	// whose actions must not be walked a second time).
	for _, v := range []string{
		"Clash", "FaceAVillianousChoice", "EachPlayerMakesAVillainousChoice",
		"ChooseActionAtRandom", "ChooseANamedAction", "EachPlayerChoosesAnAction",
		"CreateTokensForEachPlayer", "CreateTokensForEachPermanent", "EachPlayerMayCreateTokens",
		"EachPlayerCreatesTokensOfTheirChoice",
	} {
		leafActions[v] = true
	}
}

// c1None maps an action to nothing (see the family comments for why).
func c1None(*effects, node) {}

// c1Tok maps an action to fixed tokens.
func c1Tok(toks ...string) func(*effects, node) {
	return func(e *effects, _ node) { e.add(toks...) }
}

// c1Num maps an action to "<verb>:<amount>", the amount its i'th argument.
func c1Num(verb string, i int) func(*effects, node) {
	return func(e *effects, n node) { e.add(verb + ":" + gameNumber(argN(n, i))) }
}

// c1Alts adds tok (when not empty) and summarises the branches pick returns
// as alternatives: exactly one of them runs.
func c1Alts(tok string, pick func(n node) []any) func(*effects, node) {
	return func(e *effects, n node) {
		if tok != "" {
			e.add(tok)
		}
		e.alternatives(pick(n)...)
	}
}

// c1Tokens maps a for-each token creation by its creatable tokens.
func c1Tokens(i int) func(*effects, node) {
	return func(e *effects, n node) {
		for _, t := range list(argN(n, i)) {
			e.add(tokenToken(t))
		}
	}
}

// c1PlayerEffects maps the PlayerEffect list in argument i.
func c1PlayerEffects(i int) func(*effects, node) {
	return func(e *effects, n node) {
		for _, x := range list(argN(n, i)) {
			e.add(playerEffectTokensP2E(x)...)
		}
	}
}

// c1Rules maps the PermanentRule list in argument i, as the existing
// CreatePermanentRuleEffectUntil does.
func c1Rules(i int) func(*effects, node) {
	return func(e *effects, n node) {
		for _, r := range list(argN(n, i)) {
			e.add(ruleToken(variantOf(r)))
		}
	}
}

// c1Layers maps a LayerEffect-shaped list in argument i (graveyard-card,
// card-in-hand and spell-or-permanent effects share the layer variants'
// names: AddAbility, SetPT, AddColor, AddCreatureType ...).
func c1Layers(i int) func(*effects, node) {
	return func(e *effects, n node) { e.add(layerEffects(list(argN(n, i)))...) }
}

// c1SpellEffects maps the SpellEffect list in argument i: what a future or
// each spell gets ("the next spell you cast has cascade", "costs {1} less").
func c1SpellEffects(i int) func(*effects, node) {
	return func(e *effects, n node) { e.add(spellEffectTokens(list(argN(n, i)))...) }
}

// spellEffectTokens maps SpellEffect variants the way the gorge side maps an
// Effect's stack statics: a keyword the spell gains is "spell:<keyword>",
// a cost change is costreduction/costincrease.
func spellEffectTokens(xs []any) []string {
	var out []string
	for _, x := range xs {
		v := variantOf(x)
		switch {
		case strings.HasPrefix(v, "DecreaseManaCost"):
			out = append(out, "costreduction")
		case strings.HasPrefix(v, "IncreaseManaCost"), strings.HasPrefix(v, "AdditionalCost"):
			out = append(out, "costincrease")
		case v == "MayCastAsThoughItHadFlash":
			out = append(out, "flash")
		case v == "AddAbility" || v == "AddAbilityUntil":
			rs := list(rawArgs(x))
			if v == "AddAbilityUntil" {
				rs = []any{argN(x, 0)}
			}
			for _, r := range rs {
				if g := grantToken(r); g == "grant:ability" {
					out = append(out, g)
				} else {
					out = append(out, "spell:"+strings.TrimPrefix(g, "grant:"))
				}
			}
		case v == "CantBeCounteredBySpells":
			// "can't be countered by <colour> spells": the same rule.
			out = append(out, "spell:cantbecountered")
		case v == "AdjustPT":
			out = append(out, pump(signed(intOr(argN(x, 0))), signed(intOr(argN(x, 1)))))
		case v == "SetPT":
			out = append(out, "setpt")
		case v == "AddColor" || v == "SetColor":
			out = append(out, "color")
		case strings.Contains(v, "CreatureType"):
			out = append(out, "type")
		case v == "EntersWithACounter":
			out = append(out, "counters:"+counterType(rawArgs(x))+":1")
		case v == "EntersWithNumberCounters":
			out = append(out, "counters:"+counterType(argN(x, 1))+":"+gameNumber(argN(x, 0)))
		case v == "EntersWithACounterOfChoice":
			out = append(out, "counters:*")
		case v == "EntersWithLayerEffect" || v == "EntersWithLayerEffectUntil":
			out = append(out, layerEffects(list(argN(x, 0)))...)
		case v == "IfPermanentSpell":
			out = append(out, spellEffectTokens(list(rawArgs(x)))...)
		case v == "If":
			out = append(out, spellEffectTokens([]any{argN(x, 1)})...)
		case v == "CastWithPerpetualEffect":
			out = append(out, "perpetual")
		case v == "Wither" || v == "Delve" || v == "SplitSecond" || v == "Conspire" || v == "Undaunted" ||
			v == "CantBeCopied" || v == "CantBeCountered" || v == "Ripple" || v == "Affinity" ||
			v == "Devour" || v == "Blitz" || v == "Replicate" || v == "Evoke" || v == "Prowl" ||
			v == "Emerge" || v == "Freerunning" || v == "Offspring" || v == "WebSlinging" ||
			v == "Bloodthirst":
			out = append(out, "spell:"+strings.ToLower(v))
		default:
			if tok := keywordRuleToken(v); tok != "" {
				out = append(out, "spell:"+tok)
			} else {
				out = append(out, "effect")
			}
		}
	}
	return out
}

// keywordRuleToken is the canonical keyword a keyword-named variant
// (Cascade, Storm, Convoke, ...) stands for, "" for anything else.
func keywordRuleToken(v string) string {
	for _, m := range []map[string]string{simpleKeywords, costKeywords, numberKeywords, bareKeywords} {
		if tok, ok := m[v]; ok {
			return tok
		}
	}
	return ""
}

// c1CopyWithEffects maps a spell copy with modifiers: the copy, plus any
// ability it gains (a permanent copy "gains haste"), as scripts grant it.
func c1CopyWithEffects(e *effects, n node) {
	e.add("copyspell")
	for _, x := range list(argN(n, 1)) {
		switch variantOf(x) {
		case "AddAbility":
			for _, r := range list(rawArgs(x)) {
				e.add(grantToken(r))
			}
		case "AddAbilityUntil":
			e.add(grantToken(argN(x, 0)))
		}
	}
}

// exiledCardEffects maps ExiledCardEffect variants: an ability the exiled
// card gains (suspend, "you may cast it"), or its becoming plotted (Forge
// AlterAttribute Plotted). Becoming foretold is how it was exiled (scripts'
// ChangeZone with Foretold$), not a further effect.
func exiledCardEffects(xs []any) []string {
	var out []string
	for _, x := range xs {
		switch v := variantOf(x); v {
		case "AddAbility", "AddAbilityIfItDoesntHaveIt":
			for _, r := range list(rawArgs(x)) {
				out = append(out, grantToken(r))
			}
		case "IsPlotted":
			out = append(out, "alterattribute")
		case "IsForetold", "IsForetoldForCost":
		default:
			out = append(out, unmapped("ExiledCardEffect."+v))
		}
	}
	return out
}

// gameEffectToken maps a GameEffect the way the gorge side maps the static
// an Effect creates for it.
func gameEffectToken(x any) string {
	switch variantOf(x) {
	case "DamageCantBePrevented":
		return "static:damagecantbeprevented"
	case "CreaturesCantBlock":
		return "rule:cantblock"
	}
	return "effect"
}

// futurePlayerEffectToken maps a FuturePlayerEffect ("you may cast your
// next spell this turn as though it had flash", ...).
func futurePlayerEffectToken(x any) string {
	v := variantOf(x)
	switch {
	case strings.Contains(v, "Flash"):
		return "flash"
	case strings.Contains(v, "AlternateCost"):
		return "alternativecost"
	case strings.HasPrefix(v, "CanCastOnlyOneMore"):
		return "player:cantcastspells"
	case strings.Contains(v, "WithoutPaying"):
		return "mayplay"
	}
	return unmapped("FuturePlayerEffect." + v)
}

// costAsAction maps a Cost that a player must pay as an effect ("each
// opponent discards a card"): the action the cost names. A mana payment is
// not an effect.
func (e *effects) costAsAction(c any) {
	v := variantOf(c)
	switch {
	case v == "And":
		for _, x := range argsOf(c) {
			e.costAsAction(x)
		}
	case v == "Or":
		e.alternativeCosts(argsOf(c))
	case v == "PayLife":
		e.add("loselife:" + gameNumber(rawArgs(c)))
	case strings.HasPrefix(v, "PayMana"):
	default:
		e.action(node{"_Action": v, "args": rawArgs(c)}, v)
	}
}

// alternativeCosts merges costs of which one is paid.
func (e *effects) alternativeCosts(cs []any) {
	var subs []*effects
	for _, c := range cs {
		sub := &effects{}
		sub.costAsAction(c)
		subs = append(subs, sub)
	}
	e.merge(subs)
}

// c1Actions is unit C1's table, by family.
var c1Actions = map[string]func(e *effects, n node){
	// Choices whose result feeds a later action map to nothing, as the gorge
	// side maps NameCard, ChooseCard, ChoosePlayer, ChooseType, ChooseColor,
	// ChooseNumber and TwoPiles to nothing. The later action carries the
	// verb.
	"ChooseACardName": c1None, "ChooseACardNameThatHasntBeenChosen": c1None,
	"ChooseAPlayer": c1None, "ChooseAPlayerAtRandom": c1None,
	"ChooseAPlayerOrPlaneswalkerCurrentlyAttackedByPlayer": c1None,
	"ChooseACardInHand": c1None, "ChooseACardInHandAtRandom": c1None,
	"ChooseACardInHandOfEachColor": c1None, "ChooseACardInHandOrAPermanent": c1None,
	"ChooseACardOfTypeInPlayersHandAtRandom": c1None, "ChooseNumberCardsInHand": c1None,
	"ChooseUptoNumberCardsInHand": c1None, "ChooseACardFromPlayersRevealedHand": c1None,
	"ChooseNumberCardsFromAmongCardsInHandRevealedThisWay":            c1None,
	"ChooseUptoNumberCardsFromAmongCardsInPlayersHandRevealedThisWay": c1None,
	"ChooseACardFromAmongCardsDiscardedThisWay":                       c1None,
	"ChooseACardInPlayersGraveyard":                                   c1None, "ChooseACardInPlayersGraveyardAtRandom": c1None,
	"ChooseACardInEachPlayersGraveyard": c1None, "ChooseAGraveyardCard": c1None,
	"ChooseAGraveyardCardThatHasntBeenChosen": c1None, "ChooseAnUnchosenCardInPlayersGraveyard": c1None,
	"ChooseNumberCardsInEachPlayersGraveyard": c1None, "ChooseNumberCardsInGraveyards": c1None,
	"ChooseNumberGraveyardCards":                              c1None,
	"ChooseACardFromAmongTheTopNumberCardsInPlayersGraveyard": c1None,
	"ChooseAnExiledCardAtRandom":                              c1None, "ChooseUptoNumberExiledCards": c1None,
	"ChooseACardFromOutsideGame": c1None,
	"ChooseNumberPermanents":     c1None, "ChooseUptoNumberPermanents": c1None,
	"ChooseUptoOnePermanent": c1None, "ChooseAnyNumberOfPermanents": c1None,
	"ChooseAnyNumberOfGroupPermanents": c1None, "ChooseOneOrTwoPermanents": c1None,
	"ChooseAPermanentAtRandom": c1None, "ChooseASecondPermanentAtRandom": c1None,
	"ChooseAPermanentForEachPlayer": c1None, "ChooseUptoOnePermanentForEachPlayer": c1None,
	"ChooseUptoNumberPermanentsForEach": c1None, "ChooseAPermanentThatHasntBeenChosen": c1None,
	"ChooseAPermanentOfEachBasicLandTypeAvailable": c1None, "ChooseAPermanentOfEachPowerAvailable": c1None,
	"ChooseMultiplePermanentsAmoungPermanents":              c1None,
	"ChooseMultiplePermanentsAmoungPermanentsForEachPlayer": c1None,
	"ChooseAPartyFromAmongPermanents":                       c1None,
	"ChooseACommanderOnTheBattlefieldOrInTheCommandZone":    c1None,
	"ChooseASpellThatResolvedThisTurn":                      c1None,
	"ChooseADamageRecipient":                                c1None, "ChooseADamageNumber": c1None,
	"ChoosePermanentFilter": c1None, "ChooseLibraryFilter": c1None, "ChooseACheckableAbility": c1None,
	"ChooseABasicLandType": c1None, "ChooseTwoBasicLandTypes": c1None, "ChooseALandType": c1None,
	"ChooseALandTypeAndABasicLandType": c1None, "ChooseANonBasicLandType": c1None, "ChooseLandType": c1None,
	"ChooseACreatureTypeOtherThan": c1None, "ChooseACreatureType_And_ChooseACreatureTypeOtherThan": c1None,
	"ChooseACardtype": c1None, "ChooseAPermanentType": c1None, "ChooseAPlaneswalkerType": c1None,
	"ChooseAnOrderForCardTypes": c1None,
	"ChooseColors":              c1None, "ChooseTwoColorWords": c1None, "ChooseAColorOrColorless": c1None,
	"ChooseProtectionFromAColorOrFromArtifact":           c1None,
	"ChooseRandomColorPermanentDoesntHaveProtectionFrom": c1None,
	"ChooseANumber": c1None, "ChooseANumberBetween": c1None, "ChooseANumberBetweenAtRandom": c1None,
	"ChooseANumberGreaterThanNumber": c1None, "ChooseANumberFromAmongAtRandom": c1None,
	"ChooseEvenOrOdd": c1None, "ChooseLeftOrRight": c1None, "ChooseASector": c1None,
	"ChooseALetter": c1None, "ChooseWord": c1None, "ForEachPlayerChooseAWord": c1None,
	"ChoosePTMod": c1None, "ChooseAnAbilityAtRandom": c1None, "ChooseNumberAbilitiesAtRandom": c1None,
	"ChooseCopyFromCopies": c1None, "ChooseNamedPileForPermanent": c1None,
	"ChooseAPermanentPile": c1None, "ChooseAnExiledPile": c1None, "ChooseAGraveyardPile": c1None,
	"ChooseAPileCreatedByEachPlayer": c1None, "ChooseAPileCreatedByEachPlayerAtRandom": c1None,
	"ChooseAnyNumberPermanentsAndPayManaForEach": c1None,
	"GuessIfACardIsInPlayersHand":                c1None, "GuessWhichNumberWasSecretlyChosen": c1None,
	"GuessIfCardInHandPassesFilter": c1None,

	// Choices among actions: exactly one branch runs.
	"ChooseActionAtRandom": c1Alts("", func(n node) []any { return list(rawArgs(n)) }),
	"ChooseANamedAction": c1Alts("", func(n node) []any {
		var bs []any
		for _, na := range list(rawArgs(n)) {
			bs = append(bs, argN(na, 1))
		}
		return bs
	}),
	"EachPlayerChoosesAnAction": c1Alts("", func(n node) []any { return list(argN(n, 1)) }),

	// Wrappers: a player, each player or each object does the nested
	// actions, or may pay a cost first. The nested actions are walked; a
	// cost a player may pay is not an effect (as MayCost).
	"APlayerAction": c1None, "APlayerActions": c1None, "APlayerMayAction": c1None,
	"HavePlayerTakeAction": c1None, "IfHavePlayerAction": c1None,
	"EachPlayerStartingWithAction": c1None, "EachPlayerStartingWithMayAction": c1None,
	"EachPlayerStartingWithActionInDirection": c1None, "EachPlayerMayActions": c1None,
	"EachPlayerMayChooseAnAction": c1None, "EachPlayerMayChooseToDoActions": c1None,
	"EachPlayerMayCostAndFallback": c1None, "EachPlayerRepeatedMayCost": c1None,
	"EachPlayerMayCost": c1None, "ForEachPermanentAnyPlayerMayCost": c1None,
	"AnyPlayerMayPayMana":    c1None,
	"ActionForEachPermanent": c1None, "ActionForEachPermanentByController": c1None,
	"ActionForEachPermanentDestroyedThisWay": c1None, "ActionForEachExiledCard": c1None,
	"ActionForEachCardtype": c1None, "ActionForEachDistributedAnyTarget": c1None,
	"ActionForEachPermanentExiledThisWay": c1None, "ActionForEachTarget": c1None,
	"ActionForEachCheckableAbility": c1None, "ActionForEachPermanentPutInGraveyardThisWay": c1None,
	"ActionForEachPermanentThatDiedThisWay": c1None, "ActionForEachPlayerInTurnOrder": c1None,
	"ActionForEachSpellAndAbility": c1None,
	"DoNothing":                    c1None,
	// A cost each player must pay is the action it names.
	"EachPlayerMustCost": func(e *effects, n node) { e.costAsAction(argN(n, 1)) },

	// Looking at hidden cards is information, not an effect (the gorge side
	// maps RevealHand to nothing the same way); a look at the top of a
	// library is a dig, as LookAtTopOfLibrary.
	"LookAtPlayersHand": c1None, "LookAtARandomCardInPlayersHand": c1None,
	"LookAtCardsOfTypeInPlayersHand": c1None, "LookAtFaceDownExiledCards": c1None,
	"LookAtFaceDownPermanent": c1None, "LookAtEachFaceDownPermanent": c1None,
	"LookAtTheTopCardOfPlayersLibrary":                                              c1Tok("dig:1"),
	"LookAtTheTopCardOfEachPlayersLibrary":                                          c1Tok("dig:1"),
	"LookAtTheTopNumberCardsOfPlayersLibrary":                                       c1Num("dig", 1),
	"LookAtTheTopNumberCardsOfLibraryNEW":                                           c1Num("dig", 0),
	"EachPlayerRevealsTheTopNumberCardsOfLibrary":                                   c1Num("dig", 1),
	"EachPlayerRevealsCardsFromTheTopOfLibraryUntilANumberOfCardsOfTypeAreRevealed": c1Tok("diguntil"),
	"EachPlayerRevealsCardsFromTheTopOfTheirLibraryUntilTheyRevealACardOfType":      c1Tok("diguntil"),
	"Fateseal": func(e *effects, n node) { e.add("dig:"+gameNumber(argN(n, 0)), "move:library>library") },

	// Keyword actions whose keyword the card also prints: the keyword (T1)
	// carries them, and the gorge side skips the keyword's expansion.
	"AwakenPermanent": c1None, "Ascend": c1None, "Epic": c1None, "Cipher": c1None,
	// Collecting evidence is a cost paid inside the effect (scripts:
	// Cost$ CollectEvidence<N>), and a cost is not an effect.
	"CollectEvidence": c1None,
	// Bookkeeping on an amount a later action uses.
	"DoubleTheStake": c1None,
	// A copy of a card is made to be cast; the cast is the effect ("play",
	// as scripts' Play with CopyCard$).
	"CopyExiledCard": c1None, "CopyExiledCards": c1None, "CopyAnExiledCard": c1None,
	"CopyCardInHand": c1None, "CopyCard": c1None, "CopyNumberCardsAtRandom": c1None,
	"CopyEnchantedGraveyardCard": c1None, "CopyExiledCardNumberTimes": c1None,
	"CopyAnExiledCardNumberTimes": c1None,

	// Damage: the amount is the GameNumber argument.
	"HavePermanentDealDamage":             c1Num("damage", 1),
	"EachPermanentDealsDamage":            c1Num("damage", 1),
	"DiscardedCardDealsDamage":            c1Num("damage", 1),
	"HaveDiscardedCardDealDamage":         c1Num("damage", 1),
	"EmblemDealsDamage":                   c1Num("damage", 1),
	"GraveyardCardDealsDamage":            c1Num("damage", 1),
	"ExiledCardDealsDamage":               c1Num("damage", 1),
	"BoonDealsDamage":                     c1Num("damage", 1),
	"DealDamage":                          c1Num("damage", 1),
	"DeadGuestPermanentDealsDamage":       c1Num("damage", 0),
	"HaveDeadPermanentDealDamage":         c1Num("damage", 0),
	"DeadPermanentDealsDistributedDamage": c1Tok("damage:*"),
	"GraveyardCardDealsMultipleDamage": func(e *effects, n node) {
		for _, d := range list(argN(n, 1)) {
			e.add("damage:" + gameNumber(argN(d, 0)))
		}
	},

	// Life.
	"LoseLifeForEach":        c1Tok("loselife:*"),
	"LoseLifeAndLifeForEach": c1Tok("loselife:*"),
	"GainLifeAndLifeForEach": c1Tok("gainlife:*"),
	"HaveEachPlayerLoseLife": c1Num("loselife", 1),

	// Discard and draw.
	"DiscardACardAtRandom":                        c1Tok("discard:1"),
	"DiscardCard":                                 c1Tok("discard:1"),
	"DiscardACardOfType":                          c1Tok("discard:1"),
	"DiscardTheCardDrawnThisWay":                  c1Tok("discard:1"),
	"DiscardTheCardRevealedThisWay":               c1Tok("discard:1"),
	"DiscardNumberCardsAtRandom":                  c1Num("discard", 0),
	"DiscardUptoNumberCards":                      c1Num("discard", 0),
	"DiscardNumberCardsOfType":                    c1Num("discard", 0),
	"DiscardNumberCardsDrawnThisTurn":             c1Num("discard", 0),
	"DiscardAnyNumberOfCards":                     c1Tok("discard:*"),
	"DiscardAnyNumberOfCardsAtRandom":             c1Tok("discard:*"),
	"DiscardAnyNumberOfCardsOfType":               c1Tok("discard:*"),
	"DiscardCards":                                c1Tok("discard:*"),
	"DiscardEachCard":                             c1Tok("discard:*"),
	"DiscardAllButNumberCards":                    c1Tok("discard:*"),
	"LookAtPlayersHandAndChooseACardToDiscard":    c1Tok("discard:1"),
	"LookAtPlayersHandAndChooseNumCardsToDiscard": c1Num("discard", 1),
	"DrawUptoNumberCards":                         c1Num("draw", 0),
	"DrawNumberCardsForEach":                      c1Tok("draw:*"),
	"DrawNumberCardsThenDiscardNumberOfThem": func(e *effects, n node) {
		e.add("draw:"+gameNumber(argN(n, 0)), "discard:"+gameNumber(argN(n, 1)))
	},

	// Hand to another zone after looking at it.
	"LookAtPlayersHandAndChooseACardToPutOnBattlefield":                       c1Tok("move:hand>battlefield"),
	"LookAtPlayersHandAndChooseNumberCardsToPutOnTopOfTheirLibraryInAnyOrder": c1Tok("move:hand>library"),

	// Zone exchanges are two moves, as scripts write them.
	"ExchangeGraveyardAndLibrary":       c1Tok("move:graveyard>library", "move:library>graveyard"),
	"ExchangeHandAndGraveyard":          c1Tok("move:graveyard>hand", "move:hand>graveyard"),
	"ExchangeHandAndLibraryThenShuffle": c1Tok("move:hand>library", "move:library>hand"),
	"AnteTopCardOfLibrary":              c1Tok("move:library>ante"),

	// Counterspells, wherever the countered card goes.
	"CounterSpellOrAbility": c1Tok("counter"), "CounterAbility": c1Tok("counter"),
	"CounterSpellIntoExile": c1Tok("counter"), "CounterEachSpell": c1Tok("counter"),
	"CounterEachAbility": c1Tok("counter"), "CounterEachSpellAndAbility": c1Tok("counter"),
	"CounterSpellIntoTopOfLibrary": c1Tok("counter"), "CounterSpellIntoBottomOfLibrary": c1Tok("counter"),
	"CounterSpellIntoHand": c1Tok("counter"), "CounterSpellIntoTopOrBottomOfLibrary": c1Tok("counter"),
	"CounterSpellAndSpellsOfTypeAreCounteredOntoTheBattlefield": c1Tok("counter"),
	"CounterSpellIntoExileWithANumberOfCountersAndWithEffects": func(e *effects, n node) {
		e.add("counter", "counters:"+counterType(argN(n, 2))+":"+gameNumber(argN(n, 1)))
		e.add(exiledCardEffects(list(argN(n, 3)))...)
	},

	// Copying a spell or ability.
	"CopySpell": c1Tok("copyspell"), "CopySpellNumberTimes": c1Tok("copyspell"),
	"CopySpellForEach": c1Tok("copyspell"), "CopyEachSpellAndMayChooseNewTargets": c1Tok("copyspell"),
	"CopySpellNumberTimesAndMayChooseNewTargets":                         c1Tok("copyspell"),
	"CopySpellAndMustChooseNewTarget":                                    c1Tok("copyspell"),
	"CopySpellAndRandomlyChooseNewTargetsExceptFor":                      c1Tok("copyspell"),
	"CopySpellForEachPermanentAndMustChooseItAsNewTarget":                c1Tok("copyspell"),
	"CopySpellForEachOtherPermanentOrPlayerAndMustChooseThemAsNewTarget": c1Tok("copyspell"),
	"CopySpellForEachPlayerAndMustChooseNewTargetPermanentTheyControl":   c1Tok("copyspell"),
	"CopySpellForEachPlayerAndMustChooseThemAsNewTarget":                 c1Tok("copyspell"),
	"CopySpellForEachSpellPermanentCardAndOrPlayerItCouldTarget":         c1Tok("copyspell"),
	"CopySpellOrAbilityAndMayChooseNewTargets":                           c1Tok("copyspell"),
	"CopySpellOrAbilityForEachPermanentOrPlayerItCouldTarget":            c1Tok("copyspell"),
	"CopySpellOrAbilityNumberTimesAndMayChooseNewTargets":                c1Tok("copyspell"),
	"CopyAbilityAndMayChooseNewTargets":                                  c1Tok("copyspell"),
	"CopyAbilityNumberTimesAndMayChooseNewTargets":                       c1Tok("copyspell"),
	"CopyAbilityForEachPermanentItCouldTarget":                           c1Tok("copyspell"),
	"CopyActivatedAbilityAndMayChooseNewTargets":                         c1Tok("copyspell"),
	"CopyEachAbilityAndMayChooseNewTargets":                              c1Tok("copyspell"),
	"CopySpellWithModifiers":                                             c1CopyWithEffects,
	"CopySpellAndMayChooseNewTargetsWithEffects":                         c1CopyWithEffects,

	// Changing targets.
	"ChangeTargetsOfSpell": c1Tok("changetargets"), "ChangeTargetsOfAbility": c1Tok("changetargets"),
	"ChangeTheTargetOfSpell": c1Tok("changetargets"), "ChangeTheTargetOfAbility": c1Tok("changetargets"),
	"ChangeTheTargetOfSpellOrAbility":          c1Tok("changetargets"),
	"ChangeTheTargetOfSpellToPermanent":        c1Tok("changetargets"),
	"ChangeTheTargetOfSpellToAPermanent":       c1Tok("changetargets"),
	"ChangeTheTargetsOfSpellToAPlayer":         c1Tok("changetargets"),
	"ChangeATargetOfSpellOrAbilityToPermanent": c1Tok("changetargets"),
	"ChooseNewTargetsForSpell":                 c1Tok("changetargets"), "ChooseNewTargetsForSpellOrAbility": c1Tok("changetargets"),
	"ChooseNewTargetsForAnyNumberOfSpellsOrAbilities": c1Tok("changetargets"),

	// Casting a card is "play", as the existing Cast* cases.
	"CastASpellFromHandWithoutPaying": c1Tok("play"), "CastCopiedCardWithoutPaying": c1Tok("play"),
	"CastExiledCardWithoutPaying": c1Tok("play"), "CastSpellFromExileWithoutPaying": c1Tok("play"),
	"CastAnyNumberOfExiledCardsWithoutPaying": c1Tok("play"),
	"CastAnyNumberOfCopiedCardsWithoutPaying": c1Tok("play"),
	"CastGraveyardCardIntoExile":              c1Tok("play"), "CastCopiedCard": c1Tok("play"),
	"CastGraveyardCardWithoutPaying": c1Tok("play"), "CastASpellFromExile": c1Tok("play"),
	"CastGraveyardCard": c1Tok("play"), "CastTopCardOfLibraryWithoutPaying": c1Tok("play"),
	"CastAnExiledCardAndMaySpendManaAsThoughAnyTypeToCast":   c1Tok("play"),
	"CastCardInHandWithoutPaying":                            c1Tok("play"),
	"CastUptoNumberSpellsFromExileWithoutPaying":             c1Tok("play"),
	"CastACopiedCardWithoutPaying":                           c1Tok("play"),
	"CastASpellFromAPlayersGraveyardWithoutPayingIntoExile":  c1Tok("play"),
	"CastASpellFromMilledCardsWithoutPaying":                 c1Tok("play"),
	"CastASpellFromPlayersGraveyardWithoutPayingIntoExile":   c1Tok("play"),
	"CastAnyNumberOfGraveyardCardsWithoutPayingIntoExile":    c1Tok("play"),
	"CastAnyNumberOfGroupSpellsFromExileWithoutPaying":       c1Tok("play"),
	"CastAnyNumberOfSpellsFromHandWithoutPaying":             c1Tok("play"),
	"CastExiledCardWithoutPayingIntoExile":                   c1Tok("play"),
	"CastTopCardOfPlayersLibraryWithoutPaying":               c1Tok("play"),
	"CastASpellFromExileOntoBottomOfLibrary":                 c1Tok("play"),
	"CastASpellFromHandForAlternateCost":                     c1Tok("play"),
	"CastASpellFromHandGraveyardOrExileWithoutPaying":        c1Tok("play"),
	"CastASpellFromPlayersGraveyardWithoutPaying":            c1Tok("play"),
	"CastASpellFromRevealedCardInHandsWithoutPaying":         c1Tok("play"),
	"CastASpellFromTopOfLibraryWithoutPaying":                c1Tok("play"),
	"CastAnyNumberOfCardsInPlayersGraveyardWithoutPaying":    c1Tok("play"),
	"CastAnyNumberOfCopiedCards":                             c1Tok("play"),
	"CastAnyNumberOfSpellsFromAmongCopiedCardsWithoutPaying": c1Tok("play"),
	"CastAnyNumberOfSpellsFromOutsideTheGameWithoutPaying":   c1Tok("play"),
	"CastCardInHandWithoutPayingAsAFacedownCreatureSpell":    c1Tok("play"),
	"CastCommanderFromCommandZoneWithoutPaying":              c1Tok("play"),
	"CastCopiedCardForReducedCost":                           c1Tok("play"), "CastEachCopiedCardWithoutPaying": c1Tok("play"),
	"CastExiledCardAndMaySpendManaAsThoughAnyColorToCast": c1Tok("play"),
	"CastExiledCardWithoutPayingOntoBottomOfLibrary":      c1Tok("play"),
	"CastSpellFromExile": c1Tok("play"), "CastSpellFromGraveyardWithoutPayingIntoExile": c1Tok("play"),
	"CastSpellFromHandOrGraveyardAlternateCost":                                    c1Tok("play"),
	"CastSpellsFromExileWithoutPaying":                                             c1Tok("play"),
	"CastTheCardDiscardedThisWayWithoutPaying":                                     c1Tok("play"),
	"CastTheCardRevealedThisWayWithoutPaying":                                      c1Tok("play"),
	"CastTopCardOfLibraryForAlternateCost":                                         c1Tok("play"),
	"CastTopSpellOfLibraryWithoutPaying":                                           c1Tok("play"),
	"CastUptoNumberCopiedCardsWithoutPaying":                                       c1Tok("play"),
	"CastUptoNumberExiledCardsWithoutPaying":                                       c1Tok("play"),
	"CastUptoNumberGroupSpellsFromGraveyardOrHandWithoutPayingIntoExile":           c1Tok("play"),
	"CastUptoNumberSpellsFromHandWithoutPaying":                                    c1Tok("play"),
	"LookAtPlayersHandAndMayChooseACardToCastWithoutPaying":                        c1Tok("play"),
	"LookAtPlayersHandAndChooseACardForPlayerToPlayControllingThemToDoSo":          c1Tok("play"),
	"ForEachPlayerMayCastASpellFromAmongCardsThatPlayerMilledThisWayWithoutPaying": c1Tok("play"),

	// Control.
	"GainControlOfEachPermanent": c1Tok("gaincontrol"), "GainControlOfEachPermanentUntil": c1Tok("gaincontrol"),
	"GainControlOfAPermanentUntil":                           c1Tok("gaincontrol"),
	"GainControlOfAPermanentControlledByEachPlayer":          c1Tok("gaincontrol"),
	"GainControlOfSpellAndMayChooseNewTargets":               c1Tok("controlspell", "changetargets"),
	"GainControlOfSpellAndRandomlyChooseNewTargetsExceptFor": c1Tok("controlspell", "changetargets"),
	"GainControlOfPlayerDuringTheirNextTurn":                 c1Tok("controlplayer"),
	"GainControlOfPlayerDuringTheirNextCombatStep":           c1Tok("controlplayer"),
	"ExchangeControl":                               c1Tok("exchangecontrol"),
	"ExchangeControlOfEachPermanentWithPlayer":      c1Tok("exchangecontrol"),
	"ExchangeControlOfEachPermanentWithPlayerUntil": c1Tok("exchangecontrol"),
	"ExchangeControlOfSpellAndPermanent":            c1Tok("exchangecontrol"),

	// Other exchanges (Forge ExchangeLife/ExchangeLifeVariant, ExchangePower,
	// ExchangeTextBox, GainOwnership, ExchangeZone).
	"ExchangeLifeTotalWithPlayer":                 c1Tok("exchangelife"),
	"ExchangeLifeTotalWithPermanentsToughness":    c1Tok("exchangelife"),
	"ExchangeLifeTotalWithPermanentsPower":        c1Tok("exchangelife"),
	"ExchangePowerOfTwoCreaturesUntil":            c1Tok("exchangepower"),
	"ExchangeTextBoxesOfTwoPermanentsUntil":       c1Tok("exchangetextbox"),
	"ExchangeOwnershipOfTwoCards":                 c1Tok("gainownership"),
	"ExchangeAnteCardWithTopCardOfPlayersLibrary": c1Tok("exchangezone"),

	// Attaching.
	"AttachPermanentToAPermanent": c1Tok("attach"), "AttachEachPermanentToPermanent": c1Tok("attach"),
	"AttachAnyNumberOfPermanentsToPermanent": c1Tok("attach"), "AttachAPermanentToPermanent": c1Tok("attach"),
	"AttachAPermanentToAPermanent": c1Tok("attach"), "AttachEachPermanentToAPermanent": c1Tok("attach"),
	"AttachPermanentToPlayer": c1Tok("attach"), "AttachAnyNumberOfPermanentsToAnyPermanents": c1Tok("attach"),
	"AttachAnyNumberOfPermanentsToPlayerOrPermanent": c1Tok("attach"),
	"AttachEachPermanentToAPlayerOfChoice":           c1Tok("attach"),
	"AttachPermanentToACardInAPlayersGraveyard":      c1Tok("attach"),
	"AttachPermanentsToPermanent":                    c1Tok("attach"), "AttachUptoOnePermanentToEachPermanent": c1Tok("attach"),

	// Destroy and sacrifice.
	"DestroyAPermanentAtRandom": c1Tok("destroy"), "DestroyAPermanentNoRegen": c1Tok("destroy"),
	"DestroyNumberPermanents": c1Tok("destroy"), "DestroyUptoNumberPermanents": c1Tok("destroy"),
	"DestroyEachPermanentNoRegenSubset":         c1Tok("destroy"),
	"DestroyUptoOnePermanentEachPlayerControls": c1Tok("destroy"),
	"ControllersSacrificeEachPermanent":         c1Tok("sacrifice"),

	// Doubling power and/or toughness is a pump by a computed amount.
	"DoubleCreaturesPowerAndToughnessUntilEndOfTurn":     c1Tok("pump:*"),
	"DoubleEachCreaturesPowerAndToughnessUntilEndOfTurn": c1Tok("pump:*"),
	"DoubleEachCreaturesPowerUntilEndOfTurn":             c1Tok("pump:*"),
	"DoubleCreaturesPowerNumberTimesUntilEndOfTurn":      c1Tok("pump:*"),
	"DoubleEachCreaturesToughnessUntilEndOfTurn":         c1Tok("pump:*"),
	"DoubleXValueOfSpell":                                c1Tok("changex"),

	// Keyword actions, one token each (the lowercased Forge API).
	"CreatureConnives": c1Tok("connive"), "CreatureConnivesNumber": c1Tok("connive"),
	"EachCreatureConnives": c1Tok("connive"),
	"GoadCreature":         c1Tok("goad"), "GoadEachCreature": c1Tok("goad"),
	"GoadEachCreatureUntil": c1Tok("goad"), "GoadCreatureUntil": c1Tok("goad"),
	"ExploreWithPermanent": c1Tok("explore"), "ExploreWithEachPermanent": c1Tok("explore"),
	"ExploreWithPermanentNumberTimes": c1Tok("explore"),
	"Earthbend":                       c1Tok("earthbend"),
	"DraftACardFromSpellBook":         c1Tok("draft"), "DraftACardFromSpellBookNumberTimes": c1Tok("draft"),
	"EmpowerJace": c1Tok("empower"),
	"Discover":    c1Tok("discover"),
	"Incubate":    c1Tok("incubate"), "IncubateNumberTimes": c1Tok("incubate"),
	"Learn":               c1Tok("learn"),
	"InvestigateTimes":    c1Tok("investigate"),
	"AbandonScheme":       c1Tok("abandon"),
	"EndureWithPermanent": c1Tok("endure"),
	"AirbendPermanent":    c1Tok("airbend"), "AirbendPermanents": c1Tok("airbend"), "AirbendSpell": c1Tok("airbend"),
	"HeistPlayersLibrary": c1Tok("heist"),
	"DetainPermanent":     c1Tok("detain"), "DetainEachPermanent": c1Tok("detain"),
	"IntensifyCardsOwnedByPlayer": c1Tok("intensify"), "IntensifyPermanent": c1Tok("intensify"),
	"CloakTheTopCardOfPlayersLibrary": c1Tok("cloak"), "CloakCardFromHand": c1Tok("cloak"),
	"CloakEachExiledCard": c1Tok("cloak"), "CloakACardFromHand": c1Tok("cloak"),
	"Blight":    c1Tok("blight"),
	"BecomeDay": c1Tok("daytime"), "BecomeNight": c1Tok("daytime"),
	"ChaosEnsues":     c1Tok("chaosensues"),
	"LoseUnspentMana": c1Tok("drainmana"),
	"ActivateAManaAbilityOfEachPermanentAndLoseUnspentMana": c1Tok("activateability", "drainmana"),
	"DrawTheGame":                                c1Tok("gamedrawn"),
	"EndTheCombatPhase":                          c1Tok("endcombatphase"),
	"LockOrUnlockADoorOfPermanent":               c1Tok("unlockdoor"),
	"HarnessPermanent":                           c1Tok("alterattribute"),
	"ChooseAnAttackingCreatureForBlockerToBlock": c1Tok("block"),
	"DistrbuteUptoNumberArtStickersAmongAnyNumberOfPermanents": c1Tok("putsticker"),
	"FlipPermanent":                    c1Tok("setstate:flip"),
	"ConvertPermanent":                 c1Tok("transform"),
	"Adapt":                            c1Num("counters:+1/+1", 0),
	"Bolster":                          c1Num("counters:+1/+1", 0),
	"GetNumberTickets":                 c1Num("counters:ticket", 0),
	"Clash":                            c1Alts("clash", func(n node) []any { return []any{argN(n, 1), argN(n, 2)} }),
	"FaceAVillianousChoice":            c1Alts("villainouschoice", func(n node) []any { return list(rawArgs(n)) }),
	"EachPlayerMakesAVillainousChoice": c1Alts("villainouschoice", func(n node) []any { return list(argN(n, 1)) }),

	// Mana.
	"AddCombinationManaWithModifers": c1Tok("mana"), "AddManaRepeatedTwiceWithModifiers": c1Tok("mana"),
	"DoubleEachTypeOfUnspentMana": c1Tok("mana"),

	// Extra combat phases.
	"AfterThisMainPhaseThereAreAnAdditionalNumberCombatPhases":                                                               c1Tok("addphase"),
	"AfterTheSecondMainPhaseThisTurnThereIsAnAdditionalCombatPhaseAndAnAdditionalMainPhaseWithAtTheBeginningOfCombatTrigger": c1Tok("addphase"),

	// Tokens and conjured cards.
	"CreateTokensForEachPlayer":                                             c1Tokens(1),
	"CreateTokensForEachPermanent":                                          c1Tokens(1),
	"EachPlayerMayCreateTokens":                                             c1Tokens(1),
	"EachPlayerCreatesTokensOfTheirChoice":                                  c1Tok("token:*"),
	"ForEachPermanentConjureCardOntoBattlefield":                            c1Tok("conjure"),
	"ForEachValueInRangeConjureDuplicateOfARandomCardOfTypeOntoBattlefield": c1Tok("conjure"),
	"CreateCopiesOfRandomCardsWithManaCosts":                                c1Tok("conjure"),

	// Library searches, mapped by where the found cards go.
	"DigitallySearchLibrary":     func(e *effects, n node) { e.add(searchToken(rawArgs(n))) },
	"ForEachPlayerSearchLibrary": func(e *effects, n node) { e.add(searchToken(argN(n, 1))) },

	// Delayed and created triggers, as CreateFutureTrigger/CreateTriggerUntil.
	"CreateFutureTriggerI": c1Tok("delayed"), "CreateTriggerUntilI": c1Tok("delayed"),
	"CreateTrigger": c1Tok("delayed"), "CreateTriggerOnce": c1Tok("delayed"),
	"CreateFutureTrigger_UnlessPlayerPaysManaBefore": c1Tok("delayed"),
	"GetOneTimeBoon": c1Tok("delayed"), "GetOneTimeConditionalBoon": c1Tok("delayed"),
	"GetANumberTimeBoon": c1Tok("delayed"),
	"DuringNextUntap":    c1Tok("delayed"),

	// Replacement effects an effect creates, as the existing
	// CreateReplaceWouldPutIntoGraveyardUntil: "effect".
	"CreateFutureReplaceWouldDealDamage": c1Tok("effect"), "CreateReplaceWouldDealDamageUntil": c1Tok("effect"),
	"CreateFutureReplaceWouldDraw": c1Tok("effect"), "CreateReplaceWouldEnterUntil": c1Tok("effect"),
	"CreateReplaceWouldProduceManaUntil":          c1Tok("effect"),
	"CreateReplaceWouldLeaveTheBattlefieldUntil":  c1Tok("effect"),
	"CreateFutureReplaceWouldLeaveTheBattlefield": c1Tok("effect"),
	"CreateFutureReplaceWouldEnter":               c1Tok("effect"), "CreateReplaceWouldPutCountersUntil": c1Tok("effect"),
	"CreateFutureReplaceWouldAdapt": c1Tok("effect"), "CreateFutureReplaceWouldDestroy": c1Tok("effect"),
	"CreateFutureReplaceWouldLoseTheGame": c1Tok("effect"), "CreateFutureReplaceWouldRollDice": c1Tok("effect"),
	"CreateFutureReplaceWouldSetASchemeInMotion":                 c1Tok("effect"),
	"CreateReplaceAnyNumberOfTokensWouldBeCreatedUntil":          c1Tok("effect"),
	"CreateReplaceTokensWouldBeCreatedUnderAPlayersControlUntil": c1Tok("effect"),
	"CreateReplaceWouldDrawUntil":                                c1Tok("effect"), "CreateReplaceWouldGainLifeUntil": c1Tok("effect"),
	"CreateReplaceWouldMaskUntil": c1Tok("effect"), "CreateReplaceWouldPlaneswalkUntil": c1Tok("effect"),

	// Continuous effects an effect creates, mapped like the printed statics
	// (the gorge side maps an Effect's statics the same way).
	"CreatePlayerEffect":                                 c1PlayerEffects(1),
	"CreateEachPlayerEffectUntil":                        c1PlayerEffects(1),
	"CreateFuturePlayerEffect":                           func(e *effects, n node) { e.add(futurePlayerEffectToken(argN(n, 1))) },
	"EachPlayerCantCastSpellsDuringTheirNextTurn":        c1Tok("player:cantcastspells"),
	"CreateEachPermanentRuleEffect":                      c1Rules(1),
	"CreatePermanentRuleEffect":                          c1Rules(1),
	"EachPermanentDoesntUntapDuringControllersNextUntap": c1Tok("rule:doesntuntapduringcontrollersuntap"),
	"CreatureMustAttackDuringControllersNextCombatPhase": c1Tok("rule:mustattack"),
	"CreateGameEffect":                                   func(e *effects, n node) { e.add(gameEffectToken(argN(n, 1))) },
	"CreateFutureSpellEffect":                            c1SpellEffects(1),
	"CreateEachSpellEffect":                              c1SpellEffects(1),
	"CreateLimitedSpellEffect":                           c1SpellEffects(2),
	"CreatePermanentSpellLayerEffect":                    c1Layers(2),
	"CreateSpellOrPermanentEffect":                       c1Layers(2),
	"CreateEachCardInPlayersGraveyardEffectUntil":        c1Layers(2),
	"CreateCardInHandEffectUntil":                        c1Layers(1),
	"CreateExiledCardEffect":                             func(e *effects, n node) { e.add(exiledCardEffects(list(argN(n, 1)))...) },
	"CreateEachExiledCardEffect":                         func(e *effects, n node) { e.add(exiledCardEffects(list(argN(n, 1)))...) },
	"CreateGroupExileEffect": func(e *effects, n node) {
		for _, x := range list(argN(n, 2)) {
			if variantOf(x) == "OneMayBePlayedBy" {
				e.add("mayplay")
			} else {
				e.add(unmapped("GroupExiledEffect." + variantOf(x)))
			}
		}
	},
}

// gorgeAltRefs names the SVars an SA runs as alternatives (exactly one
// runs): a clash's won and lost branches, a villainous choice's two
// choices. They mirror mtgish's Clash and FaceAVillianousChoice branches.
func gorgeAltRefs(a *cards.SA) []string {
	switch a.API {
	case "Clash":
		return []string{a.Params["WinSubAbility"], a.Params["OtherwiseSubAbility"]}
	case "VillainousChoice":
		return cards.SplitModeNames(a.Params["Choices"])
	}
	return nil
}

// effectCleanupTrigger reports whether the named trigger of an Effect only
// exiles the effect itself from the command zone (Forge's bookkeeping for
// "until <event>": Execute$ ChangeZone | Defined$ Self | Origin$ Command |
// Destination$ Exile). It ends the effect; it is not a delayed trigger of
// the card.
func effectCleanupTrigger(svars map[string]string, name string) bool {
	body, ok := svars[strings.TrimSpace(name)]
	if !ok {
		return false
	}
	t, ok := cards.ParseTriggerLine(body)
	if !ok {
		return false
	}
	sa := cards.ResolveSVar(svars, t.Params["Execute"])
	if sa == nil || sa.Sub != nil && sa.Sub.API != "Cleanup" {
		return false
	}
	return effectSelfExile(sa)
}

// effectSelfExile reports whether sa is an Effect exiling itself from the
// command zone (Defined$ Self, or no Defined$, which means the same).
func effectSelfExile(sa *cards.SA) bool {
	p := sa.Params
	d := p["Defined"]
	// Imprinted/OriginalHost: an effect the host imprinted, exiling itself.
	return sa.API == "ChangeZone" && (d == "" || d == "Self" || d == "Imprinted" || d == "OriginalHost") &&
		p["Origin"] == "Command" && p["Destination"] == "Exile"
}

// atEOTTokens maps a script's AtEOT$ (what happens to the object at the
// next end step): a delayed trigger plus its action, as mtgish writes the
// "at the beginning of the next end step, exile/sacrifice it" sentence.
func atEOTTokens(v string) []string {
	switch {
	case v == "":
		return nil
	case strings.Contains(v, "Exile"):
		return []string{"delayed", "move:battlefield>exile"}
	case strings.Contains(v, "Sacrifice"):
		return []string{"delayed", "sacrifice"}
	case strings.Contains(v, "Destroy"):
		return []string{"delayed", "destroy"}
	case strings.Contains(v, "Hand"):
		return []string{"delayed", "move:battlefield>hand"}
	case strings.Contains(v, "Library"):
		return []string{"delayed", "move:battlefield>library"}
	}
	return []string{"delayed"}
}
