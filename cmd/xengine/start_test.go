package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestPlayStartThornvault(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	out := t.TempDir()
	if err := playStart(reg, []string{"testdata/start"}, "BLB", out, false); err != nil {
		t.Fatal(err)
	}
	var ids []string
	activated := map[string]bool{}
	if err := readLines(filepath.Join(out, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		ids = append(ids, it.ID)
		bf := it.Setup["p0"].Battlefield
		forests := 0
		for _, n := range bf {
			if n == "Forest" {
				forests++
			}
		}
		if !slices.Contains(bf, "Thornvault Forager") || forests != 4 {
			t.Errorf("%s: p0 battlefield %v, want Thornvault Forager and 4 Forest", it.ID, bf)
		}
		for _, s := range it.Steps {
			// not focus evidence: the explore bot activates Forager on its own in this
			// fixture; the focus is covered by oracle/xengine/game TestFocus*
			if s.Op == "activate" && strings.HasSuffix(s.Card, ":Thornvault Forager") {
				activated[it.ID] = true
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"xs:BLB:thornvault-forager:f0a2:s1", "xs:BLB:thornvault-forager:f0a2:s2"}
	if !slices.Equal(ids, want) {
		t.Fatalf("items %v, want %v", ids, want)
	}
	for _, id := range ids {
		if !activated[id] {
			t.Errorf("%s: no step activates Thornvault Forager", id)
		}
	}
	n := 0
	if err := readLines(filepath.Join(out, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		n++
		if r.Status != "lower_ok" {
			t.Errorf("%s: status %s %v, want lower_ok", r.ID, r.Status, r.Reasons)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("lower.jsonl has %d rows, want 2", n)
	}
	xprepPlayDir(t, reg, out)
}

func TestPlayStartFlagsExclusive(t *testing.T) {
	for _, f := range []string{"-windows", "-random=BLB", "-pool=x", "-decks=x", "-dirac", "-tape-turns=1", "-format=commander", "-forge-only=x"} {
		err := runPlay([]string{"-set", "BLB", "-start", "testdata/start", "-out", t.TempDir(), f})
		if err == nil || !strings.Contains(err.Error(), "-start") {
			t.Errorf("-start with %s: err %v, want a -start error", f, err)
		}
	}
	if err := runPlay([]string{"-start", "testdata/start", "-out", t.TempDir()}); err == nil {
		t.Error("-start without -set: no error")
	}
}

// TestPlayStartBezaSetup plays the Beza start as scen writes it (the forced
// start: opp-burn as p0's life, two tapped Plains for the lands need,
// xp-setup-extra-hand-card) and holds the lowered setup to the start file:
// each seat's hand as listed, 40 cards in all, and the lowering agrees with
// gorge's own runner.
func TestPlayStartBezaSetup(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	out := t.TempDir()
	if err := playStart(reg, []string{"testdata/start-beza"}, "XP", out, false); err != nil {
		t.Fatal(err)
	}
	n := 0
	if err := readLines(filepath.Join(out, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		n++
		p0, p1 := it.Setup["p0"], it.Setup["p1"]
		if want := []string{"Beza, the Bounding Spring"}; !slices.Equal(p0.Hand, want) {
			t.Errorf("%s: p0 hand %v, want %v", it.ID, p0.Hand, want)
		}
		if len(p1.Hand) != 0 {
			t.Errorf("%s: p1 hand %v, want none", it.ID, p1.Hand)
		}
		if len(p0.Battlefield) != 7 || len(p0.LibraryTop) != 32 || p0.Life == nil || *p0.Life != 17 {
			t.Errorf("%s: p0 battlefield %d library %d life %v, want 7, 32 and 17", it.ID, len(p0.Battlefield), len(p0.LibraryTop), p0.Life)
		}
		if len(p1.Battlefield) != 0 || len(p1.LibraryTop) != 40 {
			t.Errorf("%s: p1 battlefield %v library %d, want none and 40", it.ID, p1.Battlefield, len(p1.LibraryTop))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("items.jsonl has %d rows, want 1", n)
	}
	if err := readLines(filepath.Join(out, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		if r.Status != "lower_ok" {
			t.Errorf("%s: status %s %v, want lower_ok", r.ID, r.Status, r.Reasons)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestStartDeckRefusesFieldBlocked: a start file listing an instant, a
// sorcery or an Aura on the battlefield is refused (scen.FieldBlock), since
// gorge and Forge place it differently and the setup could never agree.
func TestStartDeckRefusesFieldBlocked(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, n := range []string{"Path to Exile", "Taste of Blood", "Strange Augmentation"} {
		_, _, err := startDeck(reg, "start-p0", startSeat{Battlefield: []string{"Plains", n}})
		if err == nil || !strings.Contains(err.Error(), "cannot be listed on the battlefield") {
			t.Errorf("%s on the battlefield: err %v, want a refusal", n, err)
		}
		if _, _, err := startDeck(reg, "start-p0", startSeat{Hand: []string{n}}); err != nil {
			t.Errorf("%s in hand: %v", n, err)
		}
	}
}

// TestStartSeedBase: seed_base moves where a start file's seed search
// starts, so a retried key plays new games; absent, the games are those of
// seed_base 1, as before the field.
func TestStartSeedBase(t *testing.T) {
	dir := t.TempDir()
	write := func(name, extra string) *startFile {
		t.Helper()
		p := filepath.Join(dir, name)
		body := `{"format":"xproof-start-1","card":"Llanowar Elves","key":"f0/a0","players":2,"active":0,` +
			`"max_turns":1,"setup":{"p0":{"battlefield":["Llanowar Elves"]},"p1":{}},"filler":"basics","seeds":3` + extra + `}`
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		f, err := readStartFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	def, one, later := write("a.json", ""), write("b.json", `,"seed_base":1`), write("c.json", `,"seed_base":1000`)
	d, o, l := def.gameSeeds(), one.gameSeeds(), later.gameSeeds()
	if !slices.Equal(d, o) {
		t.Errorf("absent seed_base seeds %v, seed_base 1 seeds %v: want equal", d, o)
	}
	if len(d) != 3 || len(l) != 3 {
		t.Fatalf("seeds %v / %v, want 3 each", d, l)
	}
	for i, s := range l {
		if s < 1000 || (i > 0 && s <= l[i-1]) || rules.SeedForStarter(s, 2, 0) != s {
			t.Errorf("seed_base 1000 seeds %v: want increasing, >= 1000, each starting seat 0", l)
		}
		if slices.Contains(d, s) {
			t.Errorf("seed_base 1000 replays seed %d of the default %v", s, d)
		}
	}
}

// TestStartCounterBearer: the counters a start seeds follow the ability that
// reads them. Honest Work enchants a creature an opponent controls (the p1
// Kobolds of Kher Keep certdeck already places), and Mabel's removal targets
// another creature or planeswalker; both counter needs are P1P1, not the
// literal All/Any the followers refuse, and sit on the opponent's creature.
func TestStartCounterBearer(t *testing.T) {
	reg, b := certdeckBuilder(t)
	for _, tc := range []struct {
		card, key string
		seat      int
		on        string
	}{
		{"Honest Work", "f0/t0", 1, "Kobolds of Kher Keep"},
		{"Mabel, Bitter Recluse", "f0/t0", 1, "Shifting Wall"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			res, err := scen.Generate(reg, b, tc.card, tc.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != scen.PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			seats := [2]scen.StartSeat{res.Start.Setup.P0, res.Start.Setup.P1}
			counters := seats[tc.seat].Counters
			if len(counters) != 1 {
				t.Fatalf("seat %d counters %+v, want one", tc.seat, counters)
			}
			got := counters[0]
			if got.Kind != "P1P1" {
				t.Errorf("counter kind %q, want P1P1", got.Kind)
			}
			if got.Card != tc.on {
				t.Errorf("counter bearer %q, want %q", got.Card, tc.on)
			}
			if !slices.Contains(seats[tc.seat].Battlefield, got.Card) {
				t.Errorf("counter bearer %q is not on seat %d's battlefield %v", got.Card, tc.seat, seats[tc.seat].Battlefield)
			}
			c, ok := reg.Lookup(got.Card)
			if !ok || len(c.Faces) == 0 || !c.Faces[0].IsCreature() {
				t.Errorf("counter bearer %q is not a creature", got.Card)
			}
			if other := seats[1-tc.seat].Counters; len(other) != 0 {
				t.Errorf("seat %d counters %+v, want none", 1-tc.seat, other)
			}
		})
	}
}

// TestStartCounterKindRefused: a start file that names a wildcard counter kind
// ("All", "Any") is refused at validation, so the class of bug Honest Work
// exposed cannot come back silently through a hand-written start.
func TestStartCounterKindRefused(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, kind := range []string{"All", "Any", "", "EachFromSource"} {
		_, _, err := startDeck(reg, "start-p0", startSeat{
			Battlefield: []string{"Wall of Stone"},
			Counters:    []startCounter{{Card: "Wall of Stone", Kind: kind, N: 1}},
		})
		if err == nil || !strings.Contains(err.Error(), "is not a counter kind") {
			t.Errorf("counter kind %q: err %v, want a refusal", kind, err)
		}
	}
	if _, _, err := startDeck(reg, "start-p0", startSeat{
		Battlefield: []string{"Wall of Stone"},
		Counters:    []startCounter{{Card: "Wall of Stone", Kind: "P1P1", N: 1}},
	}); err != nil {
		t.Errorf("real counter kind refused: %v", err)
	}
}
