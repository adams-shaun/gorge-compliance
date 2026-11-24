package lower

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestMarkShuffles: an activated ability whose label shuffles marks its
// seat's library order as unreliable from the step's checkpoint on; other
// picks (a cast, a plain ability) and a swapped seat map are handled.
func TestMarkShuffles(t *testing.T) {
	entry := func(kind, label string, player int) []game.Entry {
		return []game.Entry{{Kind: "priority", Picks: []game.Pick{{Kind: kind, Label: label, Player: player}}}}
	}
	cases := []struct {
		name string
		m    seatMap
		g    []game.Entry
		want []string
	}{
		{"shuffle ability", seatMap{}, entry("ability", "Feldon's Cane: Shuffle your graveyard into your library.", 0), []string{"p0.library_top@6"}},
		{"swapped seat", seatMap{swap: true}, entry("ability", "Shuffle your library.", 0), []string{"p1.library_top@6"}},
		{"cast", seatMap{}, entry("cast", "Cast Shuffle Thoughts", 0), nil},
		{"other ability", seatMap{}, entry("ability", "Draw a card.", 0), nil},
	}
	for _, c := range cases {
		var l Lowered
		l.markShuffles(c.m, c.g, 5)
		l.markShuffles(c.m, c.g, 5)
		if !reflect.DeepEqual(l.Item.Ignore, c.want) {
			t.Errorf("%s: ignore %v, want %v", c.name, l.Item.Ignore, c.want)
		}
	}
}

// TestGroupsDependOnShuffle: a play or cast after a shuffle is replayable only
// when the seat already held the card at the shuffle (a library shuffle does
// not move the hand) or the shuffling resolution fetched it (a search puts the
// card in hand before the shuffle). A card drawn after the shuffle is the
// runner's own draw (CR 701.24a), so naming the live object is not.
func TestGroupsDependOnShuffle(t *testing.T) {
	held := game.Digest{Players: []game.PlayerDigest{{Seat: 0, Hand: []string{"Island"}}}}
	empty := game.Digest{Players: []game.PlayerDigest{{Seat: 0}}}
	play := func(ref string) []game.Entry {
		return []game.Entry{{Kind: "priority", Picks: []game.Pick{{Kind: "play_land", Ref: ref}}}}
	}
	cast := func(ref string) []game.Entry {
		return []game.Entry{{Kind: "priority", Picks: []game.Pick{{Kind: "cast", Ref: ref}}}}
	}
	if groupsDependOnShuffle(play("p0:Island#2"), held, nil, false) {
		t.Error("a card the seat held at the shuffle was cut")
	}
	if !groupsDependOnShuffle(play("p0:Island#2"), empty, nil, false) {
		t.Error("a card drawn after the shuffle was not cut")
	}
	if groupsDependOnShuffle(cast("p0:Forest"), empty, map[string]bool{"Forest": true}, false) {
		t.Error("a card the shuffling resolution fetched was cut")
	}
	// A pass names no card and cannot depend on the shuffle.
	if groupsDependOnShuffle([]game.Entry{{Kind: "priority", Picks: []game.Pick{{Kind: "pass"}}}}, empty, nil, false) {
		t.Error("a pass was treated as a shuffled card ref")
	}
	// A shuffle that also moves the hand and graveyard (Weftwalking) can move
	// a card the seat held, so held is not a safe spelling then.
	if !groupsDependOnShuffle(play("p0:Island#2"), held, nil, true) {
		t.Error("a held card was not cut after a zone-moving shuffle")
	}
	// The resolution's own fetch is still safe: it moved the card before the
	// shuffle.
	if groupsDependOnShuffle(cast("p0:Forest"), empty, map[string]bool{"Forest": true}, true) {
		t.Error("a card the shuffling resolution fetched was cut after a zone-moving shuffle")
	}
}

// TestLowerCutsPlayAfterGroupShuffle drives the cut at the lowering's group
// walk, not just groupsDependOnShuffle's boolean: an entry with an unaccounted
// draw sets the shuffle zone (groupShuffle ok), and the next play that names a
// card the shuffle could have moved stops the lowering with shuffled_ref. The
// Weftwalking trigger additionally exercises zoneMovingShuffle's parsed SA
// chain (Origin$ Hand,Graveyard | Destination$ Library | Shuffle$ True).
//
// A span cannot reach this cut any more for the shuffle-plus-draw cards: the
// window's rng gate sees the draw after the shuffle (surplus != the library's
// count-1) and stops earlier, so this is the guard's end-to-end test.
func TestLowerCutsPlayAfterGroupShuffle(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	dig := func(hand ...string) game.Digest {
		return game.Digest{Players: []game.PlayerDigest{{Seat: 0, Hand: hand}, {Seat: 1}}}
	}
	shuffle := func(d game.Digest, trigs []game.Trig) game.Entry {
		return game.Entry{Kind: "priority", RNG: 2, Triggers: trigs, Picks: []game.Pick{{Kind: "pass"}}, Digest: d}
	}
	play := func(ref string) game.Entry {
		return game.Entry{Kind: "priority", Picks: []game.Pick{{Kind: "play_land", Ref: ref}}}
	}
	lower := func(es []game.Entry) Lowered {
		l := Lowered{Against: &game.Tape{Entries: es}}
		l.lowerEntries(reg, seatMap{rngOK: true}, es, 0, false)
		return l
	}
	shuffled := unsupported("shuffled_ref").reason
	// A pure library shuffle leaves the hand alone; a card the seat already
	// held is still there in the runner and stays replayable.
	if l := lower([]game.Entry{shuffle(dig("Forest"), nil), play("p0:Forest#2")}); l.Truncated == shuffled {
		t.Errorf("a held card after a pure library shuffle was cut: %+v", l.Item.Steps)
	}
	// A card the shuffle could have drawn into hand is not.
	if l := lower([]game.Entry{shuffle(dig("Forest"), nil), play("p0:Island#34")}); l.Truncated != shuffled {
		t.Errorf("truncated %q at %d, want %q: an unheld card after a shuffle must be cut", l.Truncated, l.TruncatedAt, shuffled)
	}
	// Weftwalking's trigger shuffles the hand and graveyard into the library,
	// so even a card the seat held at the shuffle is unsafe.
	weft := []game.Trig{{Name: "Weftwalking", Face: 0, Index: 0}}
	if l := lower([]game.Entry{shuffle(dig("Island"), weft), play("p0:Island#34")}); l.Truncated != shuffled {
		t.Errorf("truncated %q at %d, want %q: a held card after a zone-moving shuffle must be cut", l.Truncated, l.TruncatedAt, shuffled)
	}
}
