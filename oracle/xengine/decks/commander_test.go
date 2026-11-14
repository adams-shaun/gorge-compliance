package decks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/deck"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func repoFiles(t *testing.T, stems ...string) []CommanderFile {
	t.Helper()
	var out []CommanderFile
	for _, s := range stems {
		f, err := testutil.LoadRepoDeckFile(s)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, CommanderFile{Stem: s, File: f})
	}
	return out
}

// The kept-deck path needs effects.Supported filled by engine/rules, which
// this package's tests must not link (TestPairableAllDeclaredSets' counts
// assume it is not); TestCommanderDecksFilter in ../game covers it.
func TestCommanderDecksSkipsBeforeSupport(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	_, skips := CommanderDecks(reg, repoFiles(t, "mono-red-goblins"))
	if want := []Skip{{"mono-red-goblins", SkipNotCommander}}; !reflect.DeepEqual(skips, want) {
		t.Errorf("skips = %v, want %v", skips, want)
	}
	if d := (Deck{Name: "x", Cards: []string{"a", "b"}, Commander: "b"}); d.CommanderIndex() != 1 {
		t.Errorf("CommanderIndex = %d", d.CommanderIndex())
	}
	if (Deck{Cards: []string{"a"}}).CommanderIndex() != -1 || (Deck{Cards: []string{"a"}, Commander: "z"}).CommanderIndex() != -1 {
		t.Error("CommanderIndex of an absent commander is not -1")
	}
}

func TestCommanderDecksPartnerSkip(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	fs := repoFiles(t, "foundations-wretched-ranks")
	fs[0].File.Commanders = []string{"Ghoulcaller Gisa", "Ghalta, Primal Hunger"}
	ds, skips := CommanderDecks(reg, fs)
	if len(ds) != 0 || len(skips) != 1 || skips[0].Reason != SkipPartner {
		t.Errorf("got %v, %v", ds, skips)
	}
}

func TestCommanderPairWalksMatchups(t *testing.T) {
	ds := []Deck{{Name: "a", Cards: []string{"x"}}, {Name: "b", Cards: []string{"y"}}, {Name: "c", Cards: []string{"z"}}}
	seen := map[string]bool{}
	for g := 0; g < 6; g++ {
		p := CommanderPair(ds, "S", 0, g, 0)
		if !reflect.DeepEqual(p, CommanderPair(ds, "S", 0, g, 0)) {
			t.Errorf("game %d not deterministic", g)
		}
		for seat := 0; seat < 2; seat++ {
			pre := fmt.Sprintf("S-c0-g%d-%d-", g, seat)
			if !strings.HasPrefix(p[seat].Name, pre) {
				t.Errorf("name %q lacks prefix %q", p[seat].Name, pre)
			}
		}
		a, b := strings.TrimPrefix(p[0].Name, fmt.Sprintf("S-c0-g%d-0-", g)), strings.TrimPrefix(p[1].Name, fmt.Sprintf("S-c0-g%d-1-", g))
		if a == b {
			t.Errorf("game %d plays %s against itself", g, a)
		}
		seen[a+b] = true
	}
	if len(seen) != 6 {
		t.Errorf("6 games gave %d distinct matchups: %v", len(seen), seen)
	}
}

func TestCommanderPairPanicsOnOneDeck(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic")
		}
	}()
	CommanderPair([]Deck{{Name: "a"}}, "S", 0, 0, 0)
}

func TestReadCommanderDir(t *testing.T) {
	dir := t.TempDir()
	for _, s := range []string{"foundations-wretched-ranks", "foundations-tramplesaurus-rex", "mono-red-goblins"} {
		f, err := testutil.LoadRepoDeckFile(s)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := deck.Parse(raw); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, s+".json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fs, err := ReadCommanderDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var stems []string
	for _, f := range fs {
		stems = append(stems, f.Stem)
		if len(f.SHA256) != 64 {
			t.Errorf("%s: sha256 %q", f.Stem, f.SHA256)
		}
	}
	if want := []string{"foundations-tramplesaurus-rex", "foundations-wretched-ranks", "mono-red-goblins"}; !reflect.DeepEqual(stems, want) {
		t.Errorf("stems = %v, want %v", stems, want)
	}
	if _, err := ReadCommanderDir(t.TempDir()); err == nil {
		t.Error("empty dir: no error")
	}
}

func TestSwapRefused(t *testing.T) {
	ds := []Deck{
		{Name: "rg", Colours: "RG", Commander: "Cmdr", Cards: []string{"Cmdr", "Bad", "Bad2", "Good", "Bad", "Forest"}},
		{Name: "c", Colours: "C", Commander: "X", Cards: []string{"X", "Bad"}},
	}
	plays := func(n string) bool { return n != "Bad" && n != "Bad2" && n != "Cmdr" }
	got, swaps := SwapRefused(ds, plays)
	want := []string{"Forest", "Forest", "Good", "Cmdr", "Mountain", "Mountain"}
	sort.Strings(want)
	if !reflect.DeepEqual(got[0].Cards, want) || got[0].Commander != "Cmdr" {
		t.Errorf("rg cards %v", got[0].Cards)
	}
	wantSw := []Swap{{"rg", "Bad", "Mountain", 1}, {"rg", "Bad", "Forest", 1}, {"rg", "Bad2", "Mountain", 1}}
	if !reflect.DeepEqual(swaps, wantSw) {
		t.Errorf("swaps %v want %v", swaps, wantSw)
	}
	if !reflect.DeepEqual(got[1].Cards, []string{"Bad", "X"}) {
		t.Errorf("colourless deck changed: %v", got[1].Cards)
	}
	if ds[0].Cards[1] != "Bad" {
		t.Error("input modified")
	}
	again, swaps2 := SwapRefused(ds, plays)
	if !reflect.DeepEqual(again, got) || !reflect.DeepEqual(swaps, swaps2) {
		t.Error("not deterministic")
	}
}
