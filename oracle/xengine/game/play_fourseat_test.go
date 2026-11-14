package game

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestPlayFourSeatCommander plays a 4-seat Commander pod: the tape records
// four decks and four openings, each with its commander in the command zone.
func TestPlayFourSeatCommander(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var files []decks.CommanderFile
	for _, s := range []string{"foundations-tramplesaurus-rex", "foundations-wretched-ranks",
		"foundations-reign-of-dragons"} {
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
	// The fixtures hold three Commander decks: seat 3 replays the first under
	// its own name (the pod needs only distinct seats, not distinct lists).
	four := append([]decks.Deck{}, ds...)
	d := ds[0]
	d.Cards = append([]string(nil), d.Cards...)
	d.Name += "-4"
	four = append(four, d)
	tp, err := Play(reg, four, 1, Options{MaxTurns: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(tp.Decks) != 4 || len(tp.Open) != 4 {
		t.Fatalf("tape has %d decks, %d openings, want 4 each", len(tp.Decks), len(tp.Open))
	}
	if tp.GameFormat != GameFormatCommander {
		t.Errorf("GameFormat = %q", tp.GameFormat)
	}
	seen := map[int]bool{}
	for _, en := range tp.Entries {
		seen[en.Seat] = true
	}
	for i := range four {
		if len(tp.Open[i].Command) != 1 || tp.Open[i].Command[0] != four[i].Commander {
			t.Errorf("seat %d opening command %v, want [%s]", i, tp.Open[i].Command, four[i].Commander)
		}
		if tp.Decks[i].Name != four[i].Name {
			t.Errorf("seat %d deck %q", i, tp.Decks[i].Name)
		}
	}
	if len(seen) < 2 {
		t.Errorf("only seats %v made decisions", seen)
	}
}

// TestTapeTwoSeatRoundTrip: a 1v1 tape still writes two decks and two
// openings, and reading it back and writing it again changes no byte.
func TestTapeTwoSeatRoundTrip(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	tp := play(t, reg, pairs[0], 100, Options{Set: "FDN"})
	if len(tp.Decks) != 2 || len(tp.Open) != 2 {
		t.Fatalf("%d decks, %d openings", len(tp.Decks), len(tp.Open))
	}
	a, err := tp.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ReadTape(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	b, err := back.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("a 2-seat tape changed on a read/write round trip")
	}
	var head struct {
		Decks []json.RawMessage `json:"decks"`
		Open  []json.RawMessage `json:"opening"`
	}
	if err := json.Unmarshal(a, &head); err != nil || len(head.Decks) != 2 || len(head.Open) != 2 {
		t.Fatalf("json header: %v, %d decks, %d openings", err, len(head.Decks), len(head.Open))
	}
}

func TestPlaySeatCountBounds(t *testing.T) {
	reg, p := commanderPair(t)
	for _, n := range []int{0, 1, 5} {
		pod := make([]decks.Deck, n)
		for i := range pod {
			pod[i] = p[i%2]
		}
		if _, err := Play(reg, pod, 1, Options{MaxTurns: 1}); err == nil || !strings.Contains(err.Error(), "want 2 to 4") {
			t.Errorf("%d decks: err = %v", n, err)
		}
	}
	// A pod is a Commander game: three constructed decks are refused.
	r2, pairs := fdnPairs(t, 2)
	_, err := Play(r2, []decks.Deck{pairs[0][0], pairs[0][1], pairs[1][0]}, 1, Options{MaxTurns: 1})
	if err == nil || !strings.Contains(err.Error(), "3-seat game is a Commander game") {
		t.Errorf("3 constructed decks: err = %v", err)
	}
}
