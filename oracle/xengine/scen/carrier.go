package scen

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
)

// A carrier is the card a forced start moves to make a trigger's event happen:
// the creature that enters for "whenever another creature you control enters",
// the spell cast for "whenever you cast a noncreature spell". It is the most
// inert card the corpus holds that matches the trigger's filter, so the one
// event the start offers is the one the key reads, and the three engines face
// the same small decision tree.

// preferredCarriers are tried first, in order, before the ranked corpus scan:
// Memnite ({0} vanilla artifact creature: a creature, artifact and permanent
// spell no land pays for), Reach Through Mists ({U} instant, draw a card: an
// instant, noncreature spell with no target), Chaplain's Blessing ({W}
// sorcery, gain 5 life: a sorcery with no target), Nyxborn Courser (a vanilla
// enchantment creature: the cheapest vanilla enchantment).
var preferredCarriers = []string{"Memnite", "Reach Through Mists", "Chaplain's Blessing", "Nyxborn Courser"}

// carrierPools holds each registry's ranked carrier candidates, built once.
var carrierPools sync.Map // *cards.Registry -> []*cards.Face

// carrierPool is every card that may carry a start's event, ranked: the
// preferred carriers, then fewest keywords, then by mana value, nonlegendary
// first, then name.
func carrierPool(reg *cards.Registry, b *certdeck.Builder) []*cards.Face {
	if v, ok := carrierPools.Load(reg); ok {
		return v.([]*cards.Face)
	}
	var out []*cards.Face
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 || !b.Playable(c) {
			continue
		}
		if f := c.Faces[0]; inertCarrier(f) {
			out = append(out, f)
		}
	}
	pref := func(f *cards.Face) int {
		if i := slices.Index(preferredCarriers, f.Name); i >= 0 {
			return i
		}
		return len(preferredCarriers)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, c := out[i], out[j]
		if pa, pc := pref(a), pref(c); pa != pc {
			return pa < pc
		}
		if ka, kc := len(a.Keywords), len(c.Keywords); ka != kc {
			return ka < kc
		}
		if a.Cmc() != c.Cmc() {
			return a.Cmc() < c.Cmc()
		}
		if a.IsLegendary() != c.IsLegendary() {
			return !a.IsLegendary()
		}
		return a.Name < c.Name
	})
	carrierPools.Store(reg, out)
	return out
}

// inertCarrier is a card a start can cast and that does nothing else: a
// vanilla permanent (no ability, trigger, static or replacement, and no
// keyword but an evergreen one: evergreenKeywords) or
// an instant or sorcery whose one ability gains a fixed amount of life or
// draws a fixed number of cards, with no target and no extra cost. Its cost
// is one basic land per symbol (no X, snow or colourless-only symbol), and it
// is no Aura and no land (a land carrier is a basic, landCarrier).
func inertCarrier(f *cards.Face) bool {
	if f.IsLand() || f.ManaCost == "" || f.ManaCost == "no cost" {
		return false
	}
	for _, tok := range splitCost(f.ManaCost) {
		if !manaTokenRE.MatchString(tok) || tok == "X" || tok == "S" || tok == "C" {
			return false
		}
	}
	if FieldBlock(f) == "aura-unattached" || len(f.Triggers)+len(f.Statics)+len(f.Repls) > 0 || !evergreenOnly(f) {
		return false
	}
	if f.IsPermanent() {
		return len(f.Abilities) == 0
	}
	if len(f.Keywords) > 0 {
		return false
	}
	if len(f.Abilities) != 1 {
		return false
	}
	sa := f.Abilities[0]
	if sa.Sub != nil || sa.API != "GainLife" && sa.API != "Draw" {
		return false
	}
	for k, v := range sa.Params {
		switch k {
		case "ValidTgts", "TgtPrompt", "UnlessCost", "Condition", "ConditionCheckSVar":
			return false
		case "Defined":
			if v != "You" {
				return false
			}
		case "LifeAmount", "NumCards":
			if _, err := strconv.Atoi(v); err != nil {
				return false
			}
		case "Cost":
			if _, why := costTokens(v); why != "" {
				return false
			}
			for _, tok := range splitCost(v) {
				if !manaTokenRE.MatchString(tok) {
					return false
				}
			}
		}
	}
	return true
}

// evergreenKeywords are the keywords a carrier permanent may have: they change
// how it fights, not what the start offers (a vanilla Dragon has flying).
var evergreenKeywords = map[string]bool{"Flying": true, "Reach": true, "Vigilance": true, "Trample": true,
	"Defender": true, "Menace": true, "First Strike": true, "Lifelink": true, "Deathtouch": true}

// evergreenOnly reports whether every keyword of f is an evergreen one.
func evergreenOnly(f *cards.Face) bool {
	for _, k := range f.Keywords {
		if !evergreenKeywords[k] {
			return false
		}
	}
	return true
}

// Controller requirements of a filter alternative.
const (
	ctrlAny = -1
	ctrlYou = 0
	ctrlOpp = 1
)

// filterAlt is one comma alternative of a Forge ValidCard filter
// ("Creature.Other+YouCtrl"), read as far as a start needs it: who controls
// the card, whether it is the card itself, whether it is a token, and a test
// of a carrier's face. Known is false when some property is not understood:
// no carrier is picked for it.
type filterAlt struct {
	ctrl  int
	self  bool
	other bool
	// token is 1 when the alternative requires a token, -1 when it requires a
	// non-token card (a carrier is a card), 0 when it does not say.
	token int
	known bool
	test  func(f *cards.Face) bool
}

// matches is test plus the token requirement: a token maker's token passes
// isToken true, every placed card passes false.
func (a filterAlt) matches(f *cards.Face, isToken bool) bool {
	return (a.token <= 0 || isToken) && (a.token >= 0 || !isToken) && a.test(f)
}

var colourWord = map[string]byte{"White": 'W', "Blue": 'U', "Black": 'B', "Red": 'R', "Green": 'G'}

// faceColours is the colour letters of a face's mana cost (hybrid: both).
func faceColours(f *cards.Face) string {
	var out string
	for _, l := range "WUBRG" {
		if hasShard(f.ManaCost, byte(l)) {
			out += string(l)
		}
	}
	return out
}

func hasShard(cost string, l byte) bool {
	for _, shard := range strings.Fields(cost) {
		for _, part := range shardParts(shard) {
			if len(part) == 1 && part[0] == l {
				return true
			}
		}
	}
	return false
}

// compare reads a "<stat><OP><n>" property (cmcGE5, powerLE2): the stat and
// a test of a value, or ok false.
func compare(prop string) (stat string, test func(int) bool, ok bool) {
	for _, s := range []string{"cmc", "power", "toughness"} {
		rest, found := strings.CutPrefix(prop, s)
		if !found || len(rest) < 3 {
			continue
		}
		n, err := strconv.Atoi(rest[2:])
		if err != nil {
			return "", nil, false
		}
		switch rest[:2] {
		case "GE":
			return s, func(v int) bool { return v >= n }, true
		case "GT":
			return s, func(v int) bool { return v > n }, true
		case "LE":
			return s, func(v int) bool { return v <= n }, true
		case "LT":
			return s, func(v int) bool { return v < n }, true
		case "EQ":
			return s, func(v int) bool { return v == n }, true
		}
	}
	return "", nil, false
}

// typeWord reports whether w reads as a type word (Creature, Dragon, Legendary):
// capitalised letters and hyphens only.
func typeWord(w string) bool {
	if w == "" || w[0] < 'A' || w[0] > 'Z' {
		return false
	}
	for _, r := range w {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
			return false
		}
	}
	return true
}

// parseFilter reads a ValidCard filter into its alternatives.
func parseFilter(s string) []filterAlt {
	var out []filterAlt
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		head, props, _ := strings.Cut(raw, ".")
		a := filterAlt{ctrl: ctrlAny, known: true}
		var tests []func(*cards.Face) bool
		switch head {
		case "Card", "Spell":
		case "Permanent":
			tests = append(tests, (*cards.Face).IsPermanent)
		default:
			if !typeWord(head) {
				a.known = false
			}
			t := head
			tests = append(tests, func(f *cards.Face) bool { return f.HasTypeWord(t) })
		}
		if props != "" {
			for _, p := range strings.Split(props, "+") {
				switch p {
				case "YouCtrl", "YouOwn":
					a.ctrl = ctrlYou
					continue
				case "OppCtrl", "OppOwn":
					a.ctrl = ctrlOpp
					continue
				case "Self":
					a.self = true
					continue
				case "Other":
					a.other = true
					continue
				case "!token", "nonToken":
					a.token = -1
					continue
				case "token":
					a.token = 1
					continue
				case "Colorless":
					tests = append(tests, func(f *cards.Face) bool { return faceColours(f) == "" })
					continue
				case "Multicolor":
					tests = append(tests, func(f *cards.Face) bool { return len(faceColours(f)) > 1 })
					continue
				}
				if kw, ok := strings.CutPrefix(p, "with"); ok && evergreenKeywords[kw] {
					// withFlying, withDefender: carriers hold only evergreen keywords
					tests = append(tests, func(f *cards.Face) bool { return f.HasKeyword(kw) })
					continue
				}
				if kw, ok := strings.CutPrefix(p, "without"); ok && typeWord(kw) {
					// withoutFlying: a carrier holds no keyword but an evergreen one
					tests = append(tests, func(f *cards.Face) bool { return !f.HasKeyword(kw) })
					continue
				}
				if l, ok := colourWord[p]; ok {
					tests = append(tests, func(f *cards.Face) bool { return hasShard(f.ManaCost, l) })
					continue
				}
				if w, ok := strings.CutPrefix(p, "non"); ok && typeWord(w) {
					if l, ok := colourWord[w]; ok {
						tests = append(tests, func(f *cards.Face) bool { return !hasShard(f.ManaCost, l) })
					} else {
						tests = append(tests, func(f *cards.Face) bool { return !f.HasTypeWord(w) })
					}
					continue
				}
				if stat, cmp, ok := compare(p); ok {
					tests = append(tests, func(f *cards.Face) bool {
						switch stat {
						case "cmc":
							return cmp(int(f.Cmc()))
						case "power":
							return f.IsCreature() && cmp(f.Power())
						}
						return f.IsCreature() && cmp(f.Toughness())
					})
					continue
				}
				if typeWord(p) {
					w := p
					tests = append(tests, func(f *cards.Face) bool { return f.HasTypeWord(w) })
					continue
				}
				a.known = false
			}
		}
		a.test = func(f *cards.Face) bool {
			for _, t := range tests {
				if !t(f) {
					return false
				}
			}
			return true
		}
		out = append(out, a)
	}
	return out
}

// pickCarrier is the first carrier of the pool (carrierPool) that keep
// accepts and that is not the focus card, or "".
func pickCarrier(reg *cards.Registry, b *certdeck.Builder, card string, keep func(*cards.Face) bool) string {
	for _, f := range carrierPool(reg, b) {
		if f.Name != card && keep(f) {
			return f.Name
		}
	}
	return ""
}

// filterCarrier picks, for the first understood, non-Self alternative of the
// filter that some carrier matches, that carrier and the seat that must
// control it (an alternative with no controller: seat 0). extra further
// narrows the carriers (a permanent for an enters trigger).
func filterCarrier(reg *cards.Registry, b *certdeck.Builder, card, filter string, extra func(*cards.Face) bool) (name string, seat int) {
	for _, a := range parseFilter(filter) {
		if !a.known || a.self {
			continue
		}
		if n := pickCarrier(reg, b, card, func(f *cards.Face) bool { return extra(f) && a.matches(f, false) }); n != "" {
			if a.ctrl == ctrlOpp {
				return n, 1
			}
			return n, 0
		}
	}
	return "", 0
}

// landCarrier is, for a filter some alternative of which a basic land meets
// ("Land.YouCtrl", "Forest.YouCtrl"), the basic and the seat that plays it,
// or "".
func landCarrier(reg *cards.Registry, filter, generic string) (name string, seat int) {
	for _, a := range parseFilter(filter) {
		if !a.known || a.self {
			continue
		}
		for _, basic := range append([]string{generic}, "Plains", "Island", "Swamp", "Mountain", "Forest") {
			c, ok := reg.Lookup(basic)
			if !ok || len(c.Faces) == 0 || !a.matches(c.Faces[0], false) {
				continue
			}
			if a.ctrl == ctrlOpp {
				return basic, 1
			}
			return basic, 0
		}
	}
	return "", 0
}

// castCarrier puts a carrier in seat s's hand with exactly the basics its
// mana cost takes on that seat's battlefield.
func castCarrier(reg *cards.Registry, s *StartSeat, name string) {
	c, _ := reg.Lookup(name)
	cost := c.Faces[0].ManaCost
	s.Hand = append(s.Hand, name)
	s.Battlefield = append(s.Battlefield, landsFor(splitCost(cost), basicFor(cost))...)
}
