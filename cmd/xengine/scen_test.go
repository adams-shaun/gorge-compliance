package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// forgeEditionsFixture is synthetic editions in Forge's format, not a copy of
// Forge's (GPL) editions: it names the three Mystery Booster 2 playtest cards
// and a handful of legal cards, enough to prove the command wires the filter.
const forgeEditionsFixture = "../../oracle/xengine/certdeck/testdata/editions"

func scenArgs(extra ...string) []string {
	return append([]string{"-cards", "../../.cards", "-manifests", manifestDir, "-forge-editions", forgeEditionsFixture}, extra...)
}

func TestScenCLI(t *testing.T) {
	testutil.CorpusRegistry(t) // skips on a checkout with no corpus
	out := t.TempDir()
	if err := runScen(scenArgs("-card", "Thornvault Forager", "-key", "f0/a2", "-prefer", "start", "-set", "BLB", "-out", out)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "thornvault-forager-f0a2.start.json"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := scen.DecodeStartFile(raw)
	if err != nil {
		t.Fatalf("the start file does not decode strictly: %v\n%s", err, raw)
	}
	if f.Card != "Thornvault Forager" || f.Key != "f0/a2" || f.Focus == nil || f.Focus.Kind != "ability" || f.Focus.Ability != 2 {
		t.Errorf("start file: %+v focus %+v", f, f.Focus)
	}
	// XP3's playStart (cmd/xengine/start.go) is not on this base: when it lands,
	// lower this file with it and assert 0 refusals (see the report).

	auto := t.TempDir()
	if err := runScen(scenArgs("-card", "Llanowar Elves", "-key", "f0/a0", "-set", "FDN", "-out", auto)); err != nil {
		t.Fatal(err)
	}
	line, err := os.ReadFile(filepath.Join(auto, "llanowar-elves-f0a0.steps.jsonl"))
	if err != nil || !strings.Contains(string(line), `"id":"xs:FDN:llanowar-elves:f0a0:steps"`) || strings.Count(string(line), "\n") != 1 {
		t.Errorf("steps item: %v %s", err, line)
	}

	ref := t.TempDir()
	if err := runScen(scenArgs("-card", "Aggressive Mammoth", "-key", "static#0.0", "-prefer", "start", "-out", ref)); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(ref); len(ents) != 0 {
		t.Errorf("a refusal wrote %d files", len(ents))
	}
	if err := runScen(scenArgs("-card", "Llanowar Elves", "-key", "f9/a9", "-out", ref)); err == nil {
		t.Error("an unknown key was accepted")
	}
}

func TestScenCensusFDN(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	out := t.TempDir()
	if err := runScen(scenArgs("-census", "-set", "FDN", "-out", out)); err != nil {
		t.Fatal(err)
	}
	cands, _, err := decks.Candidates(reg, manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, c := range cands {
		if card, ok := reg.Lookup(c.Name); ok {
			want += len(scen.Keys(card))
		}
	}
	reach := scenTSVRows(t, filepath.Join(out, "reach.tsv"))
	if len(reach) != want || want == 0 {
		t.Errorf("reach.tsv has %d rows, want one per key (%d)", len(reach), want)
	}
	sum := 0
	for _, r := range scenTSVRows(t, filepath.Join(out, "summary.tsv")) {
		n, err := strconv.Atoi(r[2])
		if err != nil {
			t.Fatal(err)
		}
		sum += n
	}
	if sum != len(reach) {
		t.Errorf("summary.tsv counts %d keys, reach.tsv has %d rows", sum, len(reach))
	}
}

// scenTSVRows is a tsv file's rows without its header line.
func scenTSVRows(t *testing.T, path string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	var rows [][]string
	for _, l := range lines[1:] {
		rows = append(rows, strings.Split(l, "\t"))
	}
	return rows
}

// TestScenForgeFilteredStarts is the pinned repro: the four xproof focus cards
// whose starts held a Mystery Booster 2 playtest card the Forge card DB filters
// (Indicate for Remove Soul and Deflection, Mox Poison for Retract, Gobland for
// Devoted Retainer). The filtered pool must make each start name some other
// enabler, or none.
func TestScenForgeFilteredStarts(t *testing.T) {
	testutil.CorpusRegistry(t)
	out := t.TempDir()
	filtered := map[string]bool{"Indicate": true, "Mox Poison": true, "Gobland": true}
	for _, card := range []string{"Remove Soul", "Deflection", "Retract", "Devoted Retainer"} {
		if err := runScen(scenArgs("-card", card, "-key", "cast", "-set", "XP", "-out", out)); err != nil {
			t.Fatalf("%s: %v", card, err)
		}
		raw, err := os.ReadFile(filepath.Join(out, scen.Slug(card)+"-cast.start.json"))
		if err != nil {
			t.Fatalf("%s: %v", card, err)
		}
		f, err := scen.DecodeStartFile(raw)
		if err != nil {
			t.Fatalf("%s: %v\n%s", card, err, raw)
		}
		lists := map[string][]string{
			"p0 hand": f.Setup.P0.Hand, "p0 battlefield": f.Setup.P0.Battlefield,
			"p0 graveyard": f.Setup.P0.Graveyard, "p0 exile": f.Setup.P0.Exile, "p0 library": f.Setup.P0.Library,
			"p1 hand": f.Setup.P1.Hand, "p1 battlefield": f.Setup.P1.Battlefield,
			"p1 graveyard": f.Setup.P1.Graveyard, "p1 exile": f.Setup.P1.Exile, "p1 library": f.Setup.P1.Library,
		}
		for where, names := range lists {
			for _, name := range names {
				if filtered[name] {
					t.Errorf("%s start names %q in %s; the Forge card DB filters it", card, name, where)
				}
			}
		}
	}
}
