package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/deck"
	"github.com/adams-shaun/gorge/engine/effects"
)

// decksFlagErr is the error of a -decks run whose other flags make no sense
// with it, or nil. -decks replaces the deck source, as -pool does: -pool's
// card list, -random's cross-set union, its -forge-only extras and -dirac's
// per-set table draw (a dirac table holds one set's cards; a metagame deck is
// not a set) all build the decks another way. -set still names the run in the
// item ids.
func decksFlagErr(dir, pool, random, forgeOnly string, dirac bool) error {
	if dir == "" {
		return nil
	}
	var bad []string
	if pool != "" {
		bad = append(bad, "-pool")
	}
	if random != "" {
		bad = append(bad, "-random")
	}
	if forgeOnly != "" {
		bad = append(bad, "-forge-only")
	}
	if dirac {
		bad = append(bad, "-dirac")
	}
	if len(bad) > 0 {
		return fmt.Errorf("-decks is the whole deck source: it cannot be combined with %s", strings.Join(bad, ", "))
	}
	return nil
}

// Skip reasons of a -decks deck file, the first word of deckEntry.Skip.
const (
	deckSkipParse       = "parse"         // deck.Parse refused the file
	deckSkipCommander   = "commander"     // a Commander deck: -decks is two-seat constructed
	deckSkipNotInCorpus = "not_in_corpus" // a main-deck card the registry does not know
	deckSkipUnsupported = "unsupported"   // a main-deck card gorge does not fully support
	deckSkipXMage       = decks.ReasonXMageSpelling
	deckSkipDuplicate   = "duplicate_name" // another file (earlier in file order) has the name
)

// deckEntry is one deck file of a -decks run as DIR/decks.json records it.
type deckEntry struct {
	File   string `json:"file"`
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Cards  int    `json:"cards,omitempty"` // main-deck size
	// Skip is "" for a playable deck, else its reason: one of the deckSkip
	// words, then the detail ("unsupported Foo [Bar]").
	Skip string     `json:"skip,omitempty"`
	deck decks.Deck // the playable deck as dealt
}

// decksInfo is DIR/decks.json of a -decks run.
type decksInfo struct {
	Dir      string         `json:"dir"`
	Decks    []deckEntry    `json:"decks"`
	Playable int            `json:"playable"`
	Skipped  map[string]int `json:"skipped,omitempty"` // skip reason word -> count
}

// String is the one-line summary play prints.
func (d decksInfo) String() string {
	s := fmt.Sprintf("decks %s: %d decks, %d playable", d.Dir, len(d.Decks), d.Playable)
	keys := make([]string, 0, len(d.Skipped))
	for k := range d.Skipped {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s += fmt.Sprintf(", %d %s", d.Skipped[k], k)
	}
	return s
}

// loadDeckDir reads every *.json of dir as a gorge deck file (engine/deck's
// format: testkit/testutil/decks, cmd/deckimport) and checks each once: its
// main deck (the sideboard is ignored) must resolve in reg and every card of
// it must be one gorge fully supports (reg.Unsupported, as decks.Candidates
// applies to a set's cards). A Commander deck is skipped: -decks seats two
// constructed decks. With sp (play -xmage), a deck naming a card XMage spells
// otherwise is skipped too: an XMage item carries no per-card spelling. A
// deck failing a check is kept in the info with its reason; the playable
// decks come back sorted by name, each card in the corpus spelling of its
// front face. Fewer than two playable decks is an error.
func loadDeckDir(reg *cards.Registry, dir string, sp *decks.Spellings) ([]decks.Deck, decksInfo, error) {
	info := decksInfo{Dir: dir, Decks: []deckEntry{}, Skipped: map[string]int{}}
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, info, err
	}
	sort.Strings(paths)
	sup := effects.Supported()
	byName := map[string]string{}
	var playable []decks.Deck
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, info, err
		}
		sum := sha256.Sum256(raw)
		e := deckEntry{File: filepath.Base(p), SHA256: hex.EncodeToString(sum[:])}
		e.Name, e.Cards, e.deck, e.Skip = checkDeck(reg, raw, deck.Stem(p), sup, sp)
		if e.Skip == "" {
			if prev, ok := byName[e.Name]; ok {
				e.Skip = fmt.Sprintf("%s of %s", deckSkipDuplicate, prev)
			} else {
				byName[e.Name] = e.File
			}
		}
		if e.Skip == "" {
			playable = append(playable, e.deck)
			info.Playable++
		} else {
			r, _, _ := strings.Cut(e.Skip, " ")
			info.Skipped[r]++
		}
		info.Decks = append(info.Decks, e)
	}
	if len(paths) == 0 {
		return nil, info, fmt.Errorf("-decks %s: no *.json deck files", dir)
	}
	if len(playable) < 2 {
		return nil, info, fmt.Errorf("-decks %s: %d playable deck(s) of %d, need 2 (%s)", dir, len(playable), len(paths), info)
	}
	sort.Slice(playable, func(i, j int) bool { return playable[i].Name < playable[j].Name })
	return playable, info, nil
}

// checkDeck parses one deck file and is its name (the file's, else stem),
// main-deck size, the deck as dealt, and its skip reason ("" when playable).
func checkDeck(reg *cards.Registry, raw []byte, stem string, sup map[string]bool, sp *decks.Spellings) (string, int, decks.Deck, string) {
	f, err := deck.Parse(raw)
	if err != nil {
		return stem, 0, decks.Deck{}, fmt.Sprintf("%s %v", deckSkipParse, err)
	}
	name := strings.TrimSpace(f.Name)
	if name == "" {
		name = stem
	}
	size := 0
	for _, e := range f.Cards {
		size += e.Count
	}
	if len(f.CommanderNames()) > 0 || strings.EqualFold(f.Format, "commander") {
		return name, size, decks.Deck{}, deckSkipCommander
	}
	var missing, unsup, spelt []string
	var identity uint8
	names := make([]string, 0, size)
	for _, e := range f.Cards {
		c, ok := reg.Lookup(e.Name)
		if !ok || len(c.Faces) == 0 {
			missing = append(missing, e.Name)
			continue
		}
		front := c.Faces[0].Name
		if u := reg.Unsupported(c, sup); len(u) > 0 {
			unsup = append(unsup, fmt.Sprintf("%s %v", front, u))
		}
		if sp != nil && sp.Differs(reg, front) {
			spelt = append(spelt, front)
		}
		identity |= c.ColourIdentity()
		for i := 0; i < e.Count; i++ {
			names = append(names, front)
		}
	}
	switch {
	case len(missing) > 0:
		return name, size, decks.Deck{}, fmt.Sprintf("%s %s", deckSkipNotInCorpus, strings.Join(missing, "; "))
	case len(unsup) > 0:
		return name, size, decks.Deck{}, fmt.Sprintf("%s %s", deckSkipUnsupported, strings.Join(unsup, "; "))
	case len(spelt) > 0:
		return name, size, decks.Deck{}, fmt.Sprintf("%s %s", deckSkipXMage, strings.Join(spelt, "; "))
	}
	sort.Strings(names)
	return name, size, decks.Deck{Name: name, Colours: identityLetters(identity), Cards: names}, ""
}

// identityLetters is a colour mask as WUBRG letters, "C" when it is empty: the
// Colours of a fixed deck (game.Play names the seat after it).
func identityLetters(m uint8) string {
	var b strings.Builder
	for i, l := range []string{"W", "U", "B", "R", "G"} {
		if m&(1<<i) != 0 {
			b.WriteString(l)
		}
	}
	if b.Len() == 0 {
		return "C"
	}
	return b.String()
}

// fixedPair is the two playable decks game g of chunk plays under seed: a
// pure function of (the sorted deck names, chunk, game, seed). A PCG seeded
// from sha256("xengine-decks", each name, chunk, game, seed) draws an
// unordered pair {a < b} of distinct indexes uniformly (a from n, b from the
// n-1 others). Seat 0 takes a when chunk+game is even and b when it is odd,
// so over a run each deck sits in seat 0 in about half its games. Who is on
// the play is the game's own starting-player toss (the tape's Starter), which
// the seat order feeds. n < 2 panics: loadDeckDir refuses it.
func fixedPair(names []string, chunk, game int, seed uint64) (seat0, seat1 int) {
	n := len(names)
	if n < 2 {
		panic(fmt.Sprintf("fixedPair: %d deck(s)", n))
	}
	h := sha256.New()
	h.Write([]byte("xengine-decks\x00"))
	for _, s := range names {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	fmt.Fprintf(h, "%d\x00%d\x00%d", chunk, game, seed)
	sum := h.Sum(nil)
	rng := rand.New(rand.NewPCG(binary.LittleEndian.Uint64(sum[:8]), binary.LittleEndian.Uint64(sum[8:16])))
	a := rng.IntN(n)
	b := rng.IntN(n - 1)
	if b >= a {
		b++
	}
	if a > b {
		a, b = b, a
	}
	if (chunk+game)%2 == 1 {
		return b, a
	}
	return a, b
}

// fixedDealer deals game g its fixedPair of ds (sorted by name, as
// loadDeckDir returns them). No dirac table plays a fixed deck.
func fixedDealer(ds []decks.Deck, chunk int, seed uint64) dealer {
	names := make([]string, len(ds))
	for i, d := range ds {
		names[i] = d.Name
	}
	return func(g int) ([]decks.Deck, string) {
		a, b := fixedPair(names, chunk, g, seed)
		return []decks.Deck{ds[a], ds[b]}, ""
	}
}
