package lower

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
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
	if groupsDependOnShuffle(play("p0:Island#2"), held, nil) {
		t.Error("a card the seat held at the shuffle was cut")
	}
	if !groupsDependOnShuffle(play("p0:Island#2"), empty, nil) {
		t.Error("a card drawn after the shuffle was not cut")
	}
	if groupsDependOnShuffle(cast("p0:Forest"), empty, map[string]bool{"Forest": true}) {
		t.Error("a card the shuffling resolution fetched was cut")
	}
	// A pass names no card and cannot depend on the shuffle.
	if groupsDependOnShuffle([]game.Entry{{Kind: "priority", Picks: []game.Pick{{Kind: "pass"}}}}, empty, nil) {
		t.Error("a pass was treated as a shuffled card ref")
	}
}
