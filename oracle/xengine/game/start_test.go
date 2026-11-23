package game

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// thornvaultPair is the xproof fixture: p0 has Thornvault Forager and four
// Forests on the battlefield (deck indices 0..4) and Bakersbane Duo on top of
// the library (5), p1 plays Plains.
func thornvaultPair() ([2]decks.Deck, *rules.StartSpec) {
	d0 := decks.Deck{Name: "p0", Colours: "G", Cards: []string{"Thornvault Forager", "Forest", "Forest", "Forest", "Forest", "Bakersbane Duo"}}
	for len(d0.Cards) < 40 {
		d0.Cards = append(d0.Cards, "Forest")
	}
	d1 := decks.Deck{Name: "p1", Colours: "W"}
	for len(d1.Cards) < 40 {
		d1.Cards = append(d1.Cards, "Plains")
	}
	return [2]decks.Deck{d0, d1}, &rules.StartSpec{Seats: []rules.StartSeat{{Battlefield: []int{0, 1, 2, 3, 4}, Library: []int{5}}, {}}}
}

func liveZone(e *rules.Engine, z state.Zone, p state.PlayerID) []string {
	var out []string
	for _, id := range e.G.Zone(z, p) {
		out = append(out, e.G.Obj(id).Face().Name)
	}
	return out
}

func TestPlayStartPlaces(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair, start := thornvaultPair()
	var bf, lib []string
	seen := false
	opt := Options{Set: "BLB", MaxTurns: 2, Start: start, OnCut: func(e *rules.Engine, entry int) {
		if e.G.Turn != 1 || seen {
			return
		}
		seen = true
		bf = liveZone(e, state.ZBattlefield, 0)
		lib = liveZone(e, state.ZLibrary, 0)
	}}
	play(t, reg, pair, rules.SeedForStarter(1, 2, 0), opt)
	if !seen {
		t.Fatal("no cut at turn 1")
	}
	forests, forager := 0, 0
	for _, n := range bf {
		switch n {
		case "Forest":
			forests++
		case "Thornvault Forager":
			forager++
		}
	}
	if forager != 1 || forests != 4 || len(bf) != 5 {
		t.Errorf("p0 battlefield %v, want Thornvault Forager and 4 Forest", bf)
	}
	if len(lib) == 0 || lib[0] != "Bakersbane Duo" {
		t.Errorf("p0 library top %v, want Bakersbane Duo", lib[:min(3, len(lib))])
	}
}

func TestFocusActsOnce(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair, start := thornvaultPair()
	opt := Options{Set: "BLB", MaxTurns: 3, Start: start, Focus: &Focus{Seat: 0, Card: "Thornvault Forager", Kind: "ability", Ability: 2}}
	tp := play(t, reg, pair, rules.SeedForStarter(1, 2, 0), opt)
	n := 0
	for _, e := range tp.Entries {
		for _, p := range e.Picks {
			if e.Seat == 0 && p.Kind == "ability" && p.Ability == 2 && p.Card == "" && p.Ref == "p0:Thornvault Forager" {
				n++
			}
		}
	}
	if n != 1 {
		t.Errorf("focus seat activated ability 2 of Thornvault Forager %d times, want exactly 1", n)
	}
}
