// Command rulingseeds lists the Scryfall rulings whose comment names a second
// card, as a pointer list for picking interaction-scenario seeds.
//
//	go run ./cmd/rulingseeds -rulings .oracle-build/scryfall/rulings.json \
//	    -cards .oracle-build/scryfall/oracle-cards.json -corpus .cards \
//	    > .oracle-build/scryfall/pairs.tsv
//
// The output is a TSV (ruled card, second card, published_at, both cards
// gorge-supported yes/no) and a final "#"-prefixed summary line. It is NEVER
// ruling text: the comments are Wizards' and are read only to find names, then
// dropped. Write the output to a gitignored path; never commit it.
//
// A second card is named when the comment contains another card's exact name
// as a whole phrase (token-aligned and written as the card prints it: "the moment"
// is prose, "The Moment" is a card). Names under 4
// characters and the stoplist of names that are ordinary words are ignored.
// Both the oracle file and the rulings file are streamed; each may be a JSON
// array or JSON Lines, and a .gz path is gunzipped on the fly.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// stoplist holds lower-case card names that are ordinary words (or rules
// vocabulary) in ruling prose, so a hit on them says nothing about a second
// card. Basic land names are here for the same reason.
var stoplist = map[string]bool{
	"exile": true, "plains": true, "island": true, "swamp": true, "mountain": true, "forest": true,
	"wastes": true, "flash": true, "fear": true, "dash": true, "fury": true, "ward": true,
	"armor": true, "bury": true, "return": true, "gift": true, "rift": true, "lure": true,
	"stun": true, "regenerate": true, "destroy": true, "sacrifice": true, "target": true,
	"counter": true, "token": true, "life": true, "death": true, "time": true, "light": true,
	"fire": true, "ice": true, "heat": true, "growth": true, "hunger": true, "rampage": true,
	"duress": true, "mind": true, "bind": true, "wrath": true, "fade": true, "fall": true,
	"flood": true, "storm": true, "crush": true, "cast": true, "play": true, "draw": true,
	"discard": true, "search": true, "reveal": true, "attach": true, "mill": true, "scry": true,
	"cycling": true, "banding": true, "flying": true, "trample": true, "haste": true,
	"menace": true, "reach": true, "vigilance": true, "defender": true, "deathtouch": true,
	"lifelink": true, "hexproof": true, "indestructible": true, "protection": true,
	"flanking": true, "shadow": true, "horsemanship": true, "infect": true, "afflict": true,
	"amass": true, "explore": true, "surveil": true, "proliferate": true, "populate": true,
	"investigate": true, "adapt": true, "connive": true, "learn": true, "manifest": true,
	"transform": true, "meld": true, "fight": true, "goad": true, "exert": true, "support": true,
	"embalm": true, "eternalize": true, "escape": true, "foretell": true, "plot": true,
	"disturb": true, "daybound": true, "nightbound": true, "cleave": true, "squad": true,
	"blitz": true, "casualty": true, "prototype": true, "toxic": true, "backup": true,
	"bargain": true, "craft": true, "disguise": true, "offspring": true, "impending": true, "two-headed giant": true, "the end": true, "suspend": true, "unearth": true,
	"cancel": true, "desert": true, "shapeshifter": true, "continue?": true, "flashback": true, "remove": true, "consider": true, "clear": true,
	"continue": true, "library": true, "lethal": true, "damage": true, "copy": true, "give": true, "take": true,
}

const minNameLen = 4

type card struct {
	ID      string `json:"oracle_id"`
	Name    string `json:"name"`
	Layout  string `json:"layout"`
	SetType string `json:"set_type"`
}

// notRealCard reports the oracle_cards entries that are not playable cards:
// art cards, tokens, emblems, planes, memorabilia (whose names are ordinary
// words such as "Counters") and the silver-border/funny sets.
func notRealCard(c card) bool {
	switch c.Layout {
	case "art_series", "token", "double_faced_token", "emblem", "vanguard", "scheme", "planar", "front_card", "host", "augment":
		return true
	}
	switch c.SetType {
	case "memorabilia", "token", "funny", "minigame":
		return true
	}
	return false
}

// cardIndex maps a phrase's first token to the phrases starting with it.
type cardIndex struct {
	byID    map[string]string      // oracle_id -> full card name
	byFirst map[string][]phraseRef // first token -> candidates
}

type phraseRef struct {
	tokens []string // as printed, case kept
	id     string
}

// rawTokens splits s into letter/digit runs, keeping case; every other rune
// (spaces, punctuation, apostrophes) is a boundary.
func rawTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// streamValues decodes a top-level JSON array, or a stream of concatenated
// values (JSON Lines, which is what Scryfall's bulk files now are), one
// element at a time.
func streamValues[T any](r io.Reader, each func(T)) error {
	br := bufio.NewReaderSize(r, 1<<20)
	dec := json.NewDecoder(br)
	array := false
	for {
		b, err := br.Peek(1)
		if err != nil {
			return nil // empty input
		}
		if b[0] == ' ' || b[0] == '\n' || b[0] == '\r' || b[0] == '\t' {
			br.ReadByte()
			continue
		}
		array = b[0] == '['
		break
	}
	if array {
		if _, err := dec.Token(); err != nil {
			return err
		}
	}
	for dec.More() {
		var v T
		if err := dec.Decode(&v); err != nil {
			return err
		}
		each(v)
	}
	if array {
		_, err := dec.Token()
		return err
	}
	return nil
}

// openData opens path, gunzipping it when it ends in .gz.
func openData(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil || !strings.HasSuffix(path, ".gz") {
		return f, err
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{zr, f}, nil
}

// loadCards streams an oracle_cards file into the phrase index. A card whose
// name is "Front // Back" is indexed under the whole name and each face.
func loadCards(r io.Reader) (*cardIndex, error) {
	idx := &cardIndex{byID: map[string]string{}, byFirst: map[string][]phraseRef{}}
	seen := map[string]bool{}
	err := streamValues(r, func(c card) {
		if c.ID == "" || c.Name == "" || notRealCard(c) {
			return
		}
		idx.byID[c.ID] = c.Name
		phrases := []string{c.Name}
		// A split card's halves ("Turn", "Fire") are ordinary words in
		// ruling prose; only its full name is indexed.
		if strings.Contains(c.Name, " // ") && c.Layout != "split" {
			phrases = append(phrases, strings.Split(c.Name, " // ")...)
		}
		for _, p := range phrases {
			if len(p) < minNameLen || stoplist[strings.ToLower(p)] {
				continue
			}
			toks := rawTokens(p)
			if len(toks) == 0 {
				continue
			}
			key := c.ID + "\x00" + strings.Join(toks, " ")
			if seen[key] {
				continue
			}
			seen[key] = true
			idx.byFirst[toks[0]] = append(idx.byFirst[toks[0]], phraseRef{toks, c.ID})
		}
	})
	return idx, err
}

// namedIDs returns the oracle ids of the cards named in comment, in order of
// appearance. The longest phrase at a position wins and its tokens are
// consumed, so a shorter name inside a longer one never counts.
func (x *cardIndex) namedIDs(comment string) []string {
	toks := rawTokens(comment)
	var out []string
	for i := 0; i < len(toks); {
		best := -1
		var bestRef phraseRef
		for _, c := range x.byFirst[toks[i]] {
			n := len(c.tokens)
			if n <= best || i+n > len(toks) {
				continue
			}
			ok := true
			for j := 1; j < n; j++ {
				if toks[i+j] != c.tokens[j] {
					ok = false
					break
				}
			}
			if ok {
				best, bestRef = n, c
			}
		}
		if best < 0 {
			i++
			continue
		}
		out = append(out, bestRef.id)
		i += best
	}
	return out
}

type ruling struct {
	ID      string `json:"oracle_id"`
	Date    string `json:"published_at"`
	Comment string `json:"comment"`
}

type pair struct {
	Ruled, Second, Date string
	RuledID, SecondID   string
}

type stats struct {
	Rulings, Unknown, Boilerplate int
}

// boilerplateMin is how many rulings must carry the identical comment for it
// to count as boilerplate (a template note stamped on every Clue or Food
// card, say) rather than a ruling about one interaction.
const boilerplateMin = 3

type hit struct {
	ruledID, secondID, date string
	hash                    uint64
}

// findPairs streams the rulings and returns the deduplicated, sorted
// (ruled, second, date) pairs. Boilerplate comments (identical text on
// boilerplateMin or more rulings) are dropped. A comment is reduced to a
// 64-bit hash as it is read and never retained.
func findPairs(r io.Reader, idx *cardIndex) ([]pair, stats, error) {
	var st stats
	var hits []hit
	count := map[uint64]int{}
	err := streamValues(r, func(rl ruling) {
		st.Rulings++
		h := fnv.New64a()
		io.WriteString(h, rl.Comment)
		sum := h.Sum64()
		count[sum]++
		if _, ok := idx.byID[rl.ID]; !ok {
			st.Unknown++
			return
		}
		for _, id := range idx.namedIDs(rl.Comment) {
			if id != rl.ID {
				hits = append(hits, hit{rl.ID, id, rl.Date, sum})
			}
		}
	})
	seen := map[pair]bool{}
	var out []pair
	for _, h := range hits {
		if count[h.hash] >= boilerplateMin {
			st.Boilerplate++
			continue
		}
		p := pair{Ruled: idx.byID[h.ruledID], Second: idx.byID[h.secondID], Date: h.date, RuledID: h.ruledID, SecondID: h.secondID}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Ruled != b.Ruled {
			return a.Ruled < b.Ruled
		}
		if a.Second != b.Second {
			return a.Second < b.Second
		}
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		return a.RuledID < b.RuledID
	})
	return out, st, err
}

func main() {
	rulings := flag.String("rulings", "", "Scryfall rulings bulk JSON")
	cards := flag.String("cards", "", "Scryfall oracle_cards bulk JSON")
	corpus := flag.String("corpus", ".cards", "corpus directory (holds cardsfolder/)")
	flag.Parse()
	if *rulings == "" || *cards == "" {
		fmt.Fprintln(os.Stderr, "rulingseeds: -rulings and -cards are required")
		os.Exit(2)
	}
	cf, err := openData(*cards)
	if err != nil {
		fatal(err)
	}
	idx, err := loadCards(cf)
	cf.Close()
	if err != nil {
		fatal(fmt.Errorf("%s: %w", *cards, err))
	}
	rf, err := openData(*rulings)
	if err != nil {
		fatal(err)
	}
	pairs, st, err := findPairs(rf, idx)
	rf.Close()
	if err != nil {
		fatal(fmt.Errorf("%s: %w", *rulings, err))
	}

	reg, err := testutil.OpenCorpusRegistry(*corpus)
	if err != nil {
		fatal(err)
	}
	sup := effects.Supported()
	supported := map[string]bool{}
	isSupported := func(name string) bool {
		if v, ok := supported[name]; ok {
			return v
		}
		c, ok := reg.Lookup(name)
		v := ok && len(reg.Unsupported(c, sup)) == 0
		supported[name] = v
		return v
	}

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	fmt.Fprintln(w, "ruled_card\tsecond_card\tpublished_at\tboth_supported")
	both := 0
	ruled := map[string]bool{}
	for _, p := range pairs {
		yn := "no"
		if isSupported(p.Ruled) && isSupported(p.Second) {
			yn = "yes"
			both++
		}
		ruled[p.RuledID] = true
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Ruled, p.Second, p.Date, yn)
	}
	fmt.Fprintf(w, "# summary: rulings=%d boilerplate_hits_dropped=%d pairs=%d pairs_both_supported=%d distinct_ruled_cards=%d\n",
		st.Rulings, st.Boilerplate, len(pairs), both, len(ruled))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "rulingseeds:", err)
	os.Exit(1)
}
