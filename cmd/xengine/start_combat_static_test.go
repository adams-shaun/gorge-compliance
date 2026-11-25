package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// keyStarts is one played start batch: its play dir, items and gorge rows.
type keyStarts struct {
	reg   *cards.Registry
	play  string
	items map[string]oraclegen.Item
	gorge map[string]rules.OracleResult
}

// lowerKeyStarts writes the start scen makes for each (card, key) into a
// directory, plays them with playStart, and returns the lowered items by card
// slug (the first seed's) with the gorge rows by item id.
func lowerKeyStarts(t *testing.T, keys [][2]string) *keyStarts {
	t.Helper()
	reg, b := certdeckBuilder(t)
	dir, out := t.TempDir(), t.TempDir()
	for _, k := range keys {
		res, err := scen.Generate(reg, b, k[0], k[1], scen.Options{Prefer: "start"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Path != scen.PathStart {
			t.Fatalf("%s %s: path %q reason %q, want a start", k[0], k[1], res.Path, res.Reason)
		}
		raw, err := res.Start.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, scen.Slug(k[0])+"-"+scen.KeyID(k[1])+".json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := playStart(reg, []string{dir}, "XP", out, false); err != nil {
		t.Fatal(err)
	}
	items, gorge := map[string]oraclegen.Item{}, map[string]rules.OracleResult{}
	if err := readLines(filepath.Join(out, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		items[it.ID] = it
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := readLines(filepath.Join(out, "gorge.jsonl"), func(b []byte) error {
		var r gorgeRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		gorge[r.ID] = r.Result
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return &keyStarts{reg: reg, play: out, items: items, gorge: gorge}
}

func stepOps(it oraclegen.Item, op string) []oraclegen.Step {
	var out []oraclegen.Step
	for _, s := range it.Steps {
		if s.Op == op {
			out = append(out, s)
		}
	}
	return out
}

// readExercised reads a play dir's exercised ledger.
func readExercised(t *testing.T, play string) *cov.Ledger {
	t.Helper()
	f, err := os.Open(filepath.Join(play, "exercised.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	led, err := cov.ReadTSV(f)
	if err != nil {
		t.Fatal(err)
	}
	return led
}

// TestPlayStartCombatStatic lowers a start of each new family and holds the
// item to the key: an attack key's item attacks with the card, a block key's
// item blocks the fixture's attacker with the card (forced, whatever the
// defender's bot would do), and a static start's item carries the comparison
// that sees its effect.
func TestPlayStartCombatStatic(t *testing.T) {
	ks := lowerKeyStarts(t, [][2]string{
		{"Magnigoth Sentry", "combat#0.attack"},
		{"Magnigoth Sentry", "combat#0.block"},
		{"Suntail Hawk", "combat#0.block"},
		{"Aggressive Mammoth", "static#0.0"},
		{"Primal Rage", "static#0.0"},
		{"Glorious Anthem", "static#0.0"},
	})
	items, gorge := ks.items, ks.gorge
	for _, id := range []string{
		"xs:XP:magnigoth-sentry:combat0.attack:s1", "xs:XP:magnigoth-sentry:combat0.block:s1", "xs:XP:suntail-hawk:combat0.block:s1",
		"xs:XP:aggressive-mammoth:static0.0:s1", "xs:XP:primal-rage:static0.0:s1", "xs:XP:glorious-anthem:static0.0:s1",
	} {
		if _, ok := items[id]; !ok {
			t.Fatalf("no lowered item %s (have %d items)", id, len(items))
		}
	}

	// The start's own key -- which no tape entry names -- is credited in the
	// exercised ledger and by keyproof's per-item credit, from the item id.
	led := readExercised(t, ks.play)
	pk, err := newPlayKeys(ks.reg, ks.play)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]classify.Use{
		"xs:XP:magnigoth-sentry:combat0.attack:s1": {Card: "Magnigoth Sentry", Key: "combat#0.attack"},
		"xs:XP:magnigoth-sentry:combat0.block:s1":  {Card: "Magnigoth Sentry", Key: "combat#0.block"},
		"xs:XP:primal-rage:static0.0:s1":           {Card: "Primal Rage", Key: "static#0.0"},
		"xs:XP:glorious-anthem:static0.0:s1":       {Card: "Glorious Anthem", Key: "static#0.0"},
	} {
		if n := led.Get(want.Card, want.Key).Exercised; n == 0 {
			t.Errorf("%s: exercised.tsv has %s %s exercised 0, want > 0", id, want.Card, want.Key)
		}
		uses, ok, err := pk.credited(items[id], id)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || !slices.Contains(uses, want) {
			t.Errorf("%s: keyproof credits %v (ok %v), want %s %s", id, uses, ok, want.Card, want.Key)
		}
	}

	atk := stepOps(items["xs:XP:magnigoth-sentry:combat0.attack:s1"], "attack")
	if len(atk) != 1 || atk[0].Seat != 0 || !slices.Equal(atk[0].Attackers, []string{"p0:Magnigoth Sentry"}) {
		t.Errorf("attack key: attack steps %+v, want seat 0 attacking with the card", atk)
	}
	for _, id := range []string{"xs:XP:magnigoth-sentry:combat0.block:s1", "xs:XP:suntail-hawk:combat0.block:s1"} {
		it := items[id]
		card := "Magnigoth Sentry"
		if id == "xs:XP:suntail-hawk:combat0.block:s1" {
			card = "Suntail Hawk"
		}
		blk := stepOps(it, "block")
		if len(blk) != 1 || blk[0].Seat != 1 || len(blk[0].Blocks) != 1 || blk[0].Blocks[0] != [2]string{"p1:" + card, "p0:Grizzly Bears"} {
			t.Errorf("%s: block steps %+v, want seat 1 blocking Grizzly Bears with %s", id, blk, card)
		}
		if a := stepOps(it, "attack"); len(a) != 1 || !slices.Equal(a[0].Attackers, []string{"p0:Grizzly Bears"}) {
			t.Errorf("%s: attack steps %+v, want the fixture attacking", id, a)
		}
	}

	// a keyword grant is seen only with the keyword comparison
	for _, id := range []string{"xs:XP:aggressive-mammoth:static0.0:s1", "xs:XP:primal-rage:static0.0:s1"} {
		it := items[id]
		if !slices.Equal(it.Compare, []string{oraclediff.CompareKeywords}) {
			t.Errorf("%s: compare %v, want [keywords]", id, it.Compare)
			continue
		}
		g, err := rules.RunOracleScenarioJSON(testutil.CorpusRegistry(t), it.Raw())
		if err != nil {
			t.Fatal(err)
		}
		// the other engine's snapshots: the same, with every Trample dropped
		x := stripKeyword(t, gorge[id].Snapshots, "Trample")
		if v := oraclediff.CompareOpts(g, nil, oraclediff.XResult{ID: id, Snapshots: x}, it.Compare); v.Status == oraclediff.Agree {
			t.Errorf("%s: a missing trample agrees under compare %v", id, it.Compare)
		}
		if v := oraclediff.CompareOpts(g, nil, oraclediff.XResult{ID: id, Snapshots: x}, nil); v.Status != oraclediff.Agree {
			t.Errorf("%s: without the keyword compare the missing trample is already seen (%s): the test shows nothing", id, v.Status)
		}
	}
	// a pump is seen in P/T: no opt-in
	if c := items["xs:XP:glorious-anthem:static0.0:s1"].Compare; len(c) != 0 {
		t.Errorf("Glorious Anthem: compare %v, want none", c)
	}
}

// TestPlayStartCombatStaticCanonicalCard: a start may name a double-faced card
// by its joined "A // B" spelling, while startDeck deals its front face and
// classify.Uses credits that front-face name. The id's declared key must be
// canonicalised the same way, or exercised.tsv and keyproof carry a second row
// for the same card and action.
func TestPlayStartCombatStaticCanonicalCard(t *testing.T) {
	const (
		joined = "Aang, at the Crossroads // Aang, Destined Savior"
		front  = "Aang, at the Crossroads"
	)
	ks := lowerKeyStarts(t, [][2]string{{joined, "f0/t1"}})
	id := scen.StartID("XP", joined, "f0/t1", 1)
	it, ok := ks.items[id]
	if !ok {
		t.Fatalf("no lowered item %s", id)
	}
	if it.Card != front {
		t.Errorf("%s: item card %q, want the canonical front face %q", id, it.Card, front)
	}
	led := readExercised(t, ks.play)
	if n := led.Get(front, "f0/t1").Exercised; n == 0 {
		t.Errorf("exercised.tsv has %s f0/t1 exercised 0, want > 0", front)
	}
	if n := led.Get(joined, "f0/t1").Exercised; n != 0 {
		t.Errorf("exercised.tsv has a second row %s f0/t1 exercised %d: the declared key is not canonicalised", joined, n)
	}
	pk, err := newPlayKeys(ks.reg, ks.play)
	if err != nil {
		t.Fatal(err)
	}
	uses, ok, err := pk.credited(it, id)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("%s: keyproof has no tape", id)
	}
	want := classify.Use{Card: front, Key: "f0/t1"}
	n := 0
	for _, u := range uses {
		if u.Key != "f0/t1" {
			continue
		}
		n++
		if u != want {
			t.Errorf("%s: keyproof credits %s %s, want %s %s", id, u.Card, u.Key, want.Card, want.Key)
		}
	}
	if n != 1 {
		t.Errorf("%s: keyproof credits %d f0/t1 keys (%v), want exactly one", id, n, uses)
	}
}

// TestKeyproofItemIDKeys holds the xs: id's one constructor/reader pair to the
// key families an item can name, and declaredKey to the item fields it needs.
// The id's keyid is the only place a static# or combat# key survives the
// lowering (no tape entry names one), so credit reads it back from there.
func TestKeyproofItemIDKeys(t *testing.T) {
	for _, c := range []struct{ id, set, slug, key, ordinal string }{
		{scen.StartID("XP", "Magnigoth Sentry", "combat#0.attack", 2), "XP", "magnigoth-sentry", "combat#0.attack", "s2"},
		{scen.StartID("XP", "Primal Rage", "static#0.0", 1), "XP", "primal-rage", "static#0.0", "s1"},
		{scen.StartID("BLB", "Thornvault Forager", "f0/a2", 1), "BLB", "thornvault-forager", "f0/a2", "s1"},
		{scen.StepsID("FDN", "Llanowar Elves", "f0/a0"), "FDN", "llanowar-elves", "f0/a0", "steps"},
		{scen.StepsID("XP", "Beza, the Bounding Spring", "cast"), "XP", "beza-the-bounding-spring", "cast", "steps"},
		{scen.StepsID("XP", "Grizzly Bears", "f1/t3"), "XP", "grizzly-bears", "f1/t3", "steps"},
	} {
		set, slug, key, ordinal, ok := scen.ParseID(c.id)
		if !ok || set != c.set || slug != c.slug || key != c.key || ordinal != c.ordinal {
			t.Errorf("ParseID(%q) = %q %q %q %q %v, want %q %q %q %q", c.id, set, slug, key, ordinal, ok, c.set, c.slug, c.key, c.ordinal)
		}
	}
	reg := testutil.CorpusRegistry(t)
	for _, c := range []struct {
		it   oraclegen.Item
		want classify.Use
		ok   bool
	}{
		{oraclegen.Item{ID: "xs:XP:magnigoth-sentry:combat0.block:s1", Card: "Magnigoth Sentry"}, classify.Use{Card: "Magnigoth Sentry", Key: "combat#0.block"}, true},
		{oraclegen.Item{ID: "xs:XP:primal-rage:static0.0:s1", Card: "Primal Rage"}, classify.Use{Card: "Primal Rage", Key: "static#0.0"}, true},
		{oraclegen.Item{ID: "xs:XP:aang-at-the-crossroads-aang-destined-savior:f0t1:s1", Card: "Aang, at the Crossroads // Aang, Destined Savior"}, classify.Use{Card: "Aang, at the Crossroads", Key: "f0/t1"}, true},
		{oraclegen.Item{ID: "xs:XP:not-a-card:cast:s1", Card: "Not a Card"}, classify.Use{Card: "Not a Card", Key: "cast"}, true},
		{oraclegen.Item{ID: "xe:XP:0:1:tape", Card: "Magnigoth Sentry"}, classify.Use{}, false},
		{oraclegen.Item{ID: "xs:XP:primal-rage:static0.0:s1"}, classify.Use{}, false},
	} {
		got, ok := declaredKey(reg, c.it)
		if ok != c.ok || got != c.want {
			t.Errorf("declaredKey(%+v) = %v %v, want %v %v", c.it, got, ok, c.want, c.ok)
		}
	}
}

// stripKeyword is snaps with keyword kw removed from every permanent.
func stripKeyword(t *testing.T, snaps []rules.OracleSnapshot, kw string) []rules.OracleSnapshot {
	t.Helper()
	b, err := json.Marshal(snaps)
	if err != nil {
		t.Fatal(err)
	}
	var out []rules.OracleSnapshot
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	n := 0
	for i := range out {
		for j := range out[i].Permanents {
			p := &out[i].Permanents[j]
			if k := slices.Index(p.Keywords, kw); k >= 0 {
				p.Keywords = slices.Delete(p.Keywords, k, k+1)
				n++
			}
		}
	}
	if n == 0 {
		t.Fatalf("no permanent has %s: the start does not show the grant", kw)
	}
	return out
}

// TestPlayStartCountedStatics lowers the starts of statics whose amount counts
// a board or the source's own counters, and holds each to its key: the batch
// plays (no duplicate counter row aborts it), the key is exercised, and a
// ReduceCost key is only castable with the reduction.
func TestPlayStartCountedStatics(t *testing.T) {
	keys := [][2]string{
		{"Boon of the Spirit Realm", "static#0.0"},
		{"Cinderslash Ravager", "static#0.0"},
		{"Hamza, Guardian of Arashin", "static#0.0"},
		{"Eluge, the Shoreless Sea", "static#0.1"},
		{"Call for Unity", "static#0.0"},
	}
	ks := lowerKeyStarts(t, keys)
	led := readExercised(t, ks.play)
	for _, k := range keys {
		if n := led.Get(k[0], k[1]).Exercised; n == 0 {
			t.Errorf("%s %s: exercised 0, want > 0", k[0], k[1])
		}
	}
}
