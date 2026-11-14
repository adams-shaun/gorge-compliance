package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestFormatFlagErr(t *testing.T) {
	for _, c := range []struct {
		name                       string
		format, dir, random, forge string
		pool, poolFilter           string
		dirac                      bool
		want                       string // "" = no error
	}{
		{name: "none"},
		{name: "constructed", format: "constructed"},
		{name: "commander", format: "commander", dir: "d"},
		{name: "no dir", format: "commander", want: "needs -commander-decks"},
		{name: "no format", dir: "d", want: "needs -format commander"},
		{name: "bogus", format: "bogus", want: "want constructed or commander"},
		{name: "random", format: "commander", dir: "d", random: "FDN", forge: "f", want: "-random, -forge-only"},
		{name: "pool", format: "commander", dir: "d", pool: "p.txt", want: "-pool"},
		{name: "pool filter", format: "commander", dir: "d", poolFilter: "f.txt", want: "-pool-filter"},
		{name: "dirac", format: "commander", dir: "d", dirac: true},
	} {
		err := formatFlagErr(c.format, c.dir, c.random, c.forge, c.pool, c.poolFilter, c.dirac)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

// The flags are refused before any corpus load: -cards names nothing.
func TestRunPlayCommanderConflict(t *testing.T) {
	err := runPlay([]string{"-set", "CMDR", "-cards", "/nonexistent", "-format", "commander",
		"-commander-decks", t.TempDir(), "-random", "FDN"})
	if err == nil || !strings.Contains(err.Error(), "-random") {
		t.Fatalf("err = %v, want one naming -random", err)
	}
}

func TestPlayCommanderGame(t *testing.T) {
	src := t.TempDir()
	for _, s := range []string{"foundations-tramplesaurus-rex", "foundations-wretched-ranks", "mono-red-goblins"} {
		f, err := testutil.LoadRepoDeckFile(s)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, s+".json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := t.TempDir()
	if err := runPlay([]string{"-cards", "../../.cards", "-set", "CMDR", "-chunk", "0", "-games", "1", "-seed", "1",
		"-windows", "-format", "commander", "-commander-decks", src, "-out", out}); err != nil {
		t.Fatal(err)
	}
	var info commanderInfo
	b, err := os.ReadFile(filepath.Join(out, "commander.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &info); err != nil {
		t.Fatal(err)
	}
	if len(info.Decks) != 2 || len(info.Skipped) != 1 || info.Skipped[0].Reason != decks.SkipNotCommander {
		t.Fatalf("commander.json = %+v", info)
	}
	for _, d := range info.Decks {
		if d.Commander == "" || len(d.SHA256) != 64 {
			t.Errorf("deck %+v", d)
		}
	}
	// The lowering does not place a Commander game yet (it reports the 40-life
	// tape as a lower_gap), so items.jsonl may be empty; any item it does hold
	// must say commander.
	if err := readLines(filepath.Join(out, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		if it.Format != "commander" {
			t.Errorf("item %s format = %q", it.ID, it.Format)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tapes, _ := filepath.Glob(filepath.Join(out, "tapes", "*.json"))
	if len(tapes) == 0 {
		t.Fatal("no tape written")
	}
	raw, err := os.ReadFile(filepath.Join(out, "tapes", filepath.Base(tapes[0])))
	if err != nil {
		t.Fatal(err)
	}
	var tp game.Tape
	if err := json.Unmarshal(raw, &tp); err != nil {
		t.Fatal(err)
	}
	if tp.GameFormat != game.GameFormatCommander {
		t.Errorf("tape game_format = %q", tp.GameFormat)
	}
}

func TestPickDealerMatchesPair(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cands, _, err := decks.Candidates(reg, manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	led := cov.New()
	pick := func(int, uint64) ([]decks.Card, *cov.Ledger, string) { return cands, led, "" }
	deal := pickDealer(pick, "FDN", 0, 1)
	for g := 0; g < 4; g++ {
		got, table := deal(g)
		if want := decks.Pair(cands, led, "FDN", 0, g, 1); !reflect.DeepEqual(got, want[:]) || table != "" {
			t.Errorf("game %d: pair differs", g)
		}
	}
}
