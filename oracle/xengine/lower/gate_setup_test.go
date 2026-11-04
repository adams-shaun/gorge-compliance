package lower

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// plantBattlefieldCard replaces the card of one battlefield permanent of e with
// the named corpus card and returns the object, so a test can exercise the
// gate with a specific trigger or keyword carrier.
func plantBattlefieldCard(t *testing.T, reg *cards.Registry, e *rules.Engine, name string, pick func(o *state.Object) bool) *state.Object {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s is not in the corpus", name)
	}
	if len(c.Faces) != 1 {
		t.Fatalf("%s is not single-faced", name)
	}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.AttachedTo == 0 && o.Ability == nil && !o.IsToken && pick(o) {
			o.SetCard(c, 0)
			return o
		}
	}
	t.Fatalf("no battlefield permanent to plant %s on", name)
	return nil
}

// TestGateRefusesUntilEOTContinuous: a pump or Animate made earlier in the SAME
// turn is registered with UntilEOT and no other lifetime field. Setup cannot
// place it, and it is still live at the cut (it expires only in the cleanup
// step, CR 514.2), so the gate must refuse the window.
func TestGateRefusesUntilEOTContinuous(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	var id state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.Ability == nil && o.Card != nil {
			id = o.ID
			break
		}
	}
	if id == 0 {
		t.Fatal("no battlefield permanent to pump")
	}
	e.AddContinuous(state.ContinuousEffect{
		Source: id, Layer: state.LPT, Sub: state.SubModify,
		Affects: "Card.Self", Controller: e.G.Obj(id).Controller,
		AddPower: 1, AddToughness: 1, UntilEOT: true,
	})
	c := capture(e, at, true)
	if len(c.Reasons) != 1 || c.Reasons[0] != "field:Engine.continuous" {
		t.Fatalf("reasons %v, want [field:Engine.continuous]", c.Reasons)
	}
}

// TestWindowETBCounterNotDoubled: K:etbCounter is a Moved replacement the setup
// placement re-runs, so setup must subtract its grant from the counters it
// adds. The test plants a literal carrier with the one counter it entered with:
// the net is zero, and the runner's setup checkpoint must match the planted
// state instead of showing a second +1/+1 counter.
func TestWindowETBCounterNotDoubled(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	var carrier *cards.Card
	for _, name := range []string{"Mossborn Hydra", "Dockworker Drone", "District Mascot"} {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) != 1 {
			continue
		}
		param, ok := c.Faces[0].KeywordParam("etbCounter")
		if !ok {
			continue
		}
		kind, rest, _ := strings.Cut(param, ":")
		n, _, _ := strings.Cut(rest, ":")
		if kind == "P1P1" && n == "1" {
			carrier = c
			break
		}
	}
	if carrier == nil {
		t.Fatal("no literal +1/+1 etbCounter carrier in the corpus")
	}
	planted, at := expressibleCut(t, reg, pairs[0], 100)
	var swapped bool
	for i := range planted.G.Objs {
		o := &planted.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.AttachedTo == 0 && o.Ability == nil && !o.IsToken && !o.Tapped && len(o.Counters) == 0 {
			o.SetCard(carrier, 0)
			o.Counters = []state.Counter{{Kind: "P1P1", N: 1}}
			swapped = true
			break
		}
	}
	if !swapped {
		t.Fatal("no battlefield permanent to replace")
	}
	c := capture(planted, at, true)
	if len(c.Reasons) > 0 {
		t.Fatalf("the planted cut is refused: %v", c.Reasons)
	}
	name := carrier.Faces[0].Name
	var got map[string]int32
	for _, s := range c.Setup {
		if cs, ok := s.Counters[name]; ok {
			got = cs
		}
	}
	if len(got) != 0 {
		t.Fatalf("setup counters for %s = %v, want none (the entry grant is already carried)", name, got)
	}
	// Run the setup through the runner: its checkpoint must show the planted
	// state (one +1/+1 counter), not a second one.
	m := seatMap{swap: c.Active == 1}
	l := Lowered{Item: c.item(m, "xe:test:etbcounter", "etbcounter"), StepEntry: []int{-1}, Swap: m.swap}
	l.Item.Steps = []oraclegen.Step{{Op: "pass", Seat: 0}}
	wt := &game.Tape{Entries: []game.Entry{{Digest: game.DigestOfGame(planted)}}}
	wt.Entries[0].Digest.Turn = 1
	if chk := SelfCheck(reg, l, wt); chk.Status != StatusOK {
		t.Fatalf("setup checkpoint differs from the planted state: %s: gorge %s, tape %s %v", chk.Field, chk.Gorge, chk.Tape, chk.Fails)
	}
}

// TestGateRefusesSetupTapTrigger: a by-name tapped placement is a Tap in the
// setup drive, so a placed permanent with a "becomes tapped" trigger would
// resolve it. The tape has no such resolution, and setup cannot say so.
func TestGateRefusesSetupTapTrigger(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	// A by-name Tapped placement is the non-active seat's: the active seat's
	// untap step has already passed, so its taps are TappedAfterUntap, which
	// queues no trigger.
	victim := plantBattlefieldCard(t, reg, e, "Hemosymbic Mite", func(o *state.Object) bool {
		return o.Owner != e.G.Active && o.Owner == o.Controller
	})
	victim.Tapped = true
	if r := capture(e, at, true).Reasons; !slices.Contains(r, "setup_trigger:taps") {
		t.Fatalf("reasons %v, want setup_trigger:taps", r)
	}
}

// TestGateRefusesSetupCounterTrigger: the counters field is placed as a
// CounterChange, so a placed permanent with a "whenever counters are put on"
// trigger would resolve it.
func TestGateRefusesSetupCounterTrigger(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	sta := plantBattlefieldCard(t, reg, e, "Stalwart Successor", func(*state.Object) bool { return true })
	var counters bool
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o == sta || o.Zone != state.ZBattlefield || o.Ability != nil || o.IsToken {
			continue
		}
		o.Counters = []state.Counter{{Kind: "P1P1", N: 3}}
		if net, ok := setupCounters(o); ok && net["P1P1"] > 0 {
			counters = true
			break
		}
		o.Counters = nil
	}
	if !counters {
		t.Fatal("no battlefield permanent to put setup counters on")
	}
	if r := capture(e, at, true).Reasons; !slices.Contains(r, "setup_trigger:counter_added") {
		t.Fatalf("reasons %v, want setup_trigger:counter_added", r)
	}
}

// TestGateRefusesSetupLifeTrigger: the setup life field is a LifeChange, so a
// placed permanent with a "whenever you gain life" trigger would resolve it.
func TestGateRefusesSetupLifeTrigger(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	plantBattlefieldCard(t, reg, e, "Ajani's Pridemate", func(*state.Object) bool { return true })
	e.G.Players[0].Life = 22
	if r := capture(e, at, true).Reasons; !slices.Contains(r, "setup_trigger:life_gained") {
		t.Fatalf("reasons %v, want setup_trigger:life_gained", r)
	}
}

// TestGateRefusesSetupEntryTap: a non-active seat's untapped shock land is
// placed by a move that re-runs its "you may pay 2 life, otherwise it enters
// tapped" replacement. The runner has no answer for that replacement's
// unless-cost during placement, so it declines and taps the land; the
// non-active seat has no untap step before the cut, and the scenario schema
// has no untap field, so the window cannot be rebuilt. The active seat is
// exempt (its untap step restores the land), and a cut that already has the
// land tapped is expressible (the placement's tap agrees).
func TestGateRefusesSetupEntryTap(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)

	// A non-active seat's untapped shock land is refused.
	e, at := expressibleCut(t, reg, pairs[0], 100)
	victim := plantBattlefieldCard(t, reg, e, "Breeding Pool", func(o *state.Object) bool {
		return o.Owner != e.G.Active && o.Owner == o.Controller
	})
	victim.Tapped, victim.Counters = false, nil
	c := capture(e, at, true)
	if !hasReason(c, reasonEntryTap) {
		t.Fatalf("non-active untapped shock land: reasons %v, want %s", c.Reasons, reasonEntryTap)
	}

	// The same land on the active seat is expressible: the placement taps it
	// and turn 1's untap step restores it before the cut.
	e, at = expressibleCut(t, reg, pairs[0], 100)
	plantBattlefieldCard(t, reg, e, "Breeding Pool", func(o *state.Object) bool {
		return o.Owner == e.G.Active && o.Owner == o.Controller
	}).Tapped = false
	if r := capture(e, at, true).Reasons; slices.Contains(r, reasonEntryTap) {
		t.Fatalf("active untapped shock land: reasons %v, want no %s", r, reasonEntryTap)
	}

	// A non-active seat's TAPPED shock land is expressible: the placement's
	// entry tap is the state the cut already has.
	e, at = expressibleCut(t, reg, pairs[0], 100)
	victim = plantBattlefieldCard(t, reg, e, "Breeding Pool", func(o *state.Object) bool {
		return o.Owner != e.G.Active && o.Owner == o.Controller
	})
	victim.Tapped, victim.Counters = true, nil
	if r := capture(e, at, true).Reasons; slices.Contains(r, reasonEntryTap) {
		t.Fatalf("non-active tapped shock land: reasons %v, want no %s", r, reasonEntryTap)
	}
}

// TestWindowSetupKeepsTappedPermanents: a cut with a tapped permanent on each
// seat lowers to a setup the runner reproduces. The active seat's tap is a
// tapped_after_untap (turn 1's untap step would otherwise clear it); the
// non-active seat keeps its tap through placement. The cut also carries a
// spent stun counter, which the live object keeps as a zero entry and the
// runner's fresh placement does not: a zero counter is no counter on either
// side.
func TestWindowSetupKeepsTappedPermanents(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	active := int(e.G.Active)
	plain := func(o *state.Object) bool {
		f := o.Face()
		return f != nil && len(f.Triggers) == 0 && o.AttachedTo == 0 && len(o.Counters) == 0 &&
			!o.IsToken && o.FaceIdx == 0 && !entryTapReplacement(f)
	}
	var activePerm, otherPerm *state.Object
	for _, o := range seatBattlefield(e.G, active) {
		o.Tapped = false
		if activePerm == nil && plain(o) {
			activePerm = o
		}
	}
	for _, o := range seatBattlefield(e.G, 1-active) {
		o.Tapped = false
		if otherPerm == nil && plain(o) {
			otherPerm = o
		}
	}
	if activePerm == nil || otherPerm == nil {
		t.Fatalf("need a plain permanent on each seat (active %v, other %v)", activePerm != nil, otherPerm != nil)
	}
	activePerm.Tapped = true
	otherPerm.Tapped = true
	// The stun counter the untap step spent: the engine leaves the zero entry
	// on the live object, which the runner's fresh placement never has.
	activePerm.Counters = append(activePerm.Counters, state.Counter{Kind: "STUN", N: 0})

	c := capture(e, at, true)
	if len(c.Reasons) != 0 {
		t.Fatalf("the planted cut is refused: %v", c.Reasons)
	}
	activeRef := refOf(seatBattlefield(e.G, active), activePerm)
	otherRef := refOf(seatBattlefield(e.G, 1-active), otherPerm)
	if got := c.Setup[active].TappedAfterUntap; !slices.Contains(got, activeRef) {
		t.Errorf("active tapped_after_untap = %v, want %q", got, activeRef)
	}
	if got := append(append([]string{}, c.Setup[1-active].Tapped...), c.Setup[1-active].TappedAfterUntap...); !slices.Contains(got, otherRef) {
		t.Errorf("non-active tapped fields = %v, want %q", got, otherRef)
	}

	m := seatMap{swap: c.Active == 1}
	l := Lowered{Item: c.item(m, "xe:test:tapped", "tapped"), StepEntry: []int{-1}, Swap: m.swap}
	l.Item.Steps = []oraclegen.Step{{Op: "pass", Seat: 0}}
	wt := &game.Tape{Entries: []game.Entry{{Digest: game.DigestOfGame(e)}}}
	wt.Entries[0].Digest.Turn = 1
	if chk := SelfCheck(reg, l, wt); chk.Status != StatusOK {
		t.Fatalf("setup checkpoint differs from the planted state: %s: gorge %s, tape %s %v", chk.Field, chk.Gorge, chk.Tape, chk.Fails)
	}
}
