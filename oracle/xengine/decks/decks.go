// Package decks builds the two-colour Standard-set decks the xengine sweep
// plays: Candidates picks the cards of a set gorge fully supports, Pair draws
// two 40-card decks from them, weighted toward the least-exercised cards.
package decks

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
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
}

// Skip is a manifest card that is not a candidate, and why. Reasons:
// "unfinished", "not in corpus", "unsupported [...]", "basic", "back face of X",
// "duplicate of X" and (Options.XMage only) "xmage_spelling".
type Skip struct{ Card, Reason string }

// Deck is one 40-card deck: Cards is sorted, basics repeated.
type Deck struct {
	Name    string
	Colours string // the two colour letters in WUBRG order, e.g. "WU"
	Cards   []string
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
	return out, skips, nil
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

// colourPair is a pair of indexes into WUBRG, a < b.
type colourPair [2]int

// colourPairs is the 10 pairs in a fixed order.
var colourPairs = func() []colourPair {
	var out []colourPair
	for a := 0; a < 5; a++ {
		for b := a + 1; b < 5; b++ {
			out = append(out, colourPair{a, b})
		}
	}
	return out
}()

func (p colourPair) mask() uint8 { return 1<<p[0] | 1<<p[1] }

func (p colourPair) letters() string { return colourLetters[p[0]] + colourLetters[p[1]] }

func eligible(cands []Card, p colourPair) []Card {
	var out []Card
	for _, c := range cands {
		if c.Identity&^p.mask() == 0 {
			out = append(out, c)
		}
	}
	return out
}

// FeasiblePairs lists, in rotation order, the colour pairs ("WU", "WB", ...)
// with at least 23 eligible candidates: the only pairs Pair can build a deck
// for. A set with fewer than 23 cards in every pair has none.
func FeasiblePairs(cands []Card) []string {
	var out []string
	for _, p := range colourPairs {
		if len(eligible(cands, p)) >= spellCount {
			out = append(out, p.letters())
		}
	}
	return out
}

// Pairable is nil when Pair can build decks from cands, else an error naming
// the best pair. Callers check it before Pair, which panics when it is not nil.
func Pairable(cands []Card) error {
	if len(FeasiblePairs(cands)) > 0 {
		return nil
	}
	best, bestN := "", -1
	for _, p := range colourPairs {
		if n := len(eligible(cands, p)); n > bestN {
			best, bestN = p.letters(), n
		}
	}
	return fmt.Errorf("no colour pair has %d eligible candidates (%d candidates; best pair %s has %d)", spellCount, len(cands), best, bestN)
}

// Reachable is the number of candidates that fit at least one feasible colour
// pair, i.e. can appear in some deck, and the names of those that cannot. A
// card whose identity needs three colours, or whose only pairs have fewer than
// 23 eligible cards, is unreachable: the 23-distinct-cards rule makes this
// unavoidable, so sweeps report it instead of hiding it.
func Reachable(cands []Card) (reachable int, unreachable []string) {
	var masks []uint8
	for _, p := range colourPairs {
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

// Pair is deck pair game of chunk for set: a pure function of
// (set, chunk, game, the ledger's contents, seed). Deck i takes colour pair
// (7*chunk + 2*game + i) mod 10, advanced to the next pair with at least 23
// eligible cards, so the 10 pairs rotate. A candidate that fits no such pair
// is never drawn (see Reachable). Spells are drawn without replacement with
// weight 1/(1+uses), uses being the card's exercised total in led (nil reads
// as empty). It panics when Pairable(cands) is not nil: that is a caller bug.
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
	var out [2]Deck
	for i := range out {
		start := ((7*chunk+2*game+i)%10 + 10) % 10
		pi := -1
		for k := 0; k < 10; k++ {
			if len(eligible(cands, colourPairs[(start+k)%10])) >= spellCount {
				pi = (start + k) % 10
				break
			}
		}
		if pi < 0 {
			panic(fmt.Sprintf("decks.Pair: %s: %v", set, Pairable(cands)))
		}
		p := colourPairs[pi]
		spells := draw(eligible(cands, p), led, rng)
		var pa, pb int
		names := make([]string, 0, deckSize)
		for _, s := range spells {
			names = append(names, s.Name)
			pa += s.Pips[p[0]]
			pb += s.Pips[p[1]]
		}
		na := basicCount / 2
		if pa+pb > 0 {
			na = (basicCount*pa + (pa+pb)/2) / (pa + pb)
		}
		na = min(max(na, minBasic), basicCount-minBasic)
		for j := 0; j < na; j++ {
			names = append(names, basicNames[p[0]])
		}
		for j := na; j < basicCount; j++ {
			names = append(names, basicNames[p[1]])
		}
		sort.Strings(names)
		colours := p.letters()
		out[i] = Deck{Name: fmt.Sprintf("%s-c%d-g%d-%d-%s", set, chunk, game, i, colours), Colours: colours, Cards: names}
	}
	return out
}

// draw picks spellCount distinct cards without replacement, each pick
// proportional to 1/(1+uses). pool is in manifest order, so the draw is
// deterministic for a given rng.
func draw(pool []Card, led *cov.Ledger, rng *rand.Rand) []Card {
	pool = append([]Card(nil), pool...)
	w := make([]float64, len(pool))
	total := 0.0
	for i, c := range pool {
		w[i] = 1 / float64(1+led.Uses(c.Name))
		total += w[i]
	}
	var out []Card
	for len(out) < spellCount {
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
