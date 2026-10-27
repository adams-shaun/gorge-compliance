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
