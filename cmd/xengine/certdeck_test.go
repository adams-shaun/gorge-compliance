package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/deck"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func certdeckPlan(t *testing.T, b *certdeck.Builder, target string) *certdeck.Plan {
	t.Helper()
	tc, ok := b.Reg.Lookup(target)
	if !ok {
		t.Fatalf("%s is not in the corpus", target)
	}
	needs, err := certdeck.ParseNeeds("auto", tc, b.Types())
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.Build(target, needs)
	if err != nil {
		t.Fatalf("%s: %v", target, err)
	}
	return p
}

func certdeckBuilder(t *testing.T) (*cards.Registry, *certdeck.Builder) {
	reg := testutil.CorpusRegistry(t) // skips on a checkout with no corpus
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	return reg, certdeck.NewBuilder(reg, sp)
}

func hasNeed(p *certdeck.Plan, n certdeck.Need) bool {
	for _, x := range p.Needs {
		if x == n {
			return true
		}
	}
	return false
}

func deckCount(f deck.File, pred func(name string) bool) int {
	n := 0
	for _, e := range f.Cards {
		if pred(e.Name) {
			n += e.Count
		}
	}
	return n
}

// The need classes a card's script and Oracle text imply decide what its two
// decks hold, and the same corpus and target give byte-identical files.
func TestCertdeckNeeds(t *testing.T) {
	reg, b := certdeckBuilder(t)
	face := func(name string) *cards.Face {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s is not in the corpus", name)
		}
		return c.Faces[0]
	}

	// A snow-mana payoff gets snow lands (and snow permanents).
	p := certdeckPlan(t, b, "Icehide Troll")
	if !hasNeed(p, certdeck.Need{Class: certdeck.Snow}) {
		t.Fatalf("Icehide Troll needs %v, want snow", p.Needs)
	}
	if n := deckCount(p.Self.File, func(s string) bool { return strings.HasPrefix(s, "Snow-Covered ") }); n < 15 {
		t.Errorf("snow payoff deck holds %d snow lands, want at least 15", n)
	}
	if got := p.Self.Provided["snow"]; len(got) == 0 {
		t.Errorf("no snow enablers provided")
	} else {
		for _, n := range got {
			if !face(n).HasTypeWord("Snow") {
				t.Errorf("snow enabler %s is not a snow permanent", n)
			}
		}
	}
	if n := deckCount(p.Opponent.File, func(s string) bool { return strings.HasPrefix(s, "Snow-Covered ") }); n != 0 {
		t.Errorf("opponent deck holds %d snow lands", n)
	}

	// An Elf lord gets Elves.
	p = certdeckPlan(t, b, "Elvish Archdruid")
	if !hasNeed(p, certdeck.Need{Class: certdeck.Type, Arg: "Elf"}) {
		t.Fatalf("Elvish Archdruid needs %v, want type:Elf", p.Needs)
	}
	elves := p.Self.Provided["type:Elf"]
	if len(elves) < 4 {
		t.Errorf("Elf lord got %d Elves, want at least 4: %v", len(elves), elves)
	}
	for _, n := range elves {
		if f := face(n); !f.IsCreature() || !f.HasTypeWord("Elf") {
			t.Errorf("%s is not an Elf creature", n)
		}
	}

	// A "whenever ~ blocks" creature's opponent has attackers.
	p = certdeckPlan(t, b, "Ageless Sentinels")
	if !hasNeed(p, certdeck.Need{Class: certdeck.OppAttackers}) {
		t.Fatalf("Ageless Sentinels needs %v, want opp-attackers", p.Needs)
	}
	atk := p.Opponent.Provided["opp-attackers"]
	if len(atk) < 4 {
		t.Errorf("opponent got %d attackers, want at least 4: %v", len(atk), atk)
	}
	inDeck := map[string]bool{}
	for _, e := range p.Opponent.File.Cards {
		inDeck[e.Name] = true
	}
	for _, n := range atk {
		if f := face(n); !f.IsCreature() || f.Power() < 2 || !inDeck[n] {
			t.Errorf("attacker %s: creature %v power %d in deck %v", n, f.IsCreature(), f.Power(), inDeck[n])
		}
	}
	if hasNeed(p, certdeck.Need{Class: certdeck.OppBlockers}) {
		t.Errorf("a blocks-only card also needs blockers: %v", p.Needs)
	}

	// Both decks are 40 cards, the target is 4 copies (1 if legendary) and
	// every deck is deterministic: two runs write byte-identical files.
	for _, target := range []string{"Icehide Troll", "Ageless Sentinels", "Elvish Archdruid"} {
		var files [2]map[string][]byte
		for i := range files {
			out := t.TempDir()
			if err := runCertdeck([]string{"-cards", "../../.cards", "-manifests", manifestDir, "-target", target, "-out", out}); err != nil {
				t.Fatal(err)
			}
			files[i] = map[string][]byte{}
			for _, f := range []string{certdeckSelfFile, certdeckOppFile} {
				raw, err := os.ReadFile(filepath.Join(out, f))
				if err != nil {
					t.Fatal(err)
				}
				files[i][f] = raw
				df, err := deck.Parse(raw)
				if err != nil {
					t.Fatal(err)
				}
				if n := deckCount(df, func(string) bool { return true }); n != certdeck.DeckSize {
					t.Errorf("%s %s: %d cards, want %d", target, f, n, certdeck.DeckSize)
				}
			}
			var sf deck.File
			json.Unmarshal(files[i][certdeckSelfFile], &sf)
			if n := deckCount(sf, func(s string) bool { return s == target }); n != 4 {
				t.Errorf("%s: %d copies of the target, want 4", target, n)
			}
		}
		for f, raw := range files[0] {
			if !bytes.Equal(raw, files[1][f]) {
				t.Errorf("%s %s differs between two runs", target, f)
			}
		}
	}

	// A legendary target is one copy.
	p = certdeckPlan(t, b, "Ashaya, Soul of the Wild")
	if n := deckCount(p.Self.File, func(s string) bool { return s == p.Target }); n != 1 {
		t.Errorf("legendary target: %d copies, want 1", n)
	}

	// -need names classes explicitly; a bad name is an error naming the choices.
	tc, _ := reg.Lookup("Icehide Troll")
	if ns, err := certdeck.ParseNeeds("snow,counters,life", tc, b.Types()); err != nil || len(ns) != 4 {
		t.Errorf("ParseNeeds explicit = %v, %v; want snow, counters:P1P1, life:total, opp-burn", ns, err)
	}
	if _, err := certdeck.ParseNeeds("snow,bogus", tc, b.Types()); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("ParseNeeds bogus = %v", err)
	}
}

// The decks of 20 sampled cards with a partner need (spread over the classes,
// in name order) all load under play -decks, with XMage's spellings checked,
// and none is skipped.
func TestCertdeckPlayable(t *testing.T) {
	reg, b := certdeckBuilder(t)
	byClass := map[string][]string{}
	for _, c := range reg.AllCards() {
		if len(c.Faces) == 0 || !b.Playable(c) {
			continue
		}
		ns := certdeck.Derive(c, b.Types())
		if len(ns) == 0 {
			continue
		}
		byClass[ns[0].Class] = append(byClass[ns[0].Class], c.Faces[0].Name)
	}
	classes := make([]string, 0, len(byClass))
	for k := range byClass {
		sort.Strings(byClass[k])
		classes = append(classes, k)
	}
	sort.Strings(classes)
	var sample []string
	for i := 0; len(sample) < 20 && len(classes) > 0; i++ {
		for _, k := range classes {
			if l := byClass[k]; len(l) > 0 && len(sample) < 20 {
				sample = append(sample, l[(i*7919)%len(l)])
			}
		}
	}
	if len(sample) != 20 {
		t.Fatalf("sampled %d cards, want 20 (classes %v)", len(sample), classes)
	}
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, name := range sample {
		if seen[name] {
			continue
		}
		seen[name] = true
		dir := t.TempDir()
		if err := writeCertdeckPlan(dir, certdeckPlan(t, b, name)); err != nil {
			t.Fatal(err)
		}
		ds, info, err := loadDeckDir(reg, dir, sp)
		if err != nil || len(ds) != 2 || len(info.Skipped) != 0 || info.Playable != 2 {
			t.Errorf("%s: %d decks, %v, skipped %v, summary %q", name, len(ds), err, info.Skipped, info)
		}
	}
}
