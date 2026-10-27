package lower

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
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
	var got *rules.Engine
	var at int
	_, err := game.Play(reg, p, seed, game.Options{Set: "FDN", OnCut: func(e *rules.Engine, entry int) {
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
