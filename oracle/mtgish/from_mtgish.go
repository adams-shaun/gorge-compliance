package mtgish

import (
	"strconv"
	"strings"
)

// FromMtgish summarises every face of an mtgish card.
func FromMtgish(c *Card) []*Summary {
	var out []*Summary
	for _, f := range c.Faces {
		out = append(out, mtgishFace(c, f))
	}
	return out
}

func mtgishFace(c *Card, f node) *Summary {
	s := &Summary{Card: c.Name}
	s.Face, _ = f["Name"].(string)

	cost := f["ManaCost"]
	if cost == nil {
		cost = c.Shared["ManaCost"]
	}
	var mc manaCost
	for _, sym := range list(cost) {
		mc.add(mtgishManaSymbol(sym))
	}
	s.ManaCost = mc.String()

	tl := f["Typeline"]
	if tl == nil {
		tl = c.Shared["Typeline"]
	}
	for _, k := range []string{"Supertypes", "Cardtypes", "Subtypes"} {
		for _, w := range list(field(tl, k)) {
			if ws, ok := w.(string); ok {
				s.Types = append(s.Types, typeWord(ws))
			}
		}
	}

	m := &mtgishNorm{s: s}
	m.rules(list(f["Rules"]))

	if pt, ok := f["CardPT"].(map[string]any); ok {
		p, t := strconv.Itoa(intOr(pt["Power"])), strconv.Itoa(intOr(pt["Toughness"]))
		if m.cdaPower {
			p = "*"
		}
		if m.cdaToughness {
			t = "*"
		}
		s.PT = p + "/" + t
	}
	if l := f["Loyalty"]; l != nil {
		if variantOf(l) == "LoyaltyX" {
			s.Loyalty = "X"
		} else {
			s.Loyalty = gameNumber(l)
		}
	}
	return s
}

func intOr(x any) int {
	n, _ := intOf(x)
	return n
}

// mtgishManaSymbol maps a ManaSymbol node to the canonical symbol.
func mtgishManaSymbol(x any) string {
	v := variantOf(x)
	if v == "ManaCostGeneric" {
		return gameNumber(rawArgs(x))
	}
	s := strings.TrimPrefix(v, "ManaCost")
	switch {
	case len(s) == 1:
		return s
	case len(s) == 2 && s[1] == 'P':
		return s[:1] + "/P"
	case len(s) == 3 && s[2] == 'P': // hybrid phyrexian RWP
		return s[:1] + "/" + s[1:2] + "/P"
	case len(s) == 2:
		return s[:1] + "/" + s[1:]
	}
	return "?" + s
}

// mtgishNorm accumulates one face's summary from its Rules.
type mtgishNorm struct {
	s                      *Summary
	cdaPower, cdaToughness bool
	cdaSeen                bool
}

func (m *mtgishNorm) kw(tok string) { m.s.Keywords = append(m.s.Keywords, tok) }

func (m *mtgishNorm) ability(a Ability) { m.s.Abilities = append(m.s.Abilities, a) }

// sub maps a nested rule; conditional (under If/Unless or a level) turns
// its keywords into a static granting them.
func (m *mtgishNorm) sub(r any, conditional bool) {
	if !conditional {
		m.rule(r)
		return
	}
	n := &mtgishNorm{s: &Summary{}}
	n.rule(r)
	m.cdaPower = m.cdaPower || n.cdaPower
	m.cdaToughness = m.cdaToughness || n.cdaToughness
	m.s.Abilities = append(m.s.Abilities, n.s.Abilities...)
	if len(n.s.Keywords) > 0 {
		var eff []string
		for _, k := range n.s.Keywords {
			eff = append(eff, "grant:"+keywordHeadToken(k))
		}
		m.ability(Ability{Kind: "static", Effects: eff})
	}
}

// rules maps a Vec<Rule>.
func (m *mtgishNorm) rules(rs []any) {
	for _, r := range mergeThisOrAnother(rs) {
		m.rule(r)
	}
}

// simpleKeywords are the parameterless keyword Rule variants, by their
// canonical token.
var simpleKeywords = map[string]string{
	"Flying": "flying", "FirstStrike": "firststrike", "DoubleStrike": "doublestrike",
	"Trample": "trample", "Vigilance": "vigilance", "Reach": "reach", "Haste": "haste",
	"Flash": "flash", "Menace": "menace", "Deathtouch": "deathtouch", "Lifelink": "lifelink",
	"Indestructible": "indestructible", "Hexproof": "hexproof", "Defender": "defender",
	"Prowess": "prowess", "Shroud": "shroud", "Fear": "fear", "Intimidate": "intimidate",
	"Infect": "infect", "Wither": "wither", "Convoke": "convoke", "Delve": "delve",
	"Cascade": "cascade", "Storm": "storm", "Exalted": "exalted", "Persist": "persist",
	"Undying": "undying", "Evolve": "evolve", "Flanking": "flanking", "Shadow": "shadow",
	"Horsemanship": "horsemanship", "Skulk": "skulk", "Changeling": "changeling",
	"SplitSecond": "splitsecond", "Improvise": "improvise", "Riot": "riot",
	"Mentor": "mentor", "Training": "training", "Decayed": "decayed", "Ravenous": "ravenous",
	"Bargain": "bargain", "Myriad": "myriad", "Melee": "melee", "Extort": "extort",
	"Exploit": "exploit", "Rebound": "rebound", "Retrace": "retrace", "Phasing": "phasing",
	"Banding": "banding", "Ascend": "ascend", "Assist": "assist", "Dethrone": "dethrone",
	"Enlist": "enlist", "Unleash": "unleash", "Provoke": "provoke",
	"Soulbond": "soulbond", "Sunburst": "sunburst", "Haunt": "haunt", "Fuse": "fuse",
	"Aftermath": "aftermath", "JumpStart": "jumpstart", "Conspire": "conspire",
	"BattleCry": "battlecry", "LivingWeapon": "livingweapon", "Cipher": "cipher",
	"Demonstrate": "demonstrate", "Daybound": "daybound", "Nightbound": "nightbound",
	"Compleated": "compleated", "ReadAhead": "readahead", "ForMirrodin": "formirrodin",
	"Gravestorm": "gravestorm", "Ingest": "ingest", "Undaunted": "undaunted",
	"UmbraArmor": "umbraarmor", "Vanishing": "vanishing", "Increment": "increment",
	"DoubleTeam": "doubleteam", "LivingMetal": "livingmetal", "StartYourEngines": "startyourengines",
	"JobSelect": "jobselect", "Storied": "storied", "Station": "station", "BasicSuspend": "suspend",
	"Epic": "epic", "Paradigm": "paradigm", "BasicMayhem": "mayhem", "HiddenAgenda": "hiddenagenda",
	"DoubleAgenda": "doubleagenda", "Bloodthirst": "bloodthirst", "BloodthirstX": "bloodthirst",
}

// costKeywords are the keyword Rule variants with a Cost argument.
var costKeywords = map[string]string{
	"Ward": "ward", "Flashback": "flashback", "Kicker": "kicker", "Offspring": "offspring",
	"Cycling": "cycling", "Equip": "equip", "Madness": "madness", "Morph": "morph",
	"Megamorph": "megamorph", "Disguise": "disguise", "Dash": "dash", "Evoke": "evoke",
	"Escape": "escape", "Embalm": "embalm", "Eternalize": "eternalize", "Unearth": "unearth",
	"Bestow": "bestow", "Blitz": "blitz", "Buyback": "buyback", "Echo": "echo",
	"Emerge": "emerge", "Encore": "encore", "Foretell": "foretell", "Miracle": "miracle",
	"Multikicker": "multikicker", "Mutate": "mutate", "Ninjutsu": "ninjutsu",
	"Plot": "plot", "Prowl": "prowl", "Reconfigure": "reconfigure", "Scavenge": "scavenge",
	"Spectacle": "spectacle", "Squad": "squad", "Surge": "surge", "Transmute": "transmute",
	"Outlast": "outlast", "Fortify": "fortify", "Disturb": "disturb", "Freerunning": "freerunning",
	"Impending": "impending", "Warp": "warp", "Sneak": "sneak", "Harmonize": "harmonize",
	"Mayhem": "mayhem", "Craft": "craft", "Saddle": "saddle", "CumulativeUpkeep": "cumulativeupkeep",
	"Replicate": "replicate",
}

// bareKeywords are keyword Rule variants compared by name only: their
// parameters (a land type, a count, a cost) are too varied to normalise
// cheaply, so both sides drop them.
var bareKeywords = map[string]string{
	"Landwalk": "landwalk", "TypeCycling": "typecycling", "Suspend": "suspend", "SuspendX": "suspend",
	"Vanishing": "vanishing", "VanishingEnters": "vanishing", "StationCharged": "station",
	"StationChargedAnimate": "station", "Prototype": "prototype", "SpliceOnto": "splice",
	"Champion": "champion", "Reinforce": "reinforce", "ReinforceX": "reinforce",
	"WebSlinging": "webslinging", "Recover": "recover", "Offering": "offering",
	"Impending": "impending", "Ripple": "ripple", "Specialize": "specialize",
	"SpecializeWithModifiers": "specialize", "SpecializeFromGraveyard": "specialize",
	"MoreThanMeetsTheEye": "morethanmeetstheeye", "StartingIntensity": "startingintensity",
	"Haunt": "haunt", "Firebending": "firebending", "Devour": "devour", "Mobilize": "mobilize",
	"CraftWithCraftables": "craft", "CraftWithACraftable": "craft", "CraftWithANumberOfCraftables": "craft",
	"CraftWithANumberOfGroupCraftables": "craft", "Visit": "visit", "VisitAndPrize": "visit",
	"Bloodthirst": "bloodthirst",
}

// numberKeywords are the keyword Rule variants with an i32/GameNumber.
var numberKeywords = map[string]string{
	"Crew": "crew", "Annihilator": "annihilator", "Afflict": "afflict", "Fabricate": "fabricate",
	"Toxic": "toxic", "Modular": "modular", "Renown": "renown", "Bushido": "bushido",
	"Rampage": "rampage", "Poisonous": "poisonous", "Dredge": "dredge", "Fading": "fading",
	"Frenzy": "frenzy", "Graft": "graft", "Absorb": "absorb", "Amplify": "amplify",
	"Tribute": "tribute", "Bloodthirst": "bloodthirst", "Soulshift": "soulshift",
	"Casualty": "casualty", "Mobilize": "mobilize", "Hideaway": "hideaway",
	"Backup": "backup", "Afterlife": "afterlife",
}

// staticRules are the top-level Rule variants that are static abilities.
var staticRules = map[string]bool{
	"PermanentLayerEffect": true, "EachPermanentLayerEffect": true, "EachPermanentStickyLayerEffect": true,
	"PermanentRuleEffect": true, "EachPermanentRuleEffect": true, "PermanentRuleEffect_PlayerMayPayToIgnoreEffectUntil": true,
	"PlayerEffect": true, "EachPlayerEffect": true, "PlayerEffect_PlayerMayPayToIgnoreEffectUntil": true,
	"CastEffect": true, "ThisSpellEffect": true, "StackSpellsEffect": true, "StackEffect": true,
	"ActivatedAbilityEffect": true, "CardEffect": true, "SelfEffect": true, "SelfEffect_NonBattlefield": true,
	"EachCardInPlayersLibraryEffect": true, "EachCardInPlayersHandEffect": true,
	"EachCardInEachPlayersHandEffect": true, "EachPermanentAndSpellEffect": true,
	"EachCardInGraveyardEffect": true, "EachCardInAGraveyardEffect": true,
	"TopCardOfPlayersLibraryEffect": true, "DamageCantBePrevented": true,
	"CombatDamageCantBePrevented": true, "TheLegendsRuleDoesntApply": true,
}

// wrapperRules hold one Rule (zone gates) or a Vec<Rule> (conditions,
// class levels); their contents are summarised as if printed directly.
var wrapperRules = map[string]bool{
	"FromExileOrBattlefield": true, "FromExile": true, "FromExileIf": true, "FromStack": true,
	"FromStackIf": true, "FromGraveyardOrBattlefield": true, "FromGraveyard": true,
	"FromGraveyardIf": true, "FromHand": true, "FromAnyZone": true, "FromCommandZone": true,
	"FromCommandZoneOrBattlefield": true, "If": true, "Unless": true, "IfElse": true, "MaxSpeed": true,
	"FromTopOfLibrary_Digital": true,
}

func (m *mtgishNorm) rule(r any) {
	v := variantOf(r)
	if tok, ok := simpleKeywords[v]; ok && rawArgs(r) == nil {
		m.kw(tok)
		return
	}
	if tok, ok := costKeywords[v]; ok {
		cost := argN(r, 0)
		if v == "Equip" {
			cost = argN(r, 1)
		}
		m.kw(tok + ":" + mtgishCost(cost))
		return
	}
	if tok, ok := bareKeywords[v]; ok {
		m.kw(tok)
		switch v {
		case "StationCharged", "StationChargedAnimate":
			m.rules(list(argN(r, 1)))
		case "Visit", "VisitAndPrize":
			e := &effects{}
			e.walk(argsOf(r))
			m.ability(Ability{Kind: "triggered", Event: "visit", Targets: e.targets, Effects: e.effects})
		}
		return
	}
	if tok, ok := numberKeywords[v]; ok {
		// Backup's nested rules are the abilities it grants, which the
		// card also prints (and mtgish also encodes) at top level: they are
		// not summarised twice.
		m.kw(tok + ":" + gameNumber(argN(r, 0)))
		return
	}
	switch {
	case wrapperRules[v]:
		// Every Rule or Vec<Rule> argument; conditions are skipped by shape.
		// Under a condition ("as long as ..., it has flying") a keyword is
		// a static granting it, as scripts write it.
		cond := v == "If" || v == "Unless" || v == "IfElse" || v == "MaxSpeed"
		for _, a := range argsOf(r) {
			if enumOf(a) == "Rule" {
				m.sub(a, cond)
			} else if l := list(a); l != nil {
				for _, e := range l {
					if enumOf(e) == "Rule" {
						m.sub(e, cond)
					}
				}
			}
		}
		return
	case v == "PermanentRuleEffect" || v == "EachPermanentRuleEffect":
		// One static per rule: scripts write "can't attack or block" as
		// two statics, one per restriction mode.
		for _, x := range list(argN(r, 1)) {
			m.ability(Ability{Kind: "static", Effects: []string{"rule:" + ruleWord(variantOf(x))}})
		}
		return
	case v == "CastEffect" && strings.HasPrefix(variantOf(rawArgs(r)), "AdditionalCastingCost"):
		// An additional casting cost is a cost, not an ability (scripts put
		// it in the spell's Cost$).
		return
	case staticRules[v]:
		m.ability(Ability{Kind: "static", Effects: staticEffects(r)})
		return
	case v == "PreventDamage":
		// "Prevent all combat damage dealt to and dealt by" is one shield
		// per event in scripts; split an Or the same way.
		n := 1
		if ev := argN(r, 0); variantOf(ev) == "Or" {
			n = len(list(rawArgs(ev)))
		}
		for i := 0; i < n; i++ {
			m.ability(Ability{Kind: "replacement", Effects: []string{"replace:preventdamage"}})
		}
		return
	case v == "AsPutIntoAGraveyardFromAnywhere":
		m.ability(Ability{Kind: "replacement", Effects: []string{"replace:wouldputintograveyard"}})
		return
	case strings.HasPrefix(v, "Trigger"):
		m.trigger(r)
		return
	case strings.HasPrefix(v, "Replace") || v == "PreventDamage" || v == "AsPutIntoAGraveyardFromAnywhere":
		m.ability(Ability{Kind: "replacement", Effects: replacementEffects(r)})
		return
	case strings.HasPrefix(v, "SpellActions"):
		m.spell(r)
		return
	}
	switch v {
	case "Activated", "ActivatedWithModifiers":
		a := Ability{Kind: "activated"}
		e := &effects{}
		// The cost (argument 0) is paid, not done: scripts keep it in Cost$.
		e.walk(argN(r, 1))
		a.Targets, a.Effects = e.targets, e.effects
		m.ability(a)
	case "AsPermanentEnters", "AsPermanentEntersOrIsTurnedFaceUp", "AsPermanentEscapes":
		m.ability(Ability{Kind: "replacement", Effects: enterEffects(list(argN(r, 1)))})
	case "ClassAbilities":
		for _, ca := range list(rawArgs(r)) {
			m.rules(list(argN(ca, 1)))
		}
	case "SagaChapters":
		// One chapter ability per chapter number, as scripts list them.
		for _, ch := range list(rawArgs(r)) {
			e := &effects{}
			e.walk(argN(ch, 1))
			for range list(argN(ch, 0)) {
				m.ability(Ability{Kind: "triggered", Event: "chapter", Targets: e.targets, Effects: e.effects})
			}
		}
	case "LevelUp":
		m.kw("levelup")
		for _, l := range list(argN(r, 1)) {
			for _, x := range list(argN(l, 2)) {
				m.sub(x, true)
			}
		}
	case "CDA_Color", "CDA_ColorButNotColorIdentity":
		if strings.Contains(variantOf(rawArgs(r)), "Colorless") {
			m.kw("devoid")
		} else {
			m.ability(Ability{Kind: "static", Effects: []string{"cda"}})
		}
	case "CDA_Power", "CDA_Toughness":
		if v == "CDA_Power" {
			m.cdaPower = true
		} else {
			m.cdaToughness = true
		}
		if !m.cdaSeen {
			m.cdaSeen = true
			m.ability(Ability{Kind: "static", Effects: []string{"cda"}})
		}
	case "CDA_Types":
		if cv := variantOf(rawArgs(r)); cv == "Changeling" || strings.Contains(cv, "AllCreatureTypes") {
			m.kw("changeling")
		} else {
			// "Is the chosen type in addition to its other types": a
			// type-changing static.
			m.ability(Ability{Kind: "static", Effects: []string{"type"}})
		}
	case "EnchantPermanent":
		m.kw("enchant:" + targetToken(permanentsClasses(rawArgs(r)), "1"))
	case "EnchantPlayer":
		m.kw("enchant:player")
	case "Protection":
		for _, q := range strings.Split(protectable(rawArgs(r)), "|") {
			m.kw("protection:" + q)
		}
	case "HexproofFrom":
		for _, q := range strings.Split(protectable(rawArgs(r)), "|") {
			m.kw("hexprooffrom:" + q)
		}
	case "Affinity":
		m.kw("affinity:" + affinityWord(rawArgs(r)))
	case "Permanent_Gift":
		m.kw("gift")
	case "KickerX", "KickerXWithModifiers":
		m.kw("kicker:*")
	case "FlashbackX", "FlashbackWithModifier":
		m.kw("flashback:*")
	case "EquipWithModifiers":
		m.kw("equip:" + mtgishCost(argN(r, 1)))
	case "DeckConstruction", "DeckConstructionIfCommander":
		// A deck-building rule is not an ability of the card in play.
	case "IfCardIsInOpeningHand", "AsGameBegins", "BeforeDrawingOpeningHand":
		m.ability(Ability{Kind: "pregame"})
	default:
		if f, ok := extRule[v]; ok {
			f(m, r)
			return
		}
		m.ability(Ability{Kind: unmapped("Rule." + v)})
	}
}

// spell maps the SpellActions family: one spell ability, plus the keyword a
// Kicker/Gift spell's text also carries.
func (m *mtgishNorm) spell(r any) {
	v := variantOf(r)
	e := &effects{}
	switch v {
	case "SpellActions":
		e.walk(rawArgs(r))
	case "SpellActions_Kicker":
		m.kw("kicker:" + mtgishCost(argN(r, 0)))
		e.alternatives(argsOf(r)[1:]...)
	case "SpellActions_Gift":
		m.kw("gift")
		e.alternatives(argsOf(r)[1:]...)
	case "SpellActions_Awaken":
		m.kw("awaken")
		e.alternatives(awakenPlainBranch(argsOf(r)[1:])...)
	case "SpellActions_Overload", "SpellActions_Cleave", "SpellActions_Teamwork":
		m.kw(strings.ToLower(strings.TrimPrefix(v, "SpellActions_")))
		e.alternatives(argsOf(r)[1:]...)
	case "SpellActions_MadnessX":
		// Cast for its madness cost (argument 0) or not: the keyword, and
		// the two versions of the spell as alternatives.
		m.kw("madness:" + mtgishCost(argN(r, 0)))
		e.alternatives(argsOf(r)[1:]...)
	case "SpellActions_AdditionalCostOptions":
		// Which additional cost was paid: alternatives.
		e.alternatives(list(rawArgs(r))...)
	case "SpellActions_Tiered", "SpellActions_Spree":
		m.kw(strings.ToLower(strings.TrimPrefix(v, "SpellActions_")))
		var alts []any
		for _, x := range list(rawArgs(r)) {
			alts = append(alts, argN(x, 1))
		}
		e.alternatives(alts...)
	default:
		e.walk(argsOf(r))
		e.effects = append(e.effects, unmapped("Rule."+v))
	}
	// Modal keywords live on the modal node inside the actions.
	walk(rawArgs(r), func(n node, enum, variant string) bool {
		switch {
		case strings.HasSuffix(variant, "_Entwine"):
			m.kw("entwine")
		case strings.HasSuffix(variant, "_Escalate"):
			m.kw("escalate")
		}
		return enum != "_Rule"
	})
	m.ability(Ability{Kind: "spell", Targets: e.targets, Effects: e.effects})
}

// trigger maps the Trigger* family. An Or trigger ("enters or attacks")
// becomes one triggered ability per event, the way scripts write it.
func (m *mtgishNorm) trigger(r any) {
	v := variantOf(r)
	args := argsOf(r)
	if v == "TriggerModalA" {
		for _, ta := range list(rawArgs(r)) {
			e := &effects{}
			e.walk(argN(ta, 1))
			for _, ev := range dedup(triggerEvents(argN(ta, 0))) {
				m.ability(Ability{Kind: "triggered", Event: ev, Targets: e.targets, Effects: e.effects})
			}
		}
		return
	}
	if len(args) < 2 {
		m.ability(Ability{Kind: unmapped("Rule." + v)})
		return
	}
	e := &effects{}
	e.walk(args[len(args)-1])
	for _, ev := range dedup(triggerEvents(args[0])) {
		m.ability(Ability{Kind: "triggered", Event: ev, Targets: append([]string(nil), e.targets...), Effects: append([]string(nil), e.effects...)})
	}
}

// triggerEvents classifies a Trigger node into event classes (several for
// an Or).
func triggerEvents(t any) []string {
	v := variantOf(t)
	switch v {
	case "Or":
		if ev, ok := playerOrBattleEvents(list(rawArgs(t))); ok {
			return []string{ev}
		}
		var out []string
		for _, sub := range list(rawArgs(t)) {
			out = append(out, triggerEvents(sub)...)
		}
		return out
	case "If", "Unless":
		for _, a := range argsOf(t) {
			if enumOf(a) == "Trigger" {
				return triggerEvents(a)
			}
		}
	}
	return []string{triggerClass(v)}
}

// triggerClass maps a Trigger variant name to its event class by the name's
// words; the order of the checks matters (specific before general).
func triggerClass(v string) string {
	has := func(s ...string) bool {
		for _, x := range s {
			if strings.Contains(v, x) {
				return true
			}
		}
		return false
	}
	switch {
	case has("Upkeep"):
		return "upkeep"
	case has("EndStep"):
		return "end-step"
	case has("BeginningOfCombat"):
		return "combat"
	case has("DrawStep"):
		return "draw-step"
	case has("MainPhase"):
		return "main"
	case has("ClassBecomesLevel"):
		return "class-level"
	case has("BecomesTheTarget"):
		return "becomes-target"
	case has("CombatDamageToAPlayer", "CombatDamageToAnyNumberOfPlayers"), v == "WhenAnyNumberOfPlayersAreDealtCombatDamage":
		return "combat-damage-player"
	case has("CombatDamage") && !has("Noncombat"):
		return "combat-damage"
	case has("Damage"):
		return "damage"
	case has("Dies"):
		return "dies"
	case has("LeavesTheBattlefield", "LeaveTheBattlefield"):
		return "ltb"
	case has("EntersTheBattlefield", "EnterTheBattlefield"):
		return "etb"
	case v == "WhenAPermanentIsPutIntoAPlayersGraveyard":
		// A permanent put into a graveyard is a permanent dying.
		return "dies"
	case has("PutIntoAPlayersGraveyard", "PutIntoAGraveyard"):
		return "to-graveyard"
	case has("Attacks", "Attack"):
		return "attacks"
	case has("Blocks"):
		return "blocks"
	case has("BecomesBlocked"):
		return "blocked"
	case has("Cast", "Copies"):
		return "cast"
	case has("GainsLife", "GainLife"):
		return "lifegain"
	case has("LosesLife", "LoseLife", "LoseAnAmountOfLife"):
		return "lifeloss"
	case has("Expends"):
		return "expend"
	case has("Draws"):
		return "draw"
	case has("Sacrifices"):
		return "sacrifice"
	case has("Discards"):
		return "discard"
	case has("HasNumberCountersOfType"):
		return "state"
	case has("Counters"):
		return "counters"
	case has("Forages"):
		return "forage"
	case has("GivesAGift"):
		return "gift"
	case has("BecomesUntapped"):
		return "untap"
	case has("BecomesTapped", "TapsAPermanentForMana", "TappedForMana"):
		return "tap"
	}
	if c, ok := extTriggerClass[v]; ok {
		return c
	}
	return unmapped("Trigger." + v)
}

// mtgishCost renders a keyword's cost: "{1}{W}" for a mana cost, "life:2"
// for a life payment, "*" for anything else (it then matches any cost).
func mtgishCost(c any) string {
	switch variantOf(c) {
	case "PayMana":
		var mc manaCost
		for _, sym := range list(rawArgs(c)) {
			mc.add(mtgishManaSymbol(sym))
		}
		if s := mc.String(); s != "" {
			return s
		}
	case "PayLife":
		return "life:" + gameNumber(rawArgs(c))
	}
	if variantOf(c) == "" {
		// A bare ManaCost (Vec<ManaSymbol>), as Prowl and Surge carry.
		if l := list(c); l != nil {
			var mc manaCost
			for _, sym := range l {
				mc.add(mtgishManaSymbol(sym))
			}
			if s := mc.String(); s != "" {
				return s
			}
		}
	}
	return "*"
}

// protectable renders a Protection/HexproofFrom quality.
func protectable(p any) string {
	switch variantOf(p) {
	case "FromEverything":
		return "everything"
	case "FromColor":
		pc := rawArgs(p)
		if variantOf(pc) == "Colors" {
			var cs []string
			for _, c := range list(rawArgs(pc)) {
				cs = append(cs, strings.ToLower(variantOf(c)))
			}
			return strings.Join(dedup(cs), "|")
		}
		return strings.ToLower(strings.TrimPrefix(variantOf(pc), "Protection"))
	case "FromTypes", "FromSpells", "FromPermanents":
		var words []string
		walk(rawArgs(p), func(n node, enum, variant string) bool {
			if strings.HasSuffix(variant, "IsCardtype") || variant == "IsCardtype" {
				if s, ok := n["args"].(string); ok {
					words = append(words, typeWord(s))
				}
			}
			return true
		})
		if len(words) > 0 {
			return strings.Join(dedup(words), "|")
		}
	}
	return "*"
}

// affinityWord names what an Affinity counts: a creature/artifact subtype
// or card type.
func affinityWord(p any) string {
	var words []string
	walk(p, func(n node, enum, variant string) bool {
		switch variant {
		case "IsCreatureType", "IsArtifactType", "IsLandType", "IsCardtype", "IsEnchantmentType":
			if s, ok := n["args"].(string); ok {
				words = append(words, typeWord(s))
			}
		}
		return true
	})
	if len(words) == 0 {
		return "*"
	}
	return strings.Join(dedup(words), "|")
}

// permanentsClasses extracts the target classes a Permanents filter
// selects: the card types it names positively, Or as alternatives and And
// as an intersection ("artifact&creature"), or "permanent" when none is.
func permanentsClasses(p any) []string {
	out := classesOfAlts(typeAlts(p))
	if len(out) == 0 {
		return []string{clsPermanent}
	}
	return out
}
