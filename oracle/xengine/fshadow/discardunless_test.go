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

// thirstStage is a two-seat game at seat 0's main phase with thirstHand, the
// mana floating and three Islands to draw.
func thirstStage(t *testing.T) (*rules.Engine, []state.ObjID) {
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
	for _, n := range thirstHand {
		holder.Hand = append(holder.Hand, card(n))
	}
	st := rules.Stage{Turn: 3, Step: state.StepMain1, Players: []rules.StagedPlayer{holder, player}}
	e, ids, err := rules.NewStaged(rules.Config{Seed: 1, Names: []string{"a", "b"}, Tokens: reg.Tokens}, st)
	if err != nil {
		t.Fatal(err)
	}
	return e, ids.Hand[0]
}

// TestDiscardUnlessFollowsForgesPick is the farm cmdr4 repro (BIG-s142
// pod-14201, line 452, p1.hand): Thirst for Knowledge's "discard two cards
// unless you discard an artifact card" is an election and then a modes pick,
// neither with a Forge ask line. Both were answered by default, so gorge
// discarded the first offered artifact whatever Forge did. Forge's discard is
// read from the next snapshot, past the priority line that follows the
// resolution; that snapshot is a gorge game's own state after Forge's discard.
func TestDiscardUnlessFollowsForgesPick(t *testing.T) {
	var lines []Entry
	for i, line := range []string{
		`"seat":0,"kind":"priority","answer":{"action":"cast","card":1}`,
		`"seat":0,"kind":"priority","answer":{"action":"pass"}`,
		`"seat":1,"kind":"priority","answer":{"action":"pass"}`,
		// Thirst resolves here, drawing three and asking the discard.
		`"seat":0,"kind":"priority","answer":{"action":"pass"}`,
	} {
		lines = append(lines, Entry{Line: i + 2, T: TAsk, Ask: parseAsk(t, fmt.Sprintf(`{"t":"ask","n":%d,"turn":3,"step":"main1",%s}`, i+1, line))})
	}
	for _, c := range []struct {
		name    string
		discard []string // the cards Forge discards
	}{
		{"the second artifact", []string{"Bonesplitter"}},
		{"the first artifact", []string{"Ornithopter"}},
		{"two cards instead", []string{"Grizzly Bears", "Island"}},
		{"two cards, one an artifact", []string{"Bonesplitter", "Grizzly Bears"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Forge's state after the discard, played on a gorge game by
			// answering each ask with Forge's choice.
			forge, ids := thirstStage(t)
			submit := func(choices ...int) {
				t.Helper()
				d := forge.Pending()
				if err := forge.SubmitHypothetical(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
					t.Fatalf("%s %s: %v", d.Kind, d.ResumeKind, err)
				}
				if err := forge.AdvanceHypothetical(); err != nil {
					t.Fatal(err)
				}
			}
			option := func(match func(decision.Option) bool) int {
				t.Helper()
				d := forge.Pending()
				i := slices.IndexFunc(d.Options, match)
				if i < 0 {
					t.Fatalf("%s %s offers %+v", d.Kind, d.ResumeKind, d.Options)
				}
				return i
			}
			if err := forge.AdvanceHypothetical(); err != nil {
				t.Fatal(err)
			}
			submit(option(func(o decision.Option) bool { return o.Kind == "cast" && o.Obj == ids[0] }))
			for range 2 {
				submit(option(func(o decision.Option) bool { return o.Kind == "pass" }))
			}
			election := "ordinary"
			if len(c.discard) == 1 {
				election = "unless"
			}
			submit(option(func(o decision.Option) bool { return o.Kind == election }))
			var picks []int
			for _, name := range c.discard {
				picks = append(picks, option(func(o decision.Option) bool {
					return objName(forge.G.Obj(o.Obj)) == name && !slices.Contains(picks, o.Index)
				}))
			}
			if d := forge.Pending(); d.Kind != decision.KModes {
				t.Fatalf("the card pick is %s, want modes", d.Kind)
			}
			submit(picks...)
			submit(option(func(o decision.Option) bool { return o.Kind == "pass" }))
			snap := Snapshot(forge, "after-discard")

			e, ids := thirstStage(t)
			entries := append(slices.Clone(lines), Entry{Line: 452, T: TSnap, Snap: &Snap{T: TSnap, Snapshot: snap}})
			f := &follower{log: &Log{Entries: entries}, e: e, toG: map[int]state.ObjID{1: ids[0]},
				toF: map[state.ObjID]int{ids[0]: 1}, name: map[int]string{1: thirstHand[0]}}
			f.run(20)
			if h := f.res.Hard; h != nil {
				t.Fatalf("hard divergence at line %d: %s %s gorge %q forge %q (%s)", h.Line, h.What, h.Field, h.Gorge, h.Forge, h.Detail)
			}
			if f.res.End != "log" || f.res.Snaps != 1 || f.res.HandPicks != 1 || f.res.Modes != 0 || f.res.DiscardElections != 1 {
				t.Errorf("result %+v, want the snapshot compared, one election and one hand pick read, and no defaulted modes", f.res)
			}
		})
	}
}

// TestDiscardUnlessElectionNeedsEvidence holds each discard election to the
// next snapshot only where that is evidence: the cards gone from the seat's
// hand there. Without it the election keeps the default (the first option).
func TestDiscardUnlessElectionNeedsEvidence(t *testing.T) {
	cast := func(seat, card int, action string) Entry {
		return Entry{Line: 998, T: TAsk, Ask: parseAsk(t, fmt.Sprintf(`{"t":"ask","n":1,"seat":%d,"turn":3,"step":"upkeep","kind":"priority","answer":{"action":%q,"card":%d}}`, seat, action, card))}
	}
	const fid = 900
	for _, c := range []struct {
		name   string
		resume string
		gone   []int // hand indexes gone at the snapshot
		play   int   // the seat that plays hand card 0 before the snapshot, or -1
		noSnap bool
		want   string // the answered option's kind
		read   int    // Result.DiscardElections
	}{
		{"one gone is the unless card", "discard_unless", []int{0}, -1, false, "unless", 1},
		{"two gone is the ordinary discard", "discard_unless", []int{0, 1}, -1, false, "ordinary", 1},
		{"nothing gone keeps the default", "discard_unless", nil, -1, false, "unless", 0},
		{"a land played on the way is no second discard", "discard_unless", []int{0, 1}, 1, false, "unless", 0},
		{"another seat's play of the id", "discard_unless", []int{0, 1}, 0, false, "ordinary", 1},
		{"no snapshot keeps the default", "discard_unless", []int{0, 1}, -1, true, "unless", 0},
		{"may: nothing gone declines", "discard_may", nil, -1, false, "no", 1},
		{"may: a card gone accepts", "discard_may", []int{0}, -1, false, "yes", 1},
		{"may: no snapshot accepts", "discard_may", nil, -1, true, "yes", 0},
		{"hand: kept declines", "discard_hand", nil, -1, false, "no", 1},
		{"hand: gone accepts", "discard_hand", []int{0, 1, 2}, -1, false, "yes", 1},
		{"another election is left alone", "mill_optional", nil, -1, false, "yes", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, hand := handPickFixture(t, c.gone...)
			if c.noSnap {
				f.log.Entries = f.log.Entries[:f.pos]
			}
			if c.play >= 0 {
				f.toF[hand[0]] = fid
				f.log.Entries = slices.Insert(f.log.Entries, f.pos, cast(c.play, fid, "land"))
			}
			kinds := []string{"yes", "no"}
			if c.resume == "discard_unless" {
				kinds = []string{"unless", "ordinary"}
			}
			d := &decision.Decision{Seq: 11, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1, ResumeKind: c.resume}
			for i, k := range kinds {
				d.Options = append(d.Options, decision.Option{Index: i, Kind: k, Player: 1})
			}
			var e Entry
			if f.pos < len(f.log.Entries) {
				e = f.log.Entries[f.pos]
			}
			pos := f.pos
			in, ok := f.answer(d, e)
			if !ok || f.res.Hard != nil {
				t.Fatalf("hard divergence %+v", f.res.Hard)
			}
			if err := d.Validate(in); err != nil {
				t.Fatalf("intent %+v: %v", in, err)
			}
			if got := d.Options[in.Choices[0]].Kind; got != c.want || f.res.DiscardElections != c.read || f.res.Chooses != 1 || f.pos != pos {
				t.Errorf("answered %q (read %d, chooses %d, pos %d), want %q (read %d)", got, f.res.DiscardElections, f.res.Chooses, f.pos, c.want, c.read)
			}
		})
	}
}

// TestDiscardUnlessModesCardPicks: a modes ask of "mode" options is a card
// pick only where the options name cards other than the ask's source (a "play"
// effect's). One whose options all name the source (a charm, an unless-pay)
// picks no card, so a source that has left the hand by the next snapshot
// answers nothing.
func TestDiscardUnlessModesCardPicks(t *testing.T) {
	for _, c := range []struct {
		name         string
		source       bool // the options name the ask's source, hand card 0
		modes, picks int
	}{
		{"options naming the source stay defaulted", true, 1, 0},
		{"options naming hand cards are read", false, 0, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			// The last hand card unlike the first leaves the hand.
			probe, hand := handPickFixture(t)
			gone := slices.IndexFunc(hand, func(id state.ObjID) bool {
				return objName(probe.e.G.Obj(id)) != objName(probe.e.G.Obj(hand[0]))
			})
			if gone < 0 {
				t.Fatalf("seat 1's hand %v holds one name", hand)
			}
			if c.source {
				gone = 0
			}
			f, hand := handPickFixture(t, gone)
			d := &decision.Decision{Seq: 11, Player: 1, Kind: decision.KModes, Min: 1, Max: 1}
			objs := []state.ObjID{hand[0], hand[gone]}
			if c.source {
				d.Source, objs = hand[0], []state.ObjID{hand[0], hand[0]}
			}
			for i, id := range objs {
				d.Options = append(d.Options, decision.Option{Index: i, Kind: "mode", Obj: id, Player: 1})
			}
			in, ok := f.answer(d, f.log.Entries[f.pos])
			if !ok || f.res.Hard != nil {
				t.Fatalf("hard divergence %+v", f.res.Hard)
			}
			want := []int{0}
			if !c.source {
				want = []int{1}
			}
			if !slices.Equal(in.Choices, want) || f.res.Modes != c.modes || f.res.HandPicks != c.picks {
				t.Errorf("choices %v modes %d hand picks %d, want %v, %d and %d", in.Choices, f.res.Modes, f.res.HandPicks, want, c.modes, c.picks)
			}
		})
	}
}
