package game

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestCommanderDecksFilter(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var files []decks.CommanderFile
	for _, s := range []string{"foundations-tramplesaurus-rex", "foundations-wretched-ranks",
		"foundations-reign-of-dragons", "mono-red-goblins"} {
		f, err := testutil.LoadRepoDeckFile(s)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, decks.CommanderFile{Stem: s, File: f})
	}
	ds, skips := decks.CommanderDecks(reg, files)
	if len(ds) != 2 {
		t.Fatalf("kept %d decks, want 2 (skips %v)", len(ds), skips)
	}
	want := []struct{ name, colours, cmdr string }{
		{"foundations-tramplesaurus-rex", "G", "Ghalta, Primal Hunger"},
		{"foundations-wretched-ranks", "B", "Ghoulcaller Gisa"},
	}
	for i, w := range want {
		d := ds[i]
		if d.Name != w.name || d.Colours != w.colours || d.Commander != w.cmdr {
			t.Errorf("deck %d = %s/%s/%s, want %v", i, d.Name, d.Colours, d.Commander, w)
		}
		if len(d.Cards) != 100 || d.CommanderIndex() < 0 || d.Cards[d.CommanderIndex()] != w.cmdr {
			t.Errorf("deck %s: %d cards, commander index %d", d.Name, len(d.Cards), d.CommanderIndex())
		}
	}
	wantSkips := []decks.Skip{
		{Card: "foundations-reign-of-dragons", Reason: "unsupported [Incinerate]"},
		{Card: "mono-red-goblins", Reason: decks.SkipNotCommander},
	}
	if !reflect.DeepEqual(skips, wantSkips) {
		t.Errorf("skips = %v, want %v", skips, wantSkips)
	}
}

func commanderPair(t *testing.T) (*cards.Registry, [2]decks.Deck) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	var files []decks.CommanderFile
	for _, s := range []string{"foundations-tramplesaurus-rex", "foundations-wretched-ranks"} {
		f, err := testutil.LoadRepoDeckFile(s)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, decks.CommanderFile{Stem: s, File: f})
	}
	ds, skips := decks.CommanderDecks(reg, files)
	if len(ds) != 2 {
		t.Fatalf("kept %d decks, skips %v", len(ds), skips)
	}
	return reg, decks.CommanderPair(ds, "CMDR", 0, 0, 0)
}

func TestPlayCommanderGame(t *testing.T) {
	reg, p := commanderPair(t)
	tp := play(t, reg, p, 5, Options{MaxTurns: 3})
	if tp.GameFormat != "commander" {
		t.Fatalf("GameFormat = %q", tp.GameFormat)
	}
	for i := range p {
		c := p[i].Commander
		if tp.Decks[i].Commander != c || c == "" {
			t.Errorf("seat %d deck commander %q", i, tp.Decks[i].Commander)
		}
		if !reflect.DeepEqual(tp.Open[i].Command, []string{c}) {
			t.Errorf("seat %d opening command %v, want [%s]", i, tp.Open[i].Command, c)
		}
		for _, n := range tp.Open[i].Library {
			if n == c {
				t.Errorf("seat %d: commander %s in the library", i, c)
			}
		}
		for _, n := range tp.Open[i].Hand {
			if n == c {
				t.Errorf("seat %d: commander %s in the hand", i, c)
			}
		}
		pd := tp.Entries[0].Digest.Players[i]
		if pd.Life != 40 || !reflect.DeepEqual(pd.Command, []string{c}) {
			t.Errorf("seat %d digest life %d command %v", i, pd.Life, pd.Command)
		}
	}
	// Off: the same decks without a commander play constructed.
	q := p
	for i := range q {
		q[i].Commander = ""
	}
	off := play(t, reg, q, 5, Options{MaxTurns: 3})
	if off.GameFormat != "" || off.Entries[0].Digest.Players[0].Life != 20 {
		t.Errorf("off: format %q life %d", off.GameFormat, off.Entries[0].Digest.Players[0].Life)
	}
	if len(off.Open[0].Command) != 0 {
		t.Errorf("off: command %v", off.Open[0].Command)
	}
	found := false
	for _, n := range off.Open[0].Library {
		found = found || n == p[0].Commander
	}
	for _, n := range off.Open[0].Hand {
		found = found || n == p[0].Commander
	}
	if !found {
		t.Errorf("off: commander %s in neither hand nor library", p[0].Commander)
	}
}

func TestPlayCommanderNeedsBoth(t *testing.T) {
	reg, p := commanderPair(t)
	_, fdn := fdnPairs(t, 1)
	_, err := Play(reg, [2]decks.Deck{p[0], fdn[0][1]}, 1, Options{MaxTurns: 1})
	if err == nil || !strings.Contains(err.Error(), "needs a commander in both decks") || !strings.Contains(err.Error(), p[0].Commander) {
		t.Fatalf("err = %v", err)
	}
}

func TestConstructedTapeUnchanged(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	b, err := play(t, reg, pairs[0], 100, Options{Set: "FDN"}).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"game_format", "commander", "command"} {
		if bytes.Contains(b, []byte(`"`+k+`"`)) {
			t.Errorf("constructed tape has key %q", k)
		}
	}
}
