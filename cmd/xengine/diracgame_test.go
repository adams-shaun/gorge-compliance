package main

import "testing"

func TestDiracGameEvery(t *testing.T) {
	cases := []struct {
		every int
		want  string
	}{{1, "111111"}, {2, "010101"}, {3, "001001"}}
	for _, c := range cases {
		got := ""
		for g := 0; g < 6; g++ {
			if diracGame(g, c.every) {
				got += "1"
			} else {
				got += "0"
			}
		}
		if got != c.want {
			t.Errorf("every %d: games %s, want %s", c.every, got, c.want)
		}
	}
}
