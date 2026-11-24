package lower

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// revokerPair is a small constructed pair whose only nonland card is
// Phyrexian Revoker: its as-enters name ask has exactly one legal nonland
// name, the Revoker itself. It is a full 40-card deck so a lowered window's
// setup checkpoint counts the same library the runner pads to.
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

// TestSelfCheckNameChoice lowers the turns of a game where Phyrexian Revoker
// is cast and holds the window carrying its name ask to gorge's oracle
// runner. The runner offers only a scenario's own cards, so the live game's
// deck-scoped name ("Phyrexian Revoker") is one the replay can answer; with
// the whole corpus as the live universe the window names a card the scenario
// does not seat and the self-check fails at the harness.
func TestSelfCheckNameChoice(t *testing.T) {
	reg, pair := revokerPair(t)
	ls, rs, _, err := Windows(reg, pair[:], 1, game.Options{Set: "REV", MaxTurns: 12}, true)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, l := range ls {
		cast, named := false, false
		for _, st := range l.Item.Steps {
			if st.Op == "cast" && strings.Contains(st.Card, "Phyrexian Revoker") {
				cast = true
			}
			for _, a := range st.Answers {
				// The as-enters ask is answered while the Revoker's ETB
				// resolves, so it rides the pass after the cast, not the cast.
				// Its pick is a card name, whichever universe the live game
				// offered.
				if a.Kind != "choose" {
					continue
				}
				for _, p := range a.Pick {
					if _, ok := reg.Lookup(p); ok {
						named = true
					}
				}
			}
		}
		if !cast || !named {
			continue
		}
		checked++
		if c := CheckWindow(reg, l); c.Status != StatusOK {
			t.Errorf("%s: %s at step %d, %s: gorge %s, tape %s %v (refusals %v)",
				l.Item.ID, c.Status, c.Step, c.Field, c.Gorge, c.Tape, c.Fails, rs)
		}
	}
	if checked == 0 {
		t.Fatalf("no lowered window carried Phyrexian Revoker's name ask: %d lowered, %d refused", len(ls), len(rs))
	}
}
