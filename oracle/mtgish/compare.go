package mtgish

import (
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
	p := Pair{Card: name, Gorge: FromGorge(name, gc, idx), Mtgish: FromMtgish(mc)}
	byName := map[string]*Summary{}
	for _, s := range p.Mtgish {
		byName[s.Face] = s
	}
	var rows []Row
	seen := map[string]bool{}
	for _, g := range p.Gorge {
		m := byName[g.Face]
		if m == nil {
			rows = append(rows, MissingFace(name, g.Face, true))
			continue
		}
		seen[g.Face] = true
		rows = append(rows, Diff(g, m)...)
	}
	for _, m := range p.Mtgish {
		if !seen[m.Face] {
			rows = append(rows, MissingFace(name, m.Face, false))
		}
	}
	return p, rows
}
