package lower

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const manifestDir = "../../compliance/manifests"

// playFDN plays FDN games 0..n-1 of chunk 0, seeded 100+game as xe-3's tests
// are.
func playFDN(t *testing.T, n int) (*cards.Registry, []*game.Tape) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cands, _, err := decks.Candidates(reg, manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	var out []*game.Tape
	for g := 0; g < n; g++ {
		p := decks.Pair(cands, cov.New(), "FDN", 0, g, 7)
		tp, err := game.Play(reg, p, uint64(100+g), game.Options{Set: "FDN"})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, tp)
	}
	return reg, out
}

func lowerOK(t *testing.T, reg *cards.Registry, tp *game.Tape) Lowered {
	t.Helper()
	l, err := Tape(reg, tp)
	if err != nil {
		t.Fatal(err)
	}
	if c := SelfCheck(reg, l, tp); c.Status != StatusOK {
		t.Fatalf("%s: %s at step %d, %s: gorge %s, tape %s %v", l.Item.ID, c.Status, c.Step, c.Field, c.Gorge, c.Tape, c.Fails)
	}
	return l
}

func TestLowerTapeSelfChecks(t *testing.T) {
	reg, tapes := playFDN(t, 4)
	hist := map[string]int{}
	var kept, total int
	for i, tp := range tapes {
		l, err := Tape(reg, tp)
		if err != nil {
			t.Fatal(err)
		}
		c := SelfCheck(reg, l, tp)
		why := l.Truncated
		if why == "" {
			why = "none"
		}
		hist[why]++
		end := l.TruncatedAt
		if l.Truncated == "" {
			end = len(tp.Entries)
		}
		kept += end
		total += len(tp.Entries)
		t.Logf("game %d: %s starter=%d entries=%d lowered=%d steps=%d (%d unchecked taps) truncated=%q at=%d self-check=%s",
			i, l.Item.ID, tp.Starter, len(tp.Entries), end, len(l.Item.Steps), countTaps(l), l.Truncated, l.TruncatedAt, c.Status)
		if c.Status != StatusOK {
			t.Errorf("game %d: %s at step %d, %s: gorge %s, tape %s %v", i, c.Status, c.Step, c.Field, c.Gorge, c.Tape, c.Fails)
		}
		if len(l.Item.Steps) == 0 {
			t.Errorf("game %d: nothing lowered (%s)", i, l.Truncated)
		}
		if len(l.StepEntry) != len(l.Item.Steps) {
			t.Errorf("game %d: %d StepEntry for %d steps", i, len(l.StepEntry), len(l.Item.Steps))
		}
	}
	var keys []string
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, hist[k]))
	}
	t.Logf("truncation reasons: %s; %d of %d tape entries lowered (%d%%)", strings.Join(parts, " "), kept, total, 100*kept/total)
}

func countTaps(l Lowered) int {
	n := 0
	for _, e := range l.StepEntry {
		if e < 0 {
			n++
		}
	}
	return n
}

func TestLowerDeterministic(t *testing.T) {
	reg, tapes := playFDN(t, 2)
	again := playFDN2(t, reg, 2)
	for i, tp := range tapes {
		a, err := Tape(reg, tp)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := Tape(reg, tp)
		c, _ := Tape(reg, again[i])
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		jc, _ := json.Marshal(c)
		if !bytes.Equal(ja, jb) {
			t.Errorf("tape %d: lowering twice differs", i)
		}
		if !bytes.Equal(ja, jc) {
			t.Errorf("tape %d: lowering a replayed tape differs", i)
		}
		if !bytes.Equal(a.Item.Raw(), b.Item.Raw()) {
			t.Errorf("tape %d: Raw differs", i)
		}
	}
}

// playFDN2 replays the same games on the same registry.
func playFDN2(t *testing.T, reg *cards.Registry, n int) []*game.Tape {
	t.Helper()
	cands, _, err := decks.Candidates(reg, manifestDir, "FDN")
	if err != nil {
		t.Fatal(err)
	}
	var out []*game.Tape
	for g := 0; g < n; g++ {
		tp, err := game.Play(reg, decks.Pair(cands, cov.New(), "FDN", 0, g, 7), uint64(100+g), game.Options{Set: "FDN"})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, tp)
	}
	return out
}

// refCard is the card name inside a runner ref ("p0:Forest#2" -> "Forest").
func refCard(ref string) string {
	_, name, _ := strings.Cut(ref, ":")
	if i := strings.LastIndexByte(name, '#'); i >= 0 {
		name = name[:i]
	}
	return name
}

// tapViolations lists the casts of a spell with a mana cost that have neither
// an explicit mana tap before them nor floating mana at the cast decision.
func tapViolations(reg *cards.Registry, l Lowered, tp *game.Tape) []string {
	var bad []string
	for i, st := range l.Item.Steps {
		if st.Op != "cast" {
			continue
		}
		c, ok := reg.Lookup(refCard(st.Card))
		if !ok || c.Faces[0].ManaCost == "" || c.Faces[0].ManaCost == "0" {
			continue
		}
		tapped := false
		for k := i - 1; k >= 0 && l.StepEntry[k] < 0; k-- {
			tapped = tapped || l.Item.Steps[k].Op == "activate"
		}
		// The cast's own entry is the priority entry at or before the one
		// the step ends on (asks come after it).
		e := l.StepEntry[i]
		for e > 0 && tp.Entries[e].Kind != "priority" {
			e--
		}
		pool := tp.Entries[e].Digest.Players[tp.Entries[e].Seat].Pool
		if !tapped && pool == "" {
			bad = append(bad, fmt.Sprintf("step %d: cast %s with no mana tap and an empty pool", i, st.Card))
		}
	}
	return bad
}

func TestLowerExplicitManaTaps(t *testing.T) {
	reg, tapes := playFDN(t, 4)
	casts := 0
	for i, tp := range tapes {
		l, err := Tape(reg, tp)
		if err != nil {
			t.Fatal(err)
		}
		for _, st := range l.Item.Steps {
			if st.Op == "cast" {
				casts++
			}
		}
		for _, v := range tapViolations(reg, l, tp) {
			t.Errorf("game %d: %s", i, v)
		}
		// M1: with the mana taps dropped, a cast pays through the runner's own
		// payment, and this check must notice.
		m := dropTaps(l)
		if n := len(l.Item.Steps) - len(m.Item.Steps); n > 0 {
			if len(tapViolations(reg, m, tp)) == 0 {
				t.Errorf("game %d: dropping %d mana taps went unnoticed", i, n)
			}
		}
	}
	if casts == 0 {
		t.Fatal("no cast in 4 games: nothing was checked")
	}
	t.Logf("%d casts checked", casts)
}

// dropTaps is l without its explicit mana taps (the mutation M1).
func dropTaps(l Lowered) Lowered {
	out := l
	out.Item.Steps, out.StepEntry = nil, nil
	for i, st := range l.Item.Steps {
		if l.StepEntry[i] < 0 && st.Op == "activate" {
			continue
		}
		out.Item.Steps = append(out.Item.Steps, st)
		out.StepEntry = append(out.StepEntry, l.StepEntry[i])
	}
	return out
}

func TestLowerSeatRelabel(t *testing.T) {
	reg, tapes := playFDN(t, 4)
	n := 0
	for i, tp := range tapes {
		if tp.Starter != 1 {
			continue
		}
		n++
		l := lowerOK(t, reg, tp)
		if !l.Swap {
			t.Errorf("game %d: starter is seat 1 but Swap is false", i)
		}
		sc := l.Item.Scenario
		if got, want := sc.Setup["p0"].Hand, tp.Open[1].Hand; strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("game %d: p0's hand %v, want the starter's %v", i, got, want)
		}
		if got, want := sc.Setup["p1"].LibraryTop, tp.Open[0].Library; strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("game %d: p1's library differs from the other seat's", i)
		}
		// The first decision of the scenario belongs to the starter, p0, and
		// every step of the tape's seat 0 is a step of scenario seat 1.
		if sc.Steps[0].Seat != 0 {
			t.Errorf("game %d: first step is seat %d, want 0", i, sc.Steps[0].Seat)
		}
		for k, st := range sc.Steps {
			if e := l.StepEntry[k]; e >= 0 && tp.Entries[e].Seat != 1-st.Seat {
				t.Errorf("game %d step %d: scenario seat %d for tape seat %d", i, k, st.Seat, tp.Entries[e].Seat)
			}
		}
	}
	if n == 0 {
		t.Fatal("none of the 4 games starts on seat 1")
	}
}

func TestSelfCheckCatchesTamper(t *testing.T) {
	reg, tapes := playFDN(t, 4)
	done := false
	for i, tp := range tapes {
		l := lowerOK(t, reg, tp)
		step := -1
		for k, st := range l.Item.Steps {
			if len(st.Targets) > 0 {
				step = k
				break
			}
		}
		if step < 0 {
			continue
		}
		// Retarget the first targeted step to a card the runner cannot name.
		st := &l.Item.Steps[step]
		orig := st.Targets[0]
		st.Targets = append([]string{"p1:No Such Card"}, st.Targets[1:]...)
		c := SelfCheck(reg, l, tp)
		if c.Status != StatusGap || c.Step != step {
			t.Fatalf("game %d: tampered %s at step %d: self-check %s at step %d (%s)", i, orig, step, c.Status, c.Step, c.Field)
		}
		t.Logf("game %d: step %d %s -> %s: %s, %s: %s", i, step, orig, st.Targets[0], c.Status, c.Field, c.Gorge)
		done = true
		break
	}
	if !done {
		t.Fatal("no lowered step with targets in 4 games")
	}

	// A tape whose recorded state is off by one life is caught at the step
	// the runner's checkpoint disagrees, naming the digest field.
	tp := tapes[1]
	l := lowerOK(t, reg, tp)
	step := len(l.Item.Steps) / 2
	for l.StepEntry[step] < 0 {
		step++
	}
	e := l.StepEntry[step] + 1
	seat := 0
	if l.Swap {
		seat = 1
	}
	tp.Entries[e].Digest.Players[seat].Life++
	c := SelfCheck(reg, l, tp)
	if c.Status != StatusGap || c.Step != step || !strings.HasSuffix(c.Field, ".life") {
		t.Fatalf("tampered digest of entry %d: %s at step %d, %s (want step %d, life)", e, c.Status, c.Step, c.Field, step)
	}
	t.Logf("digest of entry %d: %s at step %d, %s: gorge %s, tape %s", e, c.Status, c.Step, c.Field, c.Gorge, c.Tape)
}

func TestTruncationReasons(t *testing.T) {
	m := seatMap{}
	// An attack at two defenders, or at a battle, ends the lowering.
	split := game.Entry{Kind: "attackers", Picks: []game.Pick{{Kind: "attacker", Ref: "p0:A", Player: 1}, {Kind: "attacker", Ref: "p0:B", Player: 2}}}
	if _, why := lowerAttack(m, split); why == nil || why.reason != ReasonAttackSplit {
		t.Errorf("split attack: %v", why)
	}
	walker := game.Entry{Kind: "attackers", Picks: []game.Pick{{Kind: "attacker", Ref: "p0:A", Player: 1, Battle: 9}}}
	if _, why := lowerAttack(m, walker); why == nil || why.reason != ReasonAttackNonPlayer {
		t.Errorf("planeswalker attack: %v", why)
	}
	// An object with no ref is never given an invented one.
	noRef := game.Entry{Kind: "priority", Picks: []game.Pick{{Kind: "play_land"}}}
	if _, why := lowerPriority(nil, m, noRef, nil); why == nil || why.reason != ReasonUnbindableRef {
		t.Errorf("land with no ref: %v", why)
	}
	// An ask after a pass is only answerable when the pass left a resolution
	// pending on its object, or the ask is a trigger order.
	pass := func(top uint32) game.Entry {
		return game.Entry{Kind: "priority", Picks: []game.Pick{{Kind: "pass"}}, StackTop: stateID(top)}
	}
	ask := func(kind string, top uint32) game.Entry { return game.Entry{Kind: kind, StackTop: stateID(top)} }
	for _, c := range []struct {
		g    []game.Entry
		want bool // truncated
	}{
		{[]game.Entry{pass(0), ask("target", 0)}, true},
		{[]game.Entry{pass(5), ask("choose", 5)}, false},
		{[]game.Entry{pass(5), ask("target", 6)}, true},
		{[]game.Entry{pass(5), ask("choose", 5), ask("target", 0)}, true},
		{[]game.Entry{pass(0), ask("trigger_order", 7)}, false},
	} {
		if got := unsettled(c.g) != nil; got != c.want {
			t.Errorf("%+v: truncated %v, want %v", c.g, got, c.want)
		}
	}
}

func TestSeatMapRefs(t *testing.T) {
	m := seatMap{swap: true}
	for in, want := range map[string]string{
		"p0:Forest#2": "p1:Forest#2", "p1:token:Soldier": "p0:token:Soldier", "p1:ability:Map#2": "p0:ability:Map#2", "": "",
	} {
		if got := m.ref(in); got != want {
			t.Errorf("ref(%q) = %q, want %q", in, got, want)
		}
	}
	if got := m.player(1); got != "p0" {
		t.Errorf("player(1) = %q", got)
	}
	if got := (seatMap{}).ref("p1:Forest"); got != "p1:Forest" {
		t.Errorf("identity relabel changed a ref: %q", got)
	}
}

func stateID(n uint32) state.ObjID { return state.ObjID(n) }
