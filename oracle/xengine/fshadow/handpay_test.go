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
// gorge's first plan, or activate the mana of taps and then cast use, or
// activate its ability'th ability. A tap is "Name", followed by what to pick at
// each ask its activation poses (":W" a colour; a label or an object's name for
// a cost), the last pick answering any further ask.
type handStep struct {
	land, plan, use string
	taps            []string
	activate        bool
	ability         int
	target          string // the cast's or ability's target, a card name or "player" (seat 1)
	mode            string // a land's or a plan cast's gorge Mode ("modal_land", "adventure_alt"); "" the plain one
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
	// Of a card's casts only the plain one counts.
	option := func(kind, name string, nth int) int {
		for i, o := range e.Pending().Options {
			if o.Kind == kind && objName(e.G.Obj(o.Obj)) == name && (kind != "cast" || o.Mode == "") {
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
			i := slices.IndexFunc(e.Pending().Options, func(o decision.Option) bool {
				return o.Kind == "play_land" && objName(e.G.Obj(o.Obj)) == s.land && o.Mode == s.mode
			})
			if i < 0 {
				return false, nil
			}
			ask(KindPriority, Answer{Action: "land", Card: toF[e.Pending().Options[i].Obj]})
			submit(decision.Intent{Choices: []int{i}})
			return true, nil
		}
		if s.plan != "" {
			for _, a := range e.EnsurePaymentPlanAlternatives() {
				if objName(e.G.Obj(a.Cast.Object)) != s.plan || actionWay(e.Pending(), a).mode != s.mode {
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
			name, rest, _ := strings.Cut(tap, ":")
			picks := strings.Split(rest, ":")
			if name == s.use && s.activate {
				continue // tapped as the ability's own cost
			}
			i := option("activate", name, 0)
			if i < 0 {
				t.Fatalf("%s: no mana activation offered: %+v", name, e.Pending().Options)
			}
			submit(decision.Intent{Choices: []int{i}})
			for k := 0; e.Pending().Kind == decision.KChoose; k++ {
				want := picks[min(k, len(picks)-1)]
				pick := slices.IndexFunc(e.Pending().Options, func(o decision.Option) bool { return o.ManaSymbol == want })
				if pick < 0 && want != "" {
					pick = slices.IndexFunc(e.Pending().Options, func(o decision.Option) bool {
						return strings.Contains(o.Label, want) || objName(e.G.Obj(o.Obj)) == want
					})
				}
				if pick < 0 {
					t.Fatalf("%s: %q not offered: %+v", name, want, e.Pending().Options)
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
			a.Answer.Action, a.Answer.Ability = "activate", intPtr(10+s.ability)
			for k := 0; option(kind, s.use, k) >= 0; k++ {
				a.Options = append(a.Options, Option{Action: "activate", Card: a.Answer.Card, Ability: intPtr(10 + k)})
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
			i := slices.IndexFunc(d.Options, func(o decision.Option) bool {
				if target == "player" {
					return o.Obj == 0 && int(o.Player) == 1
				}
				return objName(e.G.Obj(o.Obj)) == target
			})
			if i < 0 || announcing == nil {
				t.Fatalf("target ask %+v: want %q", d.Options, target)
			}
			ref := Ref{Card: toF[d.Options[i].Obj]}
			if target == "player" {
				p := 1
				ref = Ref{Player: &p}
			}
			announcing.Answer.Targets = []Ref{ref}
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
// sources and casts it. The line lists the one cast, as Forge's recorder does.
func TestFilterLandCastIsPaidByHand(t *testing.T) {
	l := shikoGame(t)
	a, _ := lastAsk(t, l, "cast")
	a.Options = []Option{{Action: "cast", Card: a.Answer.Card}}
	followClean(t, l, 1)
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
	res := followClean(t, signetGame(t), 3)
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

func intPtr(n int) *int { return &n }

// lastAsk is the last ask of l answering action.
func lastAsk(t *testing.T, l *Log, action string) (*Ask, int) {
	t.Helper()
	for _, e := range slices.Backward(l.Entries) {
		if e.Ask != nil && e.Ask.Answer.Action == action {
			return e.Ask, e.Line
		}
	}
	t.Fatalf("no %s ask", action)
	return nil, 0
}

// stopsAt follows l expecting an answer-missing stop at line after handPayments
// payments by hand, none of them that line's.
func stopsAt(t *testing.T, l *Log, line, handPayments int) {
	t.Helper()
	res := follow(t, l)
	h := res.Hard
	if h == nil || h.What != WhatAnswerMissing || h.Line != line {
		t.Fatalf("divergence %+v, want answer-missing at line %d", h, line)
	}
	if res.HandPayments != handPayments || len(h.Record.Log.Intents) != res.Decisions {
		t.Errorf("%d hand payments, want %d; %d intents for %d decisions", res.HandPayments, handPayments, len(h.Record.Log.Intents), res.Decisions)
	}
}

// unnamedAt follows l expecting the follower to stop at line because Forge's
// line does not name which of gorge's ways to cast or play the card its seat
// took (forgeWays), after handPayments payments by hand.
func unnamedAt(t *testing.T, l *Log, line, handPayments int) {
	t.Helper()
	res := follow(t, l)
	h := res.Hard
	if h == nil || h.What != WhatError || h.Harness != HarnessUnmapped || h.Line != line || !strings.Contains(h.Detail, "ways") {
		t.Fatalf("divergence %+v, want the unnamed-way stop at line %d", h, line)
	}
	if res.HandPayments != handPayments || len(h.Record.Log.Intents) != res.Decisions {
		t.Errorf("%d hand payments, want %d; %d intents for %d decisions", res.HandPayments, handPayments, len(h.Record.Log.Intents), res.Decisions)
	}
}

// TestPlannedCastOfAnUnnamedWayStops: a cast line without ability indexes
// names neither the face nor the alternative its seat cast, so a card gorge can
// cast two ways Forge marks alike stops at the line, rather than taking gorge's
// first: the Adventure plan (Stomp) listed before Bonecrusher Giant's creature
// cast that pays from the same sources.
func TestPlannedCastOfAnUnnamedWayStops(t *testing.T) {
	l := handGame(t, corpus(t), "", []string{"Mountain", "Sol Ring", "Bonecrusher Giant"}, []handStep{
		{land: "Mountain"},
		{plan: "Sol Ring"},
		{plan: "Bonecrusher Giant"},
	})
	_, line := lastAsk(t, l, "cast")
	unnamedAt(t, l, line, 0)
}

// TestPlannedCastOfAWayTheSourcesNameFollows: Forge's line names no face,
// but the sources it tapped do when only one of gorge's ways has a plan
// paying from exactly them: three Mountains pay Bonecrusher Giant's creature
// cast, while every Stomp plan taps two sources.
func TestPlannedCastOfAWayTheSourcesNameFollows(t *testing.T) {
	l := handGame(t, corpus(t), "", []string{"Mountain", "Mountain", "Mountain", "Bonecrusher Giant"}, []handStep{
		{land: "Mountain"},
		{land: "Mountain"},
		{land: "Mountain"},
		{plan: "Bonecrusher Giant"},
	})
	followClean(t, l, 0)
}

// TestPlannedCastOfAMarkedWayFollows: a second way Forge marks apart (a
// kicked cast) leaves Forge's unmarked line naming gorge's plain cast, which
// the follower takes.
func TestPlannedCastOfAMarkedWayFollows(t *testing.T) {
	l := handGame(t, corpus(t), "", []string{"Forest", "Forest", "Kavu Titan"}, []handStep{
		{land: "Forest"},
		{land: "Forest"},
		{plan: "Kavu Titan"},
	})
	followClean(t, l, 0)
}

// TestModalLandPlayStops: Forge's land line names no face ("Play <card>", no
// mode), so a modal double-faced land played as its back face stops at the
// line rather than playing gorge's first (front-face) option.
func TestModalLandPlayStops(t *testing.T) {
	l := handGame(t, corpus(t), "", []string{"Hengegate Pathway"}, []handStep{
		{land: "Hengegate Pathway", mode: "modal_land"},
	})
	_, line := lastAsk(t, l, "land")
	unnamedAt(t, l, line, 0)
}

// TestHandPaymentOfAMultiWayCardDiverges: gorge lists every way to cast a card
// as a "cast" option and Forge's line does not say which gorge option its seat
// took, so a card with a second way is never cast by hand: an Adventure, a
// split card, a foretell card. No way's plan taps exactly Forge's sources
// here, so the cast stops answer-missing at the line.
func TestHandPaymentOfAMultiWayCardDiverges(t *testing.T) {
	for _, c := range []struct {
		name    string
		library []string
		steps   []handStep
	}{
		{"adventure", []string{"Mountain", "Wastes", "Sol Ring", "Boros Signet", "Bonecrusher Giant"}, []handStep{
			{land: "Mountain"},
			{plan: "Sol Ring"},
			{land: "Wastes"},
			{plan: "Boros Signet"},
			{use: "Bonecrusher Giant", taps: []string{"Mountain", "Wastes", "Boros Signet"}},
		}},
		{"split", []string{"Wastes", "Sol Ring", "Izzet Signet", "Fire"}, []handStep{
			{land: "Wastes"},
			{plan: "Sol Ring"},
			{plan: "Izzet Signet"},
			{use: "Fire", taps: []string{"Wastes", "Izzet Signet"}, target: "player"},
		}},
		{"foretell", []string{"Plains", "Wastes", "Sol Ring", "Boros Signet", "Starnheim Unleashed"}, []handStep{
			{land: "Plains"},
			{plan: "Sol Ring"},
			{land: "Wastes"},
			{plan: "Boros Signet"},
			{use: "Starnheim Unleashed", taps: []string{"Sol Ring", "Boros Signet", "Plains"}},
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			l := handGame(t, corpus(t), "", c.library, c.steps)
			_, line := lastAsk(t, l, "cast")
			stopsAt(t, l, line, 0)
		})
	}
}

// TestHandPaymentForgeDoesNotNameDiverges: Forge listing a second cast of the
// card, or marking the cast kicked, leaves it unpaid although gorge has only the
// plain cast.
func TestHandPaymentForgeDoesNotNameDiverges(t *testing.T) {
	for name, doctor := range map[string]func(*Ask){
		"second cast": func(a *Ask) {
			for k := range 2 {
				a.Options = append(a.Options, Option{Action: "cast", Card: a.Answer.Card, Ability: &k})
			}
		},
		"marked": func(a *Ask) { a.Answer.Mode = "kicked" },
	} {
		t.Run(name, func(t *testing.T) {
			l := shikoGame(t)
			a, line := lastAsk(t, l, "cast")
			doctor(a)
			stopsAt(t, l, line, 0)
		})
	}
}

// TestHandPaymentAskingMoreDiverges: gorge offers an {X} cast the floating mana
// pays at X = 0 and then asks for X, which the follow could only guess.
func TestHandPaymentAskingMoreDiverges(t *testing.T) {
	l := handGame(t, corpus(t), "", []string{"Wastes", "Sol Ring", "Boros Signet", "Blaze"}, []handStep{
		{land: "Wastes"},
		{plan: "Sol Ring"},
		{plan: "Boros Signet"},
		{use: "Blaze", taps: []string{"Wastes", "Boros Signet"}, target: "player"},
	})
	_, line := lastAsk(t, l, "cast")
	stopsAt(t, l, line, 0)
}

// thornscapeGame activates Thornscape Master's second ability ({W}{W}) by hand.
// Clifftop Retreat's first offered colour pays the first one ({R}{R}) instead.
// Forge lists the ids listed and answers with 11.
func thornscapeGame(t *testing.T, listed ...int) (*Log, int) {
	const master = "Thornscape Master"
	l := handGame(t, corpus(t), "", []string{"Forest", "Sol Ring", "Forest", master, "Wastes", "Boros Signet", "Clifftop Retreat"}, []handStep{
		{land: "Forest"},
		{plan: "Sol Ring"},
		{land: "Forest"},
		{plan: master},
		{land: "Wastes"},
		{plan: "Boros Signet"},
		{land: "Clifftop Retreat"},
		{use: master, activate: true, taps: []string{"Wastes", "Boros Signet", "Clifftop Retreat:W", master}, target: master},
	})
	a, line := lastAsk(t, l, "activate")
	a.Answer.Ability, a.Options = intPtr(11), nil
	for _, id := range listed {
		a.Options = append(a.Options, Option{Action: "activate", Card: a.Answer.Card, Ability: &id})
	}
	return l, line
}

// TestHandActivationTakesForgesAbility: the search passes over a colour choice
// that makes only the card's other ability payable.
func TestHandActivationTakesForgesAbility(t *testing.T) {
	l, _ := thornscapeGame(t, 10, 11)
	followClean(t, l, 1)
}

// TestHandActivationOfAnUnrankedAbilityDiverges: Forge listing fewer abilities
// than gorge has leaves its id unranked, so nothing is paid by hand.
func TestHandActivationOfAnUnrankedAbilityDiverges(t *testing.T) {
	l, line := thornscapeGame(t, 11)
	stopsAt(t, l, line, 0)
}

// TestUnrankedActivationWithAnUnboundSourceDiverges: nothing is paid by hand,
// so a tapped id the header does not bind (a token) is not read and the line
// stays answer-missing.
func TestUnrankedActivationWithAnUnboundSourceDiverges(t *testing.T) {
	l, line := thornscapeGame(t, 11)
	a, _ := lastAsk(t, l, "activate")
	a.Answer.Tapped = append(a.Answer.Tapped, 9999)
	stopsAt(t, l, line, 0)
}

// cardID is the Forge id of seat 0's first card called name.
func cardID(t *testing.T, l *Log, name string) int {
	t.Helper()
	for _, c := range l.Header.Seats[0].Cards {
		if c.Name == name {
			return c.ID
		}
	}
	t.Fatalf("no %s in the deck", name)
	return 0
}

// TestHandPaymentCostingMoreThanTheLineSaysDiverges: a line names the sources
// Forge tapped, not what else their abilities cost, so a payment that would
// sacrifice, pay life or tap a permanent the line does not list is never taken:
// it stops at the cast rather than diverge at a later snapshot.
func TestHandPaymentCostingMoreThanTheLineSaysDiverges(t *testing.T) {
	const tower, drum, elves = "Phyrexian Tower", "Springleaf Drum", "Llanowar Elves"
	for _, c := range []struct {
		name    string
		library []string
		steps   []handStep
		cost    string // a card the line lists as tapped for a source's cost
	}{
		// Forge sacrifices the second creature: gorge asks which.
		{name: "sacrifice choice", library: []string{"Forest", elves, "Wastes", "Dimir Signet", tower, "Elvish Mystic", "Hypnotic Specter"}, steps: []handStep{
			{land: "Forest"},
			{plan: elves},
			{land: "Wastes"},
			{plan: "Dimir Signet"},
			{land: tower},
			{plan: "Elvish Mystic"},
			{use: "Hypnotic Specter", taps: []string{"Wastes", "Dimir Signet", tower + ":Sacrifice:Elvish Mystic"}},
		}},
		// One creature: gorge sacrifices it without asking.
		{name: "forced sacrifice", library: []string{"Forest", elves, "Wastes", "Dimir Signet", tower, "Hypnotic Specter"}, steps: []handStep{
			{land: "Forest"},
			{plan: elves},
			{land: "Wastes"},
			{plan: "Dimir Signet"},
			{land: tower},
			{use: "Hypnotic Specter", taps: []string{"Wastes", "Dimir Signet", tower + ":Sacrifice"}},
		}},
		// Forge taps the Elves for the Drum; gorge asks which creature.
		{name: "tap cost choice", cost: elves, library: []string{"Forest", "Young Wolf", "Wastes", elves, drum, "Goblin Piker"}, steps: []handStep{
			{land: "Forest"},
			{plan: "Young Wolf"},
			{land: "Wastes"},
			{plan: elves},
			{plan: drum},
			{use: "Goblin Piker", taps: []string{"Wastes", drum + ":" + elves + ":R"}},
		}},
		{name: "forced tap cost", cost: elves, library: []string{"Forest", "Wastes", elves, drum, "Goblin Piker"}, steps: []handStep{
			{land: "Forest"},
			{plan: elves},
			{land: "Wastes"},
			{plan: drum},
			{use: "Goblin Piker", taps: []string{"Wastes", drum + ":R"}},
		}},
		{name: "source sacrifices itself", library: []string{"Wastes", "Ferrous Lake", "Sol Ring", "Lotus Petal"}, steps: []handStep{
			{land: "Wastes"},
			{plan: "Sol Ring"},
			{land: "Ferrous Lake"},
			{plan: "Lotus Petal"},
			{use: shiko, taps: []string{"Sol Ring", "Ferrous Lake", "Lotus Petal:W"}},
		}},
		{name: "damage", library: []string{"Adarkar Wastes", "Ferrous Lake", "Sol Ring"}, steps: []handStep{
			{land: "Adarkar Wastes"},
			{land: "Ferrous Lake"},
			{plan: "Sol Ring"},
			{use: shiko, taps: []string{"Sol Ring", "Ferrous Lake", "Adarkar Wastes:W"}},
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			commander := ""
			if c.steps[len(c.steps)-1].use == shiko {
				commander = shiko
			}
			l := handGame(t, corpus(t), commander, c.library, c.steps)
			a, line := lastAsk(t, l, "cast")
			if c.cost != "" {
				a.Answer.Tapped = append(a.Answer.Tapped, cardID(t, l, c.cost))
			}
			stopsAt(t, l, line, 0)
		})
	}
}

// TestPlannedSacrificeOfAnotherPermanentDiverges: Forge casts Black Knight
// tapping Phyrexian Tower alone and sacrifices the second creature. gorge's
// plan from the Tower names the first as its victim and the line names neither,
// so the plan is not taken: the follow stops at the cast instead of reporting
// the wrong graveyard at a later snapshot.
func TestPlannedSacrificeOfAnotherPermanentDiverges(t *testing.T) {
	const tower, elves, mystic = "Phyrexian Tower", "Llanowar Elves", "Elvish Mystic"
	l := handGame(t, corpus(t), "", []string{"Forest", elves, tower, mystic, "Black Knight"}, []handStep{
		{land: "Forest"},
		{plan: elves},
		{land: tower},
		{plan: mystic},
		{use: "Black Knight", taps: []string{tower + ":Sacrifice:" + mystic}},
	})
	_, line := lastAsk(t, l, "cast")
	stopsAt(t, l, line, 0)
}

// TestPlannedPaymentCostingItsOwnSourceIsFollowed: only a plan sacrificing
// another permanent is refused. Phyrexian Tower's plain mana still pays by
// plan, and so does Lotus Petal, which the plan itself sacrifices.
func TestPlannedPaymentCostingItsOwnSourceIsFollowed(t *testing.T) {
	l := handGame(t, corpus(t), "", []string{"Phyrexian Tower", "Sol Ring", "Lotus Petal", "Llanowar Elves"}, []handStep{
		{land: "Phyrexian Tower"},
		{plan: "Sol Ring"},
		{plan: "Lotus Petal"},
		{plan: "Llanowar Elves"},
	})
	a, _ := lastAsk(t, l, "cast")
	if petal := cardID(t, l, "Lotus Petal"); !slices.Equal(a.Answer.Tapped, []int{petal}) {
		t.Fatalf("Llanowar Elves tapped %v, want only the Petal %d", a.Answer.Tapped, petal)
	}
	followClean(t, l, 0)
}

// signetGame casts Truefire Paladin, activates its second ability and activates
// Slayers' Stronghold on it, each paid by hand through Boros Signet.
func signetGame(t *testing.T) *Log {
	const paladin, hold = "Truefire Paladin", "Slayers' Stronghold"
	return handGame(t, corpus(t), "", []string{"Wastes", hold, "Sol Ring", "Boros Signet", paladin}, []handStep{
		{land: "Wastes"},
		{plan: "Sol Ring"},
		{land: hold},
		{plan: "Boros Signet"},
		{use: paladin, taps: []string{"Wastes", "Boros Signet"}},
		{use: paladin, activate: true, ability: 1, taps: []string{"Wastes", "Boros Signet"}},
		{use: hold, activate: true, taps: []string{"Wastes", "Boros Signet", hold}, target: paladin},
	})
}

// TestHandPaymentTapsMatchTheLine: the taps after the offer are the line's, no
// more (the Stronghold's {T} cost left off it) and no fewer (the Paladin listed
// although its ability does not tap it), and a payment whose targets the follow
// could not answer is not begun.
func TestHandPaymentTapsMatchTheLine(t *testing.T) {
	for _, c := range []struct {
		name   string
		nth    int // the activation doctored, from 0
		doctor func(t *testing.T, l *Log, a *Ask)
	}{
		{"unlisted offer tap", 1, func(t *testing.T, l *Log, a *Ask) {
			a.Answer.Tapped = slices.DeleteFunc(a.Answer.Tapped, func(id int) bool { return id == a.Answer.Card })
		}},
		{"listed but untapped", 0, func(t *testing.T, l *Log, a *Ask) {
			a.Answer.Tapped = append(a.Answer.Tapped, a.Answer.Card)
		}},
		{"unofferable target", 1, func(t *testing.T, l *Log, a *Ask) {
			a.Answer.Targets = []Ref{{Card: cardID(t, l, "Wastes")}}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			l := signetGame(t)
			var asks []Entry
			for _, e := range l.Entries {
				if e.Ask != nil && e.Ask.Answer.Action == "activate" {
					asks = append(asks, e)
				}
			}
			if len(asks) != 2 {
				t.Fatalf("%d activations, want 2", len(asks))
			}
			c.doctor(t, l, asks[c.nth].Ask)
			stopsAt(t, l, asks[c.nth].Line, 1+c.nth)
		})
	}
}

// TestHandPaymentPaysTheOffersOwnCost: the ability sacrificing its source is
// the offer's cost, which Forge paid too, so it is followed.
func TestHandPaymentPaysTheOffersOwnCost(t *testing.T) {
	const caterpillar = "Caustic Caterpillar"
	l := handGame(t, corpus(t), "", []string{"Wastes", "Sol Ring", "Golgari Signet", caterpillar}, []handStep{
		{land: "Wastes"},
		{plan: "Sol Ring"},
		{plan: "Golgari Signet"},
		{use: caterpillar, taps: []string{"Wastes", "Golgari Signet"}},
		{use: caterpillar, activate: true, taps: []string{"Wastes", "Golgari Signet"}, target: "Sol Ring"},
	})
	followClean(t, l, 2)
}

// TestOfferedActivationTapsForgesSources: gorge offers the Paladin's ability
// from its untapped lands, and its payment window would tap the first it
// offers (Plains); the follower pays it from the lands Forge tapped instead.
func TestOfferedActivationTapsForgesSources(t *testing.T) {
	const paladin = "Truefire Paladin"
	l := handGame(t, corpus(t), "", []string{"Plateau", "Mountain", paladin, "Plains"}, []handStep{
		{land: "Plateau"},
		{land: "Mountain"},
		{plan: paladin},
		{land: "Plains"},
		{use: paladin, activate: true, taps: []string{"Plateau:W", "Mountain"}},
	})
	followClean(t, l, 1)
}

// TestOfferedActivationSacrificingAnotherDiverges: Forge activates Frozen Shade
// tapping Phyrexian Tower alone and sacrifices the second creature. The line
// names no victim, so the activation is not paid by hand, and gorge's own
// payment of the offer passes the Tower over: the follow stops at the
// activation instead of reporting the graveyard at a later snapshot.
func TestOfferedActivationSacrificingAnotherDiverges(t *testing.T) {
	const tower, elves, mystic, shade = "Phyrexian Tower", "Llanowar Elves", "Elvish Mystic", "Frozen Shade"
	l := handGame(t, corpus(t), "", []string{"Forest", elves, "Sol Ring", "Swamp", shade, tower, mystic}, []handStep{
		{land: "Forest"},
		{plan: elves},
		{land: "Swamp"},
		{plan: "Sol Ring"},
		{plan: shade},
		{land: tower},
		{plan: mystic},
		{use: shade, activate: true, taps: []string{tower + ":Sacrifice:" + mystic}},
	})
	a, line := lastAsk(t, l, "activate")
	res := follow(t, l)
	h := res.Hard
	want := fmt.Sprintf("activate:%d tapping %v", a.Answer.Card, a.Answer.Tapped)
	if h == nil || h.What != WhatAnswerMissing || h.Line != line || h.Field != "payment" || h.Forge != want {
		t.Fatalf("divergence %+v, want answer-missing payment %q at line %d", h, want, line)
	}
	if res.HandPayments != 0 || len(h.Record.Log.Intents) != res.Decisions {
		t.Errorf("%d hand payments, want 0; %d intents for %d decisions", res.HandPayments, len(h.Record.Log.Intents), res.Decisions)
	}
}

// cardIDs is the Forge ids of seat 0's cards called name, in deck order.
func cardIDs(l *Log, name string) []int {
	var out []int
	for _, c := range l.Header.Seats[0].Cards {
		if c.Name == name {
			out = append(out, c.ID)
		}
	}
	return out
}

// TestOfferedActivationPaidByAnotherLandOfTheName: Vampiric Rites' own
// sacrifice keeps it from being paid by hand, and gorge's payment taps two of
// three Swamps. Whichever two Forge's line lists, the follow takes the offer:
// a Swamp costs no other card.
func TestOfferedActivationPaidByAnotherLandOfTheName(t *testing.T) {
	const swamp, rites, rats = "Swamp", "Vampiric Rites", "Typhoid Rats"
	l := handGame(t, corpus(t), "", []string{swamp, rites, swamp, rats, swamp}, []handStep{
		{land: swamp},
		{plan: rites},
		{land: swamp},
		{plan: rats},
		{land: swamp},
		{use: rites, activate: true, taps: []string{swamp, swamp}},
	})
	a, _ := lastAsk(t, l, "activate")
	ids := cardIDs(l, swamp)
	for _, pair := range [][2]int{{0, 1}, {0, 2}, {1, 0}, {1, 2}, {2, 0}, {2, 1}} {
		a.Answer.Tapped = []int{ids[pair[0]], ids[pair[1]]}
		t.Run(fmt.Sprint(pair), func(t *testing.T) { followClean(t, l, 0) })
	}
}

// TestOfferedUntapActivationTakesTheOffer: Farmstead Gleaner's {Q} cost
// untaps it, which keeps it from being paid by hand; gorge's own payment is
// taken.
func TestOfferedUntapActivationTakesTheOffer(t *testing.T) {
	const gleaner, icy = "Farmstead Gleaner", "Icy Manipulator"
	l := handGame(t, corpus(t), "", []string{"Forest", "Mountain", "Plains", gleaner, "Island", "Swamp", icy, "Wastes"}, []handStep{
		{land: "Forest"},
		{land: "Mountain"},
		{land: "Plains"},
		{plan: gleaner},
		{land: "Island"},
		{plan: icy},
		{land: "Swamp"},
		{use: icy, activate: true, taps: []string{"Swamp", icy}, target: gleaner},
		{land: "Wastes"},
		{use: gleaner, activate: true, taps: []string{"Mountain", "Wastes"}},
	})
	followClean(t, l, 1)
}

// TestOfferedActivationTappingTheTowerTakesTheOffer: Forge pays Vampiric Rites
// with Phyrexian Tower's {C} and a Swamp, and gorge's own payment taps both, so
// the Tower's sacrifice ability is no reason to stop.
func TestOfferedActivationTappingTheTowerTakesTheOffer(t *testing.T) {
	const tower, rites, rats = "Phyrexian Tower", "Vampiric Rites", "Typhoid Rats"
	l := handGame(t, corpus(t), "", []string{"Swamp", rites, tower, rats}, []handStep{
		{land: "Swamp"},
		{plan: rites},
		{land: tower},
		{plan: rats},
		{use: rites, activate: true, taps: []string{tower + ":C", "Swamp"}},
	})
	followClean(t, l, 0)
}

// TestOfferedActivationPaidByAnotherTowerOfTheName: under Mirror Box two
// Phyrexian Towers stay. Whichever one Forge's line lists, gorge's payment
// taps a Tower, and permanents of one name stand in for each other.
func TestOfferedActivationPaidByAnotherTowerOfTheName(t *testing.T) {
	const tower, rites, rats, box = "Phyrexian Tower", "Vampiric Rites", "Typhoid Rats", "Mirror Box"
	l := handGame(t, corpus(t), "", []string{"Swamp", rites, tower, rats, "Forest", box, tower}, []handStep{
		{land: "Swamp"},
		{plan: rites},
		{land: tower},
		{plan: rats},
		{land: "Forest"},
		{plan: box},
		{land: tower},
		{use: rites, activate: true, taps: []string{tower + ":C", "Swamp"}},
	})
	a, _ := lastAsk(t, l, "activate")
	swamp := cardIDs(l, "Swamp")[0]
	for _, id := range cardIDs(l, tower) {
		a.Answer.Tapped = []int{id, swamp}
		t.Run(fmt.Sprint(id), func(t *testing.T) { followClean(t, l, 0) })
	}
}

// TestOfferedActivationPaidByAnotherNameIsLeftToSnapshot: Forge taps a Forest
// where gorge's payment taps a second Swamp. Neither costs another card, so the
// follow takes the offer and the next snapshot reports the difference.
func TestOfferedActivationPaidByAnotherNameIsLeftToSnapshot(t *testing.T) {
	const rites, rats = "Vampiric Rites", "Typhoid Rats"
	l := handGame(t, corpus(t), "", []string{"Swamp", rites, "Swamp", rats, "Forest"}, []handStep{
		{land: "Swamp"},
		{plan: rites},
		{land: "Swamp"},
		{plan: rats},
		{land: "Forest"},
		{use: rites, activate: true, taps: []string{"Swamp", "Forest"}},
	})
	if h := follow(t, l).Hard; h == nil || h.What != WhatSnapshot {
		t.Fatalf("divergence %+v, want a snapshot one", h)
	}
}
