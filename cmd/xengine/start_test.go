package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/pincap"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestPlayStartThornvault(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	// -dirac is not among these: dirac follows gorge and answers a scripted
	// position beside it (TestStartFlagErrAcceptsDirac).
	for _, f := range []string{"-windows", "-random=BLB", "-pool=x", "-decks=x", "-tape-turns=1", "-format=commander", "-forge-only=x"} {
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
	t.Parallel()
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

// TestPlayStartCommanderDesignation plays the start scen writes for a
// Card.IsCommander+YouOwn trigger: the key (Tome of Legends) on the
// battlefield and the commander carrier (Isamaru, Hound of Konda) in p0's
// hand, with the file naming that card in "commanders" (CR 903.3). The
// runner hands the designation lists to game.Options and the Setup hook
// appends the held object's ID to Game.Players[0].Commanders — the list the
// engine reads for Card.IsCommander (Game.CommanderOf), never a printed
// keyword — so the focus cast plays: the id is not refused (a start whose
// focus was never taken is refused as start-not-exercised, and one whose
// lowering disagreed is not lower_ok), and the lowered setup carries the
// designation, which only the live list can show. The sibling file
// designates a card the seat does not hold (Tome of Legends in the graveyard
// only): that is a broken claim, so the game fails loud — a play_error row
// naming the designation, not a silently skipped commander.
func TestPlayStartCommanderDesignation(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	out := t.TempDir()
	if err := playStart(reg, []string{"testdata/start-commander"}, "FIC", out, false); err != nil {
		t.Fatal(err)
	}
	rows := map[string]lowerRow{}
	if err := readLines(filepath.Join(out, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		rows[r.ID] = r
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	okID := scen.StartID("FIC", "Tome of Legends", "cast", 1)
	if r := rows[okID]; r.Status != "lower_ok" {
		t.Errorf("%s: status %s reasons %v gorge %q, want lower_ok", okID, r.Status, r.Reasons, r.Gorge)
	}
	badID := scen.StartID("FIC", "Tome of Legends", "missing", 1)
	if r := rows[badID]; r.Status != statusPlayError || !strings.Contains(r.Gorge, "designates commander") {
		t.Errorf("%s: status %s gorge %q, want %s naming the designation", badID, r.Status, r.Gorge, statusPlayError)
	}
	if n := len(rows); n != 2 {
		t.Errorf("lower.jsonl has %d rows, want 2", n)
	}
	n := 0
	if err := readLines(filepath.Join(out, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		n++
		if want := []string{"Isamaru, Hound of Konda"}; !slices.Equal(it.Setup["p0"].Commanders, want) {
			t.Errorf("%s: p0 setup commanders %v, want %v", it.ID, it.Setup["p0"].Commanders, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("items.jsonl has %d rows, want 1 (the designated start)", n)
	}
}

// TestStartDeckRefusesFieldBlocked: a start file listing an instant, a
// sorcery or an Aura on the battlefield is refused (scen.FieldBlock), since
// gorge and Forge place it differently and the setup could never agree.
func TestStartDeckRefusesFieldBlocked(t *testing.T) {
	t.Parallel()
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

// TestStartToken: a start file's token scripts map to gorge's scripted tokens
// (rules.StartSeat.Tokens), and an unknown script is refused rather than
// starting without the token the key needs.
func TestStartToken(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	const script = "c_a_treasure_sac"
	_, ss, err := startDeck(reg, "start-p0", startSeat{Tokens: []startToken{{Script: script, Tapped: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ss.Tokens) != 1 || ss.Tokens[0].Script != script || !ss.Tokens[0].Tapped {
		t.Errorf("start tokens %+v, want one tapped %s", ss.Tokens, script)
	}
	for _, tok := range []startToken{{}, {Script: "no_such_token_script"}} {
		if _, _, err := startDeck(reg, "start-p0", startSeat{Tokens: []startToken{tok}}); err == nil {
			t.Errorf("start token %+v: no error, want a refusal", tok)
		}
	}
}

// TestStartTokenIsExercised: a scen start that places a token (the
// steps-needed:token class) plays, lowers and writes its items for both a cast
// and a triggered key, and each lowered item's setup carries the token: the
// token is a real scripted battlefield object, not only a field in the file.
func TestStartTokenIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, tc := range []struct{ card, key string }{
		{"Abyssal Harvester", "cast"},
		{"Academy Instructor", "f0/t0"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			res, err := scen.Generate(reg, b, tc.card, tc.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != scen.PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			if len(res.Start.Setup.P0.Tokens) == 0 {
				t.Fatalf("start places no token: %+v", res.Start.Setup.P0)
			}
			raw, err := json.Marshal(res.Start)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "x.start.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			out := t.TempDir()
			// xmage: the generic token must be one XMage's setup token table
			// maps, or its leg refuses the start while Forge's would not.
			if err := playStart(reg, []string{dir}, "XP", out, true); err != nil {
				t.Fatalf("play -start: %v", err)
			}
			// The emitted item's setup must carry the start's token, not only
			// the start file's field: startDeck's token loop is what mints it
			// for gorge, and a token missing there is a permanent the key reads
			// that both followers never placed.
			want := res.Start.Setup.P0.Tokens
			items := 0
			if err := readLines(filepath.Join(out, "items.jsonl"), func(b []byte) error {
				var it oraclegen.Item
				if err := json.Unmarshal(b, &it); err != nil {
					return err
				}
				items++
				for _, tok := range want {
					kept := false
					for _, seat := range it.Setup {
						for _, et := range seat.Tokens {
							if et.Script == tok.Script {
								kept = true
							}
						}
					}
					if !kept {
						t.Errorf("item %s setup has no token %q: the played setup dropped the start's token", it.ID, tok.Script)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if items != res.Start.Seeds {
				t.Errorf("items %d, want %d", items, res.Start.Seeds)
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
		})
	}
}

// TestStartPlayerCounter: a start file's player_counters are read strictly
// (no card, only a counter kind the engines track, n >= 1), folded into a
// sorted slice, and mapped to rules.StartSeat.PlayerCounters through the pin
// capability. The current pin has no such field; the seam proves the mapping
// path, and a start on a pin without the field is refused loudly instead of
// silently playing with no energy.
func TestStartPlayerCounter(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "start.json")
	body := `{"format":"xproof-start-1","card":"Aethersquall Ancient","key":"f0/a0","players":2,"active":0,"max_turns":1,` +
		`"setup":{"p0":{"battlefield":["Aethersquall Ancient"],"player_counters":[{"kind":"ENERGY","n":8}]},"p1":{}},` +
		`"filler":"basics","seeds":1}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := readStartFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := []startCounter{{Kind: "ENERGY", N: 8}}; !reflect.DeepEqual(f.Setup.P0.PlayerCounters, want) {
		t.Fatalf("p0 player counters %+v, want %+v", f.Setup.P0.PlayerCounters, want)
	}
	cs, err := playerCountersOf(f.Setup.P0.PlayerCounters)
	if err != nil {
		t.Fatal(err)
	}
	if want := []pincap.Counter{{Kind: "ENERGY", N: 8}}; !reflect.DeepEqual(cs, want) {
		t.Fatalf("seat 0 player counters %+v, want %+v", cs, want)
	}
	// A repeat of one kind is folded and the slice is sorted by kind, never a
	// map walk: the same JSON always produces the same item bytes.
	folded := filepath.Join(dir, "folded.json")
	foldedBody := strings.Replace(body, `[{"kind":"ENERGY","n":8}]`,
		`[{"kind":"POISON","n":2},{"kind":"ENERGY","n":3},{"kind":"ENERGY","n":5}]`, 1)
	if err := os.WriteFile(folded, []byte(foldedBody), 0o644); err != nil {
		t.Fatal(err)
	}
	ff, err := readStartFile(folded)
	if err != nil {
		t.Fatal(err)
	}
	got, err := playerCountersOf(ff.Setup.P0.PlayerCounters)
	if err != nil {
		t.Fatal(err)
	}
	if want := []pincap.Counter{{Kind: "ENERGY", N: 8}, {Kind: "POISON", N: 2}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("folded player counters %+v, want %+v", got, want)
	}
	// The native mapping: swap the pincap setter for a recorder, so this test
	// covers startDeckAt -> rules.StartSeat.PlayerCounters even while the pin
	// has no such field. Removing the mapping makes this fail.
	reg := testutil.CorpusRegistry(t)
	prior := setStartPlayerCounters
	var mapped []pincap.Counter
	setStartPlayerCounters = func(ss *rules.StartSeat, cs []pincap.Counter) bool {
		mapped = append([]pincap.Counter(nil), cs...)
		return true
	}
	t.Cleanup(func() { setStartPlayerCounters = prior })
	if _, _, err := startDeckAt(reg, "start-p0", 0, f.Setup.P0, f.Setup.P1); err != nil {
		t.Fatal(err)
	}
	if want := []pincap.Counter{{Kind: "ENERGY", N: 8}}; !reflect.DeepEqual(mapped, want) {
		t.Fatalf("mapped player counters %+v, want %+v", mapped, want)
	}
	// A seat with none does not call the setter at all.
	mapped = nil
	if _, _, err := startDeckAt(reg, "start-p0", 0, startSeat{Battlefield: []string{"Plains"}}, startSeat{}); err != nil {
		t.Fatal(err)
	}
	if mapped != nil {
		t.Errorf("seat with no player counters called the setter with %+v", mapped)
	}
	// At a pin without the native field, a start that names counters is an
	// error, not a silently unaffordable focus play written as
	// start-not-exercised far from the cause.
	setStartPlayerCounters = prior
	if pincap.PlayerCounters() {
		t.Skip("the pin now has rules.StartSeat.PlayerCounters; the pre-pin refusal no longer applies")
	}
	if _, _, err := startDeckAt(reg, "start-p0", 0, f.Setup.P0, f.Setup.P1); err == nil ||
		!strings.Contains(err.Error(), "rules.StartSeat.PlayerCounters") {
		t.Fatalf("pre-pin start with counters: err %v, want a rules.StartSeat.PlayerCounters refusal", err)
	}
	// A card name, an untracked kind and a non-positive amount are refused.
	for _, tc := range []struct{ name, counters, want string }{
		{"card", `[{"card":"Aethersquall Ancient","kind":"ENERGY","n":8}]`, "goes on the seat"},
		{"kind", `[{"kind":"CHARGE","n":1}]`, "not ENERGY, POISON or EXPERIENCE"},
		{"zero", `[{"kind":"ENERGY","n":0}]`, "want >= 1"},
	} {
		t.Run("refuse/"+tc.name, func(t *testing.T) {
			bad := filepath.Join(dir, tc.name+".json")
			b := strings.Replace(body, `[{"kind":"ENERGY","n":8}]`, tc.counters, 1)
			if err := os.WriteFile(bad, []byte(b), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := readStartFile(bad); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("player counters %s: err %v, want %q", tc.counters, err, tc.want)
			}
		})
	}
}

// TestStartSeedBase: seed_base moves where a start file's seed search
// starts, so a retried key plays new games; absent, the games are those of
// seed_base 1, as before the field.
func TestStartSeedBase(t *testing.T) {
	t.Parallel()
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
// literal All/Any the followers refuse, and sit on the opponent's creature or
// planeswalker.
func TestStartCounterBearer(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, tc := range []struct {
		card, key string
		seat      int
		on        string
	}{
		{"Honest Work", "f0/t0", 1, "Kobolds of Kher Keep"},
		// certdeck's first pick, Shifting Wall, is a 0/0 X creature that dies when a
		// start places it (scen.deadOnArrival): the next pick is a planeswalker
		{"Mabel, Bitter Recluse", "f0/t0", 1, "Ajani Resolute"},
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
			if !ok || len(c.Faces) == 0 || !(c.Faces[0].IsCreature() || c.Faces[0].IsPlaneswalker()) {
				t.Errorf("counter bearer %q is not a creature or planeswalker", got.Card)
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
	t.Parallel()
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

// TestStartFocusTimes: a start file's focus times reaches the game's focus
// ("your second spell each turn": two casts), and absent it is zero (once).
func TestStartFocusTimes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct {
		extra string
		want  int
	}{{"", 0}, {`,"times":2`, 2}} {
		p := filepath.Join(dir, "f.json")
		body := `{"format":"xproof-start-1","card":"Cori-Steel Cutter","key":"f0/t0","players":2,"active":0,"max_turns":1,` +
			`"setup":{"p0":{"hand":["Memnite","Memnite"],"battlefield":["Cori-Steel Cutter"]},"p1":{}},"filler":"basics","seeds":1,` +
			`"focus":{"seat":0,"card":"Memnite","kind":"cast","ability":0` + tc.extra + `}}`
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		f, err := readStartFile(p)
		if err != nil {
			t.Fatal(err)
		}
		fo, err := f.focusOf()
		if err != nil {
			t.Fatal(err)
		}
		if fo.Times != tc.want || fo.Card != "Memnite" || fo.Kind != "cast" {
			t.Errorf("focus %+v, want Memnite cast times %d", fo, tc.want)
		}
	}
}

// TestStartBaitTargetPlayerNeedsBait: baitTargetPlayer is independently
// settable in a start document, and it says the bait spell's own target is the
// focus player. Without a bait spell there is no such target ask, and the focus
// bot would answer an unrelated one with the focus seat, so focusOf refuses.
func TestStartBaitTargetPlayerNeedsBait(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "f.json")
	body := `{"format":"xproof-start-1","card":"Cori-Steel Cutter","key":"f0/t0","players":2,"active":0,"max_turns":1,` +
		`"setup":{"p0":{"hand":["Memnite","Memnite"],"battlefield":["Cori-Steel Cutter"]},"p1":{}},"filler":"basics","seeds":1,` +
		`"focus":{"seat":0,"card":"Memnite","kind":"cast","ability":0,"baitTargetPlayer":true}}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := readStartFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.focusOf(); err == nil || !strings.Contains(err.Error(), "baitTargetPlayer") {
		t.Fatalf("err %v, want a refusal naming baitTargetPlayer", err)
	}
}

// TestStartRefusesUnofferedFocus: a start whose focus seat is never offered the
// focus play is refused, not written. Burning Oil targets an attacking or
// blocking creature and nothing attacks at main 1, so the cast is never offered
// at any priority; Shock off the same lands is offered and cast, and its games
// are written.
func TestStartRefusesUnofferedFocus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, c := range []struct {
		card    string
		written bool
	}{{"Burning Oil", false}, {"Shock", true}} {
		t.Run(c.card, func(t *testing.T) {
			in := t.TempDir()
			body := `{"format":"xproof-start-1","card":"` + c.card + `","key":"cast","players":2,"active":0,` +
				`"max_turns":1,"setup":{"p0":{"hand":["` + c.card + `"],"battlefield":["Mountain","Mountain"]},` +
				`"p1":{"battlefield":["Memnite"]}},"filler":"basics","seeds":2,"focus":{"seat":0,"card":"` + c.card + `","kind":"cast"}}`
			if err := os.WriteFile(filepath.Join(in, "a.json"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			out := t.TempDir()
			if err := playStart(reg, []string{in}, "XP", out, false); err != nil {
				t.Fatal(err)
			}
			items, refused := 0, 0
			if err := readLines(filepath.Join(out, "items.jsonl"), func([]byte) error { items++; return nil }); err != nil {
				t.Fatal(err)
			}
			if err := readLines(filepath.Join(out, "lower.jsonl"), func(b []byte) error {
				var r lowerRow
				if err := json.Unmarshal(b, &r); err != nil {
					return err
				}
				if r.Status == statusRefused && slices.Equal(r.Reasons, []string{reasonStartNotExercised}) {
					refused++
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if c.written && (items != 2 || refused != 0) {
				t.Errorf("items %d refused %d, want 2 items and no refusal", items, refused)
			}
			if !c.written && (items != 0 || refused != 2) {
				t.Errorf("items %d refused %d, want no item and %d refusals %q", items, refused, 2, reasonStartNotExercised)
			}
		})
	}
}

// TestStartRoomDoor: a Room placed by setup enters with neither unlocked
// designation (CR 709.5d), so it has no name and no door's rules text is live
// (CR 709.5). A start that places a Room and focuses its front-door static
// would exercise nothing: the CR 709.5e unlock special action is not a step
// the three runners replay. play refuses it by name instead of writing an
// item that credits a static that never applied. Both a front-face spelling
// (Greenhouse) and a joined one (Solitary Study // Endless Corridor) are
// covered, so the check is on the card's Room type, not its spelling.
func TestStartRoomDoor(t *testing.T) {
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Greenhouse", "static#0.0"},                         // front-door static, Room placed by setup
		{"Solitary Study // Endless Corridor", "static#0.0"}, // a second Room shape, by its joined name
	} {
		t.Run(c.card+"-"+c.key, func(t *testing.T) {
			res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != scen.PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			raw, err := res.Start.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			in, out := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(in, "a.start.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := playStart(reg, []string{in}, "XP", out, false); err != nil {
				t.Fatal(err)
			}
			items, locked := 0, 0
			if err := readLines(filepath.Join(out, "items.jsonl"), func([]byte) error { items++; return nil }); err != nil {
				t.Fatal(err)
			}
			if err := readLines(filepath.Join(out, "lower.jsonl"), func(b []byte) error {
				var r lowerRow
				if err := json.Unmarshal(b, &r); err != nil {
					return err
				}
				if r.Status == statusRefused && len(r.Reasons) == 1 && strings.HasPrefix(r.Reasons[0], reasonStartRoomLocked+":") {
					locked++
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if items != 0 || locked != 2 {
				t.Errorf("items %d %s %d, want no item and 2 refusals %q (start %s)", items, reasonStartRoomLocked, locked, reasonStartRoomLocked, raw)
			}
		})
	}
}

// TestStartTransformingDFC: a start scen yields for a transforming double-faced
// card loads in play. scen lists the card on a battlefield, tapped or as a
// counter bearer by its joined name "A // B" while startDeck deals its front
// face, so binding a tapped/counter entry must resolve the name the same way.
func TestStartTransformingDFC(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, tc := range []struct{ card, key string }{
		{"Treasure Map // Treasure Cove", "f0/a0"}, // counter bearer
		{"Arcee, Sharpshooter // Arcee, Acrobatic Coupe", "cast"},
		{"Brass's Tunnel-Grinder // Tecutlan, the Searing Rift", "cast"},
		{"Aang, at the Crossroads // Aang, Destined Savior", "cast"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			res, err := scen.Generate(reg, b, tc.card, tc.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != scen.PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			raw, err := json.Marshal(res.Start)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "x.start.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := playStart(reg, []string{dir}, "XP", t.TempDir(), false); err != nil {
				t.Fatalf("play -start: %v", err)
			}
		})
	}
}

// TestStartHybridCostIsExercised: a card script writes a hybrid, two-generic
// hybrid or Phyrexian mana symbol without a slash (ManaCost:2 WB WB, 2W, UP).
// scen read only the slashed form, so it placed no land for those symbols (and
// a Plains for a cost whose only colours were hybrid): the cast was never
// affordable and play refused every such start as start-not-exercised (xproof
// 11250507: 234 of the 1661 start-not-exercised keys had a hybrid cost).
func TestStartHybridCostIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, card := range []string{
		"Lydia Frye",          // 2 UB: three lands, one an Island or a Swamp
		"Syndicate Heavy",     // 2 WB WB
		"Kitchen Finks",       // 1 GW GW
		"Spectral Procession", // 2W 2W 2W
	} {
		t.Run(card, func(t *testing.T) { _ = startIsExercised(t, reg, b, card, "cast") })
	}
}

// TestStartStepsNeededCostClassIsExercised: every card the scen
// steps-needed:cost class now starts (a cost paid by placed state: minted
// tokens, a discarded/exiled/sacrificed permanent, a counter on an inert
// bearer or on the card itself) lowers both seeds and offers the focus play.
// Generating a start is not enough: the reviewer's q2 round found Ion Storm,
// Rift Elemental, Quillspike and Sage of Fables generated starts that lowered
// to zero items (a counter on one of the mana Mountains, a 1/1 killed by its
// own -1/-1 counter, an entry counter setup cannot restate), and the dh1
// round found Rasputin Dreamweaver's personal counter arm doing the same
// (a mandatory upkeep and the startedTheTurnUntapped field), so each card is
// played through the cmd/xengine path here. The refusal list carries the
// whole-start gate sweep's named refusals (scen.startGateRefusal): starts on
// the unguarded cast and trigger lanes for the same two Rasputin shapes, and
// Vitaspore Thallid's spore ability placing a mandatory-upkeep permanent.
func TestStartStepsNeededCostClassIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Baylen, the Haymaker", "f0/a0"}, // tapXType of minted tokens
		{"Baylen, the Haymaker", "f0/a2"}, //
		{"Fear of Abduction", "cast"},     // Exile<1/Creature> additional cost
		{"Soul Exchange", "cast"},         //
		{"Necrotic Fumes", "cast"},        //
		{"Ion Storm", "f0/a0"},            // SubCounter<1/P1P1/Permanent>
		{"Ion Storm", "f0/a1"},            // SubCounter<1/CHARGE/Permanent>
		{"Rift Elemental", "f0/a0"},       // SubCounter<1/TIME/Permanent>
		{"Quillspike", "f0/a0"},           // SubCounter<1/M1M1/Creature>
		{"Korozda Gorgon", "f0/a0"},       // SubCounter<1/P1P1/Creature.YouCtrl>
		{"Bolrac-Clan Crusher", "f0/a0"},  //
		{"Zameck Guildmage", "f0/a1"},     //
		{"Ray Fillet, Man Ray", "f0/a0"},  //
		{"Shapers of Nature", "f0/a1"},    //
		{"Sunstar Chaplain", "f0/a0"},     //
		{"Migloz, Maze Crusher", "f0/a2"}, // SubCounter<N/OIL/NICKNAME>
		{"Charforger", "f0/a0"},           // SubCounter<3/OIL/CARDNAME>
		{"Glistener Seer", "f0/a0"},       // SubCounter<1/OIL/CARDNAME>
		{"Tamiyo's Immobilizer", "f0/a0"}, // SubCounter<1/OIL/CARDNAME>
	} {
		t.Run(c.card+" "+c.key, func(t *testing.T) {
			_ = startIsExercised(t, reg, b, c.card, c.key)
		})
	}
	// A shape the class cannot yet meet keeps its named refusal: the start
	// schema cannot restate Sage of Fables's entry counter on another
	// permanent, an activated ability's Exile cost does not lower (Food Chain's
	// colour ask, The Soul Stone's upkeep trigger), Moxite Refinery removes
	// a wildcard counter kind, and Rasputin Dreamweaver's personal counters
	// need a battlefield placement the start gate refuses (its upkeep trigger
	// is mandatory and its startedTheTurnUntapped word is live). The same gate
	// now sweeps every finished start (scen.startGateRefusal), so the lanes
	// that placed no guard there name it too instead of emitting a start play
	// lowers to zero items: Rasputin's cast and upkeep-trigger keys carry the
	// startedTheTurnUntapped word even from hand (the gate's gameMentions
	// stamps the field on every untapped land), and Vitaspore Thallid's spore
	// ability places a permanent whose upkeep trigger is mandatory.
	for _, c := range []struct{ card, key, reason string }{
		{"Sage of Fables", "f0/a0", "steps-needed:cost:SubCounter"},
		{"Moxite Refinery", "f0/a0", "steps-needed:cost:SubCounter"},
		{"Food Chain", "f0/a0", "steps-needed:cost:Exile"},
		{"The Soul Stone", "f0/a1", "steps-needed:cost:Exile"},
		{"Rasputin Dreamweaver", "f0/a0", "steps-needed:cost:SubCounter"},
		{"Rasputin Dreamweaver", "f0/a1", "steps-needed:cost:SubCounter"},
		{"Rasputin Dreamweaver", "cast", "start:started-turn-untapped"},
		{"Rasputin Dreamweaver", "f0/t0", "start:started-turn-untapped"},
		{"Vitaspore Thallid", "f0/a1", "start:upkeep-trigger"},
	} {
		t.Run(c.card+" refused", func(t *testing.T) {
			res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != scen.PathRefused || res.Reason != c.reason {
				t.Errorf("%s %s: path %q reason %q, want refused %q", c.card, c.key, res.Path, res.Reason, c.reason)
			}
		})
	}
}

// startIsExercised generates the start file for card's key, plays it, and
// fails when play refuses it as start-not-exercised or writes fewer items than
// the start's seeds; it returns the items written.
func startIsExercised(t *testing.T, reg *cards.Registry, b *certdeck.Builder, card, key string) []oraclegen.Item {
	t.Helper()
	res, err := scen.Generate(reg, b, card, key, scen.Options{Prefer: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != scen.PathStart || res.Start == nil {
		t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
	}
	items, rows, raw := playStartFile(t, reg, res.Start)
	var refused []string
	for _, r := range rows {
		if slices.Contains(r.Reasons, reasonStartNotExercised) {
			t.Errorf("%s: %s (start %s)", r.ID, reasonStartNotExercised, raw)
		}
		if r.Status != "" {
			b, _ := json.Marshal(r)
			refused = append(refused, string(b))
		}
	}
	// One item per seed is the generator's contract: a seed that lowers to no
	// item leaves the key unproven there even when the other seed writes one.
	if len(items) != res.Start.Seeds {
		t.Errorf("%d item(s), want %d (one per seed): lowering %v (start %s)", len(items), res.Start.Seeds, refused, raw)
	}
	return items
}

// playStartFile plays a generated start for both seeds and returns its items,
// lowering rows and the rendered start. A play error is fatal.
func playStartFile(t *testing.T, reg *cards.Registry, st *scen.StartFile) ([]oraclegen.Item, []lowerRow, []byte) {
	t.Helper()
	items, rows, raw, _ := playStartFileAt(t, reg, st)
	return items, rows, raw
}

// playStartFileAt is playStartFile that also returns the play directory, so a
// caller can read the exercised ledger the tape alone filled.
func playStartFileAt(t *testing.T, reg *cards.Registry, st *scen.StartFile) ([]oraclegen.Item, []lowerRow, []byte, string) {
	t.Helper()
	raw, err := st.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	in := t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "a.start.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := playStart(reg, []string{in}, "XP", out, false); err != nil {
		t.Fatal(err)
	}
	var items []oraclegen.Item
	if err := readLines(filepath.Join(out, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		items = append(items, it)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var rows []lowerRow
	if err := readLines(filepath.Join(out, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		rows = append(rows, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return items, rows, raw, out
}

// TestStartChangesZoneTriggersAreExercised: the ChangesZone/ChangesZoneAll
// recipes scen builds are focuses xengine can actually take: a token maker
// (Anointer Priest), an Aura attached to a bearer then destroyed (Aspect of
// Mongoose, Banewasp Affliction), a scripted combat (Abattoir Ghoul), a
// graveyard return (Attuned Hunter), and Magnetic Theft plus an end-step
// sacrifice (Avarice Amulet). A recipe that generates but is never offered
// still owes the key (start-not-exercised).
//
// The exercised ledger cannot prove the trigger fired: credit (cmd/xengine)
// always joins the item id's declared key, so Exercised > 0 for any lowered
// start. The test reads the lowered cast instead: its target must be the start
// focus's named key object (the Aura, its bearer, the Equipment and the
// sacrificial bearer), which is what makes the zone change and the trigger.
func TestStartChangesZoneTriggersAreExercised(t *testing.T) {
	t.Parallel()
	keys := [][2]string{
		{"Abattoir Ghoul", "f0/t0"},
		{"Anointer Priest", "f0/t0"},
		{"Aspect of Mongoose", "f0/t0"},
		{"Attuned Hunter", "f0/t0"},
		{"Avarice Amulet", "f0/t0"},
		{"Banewasp Affliction", "f0/t0"},
	}
	ks := lowerKeyStarts(t, keys)
	reg, b := certdeckBuilder(t)
	for _, k := range keys {
		res, err := scen.Generate(reg, b, k[0], k[1], scen.Options{Prefer: "start"})
		if err != nil || res.Start == nil || res.Start.Focus == nil {
			t.Fatalf("%s %s: generate: %+v %v", k[0], k[1], res, err)
		}
		if res.Start.Focus.Kind != "cast" {
			continue // an attack focus (Abattoir Ghoul) names no cast target
		}
		want := res.Start.Focus.Targets
		seen := 0
		for id, it := range ks.items {
			if it.Card != k[0] {
				continue
			}
			cast := false
			for _, st := range it.Steps {
				if st.Op != "cast" || !strings.HasSuffix(st.Card, ":"+res.Start.Focus.Card) {
					continue
				}
				cast = true
				if len(want) == 0 {
					continue // the focus play's own target (Regrowth's graveyard card)
				}
				var got []string
				for _, ref := range st.Targets {
					got = append(got, oraclediff.RefName(ref))
				}
				if !slices.Equal(got, want) {
					t.Errorf("%s: %s targets %v, want the named key objects %v", id, st.Card, got, want)
				}
			}
			if !cast {
				t.Errorf("%s: no cast of the focus %q", id, res.Start.Focus.Card)
				continue
			}
			seen++
		}
		if seen == 0 {
			t.Errorf("%s %s: no lowered item with the focus cast", k[0], k[1])
		}
	}
}

// TestStartChangesZoneKeysCreditedByTape: credit always joins the item id's
// declared key, so a start whose focus never moves the key object still reads
// exercised. With that join off, only the tape credits the key: each
// ChangesZone start must make its own trigger fire. Dead Man's Chest is the
// Aura whose Enchant keyword names an opponent's creature, so its bearer must
// sit on the other seat (a bearer on its own seat is no legal Enchant target
// and the Aura never attaches); Keiga and Vengeant Vampire are self-dies
// triggers with a target, which the start must still place.
func TestStartChangesZoneKeysCreditedByTape(t *testing.T) {
	creditDeclared = false
	t.Cleanup(func() { creditDeclared = true })
	keys := [][2]string{
		{"Abattoir Ghoul", "f0/t0"},
		{"Anointer Priest", "f0/t0"},
		{"Aspect of Mongoose", "f0/t0"},
		{"Attuned Hunter", "f0/t0"},
		{"Avarice Amulet", "f0/t0"},
		{"Banewasp Affliction", "f0/t0"},
		{"Dead Man's Chest", "f0/t0"},
		{"Keiga, the Tide Star", "f0/t0"},
		{"Vengeant Vampire", "f0/t0"},
	}
	ks := lowerKeyStarts(t, keys)
	led := readExercised(t, ks.play)
	for _, k := range keys {
		if got := led.Get(k[0], k[1]); got.Exercised == 0 {
			t.Errorf("%s %s: the tape credits no use of the key (ledger %+v): the start never fires the trigger", k[0], k[1], got)
		}
	}
}

// TestStartManaAbilityIsExercised: a focus that is a mana ability was never
// taken. gorge offers a mana ability at priority as one "activate" option per
// source, never as an "ability" option with the ability's index, so the focus
// seat waited for an option that never came and play refused every such start
// as start-not-exercised.
func TestStartManaAbilityIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct {
		card, key string
		ability   int
	}{
		{"Agent of Stromgald", "f0/a0", 0}, // R: Add {B}
		{"Bog Initiate", "f0/a0", 0},       // 1: Add {B}
		{"Cabal Coffers", "f0/a0", 0},      // 2, T: Add {B} for each Swamp you control
		{"Bayou", "f0/a1", 1},              // T: Add {G}, the second of Bayou's two
		{"Shivan Reef", "f0/a1", 1},        // T: Add {U} or {R}, 1 damage: asks the wheel, then the colour
		{"Talisman of Dominance", "f0/a1", 1},
		{"Karplusan Forest", "f0/a1", 1},
		{"Sliver Hive", "f0/a1", 1}, // Add one mana of any color: a wheel option, then the colour ask
		{"Grove of the Burnwillows", "f0/a1", 1},
	} {
		t.Run(c.card, func(t *testing.T) {
			for _, it := range startIsExercised(t, reg, b, c.card, c.key) {
				// the focus ability itself is a step of the item: an activate
				// of the card at the key's index
				found := false
				for _, st := range it.Steps {
					if st.Op == "activate" && strings.Contains(st.Card, c.card) && st.AbilityIndex != nil && *st.AbilityIndex == c.ability {
						found = true
					}
				}
				if !found {
					t.Errorf("%s: no activate step of %s ability %d: %+v", it.ID, c.card, c.ability, it.Steps)
				}
			}
		})
	}
}

// TestStartManaWheelColourIsExercised: a mana-ability focus that is the second
// of its source's mana abilities and asks a colour (pain lands, Talismans,
// Grove of the Burnwillows: f0/a1, "{T}: Add {U} or {B}") records its tap on the
// engine's choice among the source's mana abilities, and the lowering refused
// it as unsupported_kind:activate. That choice offers an explicit combo's
// colours as options of their own, so the step's colour answer picks it.
func TestStartManaWheelColourIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, card := range []string{"Caves of Koilos", "Talisman of Progress", "Grove of the Burnwillows"} {
		t.Run(card, func(t *testing.T) {
			for _, it := range startIsExercised(t, reg, b, card, "f0/a1") {
				found := false
				for _, st := range it.Steps {
					if st.Op == "activate" && strings.Contains(st.Card, card) && st.AbilityIndex != nil && *st.AbilityIndex == 1 &&
						len(st.Answers) == 1 && st.Answers[0].Kind == "choose" {
						found = true
					}
				}
				if !found {
					t.Errorf("%s: no activate step of %s ability 1 with its colour answer: %+v", it.ID, card, it.Steps)
				}
			}
		})
	}
}

// TestStartTargetRestrictionIsExercised: the focus play's target check read
// only a target's card type and side, never the rest of its ValidTgts filter
// (Permanent.nonLand, a colour, withFlying, cmcLE3), so a start met "target
// nonland permanent" with its own land and "target black creature" with an
// Ornithopter: the planted target was illegal, the play was never offered,
// and play refused the start as start-not-exercised.
func TestStartTargetRestrictionIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Aerial Volley", "cast"},   // Creature.withFlying
		{"Celestial Purge", "cast"}, // Permanent.Black,Permanent.Red
		{"Abrupt Decay", "cast"},    // Permanent.nonLand+cmcLE3
		{"Daraja Griffin", "f0/a0"}, // Sac<1/CARDNAME>: destroy target black creature
	} {
		t.Run(c.card, func(t *testing.T) { _ = startIsExercised(t, reg, b, c.card, c.key) })
	}
}

// TestStartTargetShapesAreExercised: the focus-target shapes that used to
// refuse as start:needs-target because the placement search only knew a card
// of a permanent type: a token target (Replicator, Dogged Hunter), a creature
// subtype no card in the corpus holds (Blinkmoth Nexus, met by a changeling), a
// six-target spell (Hex), and a monocolored permanent (Vanishing Verse). A
// TargetValidTargeting$ restriction that needs the bait spell itself to have a
// target is the stack-target sibling, covered by the hard-card census.
func TestStartTargetShapesAreExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Replicator", "f0/a0"},        // target token you control
		{"Dogged Hunter", "f0/a0"},     // target creature token
		{"Blinkmoth Nexus", "f0/a2"},   // target Blinkmoth creature (a changeling)
		{"Hex", "cast"},                // six target creatures
		{"Vanishing Verse", "cast"},    // target monocolored permanent
		{"Avoid Fate", "cast"},         // target spell that targets a permanent you control
		{"Cerulean Drake", "f0/a0"},    // counter target spell that targets you
		{"Hydromorph Gull", "f0/a0"},   // counter target spell that targets a creature you control
		{"Ring of Immortals", "f0/a0"}, // counter target spell that targets a permanent you control
	} {
		t.Run(c.card, func(t *testing.T) { _ = startIsExercised(t, reg, b, c.card, c.key) })
	}
}

// TestStartUnsupportedTargetKeepsReason: a target shape no start can place keeps
// a specific refusal instead of a written start the replay would refuse as
// start-not-exercised. Cradle to Grave needs a creature that entered this turn,
// which a setup permanent or token does not (it is placed before turn 1); Wash
// Away's default cast mode needs a spell not cast from its owner's hand, and its
// cleave mode is not the mode the bot takes. Commander's Plate and Seasinger
// name Forge predicates under the Creature head (IsCommander,
// ControllerControlsIsland), which are not creature subtypes: the changeling
// shortcut must not write a start whose target the engine rejects.
func TestStartUnsupportedTargetKeepsReason(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key, reason string }{
		{"Cradle to Grave", "cast", "start:target-condition:this-turn-entered"},
		{"Wash Away", "cast", "start:needs-stack-target"},
		{"Commander's Plate", "f0/a0", "start:needs-target"},
		{"Seasinger", "f0/a0", "start:needs-target"},
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != scen.PathRefused || res.Reason != c.reason {
				t.Fatalf("path %q reason %q, want refused %q", res.Path, res.Reason, c.reason)
			}
		})
	}
}

// TestStartStackTargetIsExercised: a focus play that targets a spell was
// refused as start:needs-stack-target, because a start holds at main 1 with an
// empty stack. The start now scripts the opponent's bait cast (Focus.Bait): the
// opponent casts it at main 1, then the focus seat casts its counterspell with
// the bait as its target.
func TestStartStackTargetIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Absorb", "cast"},           // Counter target spell
		{"Bone to Ash", "cast"},      // Counter target creature spell
		{"Assert Authority", "cast"}, // Counter target spell, exile instead
		{"Condescend", "cast"},       // Counter target spell unless its controller pays {X}
	} {
		t.Run(c.card, func(t *testing.T) {
			items := startIsExercised(t, reg, b, c.card, c.key)
			for _, it := range items {
				var casts []string
				for _, s := range it.Steps {
					if s.Op == "cast" {
						casts = append(casts, s.Card)
					}
				}
				// the bait is cast first, then the focus card (the lowered
				// item relabels the active seat p0, so the seat prefix says
				// which is which only after the swap)
				if len(casts) < 2 || !strings.HasSuffix(casts[1], ":"+c.card) ||
					casts[0] == casts[1] {
					t.Errorf("%s: casts %v, want the opponent's bait then %s", it.ID, casts, c.card)
				}
			}
		})
	}
}

// TestStartStackTargetIncidentalUpkeepIsExercised: a bait start still lowers
// when the only beginning-of-upkeep trigger is incidental to the focus play.
// Wizard's Retort needs a Wizard, and certdeck's first pick (Drifter il-Dal)
// has an upkeep trigger, so the generator must skip it for the battlefield;
// Lilting Refrain is the focus card itself, and the scenario replay walks the
// same turn-1 upkeep the live start did, so its optional trigger does not
// refuse the span.
func TestStartStackTargetIncidentalUpkeepIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Wizard's Retort", "cast"},
		{"Lilting Refrain", "f0/a0"},
	} {
		t.Run(c.card, func(t *testing.T) { _ = startIsExercised(t, reg, b, c.card, c.key) })
	}
}

// TestStartStackTargetCostIsExercised: an alternate additional cost
// ("behold a Jace or pay {1}", "tap an artifact or pay {1}") is paid in
// addition to the mana cost, and the generated start placed only the printed
// cost's lands. With a bait on the stack the focus cast was never offered and
// play refused both seeds as start-not-exercised. The mana fallback now gets
// its lands too.
func TestStartStackTargetCostIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, card := range []string{
		"Countersculpt",        // behold a Jace or pay {1}
		"Disruption Protocol",  // tap an artifact or pay {1}
		"Louisoix's Sacrifice", // sacrifice a legendary creature or pay {2}
		"Wild Unraveling",      // blight 2 or pay {1}
	} {
		t.Run(card, func(t *testing.T) { _ = startIsExercised(t, reg, b, card, "cast") })
	}
}

// stackTargetHardCards are the stack-target cards the generator has got wrong
// before, played on every default run: Remove Soul, Meddle (a creature-spell
// filter), the eleven cards whose stack target must itself target something
// (TargetValidTargeting: the first bait was an inert Reach Through Mists no
// such spell can be cast against), Ring of Immortals (an ability that targets a
// spell), Admiral's Order and Aerial Assault (the brief's needs-target
// refusals), the alternate-additional-cost cards, and the residual bait forms:
// an activated-ability target (Bind, Squelch, Interdict), a
// Spell.singleTarget target (Divert, Deflection, Misdirection), a
// you-control stack target the focus seat meets with its own held pre-cast
// (Double Major, Increasing Vengeance), and a focus-seat ability target whose
// bait's lands must not consume the focus ability's colour (Weaver of
// Harmony).
var stackTargetHardCards = []string{
	"Remove Soul", "Meddle", "Admiral's Order", "Aerial Assault",
	"Keep Safe", "Outwit", "Intervene", "Avoid Fate", "Hindering Light", "Confound",
	"Psychic Rebuttal", "Turn Aside", "Rebuff the Wicked", "Not of This World",
	"Teferi's Response", "Ring of Immortals",
	"Countersculpt", "Disruption Protocol", "Louisoix's Sacrifice", "Wild Unraveling",
	"Wizard's Retort", "Lilting Refrain", // incidental upkeep triggers on a placed enabler / the focus card
	"Bind", "Squelch", "Interdict",
	"Divert", "Deflection", "Misdirection",
	"Double Major", "Increasing Vengeance",
	"Weaver of Harmony",
}

// stackTargetWatchedCards are the stack-target cards whose incidental upkeep
// trigger once made every bait start lower to no item (Wizard's Retort's
// certdeck Wizard was Drifter il-Dal, and Lilting Refrain is the focus card).
// They are no longer exempt: the census requires an item from every bait start
// and logs each of these keys, so a regression names the card and key.
var stackTargetWatchedCards = map[string]bool{"Wizard's Retort": true, "Lilting Refrain": true}

// stackTargetCards is every playable card with a face whose script declares a
// stack target, sorted.
func stackTargetCards(reg *cards.Registry, b *certdeck.Builder) []string {
	return cardsWithStackTarget(reg, b, func(spell, ability bool) bool { return spell || ability })
}

// stackAbilityTargetCards is every playable card with a face whose script
// declares an activated or triggered ability on the stack. It is the census
// for the ability-target bait class (Bind, Squelch, Lithoform Engine, Weaver of
// Harmony): a predicate that only looked for "Spell" left every one of them
// outside the whole-corpus run.
func stackAbilityTargetCards(reg *cards.Registry, b *certdeck.Builder) []string {
	return cardsWithStackTarget(reg, b, func(_, ability bool) bool { return ability })
}

// stackTargetKinds reads a Forge TargetType value into whether it names a
// spell (Spell/SpellAbility) or an ability (Activated/Triggered). A suffix on
// the kind (Activated.singleTarget, Spell.Colorless) does not change the kind.
func stackTargetKinds(tt string) (spell, ability bool) {
	for _, tok := range strings.Split(tt, ",") {
		head, _, _ := strings.Cut(strings.TrimSpace(tok), ".")
		switch head {
		case "Spell", "SpellAbility":
			spell = true
		case "Activated", "Triggered":
			ability = true
		}
	}
	return spell, ability
}

// svarTargetKinds reads one SubAbility$ line for the stack kinds its
// TargetType or TargetValidTargeting names.
func svarTargetKinds(v string) (spell, ability bool) {
	for _, field := range strings.Split(v, "|") {
		k, val, ok := strings.Cut(field, "$")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "TargetType":
			s, a := stackTargetKinds(val)
			spell, ability = spell || s, ability || a
		case "TargetValidTargeting":
			if strings.TrimSpace(val) != "" {
				spell = true
			}
		}
	}
	return spell, ability
}

// cardsWithStackTarget is every playable card whose abilities or SubAbility
// lines name a stack target the keep predicate admits, sorted. Both the direct
// ability Params and the named SVars are read, so an ability reached only by
// SubAbility$ is not missed.
func cardsWithStackTarget(reg *cards.Registry, b *certdeck.Builder, keep func(spell, ability bool) bool) []string {
	var names []string
	for _, c := range reg.AllCards() {
		if len(c.Faces) == 0 || !b.Playable(c) {
			continue
		}
		for _, f := range c.Faces {
			spell, ability := false, false
			for _, a := range f.Abilities {
				s, ab := stackTargetKinds(a.Params["TargetType"])
				if a.Params["TargetValidTargeting"] != "" {
					s = true
				}
				spell, ability = spell || s, ability || ab
			}
			for _, v := range f.SVars {
				s, ab := svarTargetKinds(v)
				spell, ability = spell || s, ability || ab
			}
			if keep(spell, ability) {
				names = append(names, f.Name)
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

// stackTargetCensus generates every key of each card and plays each start that
// carries a bait: the focus play must be offered on both seeds (no
// start-not-exercised row), and a bait start must lower to at least one item.
// This is the guard for the class of
// regression where the bait was accepted for a spell the focus play could never
// target (TargetValidTargeting, a bait cast from the hand against "wasn't cast
// from hand", a sorcery-speed focus behind the bait): xengine play refused
// those starts as start-not-exercised, so the key could never be proven even
// though a start file existed.
func stackTargetCensus(t *testing.T, reg *cards.Registry, b *certdeck.Builder, names []string) {
	t.Helper()
	keys, refused, noBait, wrote, exercised := 0, 0, 0, 0, 0
	for _, card := range names {
		c, ok := reg.Lookup(card)
		if !ok {
			t.Errorf("%s is not in the corpus", card)
			continue
		}
		for _, key := range scen.Keys(c) {
			keys++
			res, err := scen.Generate(reg, b, card, key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Errorf("%s %s: %v", card, key, err)
				continue
			}
			if res.Path != scen.PathStart || res.Start == nil {
				refused++
				continue
			}
			if res.Start.Focus == nil || (res.Start.Focus.Bait == "" && res.Start.Focus.BaitSelf == "" &&
				res.Start.Focus.BaitAbility == "" && res.Start.Focus.BaitTarget == "") {
				noBait++ // a start with no bait at all (a non-stack key, or no stack target)
				continue
			}
			wrote++
			items, rows, raw := playStartFile(t, reg, res.Start)
			bad := false
			for _, r := range rows {
				if slices.Contains(r.Reasons, reasonStartNotExercised) {
					bad = true
					t.Errorf("%s %s: %s (start %s)", card, key, reasonStartNotExercised, raw)
				}
			}
			if len(items) != res.Start.Seeds {
				bad = true
				t.Errorf("%s %s: a bait start wrote %d item(s), want %d (one per seed) (start %s)", card, key, len(items), res.Start.Seeds, raw)
			}
			if !bad {
				exercised++
				if stackTargetWatchedCards[card] {
					t.Logf("stack-target census: %s %s exercised (%d item(s))", card, key, len(items))
				}
			}
		}
	}
	t.Logf("stack-target census: cards=%d keys=%d refused=%d no-bait-start=%d wrote-with-bait=%d exercised=%d bad=%d",
		len(names), keys, refused, noBait, wrote, exercised, wrote-exercised)
}

// TestStartStackTargetHardCardsAreExercised is the default-run census: the
// known-hard stack-target cards (stackTargetHardCards), every key.
func TestStartStackTargetHardCardsAreExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	stackTargetCensus(t, reg, b, stackTargetHardCards)
}

// TestStartAbilityTargetCensusIsExercised is the default-run census for the
// ability-target bait class: every playable card whose script names an
// Activated or Triggered stack target. The "Spell"-only predicate left this
// class out of the whole-corpus run, so a generated ability bait that the
// replay refused (Weaver of Harmony's self-activated bait ate the focus
// ability's colour) was never gated.
func TestStartAbilityTargetCensusIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	stackTargetCensus(t, reg, b, stackAbilityTargetCards(reg, b))
}

// TestStartStackTargetCensusIsExercised is the same census over every
// stack-target card in the corpus. It takes about 50s on its own, which with
// the rest of the package overruns the merge gate's 2m cap, so it runs only
// with XENGINE_STACK_TARGET_CENSUS=1 (a separate, non-gate run).
func TestStartStackTargetCensusIsExercised(t *testing.T) {
	t.Parallel()
	if os.Getenv("XENGINE_STACK_TARGET_CENSUS") == "" {
		t.Skip("set XENGINE_STACK_TARGET_CENSUS=1 to run the whole-corpus census")
	}
	reg, b := certdeckBuilder(t)
	stackTargetCensus(t, reg, b, stackTargetCards(reg, b))
}

// TestStartMultiFacedFocusIsExercised: a start names a multi-faced card as its
// card file does ("Akoum Warrior // Akoum Teeth") while the game object's face
// carries the front name alone, and the focus seat compared the two as
// written: no object was ever a focus object, so the focus play of every
// transforming, modal and adventure card went untaken and play refused the
// start as start-not-exercised (259 of the 792 such keys at bf707e46b1).
func TestStartMultiFacedFocusIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Akoum Warrior // Akoum Teeth", "cast"},                   // modal DFC
		{"Bonded Herdbeast // Plated Kilnbeast", "cast"},           // transforming DFC
		{"Curious Homunculus // Voracious Reader", "f0/t0"},        // a front-face trigger
		{"Invasion of Theros // Ephara, Ever-Sheltering", "f0/t0"}, // a battle's enters trigger
	} {
		t.Run(c.card, func(t *testing.T) { _ = startIsExercised(t, reg, b, c.card, c.key) })
	}
}

// TestStartBackFaceIsExercised: a back-face key (scen: no-generator:face until
// now) is played from the card on the battlefield on its back face, the focus
// play the back face's own ability: the listed card enters transformed
// (rules.StartSeat.BackFace) and the focus finds it by its front name.
func TestStartBackFaceIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Aclazotz, Deepest Betrayal // Temple of the Dead", "f1/a0"},        // a modal back face (a land)
		{"Shaile, Dean of Radiance // Embrose, Dean of Shadow", "f1/a0"},     // a modal creature's ability
		{"Waterlogged Hulk // Watertight Gondola", "f1/a0"},                  // a transformed artifact's ability
		{"Tamiyo, Inquisitive Student // Tamiyo, Seasoned Scholar", "f1/a2"}, // a transformed planeswalker's loyalty ability
		{"Thousand Moons Smithy // Barracks of the Thousand", "f1/t0"},       // a back-face trigger
	} {
		t.Run(c.card, func(t *testing.T) {
			items := startIsExercised(t, reg, b, c.card, c.key)
			for _, it := range items { // the lowered item still plays the card on its back face
				kept := 0
				for _, sd := range it.Setup {
					kept += len(sd.BackFace)
				}
				if kept == 0 {
					t.Errorf("%s: item %s has no back_face in its setup: the recorded span plays the front face", c.key, it.ID)
				}
			}
		})
	}
}

// startExercisesKey plays card's start and fails unless the exercised ledger
// credits key: the start was offered and the transform fired the back-face
// trigger. It is startIsExercised plus the ledger read, for a key whose item
// being written is not enough (a trigger can be lowered without firing).
func startExercisesKey(t *testing.T, reg *cards.Registry, b *certdeck.Builder, card, key string) {
	t.Helper()
	res, err := scen.Generate(reg, b, card, key, scen.Options{Prefer: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != scen.PathStart || res.Start == nil {
		t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
	}
	raw, err := res.Start.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	in, out := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "a.start.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := playStart(reg, []string{in}, "XP", out, false); err != nil {
		t.Fatal(err)
	}
	bs, err := os.ReadFile(filepath.Join(out, "exercised.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	led, err := cov.ReadTSV(bytes.NewReader(bs))
	if err != nil {
		t.Fatal(err)
	}
	if n := led.Get(canonicalCard(reg, card), key).Exercised; n == 0 {
		t.Errorf("%s %s: exercised=%d, want the start to fire the key (ledger %s)", card, key, n, bs)
	}
}

// TestStartTransformedTriggerIsExercised: a Transformed trigger's start must
// place the front face and put its transform cause on the line, and the
// triggering transform must actually fire the back-face key. The own-ability
// and Moonmist enabler shapes play and fire; a daybound or upkeep/draw front
// is refused by its narrow reason instead of being built into a start that
// lower.gate refuses (CR 702.145b for Daybound/Nightbound).
func TestStartTransformedTriggerIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Voldaren Pariah", "f1/t0"}, // own activated transform ability
		{"Kindly Stranger", "f1/t0"}, // Moonmist, the one-card enabler
	} {
		t.Run(c.card, func(t *testing.T) { startExercisesKey(t, reg, b, c.card, c.key) })
	}
	for _, c := range []struct{ card, key, reason string }{
		{"Howlpack Piper", "f1/t1", "no-generator:trigger:Transformed:day-night"},
		{"Huntmaster of the Fells", "f1/t0", "no-generator:trigger:Transformed:condition"},
		{"Afflicted Deserter", "f1/t0", "no-generator:trigger:Transformed:condition"},
		{"Ulrich of the Krallenhorde", "f1/t0", "no-generator:trigger:Transformed:condition"},
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Start != nil || res.Path != scen.PathRefused || res.Reason != c.reason {
				t.Errorf("%s %s: path %q reason %q, want refused %q", c.card, c.key, res.Path, res.Reason, c.reason)
			}
		})
	}
}

// TestStartAsEntersAndDayNightIsExercised: an as-enters choice the cast puts
// on the tape (a self-ETB card, so the choice is made by the engine during the
// cast) and a day/night designation the start declares play through playStart
// to an item on every seed and lower cleanly. A placement whose as-enters
// choice would be answered after genesis keeps a specific refusal, and the
// still-unsupported kinds keep their own name instead of a plain as-enters.
func TestStartAsEntersAndDayNightIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key, day string }{
		{"Gathering Stone", "f0/t0", ""},
		{"Weaver of Blossoms", "f0/a0", scen.DayNightDay},
		{"Weaver of Blossoms", "f1/a0", scen.DayNightNight},
		{"Graveyard Trespasser // Graveyard Glutton", "f0/t1", scen.DayNightDay},
		{"Lunar Rejection", "cast", scen.DayNightDay},
	} {
		t.Run(c.card+"/"+c.key, func(t *testing.T) {
			res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != scen.PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start", res.Path, res.Reason)
			}
			if res.Start.DayNight != c.day {
				t.Errorf("day_night %q, want %q", res.Start.DayNight, c.day)
			}
			items, rows, raw := playStartFile(t, reg, res.Start)
			if len(items) != res.Start.Seeds {
				t.Errorf("wrote %d item(s), want %d (start %s)", len(items), res.Start.Seeds, raw)
			}
			for _, r := range rows {
				if r.Status != "lower_ok" {
					t.Errorf("%s: status %s reasons %v (start %s)", r.ID, r.Status, r.Reasons, raw)
				}
			}
		})
	}
	for _, c := range []struct{ card, key, reason string }{
		// A state-backed as-enters creature is cast as the start line (see
		// TestStartAsEntersCreatureIsExercised); the kinds setup cannot read
		// keep their named refusal.
		{"Sower of Discord", "f0/t0", "as-enters:player"},
		{"Sorcerous Spyglass", "static#0.0", "as-enters:player"},
	} {
		res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Path != scen.PathRefused || res.Reason != c.reason {
			t.Errorf("%s %s: path %q reason %q, want refused %q", c.card, c.key, res.Path, res.Reason, c.reason)
		}
	}
}

// TestStartAsEntersCreatureIsExercised: a creature whose own as-enters
// replacement asks a state-backed choice (a creature type here) is not placed
// at genesis. That placement is a real move this turn, so the live cut carries
// Object.EnteredThisTurn and Object.AcqTurn -- the summoning-sick stamp the
// lowering's setup drive cannot restate (it places permanents directly, CR
// 302.6). A static key is cast as the start line; an ability or BecomesTarget
// trigger key casts the card as a prelude and then takes the key's own action,
// so the entry, the choose answer and both turn stamps are tape events without
// turning an honest refusal into an item that credits a key it never took.
//
// creditDeclared is off: the item id would otherwise credit any lowered start,
// including one whose cast never activated the ability or fired the trigger.
// The exercised ledger here is the tape's alone. A static key has no tape
// entry naming it (credit's declared join is its only reader), so the static
// cases instead assert the observation setup staticKeyStart builds (its carrier
// and Compare fields), which a plain cast start would drop, and only the
// ability and trigger keys are read from the ledger.
func TestStartAsEntersCreatureIsExercised(t *testing.T) {
	reg, b := certdeckBuilder(t)
	creditDeclared = false
	t.Cleanup(func() { creditDeclared = true })
	for _, c := range []struct {
		card, key, focusKind, focusCard string
		focusSeat                       int
		prelude, tapeOnly               bool
		carrier                         string
		compare                         []string
	}{
		{"Adaptive Automaton", "static#0.0", "cast", "Adaptive Automaton", 0, false, false, "", nil},
		// staticKeyStart's own observation setup -- the affected carrier it
		// places and the Compare fields it sets -- must survive the move to
		// hand. A plain cast start drops both and credits the key by id only.
		{"Rukarumel, Biologist", "static#0.0", "cast", "Rukarumel, Biologist", 0, false, false, "Memnite", nil},
		{"Kindred Boon", "static#0.0", "cast", "Kindred Boon", 0, false, false, "Crimson Kobolds", []string{oraclediff.CompareKeywords}},
		{"Lifecraft Engine", "f0/a0", "ability", "Lifecraft Engine", 0, true, true, "", nil},
		{"Roaming Throne", "f0/t0", "cast", "Murder", 1, true, true, "", nil}, // the opponent's targeting spell, after the prelude cast
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != scen.PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			if !slices.Contains(res.Start.Setup.P0.Hand, c.card) || slices.Contains(res.Start.Setup.P0.Battlefield, c.card) {
				t.Fatalf("p0 hand %v battlefield %v, want %s in hand and not placed",
					res.Start.Setup.P0.Hand, res.Start.Setup.P0.Battlefield, c.card)
			}
			if c.carrier != "" && !slices.Contains(res.Start.Setup.P0.Battlefield, c.carrier) {
				t.Errorf("p0 battlefield %v, want staticKeyStart's carrier %q", res.Start.Setup.P0.Battlefield, c.carrier)
			}
			if !slices.Equal(res.Start.Compare, c.compare) {
				t.Errorf("compare %v, want %v: the static setup's observation fields were dropped", res.Start.Compare, c.compare)
			}
			if c.prelude {
				if res.Start.Prelude != c.card || res.Start.PreludeSeat != 0 {
					t.Fatalf("prelude %q seat %d, want %q seat 0", res.Start.Prelude, res.Start.PreludeSeat, c.card)
				}
			} else if res.Start.Prelude != "" {
				t.Fatalf("prelude %q, want none for a cast start", res.Start.Prelude)
			}
			if res.Start.Focus == nil || res.Start.Focus.Seat == nil || *res.Start.Focus.Seat != c.focusSeat ||
				res.Start.Focus.Card != c.focusCard || res.Start.Focus.Kind != c.focusKind {
				t.Fatalf("focus %+v, want seat %d %s %s", res.Start.Focus, c.focusSeat, c.focusKind, c.focusCard)
			}
			items, rows, raw, out := playStartFileAt(t, reg, res.Start)
			if len(items) != res.Start.Seeds {
				t.Errorf("%d item(s), want %d: one per seed (start %s)", len(items), res.Start.Seeds, raw)
			}
			if len(rows) != res.Start.Seeds {
				t.Errorf("%d lowering row(s), want %d (start %s)", len(rows), res.Start.Seeds, raw)
			}
			for _, r := range rows {
				if r.Status != "lower_ok" {
					t.Errorf("%s: status %s reasons %v (start %s)", r.ID, r.Status, r.Reasons, raw)
				}
			}
			// The as-enters answer must be on the tape, not only in the start's
			// state: the engine asked it during the cast and the lowering
			// scripted the pick.
			chosen := false
			for _, it := range items {
				for _, st := range it.Steps {
					for _, a := range st.Answers {
						if a.Kind == "choose" && len(a.Pick) > 0 {
							chosen = true
						}
					}
				}
			}
			if !chosen {
				t.Errorf("no item records the as-enters choose answer (start %s)", raw)
			}
			if c.tapeOnly {
				// The tape itself must credit the key: the prelude cast alone
				// is not the ability / trigger the item exists for.
				if got := readExercised(t, out).Get(c.card, c.key); got.Exercised == 0 {
					t.Errorf("%s %s: the tape credits no use of the key (ledger %+v)", c.card, c.key, got)
				}
			}
		})
	}
	for _, c := range []struct{ card, key, reason string }{
		// A combat key is the creature's attack or block: it needs the
		// permanent placed for that play, so the entry stamp cannot be
		// restated and the generator keeps the named refusal.
		{"Haktos the Unscarred", "combat#0.attack", "start:as-enters-stamp"},
		// A creature whose as-enters answer is not state setup can read keeps
		// its named refusal too.
		{"Sower of Discord", "f0/t0", "as-enters:player"},
		// A trigger a cast this turn cannot fire (an attack, an upkeep, another
		// permanent's entry) keeps the named refusal rather than a cast-only
		// item that never exercises it.
		{"Titan of Littjara", "f0/t1", "start:as-enters-stamp"},
		{"Herald's Horn", "f0/t0", "start:as-enters-stamp"},
		// A static whose own setup refuses keeps that named refusal: casting
		// the card cannot make an unobservable static exercised.
		{"Banner of Kinship", "static#0.0", "no-generator:static:affected"},
		{"Iona, Shield of Emeria", "static#0.0", "no-generator:static:CantBeCast"},
		{"Cloud Key", "static#0.0", "no-generator:static:ReduceCost"},
	} {
		res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Path != scen.PathRefused || res.Start != nil || res.Reason != c.reason {
			t.Errorf("%s %s: path %q reason %q, want refused %q", c.card, c.key, res.Path, res.Reason, c.reason)
		}
	}
}

// TestStartDayNightMatchesEngineCut: the day_night a start declares is the
// designation the engine actually has at turn 1's first precombat main, not
// the designation the genesis placements alone suggest. A gated Moved DayTime
// replacement (Brimstone Vandal, Obsessive Astronomer) makes the game day as
// the permanent enters during genesis; CR 731.2a's turn-1 untap check then
// makes it night before the cut because PrevTurnSpells is zero. A daybound
// permanent is read by the post-untap state-based action (CR 702.145d), so a
// lone Weaver of Blossoms stays day, while its nightbound back face is night.
// TestScenStartDayNightTurnOneCut pins the generator's model to these same
// values; this test makes the engine, not the model, the source of truth.
func TestStartDayNightMatchesEngineCut(t *testing.T) {
	t.Parallel()
	reg, _ := certdeckBuilder(t)
	for _, c := range []struct {
		name string
		seat startSeat
		want state.DayNight
	}{
		{"Brimstone Vandal", startSeat{Battlefield: []string{"Brimstone Vandal"}}, state.DayNightNight},
		{"Obsessive Astronomer", startSeat{Battlefield: []string{"Obsessive Astronomer"}}, state.DayNightNight},
		{"Weaver of Blossoms front", startSeat{Battlefield: []string{"Weaver of Blossoms"}}, state.DayNightDay},
		{"Weaver of Blossoms back", startSeat{Battlefield: []string{"Weaver of Blossoms"}, BackFace: []string{"Weaver of Blossoms"}}, state.DayNightNight},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := engineCutDayNight(t, reg, c.seat); got != c.want {
				t.Errorf("turn 1 main1 DayNight = %v, want %v", got, c.want)
			}
		})
	}
}

// engineCutDayNight plays the hand-built seat as p0's scripted start and
// returns the designation the engine holds at turn 1's first-main cut.
func engineCutDayNight(t *testing.T, reg *cards.Registry, seat startSeat) state.DayNight {
	t.Helper()
	d0, s0, err := startDeckAt(reg, "start-p0", 0, seat, startSeat{})
	if err != nil {
		t.Fatal(err)
	}
	d1, s1, err := startDeckAt(reg, "start-p1", 1, startSeat{}, seat)
	if err != nil {
		t.Fatal(err)
	}
	got := state.DayNightNeither
	seen := false
	opt := game.Options{
		Set: "XP", MaxTurns: 2,
		Start: &rules.StartSpec{Seats: []rules.StartSeat{s0, s1}},
		OnCut: func(e *rules.Engine, entry int) {
			if !seen {
				got, seen = e.G.DayNight, true
			}
		},
	}
	seed := rules.SeedForStarter(1, 2, 0)
	if _, err := game.Play(reg, []decks.Deck{d0, d1}, seed, opt); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("the start never reached turn 1's first-main cut")
	}
	return got
}

// TestStartDayNightSpelling: the runner validates the declaration instead of
// silently ignoring a misspelt designation; day, night and empty are accepted.
func TestStartDayNightSpelling(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ value, wantErr string }{
		{"", ""},
		{scen.DayNightDay, ""},
		{scen.DayNightNight, ""},
		{"Day", "day_night"},
		{"dusk", "day_night"},
	} {
		t.Run("value="+tc.value, func(t *testing.T) {
			p := filepath.Join(dir, "start.json")
			body := `{"format":"xproof-start-1","card":"Llanowar Elves","key":"f0/a0","players":2,"active":0,` +
				`"max_turns":1,"setup":{"p0":{"battlefield":["Llanowar Elves"]},"p1":{}},"filler":"basics","seeds":1,` +
				`"day_night":"` + tc.value + `"}`
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := readStartFile(p)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("day_night %q: %v", tc.value, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("day_night %q: err %v, want one naming %q", tc.value, err, tc.wantErr)
			}
		})
	}
}

// TestStartBackFaceLostIsRefused: a back-face start whose card turns to its
// front before the recorded span (Doric's back face transforms it at the
// upkeep) is refused as start-face-lost, writes no item and credits nothing:
// the tape plays the front face, so the back-face key was never exercised.
func TestStartBackFaceLostIsRefused(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	const card = "Doric, Nature's Warden // Doric, Owlbear Avenger"
	for _, key := range []string{"combat#1.attack", "combat#1.block"} {
		t.Run(key, func(t *testing.T) {
			res, err := scen.Generate(reg, b, card, key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Start == nil {
				return // refused by scen: no start, nothing to credit
			}
			raw, err := res.Start.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			in, out := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(in, "a.start.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := playStart(reg, []string{in}, "XP", out, false); err != nil {
				t.Fatal(err)
			}
			if bs, err := os.ReadFile(filepath.Join(out, "items.jsonl")); err != nil || len(bytes.TrimSpace(bs)) != 0 {
				t.Errorf("items.jsonl %q (err %v), want none: the back face is lost before the span", bs, err)
			}
			if bs, err := os.ReadFile(filepath.Join(out, "exercised.tsv")); err != nil || bytes.Contains(bs, []byte(key)) {
				t.Errorf("exercised.tsv credits %s (err %v): %s", key, err, bs)
			}
		})
	}
}

// TestStartGraveyardTargetIsExercised: a focus play that targets a card in a
// graveyard (TgtZone$ Graveyard, or a ChangeZone from Origin$ Graveyard) was
// read as met, so the start placed no such card (or only the basics a
// graveyard need places), the play was never offered, and play refused the
// start as start-not-exercised. The most inert card that meets the target now
// goes to the graveyard the filter names.
func TestStartGraveyardTargetIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Necromantic Summons", "cast"},    // target creature card in a graveyard
		{"Relearn", "cast"},                // instant or sorcery card from your graveyard
		{"Scribe of the Mindful", "f0/a0"}, // Instant.YouCtrl,Sorcery.YouCtrl
		{"Protomatter Powder", "f0/a0"},    // Artifact.YouCtrl
	} {
		t.Run(c.card, func(t *testing.T) { _ = startIsExercised(t, reg, b, c.card, c.key) })
	}
}

// TestStartFocusTargetIsPlaced: the static target check read a target as met
// when only the focus card itself met it (Adarkar Valkyrie's "target creature
// other than CARDNAME"), when its type was not one a hand list placed
// (Equipment, Vehicle), or when the target was a card in a graveyard a Charm
// choice names (Aid the Fallen) or TgtZone names (Chandra, Acolyte of Flame).
// The start placed no such target, the play was never offered, and play refused
// the start as start-not-exercised. A minimal legal target now goes on the
// right side or zone.
func TestStartFocusTargetIsPlaced(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Adarkar Valkyrie", "f0/a0"},             // Creature.Other
		{"Vizier of Tumbling Sands", "f0/a0"},     // Permanent.Other
		{"Lwaxana Troi, Charming Guest", "f0/a0"}, // Creature.Other+YouCtrl
		{"Paragon of Eternal Wilds", "f0/a0"},     // Creature.Green+Other+YouCtrl
		{"Peacewalker Colossus", "f0/a0"},         // Vehicle.Other+YouCtrl
		{"Auriok Windwalker", "f0/a0"},            // Equipment.YouCtrl
		{"Kazuul's Toll Collector", "f0/a0"},      // Equipment.YouCtrl
		{"Saving Grasp", "cast"},                  // Creature.YouOwn
		{"Aid the Fallen", "cast"},                // Charm choice: creature card in your graveyard
		{"Fortuitous Find", "cast"},               // Charm choices: artifact/creature card in your graveyard
		{"Remember the Fallen", "cast"},           // Charm choices: creature/artifact card in your graveyard
		{"Reborn Hope", "cast"},                   // Card.MultiColor+YouCtrl in the graveyard
		{"Chandra, Acolyte of Flame", "f0/a2"},    // TgtZone$ Graveyard
		{"Aerial Assault", "cast"},                // Creature.tapped: the target must be placed tapped
		{"Asphyxiate", "cast"},                    // Creature.untapped: the target must be placed untapped
	} {
		t.Run(c.card, func(t *testing.T) { _ = startIsExercised(t, reg, b, c.card, c.key) })
	}
}

// TestStartTwoTargetsIsExercised: a play that names two targets was met by one
// permanent (Switcheroo's "two target creatures", Peace and Quiet's two target
// enchantments), and a target only the focus seat's own permanent meets (a
// fight spell's "target creature you control") was never placed, so the play
// was never offered: play refused the start as start-not-exercised, or scen as
// start:needs-target. A target of n permanents now needs n distinct ones, and
// the most inert carrier of an own-side target's type goes on the focus seat's
// side.
func TestStartTwoTargetsIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Switcheroo", "cast"},      // TargetMin$ 2
		{"Peace and Quiet", "cast"}, // TargetMin$ 2
		{"Hunt the Hunter", "cast"}, // Creature.Green+YouCtrl, then the opponent's
		{"Ram Through", "cast"},     // Creature.YouCtrl, then Creature.YouDontCtrl
	} {
		t.Run(c.card, func(t *testing.T) { _ = startIsExercised(t, reg, b, c.card, c.key) })
	}
}

// combatSteps is the attackers and blocks the items' attack and block steps
// declare, as "attack:<card>" and "block:<blocker>><attacker>".
func combatSteps(items []oraclegen.Item) []string {
	var out []string
	for _, it := range items {
		for _, st := range it.Steps {
			switch st.Op {
			case "attack":
				for _, a := range st.Attackers {
					out = append(out, "attack:"+a)
				}
			case "block":
				for _, b := range st.Blocks {
					out = append(out, "block:"+b[0]+">"+b[1])
				}
			}
		}
	}
	return out
}

// startCombatIsExercised is startIsExercised for a key whose line declares a
// combat: every item has an attack step (and a block step when block), each
// declaring one creature.
func startCombatIsExercised(t *testing.T, card, key string, block bool) {
	t.Helper()
	reg, b := certdeckBuilder(t)
	for _, it := range startIsExercised(t, reg, b, card, key) {
		var attacks, blocks int
		for _, s := range combatSteps([]oraclegen.Item{it}) {
			if strings.HasPrefix(s, "attack:") {
				attacks++
			} else {
				blocks++
			}
		}
		if attacks != 1 || block && blocks != 1 || !block && blocks != 0 {
			t.Errorf("%s: combat %v, want one attacker and %v one blocker", it.ID, combatSteps([]oraclegen.Item{it}), block)
		}
	}
}

// TestStartAttackTriggerIsExercised: "whenever a creature you control with
// power 2 or less attacks" (Attacks, ValidCard a filter, not Card.Self) was
// met by the card itself attacking: an enchantment never can, so the attack
// was never offered and play refused the start as start-not-exercised. The
// attacker is now the most inert carrier creature the filter takes, the
// opponent's for "whenever a creature attacks you".
func TestStartAttackTriggerIsExercised(t *testing.T) {
	t.Parallel()
	for _, card := range []string{
		"Raid Bombardment",      // Creature.powerLE2+YouCtrl
		"Righteous Cause",       // Creature
		"Cavalcade of Calamity", // Creature.powerLE1+YouCtrl
		"Lost in the Woods",     // Creature, Attacked$ You: the opponent attacks
	} {
		t.Run(card, func(t *testing.T) { startCombatIsExercised(t, card, "f0/t0", false) })
	}
}

// TestStartAttackingTargetIsExercised: a play that targets an attacking
// creature ("target attacking creature", Creature.attacking) is offered only
// in combat, and the focus seat took its play at the first main-phase
// priority: scen refused the start as start:needs-combat. The start now
// scripts the attack (StartFocus.Attack): a carrier the filter takes, on the
// battlefield since the turn began, attacks, and the play waits for it.
func TestStartAttackingTargetIsExercised(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ card, key string }{
		{"Gravel Slinger", "f0/a0"},   // T: attacking or blocking creature; the Slinger stays home
		{"Rubblehulk", "f0/a0"},       // bloodrush from hand
		{"Wasteland Viper", "f0/a0"},  // bloodrush, a {G} cast the start pays: holdPass dropped no payment
		{"Wrecking Ogre", "f0/a0"},    // bloodrush, a {3}{R}{R} cast the start pays
		{"Unraveling Mummy", "f0/a0"}, // Zombie.attacking: a subtype head
		{"Pitfall Trap", "cast"},      // Creature.attacking+withoutFlying
	} {
		t.Run(c.card, func(t *testing.T) { startCombatIsExercised(t, c.card, c.key, false) })
	}
}

// TestStartBlockingTargetIsExercised: "target blocking creature" needs a
// block, which only the defending seat declares (CR 509.1a): the start now
// makes the focus seat the defender, the opponent attacking with Grizzly
// Bears and the focus seat blocking it with a carrier (StartFocus.Block).
func TestStartBlockingTargetIsExercised(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ card, key string }{
		{"Kithkin Shielddare", "f0/a0"}, // W, T: target blocking creature gets +2/+2
		{"Rabid Rats", "f0/a0"},         // T: target blocking creature gets -1/-1
		{"Elven Fortress", "f0/a0"},     // 1 G: target blocking creature gets +0/+1
		{"Evasive Maneuvers", "cast"},   // an instant
	} {
		t.Run(c.card, func(t *testing.T) { startCombatIsExercised(t, c.card, c.key, true) })
	}
}

// TestStartFocusCombatantTargetIsExercised: a target in combat with the focus
// card ("creature blocking Knight of Dusk", Creature.blockingSource; "creature
// Wall of Corpses is blocking", blockedBySource) was read as any creature, so
// the start planted one out of combat and play refused it as
// start-not-exercised. The focus card now attacks or blocks in the scripted
// combat, and the target is its opposite number; a {T} play (Steadfast
// Armasaur) takes the blocking side, as an attack would tap it.
func TestStartFocusCombatantTargetIsExercised(t *testing.T) {
	t.Parallel()
	for _, card := range []string{
		"Knight of Dusk",      // Creature.blockingSource: the Knight attacks
		"Wall of Corpses",     // Creature.blockedBySourceLKI: the Wall blocks
		"Steadfast Armasaur",  // 1 W, T: blockingSource or blockedBySource
		"Benalish Missionary", // Creature.blocked: an attacker that was blocked
	} {
		t.Run(card, func(t *testing.T) { startCombatIsExercised(t, card, "f0/a0", true) })
	}
}

// TestStartAdditionalCostIsExercised: a cast start placed only the lands for
// the card's mana cost, never what the spell's Cost$ spends beyond it (a
// creature to sacrifice, creature cards to exile from the graveyard, creatures
// to tap, "behold or pay {2}", "waterbend {5}"), an {S} cost placed no snow
// source, and a land's enters trigger was a cast focus a land never offers: the play was never affordable and play refused the start as
// start-not-exercised (xproof 11250823: 27 of 45 such keys replayed at
// bc885e54e2).
func TestStartAdditionalCostIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Costly Plunder", "cast"},           // Sac<1/Artifact;Creature>
		{"Skaab Goliath", "cast"},            // ExileFromGrave<2/Creature>
		{"Shared Discovery", "cast"},         // tapXType<4/Creature>
		{"Silvergill Mentor", "f0/t0"},       // AlternateAdditionalCost: behold a Merfolk or {2}
		{"Benevolent River Spirit", "f0/t0"}, // RaiseCost Waterbend<5>
		{"Chilling Shade", "f0/a0"},          // Cost$ S
		{"Aetherflux Reservoir", "f0/a0"},    // PayLife<50>
		{"Idyllic Grange", "f0/t0"},          // a land: played, with three other Plains so it enters untapped
	} {
		t.Run(c.card, func(t *testing.T) { _ = startIsExercised(t, reg, b, c.card, c.key) })
	}
}

// TestStartOpeningHandFocusIsCast: a Leyline's opening-hand "yes" put it on
// the battlefield before main 1, so its cast was never offered and play
// refused the start as start-not-exercised. The focus seat now declines it;
// the lowering then refuses the game for its own reason (opening_hand_effect:
// a follower's setup would answer that ask too), not as an unexercised start.
func TestStartOpeningHandFocusIsCast(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	res, err := scen.Generate(reg, b, "Leyline of Vitality", "cast", scen.Options{Prefer: "start"})
	if err != nil || res.Start == nil {
		t.Fatalf("scen: %v %q", err, res.Reason)
	}
	raw, err := res.Start.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	in, out := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "a.start.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := playStart(reg, []string{in}, "XP", out, false); err != nil {
		t.Fatal(err)
	}
	if err := readLines(filepath.Join(out, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		if slices.Contains(r.Reasons, reasonStartNotExercised) {
			t.Errorf("%s: %s", r.ID, reasonStartNotExercised)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestStartAbilityManaFocusIsExercised: an "ability" focus whose cost needs
// mana was never funded. gorge prices an activated ability against the
// floating pool as it prices a cast, and the bot leaves the lands that would
// pay for it untapped (it has no reason to activate a pump or adapt ability),
// so the option was never offered and play refused every such start as
// start-not-exercised (xproof 11250904/07/09: 9 keys of the spot box cycles,
// Growth-Chamber Guardian f0/a0 and Resolute Rider f0/a1 among them). The
// focus now taps its lands for mana too, but never the focus card itself when
// the ability's own cost taps it (Vitu-Ghazi, the City-Tree: its {T} is the
// cost, its mana ability only a source for the rest).
func TestStartAbilityManaFocusIsExercised(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Growth-Chamber Guardian", "f0/a0"},   // {2}{G}: Adapt 2
		{"Resolute Rider", "f0/a1"},            // {W/B}{W/B}{W/B}: indestructible
		{"Stillmoon Cavalier", "f0/a2"},        // {W/B}{W/B}: +1/+0
		{"Elvish Hexhunter", "f0/a0"},          // {G/W}, {T}, Sacrifice: destroy target enchantment
		{"Vitu-Ghazi, the City-Tree", "f0/a1"}, // {2}{G}{W}, {T}: a Saproling; the focus card itself taps for {C}
	} {
		t.Run(c.card, func(t *testing.T) { _ = startIsExercised(t, reg, b, c.card, c.key) })
	}
}

// TestStartCombatTriggerIsExercised: a combat trigger (blocks, becomes blocked,
// combat damage) had no start (no-generator:trigger:<Mode>), so no engine ever
// played the key. The start now scripts the combat (StartFocus.Attack, Block):
// every item has the attack step, and the block step when the trigger reads one.
func TestStartCombatTriggerIsExercised(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		card, key string
		block     bool
	}{
		{"Acolyte of the Inferno", "f0/t0", true},                    // AttackerBlockedByCreature, the card attacks
		{"Ammit Eternal", "f0/t2", true},                             // AttackerBlocked (afflict), the card attacks
		{"Aether Membrane", "f0/t0", true},                           // AttackerBlocked, the card blocks
		{"Pious Warrior", "f0/t0", true},                             // DamageDoneOnce, the card is dealt damage
		{"Akki Lavarunner // Tok-Tok, Volcano Born", "f0/t0", false}, // DamageDone, unblocked
		{"Vulture, Feathered Fiend", "f0/t0", false},                 // DamageDoneOnce: a flier with power deals it
		{"Mu Yanling, Wind Rider", "f0/t1", false},                   // DamageDoneOnce, a flier beside the card
	} {
		t.Run(c.card, func(t *testing.T) { startCombatIsExercised(t, c.card, c.key, c.block) })
	}
}

// TestStartCombatLineNeverDeclaredIsRefused: a combat start has no focus play
// (kind ""), so its scripted combat is the focus action: a line whose attacker
// is never offered (a defender cannot attack, CR 702.3b) exercises nothing of
// the key and is refused as start-not-exercised, not written.
func TestStartCombatLineNeverDeclaredIsRefused(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	res, err := scen.Generate(reg, b, "Acolyte of the Inferno", "f0/t0", scen.Options{Prefer: "start"})
	if err != nil || res.Start == nil || res.Start.Focus == nil {
		t.Fatalf("scen: %v %q", err, res.Reason)
	}
	f := res.Start
	f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, "Wall of Stone")
	f.Focus.Attack = "Wall of Stone"
	raw, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	in, out := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(in, "a.start.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := playStart(reg, []string{in}, "XP", out, false); err != nil {
		t.Fatal(err)
	}
	refused := false
	if err := readLines(filepath.Join(out, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		refused = refused || slices.Contains(r.Reasons, reasonStartNotExercised)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !refused {
		t.Errorf("a start whose attacker never attacks was not refused as %s", reasonStartNotExercised)
	}
}

// TestStartActivationConditionIsMetOrRefused: the activation gates the engine
// reads when it offers the focus play must either be satisfied by placed
// state or refuse the start by name. Crater Elemental's Formidable count,
// Kindly Stranger's Delirium condition, Bloodline Keeper's five-Vampire
// present gate and the legendary lock on Urza's Ruinous Blast are placed to
// and the keys fire; Aven Augur's upkeep-only window is refused, never
// written as a start the play would refuse as start-not-exercised.
func TestStartActivationConditionIsMetOrRefused(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Crater Elemental", "f0/a1"},                         // CheckSVar$ FormidableTest GE8
		{"Kindly Stranger // Demon-Possessed Witch", "f0/a0"}, // Activation$ Delirium
		{"Bloodline Keeper // Lord of Lineage", "f0/a1"},      // IsPresent$ GE5
		{"Urza's Ruinous Blast", "cast"},                      // legendary sorcery
		{"Companion of the Trials", "f0/a0"},                  // IsPresent$ Planeswalker.Gideon+YouCtrl, no PresentCompare$
		{"Tectonic Edge", "f0/a1"},                            // PlayerCountOpponents$HighestValid Land.YouCtrl GE4
		{"Shadows of the Past", "f0/a0"},                      // four creature cards in the graveyard
		{"Champions of the Shoal", "cast"},                    // BeholdExile<1/Merfolk>
		// optional and X additional costs the bot may decline: still starts
		{"Crashing Wave", "cast"},
		{"March of Wretched Sorrow", "cast"},
		{"Explosive Singularity", "cast"},
		{"Aether Tide", "cast"},
	} {
		t.Run(c.card, func(t *testing.T) { startExercisesKey(t, reg, b, c.card, c.key) })
	}
	// a gate no placed state can satisfy at turn 1's main refuses by name
	for _, c := range []struct{ card, key, reason string }{
		{"Aven Augur", "f0/a0", "start:upkeep-only"},
		{"Hell's Caretaker", "f0/a0", "start:upkeep-only"},
		{"Bloodsoaked Champion", "f0/a0", "start:attackers-declared"}, // Raid: Count$AttackersDeclared
		{"Vengeful Devil", "f0/a0", "start:morbid"},                   // Count$Morbid.1.0
		{"Serra Avenger", "cast", "start:cantbecast"},                 // a vanilla creature spell with a CantBeCast static
		{"Ogre Siegebreaker", "f0/a0", "start:target-condition:was-dealt-damage-this-turn"},
		{"Witch's Mist", "f0/a0", "start:target-condition:was-dealt-damage-this-turn"},
		{"Sword of the Paruns", "f0/a0", "start:present-count"}, // IsPresent$ Card.EquippedBy, no PresentCompare$
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Start != nil || res.Path != scen.PathRefused || res.Reason != c.reason {
				t.Errorf("path %q reason %q, want refused %q", res.Path, res.Reason, c.reason)
			}
		})
	}
}

// TestPlayStartPlayerCountersPinRefusesPerID: at a pin without
// rules.StartSeat.PlayerCounters, the start that names energy is refused per
// id in the lowering log and the other starts of the directory still play.
func TestPlayStartPlayerCountersPinRefusesPerID(t *testing.T) {
	if pincap.PlayerCounters() {
		t.Skip("the pin now has rules.StartSeat.PlayerCounters; there is nothing to refuse")
	}
	reg := testutil.CorpusRegistry(t)
	dir := t.TempDir()
	good, err := os.ReadFile("testdata/start/thornvault-forager-f0a2.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b-good.json"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	energy := `{"format":"xproof-start-1","card":"Aethersquall Ancient","key":"f0/a0","players":2,"active":0,"max_turns":1,` +
		`"setup":{"p0":{"battlefield":["Aethersquall Ancient"],"player_counters":[{"kind":"ENERGY","n":8}]},"p1":{}},` +
		`"filler":"basics","seeds":2}`
	// Sorts first, so a hard error here would lose b-good.json.
	if err := os.WriteFile(filepath.Join(dir, "a-energy.json"), []byte(energy), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := playStart(reg, []string{dir}, "XP", out, false); err != nil {
		t.Fatalf("one pre-pin start aborted the batch: %v", err)
	}
	refused := 0
	if err := readLines(filepath.Join(out, "lower.jsonl"), func(b []byte) error {
		if bytes.Contains(b, []byte(reasonStartPlayerCountersPin)) {
			refused++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if refused != 2 {
		t.Errorf("%d refused rows with %q, want 2 (one per seed)", refused, reasonStartPlayerCountersPin)
	}
	items := 0
	if err := readLines(filepath.Join(out, "items.jsonl"), func([]byte) error { items++; return nil }); err != nil {
		t.Fatal(err)
	}
	if items == 0 {
		t.Error("the other start of the directory wrote no item")
	}
}

// TestStartSurvivesToMainOne: a start's focus card must still be on the
// battlefield, with the mana its play needs, at turn 1's first main priority.
// The setup drive passes through the upkeep, so a focus card that dies to
// state-based actions as placed (an X creature's unplaced counters, a
// characteristic-defining toughness of 0), a focus card whose upkeep makes it
// sacrifice itself, an enabler whose Echo or cumulative upkeep taps the focus'
// mana, and an attack focus that cannot attack (tapped, Defender) all lose the
// key before it can be exercised (xproof cycles 11250904/07/09). scen must
// either refuse the start by a named start: reason or write one play refuses
// as start-not-exercised.
func TestStartSurvivesToMainOne(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Molten-Tail Masticore", "f0/a1"}, // upkeep sacrifice, empty hand
		{"Mortivore", "f0/a0"},             // CDA */* with an empty graveyard
		{"Walking Ballista", "f0/a0"},      // X counters never placed
		{"Biogenic Ooze", "cast"},          // Mwonvuli Ooze's cumulative upkeep taps the mana
		{"Munitions Expert", "f0/t0"},      // Goblin Patrol's Echo taps the only Mountain
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s %s: path %q reason %q", c.card, c.key, res.Path, res.Reason)
			if res.Path == scen.PathRefused {
				if !strings.HasPrefix(res.Reason, "start:") {
					t.Fatalf("%s: refused %q, want a named start: reason or an exercised start", c.card, res.Reason)
				}
				return
			}
			if res.Path != scen.PathStart || res.Start == nil {
				t.Fatalf("%s: path %q reason %q, want a start or a start: refusal", c.card, res.Path, res.Reason)
			}
			_ = startIsExercised(t, reg, b, c.card, c.key)
		})
	}
	// The refined upkeep check must keep exercising the keys develop
	// exercises: the focus card elsewhere or still in hand pays nothing for
	// its own upkeep (Earthlink, Thelon's Chant), a trigger that cannot fire
	// from the battlefield or is optional never binds (Pyre Zombie, Brass
	// Gnat). Each must write a start that plays and exercises: a start:
	// refusal here is a regression.
	for _, c := range []struct{ card, key string }{
		{"Earthlink", "f0/t1"},           // upkeep sacrifice with the focus on the Kami
		{"Pyre Zombie", "f0/a0"},         // upkeep trigger fires from the graveyard
		{"Brass Gnat", "combat#0.block"}, // optional upkeep payment declines
		{"Thelon's Chant", "f0/t1"},      // upkeep sacrifice with the focus on the Grizzly Bears
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s %s: path %q reason %q", c.card, c.key, res.Path, res.Reason)
			if res.Path != scen.PathStart || res.Start == nil {
				t.Fatalf("%s %s: path %q reason %q, want an exercised start", c.card, c.key, res.Path, res.Reason)
			}
			_ = startIsExercised(t, reg, b, c.card, c.key)
		})
	}
	// The rest of the report's keys are the same class: a CDA or X focus or
	// target, an upkeep-sacrifice focus, an Echo or cumulative-upkeep enabler,
	// an attack focus that cannot attack, and a mandatory upkeep whose Destroy
	// only reads targets — The Abyss and Crumbling Ashes are refused on the
	// battlefield (a Destroy with ValidTgts is no self-removal, so the sweep
	// keeps lower.captureStart's refusal) and exercised on the cast lane, the
	// card in hand and the play firing the trigger after the cut. A key the
	// generator already could not build keeps its named no-generator refusal;
	// any start it does write must play and exercise.
	for _, c := range []struct{ card, key string }{
		{"Soramaro, First to Dream", "f0/a0"}, // CDA */* with an empty hand
		{"Savageborn Hydra", "f0/a0"},         // X counters never placed
		{"Hooded Hydra", "f0/t0"},             // 0/0 target of Murder's cast
		{"Bloodthirsty Ogre", "f0/a1"},        // Scourge of the Skyclaves' CDA enabler
		{"Charnel Troll", "f0/a0"},            // upkeep exile a creature card
		{"Haazda Shield Mate", "f0/a0"},       // upkeep pay {W}{W}
		{"Goldenhide Ox", "cast"},             // Elephant Grass' cumulative upkeep
		{"Grimgrin, Corpse-Born", "f0/t0"},    // enters tapped and does not untap
		{"Guardians of Akrasa", "f0/t0"},      // Defender
		{"The Abyss", "f0/t0"},                // upkeep destroys only targeted creatures
		{"Crumbling Ashes", "f0/t0"},          // upkeep destroys a countered creature
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s %s: path %q reason %q", c.card, c.key, res.Path, res.Reason)
			if res.Path == scen.PathRefused {
				if res.Reason == "" {
					t.Fatalf("%s: refused without a reason, want a named refusal or an exercised start", c.card)
				}
				return
			}
			if res.Path != scen.PathStart || res.Start == nil {
				t.Fatalf("%s: path %q reason %q, want a start or a named refusal", c.card, res.Path, res.Reason)
			}
			_ = startIsExercised(t, reg, b, c.card, c.key)
		})
	}
}

// TestStartKeepsCDAFocusWhoseNeedsPlaceTheCount: a focus card whose
// characteristic-defining toughness counts a resource the start's own needs
// place (Eluge's Islands, Gaea's Liege's Forest) lives to the first main
// priority, so scen must still write a start and play must exercise it. The
// dead-on-arrival check reads the finished start, not the start before its
// needs are placed (develop wrote and exercised these; a first cut of the
// check refused them).
func TestStartKeepsCDAFocusWhoseNeedsPlaceTheCount(t *testing.T) {
	t.Parallel()
	reg, b := certdeckBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Eluge, the Shoreless Sea", "f0/t1"},
		{"Gaea's Liege", "f0/a0"},
		{"Old Man Willow", "f0/t0"},
		{"Greensleeves, Maro-Sorcerer", "f0/t0"},
		// The CDA judge cannot refuse a count the focus card joins or a
		// cross-seat count, and a numeric toughness is not a zero:
		{"Ashaya, Soul of the Wild", "static#0.1"}, // counts itself as a Forest land
		{"Harmonious Grovestrider", "f0/t0"},       // a numeric toughness of one
		{"Consuming Aberration", "f0/t0"},          // counts the opponent's graveyards
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := scen.Generate(reg, b, c.card, c.key, scen.Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s %s: path %q reason %q", c.card, c.key, res.Path, res.Reason)
			if res.Path != scen.PathStart || res.Start == nil {
				t.Fatalf("%s %s: path %q reason %q, want a start", c.card, c.key, res.Path, res.Reason)
			}
			_ = startIsExercised(t, reg, b, c.card, c.key)
		})
	}
}
