package certdeck

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/deck"
	"github.com/adams-shaun/gorge/engine/effects"
)

const (
	// DeckSize is the main-deck size of both decks.
	DeckSize = 40
	// MaxEnablerSlots is how many deck slots the need classes may take.
	MaxEnablerSlots = 12
	baseLands       = 17
	extraLands      = 2 // a Lands need
	enablerCopies   = 2
	fillerMaxCMC    = 5
)

var (
	colourLetters = [5]string{"W", "U", "B", "R", "G"}
	basicNames    = [5]string{"Plains", "Island", "Swamp", "Mountain", "Forest"}
	snowBasics    = [5]string{"Snow-Covered Plains", "Snow-Covered Island", "Snow-Covered Swamp", "Snow-Covered Mountain", "Snow-Covered Forest"}
)

// Deck is one built deck: its file, the cards it holds for each need and the
// colours it plays.
type Deck struct {
	File     deck.File
	Colours  string
	Provided map[string][]string // need (Need.String) -> the cards chosen for it, in pick order
}

// Plan is the two decks of one target.
type Plan struct {
	Target   string // corpus spelling of the target's front face
	Needs    []Need
	Self     Deck
	Opponent Deck
}

// Builder holds what a build reads: the corpus, XMage's spellings and the
// supported-primitive set.
type Builder struct {
	Reg   *cards.Registry
	Sp    *decks.Spellings
	sup   map[string]bool
	types map[string]bool
	cache map[*cards.Card]*facts
	ok    map[*cards.Card]bool
}

// NewBuilder is a Builder over reg and sp. Both are required: an enabler must
// be a card gorge plays and XMage lists under the corpus spelling.
func NewBuilder(reg *cards.Registry, sp *decks.Spellings) *Builder {
	return &Builder{Reg: reg, Sp: sp, sup: effects.Supported(), types: CreatureTypes(reg),
		cache: map[*cards.Card]*facts{}, ok: map[*cards.Card]bool{}}
}

// Types is CreatureTypes of the corpus, for Derive and ParseNeeds.
func (b *Builder) Types() map[string]bool { return b.types }

// facts is what the candidate predicates read of one card, derived once.
type facts struct {
	c        *cards.Card
	f        *cards.Face
	name     string
	cmc      int
	identity uint8
	us       []unit
	subtypes map[string]bool
}

func (b *Builder) facts(c *cards.Card) *facts {
	if x, ok := b.cache[c]; ok {
		return x
	}
	f := c.Faces[0]
	x := &facts{c: c, f: f, name: f.Name, cmc: int(f.Cmc()), identity: c.ColourIdentity(), subtypes: map[string]bool{}}
	for _, t := range f.Types {
		x.subtypes[t] = true
	}
	b.cache[c] = x
	return x
}

func (x *facts) has(t string) bool { return x.subtypes[t] }

func (x *facts) units() []unit {
	if x.us == nil {
		x.us = unitsOf(x.c)
	}
	return x.us
}

// api is whether some unit is an ability with that API for which keep holds.
func (x *facts) api(name string, keep func(unit) bool) bool {
	for _, u := range x.units() {
		if u.api == name && (keep == nil || keep(u)) {
			return true
		}
	}
	return false
}

// Playable is whether c may stand in a certification deck (see playable).
func (b *Builder) Playable(c *cards.Card) bool { return len(c.Faces) > 0 && b.playable(c) }

// playable is whether c may stand in a certification deck: a real card gorge
// fully supports, that XMage lists under the corpus spelling, that its own
// script does not mark as one the AI cannot play, and that is not a token or
// basic land (the mana base is built separately).
func (b *Builder) playable(c *cards.Card) bool {
	if v, ok := b.ok[c]; ok {
		return v
	}
	v := b.playableSlow(c)
	b.ok[c] = v
	return v
}

func (b *Builder) playableSlow(c *cards.Card) bool {
	if len(c.Faces) == 0 {
		return false
	}
	f := c.Faces[0]
	if f.Name == "" || f.RemoveDeck != "" || f.IsBasic() || f.HasTypeWord("Token") {
		return false
	}
	if got, ok := b.Reg.Lookup(f.Name); !ok || got != c {
		return false
	}
	if !(f.IsCreature() || f.IsInstant() || f.IsSorcery() || f.IsArtifact() || f.IsEnchantment() || f.IsPlaneswalker() || f.IsLand()) {
		return false
	}
	if xm, ok := b.Sp.XMage(b.Reg, f.Name); !ok || xm != f.Name {
		return false
	}
	return len(b.Reg.Unsupported(c, b.sup)) == 0
}

// cand is one ranked candidate for a need.
type cand struct {
	c   *cards.Card
	key uint64
	cmc int
}

// rankKey is the deterministic tiebreak: a hash of the target and the card.
func rankKey(target, name string) uint64 {
	h := sha256.Sum256([]byte("certdeck\x00" + target + "\x00" + name))
	return binary.LittleEndian.Uint64(h[:8])
}

// Build is the two decks that certify target (a card name), for needs (nil:
// Derive's).
func (b *Builder) Build(target string, needs []Need) (*Plan, error) {
	tc, ok := b.Reg.Lookup(target)
	if !ok || len(tc.Faces) == 0 {
		return nil, fmt.Errorf("certdeck: %q is not in the corpus", target)
	}
	name := tc.Faces[0].Name
	if u := b.Reg.Unsupported(tc, b.sup); len(u) > 0 {
		return nil, fmt.Errorf("certdeck: gorge does not fully support %q: %v", name, u)
	}
	if xm, ok := b.Sp.XMage(b.Reg, name); !ok {
		return nil, fmt.Errorf("certdeck: XMage lists no card %q", name)
	} else if xm != name {
		return nil, fmt.Errorf("certdeck: XMage spells %q as %q", name, xm)
	}
	if needs == nil {
		needs = Derive(tc, b.types)
	}
	needs = sortNeeds(needs)
	plan := &Plan{Target: name, Needs: needs}
	tf := b.facts(tc)

	// The target deck.
	selfMask := tf.identity
	if selfMask == 0 {
		selfMask = 1 << (rankKey(name, "colour") % 5)
	}
	var selfNeeds, oppNeeds []Need
	for _, n := range needs {
		if OppSide(n.Class) {
			oppNeeds = append(oppNeeds, n)
		} else {
			selfNeeds = append(selfNeeds, n)
		}
	}
	snow, lands := false, false
	for _, n := range selfNeeds {
		snow = snow || n.Class == Snow
		lands = lands || n.Class == Lands
	}
	copies := 4
	if tf.f.IsLegendary() {
		copies = 1
	}
	self, err := b.assemble(name, name, selfMask, selfNeeds, map[*cards.Card]int{tc: copies}, snow, lands)
	if err != nil {
		return nil, err
	}
	self.File.Name = "certdeck " + name + " (target)"
	plan.Self = self

	// The opponent deck: the colour pair that best supplies its needs.
	oppMask := b.opponentColours(name, selfMask, oppNeeds)
	opp, err := b.assemble(name, "opponent\x00"+name, oppMask, oppNeeds, map[*cards.Card]int{tc: 0}, false, false)
	if err != nil {
		return nil, err
	}
	opp.File.Name = "certdeck " + name + " (opponent)"
	plan.Opponent = opp
	label := make([]string, len(needs))
	for i, n := range needs {
		label[i] = n.String()
	}
	for _, d := range []*Deck{&plan.Self, &plan.Opponent} {
		d.File.Format = "constructed"
		d.File.Archetype = "certdeck " + strings.Join(label, ",")
	}
	return plan, nil
}

// assemble builds one deck of colour mask from the needs it must supply. fixed
// is the cards already in the deck and their copies (a card with 0 copies is
// only excluded).
func (b *Builder) assemble(target, salt string, mask uint8, needs []Need, fixed map[*cards.Card]int, snow, moreLands bool) (Deck, error) {
	chosen := map[*cards.Card]int{}
	for c, n := range fixed {
		if n > 0 {
			chosen[c] = n
		}
	}
	exclude := map[*cards.Card]bool{}
	for c := range fixed {
		exclude[c] = true
	}
	d := Deck{Provided: map[string][]string{}}
	lands := baseLands
	if moreLands {
		lands += extraLands
	}
	spells := DeckSize - lands

	// Round-robin over the needs, a candidate at a time.
	lists := make([][]cand, len(needs))
	for i, n := range needs {
		lists[i] = b.candidates(salt, n, mask)
	}
	slots, next := 0, make([]int, len(needs))
	for progress := true; progress && slots < MaxEnablerSlots; {
		progress = false
		for i, n := range needs {
			if slots >= MaxEnablerSlots {
				break
			}
			for next[i] < len(lists[i]) {
				c := lists[i][next[i]].c
				next[i]++
				if exclude[c] || !b.playable(c) {
					continue
				}
				cp := enablerCopies
				if b.facts(c).f.IsLegendary() {
					cp = 1
				}
				exclude[c] = true
				chosen[c] = cp
				slots += cp
				d.Provided[n.String()] = append(d.Provided[n.String()], b.facts(c).name)
				progress = true
				break
			}
		}
	}

	// Filler: creatures two to one over other spells, cheapest-agnostic, by hash.
	count, chosenLands := 0, 0
	for c, n := range chosen {
		if b.facts(c).f.IsLand() {
			chosenLands += n // a land target or land enabler is part of the mana base
		} else {
			count += n
		}
	}
	if lands -= chosenLands; lands < 0 { // basics still to add; the spell count is unchanged
		lands = 0
	}

	var creatures, others []cand
	for _, c := range b.Reg.AllCards() {
		if len(c.Faces) == 0 || exclude[c] {
			continue
		}
		x := b.facts(c)
		if x.f.IsLand() || x.cmc > fillerMaxCMC || x.identity&^mask != 0 {
			continue
		}
		cd := cand{c: c, key: rankKey(salt, x.name), cmc: x.cmc}
		if x.f.IsCreature() {
			creatures = append(creatures, cd)
		} else {
			others = append(others, cd)
		}
	}
	byKey := func(cs []cand) {
		sort.Slice(cs, func(i, j int) bool {
			if cs[i].key != cs[j].key {
				return cs[i].key < cs[j].key
			}
			return b.facts(cs[i].c).name < b.facts(cs[j].c).name
		})
	}
	byKey(creatures)
	byKey(others)
	ci, oi := 0, 0
	for turn := 0; count < spells; turn++ {
		src, idx := &creatures, &ci
		if turn%3 == 2 {
			src, idx = &others, &oi
		}
		for *idx < len(*src) && count < spells {
			c := (*src)[*idx].c
			*idx++
			if exclude[c] || !b.playable(c) {
				continue
			}
			exclude[c] = true
			chosen[c] = 1
			count++
			break
		}
		if ci >= len(creatures) && oi >= len(others) {
			return d, errors.New("certdeck: not enough playable filler in the deck's colours")
		}
	}
	if count > spells {
		// Enablers plus the target may overshoot a 17-land deck by a copy.
		return d, fmt.Errorf("certdeck: %d spells over the %d the deck holds", count, spells)
	}

	// Mana base: lands split by the coloured pips of the spells.
	var pips [5]int
	for c, n := range chosen {
		if b.facts(c).f.IsLand() {
			continue
		}
		p := manaPips(b.facts(c).f.ManaCost)
		for i := range pips {
			pips[i] += p[i] * n
		}
	}
	basics := basicNames
	if snow {
		basics = snowBasics
	}
	entries := map[string]int{}
	for c, n := range chosen {
		entries[b.facts(c).name] += n
	}
	for i, n := range apportion(pips, mask, lands) {
		if n > 0 {
			entries[basics[i]] += n
		}
	}
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		d.File.Cards = append(d.File.Cards, deck.Entry{Name: n, Count: entries[n]})
	}
	d.Colours = maskLetters(mask)
	return d, nil
}

// apportion splits n lands over the colours of mask in proportion to pips
// (largest remainder; ties go to the colour earlier in WUBRG), every colour of
// the mask getting at least one.
func apportion(pips [5]int, mask uint8, n int) [5]int {
	var w [5]int
	total := 0
	for i := 0; i < 5; i++ {
		if mask&(1<<i) != 0 {
			w[i] = pips[i]
			if w[i] == 0 {
				w[i] = 1
			}
			total += w[i]
		}
	}
	var out [5]int
	type rem struct{ i, r int }
	var rems []rem
	left := n
	for i := 0; i < 5; i++ {
		if w[i] == 0 {
			continue
		}
		out[i] = n * w[i] / total
		if out[i] == 0 {
			out[i] = 1
		}
		left -= out[i]
		rems = append(rems, rem{i, n * w[i] % total})
	}
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].r > rems[b].r })
	for k := 0; left != 0 && len(rems) > 0; k++ {
		r := rems[k%len(rems)]
		if left > 0 {
			out[r.i]++
			left--
		} else if out[r.i] > 1 {
			out[r.i]--
			left++
		}
	}
	return out
}

func maskLetters(m uint8) string {
	var s strings.Builder
	for i, l := range colourLetters {
		if m&(1<<i) != 0 {
			s.WriteString(l)
		}
	}
	return s.String()
}

// manaPips counts a Forge mana cost's coloured symbols per colour ("2 W/U B":
// a hybrid shard counts for both).
func manaPips(cost string) [5]int {
	var p [5]int
	for _, shard := range strings.Fields(cost) {
		for _, part := range strings.Split(shard, "/") {
			for i, l := range colourLetters {
				if part == l {
					p[i]++
				}
			}
		}
	}
	return p
}

// opponentColours is the colour pair for the opponent's deck: the pair of the
// ten that has the most candidates for its needs (each need counted up to the
// slots it can use), ties and an empty need list broken by a hash of the
// target, and never the target deck's own mask when another pair does as well.
func (b *Builder) opponentColours(target string, selfMask uint8, needs []Need) uint8 {
	var pairs []uint8
	for a := 0; a < 5; a++ {
		for c := a + 1; c < 5; c++ {
			pairs = append(pairs, 1<<a|1<<c)
		}
	}
	lists := make([][]cand, len(needs))
	for i, n := range needs {
		lists[i] = b.candidates("opponent\x00"+target, n, 31)
	}
	best, bestScore, bestTie := pairs[0], -1, uint64(0)
	for _, p := range pairs {
		score := 0
		for _, l := range lists {
			n := 0
			for _, c := range l {
				if b.facts(c.c).identity&^p == 0 && b.playable(c.c) {
					if n++; n >= 6 {
						break
					}
				}
			}
			score += n
		}
		if p == selfMask {
			score--
		}
		tie := rankKey(target, "opp-pair"+maskLetters(p))
		if score > bestScore || (score == bestScore && tie < bestTie) {
			best, bestScore, bestTie = p, score, tie
		}
	}
	return best
}

// candidates is the corpus cards that supply need n inside mask, best first:
// cheapest, then by hash. Whether a card is playable is checked by the caller,
// lazily.
func (b *Builder) candidates(salt string, n Need, mask uint8) []cand {
	var out []cand
	for _, c := range b.Reg.AllCards() {
		if len(c.Faces) == 0 {
			continue
		}
		x := b.facts(c)
		if x.identity&^mask != 0 || x.f.IsBasic() || x.f.HasTypeWord("Token") {
			continue
		}
		if !supplies(x, n) {
			continue
		}
		out = append(out, cand{c: c, key: rankKey(salt, x.name), cmc: x.cmc})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].cmc != out[j].cmc {
			return out[i].cmc < out[j].cmc
		}
		if out[i].key != out[j].key {
			return out[i].key < out[j].key
		}
		return b.facts(out[i].c).name < b.facts(out[j].c).name
	})
	return out
}

// supplies is whether card x gives the deck need n.
func supplies(x *facts, n Need) bool {
	f := x.f
	nonLand := !f.IsLand()
	switch n.Class {
	case Snow:
		return f.HasTypeWord("Snow") && nonLand && !f.IsInstant() && !f.IsSorcery()
	case Type, OppType:
		return f.IsCreature() && x.has(n.Arg)
	case Counters:
		return nonLand && x.api("PutCounter", func(u unit) bool { return u.p["CounterType"] == n.Arg }) &&
			!x.api("PutCounter", func(u unit) bool { return u.p["Defined"] == "Opponent" })
	case Token:
		return nonLand && x.api("Token", func(u unit) bool {
			if o := u.p["TokenOwner"]; o == "Opponent" || o == "Targeted" {
				return false
			}
			return n.Arg == "" || strings.Contains(strings.ToLower(u.p["TokenScript"]), n.Arg)
		})
	case Graveyard:
		return nonLand && (x.api("Mill", func(u unit) bool { return u.p["Defined"] == "You" }) ||
			x.api("Discard", func(u unit) bool { return u.p["Defined"] == "You" && u.p["Mode"] != "Random" }) ||
			x.api("Surveil", nil))
	case Equipment:
		return x.has("Equipment") && x.cmc <= 3
	case Aura:
		return x.has("Aura") && x.cmc <= 3 && f.HasKeyword("Enchant:Creature")
	case Bodies:
		return f.IsCreature() && x.cmc <= 3
	case Life:
		return nonLand && x.api("GainLife", func(u unit) bool { return u.p["Defined"] == "" || u.p["Defined"] == "You" }) && x.cmc <= 4
	case Artifacts:
		return nonLand && f.IsArtifact() && !f.IsCreature() && x.cmc <= 3
	case Enchantments:
		return nonLand && f.IsEnchantment() && !x.has("Aura") && x.cmc <= 3
	case Lands:
		return nonLand && x.cmc <= 4 && x.api("ChangeZone", func(u unit) bool {
			return u.p["Origin"] == "Library" && u.p["Destination"] == "Battlefield" && strings.Contains(u.p["ChangeType"], "Land")
		})
	case OppAttackers:
		return f.IsCreature() && x.cmc <= 3 && f.Power() >= 2
	case OppBlockers:
		return f.IsCreature() && x.cmc <= 3 && (f.Toughness() >= 3 || f.HasKeyword("Defender"))
	case OppTargets:
		switch n.Arg {
		case "creature":
			return f.IsCreature() && x.cmc <= 3
		case "artifact":
			return nonLand && f.IsArtifact() && x.cmc <= 3
		case "enchantment":
			return nonLand && f.IsEnchantment() && x.cmc <= 3
		case "planeswalker":
			return f.IsPlaneswalker() && x.cmc <= 4
		case "land":
			return f.IsLand() && !f.IsBasic()
		case "spell":
			return (f.IsInstant() || f.IsSorcery()) && x.cmc <= 3
		}
	case OppBurn:
		return nonLand && (f.IsInstant() || f.IsSorcery()) && x.cmc <= 3 &&
			x.api("DealDamage", func(u unit) bool {
				return strings.Contains(u.p["ValidTgts"], "Any") || strings.Contains(u.p["ValidTgts"], "Player")
			})
	}
	return false
}
