package certdeck

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// apportion never returns more lands than it was given, and gives every colour
// of the mask one when there are enough.
func TestApportionClamp(t *testing.T) {
	sum := func(a [5]int) int { return a[0] + a[1] + a[2] + a[3] + a[4] }
	for _, c := range []struct {
		pips [5]int
		mask uint8
		n    int
	}{
		{[5]int{}, 31, 1}, {[5]int{}, 31, 0}, {[5]int{}, 31, 3}, {[5]int{}, 31, 5}, {[5]int{}, 31, 17},
		{[5]int{0, 2, 0, 5, 1}, 0b01010, 1}, {[5]int{4, 0, 0, 0, 0}, 1, 0}, {[5]int{1, 1, 1, 1, 1}, 31, -2},
		{[5]int{3, 0, 0, 1, 0}, 0b01001, 20},
	} {
		got := apportion(c.pips, c.mask, c.n)
		want := c.n
		if want < 0 {
			want = 0
		}
		if sum(got) != want {
			t.Errorf("apportion(%v, %b, %d) = %v (%d lands), want %d", c.pips, c.mask, c.n, got, sum(got), want)
		}
	}
	// One land, two colours: the colour with the pips gets it.
	if got := apportion([5]int{0, 2, 0, 5, 1}, 0b01010, 1); got != [5]int{0, 0, 0, 1, 0} {
		t.Errorf("one land goes to the heavier colour: %v", got)
	}
}

// Banding is met by copies, so the suppliers a deck's colours must reach depend
// on whether the target has banding itself: a banding target brings four
// copies and needs two other cards of two, besides itself; a card that only
// grants or reads banding brings none and needs four suppliers.
func TestMinReachBanding(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings("../../compliance/manifests")
	if err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(reg, sp)
	get := func(name string) *facts {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s is not in the corpus", name)
		}
		return b.facts(c)
	}
	band := Need{Class: Banding}
	for _, c := range []struct {
		target string
		copies int
		want   int
	}{{"Benalish Hero", 4, 3}, {"Banding Sliver", 4, 4}} {
		if got := minReach(band, get(c.target), c.copies); got != c.want {
			t.Errorf("minReach(banding) for %s = %d, want %d", c.target, got, c.want)
		}
	}
	if got := minReach(Need{Class: Bodies}, get("Benalish Hero"), 4); got != 1 {
		t.Errorf("minReach(bodies) = %d, want 1", got)
	}
}
