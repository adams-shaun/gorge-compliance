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
		{"CARDNAME must be blocked by two or more creatures if able.", []string{"rule:mustbeblockedbyatleastnumberdefenders"}},
		{"CARDNAME must be blocked by exactly one creature if able.", []string{"rule:mustbeblockedbyexactlyonedefender"}},
		{"CARDNAME can only attack alone.", []string{"rule:canonlyattackalone"}},
		{"CARDNAME can't be destroyed by lethal damage unless lethal damage dealt by a single source is marked on it.",
			[]string{"rule:canonlybedestroyedbylethaldamagefromasinglesource"}},
		{"CARDNAME can't attack unless a creature with greater power also attacks.", []string{"rule:cantattackunlessanothercreatureattacks"}},
		{"CARDNAME can't attack unless a black or green creature also attacks.", []string{"rule:cantattackunlessanothercreatureattacks"}},
		{"CARDNAME can't block unless a creature with greater power also blocks.", []string{"rule:cantblockunlessanotherdefender"}},
		{"CARDNAME can't attack unless at least two other creatures attack.", []string{"rule:cantattackunlessanumberofothercreatureattacks"}},
		{"CARDNAME can't block unless at least two other creatures block.", []string{"rule:cantblockunlessotherdefenders"}},
		{"While you're searching your library, you may cast CARDNAME from your library.", []string{"mayplay"}},
		{"Once each turn, you may pay {1} to reroll one or more dice you rolled.", []string{"player:onceeachturnmaypaytorerollanynumberofdicerolled"}},
		{"After you roll a die, you may pay 1 life. If you do, increase or decrease the result by 1. Do this only once each turn.",
			[]string{"player:maypaytoincrementordecrementadiceroll"}},
		{"After you roll a die, you may remove a +1/+1 counter from Xenosquirrels. If you do, increase or decrease the result by 1.",
			[]string{"player:maypaytoincrementordecrementadiceroll"}},
		{"As an additional cost to cast creature spells, you may pay any amount of mana. If you do, that creature enters with that many additional +1/+1 counters on it.",
			[]string{"player:maypayadditionalcosttocastspellsforeffect"}},
	} {
		toks := ks.toks
		extKeyword[ks.head] = func(g *gorgeNorm, _ string) {
			for _, t := range toks {
				g.ability(Ability{Kind: "static", Effects: []string{t}})
			}
		}
	}
	// "Before drawing your opening hand, choose ...": a pregame action
	// (mtgish's BeforeDrawingOpeningHand).
	extKeyword["Before drawing your opening hand, choose 1, 2, or 3."] = func(g *gorgeNorm, _ string) {
		g.ability(Ability{Kind: "pregame"})
	}
	// "Spend only colored mana on X": a restriction on casting the card
	// itself, castRestriction's twin.
	extKeyword["Spend only colored mana on X. No more than one mana of each color may be spent this way."] = func(*gorgeNorm, string) {}
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
	case "NewGame":
		// "At the beginning of the game": a pregame action (a Vanguard's),
		// mtgish's AsGameBegins; a StoreSVar one is bookkeeping.
		if a := cards.ResolveSVar(g.svars(), p["Execute"]); a != nil && a.API != "StoreSVar" && a.API != "Cleanup" {
			g.ability(Ability{Kind: "pregame"})
		}
		return true
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
// is this card or the creature it is attached to: "whenever this (or
// enchanted creature) blocks a creature", Forge's spelling of a blocks
// trigger that names the blocked attacker.
func selfBlocker(p map[string]string) bool {
	return triggerSubject(p["ValidBlocker"]) && !triggerSubject(p["ValidCard"])
}

// triggerSubject: a Valid* filter naming the source itself or the object
// it is attached to.
func triggerSubject(v string) bool {
	return strings.HasPrefix(v, "Card.Self") || strings.Contains(v, ".EnchantedBy") ||
		strings.Contains(v, ".EquippedBy") || strings.Contains(v, ".AttachedBy")
}

// keywordAbilities emits the printed abilities a bare keyword line carries
// in its parameter, as mtgish carries them beside the keyword: an
// Attraction's visit ability (Visit:<SVar>), and haunt's "when the creature
// this haunts dies" ability (Haunt:<SVar>; mtgish classes the creature
// form, "when this enters or the creature it haunts dies", as dies too)
// together with the haunt spell's own effect.
func (g *gorgeNorm) keywordAbilities(head, param string) {
	switch head {
	case "Tribute":
		// "When this enters, if tribute wasn't paid, ...": Forge's Tribute
		// runs the TrigNotTribute SVar; mtgish writes the printed trigger.
		a := cards.ResolveSVar(g.svars(), "TrigNotTribute")
		if a == nil {
			return
		}
		e := &effects{}
		g.chain(e, a, 1)
		g.ability(Ability{Kind: "triggered", Event: "etb", Targets: e.targets, Effects: e.effects})
		return
	}
	if param == "" {
		return
	}
	switch head {
	case "Visit":
		e := &effects{}
		g.chain(e, cards.ResolveSVar(g.svars(), param), 1)
		g.ability(Ability{Kind: "triggered", Event: "visit", Targets: e.targets, Effects: e.effects})
	case "Haunt":
		a := cards.ResolveSVar(g.svars(), param)
		if a == nil {
			return
		}
		if !hasType(g.f, "Creature") {
			s := &effects{}
			g.chain(s, a, 0)
			g.ability(Ability{Kind: "spell", Targets: s.targets, Effects: s.effects})
		}
		e := &effects{}
		g.chain(e, a, 1)
		g.ability(Ability{Kind: "triggered", Event: "dies", Targets: e.targets, Effects: e.effects})
	}
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
	return (p["ValidCard"] == "Card.Self" || p["Affected"] == "Card.Self") && p["Type"] == "Spell"
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

// splitStatics splits every static ability's rule and player tokens into
// statics of their own, on both sides. A restriction (can't block, must be
// blocked, goaded; can't cast spells, has no maximum hand size) is one
// static per restriction in a script (one S: line per static mode) and in
// mtgish's PermanentRuleEffect, but one sentence can carry several in
// mtgish's PlayerEffect list or beside a layer effect in a script's
// Continuous (AddHiddenKeyword$, Goad$, CantHaveKeyword$). A spell effect
// ("spells you cast have ...", one S: line each) splits the same way. The remaining
// (layer) tokens stay one static.
func splitStatics(s *Summary) {
	var out []Ability
	split := false
	for _, a := range s.Abilities {
		if a.Kind != "static" {
			out = append(out, a)
			continue
		}
		var rest, own []string
		for _, t := range a.Effects {
			if strings.HasPrefix(t, "rule:") || strings.HasPrefix(t, "player:") || t == "goad" || strings.HasPrefix(t, "spell:") {
				// ("Is goaded" is a rule too: mtgish's IsGoadedByPlayer.)
				own = append(own, t)
			} else {
				rest = append(rest, t)
			}
		}
		if len(own) == 0 || len(own) == 1 && len(rest) == 0 {
			out = append(out, a)
			continue
		}
		split = true
		if len(rest) > 0 {
			out = append(out, Ability{Kind: "static", Targets: a.Targets, Effects: rest})
		}
		for _, t := range dedup(own) {
			out = append(out, Ability{Kind: "static", Effects: []string{t}})
		}
	}
	if split {
		s.Abilities = out
	}
}

// selfGrantAbilities expands a Continuous static that grants its own card
// activated, triggered or replacement abilities ("as long as ..., this has
// '{T}: ...'": max speed, station, level up) into those abilities, the way
// the printed card and mtgish (a MaxSpeed/Station/Level wrapper around the
// rule itself) carry them. It returns the static's remaining effect tokens,
// one "grant:ability" fewer per expanded grant. A granted static ability
// stays a grant in the band's static, where mtgish folds it too.
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
	for _, name := range strings.Split(p["AddTrigger"], "&") {
		tp := svarParams(g.svars()[strings.TrimSpace(name)])
		if tp["Mode"] == "" {
			continue
		}
		g.trigger(&cards.Trigger{Mode: tp["Mode"], Params: tp})
		n++
	}
	for _, k := range []string{"AddReplacementEffects", "AddReplacementEffect"} {
		for _, name := range strings.Split(p[k], "&") {
			rp := svarParams(g.svars()[strings.TrimSpace(name)])
			if rp["Event"] == "" {
				continue
			}
			g.repl(&cards.Repl{Event: rp["Event"], Params: rp})
			n++
		}
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
		if p["IsDamage"] == "True" && strings.HasPrefix(p["Result"], "LT") {
			// "Damage that would reduce your life total to less than N
			// reduces it to N instead."
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
// up" (or "this or enchanted creature attacks") ability: same rule and
// event variant, identical actions, one event on the permanent itself and
// the other on Other(...) permanents or the host. Forge writes one trigger
// (ValidCard$ Card.Self,<Type>.Other / Card.Self,Creature.EnchantedBy).
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
			// "... enters" and "... enters under your control" are one event.
			if len(aa) < 2 || len(aa) != len(bb) || !strings.HasPrefix(variantOf(bb[0]), variantOf(aa[0])) || !otherEvent(bb[0]) && !hostEvent(bb[0]) {
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

// hostEvent: the event's subject is SinglePermanent(HostPermanent()), the
// permanent an Aura or Equipment is attached to ("whenever this creature
// or enchanted creature attacks", a bestow card).
func hostEvent(ev any) bool {
	sub := firstArg(ev)
	return variantOf(sub) == "SinglePermanent" && variantOf(rawArgs(sub)) == "HostPermanent"
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
func (m *mtgishNorm) levelStatic(pt any, rules []any) {
	n := &mtgishNorm{s: &Summary{}}
	n.rules(rules)
	m.cdaPower = m.cdaPower || n.cdaPower
	m.cdaToughness = m.cdaToughness || n.cdaToughness
	// A level's P/T box is the card's level structure, not a printed
	// ability: the gorge side drops a level static's SetPower$ and
	// SetToughness$ (selfGrantStatic), so no "setpt" here either.
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
	if len(eff) > 0 {
		m.ability(Ability{Kind: "static", Effects: eff})
	}
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

func hasType(f *cards.Face, t string) bool {
	for _, x := range f.Types {
		if x == t {
			return true
		}
	}
	return false
}

// svarParams parses a granted trigger or replacement SVar's
// "Key$ Value | Key$ Value" line into its parameters (nil for anything
// else).
func svarParams(line string) map[string]string {
	if !strings.Contains(line, "$") {
		return nil
	}
	p := map[string]string{}
	for _, part := range strings.Split(line, "|") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "$")
		if !ok {
			continue
		}
		p[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return p
}

// mergeSpells folds a face's spell abilities into the one spell ability a
// printed instant or sorcery has. Forge writes an alternative way of
// casting (cleave, a second mode set) as a second SP, whose effects are
// alternatives (alt=true: branch-max merge); mtgish writes a trailing
// sentence ("If ..., ...") as a second SpellActions rule, whose effects
// add up (alt=false).
func mergeSpells(s *Summary, alt bool) {
	first := -1
	var subs []*effects
	var out []Ability
	for _, a := range s.Abilities {
		if a.Kind != "spell" {
			out = append(out, a)
			continue
		}
		subs = append(subs, &effects{targets: a.Targets, effects: a.Effects})
		if first < 0 {
			first = len(out)
			out = append(out, a)
		}
	}
	if len(subs) < 2 {
		return
	}
	e := &effects{}
	if alt {
		e.merge(subs)
	} else {
		for _, x := range subs {
			e.targets = append(e.targets, x.targets...)
			e.effects = append(e.effects, x.effects...)
		}
	}
	out[first] = Ability{Kind: "spell", Targets: e.targets, Effects: e.effects}
	s.Abilities = out
}

// zoneDuplicate reports a zone-gate wrapper (FromGraveyard, FromExile)
// whose rule repeats one of the face's top-level rules: an ability that
// also functions from that zone ("this ability triggers from your
// graveyard too"), which Forge writes once with TriggerZones$
// Battlefield,Graveyard.
func zoneDuplicate(r any, rs []any) bool {
	switch variantOf(r) {
	case "FromGraveyard", "FromExile", "FromHand":
	default:
		return false
	}
	inner := rawArgs(r)
	for _, x := range rs {
		if reflect.DeepEqual(x, inner) {
			return true
		}
	}
	return false
}

// zoneStatics are the static Rule variants that apply one effect list to
// one zone's objects: permanents, spells, cards elsewhere.
var zoneStatics = map[string]bool{
	"EachPermanentLayerEffect": true, "StackSpellsEffect": true, "CardEffect": true,
	"EachPermanentAndSpellEffect": true,
}

// mergeZoneStatics drops a zone static whose effect list repeats an earlier
// one's: "all cards that aren't on the battlefield, spells, and permanents
// are the chosen color" is one printed static that mtgish writes once per
// zone (CardEffect, StackSpellsEffect, EachPermanentLayerEffect) and Forge
// writes once, with AffectedZone$ listing the zones.
func mergeZoneStatics(rs []any) []any {
	rank := func(v string) int {
		if v == "EachPermanentLayerEffect" || v == "EachPermanentAndSpellEffect" {
			return 0 // the layer effect on permanents carries the verbs
		}
		return 1
	}
	var drop map[int]bool
	for j, b := range rs {
		if !zoneStatics[variantOf(b)] {
			continue
		}
		for i, a := range rs {
			if i == j || drop[i] || !zoneStatics[variantOf(a)] || variantOf(a) == variantOf(b) ||
				!sameEffects(argN(a, 1), argN(b, 1)) {
				continue
			}
			// b repeats a: drop b unless it outranks a (then a goes).
			if rank(variantOf(a)) < rank(variantOf(b)) || rank(variantOf(a)) == rank(variantOf(b)) && i < j {
				if drop == nil {
					drop = map[int]bool{}
				}
				drop[j] = true
				break
			}
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

// sameEffects compares two zone statics' effect lists by variant and
// arguments: each zone's enum (LayerEffect, SpellEffect, CardEffect) names
// the same change differently.
func sameEffects(a, b any) bool {
	la, lb := list(a), list(b)
	if len(la) == 0 || len(la) != len(lb) {
		return false
	}
	for i := range la {
		if variantOf(la[i]) != variantOf(lb[i]) || !reflect.DeepEqual(rawArgs(la[i]), rawArgs(lb[i])) {
			return false
		}
	}
	return true
}

// secondaryHalf reports a Secondary$ True trigger that is the second half
// of another trigger's printed ability: the same Execute$ and the same
// event class ("whenever you cast or copy" as SpellCast + SpellCopy,
// "from the command zone or the battlefield" as one trigger per zone).
// mtgish writes such an ability once (an Or event, a zone wrapper). A
// Secondary$ half of another class ("blocks or becomes blocked") stays: it
// is a second event, as mtgish's second rule.
func secondaryHalf(f *cards.Face, t *cards.Trigger) bool {
	if t.Params["Secondary"] != "True" || t.Params["Execute"] == "" {
		return false
	}
	cls := gorgeTriggerClass(t.Mode, t.Params)
	for i := range f.Triggers {
		u := &f.Triggers[i]
		if u == t || u.Params["Secondary"] == "True" || u.Params["Execute"] != t.Params["Execute"] {
			continue
		}
		if gorgeTriggerClass(u.Mode, u.Params) == cls {
			return true
		}
	}
	return false
}
