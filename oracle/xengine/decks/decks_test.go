package decks

import (
	"fmt"
	"reflect"
	"sort"
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

// declaredSets is the sorted set codes of declared.json.
func declaredSets(t *testing.T) []string {
	t.Helper()
	d, err := compliance.EmbeddedDeclared()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for s := range d {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// wantUnreachable pins, per declared set at this gorge pin, how many
// candidates fit no feasible colour pair (so no deck can hold them). Sets in
// wantInfeasible have no feasible pair at all and are absent here.
var wantUnreachable = map[string]int{
	"BLB": 1, "DFT": 8, "DSK": 3, "ECL": 6, "EOE": 1, "FDN": 0, "FIN": 6, "FRA": 13,
	"LCI": 0, "MSH": 24, "OTJ": 17, "SOS": 2, "TLA": 0, "WOE": 0,
}

// wantInfeasible are the declared sets with no colour pair holding 23 eligible
// candidates: Pair cannot build decks for them, callers skip them.
var wantInfeasible = []string{"BIG", "HOB", "MKM", "SPM", "TDM", "TMT"}

func TestPairableAllDeclaredSets(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var infeasible []string
	got := map[string]int{}
	for _, set := range declaredSets(t) {
		cands, _, err := Candidates(reg, manifestDir, set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		reach, unreach := Reachable(cands)
		t.Logf("%s: %d candidates, feasible pairs %v, reachable %d, unreachable %d", set, len(cands), FeasiblePairs(cands), reach, len(unreach))
		if err := Pairable(cands); err != nil {
			infeasible = append(infeasible, set)
			if reach != 0 || len(unreach) != len(cands) {
				t.Errorf("%s: infeasible but reachable=%d unreachable=%d of %d", set, reach, len(unreach), len(cands))
			}
			continue
		}
		got[set] = len(unreach)
		unr := map[string]bool{}
		for _, n := range unreach {
			unr[n] = true
		}
		// Pair must not panic, and must never hold an unreachable card.
		for g := 0; g < 10; g++ {
			for _, d := range Pair(cands, nil, set, 0, g, 1) {
				for _, n := range d.Cards {
					if unr[n] {
						t.Errorf("%s: %s holds unreachable %s", set, d.Name, n)
					}
				}
			}
		}
	}
	if !reflect.DeepEqual(infeasible, wantInfeasible) {
		t.Errorf("infeasible sets %v, want %v", infeasible, wantInfeasible)
	}
	if !reflect.DeepEqual(got, wantUnreachable) {
		t.Errorf("unreachable counts %v, want %v", got, wantUnreachable)
	}
}

func TestPairPanicsWhenInfeasible(t *testing.T) {
	cands := []Card{{Name: "A", Identity: 1}}
	if Pairable(cands) == nil {
		t.Fatal("one card is pairable")
	}
	defer func() {
		if recover() == nil {
			t.Error("Pair on an infeasible set did not panic")
		}
	}()
	Pair(cands, nil, "X", 0, 0, 0)
}

// TestReachableFDN: every FDN candidate fits a feasible pair, and Pair reaches
// them: Reachable agrees with the decks actually drawn.
func TestReachableFDN(t *testing.T) {
	_, cands := fdn(t)
	reach, unreach := Reachable(cands)
	if reach+len(unreach) != len(cands) {
		t.Fatalf("reachable %d + unreachable %d != %d", reach, len(unreach), len(cands))
	}
	if len(FeasiblePairs(cands)) != 10 {
		t.Errorf("FDN feasible pairs %v, want all 10", FeasiblePairs(cands))
	}
	t.Logf("FDN: %d reachable, %d unreachable %v", reach, len(unreach), unreach)
}

// TestCandidatesXMageFilter: with Options.XMage no candidate is a card XMage
// spells differently, exactly the cards that spell differently are left out
// (as Skip "xmage_spelling", in candidate order) and the count is printed.
// FDN and BLB, the calibration sets, lose none; APC (Fire // Ice) and ALL
// (a diacritic) show the filter does bite.
func TestCandidatesXMageFilter(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	bit := 0
	for _, set := range []string{"FDN", "BLB", "APC", "ALL"} {
		all, _, err := Candidates(reg, manifestDir, set)
		if err != nil {
			t.Fatal(err)
		}
		got, skips, err := CandidatesWith(reg, manifestDir, set, Options{XMage: true})
		if err != nil {
			t.Fatal(err)
		}
		var want []Card
		var wantOut []string
		for _, c := range all {
			if sp.Differs(reg, c.Name) {
				wantOut = append(wantOut, c.Name)
			} else {
				want = append(want, c)
			}
		}
		for _, c := range got {
			if sp.Differs(reg, c.Name) {
				t.Errorf("%s: candidate %q has an XMage spelling that differs", set, c.Name)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: filtered candidates are not the unfiltered minus the differing ones (%d vs %d)", set, len(got), len(want))
		}
		var out []string
		for _, s := range skips {
			if s.Reason == ReasonXMageSpelling {
				out = append(out, s.Card)
			}
		}
		if !reflect.DeepEqual(out, wantOut) || ExcludedXMage(skips) != len(wantOut) {
			t.Errorf("%s: excluded %v (count %d), want %v", set, out, ExcludedXMage(skips), wantOut)
		}
		fmt.Printf("xmage filter %s: %d of %d candidates excluded for a differing XMage spelling\n", set, ExcludedXMage(skips), len(all))
		bit += len(wantOut)
	}
	if bit == 0 {
		t.Error("the filter excluded nothing in any set: the test proves nothing")
	}
	// Without the option nothing is left out.
	_, skips, _ := Candidates(reg, manifestDir, "APC")
	if ExcludedXMage(skips) != 0 {
		t.Errorf("Candidates without Options.XMage excluded %d cards", ExcludedXMage(skips))
	}
}
