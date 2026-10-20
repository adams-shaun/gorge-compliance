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
		if t := g.idx.gorgeTargets(a); t != "" {
			e.targets = append(e.targets, t)
		}
		e.add(gorgeAPIEffects(a, g.svars())...)
		if a.API == "Effect" {
			// The triggers an effect creates run their own effects, as
			// mtgish's CreateTriggerUntil actions do.
			for _, name := range strings.Split(a.Params["Triggers"], ",") {
				if body, ok := g.svars()[strings.TrimSpace(name)]; ok {
					if t, ok := cards.ParseTriggerLine(body); ok {
						g.chain(e, cards.ResolveSVar(g.svars(), t.Params["Execute"]), depth+1)
					}
				}
			}
		}
		if a.Params["ReplaceDyingDefined"] != "" {
			// "If that creature would die this turn, exile it instead."
			e.add("effect")
		}
		for _, ref := range svarRefs(a) {
			g.chain(e, cards.ResolveSVar(g.svars(), ref), depth+1)
		}
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
	if vt == "" && tt == "" {
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
	if tt != "" && (vt == "" || strings.Contains(zone, "Stack")) {
		cls = nil
		for _, t := range strings.Split(tt, ",") {
			t = strings.TrimSpace(t)
			if i := strings.IndexByte(t, '.'); i >= 0 {
				t = t[:i]
			}
			switch t {
			case "Spell":
				cls = append(cls, clsSpell)
			case "Activated", "Triggered":
				cls = append(cls, clsAbility)
			case "SpellAbility":
				cls = append(cls, clsSpell, clsAbility)
			}
		}
	}
	min := literalOr(p["TargetMin"], "1")
	max := literalOr(p["TargetMax"], "1")
	if p["TargetMin"] == "" && p["TargetMax"] != "" {
		min = "1"
	}
	return targetToken(cls, countRange(min, max))
}

// validClasses maps a ValidTgts$ (or Enchant) list to target classes.
func (idx TypeIndex) validClasses(vt, zone string) []string {
	var out []string
	for _, alt := range strings.Split(vt, ",") {
		alt = strings.TrimSpace(alt)
		head := alt
		if i := strings.IndexAny(head, ".+"); i >= 0 {
			head = head[:i]
		}
		w := typeWord(head)
		switch {
		case w == "any":
			out = append(out, clsAny)
		case w == "player" || w == "opponent":
			out = append(out, clsPlayer)
		case strings.Contains(zone, "Graveyard"):
			out = append(out, clsGraveyard)
		case strings.Contains(zone, "Exile"):
			out = append(out, clsExile)
		case strings.Contains(zone, "Stack"):
			out = append(out, clsSpell)
		case permanentTypes[w]:
			out = append(out, w)
		case idx[w] != "":
			out = append(out, idx[w])
		case w == "":
		default:
			out = append(out, clsPermanent)
		}
	}
	// "permanent" alongside a narrower class adds nothing a target-kind
	// comparison can use; mtgish's filter extraction drops it the same way.
	ds := dedup(out)
	if len(ds) > 1 {
		var narrowed []string
		for _, c := range ds {
			if c != clsPermanent {
				narrowed = append(narrowed, c)
			}
		}
		ds = narrowed
	}
	return ds
}

// gorgeAPIEffects maps one SA's API to its effect verbs.
func gorgeAPIEffects(a *cards.SA, svars map[string]string) []string {
	p := a.Params
	switch a.API {
	case "DealDamage", "DamageAll":
		return []string{"damage:" + literalOr(p["NumDmg"], "*")}
	case "EachDamage":
		return []string{"damage:*"}
	case "Destroy", "DestroyAll":
		return []string{"destroy"}
	case "ChangeZone", "ChangeZoneAll":
		return []string{changeZoneToken(a.API, p)}
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
		for _, k := range cards.SplitKeywordList(p["PumpKeywords"]) {
			out = append(out, "grant:"+grantKeyword(k))
		}
		return out
	case "CopyPermanent":
		out := []string{"tokencopy:" + literalOr(p["NumCopies"], "1")}
		switch p["AtEOT"] {
		case "Exile":
			out = append(out, "delayed", "move:battlefield>exile")
		case "Sacrifice":
			out = append(out, "delayed", "sacrifice")
		}
		return out
	case "PutCounter", "PutCounterAll":
		if p["CounterType"] == "EachFromSource" {
			return []string{"counters:*"}
		}
		ct := p["CounterType"]
		if ct == "" && p["CounterTypes"] != "" {
			// CounterTypes$: one of each listed type; ChosenFromList is
			// one chosen from TypeList$ (each listed, as alternatives
			// merge).
			ct = "EACH " + strings.Replace(p["CounterTypes"], "ChosenFromList", p["TypeList"], 1)
		}
		return gorgeCounterTokens(ct, literalOr(p["CounterNum"], "1"))
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
		if p["DestinationZone"] == "Exile" && (p["ChangeNum"] == "All" || p["ChangeNum"] == "") {
			// "Exile the top N cards" is a move, not a look.
			return []string{"move:library>exile"}
		}
		if p["ChangeNum"] == "All" && p["DestinationZone"] != "" {
			// "Put the top card into your hand": a move (revealed first,
			// a look as well).
			out := []string{"move:library>" + strings.ToLower(p["DestinationZone"])}
			if p["Reveal"] == "True" {
				out = append(out, "dig:"+literalOr(p["DigNum"], "1"))
			}
			return out
		}
		// "Look at the top N, put M into <zone>, the rest into <zone2>".
		out := []string{"dig:" + literalOr(p["DigNum"], "1")}
		dest := p["DestinationZone"]
		if dest == "" {
			dest = "Hand"
		}
		out = append(out, "move:library>"+strings.ToLower(dest))
		n, m := literalOr(p["DigNum"], "1"), literalOr(p["ChangeNum"], "1")
		if n == "*" || m == "*" || n != m {
			rest := p["DestinationZone2"]
			if rest == "" {
				rest = "Library"
			}
			out = append(out, "move:library>"+strings.ToLower(rest))
		}
		return out
	case "DigUntil":
		if p["FoundDestination"] == "Exile" || p["RevealedDestination"] == "Exile" {
			return []string{"move:library>exile"}
		}
		out := []string{"diguntil"}
		for _, k := range []string{"FoundDestination", "RevealedDestination"} {
			if d := p[k]; d != "" {
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
		if p["RemoveAllAbilities"] == "True" {
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
		for _, k := range cards.SplitKeywordList(p["Keywords"]) {
			out = append(out, "grant:"+grantKeyword(k))
		}
		if len(out) == 0 {
			out = append(out, "animate")
		}
		return out
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
	if api == "ChangeZone" && o == "library" && p["Defined"] == "" && p["ValidTgts"] == "" {
		return "search:" + d
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
	if a.Params["Triggers"] != "" {
		out = append(out, "delayed")
	}
	if len(out) == 0 {
		out = append(out, "effect")
	}
	return out
}
