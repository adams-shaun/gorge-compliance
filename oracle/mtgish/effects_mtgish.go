package mtgish

import (
	"strconv"
	"strings"
)

// effects collects one ability's target slots and effect verbs.
type effects struct {
	targets []string
	effects []string
}

func (e *effects) add(tok ...string) { e.effects = append(e.effects, tok...) }

// descendEnums are the enums an action tree nests further actions under;
// every other enum (filters, numbers, costs, conditions, granted rules,
// token definitions) is a leaf as far as effects are concerned.
var descendEnums = map[string]bool{
	"_Action": true, "_Actions": true, "_PawMode": true, "_SpreeAction": true,
	"_TieredAction": true, "_AdditionalCostOption": true,
}

// walk summarises an Actions/Action subtree.
func (e *effects) walk(x any) {
	walk(x, func(n node, enum, variant string) bool {
		switch enum {
		case "_LookAtTopOfLibraryAction", "_RevealTheTopNumberCardsOfLibraryAction":
			if t := lookMove(variant); t != "" {
				e.add(t)
			} else if descend, ok := e.p2cLookAction(n, variant); ok {
				return descend
			}
			return variant == "If" || variant == "Unless"
		case "_Target", "_DistributedTarget":
			if strings.HasSuffix(variant, "TargetPlayerControls") {
				// "Up to five target permanents target player controls":
				// the player is a target too.
				e.targets = append(e.targets, clsPlayer)
			}
			e.targets = append(e.targets, mtgishTarget(n))
			return false
		case "_Action":
			if variant == "ChooseAnAction" {
				// "You may A or B. When you do, ...": one of the actions
				// runs, so they are alternatives.
				for _, a := range argsOf(n) {
					if l := list(a); l != nil {
						e.alternatives(l...)
						return false
					}
				}
			}
			if variant == "IfElse" {
				a := argsOf(n)
				if len(a) == 3 {
					e.alternatives(a[1], a[2])
					return false
				}
			}
			e.action(n, variant)
			return !leafActions[variant]
		case "_Actions":
			switch variant {
			case "Targeted_Modal":
				// "Target spell or permanent": one slot (map_f.go).
				e.targetedModal(list(rawArgs(n)))
				return false
			case "TargetedDistributed":
				e.targetedDistributed(n)
				return false
			case "AdditionalCost_Modal":
				// Paid or not: alternatives, as Modal_IfElse.
				e.alternatives(list(rawArgs(n))...)
				return false
			}
			if variant == "Modal_IfElse" {
				a := argsOf(n)
				if len(a) == 3 {
					e.alternatives(a[1], a[2])
					return false
				}
			}
			if strings.HasPrefix(variant, "Modal_") || variant == "AdditionalCost_Modal" {
				// Modes are alternatives (scripts' Charm choices, merged
				// the same way). The mode list is the args itself for a
				// one-field variant, else the one list-valued argument.
				if l := list(rawArgs(n)); l != nil && enumOf(l[0]) == "Actions" {
					e.alternatives(l...)
					return false
				}
				for _, a := range argsOf(n) {
					if l := list(a); l != nil {
						e.alternatives(l...)
						return false
					}
				}
			}
		}
		if enum == "_Cost" && strings.HasPrefix(variant, "Reflexive_") {
			// "Sacrifice it unless you discard a card. When you discard a
			// card this way, ...": the reflexive trigger's actions ride on
			// the cost node.
			return true
		}
		return descendEnums[enum]
	})
}

// alternatives summarises branches of which exactly one runs (kicked or
// not, gift promised or not, if/else): each target and verb counts as
// often as the branch with the most of it, so the same target written in
// both branches is one target, as scripts write it once.
func (e *effects) alternatives(branches ...any) {
	var subs []*effects
	for _, b := range branches {
		sub := &effects{}
		sub.walk(b)
		subs = append(subs, sub)
	}
	e.merge(subs)
}

// merge folds alternative branches' summaries into e (see alternatives).
func (e *effects) merge(subs []*effects) {
	maxCount := func(lists [][]string) ([]string, map[string]int) {
		max := map[string]int{}
		var order []string
		for _, l := range lists {
			c := map[string]int{}
			for _, t := range l {
				c[t]++
			}
			for _, t := range l {
				if c[t] > max[t] {
					if max[t] == 0 {
						order = append(order, t)
					}
					max[t] = c[t]
				}
			}
		}
		return order, max
	}
	var tl, el [][]string
	for _, s := range subs {
		tl = append(tl, expandExactCounts(s.targets))
		el = append(el, s.effects)
	}
	torder, tmax := maxCount(tl)
	for _, t := range torder {
		for i := 0; i < tmax[t]; i++ {
			e.targets = append(e.targets, t)
		}
	}
	// A verb whose literal amount differs between branches ("deals 2
	// damage, or 4 if kicked") is that verb with a non-literal amount, as
	// often as the branch with the most of it has it.
	numeric := func(t string) (string, bool) {
		pre, last := splitLast(t)
		if pre == "" {
			return "", false
		}
		if _, err := strconv.Atoi(last); err != nil && last != "*" && !ptModifier(last) {
			return "", false
		}
		return pre, true
	}
	amounts := map[string]map[string]bool{}
	perBranch := map[string]int{}
	for _, l := range el {
		c := map[string]int{}
		for _, t := range l {
			if pre, ok := numeric(t); ok {
				if amounts[pre] == nil {
					amounts[pre] = map[string]bool{}
				}
				_, last := splitLast(t)
				amounts[pre][last] = true
				c[pre]++
			}
		}
		for pre, n := range c {
			if n > perBranch[pre] {
				perBranch[pre] = n
			}
		}
	}
	eorder, emax := maxCount(el)
	done := map[string]bool{}
	for _, t := range eorder {
		if pre, ok := numeric(t); ok && len(amounts[pre]) > 1 {
			if !done[pre] {
				done[pre] = true
				for i := 0; i < perBranch[pre]; i++ {
					e.effects = append(e.effects, pre+":*")
				}
			}
			continue
		}
		for i := 0; i < emax[t]; i++ {
			e.effects = append(e.effects, t)
		}
	}
}

// ptModifier reports whether s is a "+1/-2" pump amount.
func ptModifier(s string) bool {
	p, t, ok := strings.Cut(s, "/")
	if !ok {
		return false
	}
	_, e1 := strconv.Atoi(p)
	_, e2 := strconv.Atoi(t)
	return e1 == nil && e2 == nil
}

// leafActions are mapped actions whose arguments must not be searched for
// nested actions (their payload is a token definition or granted rules).
var leafActions = map[string]bool{
	"CreateTokens": true, "GetAnEmblem": true, "CreatePermanentLayerEffectUntil": true,
	"CreateEachPermanentLayerEffectUntil": true, "CreatePermanentLayerEffect": true,
	"CreateEachPermanentLayerEffect": true,
}

// wrapperActions only carry other actions (conditions, choices, repeats);
// they contribute no verb of their own.
var wrapperActions = map[string]bool{
	"If": true, "Unless": true, "IfElse": true, "MayAction": true, "MayActions": true,
	"PlayerAction": true, "EachPlayerAction": true, "ActionNumberTimes": true,
	"ActionForEachPlayer": true, "MayCost": true, "MustCost": true, "PlayerMayCost": true,
	"AnyPlayerMayCost": true, "EachPlayerMayCostOrFallback": true,
	"ChooseAPermanent": true, "ChooseAnExiledCard": true, "ChooseAColor": true,
	"ChooseACardtypeFromList": true, "ChooseAnAbility": true,
	"ChooseAPermanentOfEachPermanentTypeAvailable": true, "CreateValueX": true,
	"PlayerActions": true, "PlayerMayAction": true, "EachPlayerMayAction": true,
	"ChooseADamageSource": true, "ChooseACreatureType": true, "ChooseAnAction": true,
	"RevealHand": true,
}

func (e *effects) action(n node, v string) {
	a := argsOf(n)
	arg := func(i int) any {
		if i < len(a) {
			return a[i]
		}
		return nil
	}
	switch {
	case (v == "MayCost" || v == "MustCost") && variantOf(arg(0)) == "Forage":
		// Foraging is a keyword action whichever way it is written.
		e.add("forage")
		return
	case (v == "MayCost" || v == "MustCost") && strings.HasPrefix(variantOf(arg(0)), "Cast"):
		// "You may cast that card" written as a cost the player may pay:
		// casting is the effect, as the Cast* actions.
		e.add("play")
		return
	case v == "MustCost" || v == "MayCost":
		// "Sacrifice two Swamps. If you can't, ...": a cost the player
		// must pay is done, as EachPlayerMustCost and PlayerMustCost
		// (a mana payment stays nothing, costAsAction).
		e.p2cMustCost(arg(0))
		return
	case wrapperActions[v]:
		return
	case strings.HasPrefix(v, "Reflexive_"):
		// "Reflexive_<Cost>_WhenYouDo": the cost is paid, then a reflexive
		// trigger runs its actions (walked as children).
		// The <Cost> half is paid, not done (a mana payment); only an
		// action-shaped half counts. Foraging is a keyword action either
		// way (see MayCost below).
		if strings.Contains(v, "Forage") {
			e.add("forage")
		}
		if strings.Contains(v, "Mill") {
			e.add("mill:*")
		}
		if strings.Contains(v, "CreateTokens") {
			e.add("token:*")
		}
		if strings.Contains(v, "PutCounters") {
			e.add("counters:*")
		}
		if t, ok := reflexiveB[strings.TrimSuffix(strings.TrimPrefix(v, "Reflexive_"), "_WhenYouDo")]; ok {
			e.add(t)
		}
		e.p2cReflexive(v, a)
		return
	}
	switch v {
	case "DrawACard":
		e.add("draw:1")
	case "DrawNumberCards":
		e.add("draw:" + gameNumber(arg(0)))
	case "GainLife":
		e.add("gainlife:" + gameNumber(arg(0)))
	case "GainLifeForEach":
		e.add("gainlife:*")
	case "LoseLife":
		e.add("loselife:" + gameNumber(arg(0)))
	case "PermanentDealsDamage":
		e.add("damage:" + gameNumber(arg(1)))
	case "SpellDealsDamage":
		e.add("damage:" + gameNumber(arg(1)))
	case "DeadPermanentDealsDamage":
		e.add("damage:" + gameNumber(arg(0)))
	case "SpellDealsMultipleDamage", "PermanentDealsMultipleDamage":
		for _, d := range list(arg(1)) {
			e.add("damage:" + gameNumber(argN(d, 0)))
		}
	case "PermanentDealsDistributedDamage", "SpellDealsDistributedDamage":
		e.add("damage:*")
	case "DestroyPermanent", "DestroyEachPermanent":
		e.add("destroy")
	case "Exile":
		for _, x := range list(arg(0)) {
			e.add("move:" + exilableZone(x) + ">exile")
		}
	case "PutPermanentIntoItsOwnersHand", "PutEachPermanentIntoItsOwnersHand":
		e.add("move:battlefield>hand")
	case "ReturnPermanentToTopOrBottomOfLibrary", "PutPermanentOnTopOfLibrary", "PutPermanentOnBottomOfLibrary":
		e.add("move:battlefield>library")
	case "PutGraveyardCardIntoHand", "PutEachGraveyardCardIntoHand", "PutACardFromGraveyardIntoHand",
		"PutACardFromGraveyardIntoHandExceptForGraveyardCard", "ReturnEnchantingGraveyardCardToHand":
		e.add("move:graveyard>hand")
	case "PutGraveyardCardOntoBattlefield", "PutEachGraveyardCardOntoBattlefield", "ReturnDeadGraveyardCardToBattlefield",
		"ReturnEachCardFromPlayersGraveyardToBattlefield", "ReturnACardFromGraveyardToBattlefield",
		"PutACardFromAGraveyardOnBattlefield", "PutEachCardFromEachPlayersGraveyardOntoTheBattlefield":
		e.add("move:graveyard>battlefield")
	case "PutGraveyardCardOnBottomOfLibrary", "PutGraveyardCardOnTopOfLibrary", "ShuffleGraveyardIntoLibrary":
		e.add("move:graveyard>library")
	case "PutExiledCardOntoBattlefield":
		e.add("move:exile>battlefield")
	case "PutExiledCardIntoOwnersHand", "PutExiledCardsIntoOwnersHand":
		e.add("move:exile>hand")
	case "PutTopOfLibraryInHand":
		e.add("move:library>hand")
	case "CreatePermanentsList":
		// Names a set of permanents for the following actions.
	case "PutACardFromHandOrGraveyardOnBattlefield":
		e.add("move:hand>battlefield")
	case "SearchLibrary":
		e.add(searchToken(arg(0)))
	case "CounterSpell":
		e.add("counter")
	case "Scry":
		e.add("scry:" + gameNumber(arg(0)))
	case "Surveil":
		e.add("surveil:" + gameNumber(arg(0)))
	case "MillNumberCards":
		e.add("mill:" + gameNumber(arg(0)))
	case "DiscardACard":
		e.add("discard:1")
	case "DiscardNumberCards":
		e.add("discard:" + gameNumber(arg(0)))
	case "DiscardHand", "RevealHandAndPlayerChoosesACardToDiscard":
		e.add("discard:*")
	case "RevealHandAndPlayerChoosesACardToExile":
		e.add("move:hand>exile")
	case "TapPermanent", "TapEachPermanent":
		e.add("tap")
	case "UntapPermanent", "UntapEachPermanent", "UntapUptoNumberPermanents":
		e.add("untap")
	case "SacrificePermanent", "SacrificeAPermanent", "SacrificeNumberPermanents", "SacrificeEachPermanent":
		e.add("sacrifice")
	case "Fight":
		e.add("fight")
	case "AddMana", "AddManaWithModifiers", "AddManaRepeated", "AddManaRepeatedWithModifiers", "AddCombinationMana":
		e.add("mana")
	case "CreateTokens":
		for _, t := range list(arg(0)) {
			e.add(tokenToken(t))
		}
	case "PutCounters":
		for _, pc := range list(rawArgs(n)) {
			e.add(putCounters(pc))
		}
	case "CreatePermanentLayerEffectUntil", "CreatePermanentLayerEffect":
		e.add(layerEffects(list(arg(1)))...)
	case "CreateEachPermanentLayerEffectUntil", "CreateEachPermanentLayerEffect":
		e.add(layerEffects(list(arg(1)))...)
	case "CreatePermanentRuleEffectUntil", "CreateEachPermanentRuleEffectUntil":
		for _, r := range list(arg(1)) {
			e.add(p2cGoadAction(ruleToken(variantOf(r))))
		}
	case "CreatePlayerEffectUntil":
		for _, x := range list(arg(1)) {
			e.add(playerEffectToken(variantOf(x)))
		}
	case "GetAnEmblem":
		// An emblem's abilities, summarised like printed statics.
		m := &mtgishNorm{s: &Summary{}}
		m.rules(list(rawArgs(n)))
		for _, ab := range m.s.Abilities {
			if ab.Kind == "triggered" {
				e.add("delayed")
			}
			e.add(ab.Effects...)
		}
		for _, k := range m.s.Keywords {
			e.add("grant:" + keywordHeadToken(k))
		}
	case "CreateGraveyardCardEffectUntil":
		e.add(layerEffects(list(arg(1)))...)
	case "CreatePreventDamageUntil":
		e.add("prevent")
	case "CreateReplaceWouldPutIntoGraveyardUntil":
		e.add("effect")
	case "CreateFutureTrigger", "CreateTriggerUntil":
		e.add("delayed")
	case "LookAtTheTopNumberCardsOfLibrary", "RevealTheTopNumberCardsOfLibrary":
		e.add("dig:" + gameNumber(arg(0)))
	case "LookAtTopOfLibrary", "RevealTopCardOfLibrary":
		e.add("dig:1")
	case "RevealCardsFromTheTopOfLibraryUntilACardOfTypeIsRevealed":
		e.add("diguntil")
	case "CopySpellAndMayChooseNewTargets", "CopySpellForEachAndMayChooseNewTargets":
		e.add("copyspell")
	case "GainControlOfPermanent", "GainControlOfPermanentUntil", "APlayerGainsControlOfPermanent":
		e.add("gaincontrol")
	case "AttachPermanentToPermanent":
		e.add("attach")
	case "Forage":
		e.add("forage")
	case "RegeneratePermanent", "RegenerateEachPermanent":
		e.add("regenerate")
	case "TransformPermanent", "TransformEachPermanent":
		e.add("transform")
	case "RemoveCounters":
		e.add("countersremove")
	case "ReturnGraveyardCardToHand":
		e.add("move:graveyard>hand")
	case "PutACardFromHandOnBattlefield":
		e.add("move:hand>battlefield")
	case "GetEnergy":
		e.add("counters:energy:" + gameNumber(arg(0)))
	case "Investigate":
		e.add("investigate")
	case "SeekACard", "SeekNumberCards":
		e.add("seek")
	case "Proliferate":
		e.add("proliferate")
	case "DestroyPermanentNoRegen", "DestroyEachPermanentNoRegen":
		e.add("destroy")
	case "MillACard":
		e.add("mill:1")
	case "PutAPermanentIntoItsOwnersHand":
		e.add("move:battlefield>hand")
	case "PermanentDoesntUntapDuringControllersNextUntap":
		e.add("rule:doesntuntapduringcontrollersuntap")
	case "CreateFuturePreventDamage":
		e.add("prevent")
	case "Amass":
		e.add("amass")
	case "Goad", "GoadPermanent", "GoadEachPermanent":
		e.add("goad")
	case "BecomeTheMonarch":
		e.add("monarch")
	case "EachPlayerCreatesTokens":
		e.p2cEachPlayerTokens(arg(1))
	case "PutEachExiledCardOntoTheBattlefield":
		e.add("move:exile>battlefield")
	case "PutPermanentOnTopOfOwnersLibrary":
		e.add("move:battlefield>library")
	case "SearchLibraryAndOrGraveyard":
		e.add("search:*")
	case "TakeAnExtraTurn":
		e.add("extraturn")
	case "TemptWithRing":
		e.add("tempt")
	case "TapOrUntapPermanent":
		e.add("tapuntap")
	case "Connive", "PermanentConnives":
		e.add("connive")
	case "CreateSpellEffect":
		e.add("effect")
	case "ChangeTargetsOfSpellOrAbility":
		e.add("changetargets")
	case "CastGraveyardCardWithoutPayingIntoExile", "CastASpellFromExileWithoutPaying",
		"CastAnyNumberOfSpellsFromExileWithoutPaying", "CastExiledCardForAlternateCost":
		e.add("play")
	case "ThereIsAnAdditionalCombatPhase":
		e.add("addphase")
	case "EndTheTurn":
		e.add("endturn")
	case "WinTheGame":
		e.add("wingame")
	case "LoseTheGame":
		e.add("losegame")
	case "DoubleCreaturesPowerUntilEndOfTurn":
		e.add("pump:*")
	case "PutACardOfTypeMilledThisWayIntoHand":
		e.add("move:graveyard>hand")
	case "BeginGameWithCardOnBattlefield":
		e.add("move:hand>battlefield")
	default:
		switch {
		case strings.HasPrefix(v, "Conjure"):
			e.add("conjure")
		case strings.HasPrefix(v, "Roll"):
			e.add("dice")
		case strings.HasPrefix(v, "Flip") && strings.Contains(v, "Coin"):
			e.add("coin")
		case strings.HasPrefix(v, "CreatePerpetual"):
			e.add("perpetual")
		case strings.HasPrefix(v, "PhaseOut"), strings.HasPrefix(v, "Phase") && strings.Contains(v, "Out"):
			e.add("phaseout")
		default:
			if f, ok := extAction[v]; ok {
				f(e, n)
			} else {
				e.add(unmapped("Action." + v))
			}
		}
	}
}

// lookMove maps a look-at/reveal-the-top sub-action to the move it makes.
func lookMove(v string) string {
	switch {
	case strings.Contains(v, "Cloak"):
		return "cloak"
	case strings.Contains(v, "Manifest"):
		return "manifest"
	case strings.Contains(v, "IntoHand"), strings.Contains(v, "InHand"):
		return "move:library>hand"
	case strings.Contains(v, "OntoTheBattlefield"), strings.Contains(v, "OntoBattlefield"):
		return "move:library>battlefield"
	case strings.Contains(v, "IntoGraveyard"):
		return "move:library>graveyard"
	case strings.Contains(v, "Exile"):
		return "move:library>exile"
	case strings.Contains(v, "OfLibrary"):
		return "move:library>library"
	}
	return ""
}

// exilableZone is the zone an Exilable is exiled from.
func exilableZone(x any) string {
	v := variantOf(x)
	switch {
	case strings.Contains(v, "Graveyard"):
		return "graveyard"
	case strings.Contains(v, "Librar"):
		return "library"
	case strings.Contains(v, "Hand"):
		return "hand"
	case strings.Contains(v, "Spell"):
		return "stack"
	case strings.Contains(v, "Permanent"):
		return "battlefield"
	}
	return unmapped("Exilable." + v)
}

// searchToken maps a library search by where the found cards go.
func searchToken(steps any) string {
	dest := ""
	for _, s := range list(steps) {
		switch variantOf(s) {
		case "PutFoundCardsIntoHand":
			dest = "hand"
		case "PutFoundCardsOntoBattlefield":
			dest = "battlefield"
		case "PutFoundCardsIntoGraveyard":
			dest = "graveyard"
		case "ExileFoundCards":
			dest = "exile"
		case "PutSetAsideCardsOnTopOfLibrary":
			dest = "library"
		}
	}
	if dest == "" {
		return "search:*"
	}
	return "search:" + dest
}

// tokenToken maps one CreatableToken: "token:<count>", or "tokencopy:<count>"
// for a copy.
func tokenToken(t any) string {
	count := "1"
	for variantOf(t) == "NumberTokens" || variantOf(t) == "NumberTokensForEach" {
		if variantOf(t) == "NumberTokens" {
			count = gameNumber(argN(t, 0))
			t = argN(t, 1)
		} else {
			count = "*"
			t = argN(t, 2)
		}
	}
	v := variantOf(t)
	// OracleToken: "create a Mutavault token", a token of a named card
	// (Forge: CopyPermanent DefinedName$).
	if strings.HasPrefix(v, "TokenCopy") || v == "TokenFromCopy" || v == "OracleToken" {
		return "tokencopy:" + count
	}
	if v == "ThoseTokens" {
		return "token:*"
	}
	return "token:" + count
}

// putCounters maps one PutCountersAction.
func putCounters(pc any) string {
	v := variantOf(pc)
	a := argsOf(pc)
	ct, num := "", "1"
	for i, x := range a {
		if enumOf(x) == "CounterType" {
			ct = counterType(x)
			if i > 0 && enumOf(a[0]) == "GameNumber" {
				num = gameNumber(a[0])
			}
		}
	}
	switch {
	case strings.HasPrefix(v, "Double"):
		return "counters:double"
	case strings.Contains(v, "Distribute") || strings.Contains(v, "Different"):
		num = "*"
	}
	if ct == "" {
		return "counters:*"
	}
	return "counters:" + ct + ":" + num
}

// counterType renders a CounterType node.
func counterType(x any) string {
	v := variantOf(x)
	if v == "PTCounter" {
		return signed(intOr(argN(x, 0))) + "/" + signed(intOr(argN(x, 1)))
	}
	return counterName(strings.TrimSuffix(v, "Counter"))
}

// layerEffects maps a Vec<LayerEffect> (or StaticLayerEffect) to verbs.
func layerEffects(ls []any) []string {
	var out []string
	pumpP, pumpT, hasPump := 0, 0, false
	for _, l := range ls {
		v := variantOf(l)
		switch v {
		case "AdjustPT":
			pumpP += intOr(argN(l, 0))
			pumpT += intOr(argN(l, 1))
			hasPump = true
		case "AdjustPTX", "AdjustPTForEach", "AdjustPTXY", "AdjustPT_TheChosenPTMod":
			out = append(out, "pump:*")
		case "SetPT", "SetPower", "SetToughness", "SetPowerAndToughnessBoth", "SetCopiablePT":
			out = append(out, "setpt")
		case "SwitchPT":
			out = append(out, "switchpt")
		case "AddAbility", "AddAbilityIfItDoesntHaveIt", "AddCopiableAbility":
			for _, r := range list(rawArgs(l)) {
				out = append(out, grantToken(r))
			}
		case "AddAbilityVariable":
			out = append(out, "grant:*")
		case "LosesAllAbilities", "LosesAbility":
			out = append(out, "loseabilities")
		case "SetController":
			out = append(out, "gaincontrol")
		case "AddColor", "SetColor":
			out = append(out, "color")
		case "SetName", "SetNameToTheChosenName":
			out = append(out, "name")
		default:
			if strings.Contains(v, "type") || strings.Contains(v, "Type") || v == "HasAllCreatureTypes" {
				out = append(out, "type")
			} else if strings.Contains(v, "Copy") {
				out = append(out, "copy")
			} else if f, ok := extLayer[v]; ok {
				out = append(out, f(l)...)
			} else {
				out = append(out, unmapped("LayerEffect."+v))
			}
		}
	}
	if hasPump {
		out = append(out, pump(signed(pumpP), signed(pumpT)))
	}
	return dedup(out)
}

// grantToken names a granted Rule: "grant:<keyword>" for a keyword,
// "grant:ability" for anything else.
func grantToken(r any) string {
	m := &mtgishNorm{s: &Summary{}}
	m.rule(r)
	if len(m.s.Keywords) == 1 && len(m.s.Abilities) == 0 {
		return "grant:" + keywordHeadToken(m.s.Keywords[0])
	}
	if t, ok := p2cOneKeywordGrant(m.s); ok {
		return t
	}
	return "grant:ability"
}

// keywordHeadToken drops a keyword token's parameter ("ward:{2}" -> "ward").
func keywordHeadToken(k string) string {
	if i := strings.IndexByte(k, ':'); i >= 0 {
		return k[:i]
	}
	return k
}

// ruleWord canonicalises a PermanentRule/PlayerEffect variant.
func ruleWord(v string) string { return strings.ToLower(v) }

// mtgishTarget renders one Target/DistributedTarget node.
func mtgishTarget(n node) string {
	v := variantOf(n)
	a := argsOf(n)
	base, perGroup := targetVariantBase(v)
	count := "1"
	switch {
	case strings.HasPrefix(v, "UptoOne"):
		count = "0..1"
	case strings.HasPrefix(v, "UptoNumber"):
		count = "0.." + gameNumber(a[0])
	case strings.HasPrefix(v, "BetweenOneAndNumber"):
		count = "1.." + gameNumber(a[0])
	case strings.HasPrefix(v, "AnyNumberOf"):
		count = "0..*"
	case strings.HasPrefix(v, "OneOrMore"):
		count = "1..*"
	case strings.HasPrefix(v, "OneOrTwo"):
		count = "1..2"
	case strings.HasPrefix(v, "Number"):
		count = gameNumber(a[0])
	}
	if perGroup {
		count = perGroupCount(count)
	}
	var cls []string
	switch {
	case strings.Contains(base, "AnyTarget"), strings.HasSuffix(base, "AnyTargets"), base == "AnyOtherTarget":
		cls = []string{clsAny}
	case strings.Contains(base, "GraveyardCard"):
		cls = []string{clsGraveyard}
	case strings.Contains(base, "ExiledCard") && !strings.Contains(base, "Permanent"):
		cls = []string{clsExile}
	case strings.Contains(base, "SpellOrAbility") || strings.Contains(base, "SpellsOrAbilities"):
		cls = []string{clsSpell, clsAbility}
	case strings.Contains(base, "Ability"):
		cls = []string{clsAbility}
	default:
		if strings.Contains(base, "Player") {
			cls = append(cls, clsPlayer)
		}
		if strings.Contains(base, "Spell") {
			cls = append(cls, clsSpell)
		}
		if strings.Contains(base, "Permanent") {
			found := false
			for _, x := range a {
				if enumOf(x) == "Permanents" {
					cls = append(cls, permanentsClasses(x)...)
					found = true
				}
			}
			if !found {
				// "target spell or permanent" (one SpellsAndPermanents
				// filter).
				cls = append(cls, clsPermanent)
			}
		}
		if strings.Contains(base, "OrExiledCard") {
			cls = append(cls, clsExile)
		}
		if strings.Contains(base, "CardsInGraveyards") {
			// "target creatures and/or creature cards in graveyards".
			cls = append(cls, clsGraveyard)
		}
	}
	if len(cls) == 0 {
		return unmapped("Target." + v)
	}
	return targetToken(narrowPermanent(cls), count)
}

// staticEffects maps a static Rule to verbs.
func staticEffects(r any) []string {
	v := variantOf(r)
	a := argsOf(r)
	switch v {
	case "PermanentLayerEffect", "EachPermanentLayerEffect", "EachPermanentStickyLayerEffect":
		return layerEffects(list(a[1]))
	case "PermanentRuleEffect", "EachPermanentRuleEffect", "PermanentRuleEffect_PlayerMayPayToIgnoreEffectUntil":
		var out []string
		for _, x := range list(a[1]) {
			out = append(out, ruleToken(variantOf(x)))
		}
		return out
	case "PlayerEffect", "EachPlayerEffect", "PlayerEffect_PlayerMayPayToIgnoreEffectUntil":
		var out []string
		for _, x := range list(a[1]) {
			if variantOf(x) == "SpellsCastHaveAbility" {
				// "Sliver spells you cast have replicate": spell
				// effects (gorge: Continuous AffectedZone$ Stack).
				out = append(out, spellEffectTokens(list(argN(x, 1)))...)
				continue
			}
			out = append(out, playerEffectToken(variantOf(x)))
		}
		return dedup(out)
	case "CastEffect":
		return []string{castEffectToken(variantOf(rawArgs(r)))}
	case "ActivatedAbilityEffect":
		return []string{ruleToken(variantOf(argN(r, 1)))}
	case "ThisSpellEffect":
		var out []string
		for _, x := range list(rawArgs(r)) {
			out = append(out, p2cSpellToken(variantOf(x)))
		}
		return out
	case "StackSpellsEffect":
		var out []string
		for _, x := range list(argN(r, 1)) {
			out = append(out, p2cSpellToken(variantOf(x)))
		}
		return out
	}
	if t, ok := p2cStaticEffects(v, a); ok {
		return t
	}
	return []string{"static:" + strings.ToLower(v)}
}

// playerEffectToken maps a PlayerEffect variant.
func playerEffectToken(v string) string {
	if t, ok := d2PlayerToken(v); ok {
		return t
	}
	switch {
	case strings.Contains(v, "Flash"):
		return "flash"
	case v == "MayPlayAnAdditionalLand":
		return "player:mayplayanadditionalland"
	case strings.HasPrefix(v, "MaySpendMana"):
		return "manaconvert"
	case strings.HasPrefix(v, "DecreaseSpellCost"), strings.HasPrefix(v, "ReduceSpellCost"):
		return "costreduction"
	case strings.HasPrefix(v, "IncreaseSpellCost"):
		return "costincrease"
	case v == "MayPayAdditionalCostToCastSpellsForEffect":
		// An optional additional cost, not a permission to play.
		return "player:maypayadditionalcosttocastspellsforeffect"
	case strings.HasPrefix(v, "May") && (strings.Contains(v, "Play") || strings.Contains(v, "Cast")):
		return "mayplay"
	}
	return "player:" + strings.ToLower(v)
}

// castEffectToken maps a CastEffect variant (this spell's own cost).
func castEffectToken(v string) string {
	switch {
	case strings.HasPrefix(v, "ReduceCastingCost"):
		return "costreduction"
	case strings.HasPrefix(v, "AdditionalCastingCost"):
		return "additionalcost"
	case strings.HasPrefix(v, "AlternateCastingCost"), strings.HasPrefix(v, "MayCastWithoutPaying"):
		// "If <condition>, you may cast this spell without paying its mana
		// cost" is an alternative cost (Forge: S:Mode$ AlternativeCost).
		return "alternativecost"
	case strings.Contains(v, "Flash"):
		return "flash"
	case v == "MayPlayAnAdditionalLand":
		return "player:mayplayanadditionalland"
	}
	return "cast:" + strings.ToLower(v)
}

// replacementEffects maps a Replace*/PreventDamage rule to verbs.
func replacementEffects(r any) []string {
	v := variantOf(r)
	if t, ok := p2cReplSynonyms[v]; ok {
		return []string{t}
	}
	return []string{"replace:" + strings.ToLower(strings.TrimPrefix(v, "Replace"))}
}

// enterEffects maps the actions of an "As this enters" replacement.
func enterEffects(as []any) []string {
	var out []string
	for _, x := range as {
		v := variantOf(x)
		switch {
		case v == "EntersTapped":
			out = append(out, "etbtapped")
		case v == "EntersWithACounter":
			out = append(out, "counters:"+counterType(rawArgs(x))+":1")
		case v == "EntersWithNumberCounters":
			out = append(out, "counters:"+counterType(argN(x, 1))+":"+gameNumber(argN(x, 0)))
		case strings.HasPrefix(v, "Choose"):
			out = append(out, "choose")
		case v == "LookAtAPlayersHand":
			out = append(out, "reveal")
		case v == "MayCost":
			// "You may pay X. If you don't, it enters tapped": the cost is
			// not an effect; its consequence is mapped where it appears.
		case strings.Contains(v, "Copy"):
			out = append(out, "copy")
		case v == "If" || v == "Unless" || v == "IfPassesFilter" || v == "MayAction" || v == "MayActions":
			for _, sub := range argsOf(x) {
				if l := list(sub); l != nil {
					out = append(out, enterEffects(l)...)
				} else if enumOf(sub) == "ReplacementActionWouldEnter" {
					out = append(out, enterEffects([]any{sub})...)
				}
			}
		default:
			if f, ok := extEnter[v]; ok {
				out = append(out, f(x)...)
			} else {
				out = append(out, unmapped("ReplacementActionWouldEnter."+v))
			}
		}
	}
	return out
}
