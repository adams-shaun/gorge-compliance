package mtgish

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// FromGorge summarises every face of a compiled gorge card. name is the
// card's lookup name (the summary's Card field).
func FromGorge(name string, c *cards.Card, idx TypeIndex) []*Summary {
	var out []*Summary
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		out = append(out, gorgeFace(name, f, idx))
	}
	return out
}

func gorgeFace(card string, f *cards.Face, idx TypeIndex) *Summary {
	s := &Summary{Card: card, Face: f.Name}
	var mc manaCost
	if f.ManaCost != "no cost" {
		for _, sym := range strings.Fields(f.ManaCost) {
			mc.add(gorgeManaSymbol(sym))
		}
	}
	s.ManaCost = mc.String()
	for _, t := range f.Types {
		s.Types = append(s.Types, typeWord(t))
	}
	s.PT = f.PT
	if strings.Contains(s.PT, "*") {
		// "1+*/1+*": a characteristic-defining P/T is "*" whatever its
		// printed base; mtgish prints every CDA half as "*".
		p, t, _ := strings.Cut(s.PT, "/")
		if strings.Contains(p, "*") {
			p = "*"
		}
		if strings.Contains(t, "*") {
			t = "*"
		}
		s.PT = p + "/" + t
	}
	s.Loyalty = f.Loyalty

	g := &gorgeNorm{s: s, f: f, idx: idx}
	for _, k := range f.Keywords {
		g.keyword(k)
	}
	for _, a := range f.Abilities {
		if _, _, ok := kwTag(a.Params); ok {
			// A keyword's activated ability (equip, crew, a Class level-up).
			continue
		}
		if intrinsicMana(f, a) {
			continue
		}
		g.sa(a)
	}
	// Every "as this enters" replacement of the face (tapped, with
	// counters, a choice) is one ability, the way the printed text and
	// mtgish's AsPermanentEnters group them; scripts write one R: line each.
	var enter *Ability
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if head, _, ok := kwTag(t.Params); ok && !printedExpansion(head) {
			continue
		}
		if g.gorgeStaticTrigger(t, &enter) || secondaryHalf(f, t) {
			continue
		}
		g.trigger(t)
	}
	cda := false
	for i := range f.Statics {
		st := &f.Statics[i]
		if head, _, ok := kwTag(st.Params); ok && !printedExpansion(head) {
			continue
		}
		if st.Params["CharacteristicDefining"] == "True" {
			// One CDA ability however many statics carry it (a script may
			// split power and toughness).
			if !cda {
				cda = true
				g.ability(Ability{Kind: "static", Effects: []string{"cda"}})
			}
			continue
		}
		g.static(st)
	}
	for i := range f.Repls {
		r := &f.Repls[i]
		if head, _, ok := kwTag(r.Params); ok && !printedExpansion(head) {
			continue
		}
		if selfEnter(r) {
			if enter == nil {
				enter = &Ability{Kind: "replacement"}
			}
			enter.Effects = append(enter.Effects, g.enterEffects(r)...)
			continue
		}
		g.repl(r)
	}
	if enter != nil {
		g.ability(*enter)
	}
	mergeSpells(s, true)
	splitStatics(s)
	return s
}

// printedKeywordExpansion are the Forge keyword heads whose expansions are
// abilities printed in the card's text, not keyword abilities: a Class
// level's triggers and statics, an "As this enters, choose ..." or an
// "enters with counters" replacement. printedExpansion adds the "Prevent
// all ... damage" shields. (A Class's level-up activated abilities are
// dropped with every keyword AB in gorgeFace: mtgish carries them as the
// level structure, not as abilities.)
var printedKeywordExpansion = map[string]bool{
	"Class": true, "ETBReplacement": true, "etbCounter": true,
}

func printedExpansion(head string) bool {
	return printedKeywordExpansion[head] || strings.HasPrefix(head, "Prevent ")
}

// kwTag reports whether params belong to a keyword's expansion, and the
// keyword's head and full line. Expanders tag what they mint with
// KeywordLine (most) or Keyword (the Class levels).
func kwTag(p map[string]string) (head, line string, ok bool) {
	line = p["KeywordLine"]
	if line == "" {
		line = p["Keyword"]
	}
	if line == "" {
		return "", "", false
	}
	return cards.KeywordHead(line), line, true
}

// basicManaColour is the colour each basic land type's intrinsic ability
// makes (CR 305.6).
var basicManaColour = map[string]string{"Plains": "W", "Island": "U", "Swamp": "B", "Mountain": "R", "Forest": "G"}

// intrinsicMana reports whether a is the "{T}: Add <colour>" ability a
// basic land type grants: Forge scripts some of them, mtgish (like the
// printed card) leaves them implicit.
func intrinsicMana(f *cards.Face, a *cards.SA) bool {
	if a.Kind != "AB" || a.API != "Mana" || a.Params["Cost"] != "T" || a.Sub != nil {
		return false
	}
	for _, t := range f.Types {
		if c, ok := basicManaColour[t]; ok && a.Params["Produced"] == c {
			return true
		}
	}
	return false
}

// selfEnter reports whether r is an "as this enters" replacement of its own
// card.
func selfEnter(r *cards.Repl) bool {
	p := r.Params
	return r.Event == "Moved" && p["Destination"] == "Battlefield" &&
		(p["ValidCard"] == "Card.Self" || strings.HasPrefix(p["ValidCard"], "Card.Self+") || p["ValidCard"] == "")
}

// gorgeManaSymbol maps a Forge mana-cost token to the canonical symbol:
// "W", "2", "X", and the two-letter hybrids Forge writes without a slash
// ("GW", "2W", "WP").
func gorgeManaSymbol(t string) string {
	t = strings.ToUpper(t)
	if !strings.Contains(t, "/") && len(t) >= 2 && len(t) <= 3 {
		if _, err := strconv.Atoi(t); err != nil {
			return strings.Join(strings.Split(t, ""), "/")
		}
	}
	return t
}

type gorgeNorm struct {
	s    *Summary
	f    *cards.Face
	idx  TypeIndex
	mode int // gift/kicker branch being summarised (modeSA)
}

func (g *gorgeNorm) kw(tok string)            { g.s.Keywords = append(g.s.Keywords, tok) }
func (g *gorgeNorm) ability(a Ability)        { g.s.Abilities = append(g.s.Abilities, a) }
func (g *gorgeNorm) svars() map[string]string { return g.f.SVars }

// gorgeCostKeywords are the keyword heads whose parameter is a cost
// (mtgish's costKeywords); a head in none of these tables is a
// parameterless keyword, its token keywordWord(head).
var gorgeCostKeywords = map[string]bool{
	"Ward": true, "Flashback": true, "Kicker": true, "Offspring": true, "Cycling": true,
	"Equip": true, "Madness": true, "Morph": true, "Megamorph": true, "Disguise": true,
	"Dash": true, "Evoke": true, "Escape": true, "Embalm": true, "Eternalize": true,
	"Unearth": true, "Bestow": true, "Blitz": true, "Buyback": true, "Echo": true, "Replicate": true,
	"Emerge": true, "Encore": true, "Foretell": true, "Miracle": true, "Multikicker": true,
	"Mutate": true, "Ninjutsu": true, "Plot": true, "Prowl": true, "Reconfigure": true,
	"Scavenge": true, "Spectacle": true, "Squad": true, "Surge": true, "Transmute": true,
	"Outlast": true, "Fortify": true, "Disturb": true, "Freerunning": true, "Warp": true,
	"Sneak": true, "Harmonize": true, "Mayhem": true, "Craft": true, "Cumulative upkeep": true,
}

var gorgeNumberKeywords = map[string]bool{
	"Crew": true, "Annihilator": true, "Afflict": true, "Fabricate": true, "Toxic": true,
	"Modular": true, "Renown": true, "Bushido": true, "Rampage": true, "Poisonous": true,
	"Dredge": true, "Fading": true, "Frenzy": true, "Graft": true, "Absorb": true,
	"Amplify": true, "Tribute": true, "Bloodthirst": true, "Soulshift": true,
	"Casualty": true, "Mobilize": true, "Hideaway": true, "Backup": true, "Saddle": true,
	"Afterlife": true,
}

// gorgeSkippedKeywords are Forge keyword lines that are not keywords of the
// printed card: their meaning is summarised elsewhere (an expansion kept by
// printedKeywordExpansion) or is not an ability in play.
var gorgeSkippedKeywords = map[string]bool{
	"Class": true, "ETBReplacement": true, "etbCounter": true,
	"A deck can have any number of cards named CARDNAME.": true,
	// Deck-building and commander rules: not abilities in play, and mtgish
	// carries no Rule for them.
	"Partner": true, "Partner with": true, "Choose a Background": true, "Doctor's companion": true,
	"Companion": true, "CARDNAME can be your commander.": true, "Friends forever": true,
	"Remove CARDNAME from your deck before playing if you're not playing for ante.": true,
}

// gorgeBareKeywords are compared by name only (bareKeywords' twins).
var gorgeBareKeywords = map[string]bool{
	"Landwalk": true, "TypeCycling": true, "Suspend": true, "Vanishing": true, "Station": true,
	"Prototype": true, "Splice": true, "Champion": true, "Reinforce": true, "Web-slinging": true,
	"Recover": true, "Offering": true, "Impending": true, "Ripple": true, "Specialize": true,
	"More Than Meets the Eye": true, "Starting intensity": true, "Haunt": true, "Firebending": true,
	"Devour": true, "Craft": true, "Visit": true, "Level up": true, "Overload": true, "Awaken": true,
	"Entwine": true, "Escalate": true, "Teamwork": true, "Cleave": true, "Mobilize": true,
	"Bloodthirst": true, "Spree": true, "Tiered": true,
}

func (g *gorgeNorm) keyword(k string) {
	head := cards.KeywordHead(k)
	param := ""
	if i := strings.IndexByte(k, ':'); i >= 0 {
		param = strings.TrimSpace(k[i+1:])
	}
	switch {
	case gorgeSkippedKeywords[head]:
		return
	case head == "MayEffectFromOpeningHand":
		g.ability(Ability{Kind: "pregame"})
		return
	case head == "AlternateAdditionalCost":
		// An additional casting cost: a cost, not an ability (mtgish's
		// AdditionalCastingCost is skipped the same way).
		return
	case head == "MayFlashCost":
		g.ability(Ability{Kind: "static", Effects: []string{"flash"}})
		return
	case strings.HasPrefix(head, "Prevent "):
		// Summarised through its expansion's prevention shields.
		return
	case strings.HasPrefix(head, "Protection"):
		q := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(head, "Protection")), "from"))
		if q == "" || strings.Contains(q, ":") || param != "" {
			g.kw("protection:*")
		} else {
			g.kw("protection:" + typeWord(q))
		}
		return
	case head == "Hexproof" && param != "":
		q := param
		if i := strings.IndexByte(q, ':'); i >= 0 {
			q = q[:i]
		}
		for _, alt := range strings.Split(q, ",") {
			g.kw("hexprooffrom:" + typeWord(strings.TrimPrefix(alt, "Card.")))
		}
		return
	case head == "Enchant":
		cls := strings.SplitN(param, ":", 2)[0]
		g.kw("enchant:" + targetToken(g.idx.validClasses(cls, ""), "1"))
		return
	case head == "Affinity":
		g.kw("affinity:" + typeWord(param))
		return
	case head == "Gift":
		g.kw("gift")
		return
	case head == "Chapter":
		// "Chapter:3:A,B,C": one chapter ability per listed effect.
		parts := strings.SplitN(param, ":", 2)
		if len(parts) == 2 {
			for _, name := range strings.Split(parts[1], ",") {
				e := &effects{}
				g.chain(e, cards.ResolveSVar(g.svars(), strings.TrimSpace(name)), 1)
				g.ability(Ability{Kind: "triggered", Event: "chapter", Targets: e.targets, Effects: e.effects})
			}
		}
		return
	case gorgeBareKeywords[head]:
		g.kw(keywordWord(head))
		g.keywordAbilities(head, param)
		return
	case gorgeCostKeywords[head]:
		g.kw(keywordWord(head) + ":" + gorgeCost(param))
		return
	case gorgeNumberKeywords[head]:
		n := strings.SplitN(param, ":", 2)[0]
		if _, err := strconv.Atoi(n); err != nil {
			n = "*"
		}
		g.kw(keywordWord(head) + ":" + n)
		g.keywordAbilities(head, param)
		return
	}
	if f, ok := extKeyword[head]; ok {
		f(g, param)
		return
	}
	if param != "" || strings.ContainsAny(head, ".$") {
		g.kw(unmapped("kw:" + head))
		return
	}
	g.kw(keywordWord(head))
}

// gorgeCost renders a keyword cost parameter like mtgishCost does.
func gorgeCost(p string) string {
	p = strings.SplitN(p, ":", 2)[0]
	if strings.HasPrefix(p, "PayLife<") {
		return "life:" + strings.TrimSuffix(strings.TrimPrefix(p, "PayLife<"), ">")
	}
	var mc manaCost
	for _, t := range strings.Fields(p) {
		if !manaToken(t) {
			return "*"
		}
		mc.add(gorgeManaSymbol(t))
	}
	if s := mc.String(); s != "" {
		return s
	}
	return "*"
}

// manaToken reports whether t is a Forge mana-cost token.
func manaToken(t string) bool {
	if _, err := strconv.Atoi(t); err == nil {
		return true
	}
	for _, part := range strings.Split(t, "/") {
		if len(part) != 1 || !strings.Contains("WUBRGCXSP2", strings.ToUpper(part)) {
			return false
		}
	}
	return true
}

// sa summarises a printed SP/AB ability.
func (g *gorgeNorm) sa(a *cards.SA) {
	kind := "activated"
	switch a.Kind {
	case "SP":
		kind = "spell"
		if a.API == "PermanentCreature" || a.API == "PermanentNoncreature" {
			// The cast-a-permanent ability every permanent card has.
			return
		}
	case "AB":
	default:
		kind = unmapped("SA." + a.Kind)
	}
	e := &effects{}
	if kind == "spell" && (g.f.HasKeyword("Gift") || g.f.HasKeyword("Kicker")) {
		// Promised/kicked or not: two branches, merged like mtgish's.
		var subs []*effects
		for _, m := range []int{modeYes, modeNo} {
			sub := &effects{}
			g.mode = m
			g.chain(sub, a, 0)
			subs = append(subs, sub)
		}
		g.mode = modeNone
		e.merge(subs)
	} else {
		g.chain(e, a, 0)
	}
	g.ability(Ability{Kind: kind, Targets: e.targets, Effects: e.effects})
}

// trigger summarises a T: line.
func (g *gorgeNorm) trigger(t *cards.Trigger) {
	e := &effects{}
	eff := t.Effect
	if eff == nil {
		eff = cards.ResolveSVar(g.svars(), t.Params["Execute"])
	}
	g.chain(e, eff, 1)
	g.ability(Ability{Kind: "triggered", Event: gorgeTriggerClass(t.Mode, t.Params), Targets: e.targets, Effects: e.effects})
}

// gorgeTriggerClass maps a trigger mode and its parameters to the event
// class triggerClass assigns the mtgish spelling.
func gorgeTriggerClass(mode string, p map[string]string) string {
	switch mode {
	case "ChangesZone", "ChangesZoneAll":
		o, d := p["Origin"], p["Destination"]
		switch {
		case d == "Battlefield":
			return "etb"
		case o == "Battlefield" && d == "Graveyard":
			return "dies"
		case o == "Battlefield":
			return "ltb"
		case d == "Graveyard":
			return "to-graveyard"
		}
		return "zone"
	case "Phase":
		switch p["Phase"] {
		case "Upkeep":
			return "upkeep"
		case "End of Turn", "End":
			return "end-step"
		case "BeginCombat":
			return "combat"
		case "Draw":
			return "draw-step"
		}
		if strings.HasPrefix(p["Phase"], "Main") {
			return "main"
		}
		return unmapped("Phase." + p["Phase"])
	case "Attacks", "AttackersDeclared", "AttackersDeclaredOneTarget":
		return "attacks"
	case "Blocks":
		if triggerSubject(p["ValidBlocked"]) && !triggerSubject(p["ValidCard"]) {
			// "Whenever a creature blocks this": this becomes blocked.
			return "blocked"
		}
		return "blocks"
	case "AttackerBlocked", "AttackerBlockedByCreature":
		if selfBlocker(p) {
			return "blocks"
		}
		return "blocked"
	case "SpellCast", "SpellCastOrCopy", "SpellCopy":
		return "cast"
	case "DamageDone", "DamageDoneOnce", "DamageDealtOnce", "DamageAll", "DamageDoneOnceByController":
		combat := p["CombatDamage"] == "True"
		player := damagesPlayer(p["ValidTarget"])
		switch {
		case combat && player:
			return "combat-damage-player"
		case combat:
			return "combat-damage"
		}
		return "damage"
	case "LifeGained":
		return "lifegain"
	case "LifeLost", "LifeLostAll":
		return "lifeloss"
	case "ManaExpend":
		return "expend"
	case "BecomesTarget", "BecomesTargetOnce":
		return "becomes-target"
	case "Drawn":
		return "draw"
	case "Sacrificed", "SacrificedOnce":
		return "sacrifice"
	case "Discarded", "DiscardedAll":
		return "discard"
	case "CounterAdded", "CounterAddedOnce", "CounterAddedAll":
		return "counters"
	case "Forage":
		return "forage"
	case "GiveGift":
		return "gift"
	case "Taps", "TapsForMana":
		return "tap"
	case "Untaps":
		return "untap"
	case "Always":
		return "state"
	case "ClassLevelGained":
		return "class-level"
	}
	if f, ok := extTrigger[mode]; ok {
		return f(p)
	}
	return unmapped("Trigger." + mode)
}

// static summarises an S: line.
func (g *gorgeNorm) static(st *cards.Static) {
	eff := gorgeStaticEffects(st.Mode, st.Params)
	if eff == nil {
		return
	}
	if st.Mode == "Continuous" {
		if eff = g.selfGrantAbilities(st.Params, eff); len(eff) == 0 {
			return
		}
	}
	g.ability(Ability{Kind: "static", Effects: eff})
}

func gorgeStaticEffects(mode string, p map[string]string) []string {
	if selfCastRestriction(mode, p) {
		return nil
	}
	switch mode {
	case "Continuous":
		var out []string
		if p["AddPower"] != "" || p["AddToughness"] != "" {
			out = append(out, pump(signedParam(p["AddPower"]), signedParam(p["AddToughness"])))
		}
		player := strings.HasPrefix(p["Affected"], "You") || strings.HasPrefix(p["Affected"], "Player") || strings.HasPrefix(p["Affected"], "Opponent")
		stack := p["AffectedZone"] == "Stack"
		for _, k := range cards.SplitKeywordList(p["AddKeyword"]) {
			if stack {
				// "Spells you cast have storm": a spell effect.
				out = append(out, "spell:"+grantKeyword(k))
			} else if player {
				// "You have hexproof": a player effect.
				out = append(out, "player:"+grantKeyword(k))
			} else {
				out = append(out, "grant:"+grantKeyword(k))
			}
		}
		if p["SetMaxHandSize"] == "Unlimited" {
			out = append(out, "player:hasnomaximumhandsize")
		}
		if p["AdjustLandPlays"] != "" {
			out = append(out, "player:mayplayanadditionalland")
		}
		if p["MayLookAt"] != "" {
			out = append(out, "player:maylookattopcardoflibraryanytime")
		}
		if p["SetName"] != "" {
			out = append(out, "name")
		}
		for _, k := range []string{"AddAbility", "AddTrigger", "AddStaticAbility", "AddReplacementEffects", "AddReplacementEffect"} {
			if p[k] != "" {
				for range strings.Split(p[k], "&") {
					out = append(out, "grant:ability")
				}
			}
		}
		if p["SetPower"] != "" || p["SetToughness"] != "" {
			out = append(out, "setpt")
		}
		if p["AddType"] != "" || p["AddTypes"] != "" || p["RemoveType"] != "" || p["AddAllCreatureTypes"] != "" ||
			p["RemoveCreatureTypes"] != "" || p["RemoveCardTypes"] != "" || p["SetCreatureTypes"] != "" {
			out = append(out, "type")
		}
		if p["SetColor"] != "" || p["AddColor"] != "" {
			out = append(out, "color")
		}
		if p["RemoveAllAbilities"] == "True" || p["RemoveKeyword"] != "" {
			out = append(out, "loseabilities")
		}
		if p["GainControl"] != "" {
			out = append(out, "gaincontrol")
		}
		if p["MayPlay"] != "" {
			out = append(out, "mayplay")
		}
		if p["CantHaveKeyword"] != "" {
			// "... and can't have or gain <keyword>": a rule beside the
			// layer effect, as mtgish's CantGainAbility.
			out = append(out, "rule:cantgainability")
		}
		if p["Goad"] == "True" {
			out = append(out, "rule:isgoadedbyplayer")
		}
		for _, k := range cards.SplitKeywordList(p["AddHiddenKeyword"]) {
			out = append(out, hiddenKeywordToken(k))
		}
		if len(out) == 0 {
			if p["AddSVar"] != "" {
				// An AI hint carried as a static (AddSVar$ only): no
				// printed ability.
				return nil
			}
			return []string{unmapped("Static.Continuous")}
		}
		return dedup(out)
	case "ReduceCost":
		return []string{"costreduction"}
	case "RaiseCost":
		if selfSpellCost(p) {
			return nil
		}
		return []string{"costincrease"}
	case "CantBlockBy":
		if p["ValidBlocker"] != "" {
			return []string{"rule:cantbeblockedbydefenders"}
		}
		return []string{"rule:cantbeblocked"}
	case "CantBlock":
		return []string{"rule:cantblock"}
	case "CantAttack":
		return []string{"rule:cantattack"}
	case "MustAttack":
		return []string{"rule:mustattack"}
	case "CantGainLife":
		return []string{"player:cantgainlife"}
	case "CantBeCast":
		return []string{"player:cantcastspells"}
	case "CastWithFlash":
		return []string{"flash"}
	case "AlternativeCost":
		return []string{"alternativecost"}
	case "CantBeActivated":
		return []string{"rule:cantbeactivated"}
	case "CantPreventDamage":
		return []string{"static:damagecantbeprevented"}
	case "ManaConvert":
		return []string{"manaconvert"}
	}
	if f, ok := extStatic[mode]; ok {
		return f(p)
	}
	return []string{unmapped("Static." + mode)}
}

// hiddenKeywordToken maps a Forge hidden keyword to a rule token.
func hiddenKeywordToken(k string) string {
	k = strings.TrimPrefix(k, "HIDDEN ")
	switch {
	case strings.Contains(k, "can't block"):
		return "rule:cantblock"
	case strings.Contains(k, "can't attack"):
		return "rule:cantattack"
	case strings.Contains(k, "attacks each") || strings.Contains(k, "must attack"):
		return "rule:mustattack"
	case strings.Contains(k, "can't be blocked"):
		return "rule:cantbeblocked"
	case strings.Contains(k, "able to block CARDNAME do so"):
		return "rule:allcreaturesmustblockit"
	case strings.Contains(k, "must be blocked"):
		return "rule:mustbeblocked"
	case strings.Contains(k, "doesn't untap"):
		return "rule:doesntuntapduringcontrollersuntap"
	}
	return unmapped("hidden:" + k)
}

// grantKeyword is the canonical head of a granted keyword ("Ward:2" ->
// "ward", "Protection from red" -> "protection").
func grantKeyword(k string) string {
	head := cards.KeywordHead(k)
	switch {
	case strings.HasPrefix(head, "Protection"):
		return "protection"
	case head == "Hexproof" && strings.Contains(k, ":"):
		return "hexprooffrom"
	case strings.HasPrefix(head, "HIDDEN "):
		return strings.TrimPrefix(hiddenKeywordToken(head), "rule:")
	}
	return keywordWord(head)
}

// signedParam renders a Forge +N/-N parameter ("" is +0, non-literal "*").
func signedParam(v string) string {
	if v == "" {
		return "+0"
	}
	n, err := strconv.Atoi(strings.TrimPrefix(v, "+"))
	if err != nil {
		return "*"
	}
	return signed(n)
}

// repl summarises an R: line.
func (g *gorgeNorm) repl(r *cards.Repl) {
	p := r.Params
	if eff, ok := replStatic(r, g.svars()); ok {
		g.ability(Ability{Kind: "static", Effects: eff})
		return
	}
	switch r.Event {
	case "Untap":
		g.ability(Ability{Kind: "static", Effects: []string{"rule:doesntuntapduringcontrollersuntap"}})
		return
	case "GameLoss":
		if p["ReplaceWith"] != "" {
			// "If you would lose the game, instead ...": a replacement.
			g.ability(Ability{Kind: "replacement", Effects: []string{"replace:wouldlosethegame"}})
			return
		}
		g.ability(Ability{Kind: "static", Effects: []string{"player:cantlosethegame"}})
		return
	case "GameWin":
		g.ability(Ability{Kind: "static", Effects: []string{"player:cantwinthegame"}})
		return
	case "Counter":
		// "This spell can't be countered" / "spells you control can't be
		// countered": a static rule printed, a replacement in Forge.
		g.ability(Ability{Kind: "static", Effects: []string{"spell:cantbecountered"}})
		return
	}
	// Replacements other than "as this enters" are compared by the event
	// they replace (mtgish's Replace<Event> rule), not by what they do.
	a := Ability{Kind: "replacement"}
	switch {
	case r.Event == "Moved" && p["Discard"] == "True":
		a.Effects = []string{"replace:woulddiscard"}
	case r.Event == "Moved" && p["Destination"] == "Graveyard":
		a.Effects = []string{"replace:wouldputintograveyard"}
	case r.Event == "Moved" && p["Destination"] == "Battlefield":
		a.Effects = []string{"replace:wouldenter"}
	case r.Event == "DamageDone" && p["Prevent"] == "True":
		a.Effects = []string{"replace:preventdamage"}
	case r.Event == "DamageDone":
		a.Effects = []string{"replace:woulddealdamage"}
	case r.Event == "AddCounter":
		a.Effects = []string{"replace:wouldputcounters"}
	case r.Event == "CreateToken":
		a.Effects = []string{"replace:aneffectwouldcreateanynumberoftokens"}
	case r.Event == "GainLife":
		a.Effects = []string{"replace:wouldgainlife"}
	case r.Event == "Draw":
		a.Effects = []string{"replace:woulddraw"}
	case r.Event == "Discard":
		a.Effects = []string{"replace:woulddiscard"}
	default:
		if f, ok := extRepl[r.Event]; ok {
			a.Effects = f(r)
		} else {
			a.Effects = []string{unmapped("Repl." + r.Event)}
		}
	}
	g.ability(a)
}

// enterEffects maps a self "as this enters" replacement: tapped, with
// counters, a choice, a copy.
func (g *gorgeNorm) enterEffects(r *cards.Repl) []string {
	p := r.Params
	with := r.With
	if with == nil {
		with = cards.ResolveSVar(g.svars(), p["ReplaceWith"])
	}
	if head, kw, ok := kwTag(p); ok && head == "etbCounter" {
		parts := strings.Split(kw, ":")
		ct, n := "*", "1"
		if len(parts) > 1 {
			ct = gorgeCounterType(parts[1])
		}
		if len(parts) > 2 {
			n = literalOr(parts[2], "*")
		}
		return []string{"counters:" + ct + ":" + n}
	}
	var out []string
	for a := with; a != nil; a = a.Sub {
		switch {
		case a.API == "Tap":
			out = append(out, "etbtapped")
		case strings.HasPrefix(a.API, "Choose") || a.API == "NameCard":
			out = append(out, "choose")
		case a.API == "RevealHand":
			out = append(out, "reveal")
		case a.API == "Clone":
			out = append(out, "copy")
		case a.API == "PutCounter":
			out = append(out, "counters:"+gorgeCounterType(a.Params["CounterType"])+":"+literalOr(a.Params["CounterNum"], "1"))
		case a.API == "InternalEtbReplacement" || a.API == "Cleanup":
		default:
			if f, ok := extEnterAPI[a.API]; ok {
				out = append(out, f(a)...)
			} else {
				out = append(out, unmapped("Enter."+a.API))
			}
		}
	}
	return out
}

// literalOr returns v when it is an integer literal, else def ("" -> "1"
// is the caller's choice through def only when v is empty).
func literalOr(v, def string) string {
	if v == "" {
		return def
	}
	if _, err := strconv.Atoi(v); err == nil {
		return v
	}
	return "*"
}

// gorgeCounterType canonicalises a Forge counter type.
func gorgeCounterType(t string) string {
	if t == "" {
		return "*"
	}
	// P1P1, M1M1, P1P0, M0M1 ...
	if len(t) == 4 && (t[0] == 'P' || t[0] == 'M') && (t[2] == 'P' || t[2] == 'M') {
		sign := func(c byte) string {
			if c == 'M' {
				return "-"
			}
			return "+"
		}
		return sign(t[0]) + t[1:2] + "/" + sign(t[2]) + t[3:4]
	}
	return counterName(t)
}
