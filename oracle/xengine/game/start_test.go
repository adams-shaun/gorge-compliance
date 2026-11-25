package game

import (
	"errors"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// thornvaultPair is the xproof fixture: p0 has Thornvault Forager and four
// Forests on the battlefield (deck indices 0..4) and Bakersbane Duo on top of
// the library (5), p1 plays Plains.
func thornvaultPair() ([2]decks.Deck, *rules.StartSpec) {
	d0 := decks.Deck{Name: "p0", Colours: "G", Cards: []string{"Thornvault Forager", "Forest", "Forest", "Forest", "Forest", "Bakersbane Duo"}}
	for len(d0.Cards) < 40 {
		d0.Cards = append(d0.Cards, "Forest")
	}
	d1 := decks.Deck{Name: "p1", Colours: "W"}
	for len(d1.Cards) < 40 {
		d1.Cards = append(d1.Cards, "Plains")
	}
	return [2]decks.Deck{d0, d1}, &rules.StartSpec{Seats: []rules.StartSeat{{Battlefield: []int{0, 1, 2, 3, 4}, Library: []int{5}}, {}}}
}

func liveZone(e *rules.Engine, z state.Zone, p state.PlayerID) []string {
	var out []string
	for _, id := range e.G.Zone(z, p) {
		out = append(out, e.G.Obj(id).Face().Name)
	}
	return out
}

func TestPlayStartPlaces(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair, start := thornvaultPair()
	var bf, lib []string
	seen := false
	opt := Options{Set: "BLB", MaxTurns: 2, Start: start, OnCut: func(e *rules.Engine, entry int) {
		if e.G.Turn != 1 || seen {
			return
		}
		seen = true
		bf = liveZone(e, state.ZBattlefield, 0)
		lib = liveZone(e, state.ZLibrary, 0)
	}}
	play(t, reg, pair, rules.SeedForStarter(1, 2, 0), opt)
	if !seen {
		t.Fatal("no cut at turn 1")
	}
	forests, forager := 0, 0
	for _, n := range bf {
		switch n {
		case "Forest":
			forests++
		case "Thornvault Forager":
			forager++
		}
	}
	if forager != 1 || forests != 4 || len(bf) != 5 {
		t.Errorf("p0 battlefield %v, want Thornvault Forager and 4 Forest", bf)
	}
	if len(lib) == 0 || lib[0] != "Bakersbane Duo" {
		t.Errorf("p0 library top %v, want Bakersbane Duo", lib[:min(3, len(lib))])
	}
}

// foragerActs lists the turns of the seat-0 activations of Thornvault Forager's
// ability 2 on tp.
func foragerActs(tp *Tape) []int32 {
	var turns []int32
	for _, e := range tp.Entries {
		for _, p := range e.Picks {
			if e.Seat == 0 && p.Kind == "ability" && p.Ability == 2 && p.Ref == "p0:Thornvault Forager" {
				turns = append(turns, e.Turn)
			}
		}
	}
	return turns
}

// The focus seat activates ability 2 of Forager once, in turn 1's main phase.
// The explore bot activates Forager on its own in this fixture (every seed),
// so the tape alone cannot tell the focus from the bot: focusSeen reads the
// focus state instead. It must have found exactly the Forager in the Setup
// hook, and its bot must have taken the action (done) by the end of turn 1.
func TestFocusActsOnce(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair, start := thornvaultPair()
	var fst *focusState
	focusSeen = func(s *focusState) { fst = s }
	defer func() { focusSeen = nil }()
	opt := Options{Set: "BLB", MaxTurns: 3, Start: start, Focus: &Focus{Seat: 0, Card: "Thornvault Forager", Kind: "ability", Ability: 2}}
	tp := play(t, reg, pair, rules.SeedForStarter(1, 2, 0), opt)
	if fst == nil {
		t.Fatal("Play never built the focus state")
	}
	if len(fst.objs) != 1 {
		t.Errorf("Setup hook found %d focus objects, want exactly the Forager", len(fst.objs))
	}
	if !fst.done {
		t.Error("the focus seat never took its action: focusBot is not wired into Play's seat")
	}
	var turns []int32
	for _, e := range tp.Entries {
		for _, p := range e.Picks {
			if e.Seat == 0 && p.Kind == "ability" && p.Ability == 2 && p.Ref == "p0:Thornvault Forager" {
				turns = append(turns, e.Turn)
			}
		}
	}
	if len(turns) == 0 || turns[0] != 1 {
		t.Errorf("Forager's ability 2 was activated on turns %v, want turn 1 first", turns)
	}
}

type stubOpt = decision.Option

func focusDecision(kind decision.Kind, opts ...decision.Option) decision.Decision {
	return decision.Decision{Kind: kind, Options: opts}
}

func openState(f Focus, objs ...state.ObjID) *focusState {
	st := &focusState{f: f, objs: map[state.ObjID]bool{}}
	for _, o := range objs {
		st.objs[o] = true
	}
	st.e = &rules.Engine{G: &state.Game{Turn: 1, Step: state.StepMain1}}
	return st
}

// focusBot.act replaces the bot's answer with the matching option exactly
// once, defers when nothing matches (staying armed), and defers before turn 1's
// first main phase.
func TestFocusActUnit(t *testing.T) {
	ab := func(obj state.ObjID, i int) stubOpt { return stubOpt{Kind: "ability", Obj: obj, Ability: i} }
	pass := decision.Intent{Choices: []int{0}}

	st := openState(Focus{Seat: 0, Card: "X", Kind: "ability", Ability: 2}, 7)
	b := focusBot{st: st}
	d := focusDecision(decision.KPriority, stubOpt{Kind: "pass"}, ab(9, 2), ab(7, 1), ab(7, 2), ab(7, 2))
	in, _ := b.act(d, pass, nil)
	if !slices.Equal(in.Choices, []int{3}) || !st.done {
		t.Fatalf("first offer: choices %v done %v, want [3] and done (lowest matching index)", in.Choices, st.done)
	}
	in, _ = b.act(d, pass, nil)
	if !slices.Equal(in.Choices, []int{0}) {
		t.Errorf("second offer: choices %v, want the bot's [0]", in.Choices)
	}

	// Times 2: the action is taken at its first two offers, then the bot's
	st = openState(Focus{Seat: 0, Card: "X", Kind: "cast", Times: 2}, 7, 8)
	b = focusBot{st: st}
	casts := focusDecision(decision.KPriority, stubOpt{Kind: "pass"}, stubOpt{Kind: "cast", Obj: 7})
	if in, _ = b.act(casts, pass, nil); !slices.Equal(in.Choices, []int{1}) || st.done {
		t.Errorf("times 2, first: choices %v done %v, want [1], still armed", in.Choices, st.done)
	}
	casts = focusDecision(decision.KPriority, stubOpt{Kind: "pass"}, stubOpt{Kind: "cast", Obj: 8})
	if in, _ = b.act(casts, pass, nil); !slices.Equal(in.Choices, []int{1}) || !st.done {
		t.Errorf("times 2, second: choices %v done %v, want [1] and done", in.Choices, st.done)
	}
	if in, _ = b.act(casts, pass, nil); !slices.Equal(in.Choices, []int{0}) {
		t.Errorf("times 2, third: choices %v, want the bot's [0]", in.Choices)
	}

	// no matching option: defer and stay armed
	st = openState(Focus{Seat: 0, Card: "X", Kind: "ability", Ability: 2}, 7)
	b = focusBot{st: st}
	in, _ = b.act(focusDecision(decision.KPriority, stubOpt{Kind: "pass"}, ab(7, 1)), pass, nil)
	if !slices.Equal(in.Choices, []int{0}) || st.done {
		t.Errorf("no match: choices %v done %v, want the bot's answer and still armed", in.Choices, st.done)
	}

	// before turn 1's first main phase the focus defers
	st = openState(Focus{Seat: 0, Card: "X", Kind: "ability", Ability: 2}, 7)
	st.e.G.Step = state.StepUpkeep
	b = focusBot{st: st}
	in, _ = b.act(focusDecision(decision.KPriority, stubOpt{Kind: "pass"}, ab(7, 2)), pass, nil)
	if !slices.Equal(in.Choices, []int{0}) || st.done {
		t.Errorf("upkeep: choices %v done %v, want deferred", in.Choices, st.done)
	}

	// before main 1 the focus seat passes in place of the bot's play (a mana
	// tap that would empty before main 1); another seat keeps its answer
	st = openState(Focus{Seat: 0, Card: "X", Kind: "cast"}, 7)
	st.e.G.Step = state.StepUpkeep
	b = focusBot{st: st}
	tap := decision.Intent{Choices: []int{1}}
	up := focusDecision(decision.KPriority, stubOpt{Kind: "ability", Obj: 3}, stubOpt{Kind: "pass"})
	if in, _ = b.act(up, tap, nil); !slices.Equal(in.Choices, []int{1}) || st.done {
		t.Errorf("upkeep hold: choices %v done %v, want the pass [1], still armed", in.Choices, st.done)
	}
	tap = decision.Intent{Choices: []int{0}}
	if in, _ = b.act(up, tap, nil); !slices.Equal(in.Choices, []int{1}) {
		t.Errorf("upkeep hold: choices %v, want the pass [1] over the bot's tap [0]", in.Choices)
	}
	up.Player = 1
	if in, _ = b.act(up, tap, nil); !slices.Equal(in.Choices, []int{0}) {
		t.Errorf("upkeep, other seat: choices %v, want the bot's [0]", in.Choices)
	}

	// a kicked focus takes only the kicked cast option
	st = openState(Focus{Seat: 0, Card: "X", Kind: "cast", Mode: "kicked"}, 7)
	b = focusBot{st: st}
	d = focusDecision(decision.KPriority, stubOpt{Kind: "pass"}, stubOpt{Kind: "cast", Obj: 7}, stubOpt{Kind: "cast", Obj: 7, Mode: "kicked"})
	if in, _ = b.act(d, pass, nil); !slices.Equal(in.Choices, []int{2}) || !st.done {
		t.Errorf("kicked: choices %v done %v, want [2] and done", in.Choices, st.done)
	}
	st = openState(Focus{Seat: 0, Card: "X", Kind: "cast", Mode: "kicked"}, 7)
	b = focusBot{st: st}
	if in, _ = b.act(focusDecision(decision.KPriority, stubOpt{Kind: "pass"}, stubOpt{Kind: "cast", Obj: 7}), pass, nil); !slices.Equal(in.Choices, []int{0}) || st.done {
		t.Errorf("kicked, unkicked offer only: choices %v done %v, want the bot's [0], still armed", in.Choices, st.done)
	}

	// a bot error is returned untouched
	st = openState(Focus{Seat: 0, Card: "X", Kind: "land"}, 7)
	b = focusBot{st: st}
	if _, err := b.act(focusDecision(decision.KPriority, stubOpt{Kind: "play_land", Obj: 7}), pass, errors.New("boom")); err == nil || st.done {
		t.Errorf("bot error: err %v done %v, want the error and still armed", err, st.done)
	}

	// attack: the first focus attacker joins the bot's attackers, sorted
	st = openState(Focus{Seat: 0, Card: "X", Kind: "attack"}, 7)
	b = focusBot{st: st}
	d = focusDecision(decision.KAttackers, stubOpt{Obj: 4}, stubOpt{Obj: 7}, stubOpt{Obj: 8})
	in, _ = b.act(d, decision.Intent{Choices: []int{2}}, nil)
	if !slices.Equal(in.Choices, []int{1, 2}) || !st.done {
		t.Errorf("attack: choices %v done %v, want [1 2] and done", in.Choices, st.done)
	}

	// cast and land match their own option kinds
	for _, c := range []struct{ kind, opt string }{{"cast", "cast"}, {"land", "play_land"}} {
		st = openState(Focus{Seat: 0, Card: "X", Kind: c.kind}, 7)
		b = focusBot{st: st}
		in, _ = b.act(focusDecision(decision.KPriority, stubOpt{Kind: "pass"}, stubOpt{Kind: "ability", Obj: 7}, stubOpt{Kind: c.opt, Obj: 7}), pass, nil)
		if !slices.Equal(in.Choices, []int{2}) || !st.done {
			t.Errorf("%s: choices %v done %v, want [2] and done", c.kind, in.Choices, st.done)
		}
	}
}

// A kicked cast focus is never a planned payment: the focus taps its lands
// until the kicked option is offered, then casts it, and the gate on
// "if it was kicked" opens (the tape holds the kicked cast and its target).
func TestFocusKickedCast(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d0 := decks.Deck{Name: "p0", Colours: "B", Cards: []string{"Gatekeeper of Malakir", "Swamp", "Swamp", "Swamp"}}
	d1 := decks.Deck{Name: "p1", Colours: "W"}
	for len(d0.Cards) < 40 {
		d0.Cards = append(d0.Cards, "Swamp")
	}
	for len(d1.Cards) < 40 {
		d1.Cards = append(d1.Cards, "Plains")
	}
	start := &rules.StartSpec{Seats: []rules.StartSeat{{Hand: []int{0}, Battlefield: []int{1, 2, 3}}, {}}}
	opt := Options{Set: "XP", MaxTurns: 2, Start: start, Focus: &Focus{Seat: 0, Card: "Gatekeeper of Malakir", Kind: "cast", Mode: "kicked"}}
	tp := play(t, reg, [2]decks.Deck{d0, d1}, rules.SeedForStarter(1, 2, 0), opt)
	kicked := false
	for _, e := range tp.Entries {
		for _, p := range e.Picks {
			if e.Seat == 0 && p.Kind == "cast" && p.Ref == "p0:Gatekeeper of Malakir" {
				if p.Mode != "kicked" {
					t.Fatalf("Gatekeeper cast in mode %q, want kicked", p.Mode)
				}
				kicked = true
			}
		}
	}
	if !kicked {
		t.Fatal("the focus never cast Gatekeeper of Malakir")
	}
}

// A block focus: the focus seat's block declaration blocks with the focus card
// whatever the bot chose, every other seat's attack declaration attacks with at
// least one creature, and the block is taken once.
func TestFocusBlockUnit(t *testing.T) {
	st := openState(Focus{Seat: 0, Card: "Wall", Kind: "block"}, 7)
	b := focusBot{st: st}
	pick := func(d decision.Decision, in decision.Intent) []int {
		out, err := b.act(d, in, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out.Choices
	}
	// the opponent (seat 1) attacks even when its bot would not
	atk := decision.Decision{Kind: decision.KAttackers, Player: 1, Options: []decision.Option{{Kind: "attack", Obj: 9}}}
	if got := pick(atk, decision.Intent{}); !slices.Equal(got, []int{0}) {
		t.Errorf("seat 1 attackers: %v, want [0]", got)
	}
	// a bot that already attacks keeps its answer
	if got := pick(atk, decision.Intent{Choices: []int{0}}); !slices.Equal(got, []int{0}) {
		t.Errorf("seat 1 bot's attack: %v, want [0]", got)
	}
	// the focus seat's own attack is its bot's
	own := decision.Decision{Kind: decision.KAttackers, Player: 0, Options: []decision.Option{{Kind: "attack", Obj: 7}}}
	if got := pick(own, decision.Intent{}); len(got) != 0 {
		t.Errorf("focus seat attackers: %v, want the bot's (none)", got)
	}
	// the focus seat blocks with the focus card, the other creature left alone
	blk := decision.Decision{Kind: decision.KBlockers, Player: 0, Options: []decision.Option{
		{Kind: "block", Obj: 8, Attacker: 9}, {Kind: "block", Obj: 7, Attacker: 9}}}
	if got := pick(blk, decision.Intent{}); !slices.Equal(got, []int{1}) {
		t.Errorf("focus block: %v, want [1] (the focus card)", got)
	}
	if !st.done {
		t.Error("the block was taken but the focus is not done")
	}
	if got := pick(blk, decision.Intent{}); len(got) != 0 {
		t.Errorf("second block decision: %v, want the bot's", got)
	}
}

// A plain cast focus taps its lands too: an instant the explore bot has no
// reason to cast leaves them untapped, and a seat whose bot pays through
// plans is offered the cast only once the pool covers it. The focus casts
// Shock off one Mountain on turn 1 and reports it took the action once
// (Options.FocusTook); a seat given no way to cast reports 0.
func TestFocusPlainCastTapsLands(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d0 := decks.Deck{Name: "p0", Colours: "R", Cards: []string{"Shock", "Mountain"}}
	d1 := decks.Deck{Name: "p1", Colours: "W"}
	for len(d0.Cards) < 40 {
		d0.Cards = append(d0.Cards, "Mountain")
	}
	for len(d1.Cards) < 40 {
		d1.Cards = append(d1.Cards, "Plains")
	}
	for _, c := range []struct {
		name string
		bf   []int
		want int
	}{{"a Mountain to tap", []int{1}, 1}, {"no land", nil, 0}} {
		t.Run(c.name, func(t *testing.T) {
			took := -1
			start := &rules.StartSpec{Seats: []rules.StartSeat{{Hand: []int{0}, Battlefield: c.bf}, {}}}
			opt := Options{Set: "XP", MaxTurns: 2, Start: start, FocusTook: &took, Focus: &Focus{Seat: 0, Card: "Shock", Kind: "cast"}}
			play(t, reg, [2]decks.Deck{d0, d1}, rules.SeedForStarter(1, 2, 0), opt)
			if took != c.want {
				t.Errorf("FocusTook %d, want %d", took, c.want)
			}
		})
	}
}
