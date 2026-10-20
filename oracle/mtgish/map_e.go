package mtgish

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit E: the T1 (characteristics) normaliser classes. Each block names the
// class it closes; the census behind it is in the commit message.

func init() {
	// mtgish writes these keyword abilities of an instant or sorcery as an
	// action inside the spell's actions; Forge (and the printed card) list
	// them as keywords. mtgishKeywordActions adds the keyword on the mtgish
	// side; the action itself is no effect verb on either side.
	for v := range mtgishKeywordActions {
		extAction[v] = func(*effects, node) {}
	}

	// MayFlashSac ("you may cast this as though it had flash; if you cast
	// it when a sorcery couldn't have been cast, sacrifice it"): the cast
	// permission is the static flash mtgish writes (MayFlashCost's twin);
	// the sacrifice rider is a delayed trigger neither side summarises.
	extKeyword["MayFlashSac"] = func(g *gorgeNorm, _ string) {
		g.ability(Ability{Kind: "static", Effects: []string{"flash"}})
	}

	// Forge's sentence keywords, canonicalised by gorge's
	// cards.CanonicalKeywordLine, are combat restrictions printed as rules
	// text: mtgish writes one PermanentRule static per restriction.
	for head, rules := range gorgeRestrictionKeywords {
		rules := rules
		extKeyword[head] = func(g *gorgeNorm, _ string) {
			for _, r := range rules {
				g.ability(Ability{Kind: "static", Effects: []string{"rule:" + r}})
			}
		}
	}
}

// mtgishKeywordActions are the Action variants that are a spell's keyword
// ability, by keyword token (Forge's K: head, lowercased).
var mtgishKeywordActions = map[string]string{
	"Cipher": "cipher", "Paradigm": "paradigm", "Epic": "epic", "Ascend": "ascend",
}

// gorgeRestrictionKeywords maps gorge's canonical sentence-keyword heads to
// the PermanentRule variants (ruleWord spelling) mtgish writes for them.
var gorgeRestrictionKeywords = map[string][]string{
	"MustBlock":              {"mustbeblocked"},
	"CantAttackOrBlock":      {"cantattack", "cantblock"},
	"CantAttackOrBlockAlone": {"cantattackalone", "cantblockalone"},
	"CantAttackAlone":        {"cantattackalone"},
	"CantBlockAlone":         {"cantblockalone"},
}

// mtgishFaceE runs after a face's rules are mapped: the keyword actions
// inside its spells, and a Spacecraft's P/T.
func mtgishFaceE(m *mtgishNorm, f node) {
	oneCDA(m.s)
	walk(f["Rules"], func(n node, enum, variant string) bool {
		if tok, ok := mtgishKeywordActions[variant]; ok && enum == "_Action" {
			m.kw(tok)
		}
		return true
	})
	if m.s.PT != "" {
		return
	}
	// A Spacecraft prints its P/T box beside the station threshold that
	// makes it a creature; mtgish nests it in StationChargedAnimate.
	for _, r := range list(f["Rules"]) {
		if variantOf(r) != "StationChargedAnimate" {
			continue
		}
		if pt := argN(r, 2); variantOf(pt) == "PT" {
			p, t := argN(pt, 0), argN(pt, 1)
			if pn, ok := intOf(p); ok {
				if tn, ok := intOf(t); ok {
					m.s.PT = strconv.Itoa(pn) + "/" + strconv.Itoa(tn)
				}
			}
		}
	}
}

// stationLevel maps one STATION N+ level. The printed level is a static
// ability that applies while the permanent has N charge counters, granting
// the abilities listed under it (and, on a Spacecraft, making it an artifact
// creature): Forge writes it as one Continuous static (AddKeyword,
// AddAbility/AddTrigger, AddType), so it is one static here, with a grant
// per keyword, one grant:ability per other ability and "type" for the
// animation. The Station keyword itself is its own Rule.
func (m *mtgishNorm) stationLevel(r any) {
	n := &mtgishNorm{s: &Summary{}}
	n.rules(list(argN(r, 1)))
	var eff []string
	for _, k := range n.s.Keywords {
		eff = append(eff, "grant:"+keywordHeadToken(k))
	}
	for range n.s.Abilities {
		eff = append(eff, "grant:ability")
	}
	if variantOf(r) == "StationChargedAnimate" {
		eff = append(eff, "type")
	}
	m.ability(Ability{Kind: "static", Effects: eff})
}

// gorgeCardE runs after every face of a gorge card is summarised.
func gorgeCardE(c *cards.Card, out []*Summary) {
	// Forge writes "Loyalty:0" on the back face of a planeswalker that
	// transforms into a planeswalker (Arlinn Kord, Garruk Relentless): the
	// back face prints no loyalty (a front face may print 0: Dakkon,
	// Jeska, B.O.B.).
	if c.AlternateMode == cards.AlternateDoubleFaced && len(out) > 1 {
		for _, s := range out[1:] {
			if s.Loyalty == "0" {
				s.Loyalty = ""
			}
		}
	}
	// Fuse is printed across both halves of a split card (each half's text
	// box ends in it, and mtgish lists it on each); Forge writes K:Fuse on
	// the first half only.
	if c.AlternateMode == "Split" && len(out) > 1 && hasToken(out[0].Keywords, "fuse") {
		for _, s := range out[1:] {
			if !hasToken(s.Keywords, "fuse") {
				s.Keywords = append(s.Keywords, "fuse")
			}
		}
	}
}

func hasToken(ts []string, t string) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}

// cleaveSP reports whether a is the cleave half of a Cleave spell: Forge
// writes Cleave as a second SP with "PrecostDesc$ Cleave" and no keyword.
func cleaveSP(a *cards.SA) bool {
	return a.Kind == "SP" && strings.TrimSpace(a.Params["PrecostDesc"]) == "Cleave"
}

// cleave folds a face's cleave SP into its spell ability as an alternative
// (cast for the cleave cost or not: one spell, branch-max), the way mtgish's
// SpellActions_Cleave reads, and prints the keyword.
func (g *gorgeNorm) cleave(f *cards.Face) {
	for _, a := range f.Abilities {
		if !cleaveSP(a) {
			continue
		}
		g.kw("cleave")
		alt := &gorgeNorm{s: &Summary{}, f: g.f, idx: g.idx}
		alt.sa(a)
		for i := range g.s.Abilities {
			base := &g.s.Abilities[i]
			if base.Kind != "spell" {
				continue
			}
			e := &effects{}
			e.merge([]*effects{
				{targets: base.Targets, effects: base.Effects},
				{targets: alt.s.Abilities[0].Targets, effects: alt.s.Abilities[0].Effects},
			})
			base.Targets, base.Effects = e.targets, e.effects
			alt = nil
			break
		}
		if alt != nil {
			g.s.Abilities = append(g.s.Abilities, alt.s.Abilities...)
		}
	}
}

// gorgePT renders a face's printed P/T the way mtgish's CardPT reads. A
// "*" half is "*" only where a characteristic-defining ability sets it
// (mtgish's CDA_Power/CDA_Toughness); otherwise the star is set by an
// "as this enters" choice or a conditional static that mtgish writes as an
// ordinary rule, and CardPT holds the printed base: 0 for "*", N for "N+*"
// or "N-*".
func gorgePT(f *cards.Face) string {
	pt := f.PT
	if !strings.Contains(pt, "*") {
		return pt
	}
	var cdaP, cdaT bool
	for i := range f.Statics {
		p := f.Statics[i].Params
		if p["CharacteristicDefining"] != "True" {
			continue
		}
		cdaP = cdaP || p["SetPower"] != ""
		cdaT = cdaT || p["SetToughness"] != ""
	}
	p, t, _ := strings.Cut(pt, "/")
	return starHalf(p, cdaP) + "/" + starHalf(t, cdaT)
}

func starHalf(h string, cda bool) string {
	if !strings.Contains(h, "*") {
		return h
	}
	if cda {
		return "*"
	}
	base := strings.TrimRight(strings.TrimSuffix(h, "*"), "+-")
	if base == "" {
		return "0"
	}
	return base
}

// multiWordSubtypes are the subtypes printed as more than one word (CR
// 205.3m/n), by typeWord spelling: Forge splits them on spaces like every
// other type word, mtgish names each as one enum variant.
var multiWordSubtypes = map[string]bool{
	"timelord": true, "alfavametraxis": true, "androzaniminor": true, "bolassmeditationrealm": true,
	"horseheadnebula": true, "newearth": true, "newphyrexia": true, "outsidemuttersspiral": true,
	"serrasrealm": true, "theabyss": true, "thedalekasylum": true, "thelibrary": true,
	"unknownplanet": true,
}

// gorgeTypes renders a face's type words, joining the words of a
// multi-word subtype.
func gorgeTypes(ws []string) []string {
	var out []string
	for i := 0; i < len(ws); i++ {
		joined := ""
		for j := i; j < len(ws) && j < i+4; j++ {
			joined += typeWord(ws[j])
			if j > i && multiWordSubtypes[joined] {
				out = append(out, joined)
				i = j
				joined = ""
				break
			}
		}
		if joined != "" {
			out = append(out, typeWord(ws[i]))
		}
	}
	return out
}

// selfPTSetters are the StaticLayerEffect variants that set a P/T half.
var selfPTSetters = map[string][2]bool{
	"SetPower": {true, false}, "SetToughness": {false, true}, "SetPowerAndToughnessBoth": {true, true},
}

// selfPT maps a static that sets this permanent's own power and/or
// toughness, and nothing else, as the characteristic-defining ability it
// prints as ("*" in the P/T box). mtgish writes a P/T that depends on a
// choice or a linked number (the chosen colour, the exiled card) as a
// PermanentLayerEffect on ThisPermanent rather than CDA_Power; Forge writes
// both as CharacteristicDefining statics. A conditional one (under If or
// IfElse) reaches here through the wrapper too, as Forge's conditional CDA
// statics do.
func (m *mtgishNorm) selfPT(r any) bool {
	if variantOf(r) != "PermanentLayerEffect" || variantOf(argN(r, 0)) != "ThisPermanent" {
		return false
	}
	var p, t bool
	effs := list(argN(r, 1))
	for _, e := range effs {
		h, ok := selfPTSetters[variantOf(e)]
		if !ok {
			return false
		}
		p, t = p || h[0], t || h[1]
	}
	if len(effs) == 0 {
		return false
	}
	m.cdaPower = m.cdaPower || p
	m.cdaToughness = m.cdaToughness || t
	m.ability(Ability{Kind: "static", Effects: []string{"cda"}})
	return true
}

// oneCDA folds a face's characteristic-defining statics into one ability,
// as gorgeFace does: one CDA however many rules carry it (a colour and a
// P/T, a power and a toughness, both branches of an IfElse).
func oneCDA(s *Summary) {
	seen := false
	out := s.Abilities[:0]
	for _, a := range s.Abilities {
		if a.Kind == "static" && len(a.Effects) == 1 && a.Effects[0] == "cda" && len(a.Targets) == 0 {
			if seen {
				continue
			}
			seen = true
		}
		out = append(out, a)
	}
	s.Abilities = out
}

// colorQualities canonicalises a protection/hexproof colour quality: Forge
// writes "each color" and "Card.nonColorless", mtgish AnyColor; Forge
// MonoColor/MultiColor, mtgish Monocolored/Multicolored.
var colorQualities = map[string]string{
	"eachcolor": "anycolor", "allcolors": "anycolor", "noncolorless": "anycolor",
	"monocolored": "monocolor", "multicolored": "multicolor",
}

func colorQuality(w string) string {
	if c, ok := colorQualities[w]; ok {
		return c
	}
	return w
}

// enchantZone is the zone an Enchant keyword's filter names: "Enchant
// creature card in a graveyard" is Creature.inZoneGraveyard.
func enchantZone(cls string) string {
	if strings.Contains(cls, "inZoneGraveyard") {
		return "Graveyard"
	}
	return ""
}

// gorgeAffinity renders an Affinity filter ("Artifact", "Land.Snow",
// "Creature.Artifact") as its type words, sorted, the way affinityWord
// renders mtgish's.
func gorgeAffinity(param string) string {
	f := strings.SplitN(param, ":", 2)[0]
	var words []string
	for _, w := range strings.FieldsFunc(f, func(r rune) bool { return r == '.' || r == '+' }) {
		words = append(words, typeWord(w))
	}
	return strings.Join(sorted(dedup(words)), "|")
}

// intersectionOrder ranks card types for an intersection of them
// ("artifact creature", "enchantment creature"): its target class is the
// first. Forge spells the intersection either way round
// (Creature.Artifact, Artifact.Creature), mtgish as an And of IsCardtype.
var intersectionOrder = []string{"creature", "planeswalker", "battle", "land", "enchantment", "artifact"}

func pickIntersection(ws []string) string {
	for _, c := range intersectionOrder {
		if hasToken(ws, c) {
			return c
		}
	}
	return ws[0]
}

// intersectionClass is a Forge filter alternative's class: its head type,
// or, when type properties follow it ("Creature.Artifact+nonToken"), the
// intersection's class.
func intersectionClass(head, alt string) string {
	if !permanentTypes[head] {
		return head
	}
	ws := []string{head}
	for _, w := range strings.FieldsFunc(alt, func(r rune) bool { return r == '.' || r == '+' })[1:] {
		if w := typeWord(w); permanentTypes[w] {
			ws = append(ws, w)
		}
	}
	return pickIntersection(ws)
}

// directClass is the card-type class of one mtgish permanent filter that
// names a type outright (IsCardtype, or a subtype implying its card type),
// "" for any other filter.
func directClass(x any) string {
	switch variantOf(x) {
	case "IsCardtype":
		if s, ok := rawArgs(x).(string); ok {
			if w := typeWord(s); permanentTypes[w] {
				return w
			}
		}
	case "IsCreatureType", "IsCreatureTypeVariable":
		return "creature"
	case "IsArtifactType":
		return "artifact"
	case "IsLandType":
		return "land"
	case "IsEnchantmentType":
		return "enchantment"
	}
	return ""
}

func init() {
	// "You may also activate this card's specialize ability while it's in
	// your graveyard" is a rider of the Specialize keyword: Forge writes it
	// as a parameter of the one K:Specialize line.
	extRule["SpecializeFromGraveyard"] = func(*mtgishNorm, any) {}
	// "Enchant creature card in a graveyard": the Enchant keyword with a
	// graveyard-card class, as gorge renders Forge's
	// Enchant:<Type>.inZoneGraveyard.
	extRule["EnchantGraveyardCard"] = func(m *mtgishNorm, _ any) { m.kw("enchant:" + clsGraveyard) }
}
