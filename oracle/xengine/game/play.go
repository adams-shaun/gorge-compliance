package game

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/bots/botpolicy"
	"github.com/adams-shaun/gorge/bots/heuristic"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/events"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/seat"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/engine/view"
	"github.com/adams-shaun/gorge/testkit/bench"
)

// Default caps: 30 turns and cardfuzz's -max-intents default.
const (
	DefaultMaxTurns   = 30
	DefaultMaxIntents = 20000
)

// Options tunes Play. The zero value is the sweep's configuration.
type Options struct {
	// Set is recorded in the tape header.
	Set string
	// MaxTurns and MaxIntents are the watchdog caps; 0 means the default.
	MaxTurns, MaxIntents int
	// OnCut, when set, is called at each cut point of a window scenario: the
	// first priority decision of a turn's precombat main phase, held by the
	// active player. entry is the index the decision's tape entry is about to
	// get. It runs before the intent is submitted, so e is the engine at that
	// decision. It must only read e.G: it must not emit, answer or advance
	// anything, or the tape is no longer the game that would have been played
	// without it.
	OnCut func(e *rules.Engine, entry int)
	// Script holds the answers the game opens with, in order: the log of an
	// earlier game of the same decks and seed. The explore bots play on from
	// where it ends.
	Script []decision.Intent
	// Start, when non-nil, is a scripted start: gorge's rules.Config.Start,
	// which genesis applies before turn 1 (indices into the seat's deck).
	Start *rules.StartSpec
	// StartCommanders names, per seat indexed, the cards that seat's start
	// file designates as its commanders (scen.StartSeat.Commanders, CR 903.3).
	// The named cards are placed by Start as any other card; the Setup hook
	// records each designation by appending the object's ID to
	// Game.Players[seat].Commanders, the list the engine reads for
	// Card.IsCommander (Game.CommanderOf — the designation is an attribute of
	// the card, not a printed characteristic). It deliberately does not set
	// rules.Config.Commanders: that is the Commander-format lane above, which
	// requires a commander in every deck, switches life and format, and would
	// move the card to the command zone where StartSeat cannot reach it. A
	// name not among the seat's hand or battlefield objects fails the game.
	StartCommanders [][]string
	// Focus, when non-nil, makes one seat take the focus action once, on its
	// first chance (focus.go).
	Focus *Focus
	// FocusTook, when non-nil (with Focus), is set when Play returns to how
	// many times the focus seat took its action: 0 means the action was never
	// offered at any priority, so the game exercised nothing of the key.
	FocusTook *int
	// plainSeats seats the bare explore bot where Play seats a bandBot (a test
	// compares the two on a game with no band).
	plainSeats bool
}

// scripted answers from a shared script, then as its explore bot does. It holds the
// bot in a field: an embedded bot's DecideBoard would answer in Decide's place.
type scripted struct {
	bot    seatBot
	script *[]decision.Intent
}

// seatBot is an explore seat: the bot, or the bot with its band answers made.
type seatBot interface {
	seat.Seat
	WantsPaymentActions() bool
}

// bandBot is an explore bot that forms bands. gorge's explore bot answers every
// band declaration (CR 702.22c: one Min 1 / Max 1 choose per banding attacker,
// option 0 "no more creatures in this band") with option 0, so no band ever
// attacked in a recorded game. bandBot lets the bot decide first (its random
// stream is spent exactly as before) and then replaces its answer to a band
// ask with one picked from a hash of the seed, seat and decision number: every
// option, the decline included, is as likely as any other, so a band of two
// forms half the time it is offered one candidate and grows on each re-ask.
// Nothing else is touched, and a game with no banding creature plays exactly as
// it did.
//
// The bot is a field, not embedded: an embedded bot's DecideBoard would answer
// in Decide's place (as in scripted).
type bandBot struct {
	bot  *heuristic.Bot
	seed uint64
	seat int
}

var _ heuristic.BoardSeat = bandBot{}

func (b bandBot) WantsPaymentActions() bool { return b.bot.WantsPaymentActions() }

// bandAsk is whether d is a band declaration ask: a choose whose option 0 is
// the decline and whose others are the creatures that may join the band.
func bandAsk(d decision.Decision) bool {
	return d.Kind == decision.KChoose && len(d.Options) > 1 && d.Options[0].Kind == "band"
}

// Decide is the View path (a scripted seat's, which wraps the bot).
func (b bandBot) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	in, err := b.bot.Decide(ctx, v, d)
	return b.band(d, in, err)
}

// DecideBoard keeps the bot a heuristic.BoardSeat: without it the runner
// projects a full View for every decision of every game, banding or not, and
// the bot answers from that in place of its board path.
func (b bandBot) DecideBoard(ctx context.Context, brd botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	in, err := b.bot.DecideBoard(ctx, brd, d)
	return b.band(d, in, err)
}

// blightDecline is the index of the decline of a triggered ask "you may blight
// N. If you do, ...", or -1 when d is not that ask: a choose that offers a
// trigger_cost_pay labelled "Blight N" beside a trigger_cost_decline.
//
// The followers cannot answer a paid blight. Forge's driver scripts no
// CostBlight (ScriptedCostDecision.visit(CostBlight) misses "Missing cost
// CostBlight answer"), and XMage asks the creature to put the counters on
// where gorge, with one candidate, logs no pick. Every item that paid one
// ended owed (xpg-h-costblight: 9 keys, 4 cards). A decline reaches neither
// cost walk, so the seat declines, and a paid blight stays unplayed until the
// Forge driver scripts the cost.
func blightDecline(d decision.Decision) int {
	if d.Kind != decision.KChoose {
		return -1
	}
	pay, decline := false, -1
	for i, o := range d.Options {
		switch {
		case o.Kind == "trigger_cost_pay" && strings.HasPrefix(o.Label, "Blight "):
			pay = true
		case o.Kind == "trigger_cost_decline":
			decline = i
		}
	}
	if !pay {
		return -1
	}
	return decline
}

// band replaces the bot's answer to a band ask, and only to a band ask; it
// also declines a triggered optional blight (blightDecline).
func (b bandBot) band(d decision.Decision, in decision.Intent, err error) (decision.Intent, error) {
	if err != nil {
		return in, err
	}
	if i := blightDecline(d); i >= 0 {
		in.Choices = []int{i}
		return in, nil
	}
	if !bandAsk(d) {
		return in, nil
	}
	// splitmix64 of seed, seat and decision: no ambient randomness.
	x := b.seed ^ uint64(b.seat+1)*0x9e3779b97f4a7c15 ^ d.Seq*0xbf58476d1ce4e5b9
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	in.Choices = []int{int(x % uint64(len(d.Options)))}
	return in, nil
}

func (s scripted) WantsPaymentActions() bool { return s.bot.WantsPaymentActions() }

func (s scripted) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	if len(*s.script) == 0 {
		return s.bot.Decide(ctx, v, d)
	}
	in := (*s.script)[0]
	if in.Seq != d.Seq {
		return decision.Intent{}, fmt.Errorf("game: the script answers decision %d, decision %d is pending", in.Seq, d.Seq)
	}
	*s.script = (*s.script)[1:]
	return in, nil
}

// MaxSeats is the most seats Play deals: a 4-player Commander pod.
const MaxSeats = 4

// Play plays the decks against each other, one explore bot a seat (2 to
// MaxSeats seats; more than two only in Commander), after opt.Script if
// any, and records the game. Seat i is heuristic.NewExploreBot(seed ^ 0x9e3779b97f4a7c15*(i+1)) with
// auto-pay armed, exactly as gorge's cmd/cardfuzz builds an explore seat. A
// stall (turn or intent cap, livelock, bigboard, engine panic) returns the tape
// so far with Outcome.Stall set; only a setup failure or a seat error is an
// error.
func Play(reg *cards.Registry, pair []decks.Deck, seed uint64, opt Options) (*Tape, error) {
	maxTurns, maxIntents := opt.MaxTurns, opt.MaxIntents
	if maxTurns == 0 {
		maxTurns = DefaultMaxTurns
	}
	if maxIntents == 0 {
		maxIntents = DefaultMaxIntents
	}
	if n := len(pair); n < 2 || n > MaxSeats {
		return nil, fmt.Errorf("game: %d decks, want 2 to %d", n, MaxSeats)
	}
	t := &Tape{Format: TapeFormat, Set: opt.Set, Seed: seed, Entries: []Entry{}, Decks: make([]Deck, len(pair)), Open: make([]Opening, len(pair))}
	var dk [][]*cards.Card
	names := make([]string, len(pair))
	seats := make([]seat.Seat, len(pair))
	script := opt.Script
	var fst *focusState
	if opt.Focus != nil {
		fst = &focusState{f: *opt.Focus, objs: map[state.ObjID]bool{}}
		if focusSeen != nil {
			focusSeen(fst)
		}
	}
	for i, d := range pair {
		cs := make([]*cards.Card, 0, len(d.Cards))
		for _, n := range d.Cards {
			c, ok := reg.Lookup(n)
			if !ok {
				return nil, fmt.Errorf("game: deck %q: card %q not found", d.Name, n)
			}
			cs = append(cs, c)
		}
		dk = append(dk, cs)
		names[i] = fmt.Sprintf("%s-%d", d.Colours, i)
		t.Decks[i] = Deck{Name: d.Name, Colours: d.Colours, Cards: append([]string{}, d.Cards...), Commander: d.Commander}
		hb := heuristic.NewExploreBot(seed ^ (0x9e3779b97f4a7c15 * uint64(i+1))).EnableAutoPayMana()
		var bot seatBot = bandBot{bot: hb, seed: seed, seat: i}
		if opt.plainSeats {
			bot = hb
		}
		// a block focus, a scripted combat, a scripted bait (cast or ability
		// activation) and a prelude cast also steer the other seats: their
		// attack, block, bait or prelude is forced
		if fst != nil && (opt.Focus.Seat == i || opt.Focus.Kind == "block" || opt.Focus.Attack != "" || opt.Focus.Block != "" ||
			opt.Focus.Bait != "" || opt.Focus.BaitAbility != "" || opt.Focus.BaitTarget != "" || opt.Focus.BaitTargetPlayer ||
			opt.Focus.Prelude != "") {
			bot = focusBot{bot: bot, st: fst}
		}
		seats[i] = bot
		if len(script) > 0 {
			seats[i] = scripted{bot: bot, script: &script}
		}
	}
	// The live game and the oracle runner must offer the same card names: the
	// runner scopes a scenario's name asks to its seated cards (gorge's
	// oracleNameUniverse), so a name this game picks outside them can never be
	// replayed. NamedCorpus stays the whole registry for the derived
	// vocabularies and "copy the card named N", exactly as the runner wires it.
	cfg := rules.Config{
		Names: names, Decks: dk, Tokens: reg.Tokens, Seed: seed,
		NamedCorpus: reg.Universe(), NameUniverse: decks.NameUniverse(dk),
		Start: opt.Start,
	}
	anyCmdr, allCmdr := false, true
	for _, d := range pair {
		anyCmdr = anyCmdr || d.Commander != ""
		allCmdr = allCmdr && d.Commander != ""
	}
	if len(pair) > 2 && !allCmdr {
		return nil, fmt.Errorf("game: a %d-seat game is a Commander game, and needs a commander in every deck", len(pair))
	}
	if anyCmdr {
		if !allCmdr {
			cs := make([]string, len(pair))
			for i, d := range pair {
				cs[i] = fmt.Sprintf("%q", d.Commander)
			}
			which := "all"
			if len(pair) == 2 {
				which = "both"
			}
			return nil, fmt.Errorf("game: a Commander game needs a commander in %s decks (%s)", which, strings.Join(cs, ", "))
		}
		cfg.Format = rules.FormatCommander
		cfg.StartingLife = CommanderStartingLife
		cfg.Commanders = make([][]int, len(pair))
		for i, d := range pair {
			idx := d.CommanderIndex()
			if idx < 0 {
				return nil, fmt.Errorf("game: deck %q: commander %q is not among its cards", d.Name, d.Commander)
			}
			cfg.Commanders[i] = []int{idx}
		}
		t.GameFormat = GameFormatCommander
	}

	r := &recorder{t: t, onCut: opt.OnCut}
	var o bench.Outcome
	var e *rules.Engine
	var err error
	// cmdrErr holds a start-commander designation the Setup hook could not
	// make (a named card the seat does not hold): the game runs on and the
	// tape is returned, but Play reports it — a start claiming a commander
	// the game does not hold is a broken claim, never a silent skip.
	var cmdrErr error
	func() {
		defer func() {
			// A panic in rules.New or the first Advance happens before
			// PlayGame's own recover is armed.
			if p := recover(); p != nil {
				o = bench.Outcome{StallOn: "panic", Livelock: firstLine(fmt.Sprint(p))}
			}
		}()
		o, e, err = bench.PlayGame(cfg, seats, maxTurns, maxIntents, bench.Hooks{
			Setup: func(e *rules.Engine) {
				// designate before r.setup: the recorder's baseline is then
				// the game as the start means it to be, commander designations
				// included (the list the engine reads for Card.IsCommander,
				// CR 903.3), and the hook runs after genesis applied opt.Start.
				if cmdrErr == nil && len(opt.StartCommanders) > 0 {
					cmdrErr = designateStartCommanders(e, opt.StartCommanders)
				}
				r.setup(e)
				if fst != nil {
					fst.read(e)
				}
			},
			Submit: r.submit,
		})
	}()
	if e != nil {
		r.flush(e)
		r.markReanswered(e)
	}
	if fst != nil && opt.FocusTook != nil {
		*opt.FocusTook = fst.exercised()
	}
	t.Outcome = Outcome{Turns: o.Turns, Intents: o.Intents, Stall: o.StallOn, Diag: o.Livelock}
	if o.StallOn == "panic" {
		// The full diagnostic carries a stack with addresses.
		t.Outcome.Diag = firstLine(o.Livelock)
	}
	if o.StallOn == "" {
		if o.Draw {
			t.Outcome.Draw = true
		} else {
			w := o.WinnerSeat
			t.Outcome.Winner = &w
		}
	}
	if err != nil {
		return t, err
	}
	if cmdrErr != nil {
		return t, cmdrErr
	}
	return t, nil
}

// designateStartCommanders appends each seat's designated commander cards to
// that seat's Game.Players[seat].Commanders list — the list Game.CommanderOf
// reads to answer Card.IsCommander (CR 903.3's designation is an attribute of
// the card, held across zones, not a printed characteristic, so the engine
// never reads it off a face). It runs from the Setup hook, after genesis has
// applied the start, and deliberately not through rules.Config.Commanders:
// that route is the Commander-format lane and would move the card to the
// command zone where the start's placements cannot reach it. Names are walked
// in file order and found by a slice walk of the seat's hand and battlefield
// (the prelude's own isNamed), so no map walk reaches the list; a name the
// seat does not hold is an error, not a silent skip.
func designateStartCommanders(e *rules.Engine, perSeat [][]string) error {
	for s, names := range perSeat {
		if s >= len(e.G.Players) {
			return fmt.Errorf("game: start: %d commander seats for a %d-seat game", len(perSeat), len(e.G.Players))
		}
		p := state.PlayerID(s)
		for _, name := range names {
			want := cards.NormalizeName(name)
			var id state.ObjID
			found := false
			for _, objs := range [][]state.ObjID{e.G.Zone(state.ZHand, p), e.G.Zone(state.ZBattlefield, p)} {
				for _, o := range objs {
					if isNamed(e.G.Obj(o), want) {
						id, found = o, true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				return fmt.Errorf("game: start: seat %d designates commander %q, which is neither in its hand nor on its battlefield", s, name)
			}
			e.G.Players[s].Commanders = append(e.G.Players[s].Commanders, id)
		}
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// recorder is the Hooks state: it watches mana abilities from genesis and
// writes one Entry per decision, just before the engine commits the intent.
type recorder struct {
	t     *Tape
	mana  []ManaRec // fired since the last flush
	flips []Flip    // coin flips since the last flush
	// flipStart is the coin-flip statics on the battlefield when the entry
	// currently being recorded began to commit. The engine reads them while
	// the batch resolves; the flush only sees the board after it, so this
	// snapshot is what keeps a static destroyed in the same entry from
	// turning a forced flip into a counted draw.
	flipStart flipStatics
	flipScan  int    // how far of e.L.Events the flips were read
	rolls     []Roll // die rolls since the last flush
	rollScan  int    // how far of e.L.Events the die rolls were read
	manaScan  int    // how far of e.L.Events the open mana record's Produced was read
	spendScan int    // how far of e.L.Events the spent mana was read
	trigScan  int    // how far of e.L.Events the trigger pushes were read
	trigs     []Trig // triggers put on the stack since the last flush
	spent     string // pool mana spent since the last flush, one letter per unit
	refs      *refBinder
	started   bool     // Starter is set
	lastDraws uint64   // e.RNGDraws() at the last flush
	sent      [][]byte // the intent each entry recorded, for markReanswered
	onCut     func(e *rules.Engine, entry int)
	cutTurn   int32 // the turn onCut last fired for
}

func (r *recorder) setup(e *rules.Engine) {
	e.ManaAbilityHook = func(p state.PlayerID, source state.ObjID, sa *cards.SA) {
		rec := ManaRec{Obj: source, Player: int(p), Ability: -1}
		if o := e.G.Obj(source); o != nil && o.Face() != nil {
			rec.Owner, rec.Name = int(o.Owner), o.Face().Name
			for i, a := range o.Face().Abilities {
				if a == sa {
					rec.Ability = i
					break
				}
			}
			// A triggered mana ability (CR 605.1b) hands the hook its trigger's
			// effect body, which is no entry of Face().Abilities: mark it, so
			// the lowering can tell it from a gained or granted activation.
			if rec.Ability < 0 {
				for _, tr := range o.Face().Triggers {
					if tr.Effect == sa {
						rec.Triggered = true
						break
					}
				}
			}
		}
		rec.Ref = r.refs.refOf(e, source)
		rec.Card, rec.Face = backFace(e.G.Obj(source))
		r.closeMana(e)
		r.mana = append(r.mana, rec)
		r.manaScan = len(e.L.Events)
	}
}

// closeMana reads what the latest mana record's activation added to its
// player's pool: the ManaAdd events logged since the hook fired.
func (r *recorder) closeMana(e *rules.Engine) {
	if len(r.mana) == 0 {
		return
	}
	rec := &r.mana[len(r.mana)-1]
	for _, ev := range e.L.Events[r.manaScan:] {
		if ev.Kind == events.ManaAdd && ev.Amount > 0 && int(ev.Player) == rec.Player {
			// Counter is the colour letter, behind a spend-restriction prefix
			// ("ArtifactG") when the mana carries one.
			if n := len(ev.Counter); n > 0 && strings.ContainsRune("WUBRGC", rune(ev.Counter[n-1])) {
				rec.Produced += strings.Repeat(ev.Counter[n-1:], int(ev.Amount))
			}
		}
	}
	r.manaScan = len(e.L.Events)
}

// flush hands the mana and draws since the last flush to the latest entry, or
// to the header before the first.
func (r *recorder) flush(e *rules.Engine) {
	r.closeMana(e)
	draws := e.RNGDraws() - r.lastDraws
	r.lastDraws = e.RNGDraws()
	evs := e.L.Events
	for ; r.flipScan < len(evs); r.flipScan++ {
		if flipper, win, ok := effects.FlipNoteResult(evs[r.flipScan]); ok {
			r.flips = append(r.flips, Flip{Seat: int(flipper), Win: win})
		}
	}
	markFlipDraws(e, r.flips, r.flipStart)
	for ; r.rollScan < len(evs); r.rollScan++ {
		if roller, sides, natural, result, ok := effects.DieRollResult(evs[r.rollScan]); ok {
			r.rolls = append(r.rolls, Roll{Seat: int(roller), Sides: int(sides), Natural: int(natural), Result: int(result)})
		}
	}
	for ; r.trigScan < len(evs); r.trigScan++ {
		if t, ok := trigOf(e, evs[r.trigScan]); ok {
			r.trigs = append(r.trigs, t)
		}
	}
	seat := -1
	if n := len(r.t.Entries); n > 0 {
		seat = r.t.Entries[n-1].Seat
	}
	for ; r.spendScan < len(evs); r.spendScan++ {
		ev := evs[r.spendScan]
		if ev.Kind != events.ManaAdd || ev.Amount >= 0 {
			continue
		}
		// A cost paid by another seat inside the same commit (a ward or an
		// unless-cost) is not the decision's spend. The entry's seat is the
		// payer of its own cast/activation.
		if seat >= 0 && int(ev.Player) != seat {
			continue
		}
		// The engine emits the payment as negative ManaAdd events, one per
		// spent unit; the Counter is the colour letter behind any restriction
		// or snow prefix ("ArtifactG", "SG"). This is the single place a
		// spend is read, so every payment path (plain, restricted, typed,
		// snow) is covered without naming one.
		if n := len(ev.Counter); n > 0 && strings.ContainsRune("WUBRGC", rune(ev.Counter[n-1])) {
			r.spent += strings.Repeat(ev.Counter[n-1:], int(-ev.Amount))
		}
	}
	if n := len(r.t.Entries); n > 0 {
		last := &r.t.Entries[n-1]
		last.Mana, last.Spent, last.RNG, last.Flips, last.Rolls = r.mana, r.spent, draws, r.flips, r.rolls
		last.Triggers = r.trigs
	} else {
		r.t.GenesisMana, r.t.GenesisRNG = r.mana, draws
	}
	r.mana, r.spent, r.flips, r.rolls, r.trigs = nil, "", nil, nil, nil
}

// trigOf reads one log event as a trigger push: Amount names the trigger's
// face and index (events.TriggerPushFaceAmount). It reports false for any
// other event, and for a push it cannot pin to a printed line of a registered
// card: a copy's face, a merged pile's under-card, a left-copy trigger, or
// the layer-granted Dethrone sentinel.
func trigOf(e *rules.Engine, ev events.Event) (Trig, bool) {
	if ev.Kind != events.TriggerPush {
		return Trig{}, false
	}
	src := e.G.Obj(ev.Obj)
	if src == nil || src.Card == nil || len(src.Card.Faces) == 0 || src.Card.Faces[0] == nil ||
		src.CopyFace != nil || len(src.MergedCards) > 0 {
		return Trig{}, false
	}
	code, idx, ok := events.MergedTriggerIndexes(ev.Amount)
	if !ok {
		return Trig{}, false
	}
	front, _, _ := events.MergedTriggerIndexes(events.TriggerPushFaceAmount(0, 0))
	left, _, _ := events.MergedTriggerIndexes(events.TriggerPushLeftCopyAmount(0))
	face := code
	switch code {
	case 0: // a plain index names the live face
		face = int(src.FaceIdx)
	case front:
		face = 0
	case left:
		return Trig{}, false
	}
	if face >= len(src.Card.Faces) {
		return Trig{}, false
	}
	return Trig{Obj: ev.Obj, Name: src.Card.Faces[0].Name, Face: face, Index: idx}, true
}

// backFace is the front-face name and live face index of an object showing a
// face other than its card's front, "" and 0 otherwise (and for a copy, whose
// face is not the card's).
func backFace(o *state.Object) (string, int) {
	if o == nil || o.Card == nil || o.FaceIdx == 0 || o.CopyFace != nil ||
		len(o.Card.Faces) == 0 || o.Card.Faces[0] == nil {
		return "", 0
	}
	return o.Card.Faces[0].Name, int(o.FaceIdx)
}

// flipStatics is which seats controlled the coin-flip statics when one
// recorded entry began to commit.
type flipStatics struct {
	mod     map[int]bool
	doubler map[int]bool
}

// snapshotFlipStatics reads the two statics the engine consults while a flip
// resolves. The engine sees them mid-batch; the recorder's only other look is
// the flush, after the entry's own events have changed the board.
func snapshotFlipStatics(e *rules.Engine) flipStatics {
	s := flipStatics{mod: make(map[int]bool, len(e.G.Players)), doubler: make(map[int]bool, len(e.G.Players))}
	for i := range e.G.Players {
		p := state.PlayerID(i)
		s.mod[i] = controlsFlipCoinMod(e.G, p)
		s.doubler[i] = controlsFlipCoinDoubler(e.G, p)
	}
	return s
}

// markFlipDraws accounts for each coin flip's engine draws. A FlipCoinMod
// static turns the turn's first winning flip heads without drawing, and a
// FlipCoinDoubler static makes coinResult draw twice for one flip (Krark's
// Thumb). The canonical flip Note carries neither, and the engine reads the
// statics while the batch resolves, which the flush cannot see. The
// start-of-entry snapshot is the board the engine could have read before the
// batch; the flush board is what survives it. A winning first flip is marked
// Forced when the static was present at either end: a static destroyed (or
// created) inside the entry is treated as present, which makes the gate
// refuse an entry it cannot account for instead of accepting an unproven
// shuffle. For the same reason a doubler counts two draws only when it was
// present at BOTH ends; a doubler that changed inside the entry is counted as
// one draw, which can only make the gate stricter. Marking a whole winning
// batch forced is likewise conservative: the engine reads the static once per
// batch, and only the first flip of a FlipUntilYouLose run can be forced.
func markFlipDraws(e *rules.Engine, flips []Flip, start flipStatics) {
	if len(flips) == 0 {
		return
	}
	count := make(map[int]int, 2)
	for _, f := range flips {
		count[f.Seat]++
	}
	first := make(map[int]bool, len(count))
	for seat, n := range count {
		if seat < 0 || seat >= len(e.G.Players) {
			continue
		}
		first[seat] = int(e.G.Players[seat].CoinFlipsThisTurn)-n <= 0
	}
	for i := range flips {
		f := &flips[i]
		if f.Seat < 0 || f.Seat >= len(e.G.Players) {
			f.Draws = 1
			continue
		}
		p := state.PlayerID(f.Seat)
		f.Forced = f.Win && first[f.Seat] &&
			(start.mod[f.Seat] || controlsFlipCoinMod(e.G, p))
		if f.Forced {
			f.Draws = 0
			continue
		}
		if start.doubler[f.Seat] && controlsFlipCoinDoubler(e.G, p) {
			f.Draws = 2
		} else {
			f.Draws = 1
		}
	}
}

// controlsFlipCoinMod is effects.FlipCoinForced's static test without its
// "no flip yet this turn" half. The snapshot runs before the flip and the
// flush after it, so only the static is readable at either look. The
// parameter shape is the one gorge reads for Edgar, King of Figaro; any other
// shape is not a force.
func controlsFlipCoinMod(g *state.Game, p state.PlayerID) bool {
	if int(p) < 0 || int(p) >= len(g.Players) {
		return false
	}
	for _, controller := range g.Players {
		for _, id := range g.Zone(state.ZBattlefield, controller.ID) {
			o := g.Obj(id)
			if o == nil || o.PhasedOut || o.FaceDown || o.Face() == nil || o.Controller != p {
				continue
			}
			for _, st := range o.Face().Statics {
				if st.Mode == effects.ModeFlipCoinMod &&
					st.ParamStr(cards.PKValidPlayer) == "You" &&
					st.ParamStr(cards.PKCheckSVar) == "Count$YouFlipThisTurn" &&
					st.ParamStr(cards.PKSVarCompare) == "EQ0" &&
					st.ParamStr(cards.PKResult) == "True" {
					return true
				}
			}
		}
	}
	return false
}

// controlsFlipCoinDoubler is effects.youStaticCount(g, p, FlipCoinDoubler)
// (unexported): a live permanent p controls carrying that mode and no
// parameter beyond the corpus shape. Counted at both ends of an entry so a
// statics change can only under-count, never invent a second draw.
func controlsFlipCoinDoubler(g *state.Game, p state.PlayerID) bool {
	if int(p) < 0 || int(p) >= len(g.Players) {
		return false
	}
	for _, owner := range g.Players {
		for _, id := range g.Zone(state.ZBattlefield, owner.ID) {
			o := g.Obj(id)
			if o == nil || o.Controller != p || o.PhasedOut || o.FaceDown || o.Face() == nil {
				continue
			}
			for _, st := range o.Face().Statics {
				if st.Mode == effects.ModeFlipCoinDoubler &&
					staticParamsOnly(st.Params, "Mode", "ValidPlayer", "Description") {
					return true
				}
			}
		}
	}
	return false
}

// staticParamsOnly mirrors effects.staticKeysReadable: a static whose params
// carry a key outside the allowed corpus shape does not function.
func staticParamsOnly(params map[string]string, keys ...string) bool {
	for k := range params {
		found := false
		for _, allowed := range keys {
			if k == allowed {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func zoneNames(e *rules.Engine, z state.Zone, p state.PlayerID) []string {
	ids := e.G.Zone(z, p)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		n := ""
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			n = o.Face().Name
		}
		out = append(out, n)
	}
	return out
}

// submit is Hooks.Submit: it records, then lets the engine commit.
func (r *recorder) submit(e *rules.Engine, seatIdx int, d *decision.Decision, in decision.Intent) (bool, error) {
	r.flush(e) // what the previous entry's commit did
	// The board is now exactly as this entry's commit will leave it before it
	// starts resolving: the moment to read the statics the flip will consult.
	r.flipStart = snapshotFlipStatics(e)
	if len(r.t.Entries) == 0 {
		r.refs = newRefBinder(e)
		for i := range r.t.Open {
			p := state.PlayerID(i)
			r.t.Open[i] = Opening{Hand: zoneNames(e, state.ZHand, p), Library: zoneNames(e, state.ZLibrary, p)}
			if cmd := zoneNames(e, state.ZCommand, p); len(cmd) > 0 {
				r.t.Open[i].Command = cmd
			}
		}
	}
	if r.onCut != nil && d.Kind == decision.KPriority && e.G.Step == state.StepMain1 &&
		d.Player == e.G.Active && e.G.Turn > r.cutTurn {
		r.cutTurn = e.G.Turn
		r.onCut(e, len(r.t.Entries))
	}
	if !r.started && e.G.Turn >= 1 {
		// The starting player is turn 1's active player; a pre-game ask
		// (turn 0) comes first when a card in an opening hand asks.
		r.started = true
		r.t.Starter = int(e.G.Active)
	}
	en := Entry{
		Seq: d.Seq, Seat: seatIdx, Kind: string(d.Kind), Turn: e.G.Turn, Step: e.G.Step.String(),
		Choices: append([]int{}, in.Choices...), Rest: append([]int(nil), in.Rest...),
		Max:    d.Max,
		Digest: DigestOfGame(e),
	}
	if n := len(e.G.Stack); n > 0 {
		en.StackTop = e.G.Stack[n-1]
	}
	for _, c := range in.Choices {
		if c >= 0 && c < len(d.Options) {
			en.Picks = append(en.Picks, r.pickOf(e, d.Options[c]))
		}
	}
	switch {
	case in.Payment != nil:
		en.Payment = in.Payment.ActionID
		en.Picks = append(en.Picks, r.actionPick(e, d, in.Payment.ActionID))
	case in.Announce != nil:
		en.Announce = in.Announce.ActionID
		en.Picks = append(en.Picks, r.actionPick(e, d, in.Announce.ActionID))
	}
	r.sent = append(r.sent, canonIntent(in))
	r.t.Entries = append(r.t.Entries, en)
	return false, nil
}

func (r *recorder) pickOf(e *rules.Engine, o decision.Option) Pick {
	p := Pick{
		Index: o.Index, Kind: o.Kind, Obj: o.Obj, Ability: o.Ability, Mode: o.Mode,
		AltCost: o.AltCostIndex, Attacker: o.Attacker, Player: int(o.Player), Battle: o.Battle,
		Label: o.Label, Ref: r.refs.refOf(e, o.Obj), AttackerRef: r.refs.refOf(e, o.Attacker),
		BattleRef: r.refs.refOf(e, o.Battle),
		SVar:      o.SVar, Keyword: o.Keyword,
		GrantSource: o.GrantSource, GainedSource: o.GainedSource, GainedIdx: o.GainedIdx,
	}
	if o.Kind == "ability" && e != nil {
		p.Card, p.Face = backFace(e.G.Obj(o.Obj))
	}
	return p
}

// actionPick is the cast a payment action names: its base priority option
// when it has one, else the action itself (a floating-mana plan has none).
func (r *recorder) actionPick(e *rules.Engine, d *decision.Decision, id string) Pick {
	for _, a := range d.PaymentActions {
		if a.ID != id {
			continue
		}
		if a.BaseOptionIndex != nil && *a.BaseOptionIndex >= 0 && *a.BaseOptionIndex < len(d.Options) {
			return r.pickOf(e, d.Options[*a.BaseOptionIndex])
		}
		return Pick{Index: -1, Kind: "cast", Obj: a.Cast.Object, Mode: a.Cast.CastMode(), Player: int(d.Player), Label: a.Label,
			Ref: r.refs.refOf(e, a.Cast.Object)}
	}
	return Pick{Index: -1, Kind: "payment", Player: int(d.Player)}
}

// canonIntent is the intent's JSON with an empty Choices or Rest read as absent
// (the engine logs an empty answer as nil, a bot may send []).
func canonIntent(in decision.Intent) []byte {
	if len(in.Choices) == 0 {
		in.Choices = nil
	}
	if len(in.Rest) == 0 {
		in.Rest = nil
	}
	b, _ := json.Marshal(in)
	return b
}

// markReanswered flags every entry whose recorded intent is not the one the
// engine logged for that decision (bench's refusal ladder committed another).
func (r *recorder) markReanswered(e *rules.Engine) {
	logged := map[uint64][]byte{}
	for _, in := range e.L.Intents {
		logged[in.Seq] = canonIntent(in)
	}
	for i := range r.t.Entries {
		if b, ok := logged[r.t.Entries[i].Seq]; ok && string(b) != string(r.sent[i]) {
			r.t.Entries[i].Reanswered = true
		}
	}
}
