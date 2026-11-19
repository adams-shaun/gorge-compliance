package dside

import (
	"errors"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/dirac/cardset"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestAnswer(t *testing.T) {
	diverged := Row{Status: StatusDiverged, FromTurn: 4, ToTurn: 5}
	stalled := Row{Status: StatusCapacity, FromTurn: 4, ToTurn: 4}
	for _, c := range []struct {
		row  Row
		id   string
		want string
	}{
		{Row{Status: StatusClean}, "xe:T:0:0:w9", Agree},
		{Row{Status: StatusClean}, "xe:T:0:0:tape", Agree},
		{Row{Status: StatusNA}, "xe:T:0:0:w1", NotAppl},
		{Row{Status: StatusHang}, "xe:T:0:0:w1", NotAppl},
		{diverged, "xe:T:0:0:w3", Agree},
		{diverged, "xe:T:0:0:w4", Diverge},
		{diverged, "xe:T:0:0:w5", Diverge},
		{diverged, "xe:T:0:0:w6", NotAppl},
		{diverged, "xe:T:0:0:tape", Diverge},
		{diverged, "xe:T:0:0:tape:t2", Diverge},
		{stalled, "xe:T:0:0:w3", Agree},
		{stalled, "xe:T:0:0:w4", NotAppl},
		{stalled, "xe:T:0:0:tape", NotAppl},
		{Row{Status: StatusClean}, "not an item", NotAppl},
	} {
		if got := c.row.Answer(c.id); got != c.want {
			t.Errorf("%+v %s: got %s, want %s", c.row, c.id, got, c.want)
		}
	}
}

func TestLocate(t *testing.T) {
	tp := &game.Tape{Entries: []game.Entry{{Turn: 1}, {Turn: 1}, {Turn: 2}}}
	r := Row{Status: StatusDiverged, Intents: 2}
	r.Locate(tp)
	if r.FromTurn != 1 || r.ToTurn != 2 {
		t.Fatalf("a difference at a turn's edge: %+v", r)
	}
	r = Row{Status: StatusCapacity, Intents: 2}
	r.Locate(tp)
	if r.FromTurn != 2 || r.ToTurn != 2 {
		t.Fatalf("a stall: %+v", r)
	}
}

// TestScriptReplaysTheLockstepGame plays the game the sweep plays from a lockstep's
// answers: the script holds, so the tape's entries are the lockstep's decisions.
func TestScriptReplaysTheLockstepGame(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tables := NewTables(reg)
	cands, _, err := decks.Candidates(reg, "../../compliance/manifests", "FDN")
	if err != nil {
		t.Fatal(err)
	}
	playable, err := tables.Playable("FDN", cands)
	if NoTables(err) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(playable) == 0 || len(playable) > len(cands) {
		t.Fatalf("%d playable of %d", len(playable), len(cands))
	}
	played := 0
	for g := 0; g < 6; g++ {
		pair := decks.Pair(playable, nil, "FDN", 0, g, 1)
		seed := uint64(1000 + g)
		row, script := tables.Lockstep(GameID("FDN", 0, g), "FDN", pair, seed, game.DefaultMaxTurns)
		if row.Status == StatusNA {
			continue
		}
		played++
		tp, err := game.Play(reg, pair[:], seed, game.Options{Set: "FDN", Script: script})
		if err != nil {
			t.Fatalf("game %d: %v", g, err)
		}
		if len(tp.Entries) < row.Intents {
			t.Fatalf("game %d: %d entries for %d answers", g, len(tp.Entries), row.Intents)
		}
		for i, in := range script {
			if tp.Entries[i].Seq != in.Seq {
				t.Fatalf("game %d entry %d: seq %d, the script's %d", g, i, tp.Entries[i].Seq, in.Seq)
			}
		}
	}
	if played == 0 {
		t.Fatal("no pair played")
	}
}

// TestPlayableLeavesRefusedCardsOut: a card dirac refuses by name is known to play
// wrongly, so no sweep deck holds it.
func TestPlayableLeavesRefusedCardsOut(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tables := NewTables(reg)
	set, err := tables.Table("FDN")
	if NoTables(err) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	refused := 0
	supported := set.Supported()
	for i := range supported {
		if set.Status(i) == cardset.StatusRefused {
			refused++
			if supported[i] {
				t.Errorf("pool index %d is refused (%s) and playable", i, set.Reason(i))
			}
		}
	}
	if refused == 0 {
		t.Skip("the FDN pool has no refused card at this pin")
	}
}

func commanderPair(t *testing.T, reg *cards.Registry, stems ...string) [2]decks.Deck {
	t.Helper()
	var files []decks.CommanderFile
	for _, s := range stems {
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
	return [2]decks.Deck{ds[0], ds[1]}
}

// playablePair is the Foundations pair the tests play, on the table's terms.
func playablePair(t *testing.T, reg *cards.Registry, tables *Tables) [2]decks.Deck {
	t.Helper()
	pair := commanderPair(t, reg, "foundations-tramplesaurus-rex", "foundations-wretched-ranks")
	ds, swaps, err := tables.SwapRefusedCommander(pair[:])
	if NoTables(err) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(swaps) == 0 {
		t.Log("nothing refused")
	}
	return [2]decks.Deck{ds[0], ds[1]}
}

func TestCommanderLockstepPlays(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tables := NewTables(reg)
	pair := playablePair(t, reg, tables)
	row, script := tables.CommanderLockstep("xe:CMDR:0:0", pair, 1000, 4)
	if row.Table != "cmd5" {
		t.Errorf("table %q", row.Table)
	}
	if row.Status == StatusNA {
		t.Fatalf("not applicable: %s", row.Reason)
	}
	if row.Intents <= 0 || len(script) != row.Intents {
		t.Errorf("intents %d, script %d", row.Intents, len(script))
	}
}

// TestCommanderScriptReplaysTheLockstepGame is the proof that game.Play's Commander
// Config and the dirac matchup's agree: the lockstep's answers replay into the game.
func TestCommanderScriptReplaysTheLockstepGame(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tables := NewTables(reg)
	pair := playablePair(t, reg, tables)
	for seed := uint64(1000); seed <= 1002; seed++ {
		row, script := tables.CommanderLockstep("xe:CMDR:0:0", pair, seed, 4)
		if row.Status == StatusNA {
			t.Fatalf("seed %d: not applicable: %s", seed, row.Reason)
		}
		tp, err := game.Play(reg, pair[:], seed, game.Options{Set: "CMDR", Script: script})
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if len(tp.Entries) < len(script) {
			t.Fatalf("seed %d: %d entries for %d answers", seed, len(tp.Entries), len(script))
		}
		for i, in := range script {
			if tp.Entries[i].Seq != in.Seq {
				t.Fatalf("seed %d entry %d: seq %d, the script's %d", seed, i, tp.Entries[i].Seq, in.Seq)
			}
		}
	}
}

func TestCommanderLockstepRefusesForeignDeck(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tables := NewTables(reg)
	if _, err := tables.CommanderSet(); NoTables(err) {
		t.Skip(err)
	}
	pair := commanderPair(t, reg, "foundations-wretched-ranks", "foundations-tramplesaurus-rex")
	f, err := testutil.LoadRepoDeckFile("science-pip")
	if err != nil {
		t.Fatal(err)
	}
	foreign := decks.Deck{Name: "science-pip"}
	for _, e := range f.Cards {
		for i := 0; i < e.Count; i++ {
			foreign.Cards = append(foreign.Cards, e.Name)
		}
	}
	row, script := tables.CommanderLockstep("xe:CMDR:0:1", [2]decks.Deck{foreign, pair[0]}, 1, 4)
	if row.Status != StatusNA || row.Reason == "" || script != nil {
		t.Errorf("foreign deck: %+v", row)
	}
}

// otjRefusedPair is the first OTJ pair of the sweep's own deal that the table turns
// away as an interaction with a card of name in it, and that error.
func otjRefusedPair(t *testing.T, tables *Tables, name string) ([2]decks.Deck, *cardset.InteractionRefusal) {
	t.Helper()
	cands, _, err := decks.Candidates(tables.reg, "../../compliance/manifests", "OTJ")
	if err != nil {
		t.Skip(err)
	}
	cands, err = tables.Playable("OTJ", cands)
	if NoTables(err) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	set, err := tables.Table("OTJ")
	if err != nil {
		t.Fatal(err)
	}
	for g := 0; g < 300; g++ {
		p := decks.Pair(cands, cov.New(), "OTJ", 0, g, 1)
		_, _, _, err := set.Matchup(entries(p[0]), entries(p[1]))
		var ref *cardset.InteractionRefusal
		if errors.As(err, &ref) && slices.Contains(ref.Cards, name) {
			return p, ref
		}
	}
	t.Skipf("no OTJ pair of 300 is refused with %s at this pin", name)
	return [2]decks.Deck{}, nil
}

// TestSwapRefusedInteraction: a pair the OTJ table refuses for Archangel of Tithes
// beside another card has the named card swapped for a basic of its deck, and
// Lockstep then plays it instead of reading it not_applicable.
func TestSwapRefusedInteraction(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tables := NewTables(reg)
	pair, ref := otjRefusedPair(t, tables, "Archangel of Tithes")
	if row, _ := tables.Lockstep("g", "OTJ", pair, 1, game.DefaultMaxTurns); row.Status != StatusNA {
		t.Fatalf("the dealt pair reads %s, want not_applicable (%s)", row.Status, ref)
	}
	got, swaps, err := tables.SwapRefusedInteraction("OTJ", pair)
	if err != nil {
		t.Fatal(err)
	}
	if len(swaps) == 0 || len(swaps) > MaxInteractionSwaps {
		t.Fatalf("swaps %v, want 1..%d", swaps, MaxInteractionSwaps)
	}
	for _, s := range swaps {
		if s.Reason != "interaction "+ref.Reason || s.Count < 1 || !slices.Contains(ref.Cards, s.Card) {
			t.Errorf("swap %+v does not answer %s", s, ref)
		}
		if !slices.Contains(got[s.Seat].Cards, s.Basic) || slices.Contains(got[s.Seat].Cards, s.Card) {
			t.Errorf("swap %+v: seat %d deck %v", s, s.Seat, got[s.Seat].Cards)
		}
	}
	for i := range got {
		if len(got[i].Cards) != len(pair[i].Cards) || !slices.IsSorted(got[i].Cards) {
			t.Errorf("seat %d: %d cards sorted=%v, was %d", i, len(got[i].Cards), slices.IsSorted(got[i].Cards), len(pair[i].Cards))
		}
	}
	if row, _ := tables.Lockstep("g", "OTJ", got, 1, game.DefaultMaxTurns); row.Status == StatusNA {
		t.Errorf("the swapped pair is still not_applicable: %s", row.Reason)
	}
	again, swaps2, _ := tables.SwapRefusedInteraction("OTJ", pair)
	if !slices.Equal(swaps, swaps2) || !slices.Equal(again[0].Cards, got[0].Cards) || !slices.Equal(again[1].Cards, got[1].Cards) {
		t.Error("the swap is not a pure function of the pair")
	}
	// A held pair has nothing to swap.
	if h, none, _ := tables.SwapRefusedInteraction("OTJ", got); len(none) != 0 || !slices.Equal(h[0].Cards, got[0].Cards) {
		t.Errorf("a held pair swapped %v", none)
	}
}

func TestFewestNamed(t *testing.T) {
	d := func(cs ...string) decks.Deck { return decks.Deck{Cards: cs} }
	for _, c := range []struct {
		pair  [2]decks.Deck
		names []string
		card  string
		seat  int
		ok    bool
	}{
		{[2]decks.Deck{d("A", "A", "B"), d("C")}, []string{"A", "B", "C"}, "B", 0, true},
		{[2]decks.Deck{d("A", "B"), d("A", "A")}, []string{"A", "B"}, "B", 0, true},
		{[2]decks.Deck{d("A", "A"), d("A")}, []string{"A"}, "A", 1, true},
		{[2]decks.Deck{d("X"), d("A", "B")}, []string{"B", "A"}, "B", 1, true}, // a tie: names' order
		{[2]decks.Deck{d("X"), d("Y")}, []string{"A"}, "", 0, false},
	} {
		card, seat, ok := fewestNamed(c.pair, c.names)
		if card != c.card || seat != c.seat || ok != c.ok {
			t.Errorf("%v %v: %q seat %d %v, want %q seat %d %v", c.pair, c.names, card, seat, ok, c.card, c.seat, c.ok)
		}
	}
}
