package lower

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
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
	tp, err := game.Play(reg, pair[:], 5, game.Options{MaxTurns: 6})
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
	ws, refs, _, err := Windows(reg, pair[:], 5, game.Options{MaxTurns: 6}, false)
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
	_, err := game.Play(reg, pair[:], 5, game.Options{MaxTurns: 6, OnCut: func(e *rules.Engine, entry int) {
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

// TestWindowCommanderOnBattlefield: a window whose cut has a commander on the
// battlefield is lowered, with the card placed and named in its seat's
// Commanders, and the self-check holds the runner to the live game.
func TestWindowCommanderOnBattlefield(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var files []decks.CommanderFile
	for _, s := range []string{"foundations-keen-engineering", "foundations-wretched-ranks"} {
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
	const seed = 2 // Sai is cast by turn 6 and sits on the battlefield at the cut
	pair := decks.CommanderPair(ds, "CMDR", 0, 0, seed)
	ws, refs, _, err := Windows(reg, pair[:], seed, game.Options{MaxTurns: 10}, false)
	if err != nil {
		t.Fatal(err)
	}
	const sai = "Sai, Master Thopterist"
	var got *Lowered
	for i, l := range ws {
		for _, seat := range l.Item.Scenario.Setup {
			if slices.Contains(seat.Battlefield, sai) {
				got = &ws[i]
			}
		}
		if got != nil {
			break
		}
	}
	if got == nil {
		t.Fatalf("no lowered window with %s on the battlefield; refusals %v", sai, refs)
	}
	found := false
	for _, seat := range got.Item.Scenario.Setup {
		if slices.Contains(seat.Battlefield, sai) {
			found = true
			if !slices.Equal(seat.Commanders, []string{sai}) {
				t.Errorf("Commanders %v, want [%s]", seat.Commanders, sai)
			}
			if slices.Contains(seat.Command, sai) {
				t.Errorf("%s is also in the command zone", sai)
			}
		}
	}
	if !found {
		t.Fatal("Sai on no seat")
	}
	if c := CheckWindow(reg, *got); c.Status != StatusOK {
		t.Fatalf("CheckWindow %s step %d field %s: gorge %q tape %q", c.Status, c.Step, c.Field, c.Gorge, c.Tape)
	}
	// No window of the game is refused for a commander's zone any more.
	for _, r := range refs {
		for _, why := range r.Reasons {
			if strings.HasPrefix(why, "commander_zone:") {
				t.Errorf("window refused as %s", why)
			}
		}
	}
}

func TestGateCommanderTaxPlaced(t *testing.T) {
	e, at := cmdrCut(t)
	cmdr := frontName(e.G.Obj(e.G.Players[0].Commanders[0]))
	e.G.Players[0].CmdCasts[0] = 1
	c := capture(e, at, false)
	if len(c.Reasons) != 0 {
		t.Fatalf("reasons %v, want none", c.Reasons)
	}
	if !reflect.DeepEqual(c.Setup[0].CommanderCasts, map[string]int32{cmdr: 1}) {
		t.Errorf("seat 0 CommanderCasts %v, want {%s: 1}", c.Setup[0].CommanderCasts, cmdr)
	}
	if c.Setup[1].CommanderCasts != nil {
		t.Errorf("seat 1 CommanderCasts %v", c.Setup[1].CommanderCasts)
	}
	// A commander in the command zone stays a Command placement.
	if !slices.Equal(c.Setup[0].Command, []string{cmdr}) || len(c.Setup[0].Commanders) != 0 {
		t.Errorf("seat 0 command %v commanders %v", c.Setup[0].Command, c.Setup[0].Commanders)
	}
	// A commander elsewhere is named in Commanders, never refused for its zone.
	id := e.G.Players[0].Commanders[0]
	e.G.SetZone(state.ZCommand, 0, nil)
	e.G.SetZone(state.ZHand, 0, append(slices.Clone(e.G.Zone(state.ZHand, 0)), id))
	e.G.Obj(id).Zone = state.ZHand
	c = capture(e, at, false)
	if len(c.Reasons) != 0 {
		t.Fatalf("commander in hand: reasons %v", c.Reasons)
	}
	if !slices.Equal(c.Setup[0].Commanders, []string{cmdr}) || !slices.Contains(c.Setup[0].Hand, cmdr) || len(c.Setup[0].Command) != 0 {
		t.Errorf("commander in hand: commanders %v hand %v command %v", c.Setup[0].Commanders, c.Setup[0].Hand, c.Setup[0].Command)
	}
	// A non-commander object in the command zone is still refused.
	e2, at2 := cmdrCut(t)
	for i := range e2.G.Objs {
		o := &e2.G.Objs[i]
		if o.Zone == state.ZLibrary && o.Card != nil {
			o.Zone = state.ZCommand
			break
		}
	}
	if r := capture(e2, at2, false).Reasons; !slices.Contains(r, "zone:command") {
		t.Errorf("reasons %v, want zone:command", r)
	}
}

func TestGateCommanderDamagePlaced(t *testing.T) {
	e, at := cmdrCut(t)
	cmdr := frontName(e.G.Obj(e.G.Players[0].Commanders[0]))
	// p0's commander is at dense index 0, p1's at 1.
	e.G.Players[1].CmdDamage[0] = 3
	c := capture(e, at, false)
	if len(c.Reasons) != 0 {
		t.Fatalf("reasons %v, want none", c.Reasons)
	}
	if want := map[string]int32{"p0:" + cmdr: 3}; !reflect.DeepEqual(c.Setup[1].CommanderDamage, want) {
		t.Errorf("seat 1 CommanderDamage %v, want %v", c.Setup[1].CommanderDamage, want)
	}
	if c.Setup[0].CommanderDamage != nil {
		t.Errorf("seat 0 CommanderDamage %v", c.Setup[0].CommanderDamage)
	}
	// With the cut's active seat 1 the seats swap, and the key with them.
	it := c.item(seatMap{swap: true}, "x", "x")
	if want := map[string]int32{"p1:" + cmdr: 3}; !reflect.DeepEqual(it.Scenario.Setup["p0"].CommanderDamage, want) {
		t.Errorf("swapped p0 CommanderDamage %v, want %v", it.Scenario.Setup["p0"].CommanderDamage, want)
	}
	if it.Scenario.Setup["p1"].CommanderDamage != nil {
		t.Errorf("swapped p1 CommanderDamage %v", it.Scenario.Setup["p1"].CommanderDamage)
	}
	// p1's commander at dense index 1, dealt to p0.
	e.G.Players[1].CmdDamage[0] = 0
	e.G.Players[0].CmdDamage[1] = 5
	c = capture(e, at, false)
	cmdr1 := frontName(e.G.Obj(e.G.Players[1].Commanders[0]))
	if want := map[string]int32{"p1:" + cmdr1: 5}; len(c.Reasons) != 0 || !reflect.DeepEqual(c.Setup[0].CommanderDamage, want) {
		t.Errorf("seat 0 CommanderDamage %v (reasons %v), want %v", c.Setup[0].CommanderDamage, c.Reasons, want)
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

// TestWindowTapeTwoSeatOpening: a 1v1 window's self-check tape (the tape file
// of a window) writes two empty openings, as it always has, not null.
func TestWindowTapeTwoSeatOpening(t *testing.T) {
	reg, pair := cmdrPair(t)
	ws, refs, _, err := Windows(reg, pair[:], 5, game.Options{MaxTurns: 6}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) == 0 {
		t.Fatalf("no lowered window; refusals %v", refs)
	}
	b, err := ws[0].Against.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var head struct {
		Decks []json.RawMessage `json:"decks"`
		Open  []json.RawMessage `json:"opening"`
	}
	if err := json.Unmarshal(b, &head); err != nil || len(head.Decks) != 2 || len(head.Open) != 2 {
		t.Fatalf("window tape header: %v, %d decks, %d openings", err, len(head.Decks), len(head.Open))
	}
	if !bytes.Contains(b, []byte(`"opening": [`)) || bytes.Contains(b, []byte(`"opening": null`)) {
		t.Fatal("window tape opening is not a two-element array")
	}
}
