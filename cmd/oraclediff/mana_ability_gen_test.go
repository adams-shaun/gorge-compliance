package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestGenManaAbilitiesSendsActivation: gen -mana-abilities writes an
// activation item for every named mana source, the no-mana-cost Sol
// Talisman included, and the follower request for it carries the
// neutral.ability entry a driver's mana-source mapping reads.
func TestGenManaAbilitiesSendsActivation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	names := []string{
		"The Alright Henge",
		"Torgal, A Fine Hound",
		"Sol Talisman",
		"Riven Turnbull",
		"Princess Lucrezia",
	}
	m := compliance.Manifest{Code: "POOL"}
	for _, name := range names {
		m.Cards = append(m.Cards, compliance.ManifestCard{Name: name})
	}
	out := filepath.Join(t.TempDir(), "gen.jsonl")
	if err := genManifestOpts(reg, m, out, "A", true); err != nil {
		t.Fatalf("genManifestOpts: %v", err)
	}
	byCard := map[string][]oraclegen.Item{}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var it oraclegen.Item
		if err := json.Unmarshal([]byte(line), &it); err != nil {
			t.Fatalf("item line %q: %v", line, err)
		}
		byCard[it.Card] = append(byCard[it.Card], it)
	}
	for _, name := range names {
		items := byCard[name]
		var act *oraclegen.Item
		for i := range items {
			if items[i].Template == "activate-mana" {
				act = &items[i]
			}
		}
		if act == nil {
			t.Fatalf("%s: no activate-mana item (got %v)", name, templatesOf(items))
		}
		if len(act.Steps) == 0 || act.Steps[0].Op != "activate" || act.Steps[0].AbilityIndex == nil {
			t.Fatalf("%s: activation item steps %+v", name, act.Steps)
		}
		if !containsName(act.Setup["p0"].Battlefield, name) {
			t.Fatalf("%s: not on the battlefield: %+v", name, act.Setup["p0"])
		}
		rq, g := forgeExportItem(reg, *act, false)
		n := neutralFor(reg, rq, g, census{})
		a, ok := n.Ability["0"]
		if !ok || a.Class != "mana" {
			t.Fatalf("%s: neutral.ability[0] = %+v (ok %v), want a mana entry", name, a, ok)
		}
		if a.Ordinal == nil {
			t.Fatalf("%s: neutral.ability[0] has no ordinal", name)
		}
	}
	// A card with a mana cost keeps its cast-resolve item beside the new
	// activation item; Sol Talisman has no cast item at all.
	if len(byCard["Torgal, A Fine Hound"]) != 2 {
		t.Errorf("Torgal: %v, want cast-resolve + activate-mana", templatesOf(byCard["Torgal, A Fine Hound"]))
	}
	if ts := templatesOf(byCard["Sol Talisman"]); len(ts) != 1 || ts[0] != "activate-mana" {
		t.Errorf("Sol Talisman: %v, want only activate-mana", ts)
	}
	skips, err := os.ReadFile(out + ".skips.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(skips), "no mana cost") {
		t.Errorf("skips still name no mana cost: %s", skips)
	}
}

// TestGenArbitraryManifestUsesCommittedXMageNames: gen's CLI (runGenCLI) passes the
// committed manifests dir to installXMageKnown even when -manifest points
// outside it. A follower-check synthetic manifest lives in an arbitrary
// output directory; parsing that directory as an XMage set list failed on an
// empty manifest (no names) or a sibling non-manifest JSON.
func TestGenArbitraryManifestUsesCommittedXMageNames(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "EMPTY.json"), []byte(`{"code":"EMPTY","cards":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "sibling.json"), []byte("not a manifest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if committedManifestDir() == "" {
		t.Skip("no repository root")
	}
	out := filepath.Join(tmp, "gen.jsonl")
	// The CLI entry point itself, so a regression that stops passing the
	// committed manifests dir (an "" known dir parses tmp as a set list) fails.
	args := []string{"-cards", filepath.Join("..", "..", ".cards"), "-manifest", filepath.Join(tmp, "EMPTY.json"), "-out", out, "-level", "A"}
	if err := runGenCLI(args); err != nil {
		t.Fatalf("gen with an arbitrary manifest dir: %v", err)
	}
}

// TestGenManaAbilityGapIsWrittenBesideBase: a real mana source whose
// activation the activate family cannot build (Millikin's Mill cost) keeps
// its cast item and still writes a skip naming the activation gap. The skip
// used to be dropped whenever the base cast item existed, so follower-check
// saw only cast-resolve and reported AGREE.
func TestGenManaAbilityGapIsWrittenBesideBase(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	m := compliance.Manifest{Code: "GAP", Cards: []compliance.ManifestCard{{Name: "Millikin"}}}
	out := filepath.Join(t.TempDir(), "gen.jsonl")
	if err := genManifestOpts(reg, m, out, "A", true); err != nil {
		t.Fatalf("genManifestOpts: %v", err)
	}
	items := genItems(t, out)
	if len(items) != 1 || items[0].Template != "cast-resolve" {
		t.Fatalf("Millikin: %v, want only the cast-resolve item", templatesOf(items))
	}
	raw, err := os.ReadFile(out + ".skips.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "activate cost gap") {
		t.Fatalf("Millikin: skips %q do not name the activation cost gap", raw)
	}
}

// TestGenUnknownManaNameIsSkipped: a pool name the corpus does not know
// produces no item and a "not in corpus" skip, which follower-check turns
// into an error instead of an AGREE row.
func TestGenUnknownManaNameIsSkipped(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	m := compliance.Manifest{Code: "TYPO", Cards: []compliance.ManifestCard{{Name: "Sol Talismann"}}}
	out := filepath.Join(t.TempDir(), "gen.jsonl")
	if err := genManifestOpts(reg, m, out, "A", true); err != nil {
		t.Fatalf("genManifestOpts: %v", err)
	}
	if items := genItems(t, out); len(items) != 0 {
		t.Fatalf("Sol Talismann: %v, want no items", templatesOf(items))
	}
	raw, err := os.ReadFile(out + ".skips.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "not in corpus") {
		t.Fatalf("Sol Talismann: skips %q, want not in corpus", raw)
	}
}

func genItems(t *testing.T, path string) []oraclegen.Item {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []oraclegen.Item
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var it oraclegen.Item
		if err := json.Unmarshal([]byte(line), &it); err != nil {
			t.Fatalf("item line %q: %v", line, err)
		}
		out = append(out, it)
	}
	return out
}

func templatesOf(items []oraclegen.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Template)
	}
	return out
}

func containsName(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
