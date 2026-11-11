package lower

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// cmdrPair is the Commander pair the tests play.
func cmdrPair(t *testing.T) (*cards.Registry, [2]decks.Deck) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	var files []decks.CommanderFile
	for _, s := range []string{"foundations-tramplesaurus-rex", "foundations-wretched-ranks"} {
		f, err := testutil.LoadRepoDeckFile(s)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, decks.CommanderFile{Stem: s, File: f})
	}
	ds, skips := decks.CommanderDecks(reg, files)
	if len(ds) != 2 {
		t.Fatalf("kept %d decks, skips %v", len(ds), skips)
	}
	return reg, decks.CommanderPair(ds, "CMDR", 0, 0, 1)
}

func TestTapeCommanderSetup(t *testing.T) {
	reg, pair := cmdrPair(t)
	tp, err := game.Play(reg, pair, 5, game.Options{MaxTurns: 6})
	if err != nil {
		t.Fatal(err)
	}
	l, err := Tape(reg, tp)
	if err != nil {
		t.Fatal(err)
	}
	if l.Item.Scenario.Format != oraclegen.FormatCommander {
		t.Fatalf("format %q", l.Item.Scenario.Format)
	}
	for s := range pair {
		seat := l.Item.Scenario.Setup["p"+string(rune('0'+(s^btoi(l.Swap))))]
		if !slices.Equal(seat.Command, []string{pair[s].Commander}) {
			t.Errorf("seat %d command %v, want [%s]", s, seat.Command, pair[s].Commander)
		}
	}
	if !slices.Equal(l.Item.Compare, []string{"command"}) {
		t.Errorf("compare %v", l.Item.Compare)
	}
	if c := SelfCheck(reg, l, tp); c.Status != StatusOK && c.Step < 0 {
		t.Fatalf("self-check %s at step %d field %s: gorge %q tape %q", c.Status, c.Step, c.Field, c.Gorge, c.Tape)
	}

	// Off: without the commander lowering the setup is a constructed game.
	off := l
	off.Item.Scenario.Format = ""
	off.Item.Compare = nil
	off.Item.Scenario.Setup = map[string]oraclegen.Seat{}
	for k, v := range l.Item.Scenario.Setup {
		v.Command = nil
		off.Item.Scenario.Setup[k] = v
	}
	if c := SelfCheck(reg, off, tp); c.Status == StatusOK || c.Step != -1 || c.Field != "players[0].life" {
		t.Fatalf("off: self-check %s step %d field %s, want a gap at step -1 on players[0].life", c.Status, c.Step, c.Field)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestWindowCommanderInCommandZone(t *testing.T) {
	reg, pair := cmdrPair(t)
	ws, refs, _, err := Windows(reg, pair, 5, game.Options{MaxTurns: 6}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) == 0 {
		t.Fatalf("no lowered window; refusals %v", refs)
	}
	l := ws[0]
	if l.Item.Scenario.Format != oraclegen.FormatCommander || !slices.Equal(l.Item.Compare, []string{"command"}) {
		t.Fatalf("format %q compare %v", l.Item.Scenario.Format, l.Item.Compare)
	}
	for s := range pair {
		seat := l.Item.Scenario.Setup["p"+string(rune('0'+(s^btoi(l.Swap))))]
		if !slices.Equal(seat.Command, []string{pair[s].Commander}) {
			t.Errorf("seat %d command %v, want [%s]", s, seat.Command, pair[s].Commander)
		}
		if seat.Life == nil || *seat.Life != 40 {
			t.Errorf("seat %d life %v, want 40", s, seat.Life)
		}
	}
	if c := CheckWindow(reg, l); c.Status != StatusOK {
		t.Fatalf("CheckWindow %s step %d field %s: gorge %q tape %q", c.Status, c.Step, c.Field, c.Gorge, c.Tape)
	}
}

// cmdrCut is an expressible Commander cut: the engine at the first lowered
// window's cut, cloned so a test can plant state on it.
func cmdrCut(t *testing.T) (*rules.Engine, int) {
	t.Helper()
	reg, pair := cmdrPair(t)
	var got *rules.Engine
	var at int
	_, err := game.Play(reg, pair, 5, game.Options{MaxTurns: 6, OnCut: func(e *rules.Engine, entry int) {
		if got == nil && len(capture(e, entry, false).Reasons) == 0 {
			got, at = e.Clone(), entry
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("no expressible Commander cut")
	}
	return got, at
}

func TestGateCommanderOutsideCommandZone(t *testing.T) {
	e, at := cmdrCut(t)
	c := capture(e, at, false)
	if !c.Commander {
		t.Fatal("cut is not a Commander cut")
	}
	id := e.G.Players[0].Commanders[0]
	e.G.Obj(id).Zone = state.ZBattlefield
	if r := capture(e, at, false).Reasons; !slices.Contains(r, "commander_zone:battlefield") {
		t.Fatalf("reasons %v, want commander_zone:battlefield", r)
	}
}

func TestGateCommanderTaxAndDamage(t *testing.T) {
	e, at := cmdrCut(t)
	has := func(r string) bool { return slices.Contains(capture(e, at, false).Reasons, r) }
	if has("field:Player.CmdCasts") || has("field:Player.CmdDamage") {
		t.Fatal("genesis-sized zero slices are refused")
	}
	e.G.Players[0].CmdCasts[0] = 1
	if !has("field:Player.CmdCasts") {
		t.Error("a paid tax is not refused")
	}
	e.G.Players[0].CmdCasts[0] = 0
	e.G.Players[1].CmdDamage[0] = 3
	if !has("field:Player.CmdDamage") {
		t.Error("commander damage is not refused")
	}
	e.G.Players[1].CmdDamage[0] = 0
	if has("field:Player.CmdCasts") || has("field:Player.CmdDamage") {
		t.Error("reset state still refused")
	}
}

func TestCommanderStartLife(t *testing.T) {
	life := func(n int32) *int32 { return &n }
	c := func(cmdr bool, speed int32, opp *int32) *cut {
		return &cut{Commander: cmdr, Setup: [2]oraclegen.Seat{{Speed: speed}, {Life: opp}}}
	}
	for _, tc := range []struct {
		name string
		c    *cut
		want bool
	}{
		{"constructed, 16", c(false, 2, life(16)), true},
		{"constructed, 20", c(false, 2, life(20)), false},
		{"constructed, 40 is not a fall", c(false, 2, life(40)), false},
		{"commander, 38", c(true, 2, life(38)), true},
		{"commander, 40", c(true, 2, life(40)), false},
		{"commander, 20 is a fall", c(true, 2, life(20)), true},
	} {
		if got := tc.c.setupSpeedLatch(); got != tc.want {
			t.Errorf("%s: setupSpeedLatch = %v, want %v", tc.name, got, tc.want)
		}
	}
	if startLife(&cut{Commander: true}) != 40 || startLife(&cut{}) != 20 {
		t.Error("startLife is not 40 / 20")
	}
}
