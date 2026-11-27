package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/dside"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// The start lane's dirac lockstep: -start accepts -dirac, dirac answers each start
// file's own scripted position on one line of dirac.jsonl, and whatever it answers
// leaves gorge's rows and the exit code alone (operator 11-24: dirac follows gorge).

// startDiracCall is one call the injected startDirac saw: enough of its arguments to
// prove dirac was handed gorge's position, and not a re-dealt game.
type startDiracCall struct {
	gameID   string
	set      string
	pair     [2]decks.Deck
	seed     uint64
	maxTurns int
	start    *rules.StartSpec
	focus    dside.StartFocus
}

// useStartDirac replaces the dirac driver for one test and puts it back after.
func useStartDirac(t *testing.T, fn func(tables *dside.Tables, gameID, set string, pair [2]decks.Deck, seed uint64, maxTurns int, start *rules.StartSpec, focus dside.StartFocus) dside.Row) {
	t.Helper()
	prev := startDirac
	startDirac = fn
	t.Cleanup(func() { startDirac = prev })
}

// diracRowsOf reads a play dir's dirac.jsonl.
func diracRowsOf(t *testing.T, out string) []dside.Row {
	t.Helper()
	var rows []dside.Row
	if err := readLines(filepath.Join(out, "dirac.jsonl"), func(b []byte) error {
		var r dside.Row
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		rows = append(rows, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}

// readFileBytes is a file's bytes, or "" when it does not exist.
func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestKernelMirroredFieldsExist holds the two lists against both seat structs, in both
// directions: no list names a field neither struct has (a list running ahead of the
// kernel it was read from is a stale claim on kernelStart), and every field either
// struct does have is named by one of the lists. A field gorge adds to
// rules.StartSeat, or the corpus lane adds to a start file's seat, is this test's
// failure — the one that sends someone to read dirac/oracle kernelStart and name the
// field mirrored or refused — rather than a field silently riding into the lockstep
// unseen by the kernel, reported as dirac disagreeing with gorge.
func TestKernelMirroredFieldsExist(t *testing.T) {
	t.Parallel()
	structs := []struct {
		name string
		typ  reflect.Type
	}{
		{"rules.StartSeat", reflect.TypeOf(rules.StartSeat{})},
		{"startSeat", reflect.TypeOf(startSeat{})},
	}
	have := map[string]bool{}
	for _, s := range structs {
		for j := 0; j < s.typ.NumField(); j++ {
			have[s.typ.Field(j).Name] = true
		}
	}
	for name := range kernelMirrored {
		if !have[name] {
			t.Errorf("kernelMirrored names %s, which no seat struct has", name)
		}
	}
	for name := range kernelRefused {
		if !have[name] {
			t.Errorf("kernelRefused names %s, which no seat struct has", name)
		}
	}
	for _, s := range structs {
		for j := 0; j < s.typ.NumField(); j++ {
			name := s.typ.Field(j).Name
			if !kernelMirrored[name] && kernelRefused[name] == "" {
				t.Errorf("%s has %s, which neither list names: read dirac/oracle kernelStart and put it in one of them", s.name, name)
			}
		}
	}
}

// TestStartFlagErrAcceptsDirac: -start plays a scripted position, so every flag that
// picks another way to build the game stays refused - but -dirac is not one of them:
// dirac follows gorge and needs nothing of the deal -start replaces.
func TestStartFlagErrAcceptsDirac(t *testing.T) {
	if err := startFlagErr("XP", "", "", "", true, false, "", "", ""); err != nil {
		t.Errorf("-start -dirac: %v, want accepted", err)
	}
	// every flag that names a deck source or another kind of run is still refused,
	// by name, and -set is still required.
	refused := []struct {
		name string
		err  error
	}{
		{"-random", startFlagErr("XP", "FDN", "", "", true, false, "", "", "")},
		{"-pool", startFlagErr("XP", "", "p.txt", "", true, false, "", "", "")},
		{"-decks", startFlagErr("XP", "", "", "decks", true, false, "", "", "")},
		{"-windows", startFlagErr("XP", "", "", "", true, true, "", "", "")},
		{"-tape-turns", startFlagErr("XP", "", "", "", true, false, "2", "", "")},
		{"-format", startFlagErr("XP", "", "", "", true, false, "", "commander", "")},
		{"-forge-only", startFlagErr("XP", "", "", "", true, false, "", "", "extra.txt")},
		{"-set", startFlagErr("", "", "", "", true, false, "", "", "")},
	}
	for _, c := range refused {
		if c.err == nil {
			t.Errorf("%s with -start: accepted, want refused", c.name)
			continue
		}
		if c.name != "-set" && !strings.Contains(c.err.Error(), c.name) {
			t.Errorf("%s with -start: %q does not name the flag", c.name, c.err)
		}
	}
}

// TestStartDiracLockstepRows: with -dirac each start file gets one dirac.jsonl row,
// keyed by the game id its items share, from the lockstep of the file's own scripted
// position - the same two decks, the file's first seed and the same rules.StartSpec
// gorge opened in - and the row answers every item of the file (agree, or
// not_applicable with the reason when dirac cannot hold the position).
func TestStartDiracLockstepRows(t *testing.T) {
	reg := testutil.CorpusRegistry(t) // skips without the corpus
	const wantGame = "xs:BLB:thornvault-forager:f0a2"

	t.Run("agree", func(t *testing.T) {
		var got []startDiracCall
		useStartDirac(t, func(tables *dside.Tables, gameID, set string, pair [2]decks.Deck, seed uint64, maxTurns int, start *rules.StartSpec, focus dside.StartFocus) dside.Row {
			got = append(got, startDiracCall{gameID: gameID, set: set, pair: pair, seed: seed, maxTurns: maxTurns, start: start, focus: focus})
			return dside.Row{Game: gameID, Status: dside.StatusClean, Table: "std-blb", Decisions: 12, Intents: 12, FocusTook: true}
		})
		out := t.TempDir()
		if err := playStartDirac(reg, []string{"testdata/start"}, "BLB", out, false, dside.NewTables(reg)); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("dirac played %d games, want one per seed gorge played (2)", len(got))
		}
		rows := diracRowsOf(t, out)
		if len(rows) != 1 {
			t.Fatalf("dirac.jsonl has %d rows, want 1 (one per key)", len(rows))
		}
		if rows[0].Game != wantGame {
			t.Errorf("row game %q, want %q (the prefix of the file's item ids)", rows[0].Game, wantGame)
		}
		s1 := rules.SeedForStarter(1, 2, 0)
		seeds := []uint64{s1, rules.SeedForStarter(s1+1, 2, 0)}
		for i, c := range got {
			// dirac played gorge's position and focus play, not a game it dealt itself.
			if c.gameID != wantGame || c.set != "BLB" {
				t.Errorf("call %d: game %q set %q", i, c.gameID, c.set)
			}
			if c.pair[0].Name != "start-p0" || c.pair[1].Name != "start-p1" {
				t.Errorf("call %d: decks %q %q, want the start file's own", i, c.pair[0].Name, c.pair[1].Name)
			}
			for j, d := range c.pair {
				if len(d.Cards) != startDeckSize {
					t.Errorf("call %d: seat %d's deck holds %d cards, want %d", i, j, len(d.Cards), startDeckSize)
				}
			}
			if want := seeds[i]; c.seed != want {
				t.Errorf("call %d: seed %d, want the file's game seed %d", i, c.seed, want)
			}
			if c.maxTurns != 2 { // the file's max_turns 1, plus the watchdog turn
				t.Errorf("call %d: %d turns, want 2 (the file's max_turns plus one)", i, c.maxTurns)
			}
			if c.start == nil || len(c.start.Seats) != 2 {
				t.Fatalf("call %d: start %+v, want the file's two-seat position", i, c.start)
			}
			if len(c.start.Seats[0].Battlefield) != 5 || len(c.start.Seats[1].Battlefield) != 0 {
				t.Errorf("call %d: position seats %v / %v, want gorge's 5 permanents beside none",
					i, c.start.Seats[0].Battlefield, c.start.Seats[1].Battlefield)
			}
			if len(c.start.Seats[0].Library) != 1 {
				t.Errorf("call %d: p0 library %v, want the file's one listed card", i, c.start.Seats[0].Library)
			}
			if want := (dside.StartFocus{Seat: 0, Card: "Thornvault Forager", Key: "f0/a2", Kind: "ability"}); c.focus != want {
				t.Errorf("call %d: focus %+v, want gorge's %+v", i, c.focus, want)
			}
		}
		// Every item of the file reads dirac's answer for the key.
		for _, s := range []int{1, 2} {
			if a := rows[0].Answer(scen.StartID("BLB", "Thornvault Forager", "f0/a2", s)); a != dside.Agree {
				t.Errorf("seed %d: answer %q, want %q", s, a, dside.Agree)
			}
		}
	})

	t.Run("a seed that diverges is the key's row", func(t *testing.T) {
		// seed 2 diverges where seed 1 agreed: the row must not read agree.
		n := 0
		useStartDirac(t, func(tables *dside.Tables, gameID, set string, pair [2]decks.Deck, seed uint64, maxTurns int, start *rules.StartSpec, focus dside.StartFocus) dside.Row {
			n++
			if n == 1 {
				return dside.Row{Game: gameID, Status: dside.StatusClean, Decisions: 9, FocusTook: true}
			}
			return dside.Row{Game: gameID, Status: dside.StatusDiverged, Field: "Life[0]", Diff: []string{"Life[0] 20 != 19"}}
		})
		out := t.TempDir()
		if err := playStartDirac(reg, []string{"testdata/start"}, "BLB", out, false, dside.NewTables(reg)); err != nil {
			t.Fatal(err)
		}
		rows := diracRowsOf(t, out)
		if len(rows) != 1 || rows[0].Status != dside.StatusDiverged {
			t.Fatalf("rows %+v, want the one diverged row", rows)
		}
		if a := rows[0].Answer(scen.StartID("BLB", "Thornvault Forager", "f0/a2", 1)); a != dside.Diverge {
			t.Errorf("answer %q, want %q", a, dside.Diverge)
		}
	})

	t.Run("a key gorge never played is not an agreement", func(t *testing.T) {
		// the focus play is never offered (the only ability's source is in the
		// graveyard): gorge refuses the start as not exercised, so dirac has no
		// game to agree with and must not be asked.
		dir := t.TempDir()
		raw := `{"format":"xproof-start-1","card":"Thornvault Forager","key":"f0/a2","players":2,"active":0,"max_turns":1,
 "setup":{"p0":{"graveyard":["Thornvault Forager"],"battlefield":["Forest"]},"p1":{}},"filler":"basics","seeds":2}`
		if err := os.WriteFile(filepath.Join(dir, "x.json"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		asked := 0
		useStartDirac(t, func(tables *dside.Tables, gameID, set string, pair [2]decks.Deck, seed uint64, maxTurns int, start *rules.StartSpec, focus dside.StartFocus) dside.Row {
			asked++
			return dside.Row{Game: gameID, Status: dside.StatusClean}
		})
		out := t.TempDir()
		if err := playStartDirac(reg, []string{dir}, "BLB", out, false, dside.NewTables(reg)); err != nil {
			t.Fatal(err)
		}
		rows := diracRowsOf(t, out)
		if len(rows) != 1 || rows[0].Status != dside.StatusNA || !strings.Contains(rows[0].Reason, "not played by gorge") {
			t.Fatalf("rows %+v (dirac asked %d times), want one not_applicable row saying gorge did not play it", rows, asked)
		}
		if asked != 0 {
			t.Errorf("dirac was asked about %d seeds of a key gorge refused", asked)
		}
	})

	t.Run("refused", func(t *testing.T) {
		useStartDirac(t, func(tables *dside.Tables, gameID, set string, pair [2]decks.Deck, seed uint64, maxTurns int, start *rules.StartSpec, focus dside.StartFocus) dside.Row {
			return dside.NotApplicable(gameID, "table std-blb: card not held")
		})
		out := t.TempDir()
		if err := playStartDirac(reg, []string{"testdata/start"}, "BLB", out, false, dside.NewTables(reg)); err != nil {
			t.Fatal(err)
		}
		rows := diracRowsOf(t, out)
		if len(rows) != 1 || rows[0].Status != dside.StatusNA {
			t.Fatalf("rows %+v, want one not_applicable row", rows)
		}
		if !strings.Contains(rows[0].Reason, "card not held") {
			t.Errorf("refusal reason %q, want the table's", rows[0].Reason)
		}
		if a := rows[0].Answer(scen.StartID("BLB", "Thornvault Forager", "f0/a2", 1)); a != dside.NotAppl {
			t.Errorf("answer %q, want %q", a, dside.NotAppl)
		}
	})

	t.Run("not expressible", func(t *testing.T) {
		// A position dirac's kernel cannot mirror is refused by name before the
		// table is even asked, so the row never blames a missing injection on
		// dirac. These need no built table.
		cases := []struct {
			name string
			seat string // seat p0's JSON, as a start file spells it
		}{
			{"player counters", `{"player_counters":[{"kind":"ENERGY","n":2}]}`},
			{"tokens", `{"tokens":[{"script":"treasure"}]}`},
			{"commanders", `{"commanders":["Thornvault Forager"],"battlefield":["Thornvault Forager"]}`},
		}
		for _, c := range cases {
			f := &startFile{}
			raw := `{"format":"xproof-start-1","card":"Thornvault Forager","key":"f0/a2","players":2,` +
				`"filler":"basics","seeds":1,"setup":{"p0":` + c.seat + `,"p1":{}}}`
			if err := json.Unmarshal([]byte(raw), f); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			why := startNotExpressible(f, &rules.StartSpec{})
			if !strings.HasPrefix(why, "start not expressible: ") {
				t.Errorf("%s: reason %q, want a start not expressible", c.name, why)
			}
			if !strings.Contains(why, c.name) {
				t.Errorf("%s: reason %q does not name what dirac cannot mirror", c.name, why)
			}
			if g := startGameID("BLB", f); g != "xs:BLB:thornvault-forager:f0a2" {
				t.Errorf("%s: game id %q", c.name, g)
			}
		}
	})
}

// TestStartDiracDivergenceNeverFails: a diverged row is the first differing
// decision on the record, and gorge's items, rows and exit code are byte for byte
// what they were with no dirac at all.
func TestStartDiracDivergenceNeverFails(t *testing.T) {
	reg := testutil.CorpusRegistry(t) // skips without the corpus
	baseline := t.TempDir()
	if err := playStartDirac(reg, []string{"testdata/start"}, "XP", baseline, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(baseline, "dirac.jsonl")); err == nil {
		t.Fatal("a play without -dirac wrote dirac.jsonl")
	}

	out := t.TempDir()
	useStartDirac(t, func(tables *dside.Tables, gameID, set string, pair [2]decks.Deck, seed uint64, maxTurns int, start *rules.StartSpec, focus dside.StartFocus) dside.Row {
		seat := 1
		return dside.Row{Game: gameID, Status: dside.StatusDiverged, Table: "std-xp",
			Decisions: 7, What: "decision", Field: "p0.life", Diff: []string{"p0.life 20 != 19"},
			FromTurn: 1, ToTurn: 1, Seat: &seat, Kind: "priority"}
	})
	if err := playStartDirac(reg, []string{"testdata/start"}, "XP", out, false, dside.NewTables(reg)); err != nil {
		t.Fatalf("dirac diverged and the run failed: %v", err)
	}
	for _, name := range []string{"items.jsonl", "gorge.jsonl", "lower.jsonl", "exercised.tsv"} {
		if a, b := readFileBytes(t, filepath.Join(baseline, name)), readFileBytes(t, filepath.Join(out, name)); !bytes.Equal(a, b) {
			t.Errorf("%s differs from the run with no dirac: dirac changed gorge's verdict", name)
		}
	}
	rows := diracRowsOf(t, out)
	if len(rows) != 1 {
		t.Fatalf("dirac.jsonl has %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Status != dside.StatusDiverged || r.Field != "p0.life" || len(r.Diff) != 1 {
		t.Fatalf("row %+v, want the divergence with its first differing decision", r)
	}
	if r.FromTurn != 1 || r.Seat == nil || *r.Seat != 1 || r.Kind != "priority" {
		t.Errorf("row %+v: the first differing decision names no turn, seat and kind", r)
	}
	if r.Answer(scen.StartID("XP", "Thornvault Forager", "f0/a2", 1)) != dside.Diverge {
		t.Errorf("answer %q, want %q", r.Answer(scen.StartID("XP", "Thornvault Forager", "f0/a2", 1)), dside.Diverge)
	}

	// The flag itself reaches the same place through runPlay, and exits 0.
	run := t.TempDir()
	if err := runPlay([]string{"-cards", "../../.cards", "-set", "XP", "-start", "testdata/start", "-dirac", "-out", run}); err != nil {
		t.Fatalf("play -start -dirac: %v", err)
	}
	if _, err := os.Stat(filepath.Join(run, "dirac.jsonl")); err != nil {
		t.Fatalf("play -start -dirac wrote no dirac.jsonl: %v", err)
	}
}

// TestStartDiracRealLockstep runs the real lockstep (no double) over the in-tree start
// file on BLB's table: dirac plays the file's scripted position with the focus seat
// taking the key's ability, and the row records that the key was reached.
func TestStartDiracRealLockstep(t *testing.T) {
	reg := testutil.CorpusRegistry(t) // skips without the corpus
	tables := dside.NewTables(reg)
	if _, err := tables.Table("BLB"); err != nil {
		if dside.NoTables(err) {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := playStartDirac(reg, []string{"testdata/start"}, "BLB", out, false, tables); err != nil {
		t.Fatal(err)
	}
	rows := diracRowsOf(t, out)
	if len(rows) != 1 {
		t.Fatalf("dirac.jsonl has %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Status != dside.StatusClean || r.Decisions <= 0 || !r.FocusTook {
		t.Errorf("row %+v, want a clean game that took the focus play", r)
	}
	if a := r.Answer(scen.StartID("BLB", "Thornvault Forager", "f0/a2", 2)); a != dside.Agree {
		t.Errorf("answer %q, want %q", a, dside.Agree)
	}
}
