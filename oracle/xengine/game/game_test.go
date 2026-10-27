package game

import (
	"bytes"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const manifestDir = "../../compliance/manifests"

// fdnPairs is n FDN deck pairs, deterministic.
func fdnPairs(t *testing.T, n int) (*cards.Registry, [][2]decks.Deck) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cands, _, err := decks.Candidates(reg, manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	var out [][2]decks.Deck
	for g := 0; g < n; g++ {
		out = append(out, decks.Pair(cands, cov.New(), "FDN", 0, g, 7))
	}
	return reg, out
}

func play(t *testing.T, reg *cards.Registry, p [2]decks.Deck, seed uint64, opt Options) *Tape {
	t.Helper()
	tp, err := Play(reg, p, seed, opt)
	if err != nil {
		t.Fatal(err)
	}
	return tp
}

func TestPlayDeterministic(t *testing.T) {
	reg, pairs := fdnPairs(t, 2)
	for i, p := range pairs {
		a, err := play(t, reg, p, uint64(100+i), Options{Set: "FDN"}).Bytes()
		if err != nil {
			t.Fatal(err)
		}
		b, err := play(t, reg, p, uint64(100+i), Options{Set: "FDN"}).Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			ta, _ := ReadTape(bytes.NewReader(a))
			tb, _ := ReadTape(bytes.NewReader(b))
			for j := range ta.Entries {
				if j >= len(tb.Entries) {
					t.Fatalf("pair %d: second tape ends at entry %d", i, j)
				}
				if d := ta.Entries[j].Digest.Diff(tb.Entries[j].Digest); d != nil {
					t.Fatalf("pair %d: first differing entry %d: %v", i, j, d)
				}
			}
			t.Fatalf("pair %d: tapes differ (%d vs %d bytes)", i, len(a), len(b))
		}
		tp, err := ReadTape(bytes.NewReader(a))
		if err != nil {
			t.Fatal(err)
		}
		c, _ := tp.Bytes()
		if !bytes.Equal(a, c) {
			t.Errorf("pair %d: tape does not round-trip through ReadTape", i)
		}
		if len(tp.Entries) == 0 || tp.Outcome.Intents == 0 {
			t.Errorf("pair %d: empty tape", i)
		}
		for j, en := range tp.Entries {
			if en.Reanswered {
				t.Errorf("pair %d entry %d: the engine refused the seat's intent", i, j)
			}
		}
	}
}

// hasManaCost reports whether the named card's front face has a mana cost.
func hasManaCost(reg *cards.Registry, name string) bool {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return false
	}
	mc := c.Faces[0].ManaCost
	return mc != "" && mc != "no cost" && mc != "0"
}

// castName is the card a cast pick names: the pick carries an object id, not
// a name, so its "Cast <name>" label is matched against the deck.
func castName(p Pick, deck []string) string {
	for _, n := range deck {
		if strings.Contains(p.Label, n) {
			return n
		}
	}
	return ""
}

func TestTapeRecordsManaTaps(t *testing.T) {
	reg, pairs := fdnPairs(t, 4)
	casts, withMana, landTurnMana := 0, 0, 0
	for i, p := range pairs {
		tp := play(t, reg, p, uint64(200+i), Options{Set: "FDN"})
		for j, en := range tp.Entries {
			var pk *Pick
			for k := range en.Picks {
				if en.Picks[k].Kind == "cast" {
					pk = &en.Picks[k]
				}
			}
			switch {
			case pk != nil && pk.Mode == "" && pk.AltCost == 0 && en.Announce == "":
				name := castName(*pk, tp.Decks[en.Seat].Cards)
				if name == "" || !hasManaCost(reg, name) {
					continue
				}
				casts++
				// A targeted or modal cast is paid when its last choice is
				// committed (CR 601.2), so the cast's mana sits on the
				// entry that finishes it: scan the cast's own entry and the
				// non-priority decisions that follow it.
				mana := len(en.Mana) > 0
				for _, pl := range en.Digest.Players {
					mana = mana || pl.Pool != ""
				}
				for k := j + 1; !mana && k < len(tp.Entries) && tp.Entries[k].Kind != "priority"; k++ {
					mana = len(tp.Entries[k].Mana) > 0
				}
				if !mana {
					t.Errorf("pair %d entry %d: cast of %s has no mana record and an empty pool", i, j, name)
				} else {
					withMana++
				}
			case len(en.Picks) == 1 && (en.Picks[0].Kind == "play_land" || en.Picks[0].Kind == "pass") && en.Payment == "":
				if len(en.Mana) != 0 {
					landTurnMana++
					t.Errorf("pair %d entry %d: %s fired mana %v", i, j, en.Picks[0].Kind, en.Mana)
				}
			}
		}
	}
	if casts == 0 {
		t.Fatal("no mana-cost cast in 4 games: the test checks nothing")
	}
	t.Logf("%d casts, %d with a mana record or pool; %d land/pass entries with mana", casts, withMana, landTurnMana)
}

func TestPlayStallReturnsTape(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	tp, err := Play(reg, pairs[0], 5, Options{MaxTurns: 2})
	if err != nil {
		t.Fatal(err)
	}
	if tp.Outcome.Stall != "turns" || len(tp.Entries) == 0 {
		t.Fatalf("stall %q with %d entries, want turns with a tape", tp.Outcome.Stall, len(tp.Entries))
	}
	if tp.Outcome.Winner != nil {
		t.Error("a stalled game has a winner")
	}
	tp, err = Play(reg, pairs[0], 5, Options{MaxIntents: 10})
	if err != nil || tp.Outcome.Stall != "intents" || len(tp.Entries) != 10 {
		t.Fatalf("intent cap: err %v stall %q entries %d", err, tp.Outcome.Stall, len(tp.Entries))
	}
}

func TestExploreUsesAbilities(t *testing.T) {
	reg, pairs := fdnPairs(t, 6)
	n := 0
	for i, p := range pairs {
		for _, en := range play(t, reg, p, uint64(300+i), Options{Set: "FDN"}).Entries {
			for _, pk := range en.Picks {
				if pk.Kind == "ability" {
					n++
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no non-mana ability option picked in 6 explore games")
	}
	t.Logf("%d ability picks", n)
}

// TestDigestOfSnapshotMatchesGame: the runner does not expose its engine, so
// the same state cannot be built both ways from outside gorge. Instead (a)
// a hand-built snapshot with a creature carrying a counter, an Aura attached
// to it, and a token fills every Digest field, and (b) a real scenario run
// through rules.RunOracleScenarioJSON digests to the expected board.
func TestDigestOfSnapshotMatchesGame(t *testing.T) {
	w := 1
	snap := rules.OracleSnapshot{
		Turn: 3, Step: "main1", Active: 1, Priority: 1, Winner: &w,
		Players: []rules.OracleSnapPlayer{
			{Seat: 0, Life: 17, Counters: map[string]int32{"POISON": 2}, Hand: []string{"Island", "Shock"},
				Graveyard: []string{"Opt", "Duress"}, Exile: []string{"A", "B"}, LibraryCount: 30,
				LibraryTop: []string{"a", "b", "c", "d", "e"}, Pool: "UU"},
			{Seat: 1, Life: 20, Hand: []string{}, Graveyard: []string{}, Exile: []string{}, LibraryCount: 33, LibraryTop: []string{}},
		},
		Permanents: []rules.OracleSnapPerm{
			{Ref: "p1:Grizzly Bears", Name: "Grizzly Bears", Controller: 1, Owner: 1, Tapped: true,
				PT: "3/3", Damage: 1, Counters: map[string]int32{"P1P1": 1}},
			{Ref: "p0:Pacifism", Name: "Pacifism", Controller: 0, Owner: 0, AttachedTo: "p1:Grizzly Bears"},
			{Ref: "p0:token:Soldier#2", Name: "Soldier", Controller: 0, Owner: 0, Token: true, PT: "1/1"},
		},
	}
	d := DigestOfSnapshot(snap)
	want := Digest{
		Turn: 3, Step: "main1", Active: 1, Priority: 1,
		Players: []PlayerDigest{
			{Seat: 0, Life: 17, Counters: map[string]int32{"POISON": 2}, Hand: []string{"Island", "Shock"},
				Graveyard: []string{"Opt", "Duress"}, Exile: []string{"A", "B"}, LibraryCount: 30,
				LibraryTop: []string{"a", "b", "c", "d", "e"}, Pool: "UU"},
			{Seat: 1, Life: 20, Hand: []string{}, Graveyard: []string{}, Exile: []string{}, LibraryCount: 33, LibraryTop: []string{}},
		},
		Field: []PermDigest{
			{Name: "Pacifism", Controller: 0, Owner: 0, AttachedTo: "Grizzly Bears"},
			{Name: "Soldier", Controller: 0, Owner: 0, Token: true, PT: "1/1"},
			{Name: "Grizzly Bears", Controller: 1, Owner: 1, Tapped: true, PT: "3/3", Damage: 1, Counters: map[string]int32{"P1P1": 1}},
		},
	}
	if diff := want.Diff(d); diff != nil {
		t.Fatalf("snapshot digest differs: %v", diff)
	}
	if diff := d.Diff(d); diff != nil {
		t.Fatalf("a digest differs from itself: %v", diff)
	}
	moved := d
	moved.Players = append([]PlayerDigest(nil), d.Players...)
	moved.Players[1].Life = 19
	if diff := d.Diff(moved); len(diff) != 1 || !strings.HasPrefix(diff[0], "players[1].life:") {
		t.Errorf("life change diff = %v", diff)
	}

	reg := testutil.CorpusRegistry(t)
	res, err := rules.RunOracleScenarioJSON(reg, []byte(`{
 "name": "digest-board", "cr": ["110.1"], "why": "two permanents on the battlefield",
 "setup": {"p0": {"battlefield": ["Grizzly Bears"]}, "p1": {"battlefield": ["Forest"]}},
 "steps": [], "expect": []}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) == 0 {
		t.Fatalf("no snapshots; fails %v", res.Fails)
	}
	got := DigestOfSnapshot(res.Snapshots[len(res.Snapshots)-1])
	if len(got.Players) != 2 || len(got.Field) != 2 {
		t.Fatalf("digest has %d players, %d permanents: %+v", len(got.Players), len(got.Field), got)
	}
	if f := got.Field; f[0].Name != "Grizzly Bears" || f[0].Controller != 0 || f[0].PT != "2/2" || f[1].Name != "Forest" || f[1].Controller != 1 || f[1].PT != "" {
		t.Errorf("scenario field = %+v", f)
	}
}
