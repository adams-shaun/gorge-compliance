package lower

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
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

// TestLowerCastCarriesPayment: the pool mana a cast actually spent is the
// cast step's payment, not something each follower may choose again. Gorge
// logs the spend as negative ManaAdd events, so the tape carries it on the
// entry (or the entry that finishes the cast, when targets/modes are asked
// before payment); the lowering must merge the group's spend onto the step.
func TestLowerCastCarriesPayment(t *testing.T) {
	const ref = "p0:Intrepid Stablemaster"
	cases := []struct {
		name   string
		group  []game.Entry
		pay    string
		reason string
	}{
		{
			name:  "spend on the cast entry",
			group: []game.Entry{{Kind: "priority", Seat: 0, Picks: []game.Pick{{Kind: "cast", Obj: 1, Ref: ref}}, Spent: "R"}},
			pay:   "R",
		},
		{
			name: "spend after a target ask",
			group: []game.Entry{
				{Kind: "priority", Seat: 0, Picks: []game.Pick{{Kind: "cast", Obj: 1, Ref: ref}}},
				{Kind: "target", Seat: 0, Spent: "R"},
			},
			pay: "R",
		},
		{
			name:  "no spend means no payment",
			group: []game.Entry{{Kind: "priority", Seat: 0, Picks: []game.Pick{{Kind: "cast", Obj: 1, Ref: ref}}}},
			pay:   "",
		},
		{
			name:  "two colours keep their units",
			group: []game.Entry{{Kind: "priority", Seat: 0, Picks: []game.Pick{{Kind: "cast", Obj: 1, Ref: ref}}, Spent: "RG"}},
			pay:   "RG",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps, _, pay, _, why := lowerGroup(nil, seatMap{}, tc.group)
			if why != nil {
				t.Fatalf("lowerGroup: %s", why.reason)
			}
			if len(steps) != 1 || steps[0].Op != "cast" {
				t.Fatalf("steps = %+v, want one cast", steps)
			}
			if len(pay) != len(steps) || pay[0] != tc.pay {
				t.Errorf("cast payment = %v, want [%q] (the mana the log spent)", pay, tc.pay)
			}
		})
	}

	// The synthetic groups above pin the lowering; this pins the other half of
	// the path, the recorder that reads a real gorge log. A game 0 FDN cast
	// must carry the negative-ManaAdd spend it actually paid, so reverting the
	// recorder cannot leave the test green.
	t.Run("recorded game", func(t *testing.T) {
		reg, tapes := playFDN(t, 1)
		tp := tapes[0]
		l, err := Tape(reg, tp)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(l.Item.Raw(), []byte(`"payment"`)) {
			t.Fatal("payment leaked into the scenario Raw(); gorge's runner rejects an unknown step field")
		}
		casts := 0
		if len(l.Item.Payment) != len(l.Item.Steps) {
			t.Fatalf("%d payments for %d steps", len(l.Item.Payment), len(l.Item.Steps))
		}
		for i, st := range l.Item.Steps {
			if st.Op != "cast" {
				continue
			}
			casts++
			end := l.StepEntry[i]
			if end < 0 || end >= len(tp.Entries) {
				t.Fatalf("cast %d has no tape entry", i)
			}
			start := end
			for start > 0 && tp.Entries[start].Kind != "priority" {
				start--
			}
			want := ""
			for k := start; k <= end; k++ {
				want += tp.Entries[k].Spent
			}
			if l.Item.Payment[i] != want {
				t.Errorf("cast %d %s payment = %q, tape spent %q (entries %d..%d)", i, st.Card, l.Item.Payment[i], want, start, end)
			}
		}
		if casts == 0 {
			t.Fatal("game 0 lowered no cast: the recorder path is unchecked")
		}
	})
}

// TestLowerDropsRefusedCast: a cast or activation the live game left the whole
// state unchanged on did not happen, so it is not a replayable step. A refused
// Spree cast is the measured shape (xe:OTJ:0:2:w3): the card stays in hand,
// the pool is unchanged and the stack is empty afterwards.
func TestLowerDropsRefusedCast(t *testing.T) {
	d := game.Digest{Turn: 1, Step: "main1", Active: 0, Priority: 0}
	prio := func(p game.Pick, d game.Digest) game.Entry {
		return game.Entry{Kind: "priority", Seat: 0, Turn: 1, Step: "main1",
			Picks: []game.Pick{p}, Digest: d}
	}
	cast := prio(game.Pick{Kind: "cast", Ref: "p0:Phantom Interference"}, d)
	pass := prio(game.Pick{Kind: "pass"}, d)

	ops := func(es ...game.Entry) []string {
		var l Lowered
		l.lowerEntries(nil, seatMap{}, es, 0, false)
		var out []string
		for _, s := range l.Item.Steps {
			out = append(out, s.Op)
		}
		return out
	}
	if got := ops(cast, pass); !reflect.DeepEqual(got, []string{"pass"}) {
		t.Fatalf("no-op cast: steps %v, want [pass]", got)
	}
	// A real cast that leaves the visible state unchanged but pushes an object
	// on the stack (a {X}=0 ability that destroys nothing) must stay: the stack
	// top changed from before the group to after it.
	castStack := prio(game.Pick{Kind: "cast", Ref: "p0:Phantom Interference"}, d)
	castStack.StackTop = 7
	passStack := prio(game.Pick{Kind: "pass"}, d)
	passStack.StackTop = 9
	if got := ops(castStack, passStack); !reflect.DeepEqual(got, []string{"cast", "pass"}) {
		t.Fatalf("stack-changing cast: steps %v, want [cast pass]", got)
	}
	// A group whose digest differs is a real action and must stay.
	d2 := d
	d2.Priority = 1
	if got := ops(prio(game.Pick{Kind: "cast", Ref: "p0:Phantom Interference"}, d2), pass); !reflect.DeepEqual(got, []string{"cast", "pass"}) {
		t.Fatalf("changed cast: steps %v, want [cast pass]", got)
	}
	// The same contract for a non-mana activation.
	act := prio(game.Pick{Kind: "ability", Ability: 0, Ref: "p0:Phantom Interference"}, d)
	if got := ops(act, pass); !reflect.DeepEqual(got, []string{"pass"}) {
		t.Fatalf("no-op activate: steps %v, want [pass]", got)
	}
	// The same contract for an activation that pushes an object.
	actStack := prio(game.Pick{Kind: "ability", Ability: 0, Ref: "p0:Phantom Interference"}, d)
	actStack.StackTop = 7
	if got := ops(actStack, passStack); !reflect.DeepEqual(got, []string{"activate", "pass"}) {
		t.Fatalf("stack-changing activate: steps %v, want [activate pass]", got)
	}
	// Only cast/activate are dropped: a no-op land drop and a pass stay.
	land := prio(game.Pick{Kind: "play_land", Ref: "p0:Island"}, d)
	if got := ops(land, pass); !reflect.DeepEqual(got, []string{"play", "pass"}) {
		t.Fatalf("no-op play: steps %v, want [play pass]", got)
	}
	// The last group of a whole tape has no after-state: keep it rather than
	// inventing one.
	if got := ops(cast); !reflect.DeepEqual(got, []string{"cast"}) {
		t.Fatalf("last group, no after-state: steps %v, want [cast]", got)
	}
	// The last group of a window does have one: the next turn's first entry,
	// which windowTape keeps at the index after the window's own entries.
	window := func(against game.Digest, es ...game.Entry) []string {
		l := Lowered{Against: &game.Tape{Entries: []game.Entry{{Digest: d}, {Digest: against}}}}
		l.lowerEntries(nil, seatMap{}, es, 0, false)
		var out []string
		for _, s := range l.Item.Steps {
			out = append(out, s.Op)
		}
		return out
	}
	if got := window(d, cast); len(got) != 0 {
		t.Fatalf("window no-op cast: steps %v, want none", got)
	}
	if got := window(d2, cast); !reflect.DeepEqual(got, []string{"cast"}) {
		t.Fatalf("window changed cast: steps %v, want [cast]", got)
	}
}

// TestLowerResolvesTriggerTargetAfterPass: a pass that resolves a creature
// spell and then puts its ETB trigger on the stack cannot carry the trigger's
// target answer. The pass's own object has left the stack, and a pass answers
// exactly one priority decision, so the group lowers as one `resolve` step
// that answers the pending target and stops at the next empty-stack priority.
// Every other unsettled pass keeps the truncation.
func TestLowerResolvesTriggerTargetAfterPass(t *testing.T) {
	d := game.Digest{Turn: 1, Step: "main1", Active: 0, Priority: 0}
	prio := func(seat int, p game.Pick, top uint32) game.Entry {
		return game.Entry{Kind: "priority", Seat: seat, Turn: 1, Step: "main1",
			Picks: []game.Pick{p}, StackTop: stateID(top), Digest: d}
	}
	cast := game.Entry{Kind: "priority", Seat: 0, Turn: 1, Step: "main1", Digest: d,
		Picks: []game.Pick{{Kind: "cast", Ref: "p0:Summit Intimidator"}}}
	pass0 := prio(0, game.Pick{Kind: "pass"}, 7) // p0 hands priority to p1
	pass1 := prio(1, game.Pick{Kind: "pass"}, 7) // p1's pass resolves the creature
	target := game.Entry{Kind: "target", Seat: 0, Turn: 1, Step: "main1", Digest: d,
		StackTop: 9, Max: 1,
		Picks: []game.Pick{{Kind: "permanent", Obj: stateID(42), Ref: "p1:Wayspeaker Bodyguard"}}}
	pass2 := prio(0, game.Pick{Kind: "pass"}, 9) // trigger on the stack
	pass3 := prio(1, game.Pick{Kind: "pass"}, 9)
	pass4 := prio(0, game.Pick{Kind: "pass"}, 0) // empty stack, next priority

	t.Run("trigger target after a resolving pass", func(t *testing.T) {
		var l Lowered
		l.lowerEntries(nil, seatMap{}, []game.Entry{cast, pass0, pass1, target, pass2, pass3, pass4}, 0, false)
		if l.Truncated != "" {
			t.Fatalf("truncated %q at %d, want a resolve step", l.Truncated, l.TruncatedAt)
		}
		var ops []string
		for _, s := range l.Item.Steps {
			ops = append(ops, s.Op)
		}
		if want := []string{"cast", "pass", "resolve", "pass"}; !reflect.DeepEqual(ops, want) {
			t.Fatalf("steps %v, want %v", ops, want)
		}
		res := l.Item.Steps[2]
		if res.Seat != 0 || len(res.Answers) != 1 || res.Answers[0].Kind != "target" ||
			!reflect.DeepEqual(res.Answers[0].Pick, []string{"p1:Wayspeaker Bodyguard"}) {
			t.Fatalf("resolve step %+v, want p0's target answer", res)
		}
		// The resolve step carries entries 2..5 (the pass, the ask and the
		// passes that resolve the trigger), so its checkpoint is entry 6.
		if l.StepEntry[2] != 5 {
			t.Fatalf("resolve StepEntry = %d, want 5 (checkpoint at entry 6)", l.StepEntry[2])
		}
	})

	t.Run("choose still truncates", func(t *testing.T) {
		choose := game.Entry{Kind: "choose", Seat: 0, Turn: 1, Step: "main1",
			StackTop: stateID(9), Digest: d, Picks: []game.Pick{{Kind: "no"}}}
		var l Lowered
		l.lowerEntries(nil, seatMap{}, []game.Entry{pass1, choose, pass4}, 0, false)
		if want := "unsupported_kind:ask_after_pass:choose"; l.Truncated != want {
			t.Fatalf("truncated %q at %d, want %q", l.Truncated, l.TruncatedAt, want)
		}
	})

	t.Run("empty-stack pass still truncates", func(t *testing.T) {
		empty := prio(0, game.Pick{Kind: "pass"}, 0)
		var l Lowered
		l.lowerEntries(nil, seatMap{}, []game.Entry{empty, target, pass4}, 0, false)
		if want := "unsupported_kind:ask_after_pass:target"; l.Truncated != want {
			t.Fatalf("truncated %q at %d, want %q", l.Truncated, l.TruncatedAt, want)
		}
	})

	t.Run("same-stack target keeps the owed answer", func(t *testing.T) {
		same := target
		same.StackTop = stateID(7)
		var l Lowered
		l.lowerEntries(nil, seatMap{}, []game.Entry{pass1, same, pass4}, 0, false)
		if l.Truncated != "" {
			t.Fatalf("truncated %q at %d, want the pass to carry the answer", l.Truncated, l.TruncatedAt)
		}
		var ops []string
		for _, s := range l.Item.Steps {
			ops = append(ops, s.Op)
		}
		if want := []string{"pass", "pass"}; !reflect.DeepEqual(ops, want) {
			t.Fatalf("steps %v, want %v", ops, want)
		}
		// The pass poses the ask and owes the answer to the next step, which
		// the runner answers as it passes while the object is still on top.
		owed := l.Item.Steps[1]
		if len(owed.Answers) != 1 || owed.Answers[0].Kind != "target" ||
			!reflect.DeepEqual(owed.Answers[0].Pick, []string{"p1:Wayspeaker Bodyguard"}) {
			t.Fatalf("next step %+v, want the owed target answer", owed)
		}
	})
}
