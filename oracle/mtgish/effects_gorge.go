package mtgish

import (
	"regexp"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

const maxChainDepth = 8

// chain summarises an SA and its SubAbility chain, following the SVar
// references that carry further effects (modes, branches, repeats, delayed
// and reflexive triggers). Modes (Charm, GenericChoice and its fallback)
// are alternatives, merged as mtgish's modal actions are.
func (g *gorgeNorm) chain(e *effects, a *cards.SA, depth int) {
	if depth > maxChainDepth {
		return
	}
	if depth == 0 {
		g.parked = false
	}
	for ; a != nil; a = a.Sub {
		if a.API == "Branch" {
			// An if/else: the two branches are alternatives.
			var subs []*effects
			for _, n := range []string{a.Params["TrueSubAbility"], a.Params["FalseSubAbility"]} {
				sub := &effects{}
				g.chain(sub, cards.ResolveSVar(g.svars(), n), depth+1)
				subs = append(subs, sub)
			}
			if t := g.idx.gorgeTargets(a); t != "" {
				e.targets = append(e.targets, t)
			}
			e.merge(subs)
			continue
		}
		if a.API == "Charm" || a.API == "GenericChoice" {
			if t := g.idx.gorgeTargets(a); t != "" {
				e.targets = append(e.targets, t)
			}
			names := cards.SplitModeNames(a.Params["Choices"])
			if fb := a.Params["FallbackAbility"]; fb != "" {
				names = append(names, fb)
			}
			var subs []*effects
			for _, n := range names {
				sub := &effects{}
				g.chain(sub, cards.ResolveSVar(g.svars(), n), depth+1)
				subs = append(subs, sub)
			}
			e.merge(subs)
			continue
		}
		pa := g.modeSA(a)
		if pa == nil {
			// Not run in this gift/kicker branch.
			continue
		}
		a := pa
		if depth > 0 && strings.Contains(a.Params["Cost"], "Forage") {
			// "You may forage" inside an effect is written as a nested
			// ability whose cost is the forage; an activated ability's own
			// cost (depth 0) is a cost.
			e.add("forage")
		}
		e.add(p2dNestedCost(a)...)
		if t := g.p2aTargets(a, g.idx.gorgeTargets(a)); t != "" {
			e.targets = append(e.targets, t)
		}
		if a.Params["Duration"] == "Perpetual" {
			// A perpetual change (digital): one verb, as mtgish's
			// CreatePerpetual* actions.
			e.add("perpetual")
		} else {
			e.add(p2dPlayerGrants(a, g.p2fSearchDelivery(a, gorgeAPIEffects(a, g.svars())))...)
		}
		e.add(atEOTTokens(a.Params["AtEOT"])...)
		e.add(paramTokensP2B(a)...)
		e.add(p2fParamTokens(a)...)
		if a.API == "Effect" {
			// The triggers an effect creates run their own effects, as
			// mtgish's CreateTriggerUntil actions do. An emblem's triggers
			// choose their own targets when they trigger: like mtgish's
			// GetAnEmblem, only their verbs are this ability's.
			sub := e
			emblem := strings.HasPrefix(a.Params["Name"], "Emblem")
			if emblem {
				sub = &effects{}
			}
			for _, name := range strings.Split(a.Params["Triggers"], ",") {
				if effectCleanupTrigger(g.svars(), name) {
					continue
				}
				if body, ok := g.svars()[strings.TrimSpace(name)]; ok {
					if t, ok := cards.ParseTriggerLine(body); ok {
						g.chain(sub, cards.ResolveSVar(g.svars(), t.Params["Execute"]), depth+1)
					}
				}
			}
			if emblem {
				e.add(sub.effects...)
			}
		}
		if a.Params["ReplaceDyingDefined"] != "" || a.Params["LeaveBattlefield"] != "" {
			// "If that creature would die this turn (would leave the
			// battlefield), exile it instead."
			e.add("effect")
		}
		for _, ref := range svarRefs(a) {
			g.chain(e, cards.ResolveSVar(g.svars(), ref), depth+1)
		}
		if alts := gorgeAltRefs(a); len(alts) > 0 {
			var subs []*effects
			for _, n := range alts {
				sub := &effects{}
				if n != "" {
					g.chain(sub, cards.ResolveSVar(g.svars(), n), depth+1)
				}
				subs = append(subs, sub)
			}
			e.merge(subs)
		}
		g.c2Chase(e, a, depth)
	}
}

// Branch modes: a gift spell (promised or not) and a kicker spell (kicked
// or not) are summarised once per branch and merged, as mtgish writes
// them as alternatives. modeNone summarises the script as written.
const (
	modeNone = iota
	modeYes
	modeNo
)

// modeCount matches the branch-dependent counts scripts use:
// Count$PromisedGift.<if promised>.<if not>, Count$Kicked.<if>.<if not>.
var modeCount = regexp.MustCompile(`^Count\$(PromisedGift|Kicked)\.([^.]+)\.([^.]+)$`)

// modeSA returns a under the current branch mode: its branch-dependent
// parameters resolved to the branch's literal, or nil when the branch does
// not run it (a gift/kicked condition that fails, or no targets left).
func (g *gorgeNorm) modeSA(a *cards.SA) *cards.SA {
	if g.mode == modeNone {
		return a
	}
	cond := a.Params["ConditionPresent"] + " " + a.Params["Condition"]
	if strings.Contains(cond, "PromisedGift") || strings.Contains(cond, "Kicked") {
		want := a.Params["ConditionCompare"] != "EQ0"
		if strings.Contains(cond, "NotKicked") {
			want = !want
		}
		if (g.mode == modeYes) != want {
			return nil
		}
	}
	params := make(map[string]string, len(a.Params))
	for k, v := range a.Params {
		params[k] = v
		if body, ok := g.svars()[v]; ok {
			if m := modeCount.FindStringSubmatch(strings.TrimSpace(body)); m != nil {
				if g.mode == modeYes {
					params[k] = m[2]
				} else {
					params[k] = m[3]
				}
			}
		}
	}
	if params["TargetMax"] == "0" {
		return nil
	}
	return &cards.SA{Kind: a.Kind, API: a.API, Params: params}
}

// svarRefs names the SVars an SA runs besides its SubAbility$.
func svarRefs(a *cards.SA) []string {
	var out []string
	switch a.API {
	case "Repeat", "RepeatEach":
		out = append(out, a.Params["RepeatSubAbility"])
	case "TwoPiles":
		out = append(out, a.Params["ChosenPile"], a.Params["UnchosenPile"])
	case "DelayedTrigger", "ImmediateTrigger":
		out = append(out, a.Params["Execute"])
	case "FlipCoin":
		// (Clash's branches are alternatives: gorgeAltRefs.) What winning or losing does (mtgish writes "When you win the
		// flip, ... When you lose, ..." as reflexive actions).
		for _, k := range []string{"WinSubAbility", "LoseSubAbility", "HeadsSubAbility", "TailsSubAbility", "OtherwiseSubAbility"} {
			out = append(out, a.Params[k])
		}
	}
	if f, ok := extSVarRefs[a.API]; ok {
		out = append(out, f(a)...)
	}
	var keep []string
	for _, r := range out {
		if r != "" {
			keep = append(keep, r)
		}
	}
	return keep
}

// gorgeTargets renders the SA's own target slot, "" when it targets
// nothing.
func (idx TypeIndex) gorgeTargets(a *cards.SA) string {
	p := a.Params
	vt := p["ValidTgts"]
	tt := p["TargetType"]
	if vt == "" {
		// A TargetType$ alone restricts a Defined$ stack object (a
		// ChangeTargets of the triggering spell): no target.
		return ""
	}
	zone := p["TgtZone"]
	if zone == "" {
		switch a.API {
		case "ChangeZone", "ChangeZoneAll":
			// A ChangeZone targets in its origin zone.
			zone = p["Origin"]
		case "Counter", "CopySpellAbility", "ChangeTargets":
			zone = "Stack"
		}
	}
	if zone == "Battlefield" {
		zone = ""
	}
	cls := idx.validClasses(vt, zone)
	if sc := stackTargetTypes(tt); sc != nil {
		// TargetType$ makes the target a stack object whatever TgtZone$
		// says (ValidTgts$ then filters its card).
		cls = sc
	}
	min := literalOr(p["TargetMin"], "1")
	max := literalOr(p["TargetMax"], "1")
	if p["TargetMin"] == "" && p["TargetMax"] != "" {
		min = "1"
	}
	if min == "0" && p["DividedAsYouChoose"] != "" && p["TargetMax"] == p["DividedAsYouChoose"] {
		// "N damage divided as you choose among any number of targets":
		// the count is bounded by the amount (distributedCount).
		max = "*"
	}
	return targetToken(cls, countRange(min, max))
}

// validClasses maps a ValidTgts$ (or Enchant) list to target classes. An
// alternative naming several card types ("Creature.Artifact") is their
// intersection ("artifact&creature"), as permanentsClasses renders it.
func (idx TypeIndex) validClasses(vt, zone string) []string {
	var out []string
	zones := strings.Split(zone, ",")
	for _, alt := range strings.Split(vt, ",") {
		alt = strings.TrimSpace(alt)
		head := alt
		if i := strings.IndexAny(head, ".+"); i >= 0 {
			head = head[:i]
		}
		w := typeWord(head)
		for _, z := range altZones(alt, zones) {
			switch {
			case w == "any":
				out = append(out, clsAny)
			case w == "player" || w == "opponent" || w == "you":
				out = append(out, clsPlayer)
			case strings.Contains(z, "Graveyard"):
				out = append(out, clsGraveyard)
			case strings.Contains(z, "Exile"):
				out = append(out, clsExile)
			case strings.Contains(z, "Stack"):
				out = append(out, clsSpell)
			default:
				types, generic := idx.altTypes(alt)
				switch {
				case len(types) > 0:
					out = append(out, intersectionClass(types))
				case generic:
					out = append(out, clsPermanent)
				}
			}
		}
	}
	return narrowPermanent(out)
}

// gorgeAPIEffects maps one SA's API to its effect verbs.
func gorgeAPIEffects(a *cards.SA, svars map[string]string) []string {
	p := a.Params
	if p["Duration"] == "Perpetual" {
		// A perpetual change (Alchemy), whatever it changes: mtgish's
		// CreatePerpetual* actions are one "perpetual" each.
		return []string{"perpetual"}
	}
	switch a.API {
	case "DealDamage", "DamageAll":
		return []string{"damage:" + literalOr(p["NumDmg"], "*")}
	case "EachDamage":
		return []string{"damage:*"}
	case "Destroy", "DestroyAll":
		return []string{"destroy"}
	case "ChangeZone", "ChangeZoneAll":
		if effectSelfExile(a) || searchFollowUpP2B(a) {
			// An effect ending itself: bookkeeping, not a move.
			return nil
		}
		return p2dChangeZone(a.API, p)
	case "Draw":
		return []string{"draw:" + literalOr(p["NumCards"], "1")}
	case "Discard":
		n := literalOr(p["NumCards"], "1")
		if p["Mode"] == "Hand" || p["Mode"] == "RevealYouChoose" {
			n = "*"
		}
		return []string{"discard:" + n}
	case "Mill":
		return []string{"mill:" + literalOr(p["NumCards"], "1")}
	case "Scry":
		return []string{"scry:" + literalOr(p["ScryNum"], "1")}
	case "Surveil":
		return []string{"surveil:" + literalOr(p["Amount"], "1")}
	case "Counter":
		return []string{"counter"}
	case "Token":
		out := []string{"token:" + literalOr(p["TokenAmount"], "1")}
		out = append(out, p2dExtraTokenScripts(p)...)
		for _, k := range cards.SplitKeywordList(p["PumpKeywords"]) {
			out = append(out, "grant:"+grantKeyword(k))
		}
		return out
	case "CopyPermanent":
		out := []string{"tokencopy:" + literalOr(p["NumCopies"], "1")}
		if p["DefinedName"] != "" {
			// "Create a Spellgorger Weird token": a named predefined
			// token, not a copy of an object (mtgish OracleToken).
			out[0] = "token:" + literalOr(p["NumCopies"], "1")
		}
		// "It gains haste": a grant, as Token's PumpKeywords$.
		for _, k := range cards.SplitKeywordList(p["PumpKeywords"]) {
			out = append(out, "grant:"+grantKeyword(k))
		}
		return out
	case "PutCounter", "PutCounterAll":
		if n := p["Monstrosity"]; n != "" {
			// Monstrosity N: N +1/+1 counters, the type left implicit.
			return []string{"counters:+1/+1:" + literalOr(n, "*")}
		}
		if p["Support"] != "" {
			// Support N: one +1/+1 counter on each of up to N others.
			return []string{"counters:+1/+1:1"}
		}
		if p["CounterType"] == "EachFromSource" {
			return []string{"counters:*"}
		}
		if n := p["Adapt"] + p["Bolster"]; n != "" {
			// Adapt N / bolster N put N +1/+1 counters.
			return []string{"counters:+1/+1:" + literalOr(n, "*")}
		}
		ct := p["CounterType"]
		if ct == "" && p["CounterTypes"] != "" {
			// CounterTypes$: one of each listed type; ChosenFromList is
			// one chosen from TypeList$ (each listed, as alternatives
			// merge).
			ct = "EACH " + strings.Replace(p["CounterTypes"], "ChosenFromList", p["TypeList"], 1)
		}
		out := gorgeCounterTokens(ct, literalOr(p["CounterNum"], "1"))
		if ct2 := p["CounterType2"]; ct2 != "" {
			// "Two +1/+1 counters and a reach counter": a second type.
			n2 := p["CounterNum2"]
			if n2 == "" {
				n2 = p["CounterNum"]
			}
			out = append(out, gorgeCounterTokens(ct2, literalOr(n2, "1"))...)
		}
		return out
	case "MultiplyCounter":
		return []string{"counters:double"}
	case "RemoveCounter", "RemoveCounterAll":
		return []string{"countersremove"}
	case "Pump", "PumpAll":
		var out []string
		if p["NumAtt"] != "" || p["NumDef"] != "" {
			out = append(out, pump(signedParam(p["NumAtt"]), signedParam(p["NumDef"])))
		}
		if p["KWChoice"] != "" {
			out = append(out, "grant:*")
		}
		if p["CanBlockAny"] == "True" {
			out = append(out, "rule:canblockanynumberofcreatures")
		}
		for _, k := range cards.SplitKeywordList(p["KW"]) {
			if strings.HasPrefix(k, "Prevent ") {
				// A prevention shield granted until end of turn.
				out = append(out, "prevent")
			} else if strings.HasPrefix(k, "HIDDEN ") {
				out = append(out, hiddenKeywordToken(k))
			} else {
				out = append(out, "grant:"+grantKeyword(k))
			}
		}
		return out
	case "GainLife":
		return []string{"gainlife:" + literalOr(p["LifeAmount"], "*")}
	case "LoseLife":
		return []string{"loselife:" + literalOr(p["LifeAmount"], "*")}
	case "Tap", "TapAll":
		return []string{"tap"}
	case "Untap", "UntapAll":
		return []string{"untap"}
	case "Sacrifice", "SacrificeAll":
		return []string{"sacrifice"}
	case "Mana", "ManaReflected":
		return []string{"mana"}
	case "Fight":
		return []string{"fight"}
	case "GainControl":
		out := []string{"gaincontrol"}
		if p["Untap"] == "True" {
			out = append(out, "untap")
		}
		for _, k := range cards.SplitKeywordList(p["AddKWs"]) {
			out = append(out, "grant:"+grantKeyword(k))
		}
		return out
	case "Attach":
		return []string{"attach"}
	case "Dig":
		if p["DestinationZone"] == "Exile" && (p["ChangeNum"] == "All" || p["ChangeNum"] == "" && literalOr(p["DigNum"], "1") == "1") {
			// (ChangeNum$ defaults to 1: "look at the top three, exile one
			// of them" is a look, below.)
			// "Exile the top N cards" is a move, not a look.
			return []string{"move:library>exile"}
		}
		if p2dDigRevealOnly(p) {
			return []string{"dig:" + literalOr(p["DigNum"], "1")}
		}
		if p["ChangeNum"] == "All" && p["DestinationZone"] != "" {
			// "Put the top card into your hand": a move (revealed first,
			// a look as well).
			out := []string{"move:library>" + strings.ToLower(p["DestinationZone"])}
			if p["Reveal"] == "True" {
				out = append(out, "dig:"+literalOr(p["DigNum"], "1"))
			}
			if d2 := p["DestinationZone2"]; p["ChangeValid"] != "" && d2 != "" && d2 != "Library" {
				// "If it's a creature card, put it onto the battlefield;
				// otherwise put it into your graveyard": the rest moves too.
				out = append(out, "move:library>"+strings.ToLower(d2))
			}
			return out
		}
		// "Look at the top N, put M into <zone>, the rest into <zone2>".
		out := []string{"dig:" + literalOr(p["DigNum"], "1")}
		dest := p["DestinationZone"]
		if dest == "" {
			dest = "Hand"
		}
		if p["ChangeNum"] != "0" {
			// ChangeNum$ 0: a reveal that puts none of them anywhere but
			// back (map_p2d.go).
			out = append(out, "move:library>"+strings.ToLower(dest))
		}
		n, m := literalOr(p["DigNum"], "1"), literalOr(p["ChangeNum"], "1")
		if n == "*" || m == "*" || n != m {
			rest := p["DestinationZone2"]
			if rest == "" {
				rest = "Library"
			}
			// One card looked at and left on top is no move.
			if !(n == "1" && rest == "Library" && p["LibraryPosition2"] == "0") && !p2dDigRestStays(a, dest, rest) {
				out = append(out, "move:library>"+strings.ToLower(rest))
			}
		}
		return out
	case "DigUntil":
		if p["FoundDestination"] == "Exile" {
			// (Only an exiled find is the exile move alone; exiling the
			// other revealed cards is one of the moves below, map_p2d.)
			return []string{"move:library>exile"}
		}
		out := []string{"diguntil"}
		for _, k := range []string{"FoundDestination", "RevealedDestination"} {
			if k == "FoundDestination" && p[k] == "Library" && p["FoundLibraryPosition"] == "0" ||
				k == "RevealedDestination" && p["NoMoveRevealed"] == "True" {
				// Left on top (a later sub-ability moves it): no move.
				continue
			}
			if d := p[k]; d != "" && !p2dDigUntilShuffledBack(p, k) && !p2fShuffledAfter(a, p[k]) {
				out = append(out, "move:library>"+strings.ToLower(d))
			}
		}
		return dedup(out)
	case "PeekAndReveal":
		return []string{"dig:" + literalOr(p["PeekAmount"], "1")}
	case "CopySpellAbility":
		return []string{"copyspell"}
	case "Animate", "AnimateAll":
		var out []string
		if p["Power"] != "" || p["Toughness"] != "" {
			out = append(out, "setpt")
		}
		if p["RemoveAllAbilities"] == "True" || p["RemoveThisAbility"] == "True" {
			out = append(out, "loseabilities")
		}
		for _, k := range []string{"Triggers", "Abilities", "staticAbilities", "StaticAbilities", "Replacements"} {
			if p[k] != "" {
				out = append(out, "grant:ability")
			}
		}
		if p["Name"] != "" {
			out = append(out, "name")
		}
		if p["Types"] != "" {
			out = append(out, "type")
		}
		if p["Colors"] != "" {
			out = append(out, "color")
		}
		for _, k := range cards.SplitKeywordList(p["Keywords"]) {
			out = append(out, "grant:"+grantKeyword(k))
		}
		out = append(out, p2dAnimateExtra(p)...)
		if len(out) == 0 && !p2dAnimateCarrier(p) {
			out = append(out, "animate")
		}
		return dedup(out)
	case "Effect":
		return effectAPI(a, svars)
	case "DelayedTrigger":
		return []string{"delayed"}
	case "Play":
		return []string{"play"}
	case "AddPhase":
		return []string{"addphase"}
	case "EndTurn":
		return []string{"endturn"}
	case "WinsGame":
		return []string{"wingame"}
	case "LosesGame":
		return []string{"losegame"}
	case "ChangeTargets":
		return []string{"changetargets"}
	case "Poison":
		return []string{"counters:poison:" + literalOr(p["Num"], "*")}
	case "Forage":
		return []string{"forage"}
	case "Regenerate", "RegenerateAll":
		return []string{"regenerate"}
	case "SetState":
		if p["Mode"] == "Transform" || p["Mode"] == "" {
			return []string{"transform"}
		}
		return []string{"setstate:" + strings.ToLower(p["Mode"])}
	case "Investigate":
		return []string{"investigate"}
	case "Seek":
		return []string{"seek"}
	case "Proliferate":
		return []string{"proliferate"}
	case "PreventDamage", "PreventDamageAll":
		return []string{"prevent"}
	case "Amass":
		return []string{"amass"}
	case "MakeCard":
		return []string{"conjure"}
	case "RollDice":
		return []string{"dice"}
	case "FlipCoin":
		return []string{"coin"}
	case "Clone":
		return []string{"copy"}
	case "Goad":
		if p["NoLonger"] == "True" {
			return []string{"ungoad"}
		}
		return []string{"goad"}
	case "BecomeMonarch":
		return []string{"monarch"}
	case "AddTurn":
		return []string{"extraturn"}
	case "Protection", "ProtectionAll":
		return []string{"grant:protection"}
	case "Debuff":
		return []string{"loseabilities"}
	case "RingTemptsYou":
		return []string{"tempt"}
	case "TapOrUntap", "TapOrUntapAll":
		return []string{"tapuntap"}
	case "Connive":
		return []string{"connive"}
	case "Phases":
		return []string{"phaseout"}
	case "NameCard", "ChooseSource":
		return nil
	case "Charm", "GenericChoice", "Branch", "Repeat", "RepeatEach", "ImmediateTrigger",
		"Cleanup", "StoreSVar", "ChooseCard", "ChoosePlayer", "ChooseType", "ChooseColor",
		"ChooseNumber", "TwoPiles", "ReplaceEffect", "ReplaceCounter", "ReplaceToken",
		"DamageResolve", "Reveal", "RevealHand", "Shuffle", "Pump0":
		return nil
	}
	if f, ok := extAPI[a.API]; ok {
		return f(a, svars)
	}
	return []string{unmapped("API." + a.API)}
}

// changeZoneToken maps a ChangeZone: a library search ("search:<dest>")
// or a move ("move:<origin>><dest>").
func changeZoneToken(api string, p map[string]string) string {
	o := strings.ToLower(strings.TrimSpace(strings.SplitN(p["Origin"], ",", 2)[0]))
	d := strings.ToLower(strings.TrimSpace(p["Destination"]))
	if api == "ChangeZone" && p["Defined"] == "" && c2HiddenSearch(p["Origin"]) {
		// "Search your library and graveyard": one pool, read as a search
		// whichever origin is listed first.
		o = "library"
	}
	if api == "ChangeZone" && o == "library" && p["Defined"] == "" && (p["ValidTgts"] == "" || c2PlayerTargets(p["ValidTgts"])) {
		return "search:" + d
	}
	if t, ok := gorgeHandToLibrary(api, o, d, p); ok {
		return t
	}
	if o == "" || o == "any" || o == "all" {
		// Any origin: matched against any origin (wildMatch).
		o = "*"
	}
	return "move:" + o + ">" + d
}

// effectAPI maps an Effect: the statics it creates, mapped like printed
// statics (a "can't be blocked this turn" is the same rule either way),
// "delayed" for its triggers, "effect" for anything else.
func effectAPI(a *cards.SA, svars map[string]string) []string {
	var out []string
	for _, name := range strings.Split(a.Params["StaticAbilities"], ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		body, ok := svars[name]
		if !ok {
			out = append(out, "effect")
			continue
		}
		st, ok := cards.ParseStaticLine(body)
		if !ok {
			out = append(out, "effect")
			continue
		}
		eff := gorgeStaticEffects(st.Mode, st.Params)
		for _, t := range eff {
			if IsUnmapped(t) {
				eff = []string{"effect"}
				break
			}
		}
		out = append(out, eff...)
	}
	for _, name := range strings.Split(a.Params["Triggers"], ",") {
		if strings.TrimSpace(name) != "" && !effectCleanupTrigger(svars, name) {
			out = append(out, "delayed")
			break
		}
	}
	out = append(out, effectReplB(a, svars)...)
	if len(out) == 0 {
		out = append(out, "effect")
	}
	return out
}
