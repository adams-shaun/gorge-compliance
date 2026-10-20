package mtgish

import (
	"reflect"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit G: the ability-shape (T2) normalisations. Each helper here is called
// from one line in the normaliser it serves; the comment on each says which
// encoding convention it reconciles.

func init() {
	// Forge keyword lines that are printed static abilities, not keyword
	// abilities: mtgish writes each as a rule static (PermanentRuleEffect,
	// one static per rule; CastEffect for flash), so the gorge side emits
	// the same statics instead of a keyword.
	for _, ks := range []struct {
		head string
		toks []string
	}{
		{"You may choose not to untap CARDNAME during your untap step.", []string{"rule:maychoosenottountapduringuntap"}},
		{"MustBlock", []string{"rule:mustbeblocked"}},
		{"All creatures able to block CARDNAME do so.", []string{"rule:allcreaturesmustblockit"}},
		{"MustBeBlockedByAll", []string{"rule:allcreaturesmustblockit"}},
		{"MayFlashSac", []string{"flash"}},
		{"CantAttackOrBlockAlone", []string{"rule:cantattackalone", "rule:cantblockalone"}},
		{"CantAttackAlone", []string{"rule:cantattackalone"}},
		{"CantBlockAlone", []string{"rule:cantblockalone"}},
		{"CantAttackOrBlock", []string{"rule:cantattack", "rule:cantblock"}},
		{"You may assign CARDNAME's combat damage divided as you choose among defending player and/or any number of creatures they control.",
			[]string{"rule:mayassigncombatdamagedividedasyouchooseamongplayerorcreaturesandplaneswalkers"}},
		{"If CARDNAME is unblocked, you may have it assign its combat damage to a creature defending player controls.",
			[]string{"rule:mayassigncombatdamagetoapermanent"}},
	} {
		toks := ks.toks
		extKeyword[ks.head] = func(g *gorgeNorm, _ string) {
			for _, t := range toks {
				g.ability(Ability{Kind: "static", Effects: []string{t}})
			}
		}
	}
}

// ---- gorge side ----

// gorgeStaticTrigger handles a Static$ True trigger, reporting whether it
// consumed it. A printed triggered ability always uses the stack unless it
// is a mana ability (CR 605.1b), so a non-mana Static$ True trigger is the
// engine's bookkeeping (remembering, cleaning up, counting casts) or Forge's
// implementation of a static or replacement ability: never a printed
// triggered ability. A self "enters" one with effects is the "enters with"
// replacement it implements (kicked counters, an "as this enters" copy) and
// joins the face's enter replacement; the rest are dropped.
func (g *gorgeNorm) gorgeStaticTrigger(t *cards.Trigger, enter **Ability) bool {
	p := t.Params
	if p["Static"] != "True" {
		return false
	}
	switch t.Mode {
	case "TapsForMana", "ManaAdded":
		// A triggered mana ability: printed, and resolves at once.
		return false
	}
	if t.Mode == "ChangesZone" && p["Destination"] == "Battlefield" && strings.HasPrefix(p["ValidCard"], "Card.Self") {
		e := &effects{}
		eff := t.Effect
		if eff == nil {
			eff = cards.ResolveSVar(g.svars(), p["Execute"])
		}
		g.chain(e, eff, 1)
		if len(e.effects) > 0 {
			if *enter == nil {
				*enter = &Ability{Kind: "replacement"}
			}
			(*enter).Effects = append((*enter).Effects, e.effects...)
		}
	}
	return true
}

// selfBlocker reports an AttackerBlocked(ByCreature) trigger whose blocker
// is this card: "whenever this blocks a creature", Forge's spelling of a
// blocks trigger that names the blocked attacker.
func selfBlocker(p map[string]string) bool {
	return strings.HasPrefix(p["ValidBlocker"], "Card.Self") && !strings.HasPrefix(p["ValidCard"], "Card.Self")
}

// damagesPlayer reports a damage trigger's ValidTarget naming a player
// among its alternatives ("Player,Planeswalker", "Battle,Player").
func damagesPlayer(vt string) bool {
	for _, alt := range strings.Split(vt, ",") {
		if strings.HasPrefix(alt, "Player") || strings.HasPrefix(alt, "Opponent") || strings.HasPrefix(alt, "You") {
			return true
		}
	}
	return false
}

// selfSpellCost reports a RaiseCost static on the spell itself: an
// additional casting cost ("as an additional cost, discard a card",
// behold, waterbend) or "this spell costs {2} more if ...". mtgish keeps
// both inside the spell's casting (AdditionalCastingCost*), which the
// normaliser skips as a cost, not an ability; the gorge side skips them the
// same way.
func selfSpellCost(p map[string]string) bool {
	return p["ValidCard"] == "Card.Self" && p["Type"] == "Spell"
}

func init() {
	// "This spell can't be cast unless ...", "cast this only from your
	// graveyard": a restriction on casting the card itself, which mtgish
	// keeps in the spell's CastEffect and castRestriction skips. Other
	// CantBeCast statics ("your opponents can't cast ...") stay statics.
}

// selfCastRestriction reports a gorge static that restricts or adds to
// the cost of casting the card itself: castRestriction's twin. "This spell
// can't be cast unless ..." (CantBeCast on Card.Self), "spend only mana
// from ... to cast this spell" (ManaRestriction, always the card's own),
// and an optional additional cost of this spell (OptionalCost on
// Card.Self; mtgish's OptionalAdditionalCastingCost). The same modes on
// other cards ("your opponents can't cast ...", "as an additional cost to
// cast green spells, you may pay 2 life") stay statics.
func selfCastRestriction(mode string, p map[string]string) bool {
	switch mode {
	case "ManaRestriction":
		return true
	case "CantBeCast", "OptionalCost":
		return p["ValidCard"] == "Card.Self"
	}
	return false
}

// splitRuleStatics splits one gorge static's effect tokens into the
// statics mtgish writes for them: a rule (can't block, must be blocked,
// goaded) is its own static (PermanentRuleEffect, one per rule), separate
// from the layer effects (pump, grant) of the same sentence.
func splitRuleStatics(eff []string) [][]string {
	var rest []string
	var out [][]string
	for _, t := range eff {
		if strings.HasPrefix(t, "rule:") {
			out = append(out, []string{t})
		} else {
			rest = append(rest, t)
		}
	}
	if len(rest) > 0 || len(out) == 0 {
		out = append([][]string{rest}, out...)
	}
	return out
}

// selfGrantAbilities expands a Continuous static that grants its own card
// activated abilities ("as long as ..., this has '{T}: ...'": max speed,
// station, level up) into those abilities, the way the printed card and
// mtgish (an If/MaxSpeed/Level wrapper around the Activated rule) carry
// them. It returns the static's remaining effect tokens, one "grant:ability"
// fewer per expanded grant.
func (g *gorgeNorm) selfGrantAbilities(p map[string]string, eff []string) []string {
	if p["Affected"] != "Card.Self" && !strings.HasPrefix(p["Affected"], "Card.Self+") {
		return eff
	}
	// Only the wrappers mtgish writes as the ability itself: max speed, a
	// level band, a station threshold. Other conditional self-grants
	// ("as long as it's renowned, it has '{T}: ...'") are a layer effect
	// granting the ability on both sides.
	gate := p["IsPresent"] + " " + p["Affected"]
	if p["Condition"] != "MaxSpeed" && !strings.Contains(gate, "_LEVEL") && !strings.Contains(gate, "_CHARGE") {
		return eff
	}
	if p["AffectedZone"] != "" && p["AffectedZone"] != "Battlefield" {
		return eff
	}
	n := 0
	for _, name := range strings.Split(p["AddAbility"], "&") {
		a := cards.ResolveSVar(g.svars(), strings.TrimSpace(name))
		if a == nil || a.Kind != "AB" {
			continue
		}
		e := &effects{}
		g.chain(e, a, 1)
		g.ability(Ability{Kind: "activated", Targets: e.targets, Effects: e.effects})
		n++
	}
	if n == 0 {
		return eff
	}
	var out []string
	for _, t := range eff {
		if t == "grant:ability" && n > 0 {
			n--
			continue
		}
		out = append(out, t)
	}
	return out
}

// replStatic maps the R: events whose printed form is a static ability
// (mtgish writes a PlayerEffect/PermanentRuleEffect for them): "skip your
// draw step", "damage can't reduce your life below 1", "unspent mana
// becomes colorless", "can't be turned face up". ok=false leaves the
// replacement to repl.
func replStatic(r *cards.Repl, svars map[string]string) (eff []string, ok bool) {
	p := r.Params
	switch r.Event {
	case "BeginPhase":
		if p["Skip"] == "True" && p["Phase"] != "" {
			return []string{"player:skips" + strings.ToLower(p["Phase"]) + "step"}, true
		}
	case "LifeReduced":
		if p["Layer"] == "CantHappen" {
			return []string{"player:damagedoesntcauselifeloss"}, true
		}
		if w := cards.ResolveSVar(svars, p["ReplaceWith"]); w != nil && w.API == "ReplaceDamage" && p["ValidPlayer"] != "Opponent" {
			return []string{"player:damagethatwouldreducelifetotaltolessthannumberreducesittothatnumberinstead"}, true
		}
	case "LoseMana":
		if w := cards.ResolveSVar(svars, p["ReplaceWith"]); w != nil && w.API == "ReplaceMana" {
			if w.Params["ReplaceType"] == "C" {
				return []string{"player:unspentmanabecomescolorless"}, true
			}
			return []string{"player:unspentmanabecomescolor"}, true
		}
	case "TurnFaceUp":
		if p["Layer"] == "CantHappen" {
			return []string{"rule:cantbeturnedfaceup"}, true
		}
	}
	return nil, false
}

// ---- mtgish side ----

// castRestriction reports a CastEffect variant that restricts how this
// spell is cast ("cast this spell only during combat", "X can't be 0",
// "spend only black mana on X", an optional additional cost). Forge keeps
// these in the spell's own parameters and costs, never as a static, so they
// are casting conditions, not abilities, like an additional cost.
func castRestriction(v string) bool {
	switch {
	case strings.HasPrefix(v, "CantBeCast"), strings.HasPrefix(v, "SpendOnly"),
		strings.HasPrefix(v, "OptionalAdditionalCastingCost"), strings.HasPrefix(v, "CantSpendMana"):
		return true
	}
	switch v {
	case "XCantBeZero", "XIs":
		return true
	}
	return false
}

// mergeDamageOr folds an Or trigger's "combat damage to a player or a
// planeswalker/battle" into the one ability it is printed as (Forge: one
// DamageDone trigger with ValidTarget$ Player,Planeswalker). Other Or
// triggers ("enters or attacks") stay one ability per event, as scripts
// write them.
func mergeDamageOr(evs []string) []string {
	hasPlayer := false
	for _, e := range evs {
		if e == "combat-damage-player" {
			hasPlayer = true
		}
	}
	if !hasPlayer {
		return evs
	}
	var out []string
	for _, e := range evs {
		if e != "combat-damage" {
			out = append(out, e)
		}
	}
	return out
}

// mergeSelfOrOther drops the second of two trigger rules mtgish writes for
// one printed "whenever this or another <type> enters/leaves/is turned face
// up" ability: same rule and event variant, identical actions, one event
// on the permanent itself and the other on Other(...) permanents. Forge
// writes one trigger (ValidCard$ Card.Self,<Type>.Other).
func mergeSelfOrOther(rs []any) []any {
	var drop map[int]bool
	for i, a := range rs {
		if !strings.HasPrefix(variantOf(a), "Trigger") || !selfEvent(argN(a, 0)) {
			continue
		}
		for j, b := range rs {
			if i == j || drop[j] || variantOf(a) != variantOf(b) {
				continue
			}
			aa, bb := argsOf(a), argsOf(b)
			if len(aa) < 2 || len(aa) != len(bb) || variantOf(aa[0]) != variantOf(bb[0]) || !otherEvent(bb[0]) {
				continue
			}
			if !reflect.DeepEqual(aa[len(aa)-1], bb[len(bb)-1]) || !reflect.DeepEqual(aa[1:len(aa)-1], bb[1:len(bb)-1]) {
				continue
			}
			if drop == nil {
				drop = map[int]bool{}
			}
			drop[j] = true
		}
	}
	if drop == nil {
		return rs
	}
	out := make([]any, 0, len(rs)-len(drop))
	for i, r := range rs {
		if !drop[i] {
			out = append(out, r)
		}
	}
	return out
}

// selfEvent: the event's subject is SinglePermanent(ThisPermanent()).
func selfEvent(ev any) bool {
	sub := firstArg(ev)
	return variantOf(sub) == "SinglePermanent" && variantOf(rawArgs(sub)) == "ThisPermanent"
}

// otherEvent: the event's subject is And([Other(...), ...]).
func otherEvent(ev any) bool {
	sub := firstArg(ev)
	if variantOf(sub) != "And" {
		return false
	}
	for _, x := range list(rawArgs(sub)) {
		if variantOf(x) == "Other" {
			return true
		}
	}
	return false
}

// firstArg is a variant's first positional argument (its only one for a
// one-field variant).
func firstArg(x any) any {
	if a := argsOf(x); len(a) > 0 {
		return a[0]
	}
	return nil
}

// levelStatic summarises one LevelUp level the way the script writes it:
// one static carrying the level's P/T and the keywords it grants, with the
// level's other abilities alongside.
func (m *mtgishNorm) levelStatic(rules []any) {
	n := &mtgishNorm{s: &Summary{}}
	n.rules(rules)
	m.cdaPower = m.cdaPower || n.cdaPower
	m.cdaToughness = m.cdaToughness || n.cdaToughness
	eff := []string{"setpt"}
	for _, a := range n.s.Abilities {
		if a.Kind == "static" {
			eff = append(eff, a.Effects...)
		} else {
			m.ability(a)
		}
	}
	for _, k := range n.s.Keywords {
		eff = append(eff, "grant:"+keywordHeadToken(k))
	}
	m.ability(Ability{Kind: "static", Effects: eff})
}

// stationAnimate summarises StationChargedAnimate: "STATION N+: <keywords>,
// P/T" makes the Spacecraft an artifact creature with those keywords: one
// static granting them plus the type change, as scripts write it
// (Continuous AddType$ Creature, AddKeyword$ ...).
func (m *mtgishNorm) stationAnimate(rules []any) {
	n := &mtgishNorm{s: &Summary{}}
	n.rules(rules)
	var eff []string
	for _, a := range n.s.Abilities {
		if a.Kind == "static" {
			eff = append(eff, a.Effects...)
		} else {
			m.ability(a)
		}
	}
	for _, k := range n.s.Keywords {
		eff = append(eff, "grant:"+keywordHeadToken(k))
	}
	eff = append(eff, "type")
	m.ability(Ability{Kind: "static", Effects: eff})
}

// enterReplacement adds one AsPermanentEnters rule's effects to the face's
// single "as this enters" replacement. A card printing "This enters
// tapped. As it enters, choose a color." carries two AsPermanentEnters
// rules; the gorge side groups every self enter replacement into one
// ability (gorgeFace), so this side does too.
func (m *mtgishNorm) enterReplacement(eff []string) {
	if m.enter > 0 {
		a := &m.s.Abilities[m.enter-1]
		a.Effects = append(a.Effects, eff...)
		return
	}
	m.ability(Ability{Kind: "replacement", Effects: eff})
	m.enter = len(m.s.Abilities)
}
