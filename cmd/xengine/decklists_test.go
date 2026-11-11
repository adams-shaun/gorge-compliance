package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestDecksFlagErr(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		dir, pool, random, forgeOnly string
		dirac                        bool
		want                         string // "" = no error
	}{
		{name: "none"},
		{name: "decks alone", dir: "d"},
		{name: "pool without decks", pool: "p.txt"},
		{name: "pool", dir: "d", pool: "p.txt", want: "-pool"},
		{name: "random", dir: "d", random: "FDN", want: "-random"},
		{name: "forge-only", dir: "d", forgeOnly: "x.txt", want: "-forge-only"},
		{name: "dirac", dir: "d", dirac: true, want: "-dirac"},
		{name: "all", dir: "d", pool: "p", random: "FDN", forgeOnly: "x", dirac: true, want: "-pool, -random, -forge-only, -dirac"},
	} {
		err := decksFlagErr(tc.dir, tc.pool, tc.random, tc.forgeOnly, tc.dirac)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: unexpected error %v", tc.name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: error %v, want one naming %q", tc.name, err, tc.want)
		}
	}
}

// runPlay refuses the conflicting flags before it loads the corpus (the
// -cards dir is empty, so a corpus load would fail with another error).
func TestRunPlayDecksConflict(t *testing.T) {
	for _, extra := range [][]string{{"-pool", "p.txt"}, {"-random", "FDN"}, {"-forge-only", "x.txt"}, {"-dirac"}} {
		args := append([]string{"-set", "STD", "-decks", t.TempDir(), "-cards", t.TempDir()}, extra...)
		err := runPlay(args)
		if err == nil || !strings.Contains(err.Error(), "-decks is the whole deck source") || !strings.Contains(err.Error(), extra[0]) {
			t.Errorf("runPlay -decks %v: %v, want the conflict error", extra, err)
		}
	}
}

// fixedPair is a pure function of its inputs, always two different decks in
// range, depends on the names, chunk, game and seed, and puts each deck in
// seat 0 in about half its games.
func TestFixedPair(t *testing.T) {
	names := []string{"A", "B", "C", "D", "E"}
	seat0, games := map[int]int{}, map[int]int{}
	differ := 0
	for chunk := 0; chunk < 20; chunk++ {
		for g := 0; g < 50; g++ {
			a, b := fixedPair(names, chunk, g, 7)
			if a2, b2 := fixedPair(append([]string(nil), names...), chunk, g, 7); a2 != a || b2 != b {
				t.Fatalf("chunk %d game %d: %d,%d then %d,%d", chunk, g, a, b, a2, b2)
			}
			if a == b || a < 0 || b < 0 || a >= len(names) || b >= len(names) {
				t.Fatalf("chunk %d game %d: pair %d,%d", chunk, g, a, b)
			}
			if (a < b) != ((chunk+g)%2 == 0) {
				t.Fatalf("chunk %d game %d: seat order %d,%d does not alternate", chunk, g, a, b)
			}
			seat0[a]++
			games[a]++
			games[b]++
			if x, y := fixedPair(names, chunk, g, 8); x != a || y != b {
				differ++
			}
		}
	}
	if differ == 0 {
		t.Errorf("the seed changes no pair")
	}
	for i := range names {
		if games[i] == 0 {
			t.Fatalf("deck %d never dealt", i)
		}
		if f := float64(seat0[i]) / float64(games[i]); f < 0.4 || f > 0.6 {
			t.Errorf("deck %d in seat 0 in %d of %d games", i, seat0[i], games[i])
		}
	}
	// Two decks: every game is the pair, seats alternating.
	for g := 0; g < 4; g++ {
		a, b := fixedPair([]string{"X", "Y"}, 0, g, 1)
		if want := g % 2; a != want || b != 1-want {
			t.Errorf("two decks, game %d: %d,%d", g, a, b)
		}
	}
}

func deckJSON(t *testing.T, name string, extra string, cards map[string]int) string {
	t.Helper()
	type entry struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	var es []entry
	for _, n := range sortedKeys(cards) {
		es = append(es, entry{n, cards[n]})
	}
	b, err := json.Marshal(es)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`{"name": %q, "format": "custom"%s, "cards": %s, "sideboard": [{"name": "Not A Sideboard Card", "count": 3}]}`, name, extra, b)
}

// unsupportedCard is a corpus card gorge does not fully support: the first
// "unsupported" skip of a set's manifest.
func unsupportedCard(t *testing.T, reg *cards.Registry) string {
	t.Helper()
	for _, set := range []string{"FDN", "BLB", "DSK", "DFT", "TDM", "ALL", "APC"} {
		_, skips, err := decks.Candidates(reg, manifestDir, set)
		if err != nil {
			continue
		}
		for _, s := range skips {
			if strings.HasPrefix(s.Reason, "unsupported") {
				return s.Card
			}
		}
	}
	t.Skip("no unsupported card in the probed manifests")
	return ""
}

// loadDeckDir keeps the playable decks sorted by name, ignores sideboards,
// and skips with a reason a deck naming an unknown or unsupported card, a
// Commander deck, a deck XMage spells a card of otherwise (with -xmage) and a
// second file of a name.
func TestLoadDeckDir(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	if !sp.Differs(reg, "Fire") {
		t.Fatal(`XMage spells "Fire" as the corpus does; pick another probe`)
	}
	dir := t.TempDir()
	red := map[string]int{"Mountain": 40, "Shock": 20}
	writeFile(t, dir, "b-red.json", deckJSON(t, "Zed Red", "", red))
	writeFile(t, dir, "a-green.json", deckJSON(t, "Green", "", map[string]int{"Forest": 36, "Llanowar Elves": 24}))
	writeFile(t, dir, "c-unknown.json", deckJSON(t, "Unknown", "", map[string]int{"Mountain": 56, "Not A Real Card": 4}))
	writeFile(t, dir, "d-unsup.json", deckJSON(t, "Unsup", "", map[string]int{"Mountain": 56, unsupportedCard(t, reg): 4}))
	writeFile(t, dir, "e-cmdr.json", deckJSON(t, "Cmdr", `, "commander": "Llanowar Elves"`, map[string]int{"Forest": 99, "Llanowar Elves": 1}))
	writeFile(t, dir, "f-fire.json", deckJSON(t, "Fire Deck", "", map[string]int{"Mountain": 56, "Fire": 4}))
	writeFile(t, dir, "g-dup.json", deckJSON(t, "Zed Red", "", red))
	writeFile(t, dir, "h-bad.json", `{"name": "Empty", "cards": []}`)
	writeFile(t, dir, "notes.txt", "not a deck")

	ds, info, err := loadDeckDir(reg, dir, sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 || ds[0].Name != "Green" || ds[1].Name != "Zed Red" {
		t.Fatalf("playable = %+v", ds)
	}
	if len(ds[1].Cards) != 60 || ds[1].Colours != "R" || ds[0].Colours != "G" {
		t.Errorf("Zed Red: %d cards, colours %q; Green colours %q", len(ds[1].Cards), ds[1].Colours, ds[0].Colours)
	}
	for _, n := range ds[1].Cards {
		if n == "Not A Sideboard Card" {
			t.Errorf("the sideboard was dealt")
		}
	}
	want := map[string]string{
		"a-green.json": "", "b-red.json": "", "c-unknown.json": "not_in_corpus Not A Real Card",
		"d-unsup.json": "unsupported", "e-cmdr.json": "commander", "f-fire.json": "xmage_spelling Fire",
		"g-dup.json": "duplicate_name of b-red.json", "h-bad.json": "parse",
	}
	if len(info.Decks) != len(want) {
		t.Fatalf("decks.json lists %d decks, want %d: %+v", len(info.Decks), len(want), info.Decks)
	}
	for _, e := range info.Decks {
		w, ok := want[e.File]
		if !ok || (w == "") != (e.Skip == "") || !strings.HasPrefix(e.Skip, w) {
			t.Errorf("%s: skip %q, want %q", e.File, e.Skip, w)
		}
		if len(e.SHA256) != 64 {
			t.Errorf("%s: sha256 %q", e.File, e.SHA256)
		}
	}
	wantSkipped := map[string]int{"not_in_corpus": 1, "unsupported": 1, "commander": 1, "xmage_spelling": 1, "duplicate_name": 1, "parse": 1}
	if fmt.Sprint(info.Skipped) != fmt.Sprint(wantSkipped) || info.Playable != 2 {
		t.Errorf("skipped %v playable %d, want %v and 2", info.Skipped, info.Playable, wantSkipped)
	}
	if s := info.String(); !strings.Contains(s, "8 decks, 2 playable") || !strings.Contains(s, "1 commander") {
		t.Errorf("summary %q", s)
	}

	// Without -xmage the Fire deck plays.
	if ds, _, err := loadDeckDir(reg, dir, nil); err != nil || len(ds) != 3 {
		t.Errorf("without -xmage: %d decks, %v; want 3", len(ds), err)
	}

	// Fewer than two playable decks is an error.
	one := t.TempDir()
	writeFile(t, one, "red.json", deckJSON(t, "Red", "", red))
	writeFile(t, one, "unknown.json", deckJSON(t, "Unknown", "", map[string]int{"Not A Real Card": 60}))
	if _, _, err := loadDeckDir(reg, one, nil); err == nil || !strings.Contains(err.Error(), "need 2") {
		t.Errorf("one playable deck: %v, want the need-2 error", err)
	}
	if _, _, err := loadDeckDir(reg, t.TempDir(), nil); err == nil {
		t.Errorf("an empty dir loaded")
	}
}

// A -decks run plays the fixed decks, 60 cards each, writes decks.json and
// credits the ledger.
func TestPlayFixedDecks(t *testing.T) {
	testutil.CorpusRegistry(t) // skips on a checkout with no corpus
	in := t.TempDir()
	writeFile(t, in, "red.json", deckJSON(t, "Red", "", map[string]int{"Mountain": 40, "Shock": 20}))
	writeFile(t, in, "green.json", deckJSON(t, "Green", "", map[string]int{"Forest": 36, "Llanowar Elves": 24}))
	out := t.TempDir()
	if err := runPlay([]string{"-set", "STD", "-decks", in, "-games", "2", "-cards", "../../.cards", "-manifests", manifestDir, "-out", out}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "decks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var info decksInfo
	if err := json.Unmarshal(b, &info); err != nil || info.Playable != 2 || info.Dir != in {
		t.Fatalf("decks.json: %+v, %v", info, err)
	}
	tapes, _ := filepath.Glob(filepath.Join(out, "tapes", "*.json"))
	if len(tapes) == 0 {
		t.Fatal("no tapes")
	}
	tb, err := os.ReadFile(tapes[0]) // game 0's: gameSeed grows with g
	if err != nil {
		t.Fatal(err)
	}
	var tp struct {
		Decks [2]struct {
			Name  string   `json:"name"`
			Cards []string `json:"cards"`
		} `json:"decks"`
	}
	if err := json.Unmarshal(tb, &tp); err != nil {
		t.Fatal(err)
	}
	if tp.Decks[0].Name != "Green" || tp.Decks[1].Name != "Red" || len(tp.Decks[0].Cards) != 60 || len(tp.Decks[1].Cards) != 60 {
		t.Errorf("game 0 decks %s (%d), %s (%d)", tp.Decks[0].Name, len(tp.Decks[0].Cards), tp.Decks[1].Name, len(tp.Decks[1].Cards))
	}
	led, err := os.ReadFile(filepath.Join(out, "exercised.tsv"))
	if err != nil || !strings.Contains(string(led), "Shock") {
		t.Errorf("exercised.tsv does not credit Shock: %v", err)
	}
}

// TestDecksRefusesCommander: -decks and -format commander are both whole deck
// sources, so play refuses the pair before the corpus loads.
func TestDecksRefusesCommander(t *testing.T) {
	for _, args := range [][]string{
		{"-set", "STD", "-decks", t.TempDir(), "-format", "commander", "-commander-decks", t.TempDir(), "-out", t.TempDir()},
		{"-set", "STD", "-decks", t.TempDir(), "-commander-decks", t.TempDir(), "-out", t.TempDir()},
	} {
		err := runPlay(args)
		if err == nil || !strings.Contains(err.Error(), "-decks plays fixed constructed lists") {
			t.Errorf("runPlay(%v) = %v, want the -decks/-format commander refusal", args, err)
		}
	}
}
