package decks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const manifestDir = "../../compliance/manifests"

func fdn(t *testing.T) (*cards.Registry, []Card) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cands, _, err := Candidates(reg, manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	return reg, cands
}

func TestCandidatesFDN(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cands, skips, err := Candidates(reg, manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	m, err := compliance.LoadManifest(manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) == 0 {
		t.Fatal("no FDN candidates")
	}
	if len(cands)+len(skips) != len(m.Cards) {
		t.Errorf("%d candidates + %d skips != %d manifest cards", len(cands), len(skips), len(m.Cards))
	}
	for _, s := range skips {
		if s.Reason == "" {
			t.Errorf("skip %q has no reason", s.Card)
		}
	}
	for _, c := range cands {
		switch c.Name {
		case "Plains", "Island", "Swamp", "Mountain", "Forest":
			t.Errorf("basic land %s is a candidate", c.Name)
		}
		card, _ := reg.Lookup(c.Name)
		if card == nil || hasType(card.Faces[0].Types, "Basic") {
			t.Errorf("%s: missing or basic", c.Name)
		}
	}
	basics := 0
	for _, s := range skips {
		if s.Reason == "basic" {
			basics++
		}
	}
	if basics == 0 {
		t.Error("no FDN card was skipped as basic")
	}
}

func TestPairDeterministic(t *testing.T) {
	_, cands := fdn(t)
	led := cov.New()
	led.Add(cands[0].Name, "cast", cov.Counts{Exercised: 3})
	a := Pair(cands, led, "FDN", 1, 4, 99)
	b := Pair(cands, led, "FDN", 1, 4, 99)
	if !reflect.DeepEqual(a, b) {
		t.Error("same inputs gave different decks")
	}
	if reflect.DeepEqual(a, Pair(cands, led, "FDN", 1, 5, 99)) {
		t.Error("different game gave the same pair")
	}
	if reflect.DeepEqual(a, Pair(cands, led, "FDN", 1, 4, 100)) {
		t.Error("different seed gave the same pair")
	}
	if reflect.DeepEqual(a, Pair(cands, cov.New(), "FDN", 1, 4, 99)) {
		t.Error("different coverage gave the same pair")
	}
}

func TestPairShape(t *testing.T) {
	_, cands := fdn(t)
	byName := map[string]Card{}
	for _, c := range cands {
		byName[c.Name] = c
	}
	basicColour := map[string]int{"Plains": 0, "Island": 1, "Swamp": 2, "Mountain": 3, "Forest": 4}
	letters := "WUBRG"
	pairs := map[string]bool{}
	for g := 0; g < 50; g++ {
		for _, d := range Pair(cands, nil, "FDN", 0, g, 7) {
			pairs[d.Colours] = true
			if len(d.Cards) != 40 {
				t.Fatalf("%s: %d cards", d.Name, len(d.Cards))
			}
			var mask uint8
			for _, l := range d.Colours {
				mask |= 1 << strings.IndexRune(letters, l)
			}
			basics, seen := 0, map[string]bool{}
			counts := map[string]int{}
			for i, n := range d.Cards {
				if i > 0 && d.Cards[i-1] > n {
					t.Fatalf("%s: not sorted", d.Name)
				}
				if bi, ok := basicColour[n]; ok {
					if mask&(1<<bi) == 0 {
						t.Errorf("%s: off-colour basic %s", d.Name, n)
					}
					basics++
					counts[n]++
					continue
				}
				c, ok := byName[n]
				if !ok {
					t.Fatalf("%s: %q is not a candidate", d.Name, n)
				}
				if c.Identity&^mask != 0 {
					t.Errorf("%s: %s identity %05b outside %s", d.Name, n, c.Identity, d.Colours)
				}
				if seen[n] {
					t.Errorf("%s: duplicate %s", d.Name, n)
				}
				seen[n] = true
			}
			if basics != 17 {
				t.Errorf("%s: %d basics", d.Name, basics)
			}
			for n, k := range counts {
				if k < 7 {
					t.Errorf("%s: only %d %s", d.Name, k, n)
				}
			}
		}
	}
	if len(pairs) < 10 {
		t.Errorf("only %d of 10 colour pairs reached: %v", len(pairs), pairs)
	}
}

func TestPairWeighting(t *testing.T) {
	_, cands := fdn(t)
	led := cov.New()
	heavy := map[string]bool{}
	for i, c := range cands {
		if i%2 == 0 {
			heavy[c.Name] = true
			led.Add(c.Name, "cast", cov.Counts{Exercised: 20})
		}
	}
	var hv, lt int
	for g := 0; g < 200; g++ {
		for _, d := range Pair(cands, led, "FDN", 0, g, 7) {
			for _, n := range d.Cards {
				if heavy[n] {
					hv++
				} else if _, basic := map[string]bool{"Plains": true, "Island": true, "Swamp": true, "Mountain": true, "Forest": true}[n]; !basic {
					lt++
				}
			}
		}
	}
	t.Logf("heavily exercised appearances %d, others %d", hv, lt)
	if lt < 2*hv {
		t.Errorf("unexercised cards appear %d times vs %d for heavily exercised: want at least 2x", lt, hv)
	}
}
