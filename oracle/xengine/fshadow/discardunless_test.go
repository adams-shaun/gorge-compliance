package fshadow

import (
	"fmt"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// thirstHand is the hand seat 0 casts Thirst for Knowledge from. The two
// artifacts are offered in this order, so a defaulted pick discards the
// Ornithopter.
var thirstHand = []string{"Thirst for Knowledge", "Ornithopter", "Bonesplitter", "Grizzly Bears"}

// thirstStage is a two-seat game at seat 0's main phase with hand (Thirst for
// Knowledge first), the mana for it floating and three Islands to draw.
func thirstStage(t *testing.T, hand []string) (*rules.Engine, []state.ObjID) {
	t.Helper()
	reg := corpus(t)
	card := func(name string) *cards.Card {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%q is not in the corpus", name)
		}
		return c
	}
	island := card("Island")
	player := rules.StagedPlayer{Life: 20, Library: []*cards.Card{island, island, island, island}}
	holder := player
	holder.ManaPool = "UUU"
	for _, n := range hand {
		holder.Hand = append(holder.Hand, card(n))
	}
	st := rules.Stage{Turn: 3, Step: state.StepMain1, Players: []rules.StagedPlayer{holder, player}}
	e, ids, err := rules.NewStaged(rules.Config{Seed: 1, Names: []string{"a", "b"}, Tokens: reg.Tokens}, st)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	return e, ids.Hand[0]
}

// thirstGame plays e by choosing options of its pending decision.
type thirstGame struct {
	t *testing.T
	e *rules.Engine
}

// option is the pending decision's first option match accepts.
func (g thirstGame) option(match func(decision.Option) bool) int {
	g.t.Helper()
	d := g.e.Pending()
	i := slices.IndexFunc(d.Options, match)
	if i < 0 {
		g.t.Fatalf("%s %s offers %+v", d.Kind, d.ResumeKind, d.Options)
	}
	return i
}

func (g thirstGame) submit(choices ...int) {
	g.t.Helper()
	d := g.e.Pending()
	if err := g.e.SubmitHypothetical(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		g.t.Fatalf("%s %s: %v", d.Kind, d.ResumeKind, err)
	}
	if err := g.e.AdvanceHypothetical(); err != nil {
		g.t.Fatal(err)
	}
}

func (g thirstGame) pass() {
	g.t.Helper()
	g.submit(g.option(func(o decision.Option) bool { return o.Kind == "pass" }))
}

// resolveThirst casts thirst and passes until it asks its discard.
func (g thirstGame) resolveThirst(thirst state.ObjID) {
	g.t.Helper()
	g.submit(g.option(func(o decision.Option) bool { return o.Kind == "cast" && o.Obj == thirst }))
	g.pass()
	g.pass()
	if d := g.e.Pending(); d.ResumeKind != "discard_unless" {
		g.t.Fatalf("pending %s %s, want the discard election", d.Kind, d.ResumeKind)
	}
}

// TestDiscardUnlessFollowsForgesPick is the farm cmdr4 repro (BIG-s142
// pod-14201, line 452, p1.hand): Thirst for Knowledge's "discard two cards
// unless you discard an artifact card" is an election and then a modes pick,
// neither with a Forge ask line. Forge's discard is read from the next
// snapshot, past the priority lines that follow the resolution; that snapshot
// is a gorge game's own state after Forge's answers. A card cycled after an
// "unless" discard leaves the hand too and is no second discard: reading it as
// one made gorge discard it, and Forge's cycling then had no gorge option.
func TestDiscardUnlessFollowsForgesPick(t *testing.T) {
	cycling := []string{"Thirst for Knowledge", "Ornithopter", "Street Wraith", "Grizzly Bears"}
	cyclingTwoArtifacts := []string{"Thirst for Knowledge", "Ornithopter", "Street Wraith", "Bonesplitter"}
	for _, c := range []struct {
		name    string
		hand    []string
		discard []string // the cards Forge discards
		cycle   bool     // Forge then cycles hand card 2
		// The result's counters.
		elections, handPicks, modes int
	}{
		{"the second artifact", thirstHand, []string{"Bonesplitter"}, false, 0, 1, 0},
		{"the first artifact", thirstHand, []string{"Ornithopter"}, false, 0, 1, 0},
		{"two cards instead", thirstHand, []string{"Grizzly Bears", "Island"}, false, 1, 1, 0},
		{"a card cycled after the only artifact", cycling, []string{"Ornithopter"}, true, 0, 0, 0},
		// The cycled card is not offered, so the modes pick keeps its
		// default, the first artifact.
		{"a card cycled after the first of two artifacts", cyclingTwoArtifacts, []string{"Ornithopter"}, true, 0, 0, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Forge's side, played on a gorge game by answering each ask
			// with Forge's choice, and the priority lines it logs.
			forge, ids := thirstStage(t, c.hand)
			g := thirstGame{t, forge}
			var entries []Entry
			logged := func(seat int, answer string) {
				n := len(entries) + 1
				entries = append(entries, Entry{Line: n + 1, T: TAsk, Ask: parseAsk(t, fmt.Sprintf(
					`{"t":"ask","n":%d,"turn":3,"step":"main1","seat":%d,"kind":"priority","answer":{%s}}`, n, seat, answer))})
			}
			pass := func() {
				logged(int(forge.Pending().Player), `"action":"pass"`)
				g.pass()
			}
			logged(0, `"action":"cast","card":1`)
			logged(0, `"action":"pass"`)
			logged(1, `"action":"pass"`)
			g.resolveThirst(ids[0])
			// One card discarded is the "unless" discard of an artifact.
			election := "ordinary"
			if len(c.discard) == 1 {
				election = "unless"
			}
			g.submit(g.option(func(o decision.Option) bool { return o.Kind == election }))
			// With one artifact in hand the "unless" discard asks no pick.
			if d := forge.Pending(); d.Kind == decision.KModes {
				var picks []int
				for _, name := range c.discard {
					picks = append(picks, g.option(func(o decision.Option) bool {
						return objName(forge.G.Obj(o.Obj)) == name && !slices.Contains(picks, o.Index)
					}))
				}
				g.submit(picks...)
			}
			if c.cycle {
				logged(0, `"action":"activate","card":2`)
				g.submit(g.option(func(o decision.Option) bool { return o.Obj == ids[2] }))
				pass()
				pass()
			}
			pass()
			snap := Snapshot(forge, "after-discard")

			e, ids := thirstStage(t, c.hand)
			entries = append(entries, Entry{Line: 452, T: TSnap, Snap: &Snap{T: TSnap, Snapshot: snap}})
			f := &follower{log: &Log{Entries: entries}, e: e,
				toG: map[int]state.ObjID{1: ids[0], 2: ids[2]}, toF: map[state.ObjID]int{ids[0]: 1, ids[2]: 2},
				name: map[int]string{1: c.hand[0], 2: c.hand[2]}}
			f.run(30)
			if h := f.res.Hard; h != nil {
				t.Fatalf("hard divergence at line %d: %s %s gorge %q forge %q (%s)", h.Line, h.What, h.Field, h.Gorge, h.Forge, h.Detail)
			}
			if f.res.End != "log" || f.res.Snaps != 1 || f.res.DiscardElections != c.elections || f.res.HandPicks != c.handPicks || f.res.Modes != c.modes {
				t.Errorf("result %+v, want the snapshot compared with %d elections, %d hand picks and %d defaulted modes", f.res, c.elections, c.handPicks, c.modes)
			}
		})
	}
}

// TestDiscardUnlessElectionNeedsEvidence answers the election "ordinary" only
// where the next snapshot rules the one-artifact discard out: two cards left
// the hand and no artifact of it did. Everything else keeps the default,
// "unless".
func TestDiscardUnlessElectionNeedsEvidence(t *testing.T) {
	two := []string{"Grizzly Bears", "Island"}
	for _, c := range []struct {
		name   string
		resume string   // replaces the decision's resume kind
		gone   []string // cards gone from the hand at the snapshot
		yard   []string // cards new in the graveyard there
		exile  []string // cards new in exile there
		noSnap bool
		want   string // the answered option's kind
	}{
		{name: "two other cards gone", gone: two, yard: two, want: "ordinary"},
		{name: "nothing gone", want: "unless"},
		{name: "one other card gone", gone: []string{"Grizzly Bears"}, want: "unless"},
		{name: "one artifact gone", gone: []string{"Ornithopter"}, yard: []string{"Ornithopter"}, want: "unless"},
		{name: "an artifact and another card gone", gone: []string{"Ornithopter", "Grizzly Bears"}, want: "unless"},
		{name: "an artifact in the graveyard though still in hand", gone: two, yard: []string{"Ornithopter"}, want: "unless"},
		{name: "an artifact in exile though still in hand", gone: two, exile: []string{"Bonesplitter"}, want: "unless"},
		{name: "no snapshot", noSnap: true, want: "unless"},
		{name: "another election", resume: "discard_may", gone: two, yard: two, want: "unless"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, ids := thirstStage(t, thirstHand)
			thirstGame{t, e}.resolveThirst(ids[0])
			d := *e.Pending()
			if c.resume != "" {
				d.ResumeKind = c.resume
			}
			snap := Snapshot(e, "")
			p := &snap.Players[0]
			for _, n := range c.gone {
				p.Hand = without(p.Hand, n)
			}
			p.Graveyard = append(slices.Clone(p.Graveyard), c.yard...)
			p.Exile = append(slices.Clone(p.Exile), c.exile...)
			entry := Entry{Line: 999, T: TSnap, Snap: &Snap{T: TSnap, Snapshot: snap}}
			if c.noSnap {
				entry = Entry{Line: 999, T: TEnd, End: &End{}}
			}
			f := &follower{log: &Log{Entries: []Entry{entry}}, e: e}
			in, ok := f.answer(&d, entry)
			if !ok || f.res.Hard != nil {
				t.Fatalf("hard divergence %+v", f.res.Hard)
			}
			read := 0
			if c.want == "ordinary" {
				read = 1
			}
			if got := d.Options[in.Choices[0]].Kind; got != c.want || f.res.DiscardElections != read || f.res.Chooses != 1 || f.pos != 0 {
				t.Errorf("answered %q (read %d, chooses %d, pos %d), want %q (read %d)", got, f.res.DiscardElections, f.res.Chooses, f.pos, c.want, read)
			}
		})
	}
}
