package fshadow

import (
	"reflect"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// snapPickFixture is a two-seat game at its first decision. Seat 1's hand
// holds several names, its library opens Island, Island, Grizzly Bears (X, X,
// Y), and one more Island and Grizzly Bears sit in its graveyard. It returns
// the library, and the graveyard as (X, Y).
func snapPickFixture(t *testing.T) (f *follower, lib, yard []state.ObjID) {
	t.Helper()
	hand := []string{"Forest", "Forest", "Forest", "Plains", "Plains", "Mountain", "Mountain"}
	deck := append(slices.Clone(hand), "Island", "Island", "Grizzly Bears", "Island", "Grizzly Bears",
		"Swamp", "Mountain", "Plains", "Forest", "Swamp", "Mountain", "Plains", "Forest")
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
	f = &follower{log: &l, reg: corpus(t), toG: map[int]state.ObjID{}, toF: map[state.ObjID]int{}, name: map[int]string{}, shuffles: map[int][]*Shuffle{}, planned: map[int]int{}, start: -1}
	if err := f.setup(); err != nil {
		t.Fatal(err)
	}
	if err := f.e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	all := slices.Clone(f.e.G.Zone(state.ZLibrary, 1))
	yard = []state.ObjID{all[3], all[4]}
	lib = append(slices.Clone(all[:3]), all[5:]...)
	if got, want := namesOf(f, append(slices.Clone(lib[:3]), yard...)...), []string{"Island", "Island", "Grizzly Bears", "Island", "Grizzly Bears"}; !slices.Equal(got, want) {
		t.Fatalf("library top and graveyard %v, want %v", got, want)
	}
	f.e.G.SetZone(state.ZLibrary, 1, lib)
	f.e.G.SetZone(state.ZGraveyard, 1, yard)
	for _, id := range yard {
		f.e.G.Obj(id).Zone = state.ZGraveyard
	}
	return f, lib, yard
}

// nextSnap appends the snapshot line following the pick: gorge's state now,
// seat 1's entry changed by edit, as Forge would name it (5 library cards).
func nextSnap(f *follower, edit func(p *rules.OracleSnapPlayer)) Entry {
	s := Snapshot(f.e, "")
	lib := f.e.G.Zone(state.ZLibrary, 1)
	p := &s.Players[1]
	p.LibraryTop = nil
	for _, id := range lib[:5] {
		p.LibraryTop = append(p.LibraryTop, objName(f.e.G.Obj(id)))
	}
	edit(p)
	e := Entry{Line: 999, T: TSnap, Snap: &Snap{T: TSnap, Snapshot: s}}
	f.log.Entries = append(f.log.Entries, e)
	f.pos = len(f.log.Entries) - 1
	return e
}

func namesOf(f *follower, ids ...state.ObjID) []string {
	out := []string{}
	for _, id := range ids {
		out = append(out, objName(f.e.G.Obj(id)))
	}
	return out
}

func without(s []string, n string) []string {
	i := slices.Index(s, n)
	return slices.Delete(slices.Clone(s), i, i+1)
}

// ability is a corpus card's ability: the one its SVar svar names, or the
// card's first ability when svar is empty (its nth, with n).
func ability(t *testing.T, card, svar string, n ...int) *cards.SA {
	t.Helper()
	c, ok := corpus(t).Lookup(card)
	if !ok {
		t.Fatalf("no card %q", card)
	}
	i := 0
	if len(n) > 0 {
		i = n[0]
	}
	var sa *cards.SA
	if svar != "" {
		sa = cards.ResolveSVar(c.Faces[0].SVars, svar)
	} else if len(c.Faces[0].Abilities) > i {
		sa = c.Faces[0].Abilities[i]
	}
	if sa == nil {
		t.Fatalf("%q has no ability %q %d", card, svar, i)
	}
	return sa
}

// TestArrangeFromTheNextSnapshot is the farm cmdr4 repro (BLB-s131026052
// pod-13102605201, line 85, p3.hand): Elven Farsight's scry 3 has no Forge
// ask line, and keeping all three on top by default left an Island where Forge
// had bottomed two and drawn Elvish Piper.
func TestArrangeFromTheNextSnapshot(t *testing.T) {
	for _, c := range []struct {
		name    string
		away    string
		offered int
		// after is Forge's library (indices into gorge's, top first, only
		// its head) and what it drew and binned.
		top, drawn, binned []int
		gone               int // cards the library count dropped by
		want               []int
		read               int
	}{
		{"scry bottoms two and draws the third", "bottom", 3, []int{3, 4, 5, 6, 7}, []int{2}, nil, 1, []int{2}, 1},
		{"scry keeps all", "bottom", 3, []int{0, 1, 2, 3, 4}, nil, nil, 0, []int{0, 1, 2}, 1},
		{"scry reorders and bottoms one", "bottom", 3, []int{2, 0, 3, 4, 5}, nil, nil, 0, []int{2, 0}, 1},
		{"surveil bins the card", "graveyard", 1, []int{1, 2, 3, 4, 5}, nil, []int{0}, 1, []int{}, 1},
		{"surveil keeps the card, then draws it", "graveyard", 1, []int{1, 2, 3, 4, 5}, []int{0}, nil, 1, []int{0}, 1},
		{"no fit keeps the default", "bottom", 3, []int{8, 7, 6, 5, 4}, []int{2}, nil, 30, []int{0, 1, 2}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, lib, _ := snapPickFixture(t)
			d := &decision.Decision{Seq: 7, Player: 1, Kind: decision.KArrange, Max: c.offered, ResumeKind: "arrange"}
			for i := range c.offered {
				d.Options = append(d.Options, decision.Option{Index: i, Kind: c.away, Obj: lib[i], Player: 1})
			}
			e := nextSnap(f, func(p *rules.OracleSnapPlayer) {
				p.LibraryCount -= c.gone
				p.LibraryTop = nil
				for _, i := range c.top {
					p.LibraryTop = append(p.LibraryTop, namesOf(f, lib[i])...)
				}
				for _, i := range c.drawn {
					p.Hand = append(p.Hand, namesOf(f, lib[i])...)
				}
				for _, i := range c.binned {
					p.Graveyard = append(p.Graveyard, namesOf(f, lib[i])...)
				}
			})
			pos := f.pos
			in, ok := f.answer(d, e)
			if !ok || f.res.Hard != nil {
				t.Fatalf("hard divergence %+v", f.res.Hard)
			}
			chosen := func(idx []int) []string {
				out := []string{}
				for _, i := range idx {
					out = append(out, namesOf(f, d.Options[i].Obj)...)
				}
				return out
			}
			if got, want := chosen(in.Choices), chosen(c.want); !reflect.DeepEqual(got, want) || f.res.ArrangePicks != c.read || f.res.Arranges != 1 || f.pos != pos {
				t.Errorf("kept %v (choices %v) arrange picks %d arranges %d pos %d, want %v and %d", got, in.Choices, f.res.ArrangePicks, f.res.Arranges, f.pos, want, c.read)
			}
		})
	}
}

// TestArrivalPickFromTheNextSnapshot is the farm cmdr4 repro (DSK-s134
// pod-13402 line 209 Satyr Wayfinder, FDN Ancient Stirrings, FRA Transwarp
// Hub, TMT Gruul Turf, WOE): a pick of cards moving into a hand has no Forge
// ask line, and the default ("up to one": nothing; else the first offered)
// left Forge's hand holding a card gorge's did not.
func TestArrivalPickFromTheNextSnapshot(t *testing.T) {
	// Option 0 is an X and option 1 a Y in both offers.
	for _, c := range []struct {
		name      string
		fromYard  bool
		min, max  int
		arrive    []int // option indices new in the hand
		lost      []int // option indices gone from the source
		unoffered bool  // another card is new in the hand too
		want      []int
		read      int
	}{
		{"a dig takes the second card", false, 0, 1, []int{1}, []int{1}, false, []int{1}, 1},
		{"a graveyard return takes the second card", true, 1, 1, []int{1}, []int{1}, false, []int{1}, 1},
		{"a return of two takes both", true, 0, 2, []int{0, 1}, []int{0, 1}, false, []int{0, 1}, 1},
		{"nothing arrived keeps the default", false, 0, 1, nil, nil, false, []int{}, 0},
		{"a card its source still holds is no pick", true, 0, 1, []int{1}, nil, false, []int{}, 0},
		{"an unoffered arrival keeps the default", true, 0, 1, []int{1}, []int{1}, true, []int{}, 0},
		{"more arrivals than allowed keep the default", true, 0, 1, []int{0, 1}, []int{0, 1}, false, []int{}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, lib, yard := snapPickFixture(t)
			offer := []state.ObjID{lib[0], lib[2]}
			if c.fromYard {
				offer = yard
			}
			d := &decision.Decision{Seq: 7, Player: 1, Kind: decision.KChoose, Min: c.min, Max: c.max,
				ResumeKind: "dig", ResumeSA: ability(t, "Satyr Wayfinder", "TrigDig")}
			if c.fromYard {
				d.ResumeKind, d.ResumeSA = "hidden_pick", ability(t, "Transwarp Hub", "DBChangeZone")
			}
			for i, id := range offer {
				d.Options = append(d.Options, decision.Option{Index: i, Kind: "hidden_pick", Obj: id, Player: 1})
			}
			e := nextSnap(f, func(p *rules.OracleSnapPlayer) {
				for _, i := range c.arrive {
					p.Hand = append(p.Hand, namesOf(f, offer[i])...)
				}
				for _, i := range c.lost {
					if c.fromYard {
						p.Graveyard = without(p.Graveyard, namesOf(f, offer[i])[0])
					} else {
						p.LibraryCount--
					}
				}
				if c.unoffered {
					p.Hand = append(p.Hand, "Not Offered")
				}
			})
			pos := f.pos
			in, ok := f.answer(d, e)
			if !ok || f.res.Hard != nil {
				t.Fatalf("hard divergence %+v", f.res.Hard)
			}
			if !slices.Equal(in.Choices, c.want) || f.res.ArrivalPicks != c.read || f.res.Chooses != 1 || f.pos != pos {
				t.Errorf("choices %v arrival picks %d chooses %d pos %d, want %v and %d", in.Choices, f.res.ArrivalPicks, f.res.Chooses, f.pos, c.want, c.read)
			}
		})
	}
}

// TestPickReadAlongTheAskedRoute drives the readers with the corpus's own
// abilities. Each offers an Island (option 0) and a Grizzly Bears (option 1),
// and the log then shows the Bears making one movement: new in the hand, gone
// from the hand, or missing from the next shuffle. That names the Bears as the
// pick only where the ask sends its chosen cards that way and leaves the
// others behind. Elsewhere the Bears may be the card Forge did not choose
// (Deliver Unto Evil's two chosen cards stay in the graveyard and the others
// return), so the answer must stay the default, the Island.
func TestPickReadAlongTheAskedRoute(t *testing.T) {
	const (
		arrives = "arrives in the hand"
		leaves  = "leaves the hand"
		shuffle = "is missing from the next shuffle"
	)
	for _, c := range []struct {
		card, svar     string
		nth            int
		resume, option string
		from           state.Zone
		seen           string
		read           bool
	}{
		{"Satyr Wayfinder", "TrigDig", 0, "dig", "dig", state.ZLibrary, arrives, true},
		{"Ancient Stirrings", "", 0, "dig", "dig", state.ZLibrary, arrives, true},
		{"Memories Returning", "DBYouDigTwo", 0, "dig", "dig", state.ZLibrary, arrives, true},
		{"Transwarp Hub", "DBChangeZone", 0, "hidden_pick", "hidden_pick", state.ZGraveyard, arrives, true},
		{"Gruul Turf", "TrigReturn", 0, "hidden_pick", "hidden_pick", state.ZBattlefield, arrives, true},
		// The chosen card goes to the library bottom, the other to the hand.
		{"Memories Returning", "DBOppDigTwo", 0, "dig", "dig", state.ZLibrary, arrives, false},
		{"Memories Returning", "DBOppDigTwo", 0, "dig", "dig", state.ZLibrary, shuffle, false},
		// The chosen cards stay, or go to the graveyard or a pile.
		{"Deliver Unto Evil", "DBChoose", 0, "choice", "card", state.ZGraveyard, arrives, false},
		{"Gifts Ungiven", "DBChangeZone1", 0, "hidden_pick", "hidden_pick", state.ZLibrary, arrives, false},
		{"Fact or Fiction", "DBTwoPiles", 0, "twopiles_split", "twopiles", state.ZLibrary, arrives, false},
		// The chosen card goes to the battlefield.
		{"Kodama's Reach", "DBChangeZone1", 0, "search", "search", state.ZLibrary, arrives, false},
		{"Kodama's Reach", "DBChangeZone1", 0, "search", "search", state.ZLibrary, shuffle, true},
		// A dig whose rest leaves the library too: a missing card is no pick.
		{"Satyr Wayfinder", "TrigDig", 0, "dig", "dig", state.ZLibrary, shuffle, false},
		{"Ancient Stirrings", "", 0, "dig", "dig", state.ZLibrary, shuffle, true},
		// Hand cards: moved by the answer, or possibly kept by it.
		{"Terrain Generator", "", 1, "hand_move", "hand_move", state.ZHand, leaves, true},
		{"Dream Cache", "ChangeZoneDB", 0, "hand_move", "hand_move", state.ZHand, leaves, true},
		{"Thoughtseize", "", 0, "discard", "discard", state.ZHand, leaves, true},
		{"Thoughtseize", "", 0, "discard_reveal", "reveal", state.ZHand, leaves, false},
		{"Deliver Unto Evil", "DBChoose", 0, "choice", "card", state.ZHand, leaves, false},
	} {
		t.Run(c.card+" "+c.resume+" "+c.svar+" "+c.seen, func(t *testing.T) {
			f, lib, yard := snapPickFixture(t)
			var offer []state.ObjID
			switch c.from {
			case state.ZLibrary:
				offer = []state.ObjID{lib[0], lib[2]}
			case state.ZHand:
				hand := f.e.G.Zone(state.ZHand, 1)
				other := slices.IndexFunc(hand, func(id state.ObjID) bool { return namesOf(f, id)[0] != namesOf(f, hand[0])[0] })
				offer = []state.ObjID{hand[0], hand[other]}
			default:
				offer = yard
				if c.from == state.ZBattlefield {
					f.e.G.SetZone(state.ZGraveyard, 1, nil)
					f.e.G.SetZone(state.ZBattlefield, 1, slices.Clone(yard))
					for _, id := range yard {
						f.e.G.Obj(id).Zone = state.ZBattlefield
					}
				}
			}
			moved := namesOf(f, offer[1])[0]
			d := &decision.Decision{Seq: 7, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1,
				ResumeKind: c.resume, ResumeSA: ability(t, c.card, c.svar, c.nth)}
			for i, id := range offer {
				d.Options = append(d.Options, decision.Option{Index: i, Kind: c.option, Obj: id, Player: 1})
			}
			var e Entry
			if c.seen == shuffle {
				sh := &Shuffle{T: TShuffle, Seat: 1}
				for _, id := range f.e.G.Zone(state.ZLibrary, 1) {
					if id != offer[1] {
						sh.Order = append(sh.Order, f.toF[id])
					}
				}
				f.shuffles[1], f.planned[1] = []*Shuffle{sh}, 0
				e = Entry{Line: 999, T: TShuffle, Shuffle: sh}
				f.log.Entries = append(f.log.Entries, e)
				f.pos = len(f.log.Entries) - 1
			} else {
				e = nextSnap(f, func(p *rules.OracleSnapPlayer) {
					if c.seen == leaves {
						p.Hand = without(p.Hand, moved)
						return
					}
					p.Hand = append(p.Hand, moved)
					switch c.from {
					case state.ZLibrary:
						p.LibraryCount--
					case state.ZGraveyard:
						p.Graveyard = without(p.Graveyard, moved)
					}
				})
				if c.from == state.ZBattlefield {
					s := &e.Snap.Snapshot
					i := slices.IndexFunc(s.Permanents, func(p rules.OracleSnapPerm) bool { return p.Name == moved })
					s.Permanents = slices.Delete(slices.Clone(s.Permanents), i, i+1)
				}
			}
			in, ok := f.answer(d, e)
			if !ok || f.res.Hard != nil {
				t.Fatalf("hard divergence %+v", f.res.Hard)
			}
			want, read := []int{0}, 0
			if c.read {
				want, read = []int{1}, 1
			}
			if got := f.res.LibraryPicks + f.res.HandPicks + f.res.ArrivalPicks; !slices.Equal(in.Choices, want) || got != read {
				t.Errorf("choices %v picks read %d, want %v and %d", in.Choices, got, want, read)
			}
		})
	}
}

// TestModesCardPickFromTheNextSnapshot is the farm cmdr4 repro
// (FIN-s131026059 pod-13102605902, line 180, p2.hand): gorge poses Faithless
// Looting's discard as a modes ask, which the default answered with the first
// two cards. Modes of an ability, one or several, are no card pick and keep
// the default.
func TestModesCardPickFromTheNextSnapshot(t *testing.T) {
	f, _, _ := snapPickFixture(t)
	hand := slices.Clone(f.e.G.Zone(state.ZHand, 1))
	other := slices.IndexFunc(hand, func(id state.ObjID) bool { return namesOf(f, id)[0] != namesOf(f, hand[0])[0] })
	if other < 0 {
		t.Fatalf("seat 1's hand %v holds one name", namesOf(f, hand...))
	}
	e := nextSnap(f, func(p *rules.OracleSnapPlayer) { p.Hand = without(p.Hand, namesOf(f, hand[other])[0]) })
	d := &decision.Decision{Seq: 7, Player: 1, Kind: decision.KModes, Min: 1, Max: 1, ResumeKind: "discard"}
	for i, id := range hand {
		d.Options = append(d.Options, decision.Option{Index: i, Kind: "discard", Obj: id, Player: 1})
	}
	in, ok := f.answer(d, e)
	if !ok || len(in.Choices) != 1 || namesOf(f, hand[in.Choices[0]])[0] != namesOf(f, hand[other])[0] || f.res.HandPicks != 1 || f.res.Modes != 0 {
		t.Errorf("choices %v hand picks %d modes %d, want the departed %v", in.Choices, f.res.HandPicks, f.res.Modes, namesOf(f, hand[other]))
	}
	// A "choose up to one" charm with a single eligible mode names its
	// source, here a card gone from the hand: the ask takes no card.
	for i, n := range []int{1, 2} {
		d := &decision.Decision{Seq: 8, Player: 1, Kind: decision.KModes, Max: 1, ResumeKind: "modes"}
		for j := range n {
			d.Options = append(d.Options, decision.Option{Index: j, Kind: "mode", Obj: hand[other], Player: 1})
		}
		if in, ok := f.answer(d, e); !ok || !slices.Equal(in.Choices, []int{0}) || f.res.Modes != i+1 || f.res.HandPicks != 1 {
			t.Errorf("%d modes of one source: choices %v modes %d hand picks %d, want the default", n, in.Choices, f.res.Modes, f.res.HandPicks)
		}
	}
}
