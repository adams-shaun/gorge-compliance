package oraclediff

import "testing"

// gorge marks a creature dealt deathtouch damage with an internal DEATHTOUCHED
// counter; the followers have no such counter, so the comparison omits it and
// keeps every real counter.
func TestCountersOmitsDeathtouchedMarker(t *testing.T) {
	if got := counters(map[string]int32{"Deathtouched": 1}); got != "" {
		t.Fatalf("marker rendered: %q", got)
	}
	if got := counters(map[string]int32{"Deathtouched": 1, "P1P1": 2}); got != "P1P1=2" {
		t.Fatalf("got %q", got)
	}
	if got := counters(map[string]int32{"DEATHTOUCH": 1}); got != "DEATHTOUCH=1" {
		t.Fatalf("keyword counter dropped: %q", got)
	}
}
