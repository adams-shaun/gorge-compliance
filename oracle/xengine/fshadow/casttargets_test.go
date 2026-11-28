package fshadow

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// abradeDeck is each seat's library, top first: the opening hand holds two
// Mountains, a Sol Ring and an Abrade.
var abradeDeck = []string{"Mountain", "Sol Ring", "Mountain", "Abrade", "Mountain", "Mountain", "Mountain", "Mountain", "Mountain", "Mountain", "Mountain", "Mountain"}

// writeAbradeGame plays a scripted 2-seat game in gorge and writes the log as
// Forge's recorder writes an AI seat's: each seat casts its Sol Ring, then
// seat 0 casts Abrade on SEAT 1's Sol Ring. The targets ride the cast's own
// priority line (targets) and neither the modes nor the target get an ask line
// of their own.
func writeAbradeGame(t *testing.T, reg *cards.Registry) *Log {
	t.Helper()
	h := Header{T: THeader, Format: Format, Seed: 7, ForgeRef: "abrade", GameFormat: "constructed", Mulligans: 7, MaxTurns: 60, MaxMS: 600000}
	toF := map[state.ObjID]int{}
	cfg := rules.Config{Seed: h.Seed, Tokens: reg.Tokens, NamedCorpus: reg.Universe(), Mulligans: h.Mulligans}
	next := state.ObjID(1)
	for s := 0; s < 2; s++ {
		seat := Seat{Seat: s, Name: fmt.Sprintf("abrade-%d", s)}
		var deck []*cards.Card
		for k, n := range abradeDeck {
			c, ok := reg.Lookup(n)
			if !ok {
				t.Fatalf("%q not in the corpus", n)
			}
			id := 100*(s+1) + k
			seat.Cards = append(seat.Cards, Card{ID: id, Name: n})
			toF[next] = id
			next++
			deck = append(deck, c)
		}
		h.Seats = append(h.Seats, seat)
		cfg.Names = append(cfg.Names, seat.Name)
		cfg.Decks = append(cfg.Decks, deck)
	}
	cfg.NameUniverse = decks.NameUniverse(cfg.Decks)
	l := &Log{Header: h}
	add := func(e Entry) {
		e.Line = len(l.Entries) + 2 // the header is line 1
		l.Entries = append(l.Entries, e)
	}
	shuffle := func(c rules.ShuffleContext) ([]state.ObjID, error) {
		out := make([]state.ObjID, len(c.Library))
		order := make([]int, len(c.Library))
		for i, sc := range c.Library {
			out[i], order[i] = sc.ID, toF[sc.ID]
		}
		add(Entry{T: TShuffle, Shuffle: &Shuffle{T: TShuffle, Seat: int(c.Player), Order: order}})
		return out, nil
	}
	toss := func(rules.ChanceContext) (int, error) { return 0, nil }
	e, err := rules.NewHypotheticalChancePlanned(cfg, nil, shuffle, toss)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	add(Entry{T: TStart, Start: &Start{T: TStart, Seat: 0}})
	zoneHas := func(z state.Zone, seat state.PlayerID, name string) state.ObjID {
		for _, id := range e.G.Zone(z, seat) {
			if objName(e.G.Obj(id)) == name {
				return id
			}
		}
		return 0
	}
	// theirRing is the target seat 0's Abrade names.
	var theirRing state.ObjID
	var cast *Ask // the cast line gorge is still announcing
	asks, snaps, abraded := 0, 0, false
	lastTurn, lastStep := int32(-1), ""
	for n := 0; n < 400; n++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			t.Fatalf("decision %d: the game ended before Abrade resolved", n)
		}
		if e.G.Turn >= 1 && len(e.G.Stack) == 0 && (e.G.Turn != lastTurn || e.G.Step.String() != lastStep) {
			lastTurn, lastStep = e.G.Turn, e.G.Step.String()
			snaps++
			s := Snapshot(e, fmt.Sprintf("t%d:%s", e.G.Turn, lastStep))
			add(Entry{T: TSnap, Snap: &Snap{T: TSnap, N: snaps, Snapshot: s}})
			if abraded {
				return l // the snapshot after the resolution closes the log
			}
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}
		switch d.Kind {
		case decision.KMulligan:
			in.Choices = []int{indexOf(d, "keep")}
			add(Entry{T: TMull, Mull: &Mull{T: TMull, Seat: int(d.Player), Keep: true}})
		case decision.KModes:
			// No creature is on the board: the first mode offered, which the
			// follower's default also takes, is Abrade's "destroy target
			// artifact" (the target ask below offers the Sol Rings).
			in.Choices = []int{0}
		case decision.KTarget:
			for i, o := range d.Options {
				if o.Obj == theirRing {
					in.Choices = []int{i}
				}
			}
			if len(in.Choices) != 1 || in.Choices[0] == 0 || cast == nil {
				t.Fatalf("target ask %+v: want seat 1's Sol Ring offered, and not first (the default must be wrong)", d.Options)
			}
			cast.Answer.Targets = []Ref{{Card: toF[theirRing]}}
			cast.Answer.TargetSlots = []int{1}
			abraded = true
		case decision.KPriority:
			cast = nil
			p := d.Player
			ans := Answer{Action: "pass"}
			in.Choices = []int{indexOf(d, "pass")}
			land := -1
			for i, o := range d.Options {
				if o.Kind == "play_land" && land < 0 {
					land = i
				}
			}
			var want state.ObjID
			if e.G.Active == p && len(e.G.Stack) == 0 {
				if ring := zoneHas(state.ZHand, p, "Sol Ring"); ring != 0 {
					want = ring
				} else if p == 0 && zoneHas(state.ZBattlefield, 1, "Sol Ring") != 0 {
					want = zoneHas(state.ZHand, 0, "Abrade")
				}
			}
			if land >= 0 {
				in.Choices = []int{land}
				ans = Answer{Action: "land", Card: toF[d.Options[land].Obj]}
			} else if want != 0 {
				for _, a := range e.EnsurePaymentPlanAlternatives() {
					if a.Cast.Object != want || len(a.Plans) == 0 {
						continue
					}
					plan := a.Plans[0]
					in.Choices = nil
					in.Payment = &decision.PaymentSelection{ActionID: a.ID, Plan: decision.ClonePaymentPlan(plan)}
					ans = Answer{Action: "cast", Card: toF[want]}
					for _, x := range plan.Activations {
						ans.Tapped = append(ans.Tapped, toF[x.Source])
					}
					if objName(e.G.Obj(want)) == "Abrade" {
						theirRing = zoneHas(state.ZBattlefield, 1, "Sol Ring")
					}
				}
			}
			asks++
			a := &Ask{T: TAsk, N: asks, Seat: int(d.Acting()), Turn: int(e.G.Turn), Step: e.G.Step.String(), Kind: KindPriority, Answer: ans}
			add(Entry{T: TAsk, Ask: a})
			if ans.Action == "cast" {
				cast = a
			}
		case decision.KAttackers, decision.KBlockers:
			kind := KindAttack
			if d.Kind == decision.KBlockers {
				kind = KindBlock
			}
			asks++
			add(Entry{T: TAsk, Ask: &Ask{T: TAsk, N: asks, Seat: int(d.Acting()), Turn: int(e.G.Turn), Step: e.G.Step.String(), Kind: kind}})
		case decision.KChoose:
			in = firstMin(d)
		default:
			t.Fatalf("the Abrade game poses %s", d.Kind)
		}
		if err := e.SubmitHypothetical(in); err != nil {
			t.Fatalf("decision %d (%s): %v", n, d.Kind, err)
		}
		if err := e.AdvanceHypothetical(); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("seat 0 never cast Abrade")
	return nil
}

// TestCastTargetsAnswerGorgeTargetAsk follows that game whole (farm cmdr4
// BIG-s142 pod 14201, line 277, class snapshot|priority|p0.graveyard):
// TestTargetFromTheCastAnswer hands the follower the cast's targets itself, so
// only a followed log holds the follower to taking them off the priority line
// it answers. Without that, gorge destroys the caster's own Sol Ring, the
// first target offered ("[Sol Ring, Abrade]" against Forge's "[Abrade]").
func TestCastTargetsAnswerGorgeTargetAsk(t *testing.T) {
	l := writeAbradeGame(t, corpus(t))
	res := follow(t, l)
	if h := res.Hard; h != nil {
		t.Fatalf("hard divergence at line %d: %s %s %s gorge %q forge %q (%s)", h.Line, h.What, h.Kind, h.Field, h.Gorge, h.Forge, h.Detail)
	}
	if res.CastTargets != 1 || res.Targets != 0 {
		t.Errorf("cast %d defaulted %d targets, want the one target taken from the cast line", res.CastTargets, res.Targets)
	}
}
