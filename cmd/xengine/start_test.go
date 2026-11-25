package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/engine/cards"
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

// TestStartToken: a start file's token scripts map to gorge's scripted tokens
// (rules.StartSeat.Tokens), and an unknown script is refused rather than
// starting without the token the key needs.
func TestStartToken(t *testing.T) {
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
// literal All/Any the followers refuse, and sit on the opponent's creature or
// planeswalker.
func TestStartCounterBearer(t *testing.T) {
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

// TestStartRefusesUnofferedFocus: a start whose focus seat is never offered the
// focus play is refused, not written. Burning Oil targets an attacking or
// blocking creature and nothing attacks at main 1, so the cast is never offered
// at any priority; Shock off the same lands is offered and cast, and its games
// are written.
func TestStartRefusesUnofferedFocus(t *testing.T) {
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

// TestStartTransformingDFC: a start scen yields for a transforming double-faced
// card loads in play. scen lists the card on a battlefield, tapped or as a
// counter bearer by its joined name "A // B" while startDeck deals its front
// face, so binding a tapped/counter entry must resolve the name the same way.
func TestStartTransformingDFC(t *testing.T) {
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

// startIsExercised generates the start file for card's key, plays it, and
// fails when play refuses it as start-not-exercised or writes no item; it
// returns the items written.
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
	if len(items) == 0 {
		t.Errorf("no item written (lowering %v; start %s)", refused, raw)
	}
	return items
}

// playStartFile plays a generated start for both seeds and returns its items,
// lowering rows and the rendered start. A play error is fatal.
func playStartFile(t *testing.T, reg *cards.Registry, st *scen.StartFile) ([]oraclegen.Item, []lowerRow, []byte) {
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
	return items, rows, raw
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

// TestStartStackTargetIsExercised: a focus play that targets a spell was
// refused as start:needs-stack-target, because a start holds at main 1 with an
// empty stack. The start now scripts the opponent's bait cast (Focus.Bait): the
// opponent casts it at main 1, then the focus seat casts its counterspell with
// the bait as its target.
func TestStartStackTargetIsExercised(t *testing.T) {
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

// TestStartStackTargetCostIsExercised: an alternate additional cost
// ("behold a Jace or pay {1}", "tap an artifact or pay {1}") is paid in
// addition to the mana cost, and the generated start placed only the printed
// cost's lands. With a bait on the stack the focus cast was never offered and
// play refused both seeds as start-not-exercised. The mana fallback now gets
// its lands too.
func TestStartStackTargetCostIsExercised(t *testing.T) {
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

// TestStartStackTargetCensusIsExercised: every generated stack-target start in
// the corpus must have its focus play offered on both seeds. This is the guard
// for the class of regression where the bait was accepted for a spell the
// focus play could never target (TargetValidTargeting$, a bait cast from the
// hand against "wasn't cast from hand", a sorcery-speed focus behind the
// bait): xengine play refused those starts as start-not-exercised, so the key
// could never be proven even though a start file existed.
//
// It walks every card whose script declares a stack target, generates each
// key, and plays each start that carries a bait. Cards whose start lowers to a
// different refusal (an unscripted upkeep trigger) are not this class: their
// focus play was offered, and the census requires only that no seed is refused
// as start-not-exercised.
func TestStartStackTargetCensusIsExercised(t *testing.T) {
	reg, b := certdeckBuilder(t)
	var cards []string
	for _, c := range reg.AllCards() {
		if len(c.Faces) == 0 || !b.Playable(c) {
			continue
		}
		for _, f := range c.Faces {
			stackTarget := false
			for _, a := range f.Abilities {
				if strings.Contains(a.Params["TargetType"], "Spell") || a.Params["TargetValidTargeting"] != "" {
					stackTarget = true
				}
			}
			for _, v := range f.SVars {
				if strings.Contains(v, "TargetType$") && strings.Contains(v, "Spell") {
					stackTarget = true
				}
			}
			if stackTarget {
				cards = append(cards, f.Name)
				break
			}
		}
	}
	sort.Strings(cards)
	keys, refused, noBait, wrote, exercised := 0, 0, 0, 0, 0
	for _, card := range cards {
		c, _ := reg.Lookup(card)
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
			if res.Start.Focus == nil || res.Start.Focus.Bait == "" {
				noBait++ // a start with no bait (an ability target, or no stack target)
				continue
			}
			wrote++
			_, rows, raw := playStartFile(t, reg, res.Start)
			bad := false
			for _, r := range rows {
				if slices.Contains(r.Reasons, reasonStartNotExercised) {
					bad = true
					t.Errorf("%s %s: %s (start %s)", card, key, reasonStartNotExercised, raw)
				}
			}
			if !bad {
				exercised++
			}
		}
	}
	t.Logf("stack-target census: cards=%d keys=%d refused=%d no-bait-start=%d wrote-with-bait=%d exercised=%d start-not-exercised=%d",
		len(cards), keys, refused, noBait, wrote, exercised, wrote-exercised)
}

// TestStartMultiFacedFocusIsExercised: a start names a multi-faced card as its
// card file does ("Akoum Warrior // Akoum Teeth") while the game object's face
// carries the front name alone, and the focus seat compared the two as
// written: no object was ever a focus object, so the focus play of every
// transforming, modal and adventure card went untaken and play refused the
// start as start-not-exercised (259 of the 792 such keys at bf707e46b1).
func TestStartMultiFacedFocusIsExercised(t *testing.T) {
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

// TestStartBackFaceLostIsRefused: a back-face start whose card turns to its
// front before the recorded span (Doric's back face transforms it at the
// upkeep) is refused as start-face-lost, writes no item and credits nothing:
// the tape plays the front face, so the back-face key was never exercised.
func TestStartBackFaceLostIsRefused(t *testing.T) {
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

// TestStartTwoTargetsIsExercised: a play that names two targets was met by one
// permanent (Switcheroo's "two target creatures", Peace and Quiet's two target
// enchantments), and a target only the focus seat's own permanent meets (a
// fight spell's "target creature you control") was never placed, so the play
// was never offered: play refused the start as start-not-exercised, or scen as
// start:needs-target. A target of n permanents now needs n distinct ones, and
// the most inert carrier of an own-side target's type goes on the focus seat's
// side.
func TestStartTwoTargetsIsExercised(t *testing.T) {
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

// TestStartCombatTriggerIsExercised: a combat trigger (blocks, becomes blocked,
// combat damage) had no start (no-generator:trigger:<Mode>), so no engine ever
// played the key. The start now scripts the combat (StartFocus.Attack, Block):
// every item has the attack step, and the block step when the trigger reads one.
func TestStartCombatTriggerIsExercised(t *testing.T) {
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
