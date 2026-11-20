package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const poolsDir = "../../oracle/xengine/pools"

func TestParseNameList(t *testing.T) {
	got := parseNameList([]byte("# header\n\nLlanowar Elves\n  Shock  \nShock\nSerra Angel # a note\n#\n\r\nGiant Growth\r\n"))
	want := []string{"Llanowar Elves", "Shock", "Serra Angel", "Giant Growth"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseNameList = %q, want %q", got, want)
	}
}

func TestPoolFlagErr(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		pool, filter, random, forgeOnly string
		dirac                           bool
		want                            string // "" = no error
	}{
		{name: "none"},
		{name: "pool alone", pool: "p.txt"},
		{name: "pool and filter", pool: "p.txt", filter: "f.txt"},
		{name: "filter without pool", filter: "f.txt", want: "-pool-filter needs -pool"},
		{name: "random", pool: "p.txt", random: "FDN", want: "-random"},
		{name: "forge-only", pool: "p.txt", forgeOnly: "x.txt", want: "-forge-only"},
		{name: "dirac", pool: "p.txt", dirac: true, want: "-dirac"},
		{name: "all", pool: "p.txt", random: "FDN", forgeOnly: "x.txt", dirac: true, want: "-random, -forge-only, -dirac"},
	} {
		err := poolFlagErr(tc.pool, tc.filter, tc.random, tc.forgeOnly, tc.dirac)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: unexpected error %v", tc.name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: error %v, want one naming %q", tc.name, err, tc.want)
		}
	}
}

// runPlay refuses the conflicting flags before it loads the corpus.
func TestRunPlayPoolConflict(t *testing.T) {
	err := runPlay([]string{"-set", "FDN", "-pool", "p.txt", "-random", "FDN BLB", "-cards", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "-random") {
		t.Fatalf("runPlay -pool -random: %v, want the conflict error", err)
	}
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The committed fdn.txt deals what -set FDN deals: the same candidates, in
// the same order, so decks.Pair draws the same decks.
func TestPoolFDNMatchesSet(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, xmage := range []bool{false, true} {
		want, _, err := decks.CandidatesWith(reg, manifestDir, "FDN", decks.Options{XMage: xmage})
		if err != nil {
			t.Fatal(err)
		}
		got, info, err := poolCandidates(reg, manifestDir, filepath.Join(poolsDir, "fdn.txt"), "", xmage)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("xmage=%v: -pool fdn.txt has %d candidates, -set FDN %d; they differ", xmage, len(got), len(want))
		}
		if info.Candidates != len(want) {
			t.Errorf("xmage=%v: info.Candidates = %d, want %d", xmage, info.Candidates, len(want))
		}
		led := cov.New()
		a := decks.Pair(got, led, "FDN", 0, 1, 7)
		b := decks.Pair(want, led, "FDN", 0, 1, 7)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("xmage=%v: decks differ", xmage)
		}
	}
}

// fdn-commons.txt is a subset of fdn.txt, and the generator reproduces
// fdn.txt byte for byte from the committed manifest.
func TestPoolFiles(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	all, err := readListFile(filepath.Join(poolsDir, "fdn.txt"))
	if err != nil {
		t.Fatal(err)
	}
	commons, err := readListFile(filepath.Join(poolsDir, "fdn-commons.txt"))
	if err != nil {
		t.Fatal(err)
	}
	in := map[string]bool{}
	for _, n := range all.names {
		in[n] = true
	}
	for _, n := range commons.names {
		if !in[n] {
			t.Errorf("fdn-commons.txt: %q is not in fdn.txt", n)
		}
	}
	m, err := compliance.LoadManifest(manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	body, n, missing := poolNames(reg, m, func(string) bool { return true })
	if n != len(all.names) || len(missing) > 0 || strings.Join(all.names, "\n")+"\n" != string(body) {
		t.Errorf("fdn.txt is stale: regenerate it (oracle/xengine/pools/README.md)")
	}
}

func TestPoolCandidatesUnknownAndFilter(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	dir := t.TempDir()
	fdn := filepath.Join(poolsDir, "fdn.txt")
	b, err := os.ReadFile(fdn)
	if err != nil {
		t.Fatal(err)
	}
	bad := writeFile(t, dir, "bad.txt", string(b)+"Not A Real Card\nLlanowar Elfs\n")
	_, _, err = poolCandidates(reg, manifestDir, bad, "", false)
	if err == nil || !strings.Contains(err.Error(), "Not A Real Card") || !strings.Contains(err.Error(), "Llanowar Elfs") {
		t.Fatalf("unknown names: %v, want an error naming both", err)
	}

	full, _, err := poolCandidates(reg, manifestDir, fdn, "", false)
	if err != nil {
		t.Fatal(err)
	}
	drop := full[0].Name
	filter := writeFile(t, dir, "filter.txt", "# follower refuses\n"+drop+"\nNot A Real Card\n")
	got, info, err := poolCandidates(reg, manifestDir, fdn, filter, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(full)-1 || info.Filtered != 1 {
		t.Fatalf("filter dropped %d (info %d), want 1", len(full)-len(got), info.Filtered)
	}
	for _, c := range got {
		if c.Name == drop {
			t.Fatalf("filter kept %q", drop)
		}
	}
	if !reflect.DeepEqual(info.FilterUnmatched, []string{"Not A Real Card"}) {
		t.Errorf("FilterUnmatched = %q", info.FilterUnmatched)
	}
	if info.Filter == nil || info.Filter.SHA256 == "" || info.Pool.SHA256 == "" {
		t.Errorf("pool.json lacks the pool or filter sha256: %+v", info)
	}

	tiny := writeFile(t, dir, "tiny.txt", strings.Join([]string{full[0].Name, full[1].Name}, "\n"))
	if _, _, err := poolCandidates(reg, manifestDir, tiny, "", false); err == nil {
		t.Fatalf("a two-card pool built decks")
	}
}

// TestPoolThreeColour: a pool headed "# supp WUB" deals two decks of that
// identity, with basics of its three colours. A pool name outside the identity
// is left out and counted, so the 23 eligible cards of a 30-name pool are all
// dealt: the target is in both decks.
func TestPoolThreeColour(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const wub = uint8(cards.ColourWhite | cards.ColourBlue | cards.ColourBlack)
	// Real gorge-supported cards of Esper-ish sets: a WUB target, 22 more
	// cards inside WUB, and 7 outside it. Candidates applies the same support
	// and basic filters the pool does, so the pool is playable at this pin.
	var inWUB, offWUB []decks.Card
	seen := map[string]bool{}
	for _, set := range []string{"ALA", "ARB", "SNC", "FDN"} {
		cands, _, err := decks.Candidates(reg, manifestDir, set)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cands {
			if seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			if c.Identity&^wub == 0 {
				inWUB = append(inWUB, c)
			} else {
				offWUB = append(offWUB, c)
			}
		}
	}
	target := ""
	for _, c := range inWUB {
		if c.Identity == wub {
			target = c.Name
			break
		}
	}
	if target == "" || len(inWUB) < 23 || len(offWUB) < 7 {
		t.Fatalf("corpus gives a WUB target %q, %d cards inside WUB and %d outside; want a target, 23 and 7", target, len(inWUB), len(offWUB))
	}
	var rest []decks.Card
	for _, c := range inWUB {
		if c.Name != target && len(rest) < 22 {
			rest = append(rest, c)
		}
	}
	if len(rest) != 22 {
		t.Fatalf("only %d cards inside WUB beside the target", len(rest))
	}
	var body strings.Builder
	body.WriteString("# supp WUB tier=0 targets=1\n")
	body.WriteString(target + "\n")
	for _, c := range rest {
		body.WriteString(c.Name + "\n")
	}
	for _, c := range offWUB[:7] {
		body.WriteString(c.Name + "\n")
	}
	dir := t.TempDir()
	pool := writeFile(t, dir, "wub.txt", body.String())

	got, info, err := poolCandidates(reg, manifestDir, pool, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if info.Pool.Identity != "WUB" {
		t.Errorf("pool.json identity = %q, want WUB", info.Pool.Identity)
	}
	if len(got) != 23 || info.Candidates != 23 {
		t.Fatalf("pool kept %d candidates (info %d), want 23: %+v", len(got), info.Candidates, info.Skipped)
	}
	if info.Skipped[decks.ReasonOutsideIdentity] != 7 {
		t.Errorf("outside-identity skip = %d, want 7 (%+v)", info.Skipped[decks.ReasonOutsideIdentity], info.Skipped)
	}
	byName := map[string]decks.Card{}
	for _, c := range got {
		byName[c.Name] = c
	}

	deal := identityDealer(poolPicker(got, cov.New()), "SUP", 0, 11, info.Pool.identity)
	pair, table := deal(0)
	if table != "" || len(pair) != 2 {
		t.Fatalf("deal gave %d decks, table %q; want two", len(pair), table)
	}
	basics := map[string]bool{"Plains": true, "Island": true, "Swamp": true}
	for _, d := range pair {
		if len(d.Cards) != 40 || d.Colours != "WUB" {
			t.Errorf("%s: %d cards, colours %q; want 40 of WUB", d.Name, len(d.Cards), d.Colours)
			continue
		}
		found, nBasics := false, 0
		perBasic := map[string]int{}
		spells := map[string]bool{}
		for _, n := range d.Cards {
			if basics[n] {
				nBasics++
				perBasic[n]++
				continue
			}
			c, ok := byName[n]
			if !ok {
				t.Errorf("%s: %q is not one of the 23 candidates", d.Name, n)
				continue
			}
			if c.Identity&^wub != 0 {
				t.Errorf("%s: %s identity %05b outside WUB", d.Name, n, c.Identity)
			}
			if spells[n] {
				t.Errorf("%s: duplicate %s", d.Name, n)
			}
			spells[n] = true
			if n == target {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: does not hold the WUB target %s", d.Name, target)
		}
		if nBasics != 17 {
			t.Errorf("%s: %d basics, want 17", d.Name, nBasics)
		}
		for _, n := range []string{"Plains", "Island", "Swamp"} {
			if perBasic[n] == 0 {
				t.Errorf("%s: no %s", d.Name, n)
			}
		}
	}
}
