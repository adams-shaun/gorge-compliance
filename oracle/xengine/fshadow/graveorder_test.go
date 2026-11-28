package fshadow

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/events"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// deckFixture is a two-seat game on deck (both seats) at its first
// decision; seat 1's library is deck after its 7-card opening hand.
func deckFixture(t *testing.T, deck []string) *follower {
	t.Helper()
	var l Log
	l.Header.Format = Format
	for seat := range 2 {
		s := Seat{Seat: seat}
		sh := &Shuffle{T: TShuffle, Seat: seat}
		for i, n := range deck {
			s.Cards = append(s.Cards, Card{ID: 100*(seat+1) + i, Name: n})
			sh.Order = append(sh.Order, 100*(seat+1)+i)
		}
		l.Header.Seats = append(l.Header.Seats, s)
		l.Entries = append(l.Entries, Entry{Line: 2 + seat, T: TShuffle, Shuffle: sh})
	}
	l.Entries = append(l.Entries, Entry{Line: 4, T: TStart, Start: &Start{T: TStart, Seat: 1}})
	f := &follower{log: &l, reg: corpus(t), toG: map[int]state.ObjID{}, toF: map[state.ObjID]int{}, name: map[int]string{}, shuffles: map[int][]*Shuffle{}, planned: map[int]int{}, start: -1}
	if err := f.setup(); err != nil {
		t.Fatal(err)
	}
	if err := f.e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	return f
}

var pileDeck = []string{"Forest", "Forest", "Forest", "Plains", "Plains", "Mountain", "Mountain",
	"Fact or Fiction", "Island", "Grizzly Bears", "Swamp", "Llanowar Elves", "Plains",
	"Forest", "Swamp", "Mountain", "Plains", "Forest", "Swamp", "Mountain", "Plains", "Forest"}

// binned moves ids from seat 1's library to its graveyard's end and logs a
// move_zone event for each, with sep between them when split.
func binned(f *follower, ids []state.ObjID, split bool) {
	lib := slices.DeleteFunc(slices.Clone(f.e.G.Zone(state.ZLibrary, 1)), func(id state.ObjID) bool { return slices.Contains(ids, id) })
	f.e.G.SetZone(state.ZLibrary, 1, lib)
	f.e.G.SetZone(state.ZGraveyard, 1, append(slices.Clone(f.e.G.Zone(state.ZGraveyard, 1)), ids...))
	for i, id := range ids {
		if split && i > 0 {
			f.e.L.Append(events.Event{Kind: events.Note, Text: "between"})
		}
		f.e.G.Obj(id).Zone = state.ZGraveyard
		f.e.L.Append(events.Event{Kind: events.MoveZone, Player: 1, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
	}
}

// TestGraveyardOrderOfOneZoneChange is the farm cmdr4 repro (TLA-s137
// pod-13703 line 607, p0.graveyard): Fact or Fiction's unchosen pile went to
// the graveyard in gorge's library order and in Forge's pile order. CR 404.3
// lets the owner order cards put there at once, so the follower takes Forge's
// order -- only within one zone change's cards.
func TestGraveyardOrderOfOneZoneChange(t *testing.T) {
	for _, c := range []struct {
		name  string
		split bool // the cards moved one by one, other events between
		early bool // a card already there before the snapshot interval is out of place too
		want  int
	}{
		{"one zone change: Forge's order adopted", false, false, 1},
		{"separate moves: the difference is reported", true, false, 0},
		{"an older card out of place: the difference is reported", false, true, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := deckFixture(t, pileDeck)
			lib := slices.Clone(f.e.G.Zone(state.ZLibrary, 1))
			binned(f, lib[11:13], true) // two cards before the last snapshot
			f.graveMark = len(f.e.L.Events)
			binned(f, lib[1:4], c.split) // Island, Grizzly Bears, Swamp
			before := slices.Clone(f.e.G.Zone(state.ZGraveyard, 1))
			s := Snapshot(f.e, "")
			old := s.Players[1].Graveyard[:2]
			if c.early {
				old = []string{old[1], old[0]}
			}
			s.Players[1].Graveyard = append(slices.Clone(old), "Swamp", "Island", "Grizzly Bears")
			if got := f.adoptGraveyardOrder(s); got != c.want {
				t.Fatalf("adopted %d graveyards, want %d", got, c.want)
			}
			gy := f.e.G.Zone(state.ZGraveyard, 1)
			if c.want == 0 {
				if !slices.Equal(gy, before) {
					t.Fatalf("graveyard reordered to %v", namesOf(f, gy...))
				}
				return
			}
			if got := namesOf(f, gy...); !slices.Equal(got, s.Players[1].Graveyard) {
				t.Fatalf("graveyard %v, want %v", got, s.Players[1].Graveyard)
			}
			if g := Snapshot(f.e, ""); !slices.Equal(g.Players[1].Graveyard, s.Players[1].Graveyard) {
				t.Fatalf("snapshot graveyard %v", g.Players[1].Graveyard)
			}
		})
	}
}

// TestTwoPilesFromTheNextSnapshot: Fact or Fiction's split and pile pick
// have no Forge line; the cards new in the caster's hand at the next snapshot
// form pile A and the pick takes it. A pick of another source keeps the
// default.
func TestTwoPilesFromTheNextSnapshot(t *testing.T) {
	f := deckFixture(t, pileDeck)
	lib := f.e.G.Zone(state.ZLibrary, 1)
	fof, offered := lib[0], slices.Clone(lib[1:6])
	if got := objName(f.e.G.Obj(fof)); got != "Fact or Fiction" {
		t.Fatalf("library top %q", got)
	}
	split := &decision.Decision{Seq: 3, Player: 0, Kind: decision.KChoose, Max: len(offered), Source: fof,
		ResumeKind: "twopiles_split", ResumeSA: ability(t, "Fact or Fiction", "DBTwoPiles")}
	for i, id := range offered {
		split.Options = append(split.Options, decision.Option{Index: i, Kind: "twopiles", Label: objName(f.e.G.Obj(id)), Obj: id, Player: 0})
	}
	// Forge put Grizzly Bears and Llanowar Elves (options 1 and 3) in hand.
	nextSnap(f, func(p *rules.OracleSnapPlayer) {
		p.Hand = append(p.Hand, "Grizzly Bears", "Llanowar Elves")
		p.LibraryCount -= 5
	})
	in, ok := f.twoPilesChoose(split)
	if !ok || !slices.Equal(in.Choices, []int{1, 3}) {
		t.Fatalf("split %v %v, want pile A [1 3]", in.Choices, ok)
	}
	pick := &decision.Decision{Seq: 4, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1, Source: fof, ResumeKind: "twopiles_pick",
		Options: []decision.Option{{Index: 0, Kind: "pile-a", Player: 1}, {Index: 1, Kind: "pile-b", Player: 1}}}
	other := *pick
	other.Source = offered[0]
	f.pileRead = fof
	if _, ok := f.twoPilesChoose(&other); ok {
		t.Fatal("a pick of another source was read")
	}
	if _, ok := f.twoPilesChoose(pick); ok {
		t.Fatal("a pick with no split read before it was read")
	}
	f.pileRead = fof
	if in, ok := f.twoPilesChoose(pick); !ok || !slices.Equal(in.Choices, []int{0}) {
		t.Fatalf("pick %v %v, want pile A", in.Choices, ok)
	}
	if f.res.PilePicks != 1 {
		t.Fatalf("pile picks %d", f.res.PilePicks)
	}
}
