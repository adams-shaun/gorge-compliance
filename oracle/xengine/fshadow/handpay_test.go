package fshadow

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// handPaidGame scripts a 2-seat commander game in which seat 0, on its second
// turn, makes one play paid with mana it floats itself.
type handPaidGame struct {
	// deck is each seat's cards, top first; the last is its commander.
	deck []string
	// lands are seat 0's land drops, by turn.
	lands []string
	// action ("cast" or "activate") of the card named play is the paid play.
	action, play string
	// sources are tapped for it in this order; colour is what a source that
	// asks is told to make.
	sources []string
	colour  string
}

// write plays g in gorge and writes the log as Forge's recorder writes an AI
// seat's: the paid play is one priority line carrying the tapped sources, with
// no line for any mana activation or colour pick.
func (g handPaidGame) write(t *testing.T, reg *cards.Registry) *Log {
	t.Helper()
	h := Header{T: THeader, Format: Format, Seed: 7, ForgeRef: "handpay", GameFormat: "commander", Mulligans: 7, MaxTurns: 60, MaxMS: 600000}
	toF := map[state.ObjID]int{}
	cfg := rules.Config{Seed: h.Seed, Tokens: reg.Tokens, NamedCorpus: reg.Universe(), Mulligans: h.Mulligans, Format: rules.FormatCommander}
	next := state.ObjID(1)
	for s := 0; s < 2; s++ {
		commander := len(g.deck) - 1
		seat := Seat{Seat: s, Name: fmt.Sprintf("handpay-%d", s), Commanders: []int{commander}}
		var deck []*cards.Card
		for k, n := range g.deck {
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
		cfg.Commanders = append(cfg.Commanders, []int{commander})
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
	find := func(z state.Zone, name string) state.ObjID {
		for _, id := range e.G.Zone(z, 0) {
			if objName(e.G.Obj(id)) == name {
				return id
			}
		}
		return 0
	}
	submit := func(in decision.Intent) {
		t.Helper()
		d := e.Pending()
		if err := e.SubmitHypothetical(in); err != nil {
			t.Fatalf("%s %+v: %v", d.Kind, in, err)
		}
		if err := e.AdvanceHypothetical(); err != nil {
			t.Fatal(err)
		}
	}
	asks, snaps, paid, landTurn := 0, 0, false, int32(0)
	lastTurn, lastStep := int32(-1), ""
	for n := 0; n < 400; n++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			t.Fatalf("decision %d: the game ended before the paid play resolved", n)
		}
		if e.G.Turn >= 1 && len(e.G.Stack) == 0 && (e.G.Turn != lastTurn || e.G.Step.String() != lastStep) {
			lastTurn, lastStep = e.G.Turn, e.G.Step.String()
			snaps++
			s := Snapshot(e, fmt.Sprintf("t%d:%s", e.G.Turn, lastStep))
			add(Entry{T: TSnap, Snap: &Snap{T: TSnap, N: snaps, Snapshot: s}})
			if paid {
				return l // the snapshot after the resolution closes the log
			}
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}
		switch d.Kind {
		case decision.KMulligan:
			in.Choices = []int{indexOf(d, "keep")}
			add(Entry{T: TMull, Mull: &Mull{T: TMull, Seat: int(d.Player), Keep: true}})
		case decision.KPriority:
			p := d.Player
			ans := Answer{Action: "pass"}
			in.Choices = []int{indexOf(d, "pass")}
			// Seat 0's turns are the odd ones: turn 1 and turn 3.
			mine := p == 0 && e.G.Active == 0 && len(e.G.Stack) == 0 && e.G.Step.String() == "main1"
			drop := int(e.G.Turn) / 2
			land := -1
			for i, o := range d.Options {
				if o.Kind != "play_land" || land >= 0 || landTurn == e.G.Turn {
					continue
				}
				if p != 0 || mine && drop < len(g.lands) && objName(e.G.Obj(o.Obj)) == g.lands[drop] {
					land = i
				}
			}
			ring := find(state.ZHand, "Sol Ring")
			playIn := func() state.ObjID {
				for _, z := range []state.Zone{state.ZCommand, state.ZHand} {
					if id := find(z, g.play); id != 0 {
						return id
					}
				}
				return 0
			}
			switch {
			case land >= 0:
				landTurn = e.G.Turn
				in.Choices = []int{land}
				ans = Answer{Action: "land", Card: toF[d.Options[land].Obj]}
			case mine && ring != 0:
				for _, a := range e.EnsurePaymentPlanAlternatives() {
					if a.Cast.Object != ring || len(a.Plans) == 0 {
						continue
					}
					plan := a.Plans[0]
					in.Choices = nil
					in.Payment = &decision.PaymentSelection{ActionID: a.ID, Plan: decision.ClonePaymentPlan(plan)}
					ans = Answer{Action: "cast", Card: toF[ring]}
					for _, x := range plan.Activations {
						ans.Tapped = append(ans.Tapped, toF[x.Source])
					}
				}
			case mine && !paid && e.G.Turn == 3 && landTurn == e.G.Turn:
				obj := playIn()
				kind := map[string]string{"cast": "cast", "activate": "ability"}[g.action]
				offers := func(d *decision.Decision) int {
					return slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Kind == kind && o.Obj == obj })
				}
				if offers(d) >= 0 {
					t.Fatalf("gorge offers %s before any mana is floating: the game does not exercise the hand payment", g.play)
				}
				ans = Answer{Action: g.action, Card: toF[obj]}
				asks++
				add(Entry{T: TAsk, Ask: &Ask{T: TAsk, N: asks, Seat: 0, Turn: int(e.G.Turn), Step: e.G.Step.String(), Kind: KindPriority, Answer: ans}})
				line := l.Entries[len(l.Entries)-1].Ask
				for _, name := range g.sources {
					src := find(state.ZBattlefield, name)
					line.Answer.Tapped = append(line.Answer.Tapped, toF[src])
					d := e.Pending()
					i := slices.IndexFunc(d.Options, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == src })
					if i < 0 {
						t.Fatalf("no activation of %s offered: %+v", name, d.Options)
					}
					submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}})
					for d = e.Pending(); d.Kind != decision.KPriority; d = e.Pending() {
						i := slices.IndexFunc(d.Options, func(o decision.Option) bool {
							return o.ManaSymbol == g.colour || strings.HasSuffix(o.Label, g.colour)
						})
						if i <= 0 {
							t.Fatalf("%s's ask: want %s offered, and not first (the default must be wrong): %+v", name, g.colour, d.Options)
						}
						submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}})
					}
				}
				d = e.Pending()
				i := offers(d)
				if i < 0 {
					t.Fatalf("gorge does not offer %s from the floating mana: %+v", g.play, d.Options)
				}
				submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}})
				paid = true
				continue
			}
			asks++
			add(Entry{T: TAsk, Ask: &Ask{T: TAsk, N: asks, Seat: int(d.Acting()), Turn: int(e.G.Turn), Step: e.G.Step.String(), Kind: KindPriority, Answer: ans}})
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
			t.Fatalf("the game poses %s", d.Kind)
		}
		submit(in)
	}
	t.Fatalf("seat 0 never made the paid play")
	return nil
}

// commanderGame is the farm cmdr4 BIG-s142 pod-14201 board (line 417): Sol
// Ring, a filter land and a two-colour source cast the commander from the
// command zone. The filter's {1} is payable only from mana another source
// floats first, so gorge plans no payment and offers no cast.
var commanderGame = handPaidGame{
	deck:    []string{"Command Tower", "Sol Ring", "Skycloud Expanse", "Island", "Island", "Island", "Island", "Island", "Island", "Island", "Island", "Shiko and Narset, Unified"},
	lands:   []string{"Command Tower", "Skycloud Expanse"},
	action:  "cast",
	play:    "Shiko and Narset, Unified",
	sources: []string{"Sol Ring", "Skycloud Expanse", "Command Tower"},
	colour:  "R",
}

func wantHandPaid(t *testing.T, l *Log) {
	t.Helper()
	res := follow(t, l)
	if h := res.Hard; h != nil {
		t.Fatalf("hard divergence at line %d: %s %s %s gorge %q forge %q (%s)", h.Line, h.What, h.Kind, h.Field, h.Gorge, h.Forge, h.Detail)
	}
	if res.HandPays != 1 || res.End != "log" {
		t.Errorf("%d hand payments, end %q; want the one paid play and the whole log followed", res.HandPays, res.End)
	}
}

// TestCommanderCastPaidThroughAFilterLand follows that cast whole: the
// follower activates Forge's tapped sources (backing out of the colours that
// leave the cast unpayable) and then casts the commander. The closing snapshot
// holds it to the same board.
func TestCommanderCastPaidThroughAFilterLand(t *testing.T) {
	wantHandPaid(t, commanderGame.write(t, corpus(t)))
}

// TestCommanderCastShortOfSourcesStaysAnswerMissing: with one of Forge's
// sources gone from the line gorge cannot make the mana, so the follower
// reports the cast as before and leaves its engine at the posed decision.
func TestCommanderCastShortOfSourcesStaysAnswerMissing(t *testing.T) {
	l := commanderGame.write(t, corpus(t))
	line := 0
	for _, e := range l.Entries {
		if a := e.Ask; a != nil && a.Answer.Action == "cast" && len(a.Answer.Tapped) == 3 {
			a.Answer.Tapped, line = a.Answer.Tapped[:2], e.Line
		}
	}
	res := follow(t, l)
	h := res.Hard
	if h == nil || h.What != WhatAnswerMissing || h.Kind != "priority" || h.Line != line || res.HandPays != 0 {
		t.Fatalf("divergence %+v (%d hand payments), want answer-missing priority at line %d", h, res.HandPays, line)
	}
}

// TestHandPaidActivation is the same class for an activation: gorge offers
// cycling only from mana already floating.
func TestHandPaidActivation(t *testing.T) {
	g := handPaidGame{
		deck:    []string{"Command Tower", "Sol Ring", "Skycloud Expanse", "Perilous Landscape", "Island", "Island", "Island", "Island", "Island", "Island", "Island", "Shiko and Narset, Unified"},
		lands:   []string{"Command Tower", "Skycloud Expanse"},
		action:  "activate",
		play:    "Perilous Landscape",
		sources: []string{"Sol Ring", "Skycloud Expanse", "Command Tower"},
		colour:  "R",
	}
	wantHandPaid(t, g.write(t, corpus(t)))
}
