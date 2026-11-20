package certdeck

import "testing"

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
