package mtgish

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Pair is one card's two summaries, face-matched by name.
type Pair struct {
	Card   string
	Gorge  []*Summary
	Mtgish []*Summary
}

// Compare summarises one card on both sides and returns its pair and its
// differing rows. Faces are matched by name; a face only one side has is a
// "face" row.
func Compare(name string, gc *cards.Card, mc *Card, idx TypeIndex) (Pair, []Row) {
	return compareCard(name, gc, mc, idx, nil)
}

// compareCard is Compare with printedApart, which reports whether mtgish
// carries a face name as a card of its own (nil: assume it does).
func compareCard(name string, gc *cards.Card, mc *Card, idx TypeIndex, printedApart func(string) bool) (Pair, []Row) {
	p := Pair{Card: name, Gorge: FromGorge(name, gc, idx), Mtgish: FromMtgish(mc)}
	byName := map[string]*Summary{}
	for _, s := range p.Mtgish {
		byName[foldName(s.Face)] = s
	}
	var rows []Row
	seen := map[*Summary]bool{}
	gorge := p.Gorge[:0:0]
	for i, g := range p.Gorge {
		m := byName[foldName(g.Face)]
		if m == nil && i < len(gc.Faces) && gc.Faces[i] != nil {
			// A Universes Within card: Forge's face name is the crossover
			// printing's, the Oracle (mtgish) name its flavour alias.
			for _, a := range gc.Faces[i].Aliases {
				if m = byName[foldName(a)]; m != nil {
					break
				}
			}
		}
		if m == nil && separatelyPrinted(gc, mc) && (printedApart == nil || printedApart(g.Face)) {
			// A meld result or a specialized face: one Forge script, but a
			// card of its own in mtgish, compared under its own name.
			continue
		}
		gorge = append(gorge, g)
		if m == nil {
			rows = append(rows, MissingFace(name, g.Face, true))
			continue
		}
		seen[m] = true
		rows = append(rows, Diff(g, m)...)
	}
	p.Gorge = gorge
	for _, m := range p.Mtgish {
		if !seen[m] {
			rows = append(rows, MissingFace(name, m.Face, false))
		}
	}
	return p, rows
}

// separatelyPrinted reports whether gc's faces beyond the one mtgish has
// are cards in their own right: the meld pieces and result (one Forge
// script, three Oracle cards) and the Alchemy specializations (one script,
// six Oracle cards).
func separatelyPrinted(gc *cards.Card, mc *Card) bool {
	switch gc.AlternateMode {
	case "Meld":
		return mc.Layout == "MeldPiece" || mc.Layout == "Melded"
	case "Specialize":
		return mc.Layout == "Card"
	}
	return false
}

// nameFold maps the look-alike punctuation Oracle names use to what Forge
// writes (Ratonhnhaké꞉ton's U+A789 modifier-letter colon).
var nameFold = strings.NewReplacer("꞉", ":")

func foldName(s string) string { return nameFold.Replace(s) }
