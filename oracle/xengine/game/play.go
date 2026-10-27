package game

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/bots/heuristic"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/events"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/seat"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/testkit/bench"
)

// Default caps: 30 turns (DESIGN §2) and cardfuzz's -max-intents default.
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
}

// Play plays pair against each other with two explore bots and records the
// game. Seat i is heuristic.NewExploreBot(seed ^ 0x9e3779b97f4a7c15*(i+1)) with
// auto-pay armed, exactly as gorge's cmd/cardfuzz builds an explore seat. A
// stall (turn or intent cap, livelock, bigboard, engine panic) returns the tape
// so far with Outcome.Stall set; only a setup failure or a seat error is an
// error.
func Play(reg *cards.Registry, pair [2]decks.Deck, seed uint64, opt Options) (*Tape, error) {
	maxTurns, maxIntents := opt.MaxTurns, opt.MaxIntents
	if maxTurns == 0 {
		maxTurns = DefaultMaxTurns
	}
	if maxIntents == 0 {
		maxIntents = DefaultMaxIntents
	}
	t := &Tape{Format: TapeFormat, Set: opt.Set, Seed: seed, Entries: []Entry{}}
	var dk [][]*cards.Card
	names := make([]string, 2)
	seats := make([]seat.Seat, 2)
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
		t.Decks[i] = Deck{Name: d.Name, Colours: d.Colours, Cards: append([]string{}, d.Cards...)}
		seats[i] = heuristic.NewExploreBot(seed ^ (0x9e3779b97f4a7c15 * uint64(i+1))).EnableAutoPayMana()
	}
	cfg := rules.Config{Names: names, Decks: dk, Tokens: reg.Tokens, Seed: seed, NameUniverse: reg.Universe()}

	r := &recorder{t: t}
	var o bench.Outcome
	var e *rules.Engine
	var err error
	func() {
		defer func() {
			// A panic in rules.New or the first Advance happens before
			// PlayGame's own recover is armed.
			if p := recover(); p != nil {
				o = bench.Outcome{StallOn: "panic", Livelock: firstLine(fmt.Sprint(p))}
			}
		}()
		o, e, err = bench.PlayGame(cfg, seats, maxTurns, maxIntents, bench.Hooks{
			Setup:  r.setup,
			Submit: r.submit,
		})
	}()
	if e != nil {
		r.flush(e)
		r.markReanswered(e)
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
	return t, nil
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
	t         *Tape
	mana      []ManaRec // fired since the last flush
	flips     []Flip    // coin flips since the last flush
	flipScan  int       // how far of e.L.Events the flips were read
	manaScan  int       // how far of e.L.Events the open mana record's Produced was read
	refs      *refBinder
	started   bool     // Starter is set
	lastDraws uint64   // e.RNGDraws() at the last flush
	sent      [][]byte // the intent each entry recorded, for markReanswered
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
		}
		rec.Ref = r.refs.refOf(e, source)
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
	if n := len(r.t.Entries); n > 0 {
		last := &r.t.Entries[n-1]
		last.Mana, last.RNG, last.Flips = r.mana, draws, r.flips
	} else {
		r.t.GenesisMana, r.t.GenesisRNG = r.mana, draws
	}
	r.mana, r.flips = nil, nil
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
	if len(r.t.Entries) == 0 {
		r.refs = newRefBinder(e)
		for i := range r.t.Open {
			p := state.PlayerID(i)
			r.t.Open[i] = Opening{Hand: zoneNames(e, state.ZHand, p), Library: zoneNames(e, state.ZLibrary, p)}
		}
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
	return Pick{
		Index: o.Index, Kind: o.Kind, Obj: o.Obj, Ability: o.Ability, Mode: o.Mode,
		AltCost: o.AltCostIndex, Attacker: o.Attacker, Player: int(o.Player), Battle: o.Battle,
		Label: o.Label, Ref: r.refs.refOf(e, o.Obj), AttackerRef: r.refs.refOf(e, o.Attacker),
		BattleRef: r.refs.refOf(e, o.Battle),
	}
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
