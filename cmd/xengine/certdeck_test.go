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

// enablerCopies is the copies a deck holds of cards picked for a need: the
// provided cards, each counted once, excluding the basic lands a land-type
// need names (those are the mana base's).
func enablerCopies(d certdeck.Deck) int {
	cnt := map[string]int{}
	for _, e := range d.File.Cards {
		cnt[e.Name] = e.Count
	}
	seen := map[string]bool{}
	total := 0
	for _, k := range sortedProvidedKeys(d) {
		if strings.HasPrefix(k, "landtype:") || strings.HasPrefix(k, "opp-landtype:") {
			continue
		}
		for _, n := range d.Provided[k] {
			if !seen[n] {
				seen[n] = true
				total += cnt[n]
			}
		}
	}
	return total
}

func sortedProvidedKeys(d certdeck.Deck) []string {
	ks := make([]string, 0, len(d.Provided))
	for k := range d.Provided {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
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

	// The five classes the brief names that read other keys than the obvious:
	// each card derives its need and the deck holds a provider for it.
	for _, tc := range []struct {
		card string
		need certdeck.Need
	}{
		{"Kor Spiritdancer", certdeck.Need{Class: certdeck.Aura}},
		{"Hardened Scales", certdeck.Need{Class: certdeck.Counters, Arg: "P1P1"}},     // R:Event$ AddCounter | ValidCounterType$
		{"Corpsejack Menace", certdeck.Need{Class: certdeck.Counters, Arg: "P1P1"}},   // DB$ ReplaceCounter
		{"Winding Constrictor", certdeck.Need{Class: certdeck.Counters, Arg: "P1P1"}}, // no counter type at all
		{"Korozda Gorgon", certdeck.Need{Class: certdeck.Counters, Arg: "P1P1"}},      // Cost$ SubCounter<1/P1P1/Creature.YouCtrl/...>
		{"Anointed Procession", certdeck.Need{Class: certdeck.Token}},                 // R:Event$ CreateToken
		{"Doubling Season", certdeck.Need{Class: certdeck.Token}},
		{"Treasure Cruise", certdeck.Need{Class: certdeck.Graveyard}}, // K:Delve
		{"Gurmag Angler", certdeck.Need{Class: certdeck.Graveyard}},
	} {
		p := certdeckPlan(t, b, tc.card)
		if !hasNeed(p, tc.need) {
			t.Errorf("%s needs %v, want %v", tc.card, p.Needs, tc.need)
			continue
		}
		if len(p.Self.Provided[tc.need.String()]) == 0 {
			t.Errorf("%s: no provider for %v", tc.card, tc.need)
		}
	}
	// The aura is in the deck file, and is an Aura that enchants a creature.
	p = certdeckPlan(t, b, "Kor Spiritdancer")
	if auras := p.Self.Provided["aura"]; len(auras) == 0 {
		t.Errorf("Kor Spiritdancer has no auras: %v", p.Self.Provided)
	} else {
		inDeck := map[string]bool{}
		for _, e := range p.Self.File.Cards {
			inDeck[e.Name] = true
		}
		for _, n := range auras {
			if f := face(n); !f.HasTypeWord("Aura") || !inDeck[n] {
				t.Errorf("aura %s: Aura %v in target.json %v", n, f.HasTypeWord("Aura"), inDeck[n])
			}
		}
	}
	// A replacement's Artifact.YouCtrl is what it applies to, not a need to control artifacts.
	if p := certdeckPlan(t, b, "Winding Constrictor"); hasNeed(p, certdeck.Need{Class: certdeck.Artifacts}) {
		t.Errorf("Winding Constrictor needs %v: the replacement filter is no artifacts need", p.Needs)
	}
	// The card's own name is no Aura mention.
	if p := certdeckPlan(t, b, "Aura of Silence"); hasNeed(p, certdeck.Need{Class: certdeck.Aura}) {
		t.Errorf("Aura of Silence needs %v: its name is no aura need", p.Needs)
	}

	// A land type is a land's, not a creature's: a fetch land names the basics
	// it fetches and the deck holds them; an untap-a-Forest creature wants Forests.
	for _, tc := range []struct {
		card   string
		basics []string
	}{
		{"Arid Mesa", []string{"Mountain", "Plains"}},
		{"Bloodstained Mire", []string{"Swamp", "Mountain"}},
		{"Krosan Verge", []string{"Forest", "Plains"}},
		{"Arbor Elf", []string{"Forest"}},
	} {
		p := certdeckPlan(t, b, tc.card)
		for _, n := range p.Needs {
			if n.Class == certdeck.Type || n.Class == certdeck.OppType {
				t.Errorf("%s derives creature need %v from a land type", tc.card, n)
			}
		}
		for _, basic := range tc.basics {
			if !hasNeed(p, certdeck.Need{Class: certdeck.LandType, Arg: basic}) {
				t.Errorf("%s needs %v, want landtype:%s", tc.card, p.Needs, basic)
			}
			if n := deckCount(p.Self.File, func(s string) bool { return s == basic }); n < 4 {
				t.Errorf("%s: %d %s in the target deck, want at least 4", tc.card, n, basic)
			}
		}
	}
	// Land Creatures (Jasconian Isle, Gobland, Dryad Arbor) print land types
	// next to their creature types; the creature types keep the creature ones.
	for _, lt := range []string{"Plains", "Island", "Swamp", "Mountain", "Forest", "Desert"} {
		if b.Types()[lt] {
			t.Errorf("CreatureTypes holds the land type %s", lt)
		}
	}
	for _, ct := range []string{"Fish", "Goblin", "Dryad"} {
		if !b.Types()[ct] {
			t.Errorf("CreatureTypes lost the creature type %s", ct)
		}
	}

	// A card name in a filter is not a type: Detention Sphere's
	// "!namedDetention Sphere" is not the Sphere land type, nor is "namedCommand
	// Tower" the Tower one, "namedNikara, Lair Scavenger" the Lair one, or
	// "namedPower Plant Worker" the Plant creature type.
	for _, c := range []struct{ card, class, arg string }{
		{"Detention Sphere", certdeck.LandType, "Sphere"},
		{"Tower Winder", certdeck.LandType, "Tower"},
		{"Yannik, Scavenging Sentinel", certdeck.LandType, "Lair"},
		{"Tower Worker", certdeck.Type, "Plant"},
	} {
		p := certdeckPlan(t, b, c.card)
		if hasNeed(p, certdeck.Need{Class: c.class, Arg: c.arg}) {
			t.Errorf("%s derives %s:%s from a card name in a filter (needs %v)", c.card, c.class, c.arg, p.Needs)
		}
		if n := deckCount(p.Self.File, func(s string) bool { return strings.Contains(s, c.arg) && s != p.Target }); n > 0 && c.class == certdeck.LandType {
			t.Errorf("%s deck holds %d cards named for %s", c.card, n, c.arg)
		}
	}

	// ChooseType's ValidTypes$ is a list of choices, not a filter: the card
	// derives no creature-type need from it.
	p = certdeckPlan(t, b, "Eclipsed Realms")
	for _, n := range p.Needs {
		if n.Class == certdeck.Type || n.Class == certdeck.OppType {
			t.Errorf("Eclipsed Realms derives %v from a ChooseType list", n)
		}
	}

	// "Time Lord" is one creature type the corpus prints as two words.
	p = certdeckPlan(t, b, "Trenzalore Clocktower")
	if !hasNeed(p, certdeck.Need{Class: certdeck.Type, Arg: "Time-Lord"}) {
		t.Errorf("Trenzalore Clocktower needs %v, want type:Time-Lord", p.Needs)
	}
	for _, n := range p.Needs {
		if n.Arg == "Time" || n.Arg == "Lord" {
			t.Errorf("Trenzalore Clocktower derives %v, want the one type Time-Lord", n)
		}
	}
	if len(p.Self.Provided["type:Time-Lord"]) == 0 {
		t.Errorf("no Time Lord in the Trenzalore Clocktower deck: %v %s", p.Self.Provided, p.Self.Colours)
	}

	// A colourless target plays the colours that supply what it needs (the
	// Halflings of Hobbit Hole are not red).
	p = certdeckPlan(t, b, "Hobbit Hole")
	if len(p.Self.Provided["type:Halfling"]) == 0 {
		t.Errorf("Hobbit Hole deck (%s) holds no Halfling; needs %v", p.Self.Colours, p.Needs)
	}

	// Whatever a deck provides for a land type is a land that has it.
	for _, target := range []string{"Arid Mesa", "Krosan Verge", "Detention Sphere", "Command Tower"} {
		p := certdeckPlan(t, b, target)
		assertLandTypeProviders(t, b, target, p.Self)
		assertLandTypeProviders(t, b, target, p.Opponent)
	}

	// The twelve enabler slots are a cap: no deck, on either side, holds more
	// than twelve copies of cards picked for a need (Candles of Leng held 13).
	for _, target := range []string{"Candles of Leng", "Icehide Troll", "Elvish Archdruid", "Kor Spiritdancer", "Hardened Scales", "Treasure Cruise"} {
		p := certdeckPlan(t, b, target)
		for _, side := range []struct {
			name string
			d    certdeck.Deck
		}{{"target", p.Self}, {"opponent", p.Opponent}} {
			if n := enablerCopies(side.d); n > certdeck.MaxEnablerSlots {
				t.Errorf("%s %s deck holds %d enabler copies, want at most %d", target, side.name, n, certdeck.MaxEnablerSlots)
			}
		}
	}

	// A split card is refused with the reason (XMage lists "Cut // Ribbons"
	// and play -decks -xmage skips a deck that names the front face).
	if _, err := b.Build("Cut", nil); err == nil || !strings.Contains(err.Error(), "XMage spells") {
		t.Errorf("Build(Cut) = %v, want the XMage spelling refusal", err)
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

// assertLandTypeProviders: every card provided for a landtype need is a land
// with that subtype (or the basic of it, for a basic type).
func assertLandTypeProviders(t *testing.T, b *certdeck.Builder, target string, d certdeck.Deck) {
	t.Helper()
	for _, k := range sortedProvidedKeys(d) {
		arg, ok := strings.CutPrefix(k, "landtype:")
		if !ok {
			if arg, ok = strings.CutPrefix(k, "opp-landtype:"); !ok {
				continue
			}
		}
		for _, name := range d.Provided[k] {
			c, ok := b.Reg.Lookup(name)
			if !ok {
				t.Errorf("%s: %s provider %q is not in the corpus", target, k, name)
				continue
			}
			f := c.Faces[0]
			if !f.IsLand() || (!f.HasTypeWord(arg) && !strings.HasSuffix(name, arg)) {
				t.Errorf("%s: %s provider %q is not a land with that type (types %v)", target, k, name, f.Types)
			}
		}
	}
}

// The decks of 20 distinct sampled cards with a partner need (spread over the
// classes, in name order) all load under play -decks, with XMage's spellings
// checked, none is skipped, and every need a card derives has a card in the
// deck that supplies it. (No list of the stuck cards is in the repo: the
// sample is every playable card whose script derives a need.)
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
		// A counter type no other card puts (a card's own TIME or UNITY
		// counters) has no partner to supply; certdeck reports it UNMET.
		if ns[0].Class == certdeck.Counters && ns[0].Arg != "P1P1" {
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
	seen := map[string]bool{}
	var sample []string
	for i := 0; len(sample) < 20 && i < 1000; i++ {
		for _, k := range classes {
			l := byClass[k]
			if len(l) == 0 || len(sample) == 20 {
				continue
			}
			if n := l[(i*7919)%len(l)]; !seen[n] {
				seen[n] = true
				sample = append(sample, n)
			}
		}
	}
	if len(sample) != 20 {
		t.Fatalf("sampled %d distinct cards, want 20 (classes %v)", len(sample), classes)
	}
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range sample {
		plan := certdeckPlan(t, b, name)
		for _, n := range plan.Needs {
			d := plan.Self
			if certdeck.OppSide(n.Class) {
				d = plan.Opponent
			}
			if len(d.Provided[n.String()]) == 0 {
				t.Errorf("%s: need %v has no provider in its deck", name, n)
			}
		}
		assertLandTypeProviders(t, b, name, plan.Self)
		assertLandTypeProviders(t, b, name, plan.Opponent)
		dir := t.TempDir()
		if err := writeCertdeckPlan(dir, plan); err != nil {
			t.Fatal(err)
		}
		ds, info, err := loadDeckDir(reg, dir, sp)
		if err != nil || len(ds) != 2 || len(info.Skipped) != 0 || info.Playable != 2 {
			t.Errorf("%s: %d decks, %v, skipped %v, summary %q", name, len(ds), err, info.Skipped, info)
		}
	}
}
