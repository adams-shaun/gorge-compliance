package decks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/deck"
	"github.com/adams-shaun/gorge/engine/effects"
)

// Skip reasons of CommanderDecks (the others are "invalid: ...",
// "not in corpus: ..." and "unsupported [...]").
const (
	SkipNotCommander = "not_commander"
	SkipPartner      = "partner_commanders"
)

// CommanderFile is one parsed deck file: its file-name stem, the sha256 of its
// bytes (hex) and the parsed list.
type CommanderFile struct {
	Stem, SHA256 string
	File         deck.File
}

// ReadCommanderDir reads every *.json in dir, in name order. A file that does
// not parse is an error naming it; so is a directory with no deck file.
func ReadCommanderDir(dir string) ([]CommanderFile, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("decks: no *.json deck file in %s", dir)
	}
	out := make([]CommanderFile, 0, len(paths))
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		f, err := deck.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("decks: %s: %w", p, err)
		}
		sum := sha256.Sum256(raw)
		out = append(out, CommanderFile{
			Stem:   strings.TrimSuffix(filepath.Base(p), ".json"),
			SHA256: hex.EncodeToString(sum[:]),
			File:   f,
		})
	}
	return out, nil
}

// CommanderDecks keeps the Commander decks gorge can play and names why it
// skips each other file (Skip.Card is the file stem). The checks run in this
// order and the first that applies is the reason: not a Commander deck, more
// than one commander, ValidateCommander, Resolve, then any card
// reg.Unsupported flags (the call candidatesOf makes).
func CommanderDecks(reg *cards.Registry, files []CommanderFile) ([]Deck, []Skip) {
	sup := effects.Supported()
	var out []Deck
	var skips []Skip
	for _, cf := range files {
		f := cf.File
		skip := func(reason string) { skips = append(skips, Skip{cf.Stem, reason}) }
		names := f.CommanderNames()
		if len(names) == 0 || f.Format != "commander" {
			skip(SkipNotCommander)
			continue
		}
		if len(names) > 1 {
			skip(SkipPartner)
			continue
		}
		if err := f.ValidateCommander(reg); err != nil {
			skip("invalid: " + err.Error())
			continue
		}
		cs, err := f.Resolve(reg)
		if err != nil {
			skip("not in corpus: " + err.Error())
			continue
		}
		bad := map[string]bool{}
		for _, c := range cs {
			if len(reg.Unsupported(c, sup)) > 0 {
				bad[c.Faces[0].Name] = true
			}
		}
		if len(bad) > 0 {
			ns := make([]string, 0, len(bad))
			for n := range bad {
				ns = append(ns, n)
			}
			sort.Strings(ns)
			skip(fmt.Sprintf("unsupported %v", ns))
			continue
		}
		cmdr, ok := reg.Lookup(names[0])
		if !ok || len(cmdr.Faces) == 0 {
			skip("not in corpus: commander " + names[0])
			continue
		}
		list := make([]string, len(cs))
		for i, c := range cs {
			list[i] = c.Faces[0].Name
		}
		sort.Strings(list)
		out = append(out, Deck{
			Name: cf.Stem, Colours: identityLetters(cmdr.ColourIdentity()),
			Cards: list, Commander: cmdr.Faces[0].Name,
		})
	}
	return out, skips
}

// identityLetters is a colour identity mask as letters in WUBRG order, "C" when
// colourless.
func identityLetters(m uint8) string {
	var b strings.Builder
	for i, l := range colourLetters {
		if m&(1<<uint(i)) != 0 {
			b.WriteString(l)
		}
	}
	if b.Len() == 0 {
		return "C"
	}
	return b.String()
}

// CommanderPair is the ordered pair of decks game `game` of chunk `chunk`
// plays: the ordered pairs (i, j), i != j, i-major, indexed by
// (seed*1_000_003 + chunk*10_007 + game) mod len(pairs), so consecutive games
// walk consecutive matchups. Each deck is a copy renamed
// "<set>-c<chunk>-g<game>-<seat>-<stem>". It panics on fewer than two decks.
func CommanderPair(ds []Deck, set string, chunk, game int, seed uint64) [2]Deck {
	n := len(ds)
	if n < 2 {
		panic(fmt.Sprintf("decks: CommanderPair needs at least 2 decks, have %d", n))
	}
	k := int((seed*1_000_003 + uint64(chunk)*10_007 + uint64(game)) % uint64(n*(n-1)))
	i := k / (n - 1)
	j := k % (n - 1)
	if j >= i {
		j++
	}
	var out [2]Deck
	for seat, x := range [2]int{i, j} {
		d := ds[x]
		d.Cards = append([]string(nil), d.Cards...)
		d.Name = fmt.Sprintf("%s-c%d-g%d-%d-%s", set, chunk, game, seat, ds[x].Name)
		out[seat] = d
	}
	return out
}

// CommanderPod is the n decks (2 <= n <= len(ds)) game `game` of chunk `chunk`
// plays, n distinct decks in seat order. For n == 2 it is CommanderPair. For
// more, the index k = seed*1_000_003 + chunk*10_007 + game is read as a
// mixed-radix number: seat 0 takes deck k mod len(ds) of the decks left, seat 1
// takes (k / len(ds)) mod (len(ds)-1) of those still left, and so on, so
// consecutive games walk consecutive first decks. Each deck is a copy renamed
// like CommanderPair's. It panics when n is below 2 or above len(ds).
func CommanderPod(ds []Deck, n int, set string, chunk, game int, seed uint64) []Deck {
	if n == 2 {
		p := CommanderPair(ds, set, chunk, game, seed)
		return p[:]
	}
	if n < 2 || n > len(ds) {
		panic(fmt.Sprintf("decks: CommanderPod needs 2 <= n <= %d decks, n is %d", len(ds), n))
	}
	k := seed*1_000_003 + uint64(chunk)*10_007 + uint64(game)
	left := make([]int, len(ds))
	for i := range left {
		left[i] = i
	}
	out := make([]Deck, n)
	for seat := range out {
		m := uint64(len(left))
		x := left[k%m]
		k /= m
		i := k0(left, x)
		left = append(left[:i], left[i+1:]...)
		d := ds[x]
		d.Cards = append([]string(nil), d.Cards...)
		d.Name = fmt.Sprintf("%s-c%d-g%d-%d-%s", set, chunk, game, seat, ds[x].Name)
		out[seat] = d
	}
	return out
}

// k0 is the index of v in s (v is in s).
func k0(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
