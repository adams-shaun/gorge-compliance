package cov

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// goldenCards is the fixed inventory fixture: one card per shape.
var goldenCards = []string{
	"Grizzly Bears",      // vanilla creature
	"Prodigal Sorcerer",  // one activated ability
	"Elvish Visionary",   // ETB trigger
	"Jace Beleren",       // planeswalker
	"Sulfurous Springs",  // land, mana abilities
	"Valki, God of Lies", // modal DFC
	"Bonecrusher Giant",  // adventure
	"The Eldest Reborn",  // saga
}

// keysGolden renders testdata/keys.golden from the fixed corpus fixture.
func keysGolden(t *testing.T, reg *cards.Registry) string {
	t.Helper()
	var b strings.Builder
	for _, n := range goldenCards {
		c, ok := reg.Lookup(n)
		if !ok {
			t.Fatalf("%q not in the corpus", n)
		}
		fmt.Fprintf(&b, "%s\n", n)
		for _, k := range Keys(c) {
			m := ""
			if k.Mana {
				m = " mana"
			}
			fmt.Fprintf(&b, "  %s\t%s%s\n", k.ID, k.Desc, m)
		}
	}
	return b.String()
}

func TestInventoryGolden(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	b := keysGolden(t, reg)
	const path = "testdata/keys.golden"
	if os.Getenv("XENGINE_REGEN") != "" {
		if err := os.WriteFile(path, []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != b {
		t.Errorf("keys differ from %s (XENGINE_REGEN=1 to regenerate)\n--- got\n%s--- want\n%s", path, b, want)
	}
}

// universeFixture is a hand-built card and the exact Universe ids it must
// produce. The cards stand in for loader output -- an expanded keyword carries
// the printed name in Params["Keyword"] -- so the universe tests do not depend
// on which cards happen to be in the corpus pin.
type universeFixture struct {
	name string
	card *cards.Card
	want []string
}

func universeFixtures() []universeFixture {
	creature := func(keywords ...string) *cards.Card {
		return &cards.Card{Faces: []*cards.Face{{Types: []string{"Creature"}, Keywords: keywords}}}
	}
	return []universeFixture{
		{
			name: "static",
			card: &cards.Card{Faces: []*cards.Face{{
				Types:   []string{"Creature"},
				Statics: []cards.Static{{Mode: "Continuous"}},
			}}},
			want: []string{"cast", "static#0.0"},
		},
		{
			name: "replacement",
			card: &cards.Card{Faces: []*cards.Face{{
				Types: []string{"Enchantment"},
				Repls: []cards.Repl{{Event: "GainLife", Params: map[string]string{"Keyword": "Test"}}},
			}}},
			want: []string{"cast", "repl#0.0"},
		},
		{
			name: "unexpanded keyword",
			card: creature("Haste"),
			want: []string{"cast", "kw#0.0"},
		},
		{
			name: "expanded keyword",
			card: &cards.Card{Faces: []*cards.Face{{
				Types:    []string{"Creature"},
				Keywords: []string{"Cycling:2"},
				Abilities: []*cards.SA{{
					Kind: "AB", API: "ChangeZone",
					Params: map[string]string{"Keyword": "Cycling"},
				}},
			}}},
			want: []string{"cast", "f0/a0"},
		},
		{
			name: "three-mode charm",
			card: &cards.Card{Faces: []*cards.Face{{
				Types: []string{"Sorcery"},
				Abilities: []*cards.SA{{
					Kind: "SP", API: "Charm",
					Params: map[string]string{"Choices": "Dosan's Oldest Chant,Kodama's Reach,Time of Need"},
				}},
			}}},
			want: []string{"cast", "mode#0.0.0", "mode#0.0.1", "mode#0.0.2"},
		},
		{
			name: "flying creature",
			card: creature("Flying"),
			want: []string{"cast", "combat#0.attack", "combat#0.block"},
		},
		{
			name: "two faces",
			card: &cards.Card{Faces: []*cards.Face{
				{Types: []string{"Creature"}, Keywords: []string{"Haste"}, Statics: []cards.Static{{Mode: "Continuous"}}},
				{Types: []string{"Creature"}, Keywords: []string{"Flying"}, Repls: []cards.Repl{{Event: "Moved"}}},
			}},
			want: []string{"cast", "static#0.0", "repl#1.0", "kw#0.0", "combat#1.attack", "combat#1.block"},
		},
		{
			name: "basic land",
			card: &cards.Card{Faces: []*cards.Face{{
				Types:    []string{"Basic", "Land"},
				Keywords: []string{"Haste"}, // not a level-B combat keyword, but still no group 5
			}}},
			want: []string{"land"},
		},
	}
}

func TestUniverseCoversStaticsReplacementsKeywords(t *testing.T) {
	for _, fx := range universeFixtures() {
		var ids []string
		for _, k := range Universe(fx.card) {
			ids = append(ids, k.ID)
		}
		if !slices.Equal(ids, fx.want) {
			t.Errorf("%s: Universe ids = %v, want %v", fx.name, ids, fx.want)
		}
	}
}

func TestUniverseKeepsKeysPrefix(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	type namedCard struct {
		name string
		card *cards.Card
	}
	var fixtures []namedCard
	for _, n := range goldenCards {
		c, ok := reg.Lookup(n)
		if !ok {
			t.Fatalf("%q not in the corpus", n)
		}
		fixtures = append(fixtures, namedCard{n, c})
	}
	for _, fx := range universeFixtures() {
		fixtures = append(fixtures, namedCard{fx.name, fx.card})
	}
	for _, fx := range fixtures {
		keys := Keys(fx.card)
		all := Universe(fx.card)
		if len(all) < len(keys) || !slices.Equal(all[:len(keys)], keys) {
			t.Errorf("%s: Universe = %v, want Keys prefix %v", fx.name, all, keys)
		}
	}
	// Keys is unchanged: the committed keys.golden still renders byte for byte.
	gotKeys := keysGolden(t, reg)
	wantKeys, err := os.ReadFile("testdata/keys.golden")
	if err != nil {
		t.Fatal(err)
	}
	if gotKeys != string(wantKeys) {
		t.Errorf("testdata/keys.golden changed (XENGINE_REGEN=1 to regenerate)\n--- got\n%s--- want\n%s", gotKeys, wantKeys)
	}
	// One line per fixture card and key: <card>\t<id>\t<desc>.
	var b strings.Builder
	for _, fx := range fixtures {
		for _, k := range Universe(fx.card) {
			fmt.Fprintf(&b, "%s\t%s\t%s\n", fx.name, k.ID, k.Desc)
		}
	}
	const path = "testdata/universe.golden"
	if os.Getenv("XENGINE_REGEN") != "" {
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if b.String() != string(want) {
		t.Errorf("universe differs from %s (XENGINE_REGEN=1 to regenerate)\n--- got\n%s--- want\n%s", path, b.String(), want)
	}
}

func TestFamily(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{"cast", "cast"},
		{"land", "land"},
		{"f0/a0", "activated"},
		{"f11/a23", "activated"},
		{"f0/t0", "trigger"},
		{"f11/t23", "trigger"},
		{"mode#0.0.0", "mode"},
		{"static#1.0", "static"},
		{"repl#0.2", "repl"},
		{"kw#0.1", "keyword"},
		{"combat#0.attack", "combat"},
		{"combat#1.block", "combat"},
		{"not-a-key", ""},
	} {
		if got := Family(tc.id); got != tc.want {
			t.Errorf("Family(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestLedgerRoundTrip(t *testing.T) {
	a := New()
	a.Add("Alpha", "cast", Counts{Exercised: 2, Compared: 1})
	a.Add("Alpha", "f0/a0", Counts{Offered: 3})
	a.Add("Beta", "land", Counts{Exercised: 1})
	a.Add("Alpha", "f0/t0", Counts{})
	var one bytes.Buffer
	if err := a.WriteTSV(&one); err != nil {
		t.Fatal(err)
	}
	b, err := ReadTSV(bytes.NewReader(one.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var two bytes.Buffer
	if err := b.WriteTSV(&two); err != nil {
		t.Fatal(err)
	}
	if one.String() != two.String() {
		t.Fatalf("round trip not stable:\n%s\n%s", one.String(), two.String())
	}
	if !strings.HasPrefix(one.String(), tsvHeader+"\nAlpha\tcast\t") {
		t.Errorf("not sorted under a header:\n%s", one.String())
	}
	m := New()
	m.Merge(a)
	m.Merge(b)
	if got := m.Get("Alpha", "cast"); got != (Counts{Exercised: 4, Compared: 2}) {
		t.Errorf("merge twice: %+v", got)
	}
	if got := m.Get("Alpha", "f0/a0"); got.Offered != 6 {
		t.Errorf("offered: %+v", got)
	}
	var m1, m2 bytes.Buffer
	m.WriteTSV(&m1)
	m.WriteTSV(&m2)
	if m1.String() != m2.String() {
		t.Error("merged bytes unstable")
	}
	var rep bytes.Buffer
	m.Report(&rep, map[string][]string{"X": {"Alpha", "Beta", "Gamma"}})
	want := "X: candidates 3, exercised 2, fully compared 0, never exercised 1\n  never exercised: Gamma\n"
	if rep.String() != want {
		t.Errorf("report:\n%s\nwant\n%s", rep.String(), want)
	}
}

// TestReportFullyCompared: "fully compared" needs the cast/land key AND every
// ability key compared, which Report can only see for keys that were seeded.
func TestReportFullyCompared(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const withAbility, vanilla = "Prodigal Sorcerer", "Grizzly Bears"
	l := New()
	for _, n := range []string{withAbility, vanilla} {
		c, ok := reg.Lookup(n)
		if !ok {
			t.Fatalf("%q not in the corpus", n)
		}
		l.Seed(n, c)
	}
	sets := map[string][]string{"X": {withAbility, vanilla}}
	report := func() string {
		var b bytes.Buffer
		l.Report(&b, sets)
		return b.String()
	}
	// Both cast, only the cast keys compared: the ability key is unexercised.
	l.Add(withAbility, "cast", Counts{Exercised: 1, Compared: 1})
	l.Add(vanilla, "cast", Counts{Exercised: 1, Compared: 1})
	want := "X: candidates 2, exercised 2, fully compared 1, never exercised 0\n"
	if got := report(); got != want {
		t.Errorf("ability key still zero:\n%s\nwant\n%s", got, want)
	}
	// Exercised but not compared ability key: still not fully compared.
	l.Add(withAbility, "f0/a0", Counts{Exercised: 1})
	if got := report(); got != want {
		t.Errorf("ability key exercised only:\n%s\nwant\n%s", got, want)
	}
	l.Add(withAbility, "f0/a0", Counts{Compared: 1})
	want = "X: candidates 2, exercised 2, fully compared 2, never exercised 0\n"
	if got := report(); got != want {
		t.Errorf("all keys compared:\n%s\nwant\n%s", got, want)
	}
}

func TestLedgerSub(t *testing.T) {
	base := New()
	base.Add("A", "cast", Counts{Exercised: 5, Compared: 2, Offered: 1})
	base.Add("D", "k", Counts{Exercised: 4})
	base.Add("E", "k", Counts{Exercised: 1, Compared: 1, Offered: 1})
	l := New()
	l.Add("A", "cast", Counts{Exercised: 8, Compared: 7, Offered: 3})
	l.Add("B", "land", Counts{Exercised: 1})
	l.Add("E", "k", Counts{Exercised: 1, Compared: 1, Offered: 1})
	var baseBefore bytes.Buffer
	base.WriteTSV(&baseBefore)

	l.Sub(base)
	for _, w := range []struct {
		card, key string
		want      Counts
	}{
		{"A", "cast", Counts{Exercised: 3, Compared: 5, Offered: 2}},
		{"B", "land", Counts{Exercised: 1}},
		{"E", "k", Counts{}},
		{"D", "k", Counts{}},
	} {
		if got := l.Get(w.card, w.key); got != w.want {
			t.Errorf("%s/%s after Sub: %+v, want %+v", w.card, w.key, got, w.want)
		}
	}
	var one, two bytes.Buffer
	l.WriteTSV(&one)
	l.WriteTSV(&two)
	if one.String() != two.String() {
		t.Error("written bytes unstable")
	}
	wantTSV := tsvHeader + "\nA\tcast\t3\t5\t2\nB\tland\t1\t0\t0\nE\tk\t0\t0\t0\n"
	if one.String() != wantTSV {
		t.Errorf("tsv:\n%swant\n%s", one.String(), wantTSV)
	}
	var baseAfter bytes.Buffer
	base.WriteTSV(&baseAfter)
	if baseBefore.String() != baseAfter.String() {
		t.Error("Sub changed its argument")
	}

	l.Sub(base)
	if got := l.Get("A", "cast"); got != (Counts{Exercised: 0, Compared: 3, Offered: 1}) {
		t.Errorf("second Sub: %+v (want saturation at 0)", got)
	}
	if got := l.Get("B", "land"); got != (Counts{Exercised: 1}) {
		t.Errorf("untouched row: %+v", got)
	}
}
