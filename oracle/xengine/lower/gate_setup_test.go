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

// TestPlaceKeepsExiledObject pins the two halves of the setup exile list. A
// plain card in exile is named there (names does not drop it), so a window
// holding one is not a lower_gap. A battlefield card whose own entry
// replacement grants Prepared is refused: the runner's placement re-runs the
// replacement and mints the CR 722.3c exile copy, an extra exile object no
// setup field can remove. Both the keyword form (K:ETBReplacement, Skycoach
// Conductor) and the printed Moved replacement (Emeritus of Woe) go through
// the same detector, so a new spelling of the same body cannot slip past.
func TestPlaceKeepsExiledObject(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)

	// A card exiled through the real zone lists stays in the seat's setup
	// exile: capture names every exiled card, not just the ones a link keeps.
	e, at := expressibleCut(t, reg, pairs[0], 100)
	o := libraryObjs(t, e, 1)[0]
	owner, name := o.Owner, frontName(o)
	if name == "" {
		t.Fatal("the library card has no front name")
	}
	e.G.SetZone(state.ZLibrary, owner, slices.DeleteFunc(slices.Clone(e.G.Zone(state.ZLibrary, owner)), func(id state.ObjID) bool {
		return id == o.ID
	}))
	e.G.SetZone(state.ZExile, owner, append(slices.Clone(e.G.Zone(state.ZExile, owner)), o.ID))
	o.Zone = state.ZExile
	c := capture(e, at, true)
	if len(c.Reasons) != 0 {
		t.Fatalf("a plain exiled card is refused: %v", c.Reasons)
	}
	if !slices.Contains(c.Setup[owner].Exile, name) {
		t.Fatalf("setup exile for seat %d = %v, want %q", owner, c.Setup[owner].Exile, name)
	}

	// An entry-prepared permanent is refused, whichever card form carries the
	// replacement: the keyword expansion or a printed R:Event$ Moved.
	for _, card := range []string{"Skycoach Conductor", "Emeritus of Woe", "The Death of Gwen Stacy"} {
		prep, ok := reg.Lookup(card)
		if !ok {
			t.Fatalf("%s is not in the corpus", card)
		}
		e, at := expressibleCut(t, reg, pairs[0], 100)
		var planted bool
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Zone == state.ZBattlefield && o.Ability == nil && !o.IsToken && o.Card != nil && o.AttachedTo == 0 {
				o.SetCard(prep, 0)
				planted = true
				break
			}
		}
		if !planted {
			t.Fatalf("%s: no battlefield permanent to plant", card)
		}
		// A Saga's placement re-fires its chapters (reasonSagaEntry).
		want := reasonPrepareEntry
		if card == "The Death of Gwen Stacy" {
			want = reasonSagaEntry
		}
		if r := capture(e, at, true).Reasons; !slices.Contains(r, want) {
			t.Fatalf("%s: reasons %v, want %s", card, r, want)
		}
	}
}

// TestPlaceCarriesPT: Patched Plaything (4/3, K:etbCounter two -1/-1 counters
// only if cast from hand). Cast from hand it carries the two counters (2/1),
// but the setup placement is not a cast, so the re-run entry grants none and
// setup adds none (net zero): the placed permanent would be 4/3. A gated
// etbCounter is refused whatever the carried counters, including none (a
// permanent that did not come from a hand cast).
func TestPlaceCarriesPT(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	for _, tc := range []struct {
		name     string
		counters []state.Counter
		want     string
	}{
		{"cast from hand", []state.Counter{{Kind: "M1M1", N: 2}}, reasonCountersEntry},
		{"not cast from hand", nil, reasonCountersEntry},
	} {
		e, at := expressibleCut(t, reg, pairs[0], 100)
		o := plantBattlefieldCard(t, reg, e, "Patched Plaything", func(o *state.Object) bool { return !o.Tapped && len(o.Counters) == 0 })
		o.Counters = tc.counters
		if r := capture(e, at, true).Reasons; !slices.Contains(r, tc.want) {
			t.Errorf("%s: reasons %v, want %s", tc.name, r, tc.want)
		}
	}
}

// TestPlaceRefusesEntryCounterOther: Deanna Troi's "other creatures you control
// enter with an additional +1/+1 counter" is re-run by the placement on every
// permanent placed after her, so a carried +1/+1 counter would be doubled
// (live 3/3, placed 4/4). The gate refuses her presence, whatever else is
// placed. A plain permanent and a non-counter entry replacement stay
// placeable, so the detector is not over-broad.
func TestPlaceRefusesEntryCounterOther(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	plantBattlefieldCard(t, reg, e, "Deanna Troi, Psychic Counsel", func(o *state.Object) bool { return !o.Tapped })
	if r := capture(e, at, true).Reasons; !slices.Contains(r, reasonEntryCounterOther) {
		t.Fatalf("reasons %v, want %s", r, reasonEntryCounterOther)
	}

	// A vanilla permanent carries no such replacement.
	e, at = expressibleCut(t, reg, pairs[0], 100)
	plantBattlefieldCard(t, reg, e, "Grizzly Bears", func(*state.Object) bool { return true })
	if r := capture(e, at, true).Reasons; slices.Contains(r, reasonEntryCounterOther) {
		t.Fatalf("plain permanent: reasons %v, want no %s", r, reasonEntryCounterOther)
	}

	// An entry replacement that puts no counters (Karn's Sylex enters tapped)
	// must not trip the counter detector.
	e, at = expressibleCut(t, reg, pairs[0], 100)
	victim := plantBattlefieldCard(t, reg, e, "Karn's Sylex", func(o *state.Object) bool {
		return o.Owner == e.G.Active && o.Owner == o.Controller
	})
	victim.Tapped = false
	if r := capture(e, at, true).Reasons; slices.Contains(r, reasonEntryCounterOther) {
		t.Fatalf("non-counter entry replacement: reasons %v, want no %s", r, reasonEntryCounterOther)
	}
}

// TestPlaceRefusesEntryCounterSelf: Dust Animus's K:ETBReplacement body
// (ConditionCheckSVar$ X, five or more untapped lands) is a self-targeted
// entry-counter replacement. The live permanent may have entered with no
// counters while the placement re-runs the body against the checkpoint's
// board and grants two +1/+1 + a lifelink counter (4/5 placed versus 2/3
// live). setupCounters cannot restate a conditional grant, so the gate must
// refuse it with either carried-counter state.
func TestPlaceRefusesEntryCounterSelf(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	for _, counters := range []struct {
		name string
		cs   []state.Counter
	}{
		{"entered before the condition held", nil},
		{"entered with the grant", []state.Counter{{Kind: "P1P1", N: 2}, {Kind: "Lifelink", N: 1}}},
	} {
		e, at := expressibleCut(t, reg, pairs[0], 100)
		o := plantBattlefieldCard(t, reg, e, "Dust Animus", func(o *state.Object) bool { return !o.Tapped })
		o.Counters = counters.cs
		if r := capture(e, at, true).Reasons; !slices.Contains(r, reasonCountersEntry) {
			t.Errorf("%s: reasons %v, want %s", counters.name, r, reasonCountersEntry)
		}
	}
}

// TestGateRefusesSetupRegenerationShield: gorge records a regeneration shield
// as the internal "Shield" counter marker, not a counter (CR 701.19a: the
// shield is a replacement effect). The live digest drops the marker
// (game.counterMap), while setupCounters would copy it into the scenario as a
// real counter; a live shield is also state setup cannot restate. The gate
// must refuse the window, and the token path that copies Object.Counters must
// refuse a marker too.
func TestGateRefusesSetupRegenerationShield(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	o := plantBattlefieldCard(t, reg, e, "Mox Tantalite", func(o *state.Object) bool {
		return o.Owner == e.G.Active
	})
	if r := capture(e, at, true).Reasons; len(r) != 0 {
		t.Fatalf("planted cut already refused: %v", r)
	}

	o.Counters = []state.Counter{{Kind: "Shield", N: 1}}
	if net, ok := setupCounters(o); ok || net["Shield"] != 0 {
		t.Fatalf("setupCounters = %v, %v; want refused without the marker", net, ok)
	}
	c := capture(e, at, true)
	if !slices.Contains(c.Reasons, reasonCountersEntry) {
		t.Fatalf("reasons %v, want %s", c.Reasons, reasonCountersEntry)
	}
	for s := range c.Setup {
		for name, cs := range c.Setup[s].Counters {
			if n := cs["Shield"]; n != 0 {
				t.Errorf("setup counter %s Shield = %d, want no marker counter", name, n)
			}
		}
		for _, tk := range c.Setup[s].Tokens {
			if n := tk.Counters["Shield"]; n != 0 {
				t.Errorf("setup token counter Shield = %d, want no marker counter", n)
			}
		}
	}

	// A zero amount is not state (a spent marker leaves the entry behind): it
	// must neither refuse the window nor be placed.
	o.Counters = []state.Counter{{Kind: "Shield", N: 0}}
	if net, ok := setupCounters(o); !ok || net["Shield"] != 0 {
		t.Fatalf("zero-amount marker: setupCounters = %v, %v; want placed with no marker", net, ok)
	}
	if c := capture(e, at, true); len(c.Reasons) != 0 {
		t.Fatalf("zero-amount marker refused: %v", c.Reasons)
	}

	// The token counter path is the sibling that also copies Object.Counters
	// into setup; a marker on a token must take the same refusal.
	tokCard := &cards.Card{}
	g := &state.Game{Tokens: map[string]*cards.Card{"zz_tok": tokCard}}
	w := &walker{g: g, why: map[string]bool{}, xmage: false}
	w.checkToken(&state.Object{
		Card: tokCard, Zone: state.ZBattlefield, IsToken: true,
		Counters: []state.Counter{{Kind: "Shield", N: 1}},
	})
	if !w.why[reasonCountersEntry] {
		t.Fatalf("token with a regeneration marker: reasons %v, want %s", sortedKeys(w.why), reasonCountersEntry)
	}
}

// TestPlaceRefusesPTTurnHistory: Duelist of the Mind's power is the cards its
// controller drew this turn. The live game at a window start has drawn the
// turn's card (1/3), the placed one has drawn none (0/3), so the gate refuses
// the card. A static P/T that reads no turn history stays placeable.
func TestPlaceRefusesPTTurnHistory(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	plantBattlefieldCard(t, reg, e, "Duelist of the Mind", func(o *state.Object) bool { return !o.Tapped })
	if r := capture(e, at, true).Reasons; !slices.Contains(r, reasonPTTurnHistory) {
		t.Fatalf("reasons %v, want %s", r, reasonPTTurnHistory)
	}

	// A CheckSVar gate on the P/T static is the same history: Ulna Alley
	// Shopkeep gets +2/+0 only while its controller gained life this turn.
	e, at = expressibleCut(t, reg, pairs[0], 100)
	plantBattlefieldCard(t, reg, e, "Ulna Alley Shopkeep", func(o *state.Object) bool { return !o.Tapped })
	if r := capture(e, at, true).Reasons; !slices.Contains(r, reasonPTTurnHistory) {
		t.Fatalf("gated static: reasons %v, want %s", r, reasonPTTurnHistory)
	}

	// Rampant Frogantua counts players who have lost the GAME, not this
	// turn's history: PlayerCountHasLost must not trip the marker.
	e, at = expressibleCut(t, reg, pairs[0], 100)
	plantBattlefieldCard(t, reg, e, "Rampant Frogantua", func(o *state.Object) bool { return !o.Tapped })
	if r := capture(e, at, true).Reasons; slices.Contains(r, reasonPTTurnHistory) {
		t.Fatalf("game-long lost count: reasons %v, want no %s", r, reasonPTTurnHistory)
	}

	// Glorious Anthem is a printed static pump with no Count$ history.
	e, at = expressibleCut(t, reg, pairs[0], 100)
	plantBattlefieldCard(t, reg, e, "Glorious Anthem", func(*state.Object) bool { return true })
	if r := capture(e, at, true).Reasons; slices.Contains(r, reasonPTTurnHistory) {
		t.Fatalf("printed static pump: reasons %v, want no %s", r, reasonPTTurnHistory)
	}
}

// TestGateRefusesSetupCounterReplacement: the counters field is placed as a
// CounterChange, so a placed permanent's AddCounter replacement (Michelangelo,
// Weirdness to 11: "that many plus one") re-runs on it and the permanent gets
// one more counter than the cut (2 carried -> 3 placed, 3/3 -> 4/4). The gate
// must refuse the counter placement, and a counter-carrier with no replacement
// must stay placeable.
func TestGateRefusesSetupCounterReplacement(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	o := plantBattlefieldCard(t, reg, e, "Michelangelo, Weirdness to 11", func(o *state.Object) bool { return !o.Tapped })
	o.Counters = []state.Counter{{Kind: "P1P1", N: 2}}
	if r := capture(e, at, true).Reasons; !slices.Contains(r, reasonSetupCounterRepl) {
		t.Fatalf("reasons %v, want %s", r, reasonSetupCounterRepl)
	}

	e, at = expressibleCut(t, reg, pairs[0], 100)
	plain := plantBattlefieldCard(t, reg, e, "Grizzly Bears", func(*state.Object) bool { return true })
	plain.Counters = []state.Counter{{Kind: "P1P1", N: 2}}
	if r := capture(e, at, true).Reasons; slices.Contains(r, reasonSetupCounterRepl) {
		t.Fatalf("plain counter carrier: reasons %v, want no %s", r, reasonSetupCounterRepl)
	}
}

// TestGateRefusesTokenCounterReplacement: a placed token's counters are
// CounterChange events as well, so a battlefield AddCounter replacement re-runs
// on them (a 2-counter token becomes 3). The refusal must not depend on a card
// carrying counters; a counter-carrying token beside a plain card stays
// placeable.
func TestGateRefusesTokenCounterReplacement(t *testing.T) {
	reg, pairs := setPairs(t, "DSK", 40)
	e, at := tokenCutIn(t, reg, "DSK", pairs)
	battlefieldTokens(e.G)[0].Counters = []state.Counter{{Kind: "P1P1", N: 2}}
	plantBattlefieldCard(t, reg, e, "Michelangelo, Weirdness to 11", func(o *state.Object) bool { return !o.Tapped && len(o.Counters) == 0 })
	if r := capture(e, at, true).Reasons; !slices.Contains(r, reasonSetupCounterRepl) {
		t.Fatalf("token counters + carrier: reasons %v, want %s", r, reasonSetupCounterRepl)
	}

	e, at = tokenCutIn(t, reg, "DSK", pairs)
	battlefieldTokens(e.G)[0].Counters = []state.Counter{{Kind: "P1P1", N: 2}}
	plantBattlefieldCard(t, reg, e, "Grizzly Bears", func(o *state.Object) bool { return !o.Tapped && len(o.Counters) == 0 })
	if r := capture(e, at, true).Reasons; slices.Contains(r, reasonSetupCounterRepl) {
		t.Fatalf("token counters, plain card: reasons %v, want no %s", r, reasonSetupCounterRepl)
	}
}

// TestGateRefusesStaticEffectGrant: a StaticEffect$ rider registers an
// undurationed source-scoped continuous effect (Enduring Innocence returns as
// a non-creature enchantment). The placement starts from the card face and
// would recreate the printed 2/1 creature, so the whole registered-effect
// registry is refused, not just the duration-flagged subset. A printed static
// is not in that registry and stays placeable.
func TestGateRefusesStaticEffectGrant(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	e, at := expressibleCut(t, reg, pairs[0], 100)
	o := plantBattlefieldCard(t, reg, e, "Enduring Innocence", func(o *state.Object) bool { return !o.Tapped })
	e.AddContinuous(state.ContinuousEffect{
		Source: o.ID, Affects: "Card.Self", Controller: o.Controller,
		Layer: state.LType, AddTypes: []string{"Enchantment"}, RemoveCardTypes: true,
	})
	if r := capture(e, at, true).Reasons; !slices.Contains(r, reasonContinuous) {
		t.Fatalf("registered type change: reasons %v, want %s", r, reasonContinuous)
	}

	// The same card without the registered rider is a printed static and
	// stays placeable.
	e, at = expressibleCut(t, reg, pairs[0], 100)
	plantBattlefieldCard(t, reg, e, "Enduring Innocence", func(o *state.Object) bool { return !o.Tapped })
	if r := capture(e, at, true).Reasons; slices.Contains(r, reasonContinuous) {
		t.Fatalf("printed face: reasons %v, want no %s", r, reasonContinuous)
	}
}
