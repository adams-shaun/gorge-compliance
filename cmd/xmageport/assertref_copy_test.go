package main

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestAssertRefCopyBindsToAssertingSeat: an assert on a name only the other
// seat holds in setup names the asserting seat's object when that seat has a
// copy-capable card (the copy carries the original's name), and keeps the
// other seat's setup ref otherwise.
func TestAssertRefCopyBindsToAssertingSeat(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	mk := func(seat0, seat1 []*placement) *translator {
		tr := &translator{reg: reg}
		tr.places[0], tr.places[1] = seat0, seat1
		return tr
	}
	pl := func(name, zone string) *placement { return &placement{name: name, zone: zone, ord: 1} }

	// Preconditions: Altered Ego can copy; Grizzly Bears cannot.
	for name, want := range map[string]bool{"Altered Ego": true, "Grizzly Bears": false} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus lacks %s", name)
		}
		got := false
		for _, f := range c.Faces {
			for _, b := range f.SVars {
				got = got || strings.Contains(b, "DB$ Clone")
			}
		}
		if got != want {
			t.Fatalf("%s has DB$ Clone = %v, want %v", name, got, want)
		}
	}

	cases := []struct {
		label        string
		seat0, seat1 []*placement
		p            int
		name, want   string
	}{
		{"copyXCreature: B's Altered Ego copies A's Endless One",
			[]*placement{pl("Endless One", "hand")}, []*placement{pl("Altered Ego", "hand")},
			1, "Endless One", "p1:Endless One"},
		{"testAddingTheCounters: A's Altered Ego copies B's Lion",
			[]*placement{pl("Altered Ego", "hand")}, []*placement{pl("Silvercoat Lion", "battlefield")},
			0, "Silvercoat Lion", "p0:Silvercoat Lion"},
		{"negative control: no copier keeps the other seat's setup ref",
			[]*placement{pl("Grizzly Bears", "battlefield")}, []*placement{pl("Silvercoat Lion", "battlefield")},
			0, "Silvercoat Lion", "p1:Silvercoat Lion"},
		{"own setup card still wins over a copy",
			[]*placement{pl("Altered Ego", "hand"), pl("Silvercoat Lion", "battlefield")},
			[]*placement{pl("Silvercoat Lion", "battlefield")},
			0, "Silvercoat Lion", "p0:Silvercoat Lion"},
	}
	for _, c := range cases {
		tr := mk(c.seat0, c.seat1)
		if len(tr.anyRefs(1-c.p, c.name)) == 0 {
			t.Fatalf("%s: precondition: other seat has no setup ref for %s", c.label, c.name)
		}
		got, r := tr.assertRef(c.p, c.name)
		if r != nil || got != c.want {
			t.Errorf("%s: assertRef(%d, %q) = %q, %v; want %q", c.label, c.p, c.name, got, r, c.want)
		}
	}
}
