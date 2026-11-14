package lower

import (
	"errors"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// fourSeatPod is four Commander decks (the fixtures hold three, so the fourth
// is the first under another name).
func fourSeatPod(t *testing.T) (*cards.Registry, []decks.Deck) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	var files []decks.CommanderFile
	for _, s := range []string{"foundations-tramplesaurus-rex", "foundations-wretched-ranks", "foundations-reign-of-dragons"} {
		f, err := testutil.LoadRepoDeckFile(s)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, decks.CommanderFile{Stem: s, File: f})
	}
	ds, skips := decks.CommanderDecks(reg, files)
	if len(ds) != 3 {
		t.Fatalf("kept %d decks, skips %v", len(ds), skips)
	}
	d := ds[0]
	d.Cards = slices.Clone(d.Cards)
	d.Name += "-4"
	return reg, append(ds, d)
}

// TestLowerFourSeatRefused: every tape and window of a 4-seat game is refused
// with reason seats:4, and a 1v1 game is not.
func TestLowerFourSeatRefused(t *testing.T) {
	reg, pod := fourSeatPod(t)
	want := []string{"seats:4"}

	tp, err := game.Play(reg, pod, 1, game.Options{MaxTurns: 4})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Tape(reg, tp)
	var re *RefusedError
	if !errors.As(err, &re) || !slices.Equal(re.Reasons, want) {
		t.Fatalf("Tape: err = %v, want a RefusedError %v", err, want)
	}

	ls, rs, wt, err := Windows(reg, pod, 1, game.Options{MaxTurns: 4}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(wt.Decks) != 4 || len(ls) != 0 || len(rs) == 0 {
		t.Fatalf("Windows: %d decks, %d lowered, %d refused", len(wt.Decks), len(ls), len(rs))
	}
	for _, r := range rs {
		if !slices.Equal(r.Reasons, want) || r.Turn < 1 {
			t.Errorf("refusal %+v, want reasons %v", r, want)
		}
	}

	// Three seats name their count.
	tp3, err := game.Play(reg, pod[:3], 1, game.Options{MaxTurns: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Tape(reg, tp3); !errors.As(err, &re) || !slices.Equal(re.Reasons, []string{"seats:3"}) {
		t.Fatalf("3-seat Tape: err = %v", err)
	}
}
