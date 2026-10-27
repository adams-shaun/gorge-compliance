package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit E: the T1 (characteristics) normaliser classes. Each block names the
// class it closes; the census behind it is in the commit messages.

func init() {
	// mtgish writes these keyword abilities of an instant or sorcery as an
	// action inside the spell's actions; Forge (and the printed card) list
	// them as keywords. mtgishFaceE adds the keyword on the mtgish side; the
	// action itself is no effect verb on either side.
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
	"AllMustBlock":           {"allcreaturesmustblockit"},
	"CantAttackOrBlock":      {"cantattack", "cantblock"},
	"CantAttackOrBlockAlone": {"cantattackalone", "cantblockalone"},
	"CantAttackAlone":        {"cantattackalone"},
	"CantBlockAlone":         {"cantblockalone"},
}

// mtgishFaceE runs after a face's rules are mapped and its P/T read.
func mtgishFaceE(m *mtgishNorm, f node) {
	defer oneCDA(m.s)
	riders := 0
	for _, r := range list(f["Rules"]) {
		switch {
		case variantOf(r) == "SpecializeFromGraveyard":
			riders++
		case variantOf(r) == "CDA_Color" && variantOf(rawArgs(r)) == "Colorless":
			// "This is colorless" is a colour CDA (Forge: SetColor$
			// Colorless), not the devoid keyword the CDA_Color case
			// reads it as.
			m.s.Keywords = dropOne(m.s.Keywords, "devoid")
			m.ability(Ability{Kind: "static", Effects: []string{"cda"}})
		}
	}
	walk(f["Rules"], func(n node, enum, variant string) bool {
		if tok, ok := mtgishKeywordActions[variant]; ok && enum == "_Action" {
			m.kw(tok)
		}
		return true
	})
	// "You may also activate this card's specialize ability while it's in
	// your graveyard" is a rider of the Specialize keyword (Forge writes it
	// as a parameter of the one K:Specialize line), not a second keyword.
	for ; riders > 0; riders-- {
		m.s.Keywords = dropOne(m.s.Keywords, "specialize")
	}
}

func dropOne(ts []string, t string) []string {
	for i, x := range ts {
		if x == t {
			return append(ts[:i:i], ts[i+1:]...)
		}
	}
	return ts
}

// gorgeCardE runs after every face of a gorge card is summarised; out holds
// the summaries of c's non-nil faces, in order.
func gorgeCardE(c *cards.Card, out []*Summary, idx TypeIndex) {
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
	i := 0
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		if i < len(out) {
			cleave(f, out[i])
		}
		i++
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

// cleave prints a Cleave face's keyword and folds its spell abilities into
// one, as mtgish's SpellActions_Cleave reads (cast for the cleave cost or
// not: one spell, branch-max). Forge writes Cleave as a second SP with
// "PrecostDesc$ Cleave" and no keyword line.
func cleave(f *cards.Face, s *Summary) {
	has := false
	for _, a := range f.Abilities {
		if a.Kind == "SP" && strings.TrimSpace(a.Params["PrecostDesc"]) == "Cleave" {
			has = true
		}
	}
	if !has {
		return
	}
	// Idempotent: another pass may already have read the cleave SP.
	if !hasToken(s.Keywords, "cleave") {
		s.Keywords = append(s.Keywords, "cleave")
	}
	var subs []*effects
	first := -1
	kept := s.Abilities[:0]
	for _, a := range s.Abilities {
		if a.Kind == "spell" {
			subs = append(subs, &effects{targets: a.Targets, effects: a.Effects})
			if first >= 0 {
				continue
			}
			first = len(kept)
		}
		kept = append(kept, a)
	}
	s.Abilities = kept
	if first >= 0 && len(subs) > 1 {
		e := &effects{}
		e.merge(subs)
		s.Abilities[first].Targets, s.Abilities[first].Effects = e.targets, e.effects
	}
}

// gorgePT renders a face's printed P/T the way mtgish's CardPT reads. A
// "*" half is "*" only where a characteristic-defining ability sets it
// (mtgish's CDA_Power/CDA_Toughness, or the self P/T layer effect selfPT
// reads as one); otherwise the star is set by an "as this enters" choice,
// and CardPT holds the printed base: 0 for "*", N for "N+*" or "N-*".
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
