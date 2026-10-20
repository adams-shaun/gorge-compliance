package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit D2: the static abilities, game-rule modifiers and "as this enters"
// effects on both sides, so the two halves agree by construction. Gorge's
// side keys on the S: mode, the keyword line and the as-enters API; mtgish's
// on the Rule, LayerEffect and ReplacementActionWouldEnter variants.
//
// Restriction and permission statics take mtgish's own PermanentRule or
// PlayerEffect variant name ("rule:<variant>", "player:<variant>"), chosen
// from the S: line's parameters, since that is the spelling mtgish's
// normaliser already emits for them. Rules mtgish carries as a top-level
// Rule variant of their own (trigger doubling, the legend rule, attack
// limits) take a token named after the Forge mode instead.

func init() {
	for k, f := range d2Statics {
		extStatic[k] = f
	}
	for k, f := range d2Keywords {
		extKeyword[k] = f
	}
	for k, f := range d2Rules {
		extRule[k] = f
	}
	for k, f := range d2Enters {
		extEnter[k] = f
	}
	for k, f := range d2Layers {
		extLayer[k] = f
	}
}

func fixed(tok ...string) func(map[string]string) []string {
	return func(map[string]string) []string { return tok }
}

// Shared tokens of this unit (VOCAB).
const (
	tokPanharmonicon   = "panharmonicon"   // a triggered ability triggers an additional time
	tokDisableTriggers = "disabletriggers" // events of a kind cause no triggers
	tokLegendRule      = "static:thelegendsruledoesntapply"
	tokNoCleanupDamage = "nocleanupdamage"   // damage isn't removed during cleanup
	tokWitherDamage    = "witherdamage"      // all damage is dealt as though its source had wither
	tokCantDayTime     = "cantchangedaytime" // it can't become night (or day)
	tokCantPhaseIn     = "cantphasein"       // permanents can't phase in
	tokVillainous      = "additionalvillainouschoice"
	tokPrepared        = "alterattribute:prepared"
	tokDay             = "daytime:day"
	tokChangeText      = "changetext" // a colour or type word in a text box is changed
)

// d2Statics maps S: modes.
var d2Statics = map[string]func(p map[string]string) []string{
	"Panharmonicon":     fixed(tokPanharmonicon),
	"DisableTriggers":   fixed(tokDisableTriggers),
	"IgnoreLegendRule":  fixed(tokLegendRule),
	"NoCleanupDamage":   fixed(tokNoCleanupDamage),
	"WitherDamage":      fixed(tokWitherDamage),
	"CantChangeDayTime": fixed(tokCantDayTime),
	"CantPhaseIn":       fixed(tokCantPhaseIn),
	"AttackRestrict": func(p map[string]string) []string {
		return []string{"attackrestrict:" + literalOr(p["MaxAttackers"], "*")}
	},
	"BlockRestrict": func(p map[string]string) []string {
		return []string{"blockrestrict:" + literalOr(p["MaxBlockers"], "*")}
	},
	"MinMaxBlocker": func(p map[string]string) []string {
		switch {
		case p["Max"] == "1":
			return []string{"rule:cantbeblockedbymorethanone"}
		case p["Min"] == "All":
			return []string{"rule:cantbeblockedunlessalldefendersblockit"}
		}
		return []string{"rule:cantbeblockedexceptbymultipledefenders"}
	},
	"OptionalCost": func(p map[string]string) []string {
		if p["ReduceColor"] != "" {
			// "You may pay 2 life rather than {R} for red permanent spells."
			return []string{"player:maypaytoreducecoloredmanacostofspells"}
		}
		// "As an additional cost to cast this spell, you may ...": a cost,
		// not an ability, like AlternateAdditionalCost (mtgish's
		// OptionalAdditionalCastingCost is skipped the same way; it also
		// writes the choice as AdditionalCost_Modal actions or a cost
		// reduction, never consistently as a static).
		return nil
	},
	"CanAttackDefender": func(p map[string]string) []string {
		if strings.HasPrefix(p["ValidAttacked"], "Player") {
			return []string{"rule:canattackaplayerasthoughitdidnthavedefender"}
		}
		return []string{"rule:canattackasthoughitdidnthavedefender"}
	},
	"OptionalAttackCost": func(p map[string]string) []string {
		if p["Trigger"] != "" {
			return []string{"rule:maybeexertedasitattackswithtrigger"}
		}
		return []string{"rule:maybeexertedasitattacks"}
	},
	"CantTarget": func(p map[string]string) []string {
		switch {
		case p["ValidTarget"] == "" && strings.Contains(p["AffectedZone"], "Graveyard"):
			// "Cards in graveyards can't be the targets of spells or
			// abilities": mtgish's EachCardInAGraveyardEffect.
			return []string{"static:eachcardinagraveyardeffect"}
		case p["EffectZone"] == "Stack" || strings.HasPrefix(p["ValidSource"], "Spell.Self"):
			// "This spell can't target untapped creatures."
			return []string{"cast:cantchooseatarget"}
		}
		sa := p["ValidSA"]
		spells := sa == "" || strings.Contains(sa, "Spell")
		abilities := sa == "" || strings.Contains(sa, "Activated") || strings.Contains(sa, "Triggered")
		switch {
		case spells && !abilities:
			return []string{"rule:cantbethetargetofspells"}
		case abilities && !spells:
			return []string{"rule:cantbethetargetofabilities"}
		}
		return []string{"rule:cantbethetargetofspellsorabilities"}
	},
	// S:Mode$ CantAttackUnless: one mode whatever it names as the defender
	// (mtgish's per-defender variants fold into it, d2RuleSynonyms).
	"CantAttackUnless": fixed("rule:cantattackunlesscost"),
	"CantBlockUnless": func(p map[string]string) []string {
		switch {
		case p["Attacker"] != "" && p["ValidCard"] == "":
			// "Enchanted creature can't be blocked unless ... pays."
			return []string{"rule:cantbeblockedunlesscost"}
		case p["Attacker"] != "":
			return []string{"rule:cantblockattackersunlesscost"}
		}
		return []string{"rule:cantblockunlesscost"}
	},
	"CombatDamageToughness":         fixed("rule:assignstoughnesscombatdamage"),
	"UntapOtherPlayer":              fixed("rule:untapsduringotherplayersuntapsteps"),
	"AssignCombatDamageAsUnblocked": fixed("rule:mayassigncombatdamageasthoughnotblocked"),
	"CantPlayLand": func(p map[string]string) []string {
		switch {
		case p["Origin"] == "Graveyard":
			return []string{"player:cantplaylandsfromgraveyards"}
		case p["Origin"] == "Hand":
			return []string{"player:cantplaylandsfromtheirhand"}
		case p["ValidCard"] != "" && !strings.Contains(p["ValidCard"], ","):
			// A list is the shared filter of a multi-mode static (lands
			// and spells alike), not a land type.
			return []string{"player:cantplaylandsoftype"}
		}
		return []string{"player:cantplaylands"}
	},
	"IgnoreLandWalk": fixed("rule:canbeblockedasthoughitdidnthave"),
	"CantSacrifice": func(p map[string]string) []string {
		cause := p["ValidCause"]
		switch {
		case cause == "":
			return []string{"rule:cantbesacrificed"}
		case p["ForCost"] == "True":
			return []string{"player:cantsacrificepermanentstoactivateabilities", "player:cantsacrificepermanentstocastspells"}
		case strings.HasPrefix(cause, "Triggered") || strings.HasPrefix(cause, "Activated"):
			return []string{"player:cantbecausedtosacrificepermanentsbyabilities"}
		}
		return []string{"player:cantbecausedtosacrificepermanentsbyspellandabilities"}
	},
	"CantPayLife": func(p map[string]string) []string {
		return []string{"player:cantpaylifetoactivateabilities", "player:cantpaylifetocastspells"}
	},
	"CantDiscard": fixed("player:cantbecausedtodiscardcardsbyspellandabilities"),
	"CantExile":   fixed("player:cantbecausedtoexilepermanentsbyabilities"),
	"TapPowerValue": func(p map[string]string) []string {
		how := "asthoughpowerweregreater"
		if p["Value"] == "Toughness" {
			how = "usingtoughnessratherthanpower"
		}
		var out []string
		for _, sa := range strings.Split(p["ValidSA"], ",") {
			switch {
			case strings.Contains(sa, "Crew"):
				out = append(out, "rule:crewsvehicles"+how)
			case strings.Contains(sa, "Saddle"):
				out = append(out, "rule:saddlesmounts"+how)
			case strings.Contains(sa, "Station"):
				out = append(out, "rule:stationspermanents"+how)
			}
		}
		return out
	},
	"CantPutCounter": func(p map[string]string) []string {
		typed := p["CounterType"] != ""
		switch {
		case p["ValidPlayer"] != "" && typed:
			return []string{"player:cantgetcountersoftype"}
		case p["ValidPlayer"] != "":
			return []string{"player:cantgetcounters"}
		case typed:
			return []string{"rule:canthavecountersoftypeputonit"}
		}
		return []string{"rule:canthavecountersputonit"}
	},
	"CantDraw": func(p map[string]string) []string {
		if p["DrawLimit"] != "" {
			return []string{"player:cantdrawmorethanonecardeachturn"}
		}
		return []string{"player:cantdrawcards"}
	},
	"UnspentMana": func(p map[string]string) []string {
		if p["ManaType"] != "" {
			return []string{"player:doesntlosecoloredmanaasstepsandphasesend"}
		}
		return []string{"player:doesntlosemanaasstepsandphasesend"}
	},
	"ActivateAbilityAsIfHaste": fixed("player:mayactivateabilitiesofcreaturesasthoughtheyhadhaste"),
	"AttackRequirement": func(p map[string]string) []string {
		if p["ValidAttacker"] == "Card.Self" {
			// "If a creature you control attacks, this also attacks."
			return []string{"rule:mustattackifanothercreatureattacks"}
		}
		return []string{"rule:attackingcausesotherstoattack"}
	},
	// Both spellings mtgish uses ("as though they didn't have shadow", "as
	// though it had shadow") are the same rule; Forge writes one.
	"CanBlockIfShadow": fixed("rule:canblockcreatureswithshadowasthoughithadshadow"),
	"MustBlock":        fixed("rule:mustblock"),
	"CantAttach": func(p map[string]string) []string {
		switch {
		case strings.HasPrefix(p["ValidCard"], "Card.Equipment") || strings.HasPrefix(p["ValidCard"], "Equipment"):
			return []string{"rule:cantbeequipped"}
		case strings.HasPrefix(p["ValidCardToTarget"], "Aura"):
			// "Can't be enchanted by other Auras" (new attachments only).
			return []string{"rule:cantbeenchantedanddoesntremove"}
		case strings.HasPrefix(p["ValidCard"], "Aura"):
			return []string{"rule:cantbeenchantedbyanenchantment"}
		}
		// "Can be attached only to a creature with ...".
		return []string{"rule:canbeattachedonlytoapermanent"}
	},
	"Activations": func(p map[string]string) []string {
		sa := p["ValidSA"]
		switch {
		case strings.Contains(sa, "Boast"):
			return []string{"rule:canboasttwice"}
		case strings.Contains(sa, "Exhaust"):
			return []string{"player:mayactivateexhaustabilitiesasthoughtheyhaventbeenactivated"}
		case strings.Contains(sa, "PowerUp"):
			return []string{"player:mayactivatepowerupabilitiesofpermanentsanadditionaltime"}
		}
		return []string{"activations"}
	},
	"CanAttackIfHaste": func(p map[string]string) []string {
		if p["ValidTarget"] != "" {
			return []string{"rule:canattackplayersandplaneswalkersasthoughithadhaste"}
		}
		return []string{"rule:canattackasthoughithadhaste"}
	},
	"CantBeCopied": fixed("spell:cantbecopied"),
	"CantCrew":     fixed("rule:cantcrew"),
	"IgnoreHexproof": func(p map[string]string) []string {
		out := []string{"rule:canbethetargetofspellsandabilitiesasthoughtheydidnthavehexproof"}
		for _, v := range strings.Split(p["ValidEntity"], ",") {
			if v == "Opponent" || v == "Player" {
				out = append(out, "player:canbethetargetofspellsandabilitiesasthoughtheydidnthavehexproof")
			}
		}
		return out
	},
	"ManaRestriction": func(p map[string]string) []string {
		if p["InvertValidSource"] == "True" {
			// "You can't spend mana to cast this spell."
			return []string{"cast:cantspendmanatocast"}
		}
		return []string{"cast:spendonlymanafrompermanentstocast"}
	},
	"MustTarget":    fixed("player:whilechoosingtargetsmustchooseatleastonepermanentifable"),
	"CantTransform": fixed("rule:canttransform"),
	"NumLoyaltyAct": func(p map[string]string) []string {
		if p["ValidCard"] == "Card.Self" {
			return []string{"player:mayactivateloyaltyabilityofplanewalkeranadditionaltime"}
		}
		return []string{"player:mayactivateloyaltyabilitiestwiceeachturn"}
	},
	"PlayerMustAttack": func(p map[string]string) []string {
		if p["MustAttack"] != "" {
			return []string{"player:mustattackplayerorplaneswalkerscontrolledby"}
		}
		return []string{"player:mustattackwithanumberofcreatures"}
	},
	"CantBeSuspected":                   fixed("rule:cantbecomesuspected"),
	"Devotion":                          fixed("player:increasedevotiontocolorandcolorcombinationsbynumber"),
	"CanBlockIfReach":                   fixed("rule:canblockcreatureswithflyingasthoughithadreach"),
	"FlipCoinMod":                       fixed("player:firsttwocoinflipseachturnareheadsandyouwin"),
	"SurveilNum":                        fixed("player:maylookatanadditionalnumbercardsastheysurveil"),
	"PlotZone":                          fixed("player:mayplotcardsfromthetopoftheirlibrary"),
	"ColorlessDamageSource":             fixed("rule:isacolorlesssourceofdamage"),
	"CantGainControl":                   fixed("rule:cantbegainedcontrolof"),
	"CantVenture":                       fixed("player:cantventureintothedungeonmorethanonceeachturn"),
	"CombatDamageNegatePower":           fixed("rule:assignscombatdamageasthoughpowerwerepositive"),
	"CantBeBeamedUp":                    fixed("rule:cantbebeamedup"),
	"BlockTapped":                       fixed("rule:canblockasthoughuntapped"),
	"CantChangeLife":                    fixed("player:lifetotalcantchange"),
	"MaxCounter":                        fixed("rule:canthavemorethannumbercountersoftype"),
	"DrawFromBottom":                    fixed("player:drawscardsfrombottomoftheirlibrary"),
	"IgnorePlaneswalkerZeroLoyaltyRule": fixed("rule:arentputintograveyardforhaving0loyalty"),
	"GainLifeRadiation":                 fixed("player:gainsliferatherthanloselifefromradiation"),
	"SetCost":                           fixed("player:setminimumspellcost"),
	"ManaBurn":                          fixed("player:asloseunspentmana"),
	// A coin-flip doubler and "damage dealt to you is dealt as poison" are
	// replacements on mtgish's side (one Replace rule each).
	"FlipCoinDoubler": fixed("replace:wouldflipacoin"),
	"InfectDamage":    fixed("replace:woulddealdamage"),
	// "Counters remain on it as it changes zones": a rule about the card
	// itself, mtgish's SelfEffect.
	"CountersRemain": fixed("static:selfeffect"),
	"CantRegenerate": fixed("rule:cantberegenerated"),
}

// d2ContinuousEffects maps the Continuous parameters gorgeStaticEffects'
// own case does not read; their tokens are added to that case's.
func d2ContinuousEffects(p map[string]string) []string {
	var out []string
	if n := p["CanBlockAmount"]; n != "" {
		if n == "1" {
			out = append(out, "rule:canblockanadditionalcreature")
		} else {
			out = append(out, "rule:canblockanadditionalnumbercreatures")
		}
	}
	if p["CanBlockAny"] != "" {
		out = append(out, "rule:canblockanynumberofcreatures")
	}
	if n := p["SetMaxHandSize"]; n != "" && n != "Unlimited" {
		out = append(out, "player:setmaximumhandsize")
	}
	if n := p["RaiseMaxHandSize"]; n != "" {
		if strings.HasPrefix(n, "-") {
			out = append(out, "player:reducemaximumhandsize")
		} else {
			out = append(out, "player:increasemaximumhandsize")
		}
	}
	if p["GainsAbilitiesOf"] != "" || p["GainsAbilitiesOfDefined"] != "" || p["GainsAbilitiesOfZones"] != "" {
		// "Has all activated abilities of the exiled cards": abilities not
		// named on the card.
		out = append(out, "grant:*")
	}
	if p["GainTextOf"] != "" {
		out = append(out, "copy")
	}
	if p["ManaConversion"] != "" {
		out = append(out, "manaconvert")
	}
	if p["Goad"] != "" {
		out = append(out, "goad")
	}
	if p["RemoveNonManaAbilities"] != "" {
		out = append(out, "loseabilities")
	}
	if p["ControlOpponentsSearchingLibrary"] != "" {
		out = append(out, "player:controlsplayerswhiletheyaresearchinglibraries")
	}
	if p["LethalDamageByPower"] != "" {
		out = append(out, "rule:determinelethaldamageusingpowerratherthantoughness")
	}
	if p["AdditionalOptionalVote"] != "" {
		out = append(out, "player:getsanoptionaladditionalvote")
	}
	if p["AdditionalVote"] != "" {
		out = append(out, "player:getsanadditionalvote")
	}
	if p["ControlVote"] != "" {
		out = append(out, "player:chooseshowplayersvote")
	}
	if p["DeclaresAttackers"] != "" {
		out = append(out, "player:chooseswhichcreaturesattack")
	}
	if p["DeclaresBlockers"] != "" {
		out = append(out, "player:chooseswhichcreaturesblockandhowtheyblock")
	}
	if p["ChangeColorWordsTo"] != "" {
		out = append(out, tokChangeText)
	}
	if p["AdditionalVillainousChoice"] != "" {
		out = append(out, tokVillainous)
	}
	return out
}

// d2IgnoreEffectCostOnly reports whether p's only effect is IgnoreEffectCost:
// "its controller may pay to ignore this effect" modifies the statics it
// sits beside and is no ability of its own (mtgish folds it into the
// restricted rule, PermanentRuleEffect_PlayerMayPayToIgnoreEffectUntil).
func d2IgnoreEffectCostOnly(p map[string]string) bool {
	return p["IgnoreEffectCost"] != ""
}

// d2KeywordOnlyCDA reports whether a CharacteristicDefining static only
// grants keywords ("this spell has flash as long as ..."): Forge marks it
// characteristic-defining so it works in every zone, but it is a keyword
// grant, compared as one.
func d2KeywordOnlyCDA(p map[string]string) bool {
	return p["AddKeyword"] != "" && p["SetPower"] == "" && p["SetToughness"] == "" &&
		p["SetColor"] == "" && p["AddType"] == ""
}

func staticAbility(tok ...string) func(g *gorgeNorm, param string) {
	return func(g *gorgeNorm, _ string) { g.ability(Ability{Kind: "static", Effects: tok}) }
}

// staticAbilities is one static per token.
func staticAbilities(tok ...string) func(g *gorgeNorm, param string) {
	return func(g *gorgeNorm, _ string) {
		for _, t := range tok {
			g.ability(Ability{Kind: "static", Effects: []string{t}})
		}
	}
}

func pregameAbility(g *gorgeNorm, _ string) { g.ability(Ability{Kind: "pregame"}) }

// nothing maps a keyword line that is no ability of the card in play
// (deck-building, a cost restriction); the comment at each use says why
// mtgish has nothing either.
func nothing(*gorgeNorm, string) {}

// d2Keywords maps K: lines by their head (the whole line for the
// reminder-text keywords Forge writes as sentences).
var d2Keywords = map[string]func(g *gorgeNorm, param string){
	"You may choose not to untap CARDNAME during your untap step.": staticAbility("rule:maychoosenottountapduringuntap"),
	"You may assign CARDNAME's combat damage divided as you choose among defending player and/or any number of creatures they control.": staticAbility(
		"rule:mayassigncombatdamagedividedasyouchooseamongplayerorcreaturesandplaneswalkers"),
	"If CARDNAME is unblocked, you may have it assign its combat damage to a creature defending player controls.": staticAbility(
		"rule:mayassigncombatdamagetoapermanent"),
	"All creatures able to block CARDNAME do so.":                staticAbility("rule:allcreaturesmustblockit"),
	"MustBeBlockedByAll":                                         staticAbility("rule:allcreaturesmustblockit"),
	"CARDNAME must be blocked by two or more creatures if able.": staticAbility("rule:mustbeblockedbyatleastnumberdefenders"),
	"CARDNAME must be blocked by exactly one creature if able.":  staticAbility("rule:mustbeblockedbyexactlyonedefender"),
	"CARDNAME can only attack alone.":                            staticAbility("rule:canonlyattackalone"),
	"CARDNAME can't be destroyed by lethal damage unless lethal damage dealt by a single source is marked on it.": staticAbility("rule:canonlybedestroyedbylethaldamagefromasinglesource"),
	"CARDNAME can't attack unless a creature with greater power also attacks.":                                    staticAbility("rule:cantattackunlessanothercreatureattacks"),
	"CARDNAME can't attack unless a black or green creature also attacks.":                                        staticAbility("rule:cantattackunlessanothercreatureattacks"),
	"CARDNAME can't block unless a creature with greater power also blocks.":                                      staticAbility("rule:cantblockunlessanotherdefender"),
	"CARDNAME can't attack unless at least two other creatures attack.":                                           staticAbility("rule:cantattackunlessanumberofothercreatureattacks"),
	"CARDNAME can't block unless at least two other creatures block.":                                             staticAbility("rule:cantblockunlessotherdefenders"),
	"Once each turn, you may pay {1} to reroll one or more dice you rolled.":                                      staticAbility("player:onceeachturnmaypaytorerollanynumberofdicerolled"),
	"After you roll a die, you may pay 1 life. If you do, increase or decrease the result by 1. Do this only once each turn.": staticAbility(
		"player:onceeachturnmaypaytoincrementordecrementadiceroll"),
	"After you roll a die, you may remove a +1/+1 counter from Xenosquirrels. If you do, increase or decrease the result by 1.": staticAbility(
		"player:maypaytoincrementordecrementadiceroll"),
	"As an additional cost to cast creature spells, you may pay any amount of mana. If you do, that creature enters with that many additional +1/+1 counters on it.": staticAbility(
		"player:maypayadditionalcosttocastspellsforeffect"),
	// "You may cast this from your library while searching it": a rule
	// about the card itself, mtgish's SelfEffect.
	"While you're searching your library, you may cast CARDNAME from your library.": staticAbility("static:selfeffect"),
	// Sentence keywords gorge's compiler canonicalises (cards.KeywordHead's
	// canonicalKeywordHeads): restrictions, one static per rule as mtgish
	// writes them.
	"MustBlock":                staticAbility("rule:mustbeblocked"),
	"CantAttackOrBlock":        staticAbilities("rule:cantattack", "rule:cantblock"),
	"CantAttackOrBlockAlone":   staticAbilities("rule:cantattackalone", "rule:cantblockalone"),
	"CantAttackAlone":          staticAbility("rule:cantattackalone"),
	"CantBlockAlone":           staticAbility("rule:cantblockalone"),
	"MayEffectFromOpeningDeck": pregameAbility,
	"Before drawing your opening hand, choose 1, 2, or 3.": pregameAbility,
	"Your starting hand size is five.":                     pregameAbility,
	// Strive ("this spell costs more for each target beyond the first") is
	// an additional casting cost; mtgish's AdditionalCastingCost is skipped
	// the same way.
	"Strive": nothing,
	// A restriction on paying the cost; mtgish carries no rule for it.
	"Spend only colored mana on X. No more than one mana of each color may be spent this way.": nothing,
	// Deck-building rules, not abilities in play (mtgish's DeckConstruction
	// and ConspiracyDeck are skipped the same way).
	"DeckLimit": nothing,
	"Your minimum deck size is reduced by five.": nothing,
	"Your deck can't have basic land cards.":     nothing,
	"Aura swap":                                  func(g *gorgeNorm, param string) { g.kw("auraswap:" + gorgeCost(param)) },
	"Beam me up":                                 func(g *gorgeNorm, param string) { g.kw("beammeup:" + gorgeCost(param)) },
	"Transfigure":                                func(g *gorgeNorm, param string) { g.kw("transfigure:" + gorgeCost(param)) },
	"Trample": func(g *gorgeNorm, param string) {
		if strings.Contains(param, "Planeswalker") {
			g.kw("trampleoverplaneswalkers")
			return
		}
		g.kw("trample")
	},
}

// The mtgish side.

func staticRule(tok ...string) func(m *mtgishNorm, r any) {
	return func(m *mtgishNorm, _ any) { m.ability(Ability{Kind: "static", Effects: tok}) }
}

func replacementRule(tok string) func(m *mtgishNorm, r any) {
	return func(m *mtgishNorm, _ any) { m.ability(Ability{Kind: "replacement", Effects: []string{tok}}) }
}

func skipRule(*mtgishNorm, any) {}

func pregameRule(m *mtgishNorm, _ any) { m.ability(Ability{Kind: "pregame"}) }

// xCost renders an "X" keyword's cost: a bare ManaCostX list, or a PayManaX
// cost whose first argument is that list.
func xCost(c any) string {
	if variantOf(c) == "PayManaX" {
		return mtgishCost(argN(c, 0))
	}
	return mtgishCost(c)
}

func costKeywordRule(tok string) func(m *mtgishNorm, r any) {
	return func(m *mtgishNorm, r any) { m.kw(tok + ":" + xCost(rawArgs(r))) }
}

// d2Rules maps Rule variants.
var d2Rules = map[string]func(m *mtgishNorm, r any){
	// Trigger doubling, one Forge mode (Panharmonicon) for every event.
	"AbilitiesTriggerAnAdditionalTime":                                                     staticRule(tokPanharmonicon),
	"APermanentEnteringTheBattlefieldCausesAbilitiesToTriggerAnAdditionalTime":             staticRule(tokPanharmonicon),
	"APermanentAttackingCausesAbilitiesToTriggerAnAdditionalTime":                          staticRule(tokPanharmonicon),
	"APermanentDyingCausesAbilitiesToTriggerAnAdditionalTime":                              staticRule(tokPanharmonicon),
	"APermanentLeavingTheBattlefieldCausesAbilitiesToTriggerAnAdditionalTime":              staticRule(tokPanharmonicon),
	"APermanentTurningFaceUpCausesAbilitiesToTriggerAnAdditionalTime":                      staticRule(tokPanharmonicon),
	"APermanentBeingDealtDamageCausesAbilitiesToTriggerAnAdditionalTime":                   staticRule(tokPanharmonicon),
	"APermanentDealingCombatDamageToAPlayerCausesAbilitiesToTriggerAnAdditionalTime":       staticRule(tokPanharmonicon),
	"APermanentBecomingTheTargetOfASpellOrAbilityCausesAbilitiesToTriggerAnAdditionalTime": staticRule(tokPanharmonicon),
	"APlayerCastingOrCopyingASpellCausesAnAbilityToTriggerAnAdditionalTime":                staticRule(tokPanharmonicon),
	"APlayerDrawingACardCausesAbilitiesToTriggerAnAdditionalTime":                          staticRule(tokPanharmonicon),
	"APlayerGainingLifeCausesAbilitiesToTriggerAnAdditionalTime":                           staticRule(tokPanharmonicon),
	"PermanentsEnteringTheBattlefieldDontCauseAbilitiesToTrigger":                          staticRule(tokDisableTriggers),
	"PermanentsDyingDontCauseAbilitiesToTrigger":                                           staticRule(tokDisableTriggers),
	"WardAbilitiesOfPermanentsDontTrigger":                                                 staticRule(tokDisableTriggers),
	"TheLegendsRuleDoesntApplyToPermanents":                                                staticRule(tokLegendRule),
	"DamageIsntRemovedFromCreaturesDuringCleanup":                                          staticRule(tokNoCleanupDamage),
	"AllDamageIsDealtAsThoughItsSourceHadWither":                                           staticRule(tokWitherDamage),
	"ItCantBecomeNight":     staticRule(tokCantDayTime),
	"PermanentsCantPhaseIn": staticRule(tokCantPhaseIn),
	"APlayerFacingAVillainousChoiceFacesItAnAdditionalTime": staticRule(tokVillainous),
	"DamageFromPermanentCantBePrevented":                    staticRule("static:damagecantbeprevented"),
	"CombatDamageOfCreaturesCantBePrevented":                staticRule("static:damagecantbeprevented"),
	"CountsAsACardWithNameForSpellsNamed":                   staticRule("rule:countsasacardwithnameforspellsnamed"),
	"FlashForCasters":                                       staticRule("grant:flash"),
	"NoMoreThanNumberCreaturesCanAttack":                    attackLimit("attackrestrict"),
	"NoMoreThanNumberCreaturesCanAttackPlayer":              attackLimit("attackrestrict"),
	"NoMoreThanNumberCreaturesCanAttackPermanent":           attackLimit("attackrestrict"),
	"NoMoreThanNumberCreaturesCanBlock":                     attackLimit("blockrestrict"),
	"IncreaseBuybackCosts":                                  staticRule("costincrease"),
	"IncreaseMorphCosts":                                    staticRule("costincrease"),
	// "Cards can't enter the battlefield": a CantHappen replacement of the
	// event in Forge.
	"CardsCantEnterTheBattlefield":                        replacementRule("replace:wouldenter"),
	"CardsInEachPlayersGraveyardsCantEnterTheBattlefield": replacementRule("replace:wouldenter"),
	"CardsInEachPlayersLibrariesCantEnterTheBattlefield":  replacementRule("replace:wouldenter"),
	// "As this transforms / is turned face up / becomes attached, ...":
	// a replacement compared by what it does, as unit B maps the R: line's
	// ReplaceWith chain.
	"AsPermanentTransforms":                  replacementActions(1),
	"AsPermanentIsTurnedFaceUp":              replacementActions(1),
	"AsPermanentBecomesAttachedToAPermanent": replacementActions(2),
	// "While an opponent searches their library, they exile each card they
	// find; you may play those cards": the R: line's exile and its MayPlay
	// effect.
	"WhilePlayersAreSearchingTheirLibraryTheyExileEachCardTheyFindAndPlayerMayPlayThoseCardsWhileTheyRemainExiledAndMaySpendManaAsThoughItWereAnyColor": func(m *mtgishNorm, _ any) {
		m.ability(Ability{Kind: "replacement", Effects: []string{"move:library>exile", "mayplay"}})
	},
	// Not abilities in play: the companion condition and the deck rules
	// (gorge skips the Companion keyword and the deck-building lines).
	"Companion":                         skipRule,
	"DeckBuildingIfCommander":           skipRule,
	"ConspiracyDeck":                    skipRule,
	"BeforeShufflingDeckToStartTheGame": pregameRule,
	"StartingHandSizeIs":                pregameRule,
	// Keywords whose cost carries an X (or another cost shape).
	"CyclingX":           costKeywordRule("cycling"),
	"MiracleX":           costKeywordRule("miracle"),
	"MorphX":             costKeywordRule("morph"),
	"DisguiseX":          costKeywordRule("disguise"),
	"HarmonizeX":         costKeywordRule("harmonize"),
	"SurgeX":             costKeywordRule("surge"),
	"WarpX":              costKeywordRule("warp"),
	"ForetellX":          costKeywordRule("foretell"),
	"FreerunningX":       costKeywordRule("freerunning"),
	"BestowX":            costKeywordRule("bestow"),
	"EmergeFromArtifact": costKeywordRule("emerge"),
	"CommanderNinjutsu":  costKeywordRule("ninjutsu"),
	"Transfigure":        costKeywordRule("transfigure"),
	"AuraSwamp":          costKeywordRule("auraswap"),
	"BeamMeUp":           costKeywordRule("beammeup"),
	"CasualtyX":          func(m *mtgishNorm, _ any) { m.kw("casualty:*") },
	"CrewOnceEachTurn":   func(m *mtgishNorm, r any) { m.kw("crew:" + gameNumber(rawArgs(r))) },
	"SpaceSculptor":      func(m *mtgishNorm, _ any) { m.kw("spacesculptor") },
	"TrampleOverPlaneswalkers": func(m *mtgishNorm, _ any) {
		m.kw("trampleoverplaneswalkers")
	},
	"EnchantGraveyardCard": func(m *mtgishNorm, _ any) { m.kw("enchant:" + clsGraveyard) },
	"KickerForSpellAbility": func(m *mtgishNorm, r any) {
		// "Kicker {R}. If this spell was kicked, it has split second."
		m.kw("kicker:" + mtgishCost(argN(r, 0)))
		m.sub(argN(r, 1), true)
	},
	"AnchorWord": func(m *mtgishNorm, r any) {
		// "As this enters, choose Khans or Dragons. • Khans — ...": each
		// anchor grants its ability while chosen, as scripts write it.
		m.ability(Ability{Kind: "static", Effects: []string{grantToken(argN(r, 1))}})
	},
	"Mystery": func(m *mtgishNorm, r any) {
		// A Case: "To solve — <condition>" is checked at the beginning of
		// its controller's end step; "Solved — <rules>" are its rules.
		// Solving itself is the rule's structure, not an effect (unit B maps
		// the script's AlterAttribute Solved to nothing).
		m.ability(Ability{Kind: "triggered", Event: "end-step"})
		m.rules(list(argN(r, 1)))
	},
	"AsSchemeIsSetInMotion": func(m *mtgishNorm, r any) {
		e := &effects{}
		for _, a := range list(argN(r, 1)) {
			if n, ok := a.(node); ok {
				e.action(n, variantOf(n))
			}
		}
		m.ability(Ability{Kind: "triggered", Event: "setinmotion", Targets: e.targets, Effects: e.effects})
	},
	"SpellActions_MadnessX": func(m *mtgishNorm, r any) {
		m.kw("madness:" + xCost(argN(r, 0)))
		e := &effects{}
		e.alternatives(argsOf(r)[1:]...)
		m.ability(Ability{Kind: "spell", Targets: e.targets, Effects: e.effects})
	},
	"SpellActions_AdditionalCostOptions": func(m *mtgishNorm, r any) {
		// One branch per additional-cost choice (paid or not), merged.
		var alts []any
		for _, o := range list(rawArgs(r)) {
			alts = append(alts, argN(o, len(argsOf(o))-1))
		}
		e := &effects{}
		e.alternatives(alts...)
		m.ability(Ability{Kind: "spell", Targets: e.targets, Effects: e.effects})
	},
}

// replacementActions maps a rule whose argument i is its action list
// (FaceUpAction, TransformAction, AttachAction): each action by its verb.
func replacementActions(i int) func(m *mtgishNorm, r any) {
	return func(m *mtgishNorm, r any) {
		e := &effects{}
		for _, a := range list(argN(r, i)) {
			if n, ok := a.(node); ok {
				e.action(n, variantOf(n))
			}
		}
		m.ability(Ability{Kind: "replacement", Targets: e.targets, Effects: e.effects})
	}
}

func attackLimit(tok string) func(m *mtgishNorm, r any) {
	return func(m *mtgishNorm, r any) {
		m.ability(Ability{Kind: "static", Effects: []string{tok + ":" + gameNumber(argN(r, 0))}})
	}
}

// d2Enters maps ReplacementActionWouldEnter variants ("as this enters").
var d2Enters = map[string]func(x any) []string{
	"EntersPrepared":                                   fixedEnter(tokPrepared),
	"BecomeDay":                                        fixedEnter(tokDay),
	"EntersWithLayerEffect":                            func(x any) []string { return layerEffects(list(rawArgs(x))) },
	"EntersWithLayerEffectUntil":                       func(x any) []string { return layerEffects(list(argN(x, 0))) },
	"EntersWithLayerEffectOfChoice":                    fixedEnter("choose"),
	"EntersWithPTOfChoice":                             fixedEnter("choose"),
	"SecretlyChooseAPlayer":                            fixedEnter("choose"),
	"SecretlyChooseANumberBetween":                     fixedEnter("choose"),
	"NoteTheMostPrevalentCreaturTypeInAPlayersLibrary": fixedEnter("choose"),
	"EntersWithNumberCountersForEach": func(x any) []string {
		return []string{"counters:" + counterType(argN(x, 1)) + ":*"}
	},
	// A counter of the player's choice: the type is not a literal.
	"EntersWithACounterOfChoice":                  fixedEnter("counters:*"),
	"EntersWithNumberDifferentCountersOfChoice":   fixedEnter("counters:*"),
	"EntersWithDuplicateCountersOfPermanent":      fixedEnter("counters:*"),
	"EntersWithNumberCombinationCountersOfChoice": fixedEnter("counters:*"),
	"RemoveCounters":                              fixedEnter("countersremove"),
	// An opponent reveals their hand as this enters.
	"RevealHand": fixedEnter("reveal"),
	"EntersWithAnAbilityCounterForEachAbilityOnACardDiscardedThisWay": fixedEnter("counters:*"),
	"PutCounters": func(x any) []string {
		var out []string
		for _, pc := range list(rawArgs(x)) {
			out = append(out, putCounters(pc))
		}
		return out
	},
	"EntersUnderAPlayersControl": func(x any) []string {
		if variantOf(rawArgs(x)) == "SinglePlayer" {
			return []string{"gaincontrol"}
		}
		// "Under the control of an opponent of your choice".
		return []string{"choose", "gaincontrol"}
	},
	"Exile": func(x any) []string {
		var out []string
		for _, ex := range list(argN(x, 0)) {
			out = append(out, "move:"+exilableZone(ex)+">exile")
		}
		return out
	},
	"ExileUptoNumberGraveyardCards":  fixedEnter("move:graveyard>exile"),
	"SacrificeAnyNumberOfPermanents": fixedEnter("sacrifice"),
	"SacrificeEachPermanent":         fixedEnter("sacrifice"),
	"EachPlayerAction":               func(x any) []string { return enterEffects([]any{argN(x, 1)}) },
	"APlayerAction":                  func(x any) []string { return enterEffects([]any{argN(x, 1)}) },
	"IfElsePassesFilter":             enterIfElse,
	"IfElse":                         enterIfElse,
	// The cost of "you may/must pay X. If you don't, ..." is paid, not done
	// (MayCost is skipped the same way); the two outcomes carry no verb.
	"MustCost":                fixedEnter(),
	"EntersNormally":          fixedEnter(),
	"PutIntoGraveyardInstead": fixedEnter(),
	// Bookkeeping for a later "the life total it had as it entered".
	"RememberLifeTotal":                fixedEnter(),
	"EntersAttachedToAPermanent":       fixedEnter("attach"),
	"DiscardHand":                      fixedEnter("discard:*"),
	"DiscardAnyNumberOfCardsOfType":    fixedEnter("discard:*"),
	"TurnEachPermanentFaceDown":        fixedEnter("setstate:turnfacedown"),
	"ExchangeTextBoxesOfTwoPermanents": fixedEnter("exchangetextbox"),
	"LoseLife":                         func(x any) []string { return []string{"loselife:" + gameNumber(rawArgs(x))} },
	"PayAnyAmountOfLife":               fixedEnter("paylife:*"),
	"PayAnyAmountOfLifeUpto":           fixedEnter("paylife:*"),
	"FlipACoin_OnHeadAndOnTails":       fixedEnter("coin"),
	"MillNumberCards":                  func(x any) []string { return []string{"mill:" + gameNumber(rawArgs(x))} },
	"MillNumberCardsForEach":           fixedEnter("mill:*"),
	"RollNumberD6":                     fixedEnter("dice"),
	"RollTwoD6":                        fixedEnter("dice"),
	"DraftACardFromSpellBook":          fixedEnter("draft"),
	"CreateFutureTrigger":              fixedEnter("delayed"),
	"GetAnEmblem": func(x any) []string {
		e := &effects{}
		if n, ok := x.(node); ok {
			e.action(n, "GetAnEmblem")
		}
		return e.effects
	},
	"ReflexiveEnters": func(x any) []string {
		// "When it enters this way, ...": the reflexive trigger's actions.
		e := &effects{}
		e.walk(rawArgs(x))
		return e.effects
	},
}

func fixedEnter(tok ...string) func(any) []string {
	return func(any) []string { return tok }
}

// enterIfElse merges the two branches of an as-enters if/else, as
// alternatives merge elsewhere.
func enterIfElse(x any) []string {
	a := argsOf(x)
	if len(a) < 3 {
		return nil
	}
	e := &effects{}
	e.merge([]*effects{{effects: enterEffects(list(a[1]))}, {effects: enterEffects(list(a[2]))}})
	return e.effects
}

// d2Layers maps LayerEffect variants.
var d2Layers = map[string]func(l any) []string{
	"AddAbilityFromAnExiledHasable":                                       hasableGrants,
	"AddAbilityFromAnExiledHasable_MayOnlyActivateOnecEachTurn":           hasableGrants,
	"AddAbilityFromEachPermanentHasable":                                  hasableGrants,
	"AddAbilityFromCardsInGraveyardsHasable":                              hasableGrants,
	"AddAbilityFromExiledHasable":                                         hasableGrants,
	"AddAbilityFromPermanentHasable":                                      hasableGrants,
	"AddAbilityFromTopOfLibraryHasable":                                   hasableGrants,
	"AddAbilityFromCardsInHandHasable":                                    hasableGrants,
	"AddAbilityFromCardsInPlayersGraveyardHasable":                        hasableGrants,
	"AddAbilityFromCardsRemovedFromDraftWithCardsNamedHasable":            hasableGrants,
	"AddAbilityFromGraveyardCardHasable":                                  hasableGrants,
	"AddActivatedAbilitiesAndMaySpendManaAsThoughItWasAnyColorToActivate": func(any) []string { return []string{"grant:*", "manaconvert"} },
	"AddActivatedAbilitiesAndMaySpendColorManaAsThoughAnyColorToActivate": func(any) []string { return []string{"grant:*", "manaconvert"} },
	"AddAbility_ActivatedWithModifiers":                                   func(any) []string { return []string{"grant:ability"} },
	"ReplaceColorWordVariableWithNewColorWordVariable":                    func(any) []string { return []string{tokChangeText} },
	"HasAllNamesOfNonlegendaryCreatures":                                  func(any) []string { return []string{"name"} },
	"HasTextOfGraveyardCardAndTheText":                                    func(any) []string { return []string{"copy"} },
	"AddAbilityAndLoseAllOtherAbilities": func(l any) []string {
		out := []string{"loseabilities"}
		for _, r := range list(rawArgs(l)) {
			if enumOf(r) == "Rule" {
				out = append(out, grantToken(r))
			}
		}
		return out
	},
}

// hasableGrants maps "has <quality> as long as <a card> has it" (Odric's,
// Cairn Wanderer's, the Cauldron's "all activated abilities of"): one grant
// per keyword quality, "grant:*" for the abilities a card happens to have.
func hasableGrants(l any) []string {
	var out []string
	for _, a := range argsOf(l) {
		for _, h := range list(a) {
			if enumOf(h) != "CheckHasable" {
				continue
			}
			v := variantOf(h)
			if strings.Contains(v, "Abilit") {
				out = append(out, "grant:*")
				continue
			}
			out = append(out, "grant:"+keywordWord(strings.TrimPrefix(v, "Any")))
		}
	}
	if len(out) == 0 {
		return []string{"grant:*"}
	}
	return out
}

// The gorge side of "as this enters".

// d2EnterAPIs map as-enters APIs that need the face's SVars.
var d2EnterAPIs = map[string]func(g *gorgeNorm, a *cards.SA) []string{
	"AlterAttribute": func(_ *gorgeNorm, a *cards.SA) []string {
		return []string{"alterattribute:" + strings.ToLower(a.Params["Attributes"])}
	},
	"DayTime": func(_ *gorgeNorm, a *cards.SA) []string {
		return []string{"daytime:" + strings.ToLower(a.Params["Value"])}
	},
	"Draft":           func(*gorgeNorm, *cards.SA) []string { return []string{"draft"} },
	"ExchangeTextBox": func(*gorgeNorm, *cards.SA) []string { return []string{"exchangetextbox"} },
	"GenericChoice":   func(*gorgeNorm, *cards.SA) []string { return []string{"choose"} },
	"Radiation": func(_ *gorgeNorm, a *cards.SA) []string {
		return []string{"counters:rad:" + literalOr(a.Params["Num"], "*")}
	},
	"StoreSVar": func(_ *gorgeNorm, a *cards.SA) []string {
		// "As this enters, pay any amount of life" is a StoreSVar whose
		// cost is the payment; any other StoreSVar is bookkeeping.
		if strings.Contains(a.Params["Cost"], "PayLife") {
			return []string{"paylife:*"}
		}
		return nil
	},
	"ChangeZone": func(g *gorgeNorm, a *cards.SA) []string {
		p := a.Params
		self := p["Defined"] == "ReplacedCard" || p["Defined"] == "Self"
		switch {
		case self && p["GainControl"] != "":
			return []string{"gaincontrol"}
		case self:
			// "If you can't, put it into its owner's graveyard instead".
			return nil
		}
		return gorgeAPIEffects(a, g.svars())
	},
	"ImmediateTrigger": func(g *gorgeNorm, a *cards.SA) []string {
		// "When it enters this way, ...": the reflexive trigger's effects.
		e := &effects{}
		g.chain(e, cards.ResolveSVar(g.svars(), a.Params["Execute"]), 1)
		return e.effects
	},
}

// d2EnterDelegated are as-enters APIs that are ordinary actions, mapped as
// the action mapper maps them.
var d2EnterDelegated = map[string]bool{
	"Sacrifice": true, "SacrificeAll": true, "Discard": true, "Mill": true, "LoseLife": true,
	"RollDice": true, "FlipCoin": true, "ChangeZoneAll": true, "RemoveCounter": true, "Attach": true,
	"Effect": true, "DelayedTrigger": true, "SetState": true, "Animate": true, "Reveal": true,
	"ReplaceEffect": true,
}

// d2EnterEffects maps one as-enters API; ok is false when this unit has no
// mapping for it.
func (g *gorgeNorm) d2EnterEffects(a *cards.SA) (out []string, ok bool) {
	if f, ok := d2EnterAPIs[a.API]; ok {
		return f(g, a), true
	}
	if d2EnterDelegated[a.API] {
		return gorgeAPIEffects(a, g.svars()), true
	}
	return nil, false
}

// d2InsteadEnter reports whether a self Moved->Battlefield replacement is
// not an "as this enters" but replaces the entering ("If this would enter,
// sacrifice two lands instead. If you do, put it onto the battlefield"),
// or makes it impossible: mtgish's ReplaceWouldEnter, compared by the
// event like every other replacement.
func (g *gorgeNorm) d2InsteadEnter(r *cards.Repl) bool {
	if r.Params["Prevent"] == "True" || r.Params["Layer"] == "CantHappen" {
		return true
	}
	with := r.With
	if with == nil {
		with = cards.ResolveSVar(g.svars(), r.Params["ReplaceWith"])
	}
	for a := with; a != nil; a = a.Sub {
		p := a.Params
		if a.API == "ChangeZone" && p["Destination"] == "Battlefield" && p["Defined"] == "ReplacedCard" && p["GainControl"] == "" {
			return true
		}
	}
	return false
}

// ruleToken is the token of a PermanentRule variant: "rule:<variant>",
// except for the variants that are one Forge static mode written several
// ways by mtgish (which player or permanent a creature can't attack, which
// creatures a creature can't block), whose Forge mode cannot tell the
// variants apart: those take the family's token, the one the gorge side
// emits for the mode.
func ruleToken(v string) string {
	if t, ok := d2RuleSynonyms[v]; ok {
		return t
	}
	return "rule:" + ruleWord(v)
}

var d2RuleSynonyms = map[string]string{
	// S:Mode$ CantAttack (with ValidDefender$/UnlessDefender$ ...).
	"CantAttackPlayer": "rule:cantattack", "CantAttackAPlayer": "rule:cantattack",
	"CantAttackAPermanent": "rule:cantattack", "CantAttackPlayerOrPlaneswalkerControlledBy": "rule:cantattack",
	"CantAttackAnyPlayerOrPlaneswalkerControlledBy": "rule:cantattack",
	"CantAttackIfDefendingPlayer":                   "rule:cantattack", "CantAttackUnlessDefendingPlayer": "rule:cantattack",
	"CantAttackPlayerUnlessCost": "rule:cantattackunlesscost", "CantAttackAPermanentUnlessCost": "rule:cantattackunlesscost",
	"CantAttackPlayerOrPlaneswalkerControlledByUnlessCost": "rule:cantattackunlesscost",
	// S:Mode$ MustAttack (with MustAttack$ <defender>).
	"MustAttackPlayer": "rule:mustattack", "MustAttackAPlayer": "rule:mustattack", "MustAttackPlaneswalker": "rule:mustattack",
	// S:Mode$ CantBeActivated (with ValidSA$ ...).
	"AbilitiesCantBeActivated": "rule:cantbeactivated", "AbilitiesOfTypeCantBeActivated": "rule:cantbeactivated",
	// S:Mode$ CantBlockBy with ValidBlocker$: which creatures can block
	// which, from either side.
	"CantBeBlockedExceptByDefenders": "rule:cantbeblockedbydefenders", "CanBlockOnly": "rule:cantbeblockedbydefenders",
	"CantBlockAttacker": "rule:cantbeblockedbydefenders", "CantBlockAttackers": "rule:cantbeblockedbydefenders",
	// S:Mode$ CanBlockIfShadow / IgnoreLandWalk: one rule, two spellings.
	"CanBlockCreaturesWithShadowAsThoughTheyDidntHaveShadow":          "rule:canblockcreatureswithshadowasthoughithadshadow",
	"CanBlockCreaturesWithLandwalkAbilitiesAsThoughTheyDidntHaveThem": "rule:canbeblockedasthoughitdidnthave",
	// "Its activated abilities cost {N} more": S:Mode$ RaiseCost.
	"IncreaseManaCost": "costincrease",
	// "Is goaded": the goad action's result, written as a rule.
	"IsGoadedByPlayer": "goad",
}

// d2PlayerToken maps the PlayerEffect variants this unit folds or names
// exactly; ok is false for the rest (playerEffectToken's own rules).
func d2PlayerToken(v string) (string, bool) {
	if t, ok := d2PlayerSynonyms[v]; ok {
		return t, true
	}
	switch {
	case (strings.HasPrefix(v, "Decrease") || strings.HasPrefix(v, "Reduce")) && strings.Contains(v, "Cost"):
		// Every "<abilities> cost {N} less" is S:Mode$ ReduceCost.
		return "costreduction", true
	case strings.HasPrefix(v, "Increase") && strings.Contains(v, "Cost"):
		return "costincrease", true
	}
	return "", false
}

// d2MayLookAt maps a Continuous MayLookAt$ by the zone it opens and to whom:
// "you may look at the top card of your library", "play with the top card
// revealed" (every player may look), a revealed hand, face-down exiled
// cards or permanents.
func d2MayLookAt(p map[string]string) string {
	switch p["AffectedZone"] {
	case "Hand":
		return "player:playswithhandrevealed"
	case "Exile":
		return "player:maylookatfacedownexiledcards"
	case "Battlefield":
		return "player:maylookatfacedownpermanents"
	}
	if p["MayLookAt"] == "Player" {
		return "player:playswithtopoflibraryrevealed"
	}
	return "player:maylookattopcardoflibraryanytime"
}

// d2PlayerSynonyms are PlayerEffect variants that are a gorge static of
// another family: "creatures can't attack you" is S:Mode$ CantAttack.
var d2PlayerSynonyms = map[string]string{
	"CantAttackPlayer": "rule:cantattack", "CantAttackPlayerOrPlaneswalkerControlledBy": "rule:cantattack",
	"CantAttackAPermanent": "rule:cantattack", "CantAttackWithCreatures": "rule:cantattack",
	// S:Mode$ CantBeCast with Origin$ <zone>: one mode, the zone a filter.
	"CantCastSpellsFromTheirHand": "player:cantcastspells", "CantCastSpellsFromGraveyards": "player:cantcastspells",
	"CantCastSpellsFromLibraries": "player:cantcastspells", "CantCastSpellsFromExile": "player:cantcastspells",
	"MayLookAtFaceDownExiledCard": "player:maylookatfacedownexiledcards",
	// "You don't lose the game for having 0 or less life": an R:GameLoss
	// replacement in Forge, the same token as "can't lose the game".
	"DoesntLoseTheGameForHaving": "player:cantlosethegame",
	// Unit B's spelling for the gorge side.
	"CantSearchLibraries": "player:cantsearchlibrary",
	// Not a permission to cast (playerEffectToken's May...Cast rule).
	"MayDiscardCardAnyTimeTheyCouldCastAnInstant": "player:maydiscardcardanytimetheycouldcastaninstant",
}
