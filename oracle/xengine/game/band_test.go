package game

import (
	"bytes"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// bandDeck is 18 creatures with banding and 22 Plains.
func bandDeck(name string) decks.Deck {
	d := decks.Deck{Name: name, Colours: "W"}
	for i := 0; i < 9; i++ {
		d.Cards = append(d.Cards, "Benalish Hero", "Kjeldoran Warrior")
	}
	for i := 0; i < 22; i++ {
		d.Cards = append(d.Cards, "Plains")
	}
	return d
}

// gorge's explore bot answers every band declaration (CR 702.22c) with option
// 0, "no more creatures in this band", so no recorded game ever had a band
// attack. bandBot answers from a hash instead: over a few games of banding
// creatures a band forms, a band is declined too, and the same seed plays the
// same tape.
func TestBandsForm(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair := [2]decks.Deck{bandDeck("band-a"), bandDeck("band-b")}
	accepted, declined := 0, 0
	for seed := uint64(1); seed <= 6; seed++ {
		tp := play(t, reg, pair, seed, Options{Set: "SC15"})
		again, err := play(t, reg, pair, seed, Options{Set: "SC15"}).Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if first, err := tp.Bytes(); err != nil || string(first) != string(again) {
			t.Fatalf("seed %d: two plays differ (err %v)", seed, err)
		}
		for _, e := range tp.Entries {
			if e.Kind != "choose" || len(e.Picks) != 1 || e.Picks[0].Kind != "band" {
				continue
			}
			if e.Choices[0] == 0 {
				declined++
			} else {
				accepted++
			}
		}
	}
	if accepted == 0 || declined == 0 {
		t.Errorf("band asks: %d accepted, %d declined over six games, want some of each", accepted, declined)
	}
}

// A game with no banding creature plays exactly as the bare explore bot plays
// it: bandBot only answers a band ask, and stays a heuristic.BoardSeat so the
// runner still hands the bot its board and not a projected View.
func TestBandBotLeavesOtherGamesAlone(t *testing.T) {
	reg, pairs := fdnPairs(t, 2)
	for i, p := range pairs {
		seed := uint64(100 + i)
		with, err := play(t, reg, p, seed, Options{Set: "FDN"}).Bytes()
		if err != nil {
			t.Fatal(err)
		}
		bare, err := play(t, reg, p, seed, Options{Set: "FDN", plainSeats: true}).Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(with, bare) {
			t.Errorf("pair %d: the tape with bandBot seats differs from the bare bot's (%d vs %d bytes)", i, len(with), len(bare))
		}
	}
}
