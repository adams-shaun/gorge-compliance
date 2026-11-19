package main

import (
	"math/bits"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// setPicker deals an infeasible bonus-sheet set (BIG) with its parent: the pool is
// feasible, marks the child's cards, and names no dirac table.
func TestSetPickerParentSet(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const manifests = "../../oracle/compliance/manifests"
	alone, _, err := decks.Candidates(reg, manifests, "BIG")
	if err != nil {
		t.Fatal(err)
	}
	if decks.Pairable(alone) == nil {
		t.Skip("BIG is feasible alone")
	}
	pick, err := setPicker(reg, manifests, "BIG", cov.New(), decks.Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cands, _, table := pick(0, 1)
	if table != "" || decks.Pairable(cands) != nil {
		t.Fatalf("table %q, pairable %v", table, decks.Pairable(cands))
	}
	child := 0
	for _, c := range cands {
		if c.Child {
			child++
		}
	}
	if child != len(alone) {
		t.Errorf("%d child cards in the pool, want BIG's %d", child, len(alone))
	}
	// With the full effect registry the child has well over 8 cards: every deck
	// holds at least 8 of them (all it has in its colours when fewer).
	isChild := map[string]uint8{}
	for _, c := range cands {
		if c.Child {
			isChild[c.Name] = c.Identity
		}
	}
	for g := 0; g < 100; g++ {
		for _, d := range decks.Pair(cands, cov.New(), "BIG", 0, g, 1) {
			var m uint8
			for _, l := range d.Colours {
				m |= 1 << strings.IndexRune("WUBRG", l)
			}
			avail, got := 0, 0
			for _, id := range isChild {
				if id&^m == 0 {
					avail++
				}
			}
			for _, n := range d.Cards {
				if _, ok := isChild[n]; ok {
					got++
				}
			}
			if got < min(decks.MinChild, avail) {
				t.Errorf("%s: %d BIG cards, want at least %d", d.Name, got, min(decks.MinChild, avail))
			}
		}
	}
}

// With the full effect registry, a 200-game TDM deal against a ledger that records each dealt card puts every reachable
// three-colour candidate and tri-land in some deck.
func TestSetPickerThreeColour(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pick, err := setPicker(reg, "../../oracle/compliance/manifests", "TDM", cov.New(), decks.Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cands, _, _ := pick(0, 1)
	_, unreach := decks.Reachable(cands)
	unr := map[string]bool{}
	for _, n := range unreach {
		unr[n] = true
	}
	want := map[string]bool{}
	for _, c := range cands {
		if bits.OnesCount8(c.Identity) == 3 && !unr[c.Name] {
			want[c.Name] = true
		}
	}
	// Each dealt card is exercised once, as a sweep's ledger records it, so the
	// weights steer later games to the cards not yet dealt.
	led := cov.New()
	for g := 0; g < 200; g++ {
		for _, d := range decks.Pair(cands, led, "TDM", 0, g, 1) {
			for _, n := range d.Cards {
				delete(want, n)
				led.Add(n, "cast", cov.Counts{Exercised: 1})
			}
		}
	}
	t.Logf("%d candidates, %d unreachable", len(cands), len(unreach))
	if len(want) > 0 {
		t.Errorf("three-colour cards never dealt in 200 games: %v", want)
	}
}
