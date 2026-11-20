// Package decks builds the Standard-set decks the xengine sweep plays:
// Candidates picks the cards of a set gorge fully supports, Pair draws two
// 40-card decks from them, weighted toward the least-exercised cards, and
// PairIdentity draws both decks of one colour identity for a -pool header.
package decks

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/bits"
	"math/rand/v2"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
)

// Card is one candidate: a manifest card gorge fully supports.
type Card struct {
	Name     string // corpus spelling of the front face
	Identity uint8  // cards.Colour* mask over every face
	Land     bool   // front face is a land
	Pips     [5]int // coloured mana symbols of the front face, WUBRG order
	Child    bool   // a child set's card in a child-plus-parent pool (WithParent)
}

// Skip is a manifest card that is not a candidate, and why. Reasons:
// "unfinished", "not in corpus", "unsupported [...]", "basic", "back face of X",
// "duplicate of X" and (Options.XMage only) "xmage_spelling".
type Skip struct{ Card, Reason string }

// ReasonOutsideIdentity is the skip of a -pool name whose colour identity does
// not fit the pool's "# supp <IDENTITY>" header. suppq.py computes the identity
// from a Forge script by approximation; gorge computes its own, so a name the
// header does not cover is left out and counted, not dealt.
const ReasonOutsideIdentity = "outside_identity"

// Deck is one deck: Cards is sorted, basics repeated. Pair builds 40-card set
// decks, a Commander deck has 100 cards, and play -decks deals fixed lists of
// any size.
type Deck struct {
	Name    string
	Colours string // the colour identity's letters in WUBRG order, e.g. "WU", "WUB" or "WUBRG"; a Commander or fixed deck's identity ("C" colourless)
	Cards   []string
	// Commander is the commander's corpus spelling (front face name), "" for a
	// constructed deck.
	Commander string
}

// CommanderIndex is the index in d.Cards of the first entry equal to
// d.Commander, or -1 when d.Commander is "" or absent. It is the
// rules.Config.Commanders entry for the deck.
func (d Deck) CommanderIndex() int {
	if d.Commander == "" {
		return -1
	}
	for i, n := range d.Cards {
		if n == d.Commander {
			return i
		}
	}
	return -1
}

const (
	deckSize   = 40
	basicCount = 17
	minBasic   = 7
	spellCount = deckSize - basicCount
)

var colourLetters = [5]string{"W", "U", "B", "R", "G"}
var basicNames = [5]string{"Plains", "Island", "Swamp", "Mountain", "Forest"}

// Candidates is set's manifest cards (manifestDir/<set>.json) minus the set's
// unfinished names, resolved to the corpus spelling and kept only when gorge
// fully supports them. It makes the same calls as oraclediff's genManifest
// (cmd/oraclediff/main.go): compliance.CorpusNameFold, then reg.Unsupported.
// Basic lands are skipped. Both lists are in manifest order.
func Candidates(reg *cards.Registry, manifestDir, set string) ([]Card, []Skip, error) {
	return CandidatesWith(reg, manifestDir, set, Options{})
}

// CandidatesWith is Candidates under opt. With opt.XMage a card the manifest
// lists under another name than the corpus spelling (the manifest is XMage's
// own list, so that is XMage's spelling: Spellings.Differs agrees) is a Skip
// "xmage_spelling" instead of a candidate; ExcludedXMage counts them.
func CandidatesWith(reg *cards.Registry, manifestDir, set string, opt Options) ([]Card, []Skip, error) {
	m, err := compliance.LoadManifest(manifestDir, set)
	if err != nil {
		return nil, nil, err
	}
	out, skips := candidatesOf(reg, m, opt)
	return out, skips, nil
}

// CandidatesNamed is CandidatesWith over a list of card names instead of a set's
// manifest: the cards of a set XMage has no class for.
func CandidatesNamed(reg *cards.Registry, names []string, opt Options) ([]Card, []Skip) {
	var m compliance.Manifest
	for _, n := range names {
		m.Cards = append(m.Cards, compliance.ManifestCard{Name: n})
	}
	return candidatesOf(reg, m, opt)
}

func candidatesOf(reg *cards.Registry, m compliance.Manifest, opt Options) ([]Card, []Skip) {
	sup := effects.Supported()
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)
	unfinished := map[string]bool{}
	for _, u := range m.Unfinished {
		unfinished[compliance.FoldName(u)] = true
	}
	listed := map[string]bool{}
	for _, mc := range m.Cards {
		listed[compliance.FoldName(mc.Name)] = true
	}
	var out []Card
	var skips []Skip
	seen := map[string]bool{}
	for _, mc := range m.Cards {
		if unfinished[compliance.FoldName(mc.Name)] {
			skips = append(skips, Skip{mc.Name, "unfinished"})
			continue
		}
		name, ok := compliance.CorpusNameFold(has, folded, mc.Name)
		if !ok {
			skips = append(skips, Skip{mc.Name, "not in corpus"})
			continue
		}
		c, found := reg.Lookup(name)
		if !found || len(c.Faces) == 0 {
			skips = append(skips, Skip{mc.Name, "not in corpus"})
			continue
		}
		name = c.Faces[0].Name
		if name != mc.Name && compliance.FoldName(name) != compliance.FoldName(mc.Name) && listed[compliance.FoldName(name)] {
			skips = append(skips, Skip{mc.Name, "back face of " + name})
			continue
		}
		if seen[name] {
			skips = append(skips, Skip{mc.Name, "duplicate of " + name})
			continue
		}
		seen[name] = true
		if u := reg.Unsupported(c, sup); len(u) > 0 {
			skips = append(skips, Skip{name, fmt.Sprintf("unsupported %v", u)})
			continue
		}
		f := c.Faces[0]
		if hasType(f.Types, "Basic") && hasType(f.Types, "Land") {
			skips = append(skips, Skip{name, "basic"})
			continue
		}
		if opt.XMage && mc.Name != name {
			skips = append(skips, Skip{name, ReasonXMageSpelling})
			continue
		}
		out = append(out, Card{Name: name, Identity: c.ColourIdentity(), Land: hasType(f.Types, "Land"), Pips: pips(f.ManaCost)})
	}
	return out, skips
}

func hasType(types []string, t string) bool {
	for _, x := range types {
		if x == t {
			return true
		}
	}
	return false
}

// pips counts the coloured symbols of a Forge mana cost ("2 W/U B"): each
// colour letter of a shard counts once (a hybrid shard counts for both).
func pips(cost string) [5]int {
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

// Union is the lists' cards, each name once, in list order: the candidates of
// decks drawn across sets.
func Union(lists ...[]Card) []Card {
	seen := map[string]bool{}
	var out []Card
	for _, l := range lists {
		for _, c := range l {
			if !seen[c.Name] {
				seen[c.Name] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// colourCombo is a deck's colours: indexes into WUBRG, ascending. The rotation
// is the 10 pairs, then (for a set that needs them) the 10 three-colour
// combinations, the 5 shards and the 5 wedges.
type colourCombo []int

var pairCombos, tripleCombos = func() (pairs, triples []colourCombo) {
	for a := 0; a < 5; a++ {
		for b := a + 1; b < 5; b++ {
			pairs = append(pairs, colourCombo{a, b})
			for c := b + 1; c < 5; c++ {
				triples = append(triples, colourCombo{a, b, c})
			}
		}
	}
	return pairs, triples
}()

// combosFor is the colour combinations cands rotate through: the 10 pairs, plus
// the 10 triples only when some candidate's identity needs three colours, so a
// two-colour set's rotation is the same as it was before triples existed.
func combosFor(cands []Card) []colourCombo {
	for _, c := range cands {
		if bits.OnesCount8(c.Identity) == 3 {
			return append(append([]colourCombo(nil), pairCombos...), tripleCombos...)
		}
	}
	return pairCombos
}

func (p colourCombo) mask() uint8 {
	var m uint8
	for _, i := range p {
		m |= 1 << i
	}
	return m
}

func (p colourCombo) letters() string {
	var s string
	for _, i := range p {
		s += colourLetters[i]
	}
	return s
}

// ParseIdentity reads a colour identity ("W", "WU", ..., "WUBRG") into a
// cards.Colour* mask: one to five distinct WUBRG letters in WUBRG order. It is
// the grammar of a -pool "# supp <IDENTITY>" header.
func ParseIdentity(s string) (uint8, error) {
	if s == "" {
		return 0, fmt.Errorf("empty colour identity")
	}
	var m uint8
	last := -1
	for _, r := range s {
		i := strings.IndexRune("WUBRG", r)
		if i < 0 {
			return 0, fmt.Errorf("%q: %q is not a WUBRG colour", s, r)
		}
		if i <= last {
			return 0, fmt.Errorf("%q: colours must be in WUBRG order, each once", s)
		}
		last = i
		m |= 1 << i
	}
	return m, nil
}

// comboOf is the colour combination of an identity mask: its colours in WUBRG
// order. A mask with no colours (colourless) has an empty combination; a pool
// header always names at least one.
func comboOf(m uint8) colourCombo {
	var p colourCombo
	for i := range colourLetters {
		if m&(1<<i) != 0 {
			p = append(p, i)
		}
	}
	return p
}

func eligible(cands []Card, p colourCombo) []Card {
	var out []Card
	for _, c := range cands {
		if c.Identity&^p.mask() == 0 {
			out = append(out, c)
		}
	}
	return out
}

// FeasiblePairs lists, in rotation order, the colour combinations ("WU", "WB",
// ..., and "WUB"... when the set has a three-colour candidate) with at least 23
// eligible candidates: the only ones Pair can build a deck for.
func FeasiblePairs(cands []Card) []string {
	var out []string
	for _, p := range combosFor(cands) {
		if len(eligible(cands, p)) >= spellCount {
			out = append(out, p.letters())
		}
	}
	return out
}

// Pairable is nil when Pair can build decks from cands, else an error naming
// the best combination. Callers check it before Pair, which panics when it is
// not nil.
func Pairable(cands []Card) error {
	if len(FeasiblePairs(cands)) > 0 {
		return nil
	}
	best, bestN := "", -1
	for _, p := range combosFor(cands) {
		if n := len(eligible(cands, p)); n > bestN {
			best, bestN = p.letters(), n
		}
	}
	return fmt.Errorf("no colour pair has %d eligible candidates (%d candidates; best pair %s has %d)", spellCount, len(cands), best, bestN)
}

// Reachable is the number of candidates that fit at least one feasible colour
// combination, i.e. can appear in some deck, and the names of those that cannot.
// A card whose identity needs four colours, or whose only combinations have
// fewer than 23 eligible cards, is unreachable: the 23-distinct-cards rule makes
// this unavoidable, so sweeps report it instead of hiding it.
func Reachable(cands []Card) (reachable int, unreachable []string) {
	var masks []uint8
	for _, p := range combosFor(cands) {
		if len(eligible(cands, p)) >= spellCount {
			masks = append(masks, p.mask())
		}
	}
	for _, c := range cands {
		ok := false
		for _, m := range masks {
			if c.Identity&^m == 0 {
				ok = true
				break
			}
		}
		if ok {
			reachable++
		} else {
			unreachable = append(unreachable, c.Name)
		}
	}
	return reachable, unreachable
}

// ParentSets maps a bonus-sheet set too small to deal alone to the set whose
// cards fill its decks (WithParent).
var ParentSets = map[string]string{"BIG": "OTJ"}

// MinChild is the number of a child set's cards each deck of a child-plus-parent
// pool holds, when the child has that many in the deck's colours.
const MinChild = 8

// WithParent is the candidates of a child set dealt with its parent: the child's
// cards first, marked Child, then the parent's not already listed. Pair gives
// every deck at least MinChild Child cards when the colours hold them.
func WithParent(child, parent []Card) []Card {
	var out []Card
	for _, c := range child {
		c.Child = true
		out = append(out, c)
	}
	return Union(out, parent)
}

// Pair is deck pair game of chunk for set: a pure function of
// (set, chunk, game, the ledger's contents, seed). Deck i takes colour
// combination (7*chunk + 2*game + i) mod n of the rotation (the 10 pairs, plus
// the 10 triples for a set with a three-colour candidate), advanced to the next
// one with at least 23 eligible cards. A candidate that fits no such
// combination is never drawn (see Reachable). Spells are drawn without
// replacement with weight 1/(1+uses), uses being the card's exercised total in
// led (nil reads as empty); with Child cards in the pool, MinChild of them are
// drawn first. A three-colour deck splits its 17 basics evenly. It panics when
// Pairable(cands) is not nil: that is a caller bug.
func Pair(cands []Card, led *cov.Ledger, set string, chunk, game int, seed uint64) [2]Deck {
	if led == nil {
		led = cov.New()
	}
	var tsv bytes.Buffer
	led.WriteTSV(&tsv)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\x00", set, chunk, game, seed)
	h.Write(tsv.Bytes())
	sum := h.Sum(nil)
	rng := rand.New(rand.NewPCG(binary.LittleEndian.Uint64(sum[:8]), binary.LittleEndian.Uint64(sum[8:16])))
	combos := combosFor(cands)
	n := len(combos)
	var out [2]Deck
	for i := range out {
		start := ((7*chunk+2*game+i)%n + n) % n
		pi := -1
		for k := 0; k < n; k++ {
			if len(eligible(cands, combos[(start+k)%n])) >= spellCount {
				pi = (start + k) % n
				break
			}
		}
		if pi < 0 {
			panic(fmt.Sprintf("decks.Pair: %s: %v", set, Pairable(cands)))
		}
		p := combos[pi]
		out[i] = deckOf(cands, p, led, rng, fmt.Sprintf("%s-c%d-g%d-%d-%s", set, chunk, game, i, p.letters()))
	}
	return out
}

// PairableIdentity is nil when PairIdentity can build decks of identity from
// cands, else an error naming how many eligible candidates that identity has.
// Callers check it before PairIdentity, which panics when it is not nil.
func PairableIdentity(cands []Card, identity uint8) error {
	p := comboOf(identity)
	if n := len(eligible(cands, p)); n < spellCount {
		return fmt.Errorf("no %s deck has %d eligible candidates (%d candidates; %s has %d)", p.letters(), spellCount, len(cands), p.letters(), n)
	}
	return nil
}

// PairIdentity is Pair for a pool whose "# supp" header names one colour
// identity: both decks are that identity, every spell fits it and the 17
// basics are its colours. Identity may name one to five colours. It is a pure
// function of the same inputs as Pair plus identity, and panics when
// PairableIdentity(cands, identity) is not nil: that is a caller bug.
func PairIdentity(cands []Card, led *cov.Ledger, set string, chunk, game int, seed uint64, identity uint8) [2]Deck {
	if led == nil {
		led = cov.New()
	}
	if err := PairableIdentity(cands, identity); err != nil {
		panic(fmt.Sprintf("decks.PairIdentity: %s: %v", set, err))
	}
	var tsv bytes.Buffer
	led.WriteTSV(&tsv)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\x00%02x\x00", set, chunk, game, seed, identity)
	h.Write(tsv.Bytes())
	sum := h.Sum(nil)
	rng := rand.New(rand.NewPCG(binary.LittleEndian.Uint64(sum[:8]), binary.LittleEndian.Uint64(sum[8:16])))
	p := comboOf(identity)
	var out [2]Deck
	for i := range out {
		out[i] = deckOf(cands, p, led, rng, fmt.Sprintf("%s-c%d-g%d-%d-%s", set, chunk, game, i, p.letters()))
	}
	return out
}

// deckOf builds one deck of colour combination p: spellCount spells drawn from
// cands against led with rng, then basicCount basics of p's colours.
func deckOf(cands []Card, p colourCombo, led *cov.Ledger, rng *rand.Rand, name string) Deck {
	spells := drawDeck(eligible(cands, p), led, rng)
	var pips [5]int
	names := make([]string, 0, deckSize)
	for _, s := range spells {
		names = append(names, s.Name)
		for _, ci := range p {
			pips[ci] += s.Pips[ci]
		}
	}
	for j, k := range basicSplit(p, pips) {
		for ; k > 0; k-- {
			names = append(names, basicNames[p[j]])
		}
	}
	sort.Strings(names)
	return Deck{Name: name, Colours: p.letters(), Cards: names}
}

// basicSplit is the basicCount basics per colour of p: a pair splits by pips
// (at least minBasic each), three or more colours split evenly.
func basicSplit(p colourCombo, pips [5]int) []int {
	if len(p) == 2 {
		pa, pb := pips[p[0]], pips[p[1]]
		na := basicCount / 2
		if pa+pb > 0 {
			na = (basicCount*pa + (pa+pb)/2) / (pa + pb)
		}
		na = min(max(na, minBasic), basicCount-minBasic)
		return []int{na, basicCount - na}
	}
	out := make([]int, len(p))
	for j := range out {
		out[j] = basicCount / len(p)
		if j < basicCount%len(p) {
			out[j]++
		}
	}
	return out
}

// drawDeck is the deck's spells from pool: spellCount cards, the first MinChild
// of them from the pool's Child cards when it has any.
func drawDeck(pool []Card, led *cov.Ledger, rng *rand.Rand) []Card {
	var child, rest []Card
	for _, c := range pool {
		if c.Child {
			child = append(child, c)
		} else {
			rest = append(rest, c)
		}
	}
	if len(child) == 0 {
		return draw(pool, spellCount, led, rng)
	}
	first := draw(child, min(MinChild, len(child)), led, rng)
	taken := map[string]bool{}
	for _, c := range first {
		taken[c.Name] = true
	}
	var left []Card
	for _, c := range pool {
		if !taken[c.Name] {
			left = append(left, c)
		}
	}
	return append(first, draw(left, spellCount-len(first), led, rng)...)
}

// draw picks k distinct cards without replacement, each pick
// proportional to 1/(1+uses). pool is in manifest order, so the draw is
// deterministic for a given rng.
func draw(pool []Card, k int, led *cov.Ledger, rng *rand.Rand) []Card {
	pool = append([]Card(nil), pool...)
	w := make([]float64, len(pool))
	total := 0.0
	for i, c := range pool {
		w[i] = 1 / float64(1+led.Uses(c.Name))
		total += w[i]
	}
	var out []Card
	for len(out) < k {
		x := rng.Float64() * total
		pick := len(pool) - 1
		for i := range pool {
			if x < w[i] {
				pick = i
				break
			}
			x -= w[i]
		}
		out = append(out, pool[pick])
		total -= w[pick]
		pool = append(pool[:pick], pool[pick+1:]...)
		w = append(w[:pick], w[pick+1:]...)
	}
	return out
}
