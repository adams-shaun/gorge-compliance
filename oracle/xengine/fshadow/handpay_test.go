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

// handStep is one thing seat 0 does in handGame: play a land, cast a card with
// gorge's first plan, or activate the mana of taps ("Name" or "Name:W", the
// colour chosen) and then cast use, or activate its ability'th ability.
type handStep struct {
	land, plan, use string
	taps            []string
	activate        bool
	ability         int
	target          string // the ability's target
}

// handGame plays steps in a real 2-seat gorge game, seat 0's own main phase,
// and writes the log as Forge's recorder would: the mana abilities of a hand
// payment get no ask line, only the cast's or activation's "tapped".
func handGame(t *testing.T, reg *cards.Registry, commander string, library []string, steps []handStep) *Log {
	t.Helper()
	h := Header{T: THeader, Format: Format, Seed: 7, ForgeRef: "handpay", GameFormat: "constructed", Mulligans: 7, MaxTurns: 60, MaxMS: 600000}
	deck := append([]string{"Grizzly Bears", "Grizzly Bears"}, library...)
	for len(deck) < 24 {
		deck = append(deck, "Grizzly Bears")
	}
	cfg := rules.Config{Seed: h.Seed, Tokens: reg.Tokens, NamedCorpus: reg.Universe(), Mulligans: h.Mulligans}
	if commander != "" {
		h.GameFormat, cfg.Format = "commander", rules.FormatCommander
		deck = append(deck, commander)
	}
	toF := map[state.ObjID]int{}
	next := state.ObjID(1)
	for s := 0; s < 2; s++ {
		seat := Seat{Seat: s, Name: fmt.Sprintf("handpay-%d", s)}
		var cs []*cards.Card
		for k, n := range deck {
			c, ok := reg.Lookup(n)
			if !ok {
				t.Fatalf("%q not in the corpus", n)
			}
			id := 100*(s+1) + k
			seat.Cards = append(seat.Cards, Card{ID: id, Name: n})
			toF[next] = id
			next++
			cs = append(cs, c)
		}
		if commander != "" {
			seat.Commanders = []int{len(deck) - 1}
			cfg.Commanders = append(cfg.Commanders, seat.Commanders)
		}
		h.Seats = append(h.Seats, seat)
		cfg.Names = append(cfg.Names, seat.Name)
		cfg.Decks = append(cfg.Decks, cs)
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
	e, err := rules.NewHypotheticalChancePlanned(cfg, nil, shuffle, func(rules.ChanceContext) (int, error) { return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	add(Entry{T: TStart, Start: &Start{T: TStart, Seat: 0}})
	submit := func(in decision.Intent) {
		t.Helper()
		d := e.Pending()
		in.Seq, in.Player = d.Seq, d.Player
		if err := e.SubmitHypothetical(in); err != nil {
			t.Fatalf("%s: %v", d.Kind, err)
		}
		if err := e.AdvanceHypothetical(); err != nil {
			t.Fatal(err)
		}
	}
	// option is the nth pending option of kind on a card called name, or -1.
	option := func(kind, name string, nth int) int {
		for i, o := range e.Pending().Options {
			if o.Kind == kind && objName(e.G.Obj(o.Obj)) == name {
				if nth == 0 {
					return i
				}
				nth--
			}
		}
		return -1
	}
	untapped := func(name string) state.ObjID {
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if o := e.G.Obj(id); objName(o) == name && !o.Tapped {
				return id
			}
		}
		return 0
	}
	asks, snaps := 0, 0
	ask := func(kind string, ans Answer) *Ask {
		d := e.Pending()
		asks++
		a := &Ask{T: TAsk, N: asks, Seat: int(d.Acting()), Turn: int(e.G.Turn), Step: e.G.Step.String(), Kind: kind, Answer: ans}
		add(Entry{T: TAsk, Ask: a})
		return a
	}
	// try performs s at this priority if the board allows it yet.
	try := func(s handStep) (done bool, targeted *Ask) {
		if s.land != "" {
			i := option("play_land", s.land, 0)
			if i < 0 {
				return false, nil
			}
			ask(KindPriority, Answer{Action: "land", Card: toF[e.Pending().Options[i].Obj]})
			submit(decision.Intent{Choices: []int{i}})
			return true, nil
		}
		if s.plan != "" {
			for _, a := range e.EnsurePaymentPlanAlternatives() {
				if objName(e.G.Obj(a.Cast.Object)) != s.plan {
					continue
				}
				plan := a.Plans[0]
				ans := Answer{Action: "cast", Card: toF[a.Cast.Object]}
				for _, x := range plan.Activations {
					ans.Tapped = append(ans.Tapped, toF[x.Source])
				}
				ask(KindPriority, ans)
				submit(decision.Intent{Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: decision.ClonePaymentPlan(plan)}})
				return true, nil
			}
			return false, nil
		}
		ans := Answer{Action: "cast"}
		for _, tap := range s.taps {
			name, _, _ := strings.Cut(tap, ":")
			id := untapped(name)
			if id == 0 {
				return false, nil
			}
			ans.Tapped = append(ans.Tapped, toF[id])
		}
		// The recorder's line precedes whatever the payment does.
		a := ask(KindPriority, ans)
		for _, tap := range s.taps {
			name, colour, _ := strings.Cut(tap, ":")
			if name == s.use && s.activate {
				continue // tapped as the ability's own cost
			}
			i := option("activate", name, 0)
			if i < 0 {
				t.Fatalf("%s: no mana activation offered: %+v", name, e.Pending().Options)
			}
			submit(decision.Intent{Choices: []int{i}})
			for e.Pending().Kind == decision.KChoose {
				pick := slices.IndexFunc(e.Pending().Options, func(o decision.Option) bool { return o.ManaSymbol == colour })
				if pick < 0 {
					t.Fatalf("%s: colour %q not offered: %+v", name, colour, e.Pending().Options)
				}
				submit(decision.Intent{Choices: []int{pick}})
			}
		}
		kind, nth := "cast", 0
		if s.activate {
			kind, nth = "ability", s.ability
		}
		i := option(kind, s.use, nth)
		if i < 0 {
			t.Fatalf("%s: no %s offered after paying: %+v", s.use, kind, e.Pending().Options)
		}
		a.Answer.Card = toF[e.Pending().Options[i].Obj]
		if s.activate {
			a.Answer.Action, a.Answer.Ability = "activate", 10+s.ability
			for k := 0; option(kind, s.use, k) >= 0; k++ {
				a.Options = append(a.Options, Option{Action: "activate", Card: a.Answer.Card, Ability: 10 + k})
			}
		}
		submit(decision.Intent{Choices: []int{i}})
		if s.target != "" {
			return true, a
		}
		return true, nil
	}
	var announcing *Ask // the line gorge is still taking targets for
	var target string
	step := 0
	lastTurn, lastStep := int32(-1), ""
	for n := 0; n < 600; n++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			t.Fatalf("decision %d: the game ended at step %d of %d", n, step, len(steps))
		}
		if e.G.Turn >= 1 && len(e.G.Stack) == 0 && (e.G.Turn != lastTurn || e.G.Step.String() != lastStep) {
			lastTurn, lastStep = e.G.Turn, e.G.Step.String()
			snaps++
			add(Entry{T: TSnap, Snap: &Snap{T: TSnap, N: snaps, Snapshot: Snapshot(e, fmt.Sprintf("t%d:%s", e.G.Turn, lastStep))}})
			if step == len(steps) {
				return l // the snapshot after the last resolution closes the log
			}
		}
		switch d.Kind {
		case decision.KMulligan:
			add(Entry{T: TMull, Mull: &Mull{T: TMull, Seat: int(d.Player), Keep: true}})
			submit(decision.Intent{Choices: []int{indexOf(d, "keep")}})
		case decision.KPriority:
			if step < len(steps) && d.Player == 0 && e.G.Active == 0 && len(e.G.Stack) == 0 && e.G.Step.String() == "main1" {
				if done, a := try(steps[step]); done {
					if a != nil {
						announcing, target = a, steps[step].target
					}
					step++
					continue
				}
			}
			ask(KindPriority, Answer{Action: "pass"})
			submit(decision.Intent{Choices: []int{indexOf(d, "pass")}})
		case decision.KTarget:
			i := slices.IndexFunc(d.Options, func(o decision.Option) bool { return objName(e.G.Obj(o.Obj)) == target })
			if i < 0 || announcing == nil {
				t.Fatalf("target ask %+v: want %q", d.Options, target)
			}
			announcing.Answer.Targets = []Ref{{Card: toF[d.Options[i].Obj]}}
			submit(decision.Intent{Choices: []int{i}})
		case decision.KAttackers, decision.KBlockers:
			kind := KindAttack
			if d.Kind == decision.KBlockers {
				kind = KindBlock
			}
			ask(kind, Answer{})
			submit(decision.Intent{Choices: []int{}})
		case decision.KChoose:
			submit(firstMin(d))
		default:
			t.Fatalf("the hand-payment game poses %s", d.Kind)
		}
	}
	t.Fatalf("stopped at step %d of %d", step, len(steps))
	return nil
}

const shiko = "Shiko and Narset, Unified"

// shikoGame is farm cmdr4 BIG-s142 pod 14201, line 417: a commander cast
// tapping Sol Ring, Ferrous Lake and Clifftop Retreat. The Retreat's first
// offered colour (R) does not pay it, so the follower must try the second.
func shikoGame(t *testing.T) *Log {
	return handGame(t, corpus(t), shiko, []string{"Clifftop Retreat", "Ferrous Lake", "Sol Ring"}, []handStep{
		{land: "Clifftop Retreat"},
		{land: "Ferrous Lake"},
		{plan: "Sol Ring"},
		{use: shiko, taps: []string{"Sol Ring", "Ferrous Lake", "Clifftop Retreat:W"}},
	})
}

func followClean(t *testing.T, l *Log, handPayments int) *Result {
	t.Helper()
	res := follow(t, l)
	if h := res.Hard; h != nil {
		t.Fatalf("hard divergence at line %d: %s %s %s gorge %q forge %q (%s)", h.Line, h.What, h.Kind, h.Field, h.Gorge, h.Forge, h.Detail)
	}
	if res.HandPayments != handPayments {
		t.Errorf("%d hand payments, want %d", res.HandPayments, handPayments)
	}
	return res
}

// TestFilterLandCastIsPaidByHand: gorge offers neither a plan nor a cast for a
// commander only a filter land's mana pays; the follower activates Forge's
// sources and casts it.
func TestFilterLandCastIsPaidByHand(t *testing.T) {
	followClean(t, shikoGame(t), 1)
}

// TestHandPaymentShortOfTheCostDiverges: sources that cannot pay the cast
// leave it answer-missing, and nothing is activated.
func TestHandPaymentShortOfTheCostDiverges(t *testing.T) {
	l := shikoGame(t)
	var line int
	for _, e := range l.Entries {
		if a := e.Ask; a != nil && len(a.Answer.Tapped) == 3 {
			a.Answer.Tapped, line = []int{a.Answer.Tapped[0], a.Answer.Tapped[2]}, e.Line
		}
	}
	res := follow(t, l)
	h := res.Hard
	if h == nil || h.What != WhatAnswerMissing || h.Field != "" || h.Line != line || line == 0 {
		t.Fatalf("divergence %+v, want answer-missing at line %d", h, line)
	}
	if res.HandPayments != 0 || len(h.Record.Log.Intents) != res.Decisions {
		t.Errorf("%d hand payments, %d intents for %d decisions", res.HandPayments, len(h.Record.Log.Intents), res.Decisions)
	}
}

// TestSignetPaymentsByHand covers the siblings of that cast: an activation
// gorge does not offer, and one whose own card is tapped for its {T} cost
// although it also makes mana.
func TestSignetPaymentsByHand(t *testing.T) {
	const paladin, hold = "Truefire Paladin", "Slayers' Stronghold"
	l := handGame(t, corpus(t), "", []string{"Wastes", hold, "Sol Ring", "Boros Signet", paladin}, []handStep{
		{land: "Wastes"},
		{plan: "Sol Ring"},
		{land: hold},
		{plan: "Boros Signet"},
		{use: paladin, taps: []string{"Wastes", "Boros Signet"}},
		{use: paladin, activate: true, ability: 1, taps: []string{"Wastes", "Boros Signet"}},
		{use: hold, activate: true, taps: []string{"Wastes", "Boros Signet", hold}, target: paladin},
	})
	res := followClean(t, l, 3)
	if res.CastTargets != 1 {
		t.Errorf("%d cast targets, want the Stronghold's from its activation line", res.CastTargets)
	}
}

// TestUnplannedSourcesPaidByHand: gorge plans the cast from other lands, but
// Forge tapped the Signet.
func TestUnplannedSourcesPaidByHand(t *testing.T) {
	const paladin = "Truefire Paladin"
	l := handGame(t, corpus(t), "", []string{"Mountain", "Plains", "Wastes", "Sol Ring", "Boros Signet", paladin}, []handStep{
		{land: "Mountain"},
		{plan: "Sol Ring"},
		{land: "Plains"},
		{plan: "Boros Signet"},
		{land: "Wastes"},
		{use: paladin, taps: []string{"Wastes", "Boros Signet"}},
	})
	followClean(t, l, 1)
}

// TestHandPaymentThatTriggersDiverges: City of Brass's damage trigger would sit
// under an ability paid for after the tap, but above it in Forge (CR 603.3), so
// the follower does not pay by hand that way.
func TestHandPaymentThatTriggersDiverges(t *testing.T) {
	const paladin, brass = "Truefire Paladin", "City of Brass"
	l := handGame(t, corpus(t), "", []string{"Wastes", brass, "Sol Ring", "Boros Signet", paladin}, []handStep{
		{land: "Wastes"},
		{plan: "Sol Ring"},
		{land: brass},
		{plan: "Boros Signet"},
		{use: paladin, taps: []string{"Wastes", "Boros Signet"}},
		{use: paladin, activate: true, taps: []string{brass + ":W", "Boros Signet"}},
	})
	line := 0
	for _, e := range l.Entries {
		if a := e.Ask; a != nil && a.Answer.Action == "activate" {
			line = e.Line
		}
	}
	res := follow(t, l)
	h := res.Hard
	if h == nil || h.What != WhatAnswerMissing || h.Line != line || line == 0 {
		t.Fatalf("divergence %+v, want answer-missing at line %d", h, line)
	}
	if res.HandPayments != 1 {
		t.Errorf("%d hand payments, want only the Paladin's cast", res.HandPayments)
	}
}
