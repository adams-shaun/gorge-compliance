package lower

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// fdnPairs is the first n FDN deck pairs of chunk 0, seeded 100+game as the
// tape tests are.
func fdnPairs(t *testing.T, n int) (*cards.Registry, [][2]decks.Deck) {
	t.Helper()
	return setPairs(t, "FDN", n)
}

// setPairs is the first n deck pairs of set's chunk 0.
func setPairs(t *testing.T, set string, n int) (*cards.Registry, [][2]decks.Deck) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cands, _, err := decks.Candidates(reg, manifestDir, set)
	if err != nil {
		t.Fatal(err)
	}
	var out [][2]decks.Deck
	for g := 0; g < n; g++ {
		out = append(out, decks.Pair(cands, cov.New(), set, 0, g, 7))
	}
	return reg, out
}

func TestGateTableCoversEveryField(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typ   reflect.Type
		table map[string]entry
	}{
		{"Game", reflect.TypeOf(state.Game{}), gameTable},
		{"Player", reflect.TypeOf(state.Player{}), playerTable},
		{"Object", reflect.TypeOf(state.Object{}), objectTable},
	} {
		real := map[string]bool{}
		for i := 0; i < tc.typ.NumField(); i++ {
			f := tc.typ.Field(i)
			if !f.IsExported() {
				continue
			}
			real[f.Name] = true
			en, ok := tc.table[f.Name]
			if !ok {
				t.Errorf("%s.%s has no table entry: classify it in gate_table.go", tc.name, f.Name)
				continue
			}
			if en.Class < placed || en.Class > refuse {
				t.Errorf("%s.%s: bad class %d", tc.name, f.Name, en.Class)
			}
			if (en.Class == ignored || en.Class == refuse) && strings.TrimSpace(en.Why) == "" {
				t.Errorf("%s.%s is %s and needs a reason", tc.name, f.Name, en.Class)
			}
		}
		for name := range tc.table {
			if !real[name] {
				t.Errorf("%s table names %s, which is not a field of state.%s", tc.name, name, tc.name)
			}
		}
	}
	for name := range turnStamps {
		if objectTable[name].Class != refuse {
			t.Errorf("turn stamp %s must be a refuse field", name)
		}
	}
}

// windowsOf plays the first n FDN games and returns every game's windows.
type played struct {
	lowered []Lowered
	refused []Refusal
	tape    *game.Tape
}

func windowsOf(t *testing.T, n int) (*cards.Registry, []played) {
	t.Helper()
	reg, pairs := fdnPairs(t, n)
	var out []played
	for g, p := range pairs {
		ls, rs, tp, err := Windows(reg, p, uint64(100+g), game.Options{Set: "FDN"})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, played{ls, rs, tp})
	}
	return reg, out
}

func TestWindowsSelfCheck(t *testing.T) {
	reg, games := windowsOf(t, 4)
	reasons := map[string]int{}
	gaps := map[string]int{}
	var lowered, ok, refused, truncated, rngWin, attachWin, answerWin int
	truncs := map[string]int{}
	for _, g := range games {
		for _, r := range g.refused {
			refused++
			for _, why := range r.Reasons {
				reasons[why]++
			}
		}
		for _, l := range g.lowered {
			lowered++
			if l.Truncated != "" {
				truncated++
				truncs[l.Truncated]++
			}
			if l.HasRNG {
				rngWin++
			}
			if l.Prelude > 0 {
				attachWin++
			}
			if len(l.Item.SetupAnswers) > 0 {
				answerWin++
			}
			c := CheckWindow(reg, l)
			if c.Status == StatusOK {
				ok++
				continue
			}
			gaps[fmt.Sprintf("%s@%s", c.Field, l.Item.ID)]++
			t.Errorf("%s: %s at step %d, %s: gorge %s, tape %s %v", l.Item.ID, c.Status, c.Step, c.Field, c.Gorge, c.Tape, c.Fails)
		}
	}
	t.Logf("turns %d: lowered %d (lower_ok %d, truncated %d), refused %d", lowered+refused, lowered, ok, truncated, refused)
	t.Logf("windows with an rng delta: %d, with an attach prelude: %d, with setup answers: %d; truncations %v", rngWin, attachWin, answerWin, truncs)
	var keys []string
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if reasons[keys[a]] != reasons[keys[b]] {
			return reasons[keys[a]] > reasons[keys[b]]
		}
		return keys[a] < keys[b]
	})
	for _, k := range keys {
		t.Logf("refusal %-40s %d", k, reasons[k])
	}
	if ok*3 < lowered+refused {
		t.Errorf("only %d of %d turns lowered to a lower_ok window (stop rule: a third)", ok, lowered+refused)
	}
}

func windowBytes(t *testing.T, ls []Lowered, rs []Refusal) []byte {
	t.Helper()
	var b bytes.Buffer
	for _, l := range ls {
		j, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(j)
		b.WriteByte('\n')
		// Against is not marshalled; the tape it holds is part of the window.
		tb, err := l.Against.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		b.Write(tb)
	}
	j, _ := json.Marshal(rs)
	b.Write(j)
	return b.Bytes()
}

func TestWindowsDeterministic(t *testing.T) {
	reg, pairs := fdnPairs(t, 2)
	for g, p := range pairs {
		var first []byte
		for run := 0; run < 2; run++ {
			ls, rs, _, err := Windows(reg, p, uint64(100+g), game.Options{Set: "FDN"})
			if err != nil {
				t.Fatal(err)
			}
			b := windowBytes(t, ls, rs)
			if run == 0 {
				first = b
			} else if !bytes.Equal(first, b) {
				t.Fatalf("game %d: two runs gave different windows or refusals", g)
			}
		}
		if len(first) == 0 {
			t.Fatalf("game %d: no windows", g)
		}
	}
}

func TestOnCutIsReadOnly(t *testing.T) {
	reg, pairs := fdnPairs(t, 2)
	for g, p := range pairs {
		without, err := game.Play(reg, p, uint64(100+g), game.Options{Set: "FDN"})
		if err != nil {
			t.Fatal(err)
		}
		var cuts int
		with, err := game.Play(reg, p, uint64(100+g), game.Options{Set: "FDN", OnCut: func(e *rules.Engine, entry int) {
			cuts++
			capture(e, entry)
		}})
		if err != nil {
			t.Fatal(err)
		}
		a, _ := without.Bytes()
		b, _ := with.Bytes()
		if !bytes.Equal(a, b) {
			t.Fatalf("game %d: the tape changed when the OnCut hook ran", g)
		}
		if cuts == 0 {
			t.Fatalf("game %d: no cut points", g)
		}
	}
}

// expressibleCut plays pair until a cut from turn 3 on that the gate accepts
// and returns a clone of the engine there, so a test can plant state on it
// without touching the game.
func expressibleCut(t *testing.T, reg *cards.Registry, p [2]decks.Deck, seed uint64) (*rules.Engine, int) {
	t.Helper()
	return expressibleCutIn(t, reg, "FDN", p, seed)
}

func expressibleCutIn(t *testing.T, reg *cards.Registry, set string, p [2]decks.Deck, seed uint64) (*rules.Engine, int) {
	t.Helper()
	var got *rules.Engine
	var at int
	_, err := game.Play(reg, p, seed, game.Options{Set: set, OnCut: func(e *rules.Engine, entry int) {
		if got == nil && e.G.Turn >= 3 && len(capture(e, entry).Reasons) == 0 {
			got, at = e.Clone(), entry
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("no expressible cut from turn 3")
	}
	return got, at
}

func TestGateRefusesDelayedTrigger(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	e.G.Delayed = append(e.G.Delayed, state.DelayedTrigger{ID: 1, Phase: state.StepEnd, Execute: "TrigReturn"})
	c := capture(e, at)
	if len(c.Reasons) != 1 || c.Reasons[0] != "field:Game.Delayed" {
		t.Fatalf("reasons %v, want [field:Game.Delayed]", c.Reasons)
	}
}

// TestGateRefusesChosenMode: an as-enters mode choice (SetChosenMode$) is held
// on the permanent for its lifetime on the battlefield, but setup places the
// permanent, not the choice, so the gate must refuse it.
func TestGateRefusesChosenMode(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	if r := capture(e, at).Reasons; len(r) != 0 {
		t.Fatalf("base cut refused: %v", r)
	}
	var planted bool
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.Card != nil {
			o.ChosenModes = []string{"Abzan"}
			planted = true
			break
		}
	}
	if !planted {
		t.Fatal("no battlefield permanent with a card to plant the mode on")
	}
	if r := capture(e, at).Reasons; !slices.Contains(r, "field:Object.ChosenModes") {
		t.Fatalf("reasons %v, want field:Object.ChosenModes", r)
	}
}

// TestSetupSpeedLatch: setup places the non-active seat's life as a life-loss
// event; with the active seat at speed 1..3 that consumes the once-per-turn
// CR 702.179d speed trigger the live game still had. The window must be
// refused.
func TestSetupSpeedLatch(t *testing.T) {
	life := func(n int32) *int32 { return &n }
	players := func(p0, p1 int32) []game.PlayerDigest {
		return []game.PlayerDigest{{Seat: 0, Life: p0}, {Seat: 1, Life: p1}}
	}
	tape := func(entries ...game.Entry) *game.Tape { return &game.Tape{Entries: entries} }
	entry := func(turn int32, p0, p1 int32) game.Entry {
		return game.Entry{Turn: turn, Digest: game.Digest{Turn: turn, Players: players(p0, p1)}}
	}
	c := func(speed int32, opp *int32) *cut {
		return &cut{Entry: 0, Turn: 1, Active: 0, Setup: [2]oraclegen.Seat{
			{Speed: speed},
			{Life: opp},
		}}
	}
	fall := tape(entry(1, 20, 16), entry(1, 20, 12))
	sentinel := tape(entry(1, 20, 16), entry(2, 20, 12))
	flat := tape(entry(1, 20, 16), entry(1, 20, 16))
	seat0 := tape(entry(1, 20, 16), entry(1, 12, 16))
	for _, tc := range []struct {
		name string
		c    *cut
		t    *game.Tape
		want bool
	}{
		{"speed 2, opponent 16, in-window fall", c(2, life(16)), fall, true},
		{"speed 4 (max)", c(4, life(16)), fall, false},
		{"speed 0", c(0, life(16)), fall, false},
		{"opponent life 20", c(2, life(20)), fall, false},
		{"no fall in window", c(2, life(16)), flat, false},
		{"fall on active seat only", c(2, life(16)), seat0, false},
		{"fall on next-turn sentinel", c(2, life(16)), sentinel, true},
		{"nil tape", c(2, life(16)), nil, false},
	} {
		if got := tc.c.setupSpeedLatch(tc.t); got != tc.want {
			t.Errorf("%s: setupSpeedLatch = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A planeswalker's printed loyalty is a counter the card enters with; setup
// counters are added to it, so the window must place only the difference. The
// test plants a planeswalker (a corpus card that is not in the game's decks) in
// place of a battlefield permanent of a real cut, with two loyalty above its
// printed value, lowers the cut and holds the runner's setup checkpoint to the
// planted state's digest.
func TestWindowCountersNotDoubled(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	var pw *cards.Card
	var printed int32
	names := reg.Universe().Names()
	sort.Strings(names)
	for _, n := range names {
		c, ok := reg.Lookup(n)
		if !ok || len(c.Faces) != 1 || !c.Faces[0].IsPlaneswalker() {
			continue
		}
		var v int32
		if _, err := fmt.Sscanf(c.Faces[0].Loyalty, "%d", &v); err == nil && v > 0 {
			pw, printed = c, v
			break
		}
	}
	if pw == nil {
		t.Fatal("no planeswalker with a numeric loyalty in the corpus")
	}
	planted, at := expressibleCut(t, reg, pairs[0], 100)
	var swapped bool
	for i := range planted.G.Objs {
		o := &planted.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.AttachedTo == 0 && o.Ability == nil && !o.Tapped && len(o.Counters) == 0 {
			o.SetCard(pw, 0)
			o.Counters = []state.Counter{{Kind: "LOYALTY", N: printed + 2}}
			swapped = true
			break
		}
	}
	if !swapped {
		t.Fatal("no battlefield permanent to replace")
	}
	c := capture(planted, at)
	if len(c.Reasons) > 0 {
		t.Fatalf("the planted cut is refused: %v", c.Reasons)
	}
	name := pw.Faces[0].Name
	var got map[string]int32
	for _, s := range c.Setup {
		if cs, ok := s.Counters[name]; ok {
			got = cs
		}
	}
	if len(got) != 1 || got["LOYALTY"] != 2 {
		t.Fatalf("setup counters for %s = %v, want LOYALTY 2 (printed %d is entered with, not placed)", name, got, printed)
	}
	// Run the setup through the runner: its checkpoint must show printed+2.
	m := seatMap{swap: c.Active == 1}
	l := Lowered{Item: c.item(m, "xe:test:planted", "planted"), StepEntry: []int{-1}, Swap: m.swap}
	l.Item.Steps = []oraclegen.Step{{Op: "pass", Seat: 0}}
	wt := &game.Tape{Entries: []game.Entry{{Digest: game.DigestOfGame(planted)}}}
	wt.Entries[0].Digest.Turn = 1
	if chk := SelfCheck(reg, l, wt); chk.Status != StatusOK && chk.Step < 0 {
		t.Fatalf("setup checkpoint differs from the planted state: %s: gorge %s, tape %s %v", chk.Field, chk.Gorge, chk.Tape, chk.Fails)
	}
}

// plantCard replaces the card of a library object of e with the named corpus
// card, so the game mentions its text.
func plantCard(t *testing.T, reg *cards.Registry, e *rules.Engine, name string) {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s is not in the corpus", name)
	}
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Zone == state.ZLibrary && o.Card != nil {
			o.SetCard(c, 0)
			return
		}
	}
	t.Fatal("no library card to replace")
}

func hasReason(c *cut, r string) bool { return slices.Contains(c.Reasons, r) }

// A hand card that can act before turn 1 (Leyline, Chancellor, Gemstone
// Caverns) is refused: the runner's setup drive would answer its opening-hand ask.
func TestGateRefusesOpeningHandCard(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	if hasReason(capture(e, at), "opening_hand_effect") {
		t.Fatal("unplanted cut already refused as opening_hand_effect")
	}
	var o *state.Object
	for p := range e.G.Players {
		if ids := e.G.Zone(state.ZHand, state.PlayerID(p)); len(ids) > 0 {
			o = e.G.Obj(ids[0])
			break
		}
	}
	if o == nil {
		t.Fatal("both hands are empty")
	}
	for _, name := range []string{"Leyline of Hope", "Chancellor of the Tangle", "Gemstone Caverns"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s is not in the corpus", name)
		}
		o.SetCard(c, 0)
		if !hasReason(capture(e, at), "opening_hand_effect") {
			t.Errorf("%s in hand: reasons %v, want opening_hand_effect", name, capture(e, at).Reasons)
		}
	}
}

func TestGateDeadTurnHistory(t *testing.T) {
	reg, pairs := setPairs(t, "DFT", 1)
	base, at := expressibleCutIn(t, reg, "DFT", pairs[0], 100)
	var bf state.ObjID
	var perm int
	for i := range base.G.Objs {
		if o := &base.G.Objs[i]; o.Zone == state.ZBattlefield && o.Card != nil && o.Ability == nil {
			bf, perm = o.ID, i
			break
		}
	}
	if bf == 0 {
		t.Fatal("no battlefield permanent")
	}

	// (a) a card-less ability object's entry is not history; (b) a card's is.
	e := base.Clone()
	e.G.Objs = append(e.G.Objs, state.Object{ID: state.ObjID(len(e.G.Objs) + 1), Zone: state.ZCeased, Source: bf})
	e.G.Entered = append(e.G.Entered, state.ZoneEntry{Obj: state.ObjID(len(e.G.Objs)), From: state.ZLibrary, To: state.ZStack})
	if c := capture(e, at); len(c.Reasons) != 0 {
		t.Fatalf("(a) card-less entry: reasons %v, want none", c.Reasons)
	}
	e.G.Entered = append(e.G.Entered, state.ZoneEntry{Obj: bf, From: state.ZGraveyard, To: state.ZExile})
	plantCard(t, reg, e, entryReaderCard(t, reg))
	if c := capture(e, at); len(c.Reasons) != 1 || c.Reasons[0] != "field:Game.Entered" {
		t.Fatalf("(b) card entry: reasons %v, want [field:Game.Entered]", c.Reasons)
	}

	// (c) the tally is live only when a card in the game reads it.
	e = base.Clone()
	e.G.ResolvedThisTurn = map[string]int32{"x": 1}
	if c := capture(e, at); len(c.Reasons) != 0 {
		t.Fatalf("(c) unread tally: reasons %v, want none", c.Reasons)
	}
	plantCard(t, reg, e, "Tannuk, Memorial Ensign")
	if c := capture(e, at); !hasReason(c, "field:Game.ResolvedThisTurn") {
		t.Fatalf("(c) read tally: reasons %v, want field:Game.ResolvedThisTurn", c.Reasons)
	}

	// (d) a runtime SVar is live only when a card in the game names its key.
	e = base.Clone()
	e.G.Objs[perm].RuntimeSVars = map[string]int32{"AFLifeLost": 2}
	if c := capture(e, at); len(c.Reasons) != 0 {
		t.Fatalf("(d) unread SVar: reasons %v, want none", c.Reasons)
	}
	plantCard(t, reg, e, "Gray Merchant of Asphodel")
	if c := capture(e, at); !hasReason(c, "field:Object.RuntimeSVars") {
		t.Fatalf("(d) read SVar: reasons %v, want field:Object.RuntimeSVars", c.Reasons)
	}
}

// The relaxed fields open windows that lower and self-check: the first twelve
// DFT games are played with the cut hook recording which cuts carried one.
func TestWindowsDeadTurnHistory(t *testing.T) {
	reg, pairs := setPairs(t, "DFT", 12)
	byStatus := map[string]int{}
	var okRelaxed int
	for g, p := range pairs {
		relaxed := map[int32]bool{}
		opt := game.Options{Set: "DFT", OnCut: func(e *rules.Engine, entry int) {
			if nonDrawEntry(e.G) || len(e.G.ResolvedThisTurn) > 0 || runtimeSVarsOf(e.G) {
				relaxed[e.G.Turn] = true
			}
		}}
		ls, _, _, err := Windows(reg, p, uint64(100+g), opt)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range ls {
			var turn int32
			if _, err := fmt.Sscanf(l.Item.ID[strings.LastIndex(l.Item.ID, ":w")+2:], "%d", &turn); err != nil {
				t.Fatal(err)
			}
			if !relaxed[turn] {
				continue
			}
			c := CheckWindow(reg, l)
			byStatus[string(c.Status)]++
			if c.Status == StatusOK {
				okRelaxed++
			}
		}
	}
	t.Logf("relaxed lowered windows by status: %v", byStatus)
	if okRelaxed == 0 {
		t.Fatalf("no relaxed window self-checked ok: %v", byStatus)
	}
}

func runtimeSVarsOf(g *state.Game) bool {
	for i := range g.Objs {
		if len(g.Objs[i].RuntimeSVars) > 0 {
			return true
		}
	}
	return false
}

func nonDrawEntry(g *state.Game) bool {
	for _, z := range g.Entered {
		if z.To != state.ZHand || z.From != state.ZLibrary {
			return true
		}
	}
	return false
}

// libraryObjs is up to n distinct library objects of e that carry a card.
func libraryObjs(t *testing.T, e *rules.Engine, n int) []*state.Object {
	t.Helper()
	var out []*state.Object
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Zone == state.ZLibrary && o.Card != nil && o.Ability == nil {
			out = append(out, o)
			if len(out) == n {
				return out
			}
		}
	}
	t.Fatalf("only %d library cards, want %d", len(out), n)
	return nil
}

func TestCardMentions(t *testing.T) {
	reg, _ := setPairs(t, "DFT", 1)
	look := func(n string) *cards.Card {
		c, ok := reg.Lookup(n)
		if !ok {
			t.Fatalf("%s is not in the corpus", n)
		}
		return c
	}
	for _, tc := range []struct {
		card, word string
		want       bool
	}{
		{"Ghost Vacuum", "ExiledWith", true},
		{"Gonti, Night Minister", "Exiler", true},
		{"Ride's End", "ExiledWith", false},
	} {
		if got := cardMentions(look(tc.card), tc.word); got != tc.want {
			t.Errorf("cardMentions(%s, %s) = %v, want %v", tc.card, tc.word, got, tc.want)
		}
	}
	if cardMentions(nil, "ExiledWith") {
		t.Error("a nil card mentions nothing")
	}
}

func TestGateDeadExileLink(t *testing.T) {
	reg, pairs := setPairs(t, "DFT", 1)
	base, at := expressibleCutIn(t, reg, "DFT", pairs[0], 100)
	link := func(e *rules.Engine, source string) {
		objs := libraryObjs(t, e, 2)
		src, exiled := objs[0], objs[1]
		c, ok := reg.Lookup(source)
		if !ok {
			t.Fatalf("%s is not in the corpus", source)
		}
		src.SetCard(c, 0)
		src.Zone = state.ZGraveyard
		src.ExiledCards = []state.ObjID{exiled.ID}
		exiled.Zone = state.ZExile
		exiled.ExiledWith = src.ID
		exiled.HasExiledBy = true
	}
	reasons := func(e *rules.Engine) []string { return capture(e, at).Reasons }

	// (a) the removal spell's script never reads the link.
	e := base.Clone()
	link(e, "Ride's End")
	if r := reasons(e); len(r) != 0 {
		t.Fatalf("(a) dead link: reasons %v, want none", r)
	}

	// (b) a source that reads ExiledWith keeps both link fields live.
	e = base.Clone()
	link(e, "Ghost Vacuum")
	r := reasons(e)
	if !slices.Contains(r, "field:Object.ExiledCards") || !slices.Contains(r, "field:Object.ExiledWith") {
		t.Fatalf("(b) read link: reasons %v, want ExiledCards and ExiledWith", r)
	}

	// (c) a card in the game that reads the exiler makes HasExiledBy live.
	e = base.Clone()
	link(e, "Ride's End")
	g, ok := reg.Lookup("Gonti, Night Minister")
	if !ok {
		t.Fatal("Gonti, Night Minister is not in the corpus")
	}
	libraryObjs(t, e, 3)[2].SetCard(g, 0)
	if r := reasons(e); len(r) != 1 || r[0] != "field:Object.HasExiledBy" {
		t.Fatalf("(c) exiler read: reasons %v, want [field:Object.HasExiledBy]", r)
	}

	// (d) a card that exiled itself (warp) is refused as exile_self.
	e = base.Clone()
	o := libraryObjs(t, e, 1)[0]
	o.Zone = state.ZExile
	o.ExiledCards = []state.ObjID{o.ID}
	o.ExiledWith = o.ID
	o.HasExiledBy = true
	r = reasons(e)
	if !slices.Contains(r, reasonSelfExile) {
		t.Fatalf("(d) self-exile: reasons %v, want %s", r, reasonSelfExile)
	}
	for _, x := range r {
		if strings.HasPrefix(x, "field:") {
			t.Fatalf("(d) self-exile: reasons %v carry a field reason", r)
		}
	}
}

// exileLinkOf is true when the game holds an exile link the gate relaxes.
func exileLinkOf(g *state.Game) bool {
	for i := range g.Objs {
		if o := &g.Objs[i]; len(o.ExiledCards) > 0 || o.ExiledWith != 0 || o.HasExiledBy {
			return true
		}
	}
	return false
}

// The relaxed fields open windows that lower and self-check: the first twelve
// EOE games are played with the cut hook recording which turns carried a link.
func TestWindowsDeadExileLink(t *testing.T) {
	reg, pairs := setPairs(t, "EOE", 12)
	byStatus := map[string]int{}
	var okRelaxed, relaxedWindows int
	for g, p := range pairs {
		relaxed := map[int32]bool{}
		opt := game.Options{Set: "EOE", OnCut: func(e *rules.Engine, entry int) {
			if exileLinkOf(e.G) && len(capture(e, entry).Reasons) == 0 {
				relaxed[e.G.Turn] = true
			}
		}}
		ls, _, _, err := Windows(reg, p, uint64(100+g), opt)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range ls {
			var turn int32
			if _, err := fmt.Sscanf(l.Item.ID[strings.LastIndex(l.Item.ID, ":w")+2:], "%d", &turn); err != nil {
				t.Fatal(err)
			}
			if !relaxed[turn] {
				continue
			}
			relaxedWindows++
			c := CheckWindow(reg, l)
			byStatus[string(c.Status)]++
			if c.Status == StatusOK {
				okRelaxed++
			}
		}
	}
	t.Logf("relaxed lowered windows by status: %v", byStatus)
	if okRelaxed == 0 {
		t.Fatalf("relaxed windows but none self-checked ok: %v", byStatus)
	}
}

// entryReaderCard is the first corpus card, by name, whose script names one of
// turnEntryReaders.
func entryReaderCard(t *testing.T, reg *cards.Registry) string {
	t.Helper()
	names := reg.Universe().Names()
	sort.Strings(names)
	for _, n := range names {
		if c, ok := reg.Lookup(n); ok && cardMentions(c, turnEntryReaders...) {
			return n
		}
	}
	t.Fatal("no corpus card names a turn-entry reader")
	return ""
}

// entryReaderIn is true when a card of the game names a turn-entry reader.
func entryReaderIn(g *state.Game) bool {
	for i := range g.Objs {
		if cardMentions(g.Objs[i].Card, turnEntryReaders...) {
			return true
		}
	}
	return false
}

func TestGateTurnEntryReaders(t *testing.T) {
	reg, pairs := setPairs(t, "DFT", 1)
	base, at := expressibleCutIn(t, reg, "DFT", pairs[0], 100)
	if entryReaderIn(base.G) {
		t.Fatal("the base game already holds a turn-entry reader")
	}
	// died moves a library card to the graveyard as if it had died this turn.
	died := func(e *rules.Engine) *state.Object {
		o := libraryObjs(t, e, 1)[0]
		o.Zone, o.EnteredThisTurn, o.EnteredFrom = state.ZGraveyard, true, state.ZBattlefield
		e.G.Entered = append(e.G.Entered, state.ZoneEntry{Obj: o.ID, From: state.ZBattlefield, To: state.ZGraveyard})
		return o
	}

	// (a) with no reader in the game a this-turn death is dead history.
	e := base.Clone()
	died(e)
	if c := capture(e, at); len(c.Reasons) != 0 {
		t.Fatalf("(a) no reader: reasons %v, want none", c.Reasons)
	}

	// (b) a reader anywhere in the game, library included, makes both live.
	e = base.Clone()
	o := died(e)
	for _, x := range libraryObjs(t, e, 2) {
		if x.ID != o.ID {
			x.SetCard(mustLookup(t, reg, entryReaderCard(t, reg)), 0)
			break
		}
	}
	c := capture(e, at)
	if !hasReason(c, "field:Game.Entered") || !hasReason(c, "field:Object.EnteredThisTurn") {
		t.Fatalf("(b) reader: reasons %v, want Game.Entered and Object.EnteredThisTurn", c.Reasons)
	}

	// (c) a battlefield permanent that entered this turn stays refused.
	e = base.Clone()
	for i := range e.G.Objs {
		if p := &e.G.Objs[i]; p.Zone == state.ZBattlefield && p.Card != nil && p.Ability == nil {
			p.EnteredThisTurn = true
			break
		}
	}
	if c := capture(e, at); !hasReason(c, "field:Object.EnteredThisTurn") {
		t.Fatalf("(c) battlefield entry: reasons %v, want field:Object.EnteredThisTurn", c.Reasons)
	}
}

func mustLookup(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s is not in the corpus", name)
	}
	return c
}

// Windows whose cut carried an off-battlefield entry with no reader in the game
// are now expressible: the first forty DFT games are played with the cut hook
// recording which cuts did, and the relaxed windows must lower and self-check.
// Such cuts are rare (the other gate reasons usually remain): the first twelve
// DSK games hold none, DFT games 13 and 33 hold one each.
func TestWindowsTurnEntryReaders(t *testing.T) {
	reg, pairs := setPairs(t, "DFT", 40)
	byStatus := map[string]int{}
	var okRelaxed int
	for g, p := range pairs {
		relaxed := map[int32]bool{}
		opt := game.Options{Set: "DFT", OnCut: func(e *rules.Engine, entry int) {
			if !entryReaderIn(e.G) && offBattlefieldEntry(e.G) && len(capture(e, entry).Reasons) == 0 {
				relaxed[e.G.Turn] = true
			}
		}}
		ls, _, _, err := Windows(reg, p, uint64(100+g), opt)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range ls {
			var turn int32
			if _, err := fmt.Sscanf(l.Item.ID[strings.LastIndex(l.Item.ID, ":w")+2:], "%d", &turn); err != nil {
				t.Fatal(err)
			}
			if !relaxed[turn] {
				continue
			}
			c := CheckWindow(reg, l)
			byStatus[string(c.Status)]++
			if c.Status == StatusOK {
				okRelaxed++
			}
		}
	}
	t.Logf("relaxed lowered windows by status: %v", byStatus)
	if okRelaxed == 0 {
		t.Fatalf("no relaxed window self-checked ok: %v", byStatus)
	}
}

// offBattlefieldEntry is true when the game holds a this-turn zone entry other
// than the draw step's, or an off-battlefield object flagged EnteredThisTurn.
func offBattlefieldEntry(g *state.Game) bool {
	if nonDrawEntry(g) {
		return true
	}
	for i := range g.Objs {
		if o := &g.Objs[i]; o.EnteredThisTurn && o.Zone != state.ZBattlefield &&
			!(o.Zone == state.ZHand && o.EnteredFrom == state.ZLibrary) {
			return true
		}
	}
	return false
}

// seatBattlefield is the seat's placeable battlefield in the order the gate
// binds refs: timestamp, then id.
func seatBattlefield(g *state.Game, seat int) []*state.Object {
	var bf []*state.Object
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone == state.ZBattlefield && o.Ability == nil && int(o.Controller) == seat {
			bf = append(bf, o)
		}
	}
	sort.SliceStable(bf, func(a, b int) bool {
		if bf[a].Timestamp != bf[b].Timestamp {
			return bf[a].Timestamp < bf[b].Timestamp
		}
		return bf[a].ID < bf[b].ID
	})
	return bf
}

// refOf is o's seat-relative ref among bf: its front name, "#k" from the
// second placement of the name on.
func refOf(bf []*state.Object, o *state.Object) string {
	n, k := frontName(o), 0
	for _, x := range bf {
		if frontName(x) == n {
			k++
		}
		if x == o {
			break
		}
	}
	if k > 1 {
		return fmt.Sprintf("%s#%d", n, k)
	}
	return n
}

func TestGateTappedAfterUntap(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	active := int(e.G.Active)
	bf := seatBattlefield(e.G, active)
	for _, o := range bf {
		o.Tapped = false
	}
	var plain []*state.Object
	for _, o := range bf {
		if o.Face() != nil && o.AttachedTo == 0 && len(o.Counters) == 0 && !o.IsToken && o.FaceIdx == 0 {
			plain = append(plain, o)
		}
	}
	if len(plain) < 2 {
		t.Fatalf("active seat has %d plain plain, need 2", len(plain))
	}

	// (a) One tapped permanent: no refusal, its ref is the only entry.
	first := plain[0]
	first.Tapped = true
	c := capture(e, at)
	if len(c.Reasons) != 0 {
		t.Fatalf("a tapped active-seat permanent is refused: %v", c.Reasons)
	}
	if got, want := c.Setup[active].TappedAfterUntap, []string{refOf(bf, first)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tapped_after_untap = %v, want %v", got, want)
	}

	// (b) Two placements of one name, only the later one tapped.
	first.Tapped = false
	a, b := plain[0], plain[1]
	b.SetCard(a.Card, a.FaceIdx)
	bf = seatBattlefield(e.G, active)
	later := a
	for _, o := range bf {
		if o == a || o == b {
			later = o
		}
	}
	later.Tapped = true
	c = capture(e, at)
	if len(c.Reasons) != 0 {
		t.Fatalf("one tapped of two same-name permanents is refused: %v", c.Reasons)
	}
	want := []string{frontName(a) + "#2"}
	if got := c.Setup[active].TappedAfterUntap; !reflect.DeepEqual(got, want) {
		t.Fatalf("tapped_after_untap = %v, want %v", got, want)
	}

	// (c) The active seat never uses the by-name list.
	if got := c.Setup[active].Tapped; len(got) != 0 {
		t.Errorf("active seat: Setup.Tapped = %v, want empty", got)
	}
}

// The seat that does not start keeps its taps through turn 1, so a name whose
// placements are all tapped is the by-name tapped field (the Forge fork reads
// only that one); the active seat's tap stays a tapped_after_untap entry, and
// a name with mixed placements falls back to tapped_after_untap.
func TestWindowsTappedNonActiveUsesTapped(t *testing.T) {
	reg, pairs := fdnPairs(t, 6)
	var (
		e     *rules.Engine
		at    int
		plain []*state.Object
	)
	for g, p := range pairs {
		e, at = expressibleCut(t, reg, p, uint64(100+g))
		plain = nil
		for _, o := range seatBattlefield(e.G, 1-int(e.G.Active)) {
			o.Tapped = false
			if o.Face() != nil && o.AttachedTo == 0 && len(o.Counters) == 0 && !o.IsToken && o.FaceIdx == 0 {
				plain = append(plain, o)
			}
		}
		if len(plain) >= 2 {
			break
		}
	}
	active := int(e.G.Active)
	other := 1 - active
	for _, o := range seatBattlefield(e.G, active) {
		o.Tapped = false
	}
	if len(plain) < 2 {
		t.Fatalf("non-active seat has %d plain permanents, need 2", len(plain))
	}
	a, b := plain[0], plain[1]
	b.SetCard(a.Card, a.FaceIdx)
	name := frontName(a)
	activeBF := seatBattlefield(e.G, active)
	var mine *state.Object
	for _, o := range activeBF {
		if o.Face() != nil && o.AttachedTo == 0 && len(o.Counters) == 0 && !o.IsToken && o.FaceIdx == 0 && frontName(o) != name {
			mine = o
			break
		}
	}
	if mine == nil {
		t.Fatal("active seat has no plain permanent of another name")
	}

	// (a) Both copies tapped: the by-name field, nothing per object.
	a.Tapped, b.Tapped, mine.Tapped = true, true, true
	c := capture(e, at)
	if len(c.Reasons) != 0 {
		t.Fatalf("reasons %v", c.Reasons)
	}
	if got, want := c.Setup[other].Tapped, []string{name}; !reflect.DeepEqual(got, want) {
		t.Errorf("non-active tapped = %v, want %v", got, want)
	}
	if got := c.Setup[other].TappedAfterUntap; len(got) != 0 {
		t.Errorf("non-active tapped_after_untap = %v, want empty", got)
	}
	if got, want := c.Setup[active].TappedAfterUntap, []string{refOf(activeBF, mine)}; !reflect.DeepEqual(got, want) {
		t.Errorf("active tapped_after_untap = %v, want %v", got, want)
	}
	if got := c.Setup[active].Tapped; len(got) != 0 {
		t.Errorf("active tapped = %v, want empty", got)
	}

	// (b) One copy tapped, one not: the per-object field.
	b.Tapped = false
	c = capture(e, at)
	if got := c.Setup[other].Tapped; len(got) != 0 {
		t.Errorf("mixed copies: non-active tapped = %v, want empty", got)
	}
	if got, want := c.Setup[other].TappedAfterUntap, []string{name}; !reflect.DeepEqual(got, want) {
		t.Errorf("mixed copies: non-active tapped_after_untap = %v, want %v", got, want)
	}
}

// A permanent the active player tapped before the cut is lowered with
// tapped_after_untap, so the runner's untap step does not undo it.
func TestWindowsTappedAfterUntap(t *testing.T) {
	reg, pairs := setPairs(t, "DSK", 12)
	byStatus := map[string]int{}
	var tappedWin, activeTapped, okActive int
	for g, p := range pairs {
		ls, _, _, err := Windows(reg, p, uint64(100+g), game.Options{Set: "DSK"})
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range ls {
			var any bool
			for _, s := range l.Item.Setup {
				any = any || len(s.TappedAfterUntap)+len(s.Tapped) > 0
			}
			if !any {
				continue
			}
			tappedWin++
			act := len(l.Item.Setup["p0"].TappedAfterUntap)+len(l.Item.Setup["p0"].Tapped) > 0
			if act {
				activeTapped++
			}
			c := CheckWindow(reg, l)
			byStatus[c.Status]++
			if c.Status == StatusOK && act {
				okActive++
			}
			if c.Status != StatusOK {
				// A Clone-renamed token (Niko, Light of Hope's Shards) is
				// named by its pre-copy ref, which the runner no longer
				// resolves (ticket xe-window-copied-token-ref). The window
				// was refused for its Aura until Auras were placed attached.
				if strings.Contains(c.Gorge, ":token:") && strings.Contains(c.Gorge, "names no object") {
					t.Logf("%s: known copied-token ref gap: %s", l.Item.ID, c.Gorge)
					continue
				}
				t.Errorf("%s: %s at step %d, %s: gorge %s, tape %s %v", l.Item.ID, c.Status, c.Step, c.Field, c.Gorge, c.Tape, c.Fails)
			}
		}
	}
	t.Logf("windows with a tapped permanent: %d (active seat %d, lower_ok active %d); by status %v", tappedWin, activeTapped, okActive, byStatus)
	if okActive == 0 {
		t.Fatal("no window with an active-seat tapped permanent lowered to lower_ok")
	}
}

func TestRemapRenamedSetupToken(t *testing.T) {
	c := &cut{Refs: map[state.ObjID]string{
		83: "p0:token:Shard Token",
		20: "p0:Niko, Light of Hope",
	}, MaxID: 100}
	for _, tc := range []struct {
		id         state.ObjID
		live, want string
	}{
		{83, "p0:token:Hardened Escort", "p0:token:Hardened Escort"},
		{83, "p0:token:Shard Token", "p0:token:Shard Token"},
		{83, "", "p0:token:Shard Token"},
		{20, "p0:Niko, Light of Hope", "p0:Niko, Light of Hope"},
		{101, "p0:token:Soldier", "p0:token:Soldier"},
	} {
		if got := c.remap(tc.id, tc.live); got != tc.want {
			t.Errorf("remap(%d, %q) = %q, want %q", tc.id, tc.live, got, tc.want)
		}
	}
}

// DSK pair 10 turn 11 makes a Shard token a copy of Hardened Escort; the
// window's attack names it by the copied name.
func TestWindowsRenamedTokenRef(t *testing.T) {
	reg, pairs := setPairs(t, "DSK", 11)
	ls, _, tp, err := Windows(reg, pairs[10], 110, game.Options{Set: "DSK"})
	if err != nil {
		t.Fatal(err)
	}
	id := windowID(tp, 11)
	for _, l := range ls {
		if l.Item.ID != id {
			continue
		}
		if c := CheckWindow(reg, l); c.Status != StatusOK {
			t.Errorf("%s: status %s at step %d: %s", id, c.Status, c.Step, c.Gorge)
		}
		return
	}
	t.Fatalf("window %s not lowered", id)
}
