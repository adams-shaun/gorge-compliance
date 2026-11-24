package certdeck

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// apportion never returns more lands than it was given, and gives every colour
// of the mask one when there are enough.
func TestApportionClamp(t *testing.T) {
	sum := func(a [5]int) int { return a[0] + a[1] + a[2] + a[3] + a[4] }
	for _, c := range []struct {
		pips [5]int
		mask uint8
		n    int
	}{
		{[5]int{}, 31, 1}, {[5]int{}, 31, 0}, {[5]int{}, 31, 3}, {[5]int{}, 31, 5}, {[5]int{}, 31, 17},
		{[5]int{0, 2, 0, 5, 1}, 0b01010, 1}, {[5]int{4, 0, 0, 0, 0}, 1, 0}, {[5]int{1, 1, 1, 1, 1}, 31, -2},
		{[5]int{3, 0, 0, 1, 0}, 0b01001, 20},
	} {
		got := apportion(c.pips, c.mask, c.n)
		want := c.n
		if want < 0 {
			want = 0
		}
		if sum(got) != want {
			t.Errorf("apportion(%v, %b, %d) = %v (%d lands), want %d", c.pips, c.mask, c.n, got, sum(got), want)
		}
	}
	// One land, two colours: the colour with the pips gets it.
	if got := apportion([5]int{0, 2, 0, 5, 1}, 0b01010, 1); got != [5]int{0, 0, 0, 1, 0} {
		t.Errorf("one land goes to the heavier colour: %v", got)
	}
}

// Banding is met by copies, so the suppliers a deck's colours must reach depend
// on whether the target has banding itself: a banding target brings four
// copies and needs two other cards of two, besides itself; a card that only
// grants or reads banding brings none and needs four suppliers.
func TestMinReachBanding(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings("../../compliance/manifests")
	if err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(reg, sp)
	get := func(name string) *facts {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s is not in the corpus", name)
		}
		return b.facts(c)
	}
	band := Need{Class: Banding}
	for _, c := range []struct {
		target string
		copies int
		want   int
	}{{"Benalish Hero", 4, 3}, {"Banding Sliver", 4, 4}} {
		if got := minReach(band, get(c.target), c.copies); got != c.want {
			t.Errorf("minReach(banding) for %s = %d, want %d", c.target, got, c.want)
		}
	}
	if got := minReach(Need{Class: Bodies}, get("Benalish Hero"), 4); got != 1 {
		t.Errorf("minReach(bodies) = %d, want 1", got)
	}
}

// forgeTestBuilder is a Builder with the synthetic Forge editions fixture
// applied. It is synthetic test data in Forge's edition format, not a copy of
// Forge's (GPL) editions.
func forgeTestBuilder(t *testing.T) (*cards.Registry, *Builder) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings("../../compliance/manifests")
	if err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(reg, sp)
	if err := b.UseForgeEditions("testdata/editions"); err != nil {
		t.Fatal(err)
	}
	return reg, b
}

func lookupCard(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("%s is not in the corpus", name)
	}
	return c
}

// writeEdition writes one synthetic Forge edition file under dir.
func writeEdition(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A Funny or Silver-border edition filters a card outside [eternal] by name,
// a legal [eternal] reprint stays, an all-digital printing excludes a card
// with no paper one, and a paper reprint keeps a digital card in.
func TestForgeEditionsParser(t *testing.T) {
	dir := t.TempDir()
	writeEdition(t, dir, "Funny Fixture.txt", `[metadata]
Code=FUN
Name=Funny Fixture
Type=Funny

[eternal]
1 C Eternal Bear @A

[cards]
F501 R Playtest Only @A
CAa C Suffixed Number @A
C Rarity Only @A
2 C Plains @A
3 C Grizzly Bears @A
`)
	writeEdition(t, dir, "Silver Fixture.txt", `[metadata]
Code=SIL
Name=Silver Fixture
Border=Silver

[cards]
1 C Silver Only @A
`)
	writeEdition(t, dir, "Paper Fixture.txt", `[metadata]
Code=PAP
Name=Paper Fixture
Type=Core

[cards]
1 C Legal Card @A
2 C Grizzly Bears @A
3 C Shared Digital And Paper @A
`)
	writeEdition(t, dir, "Digital Fixture.txt", `[metadata]
Code=DIG
Name=Digital Fixture
Type=Online

[cards]
1 R Digital Only @A
2 R Shared Digital And Paper @A
`)
	ed, err := loadForgeEditions(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"Playtest Only", true},
		{"Suffixed Number", true},
		{"Rarity Only", true},
		{"Grizzly Bears", true}, // Forge filters by name even with a paper reprint
		{"Silver Only", true},
		{"Digital Only", true},
		{"Eternal Bear", false}, // [eternal] is the legal reprint
		{"Legal Card", false},
		{"Shared Digital And Paper", false}, // a paper printing wins
		{"Plains", false},                   // basic lands are excepted
		{"Snow-Covered Forest", false},
		{"Not Printed Anywhere", false},
	} {
		if got := ed.excluded(tc.name); got != tc.want {
			t.Errorf("excluded(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The pinned repro cards are not playable as enablers while tournament-legal
// cards of the same shape are; the focus card itself is untouched.
func TestPlayableExcludesForgeFiltered(t *testing.T) {
	reg, b := forgeTestBuilder(t)
	for _, name := range []string{"Indicate", "Mox Poison", "Gobland"} {
		if b.Playable(lookupCard(t, reg, name)) {
			t.Errorf("%s is playable, want excluded: it is a Mystery Booster 2 playtest card Forge's card DB filters", name)
		}
	}
	// Same shape as the three: a 0-mana sorcery that targets a permanent, a
	// 0-mana artifact, and a land creature. All are tournament-legal paper
	// cards in an ordinary edition.
	for _, name := range []string{"Vindicate", "Mox Amber", "Dryad Arbor"} {
		if !b.Playable(lookupCard(t, reg, name)) {
			t.Errorf("%s is not playable, want playable: it is a tournament-legal paper card", name)
		}
	}
	// The focus card is not the pool: a deck certifying a filtered card still
	// builds, with the card as its target.
	plan, err := b.Build("Indicate", nil)
	if err != nil {
		t.Fatalf("Build(Indicate) failed: the focus card must be unaffected by the pool filter: %v", err)
	}
	if plan.Target != "Indicate" {
		t.Errorf("Build(Indicate) target = %q, want Indicate", plan.Target)
	}
}

// The real pinned Forge editions, when a clone is on the box, exclude the same
// three cards. The test skips on a checkout that has never set up Forge; its
// absence is not evidence the pinned editions fail to name them.
func TestPlayableAgainstPinnedForgeEditions(t *testing.T) {
	dir := FindForgeEditions()
	if dir == "" {
		t.Skip("no pinned Forge editions on this box (set FORGE_ORACLE_DIR or -forge-editions)")
	}
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings("../../compliance/manifests")
	if err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(reg, sp)
	if err := b.UseForgeEditions(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Indicate", "Mox Poison", "Gobland"} {
		if b.Playable(lookupCard(t, reg, name)) {
			t.Errorf("%s is playable against %s, want excluded", name, dir)
		}
	}
}
