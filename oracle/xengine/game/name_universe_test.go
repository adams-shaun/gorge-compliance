package game

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// revokerPair is a small constructed pair whose only nonland card is
// Phyrexian Revoker, so a name ask has exactly one legal nonland name: the
// Revoker itself. It is a full 40-card deck so a lowered window's setup
// checkpoint counts the same library the runner pads to.
func revokerPair(t *testing.T) (*cards.Registry, [2]decks.Deck) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	list := make([]string, 0, 40)
	for i := 0; i < 10; i++ {
		list = append(list, "Phyrexian Revoker")
	}
	for i := 0; i < 30; i++ {
		list = append(list, "Swamp")
	}
	d := decks.Deck{Name: "revoker", Colours: "B", Cards: list}
	return reg, [2]decks.Deck{d, d}
}

// TestPlayNameUniverseIsDeckScoped: a game with Phyrexian Revoker must offer
// only names its decks contain. The live game's universe is the dealt decks'
// (nil unless a seated card chooses a name), the same universe gorge's oracle
// runner scopes a lowered scenario to, so every name the live game records
// can be replayed. Before the fix the bot named the alphabetically first
// corpus card ("Ach! Hans, Run!"), which no lowered window can offer.
func TestPlayNameUniverseIsDeckScoped(t *testing.T) {
	reg, pair := revokerPair(t)
	tp := play(t, reg, pair, 1, Options{MaxTurns: 12})
	inDeck := map[string]bool{}
	for _, d := range tp.Decks {
		for _, n := range d.Cards {
			inDeck[n] = true
		}
	}
	asks := 0
	for _, en := range tp.Entries {
		for _, p := range en.Picks {
			if p.Kind != "name" {
				continue
			}
			asks++
			if !inDeck[p.Label] {
				t.Errorf("entry %d (seat %d): the game named %q, outside its decks", en.Seq, en.Seat, p.Label)
			}
		}
	}
	if asks == 0 {
		t.Fatal("the game posed no name ask: Phyrexian Revoker was never cast")
	}
}
