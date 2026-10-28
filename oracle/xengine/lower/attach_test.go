package lower

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// auraCut is a cut the gate accepted with an Aura attached to a permanent: the
// capture, and for each such Aura the entry setup must hold.
type auraCut struct {
	c    *cut
	want []wantAttach // in seat then battlefield order of the capture
}

type wantAttach struct {
	seat int
	oraclegen.SetupAttach
}

// auraCuts plays the first n pairs of set (seeded 100+game) and returns, by
// game, every accepted cut with an attached battlefield Aura.
func auraCuts(t *testing.T, set string, n int) map[int][]auraCut {
	t.Helper()
	reg, pairs := setPairs(t, set, n)
	out := map[int][]auraCut{}
	for g, p := range pairs {
		_, err := game.Play(reg, p, uint64(100+g), game.Options{Set: set, OnCut: func(e *rules.Engine, entry int) {
			c := capture(e, entry)
			if len(c.Reasons) != 0 {
				return
			}
			var ac auraCut
			ac.c = c
			for i := range e.G.Objs {
				o := &e.G.Objs[i]
				if o.Zone != state.ZBattlefield || o.AttachedTo == 0 || !slices.Contains(e.Derived(o.ID).Types, "Aura") {
					continue
				}
				prefix := fmt.Sprintf("p%d:", o.Controller)
				ac.want = append(ac.want, wantAttach{int(o.Controller), oraclegen.SetupAttach{
					Card: strings.TrimPrefix(c.Refs[o.ID], prefix), To: c.Refs[o.AttachedTo],
				}})
			}
			if len(ac.want) > 0 {
				out[g] = append(out[g], ac)
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// An Aura attached to a permanent is placed attached by setup (CR 303.4f), not
// by a step after the setup checkpoint: the gate accepts the cut, the seat's
// attached list names the Aura and its bearer, and the lowered item has no
// attach step for it. A swap of the window's seats relabels the bearer's ref
// and leaves the seat-relative card alone.
func TestGateAttachedAura(t *testing.T) {
	cuts := auraCuts(t, "DSK", 40)
	var found *auraCut
	var seed int
	for g := 0; g < 40 && found == nil; g++ {
		if len(cuts[g]) > 0 {
			found, seed = &cuts[g][0], 100+g
		}
	}
	if found == nil {
		t.Fatal("no accepted DSK cut with an attached Aura in the first 40 pairs")
	}
	c := found.c
	t.Logf("aura cut: DSK seed %d, entry %d, turn %d, %d attached Aura(s)", seed, c.Entry, c.Turn, len(found.want))
	for _, w := range found.want {
		if !slices.Contains(c.Setup[w.seat].Attached, w.SetupAttach) {
			t.Errorf("seat %d attached %+v does not list %+v", w.seat, c.Setup[w.seat].Attached, w.SetupAttach)
		}
		if strings.HasPrefix(w.Card, "p") && strings.Contains(w.Card, ":") {
			t.Errorf("attached card %q is not seat-relative", w.Card)
		}
		full := fmt.Sprintf("p%d:%s", w.seat, w.Card)
		for _, st := range c.Attach {
			if st.Card == full {
				t.Errorf("the Aura %s also has an attach prelude step %+v", full, st)
			}
		}
		// The lowered item, both orientations.
		for _, swap := range []bool{false, true} {
			m := seatMap{swap: swap}
			item := c.item(m, "xe:test:aura", "aura")
			seat := item.Scenario.Setup[fmt.Sprintf("p%d", m.seat(w.seat))]
			want := oraclegen.SetupAttach{Card: w.Card, To: m.ref(w.To)}
			if !slices.Contains(seat.Attached, want) {
				t.Errorf("swap=%v: seat %d attached %+v does not list %+v", swap, m.seat(w.seat), seat.Attached, want)
			}
			if swap && want.To == w.To {
				t.Errorf("swap=%v: To %q kept its seat", swap, w.To)
			}
		}
	}
	// The window lowered from the same game carries it, with no attach step.
	reg, pairs := setPairs(t, "DSK", seed-100+1)
	ls, _, tp, err := Windows(reg, pairs[seed-100], uint64(seed), game.Options{Set: "DSK"})
	if err != nil {
		t.Fatal(err)
	}
	var lowered bool
	for _, l := range ls {
		if l.Item.ID != windowID(tp, c.Turn) {
			continue
		}
		lowered = true
		for _, w := range found.want {
			m := seatMap{swap: l.Swap}
			seat := l.Item.Scenario.Setup[fmt.Sprintf("p%d", m.seat(w.seat))]
			if !slices.Contains(seat.Attached, oraclegen.SetupAttach{Card: w.Card, To: m.ref(w.To)}) {
				t.Errorf("%s: lowered seat attached %+v does not list %+v", l.Item.ID, seat.Attached, w)
			}
			card := m.ref(fmt.Sprintf("p%d:%s", w.seat, w.Card))
			for _, st := range l.Item.Steps {
				if st.Op == "attach" && st.Card == card {
					t.Errorf("%s: the Aura %s has an attach step %+v", l.Item.ID, card, st)
				}
			}
		}
	}
	if !lowered {
		t.Fatalf("no lowered window %s", windowID(tp, c.Turn))
	}
}

// Every window of the first 12 DSK pairs: the ones that carry an attached Aura
// lower, and at least one agrees with the recorded game through every step. A
// placement disagreement (the setup checkpoint, or attached_to later) fails.
func TestWindowsAttachedAuras(t *testing.T) {
	reg, pairs := setPairs(t, "DSK", 12)
	status := map[string]int{}
	var attachedWin, attachedOK int
	for g, p := range pairs {
		ls, rs, _, err := Windows(reg, p, uint64(100+g), game.Options{Set: "DSK"})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			status["refused"]++
			for _, why := range r.Reasons {
				if why == reasonAuraUnattached {
					t.Errorf("%s refused for %s", r.ID, why)
				}
			}
		}
		for _, l := range ls {
			var hasAttached bool
			for _, s := range l.Item.Scenario.Setup {
				hasAttached = hasAttached || len(s.Attached) > 0
			}
			c := CheckWindow(reg, l)
			status[c.Status]++
			if !hasAttached {
				continue
			}
			attachedWin++
			if c.Status == StatusOK {
				attachedOK++
				continue
			}
			// The setup checkpoint holds the placement (Prelude is 0, so it
			// compares attached_to), and a later checkpoint that differs on
			// attached_to means the Aura moved: both are the runner's
			// placement disagreeing. Any other gap is the window's own (a
			// step the lowering cannot yet carry) and is only logged.
			if c.Step == -1 || strings.Contains(c.Field, "attached") {
				t.Errorf("%s: placement disagrees at step %d, %s: gorge %s, tape %s", l.Item.ID, c.Step, c.Field, c.Gorge, c.Tape)
			} else {
				t.Logf("%s: unrelated gap at step %d, %s: gorge %s, tape %s %v", l.Item.ID, c.Step, c.Field, c.Gorge, c.Tape, c.Fails)
			}
		}
	}
	t.Logf("DSK 12 pairs: refused %d, lower_ok %d, lower_gap %d; windows with an attached Aura %d (lower_ok %d)",
		status["refused"], status[StatusOK], status[StatusGap], attachedWin, attachedOK)
	if attachedOK == 0 {
		t.Error("no window with an attached Aura lowered to lower_ok")
	}
}
