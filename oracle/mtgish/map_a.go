package mtgish

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit A: the cross-check's cost and threshold blind spots. Each family
// below used to be summarised without the number or cost that distinguishes
// one printing from another, so an encoding that got it wrong compared
// equal:
//
//   - Class (and Talent) level-up costs: "class:<level>:<cost>" keywords,
//     one per level line (T1 keywords, beside the cost keywords like
//     kicker:{1}{W}). mtgish numbers a level by its position (its first
//     ClassAbility is level 2); Forge names it on the K:Class line and
//     gorge compiles it to a ClassLevelUp ability carrying Level$ and Cost$.
//   - Spree and Tiered mode costs: one "spree:<cost>" / "tiered:<cost>"
//     keyword per mode, in place of the bare keyword. Forge writes the cost
//     as the mode's ModeCost$; mtgish as the SpreeAction's Cost.
//   - Spacecraft and Planet STATION thresholds: one "station:<N>+" keyword
//     per threshold, the abilities under it summarised as printed (the
//     keywords under one threshold as one static granting them, plus "type"
//     when the threshold animates), and the animated P/T as the face's P/T.
//   - Putting N cards from hand on top of the library (Brainstorm): the
//     hand-to-library move carries its count, "move:hand>library:<n>".
//
// MDFC back faces needed no change: every face's mana cost is already
// compared (T1 mana_cost), for every layout.

func init() {
	// mtgish side.
	extRule["ClassAbilities"] = mtgishClassAbilities
	extRule["StationCharged"] = mtgishStation
	extRule["StationChargedAnimate"] = mtgishStation
	extAction["PutNumCardsFromHandOnTopOfLibraryAnyOrder"] = func(e *effects, n node) {
		e.add(handToLibrary(gameNumber(argN(n, 0))))
	}

	// gorge side.
	extKeyword["Class"] = gorgeClassLevels
	extKeyword["Spree"] = func(g *gorgeNorm, _ string) { gorgeModeCosts(g, "spree") }
	extKeyword["Tiered"] = func(g *gorgeNorm, _ string) { gorgeModeCosts(g, "tiered") }
}

// classLevel is the keyword token for one Class level line.
func classLevel(level int, cost string) string {
	return "class:" + strconv.Itoa(level) + ":" + cost
}

// mtgishClassAbilities maps ClassAbilities: each ClassAbility is the next
// level, from 2 (level 1's abilities are printed directly). Its rules are
// summarised as if printed, as gorge keeps a Class grant's expansion.
func mtgishClassAbilities(m *mtgishNorm, r any) {
	for i, ca := range list(rawArgs(r)) {
		m.kw(classLevel(i+2, mtgishCost(argN(ca, 0))))
		m.rules(list(argN(ca, 1)))
	}
}

// gorgeClassLevels maps one K:Class:<level>:<cost>:<grants> line to its
// level-up ability as gorge compiled it (kwClass: API ClassLevelUp, Level$,
// Cost$). A line whose activator the compile did not mint emits nothing, so
// the mtgish side's level shows as a difference.
func gorgeClassLevels(g *gorgeNorm, param string) {
	lv, _, _ := strings.Cut(param, ":")
	lv = strings.TrimSpace(lv)
	for _, a := range g.f.Abilities {
		if a.API == "ClassLevelUp" && strings.TrimSpace(a.Params["Level"]) == lv {
			n, err := strconv.Atoi(lv)
			if err != nil {
				g.kw(unmapped("kw:Class"))
				return
			}
			g.kw(classLevel(n, gorgeCost(a.Params["Cost"])))
			return
		}
	}
}

// mtgishModeCosts emits one "<kw>:<cost>" keyword per Spree/Tiered mode.
func mtgishModeCosts(m *mtgishNorm, kw string, r any) {
	modes := list(rawArgs(r))
	if len(modes) == 0 {
		m.kw(kw)
		return
	}
	for _, x := range modes {
		m.kw(kw + ":" + mtgishCost(argN(x, 0)))
	}
}

// gorgeModeCosts emits one "<kw>:<cost>" keyword per mode of the face's
// spell Charm, reading each mode's ModeCost$. A face without one keeps the
// bare keyword, so the mtgish side's costs show as a difference.
func gorgeModeCosts(g *gorgeNorm, kw string) {
	n := 0
	for _, a := range g.f.Abilities {
		if a.Kind != "SP" || a.API != "Charm" {
			continue
		}
		for _, name := range cards.SplitModeNames(a.Params["Choices"]) {
			mode := cards.ResolveSVar(g.svars(), name)
			if mode == nil {
				continue
			}
			cost := "*"
			if c := strings.TrimSpace(mode.Params["ModeCost"]); c != "" {
				cost = gorgeCost(c)
			}
			g.kw(kw + ":" + cost)
			n++
		}
	}
	if n == 0 {
		g.kw(kw)
	}
}

// stationToken is the keyword token for one STATION threshold.
func stationToken(n string) string { return "station:" + n + "+" }

// mtgishStation maps StationCharged(range, rules) and
// StationChargedAnimate(range, rules, PT): the threshold keyword, the
// keywords under it (and the animation) as one static, every other rule as
// printed, and the animated P/T as the face's.
func mtgishStation(m *mtgishNorm, r any) {
	th := "*"
	if rg := argN(r, 0); variantOf(rg) == "ValueOrBigger" {
		th = gameNumber(rawArgs(rg))
	}
	m.kw(stationToken(th))
	n := &mtgishNorm{s: &Summary{}}
	n.rules(list(argN(r, 1)))
	m.s.Abilities = append(m.s.Abilities, n.s.Abilities...)
	var grant []string
	for _, k := range n.s.Keywords {
		grant = append(grant, "grant:"+keywordHeadToken(k))
	}
	if variantOf(r) == "StationChargedAnimate" {
		grant = append(grant, "type")
		if pt := argN(r, 2); pt != nil && m.s.PT == "" {
			m.s.PT = strconv.Itoa(intOr(argN(pt, 0))) + "/" + strconv.Itoa(intOr(argN(pt, 1)))
		}
	}
	if len(grant) > 0 {
		m.ability(Ability{Kind: "static", Effects: dedup(grant)})
	}
}

// stationAffected matches a STATION threshold static's Affected$: the
// script's "AffectedDefined$ Self | Affected$ Card.counters_GE<N>_CHARGE",
// which gorge compiles to "Affected$ Card.Self+counters_GE<N>_CHARGE".
var stationAffected = regexp.MustCompile(`^Card\.(Self\+)?counters_GE(\d+)_CHARGE$`)

// stationGrants are the Continuous parameters that carry the abilities a
// STATION threshold grants; each is summarised as the printed ability.
var stationGrants = []string{"AddStaticAbility", "AddTrigger", "AddAbility"}

// gorgeStation summarises a STATION threshold static (a Station face's
// "Mode$ Continuous | AffectedDefined$ Self | Affected$
// Card.counters_GE<N>_CHARGE" grant) the way the card prints it, and
// reports whether st was one.
func gorgeStation(g *gorgeNorm, st *cards.Static) bool {
	p := st.Params
	if st.Mode != "Continuous" || !hasKeywordHead(g.f, "Station") {
		return false
	}
	m := stationAffected.FindStringSubmatch(p["Affected"])
	if m == nil || (m[1] == "" && p["AffectedDefined"] != "Self") {
		return false
	}
	g.kw(stationToken(m[2]))
	// What the threshold does to the permanent itself (keywords, the
	// creature type): one static, as mtgish's keyword grant.
	rest := map[string]string{}
	for k, v := range p {
		rest[k] = v
	}
	for _, k := range stationGrants {
		delete(rest, k)
	}
	delete(rest, "AffectedDefined")
	if eff := gorgeStaticEffects(st.Mode, rest); len(eff) > 0 && !(len(eff) == 1 && eff[0] == unmapped("Static.Continuous")) {
		g.ability(Ability{Kind: "static", Effects: eff})
	}
	sv := g.svars()
	for _, name := range cards.SplitGrantNames(p["AddStaticAbility"]) {
		inners, ok := cards.ParseStaticLines(sv[name])
		if !ok {
			g.ability(Ability{Kind: "static", Effects: []string{unmapped("Station.AddStaticAbility")}})
			continue
		}
		for i := range inners {
			g.static(&inners[i])
		}
	}
	for _, name := range cards.SplitGrantNames(p["AddTrigger"]) {
		t, ok := cards.ParseTriggerLine(sv[name])
		if !ok {
			g.ability(Ability{Kind: unmapped("Station.AddTrigger")})
			continue
		}
		g.trigger(&t)
	}
	for _, name := range cards.SplitGrantNames(p["AddAbility"]) {
		a := cards.ResolveSVar(sv, name)
		if a == nil {
			g.ability(Ability{Kind: unmapped("Station.AddAbility")})
			continue
		}
		g.sa(a)
	}
	return true
}

// hasKeywordHead reports whether the face prints a keyword line with head.
func hasKeywordHead(f *cards.Face, head string) bool {
	for _, k := range f.Keywords {
		if strings.EqualFold(cards.KeywordHead(k), head) {
			return true
		}
	}
	return false
}

// handToLibrary is the token for putting n cards from a hand into a
// library; the count is compared, as discard's is.
func handToLibrary(n string) string { return "move:hand>library:" + n }

// gorgeHandToLibrary maps a ChangeZone from hand to library: its ChangeNum$
// literal, 1 when absent (Forge's default), "*" for a computed count or a
// Defined$ set. ok=false for any other ChangeZone, and for ChangeZoneAll (a
// whole hand: the amount-free move).
func gorgeHandToLibrary(api, origin, dest string, p map[string]string) (string, bool) {
	if api != "ChangeZone" || origin != "hand" || dest != "library" {
		return "", false
	}
	n := p["ChangeNum"]
	switch {
	case n != "":
		n = literalOr(n, "*")
	case p["Defined"] != "":
		n = "*"
	default:
		n = "1"
	}
	return handToLibrary(n), true
}
