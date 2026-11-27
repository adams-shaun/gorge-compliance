package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
}

// useStartDirac replaces the dirac driver for one test and puts it back after.
func useStartDirac(t *testing.T, fn func(tables *dside.Tables, gameID, set string, pair [2]decks.Deck, seed uint64, maxTurns int, start *rules.StartSpec) dside.Row) {
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
		var got startDiracCall
		calls := 0
		useStartDirac(t, func(tables *dside.Tables, gameID, set string, pair [2]decks.Deck, seed uint64, maxTurns int, start *rules.StartSpec) dside.Row {
			got = startDiracCall{gameID: gameID, set: set, pair: pair, seed: seed, maxTurns: maxTurns, start: start}
			calls++
			return dside.Row{Game: gameID, Status: dside.StatusClean, Table: "std-blb", Decisions: 12, Intents: 12}
		})
		out := t.TempDir()
		if err := playStartDirac(reg, []string{"testdata/start"}, "BLB", out, false, dside.NewTables(reg)); err != nil {
			t.Fatal(err)
		}
		if calls != 1 {
			t.Fatalf("dirac answered %d starts, want one row per start file", calls)
		}
		rows := diracRowsOf(t, out)
		if len(rows) != 1 {
			t.Fatalf("dirac.jsonl has %d rows, want 1", len(rows))
		}
		if rows[0].Game != wantGame {
			t.Errorf("row game %q, want %q (the prefix of the file's item ids)", rows[0].Game, wantGame)
		}
		// dirac played gorge's position, not a game it dealt itself.
		if got.gameID != wantGame || got.set != "BLB" {
			t.Errorf("dirac called with game %q set %q", got.gameID, got.set)
		}
		if got.pair[0].Name != "start-p0" || got.pair[1].Name != "start-p1" {
			t.Errorf("dirac played decks %q %q, want the start file's own", got.pair[0].Name, got.pair[1].Name)
		}
		for i, d := range got.pair {
			if len(d.Cards) != startDeckSize {
				t.Errorf("seat %d's deck holds %d cards, want %d", i, len(d.Cards), startDeckSize)
			}
		}
		if got.seed != rules.SeedForStarter(1, 2, 0) {
			t.Errorf("dirac played seed %d, want the file's first game seed %d", got.seed, rules.SeedForStarter(1, 2, 0))
		}
		if got.maxTurns != 2 { // the file's max_turns 1, plus the watchdog turn
			t.Errorf("dirac played %d turns, want 2 (the file's max_turns plus one)", got.maxTurns)
		}
		if got.start == nil || len(got.start.Seats) != 2 {
			t.Fatalf("dirac got start %+v, want the file's two-seat position", got.start)
		}
		if len(got.start.Seats[0].Battlefield) != 5 || len(got.start.Seats[1].Battlefield) != 0 {
			t.Errorf("dirac's position seats %v / %v, want gorge's 5 permanents beside none",
				got.start.Seats[0].Battlefield, got.start.Seats[1].Battlefield)
		}
		if len(got.start.Seats[0].Library) != 1 {
			t.Errorf("dirac's p0 library %v, want the file's one listed card", got.start.Seats[0].Library)
		}
		// Every item of the file reads dirac's answer for the game it was lowered from.
		for _, s := range []int{1, 2} {
			if a := rows[0].Answer(scen.StartID("BLB", "Thornvault Forager", "f0/a2", s)); a != dside.Agree {
				t.Errorf("seed %d: answer %q, want %q", s, a, dside.Agree)
			}
		}
	})

	t.Run("refused", func(t *testing.T) {
		useStartDirac(t, func(tables *dside.Tables, gameID, set string, pair [2]decks.Deck, seed uint64, maxTurns int, start *rules.StartSpec) dside.Row {
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
			row := startDiracRow(dside.NewTables(reg), "BLB", f,
				[2]decks.Deck{{Name: "start-p0"}, {Name: "start-p1"}}, 1, &rules.StartSpec{})
			if row.Status != dside.StatusNA || !strings.HasPrefix(row.Reason, "start not expressible: ") {
				t.Errorf("%s: row %+v, want not_applicable with the reason", c.name, row)
			}
			if !strings.Contains(row.Reason, c.name) {
				t.Errorf("%s: reason %q does not name what dirac cannot mirror", c.name, row.Reason)
			}
			if row.Game != "xs:BLB:thornvault-forager:f0a2" {
				t.Errorf("%s: row game %q", c.name, row.Game)
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
	useStartDirac(t, func(tables *dside.Tables, gameID, set string, pair [2]decks.Deck, seed uint64, maxTurns int, start *rules.StartSpec) dside.Row {
		return dside.Row{Game: gameID, Status: dside.StatusDiverged, Table: "std-xp",
			Decisions: 7, What: "kernel", Field: "p0.life", Diff: []string{"p0.life 20 != 19"}}
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
