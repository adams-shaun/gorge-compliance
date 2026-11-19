package ptcounter

import "testing"

// A digit run too large for int is not a counter this package understands:
// it must report ok=false so the caller keeps its fallback, not fold to P0
// (which would collide with a real +0 component).
func TestCanonicalOverflow(t *testing.T) {
	for _, kind := range []string{
		"P999999999999999999999P1",
		"+999999999999999999999/+1",
		"P1M999999999999999999999",
	} {
		if got, ok := Canonical(kind); ok || got != kind {
			t.Fatalf("Canonical(%q) = %q, %v; want the kind back and ok=false", kind, got, ok)
		}
	}
}
