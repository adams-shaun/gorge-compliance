package game

import (
	"context"
	"errors"
	"slices"

	"github.com/adams-shaun/gorge/bots/botpolicy"
	"github.com/adams-shaun/gorge/bots/heuristic"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/rules/pay"
	"github.com/adams-shaun/gorge/engine/state"
	"github.com/adams-shaun/gorge/engine/view"
)

// Focus is the seat that takes the one action an xproof item exists for. The
// seat acts at its first chance from turn 1's first precombat main priority on
// (an earlier action, an upkeep activation, would be missing from the span a
// start item replays, so a key usable only before main 1 cannot be focused).
type Focus struct {
	Seat    int
	Card    string // front-face name of the focus card
	Kind    string // "ability", "cast", "land", "attack", "block" or "" (no forced action)
	Ability int    // for "ability": the index into Face().Abilities (decision.Option.Ability)
	// Mode, for "cast", is the cast option's payment kind (decision.Option.Mode,
	// e.g. "kicked"); "" takes any cast option of the card.
	Mode string
	// Targets are the permanents the focus play must target, in the order the
	// play asks for them. A forced start names them because another placed
	// card can be a legal alternative the bot would pick instead (an Aura's
	// bearer, a need's enabler), so the key object must be named.
	Targets []string
	// Times is how many times the seat takes the action (0: once), each at its
	// first chance after the last.
	Times int
	// Attack and Block are the scripted combat the focus play waits for (the
	// line of a play that targets a creature in combat, "target attacking
	// creature"): the active seat attacks with exactly Attack, the defending
	// seat blocks it with exactly Block (no blocks when "" while Attack is
	// set), and the focus seat takes its play at its first chance after the
	// last of them is declared. Front-face names; "" for none.
	Attack, Block string
}

// focusState is what every seat's focusBot shares with the Setup hook: the
// focus objects, read once from the live engine before the first decision, and
// whether the action has been taken.
type focusState struct {
	f    Focus
	e    *rules.Engine
	objs map[state.ObjID]bool
	// attackObjs and blockObjs are the objects, of any seat, of the line's
	// Attack and Block cards; attacked and blocked whether the line has
	// declared each.
	attackObjs, blockObjs map[state.ObjID]bool
	attacked, blocked     bool
	done                  bool
	n                     int // actions taken
	// tapsFocus is whether the focus ability's own cost taps or untaps its
	// source ({T}, {Q}): a land focus such as Vitu-Ghazi, the City-Tree must
	// stay untapped to pay that part, so it is never tapped for mana first.
	tapsFocus bool
	// manaPick is the mana ability a focus "activate" answer chose, while
	// the engine may still ask which of the source's mana abilities it
	// activates (manaChoice); nil otherwise.
	manaPick *cards.SA
}

// took records one focus action, done once the seat has taken Times of them.
func (s *focusState) took() {
	s.n++
	if s.n >= max(s.f.Times, 1) {
		s.done = true
	}
}

// exercised is how many focus actions the game took: the seat's plays, and for
// a focus with no play (Kind "") the scripted combat it declared (the attack,
// and the block when the line has one) as the one action. A line whose attack
// was never offered leaves the key unplayed, the same as a play never offered.
func (s *focusState) exercised() int {
	if s.f.Kind == "" && s.hasLine() && s.lineDone() {
		return max(s.n, 1)
	}
	return s.n
}

// focusSeen, when set (by a test), is handed each Play's focusState as it is
// made, so the test can read what the Setup hook found and whether the focus
// acted. It is nil outside tests.
var focusSeen func(*focusState)

// read fills the focus objects: every object of the focus seat, in any zone,
// whose front face is the focus card. A multi-faced card is named as its
// card file names it ("Akoum Warrior // Akoum Teeth") while the object's face
// carries the front name alone, so both are compared as cards.NormalizeName
// folds them (to the front face): compared as written, no object of a
// transforming, modal or adventure card was ever a focus object, and the
// focus play of every such start went untaken.
func (s *focusState) read(e *rules.Engine) {
	s.e = e
	s.objs = map[state.ObjID]bool{}
	s.attackObjs, s.blockObjs = map[state.ObjID]bool{}, map[state.ObjID]bool{}
	for _, l := range []struct {
		name string
		objs map[state.ObjID]bool
	}{{s.f.Attack, s.attackObjs}, {s.f.Block, s.blockObjs}} {
		if l.name == "" {
			continue
		}
		want := cards.NormalizeName(l.name)
		for p := range e.G.Players {
			for _, id := range e.G.Zone(state.ZBattlefield, state.PlayerID(p)) {
				if isNamed(e.G.Obj(id), want) {
					l.objs[id] = true
				}
			}
		}
	}
	if s.f.Seat < 0 || s.f.Seat >= len(e.G.Players) {
		return
	}
	p := state.PlayerID(s.f.Seat)
	want := cards.NormalizeName(s.f.Card)
	for _, z := range []state.Zone{state.ZHand, state.ZBattlefield, state.ZLibrary, state.ZGraveyard, state.ZExile} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if !isNamed(o, want) {
				continue
			}
			s.objs[id] = true
			// only a battlefield source can be tapped for mana first: the
			// exclusion is about the land the focus ability's {T} pays with
			if z != state.ZBattlefield || s.f.Kind != "ability" || s.tapsFocus || s.f.Ability < 0 || s.f.Ability >= len(o.Face().Abilities) {
				continue
			}
			c := rules.ParseCost(o.Face().Abilities[s.f.Ability].Params["Cost"])
			s.tapsFocus = c.Tap || c.Untap
		}
	}
}

// isNamed reports whether o is the card want (a cards.NormalizeName) names:
// its current face, or its front face, which a transformed permanent (a start's
// BackFace placement) no longer shows.
func isNamed(o *state.Object, want string) bool {
	if o == nil || o.Face() == nil {
		return false
	}
	if cards.NormalizeName(o.Face().Name) == want {
		return true
	}
	return o.Card != nil && len(o.Card.Faces) > 0 && cards.NormalizeName(o.Card.Faces[0].Name) == want
}

// Kind "block": the focus seat is the defender. Its first block declaration
// blocks with the focus card (the first option naming it), and every other
// seat's attackers decision is answered with an attack by at least one creature
// (the bot's own pick, or the first option), so the block is offered and taken
// whatever the bots' policies.

// focusBot is a seat that takes the focus action once. It lets the bot it wraps
// decide first (its random stream is spent exactly as before, as bandBot does),
// replaces that answer when the focus action is on offer, and from then on
// defers. The bot is a field, not embedded: an embedded bot's DecideBoard
// would answer in Decide's place.
type focusBot struct {
	bot seatBot
	st  *focusState
}

var _ heuristic.BoardSeat = focusBot{}

func (b focusBot) WantsPaymentActions() bool { return b.bot.WantsPaymentActions() }

func (b focusBot) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	in, err := b.bot.Decide(ctx, v, d)
	return b.act(d, in, err)
}

func (b focusBot) DecideBoard(ctx context.Context, brd botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	hb, ok := b.bot.(heuristic.BoardSeat)
	if !ok {
		return decision.Intent{}, errors.New("game: focus seat's bot has no board path")
	}
	in, err := hb.DecideBoard(ctx, brd, d)
	return b.act(d, in, err)
}

// answer is in with its choice replaced by picks and every other form of
// answer dropped: the bot's payment, announce and arrange order belong to the
// action the bot chose, not the one the focus substitutes, and a leftover one
// makes the recorded priority a second pick (the bot's cast on top of the
// focus's pass).
func answer(in decision.Intent, picks ...int) decision.Intent {
	in.Choices, in.Rest, in.Payment, in.Announce = picks, nil, nil, nil
	return in
}

// holdPass is the focus seat's answer to a priority decision before the focus
// opens: pass, whatever the bot chose. Any other seat or decision keeps the
// bot's answer.
func (s *focusState) holdPass(d decision.Decision, in decision.Intent) decision.Intent {
	if d.Kind != decision.KPriority || int(d.Player) != s.f.Seat {
		return in
	}
	for i, o := range d.Options {
		if o.Kind == "pass" {
			return answer(in, i)
		}
	}
	return in
}

// manaFor is, for a cast or ability focus, the focus seat's first activation of
// a land, or -1. gorge offers a cast or activated ability only once the floating
// pool covers its cost (castable prices against the pool), and a seat whose bot
// pays through plans leaves the lands of a spell it has no reason to cast
// untapped (a kicked cast is never a planned payment either, as the planner
// plans the card's own cost; a pump or adapt ability is no plan at all), so the
// focus taps its lands (a forced start's mana sources: basics) one by one until
// the option, the mode's when it has one, is offered. An ability whose own cost
// taps its source never taps that source for mana: the {T} of Vitu-Ghazi, the
// City-Tree's token ability is paid by the very land the pool would have used.
func (s *focusState) manaFor(d decision.Decision) int {
	if s.f.Kind != "cast" && s.f.Kind != "ability" || int(d.Player) != s.f.Seat {
		return -1
	}
	for i, o := range d.Options {
		if o.Kind != "activate" || s.e == nil {
			continue
		}
		if s.tapsFocus && s.objs[o.Obj] {
			continue
		}
		if ob := s.e.G.Obj(o.Obj); ob != nil && ob.Face() != nil && ob.Face().IsLand() {
			return i
		}
	}
	return -1
}

// manaAbility is the focus ability of obj when it is a mana ability, else
// nil. gorge offers a mana ability at priority as the source's one
// "activate" option (CR 605.3a), never as an "ability" option carrying the
// ability's index, so an "ability" focus whose ability is a mana ability is
// taken through that option, and manaChoice answers the follow-up choice of
// which of the source's mana abilities it is.
func (s *focusState) manaAbility(obj state.ObjID) *cards.SA {
	if s.f.Kind != "ability" || s.e == nil {
		return nil
	}
	o := s.e.G.Obj(obj)
	if o == nil || o.Face() == nil || s.f.Ability < 0 || s.f.Ability >= len(o.Face().Abilities) {
		return nil
	}
	if ma := o.Face().Abilities[s.f.Ability]; cards.IsManaAbilitySA(ma) {
		return ma
	}
	return nil
}

// manaChoice is, for the engine's choice among a focus source's mana
// abilities (a KChoose of "mana" options), the option of ma: the one whose
// label is ma's, else the one whose index is ma's ordinal among the face's
// mana abilities; -1 when d is no such choice.
func (s *focusState) manaChoice(d decision.Decision, ma *cards.SA) int {
	if d.Kind != decision.KChoose || int(d.Player) != s.f.Seat || !s.objs[d.Source] {
		return -1
	}
	label := pay.ManaAbilityLabel(ma, pay.ChosenProducedColour(s.e.G, d.Source))
	for i, o := range d.Options {
		if o.Kind == "mana" && o.Label == label {
			return i
		}
	}
	ord := 0
	if o := s.e.G.Obj(d.Source); o != nil && o.Face() != nil {
		for _, a := range o.Face().Abilities[:s.f.Ability] {
			if cards.IsManaAbilitySA(a) {
				ord++
			}
		}
	}
	for i, o := range d.Options {
		if o.Kind == "mana" && o.Ability == ord {
			return i
		}
	}
	return -1
}

// target answers the focus play's target ask: the named permanents (Focus.
// Targets), when the ask is the focus card's own spell or ability. The bot's
// pick would otherwise take the first legal option, which a forced start's
// other placed cards (an Aura's bearer, a need's enabler) can be. picks the
// options whose object is named; ok is false for another source, another
// seat's ask or a target not on offer, which the bot then answers as before.
func (s *focusState) target(d decision.Decision, in decision.Intent) (decision.Intent, bool) {
	if len(s.f.Targets) == 0 || d.Kind != decision.KTarget || int(d.Player) != s.f.Seat || s.e == nil {
		return in, false
	}
	if o := s.e.G.Obj(d.Source); o == nil || o.Face() == nil ||
		cards.NormalizeName(o.Face().Name) != cards.NormalizeName(s.f.Card) {
		return in, false
	}
	want := make(map[string]bool, len(s.f.Targets))
	for _, n := range s.f.Targets {
		want[cards.NormalizeName(n)] = true
	}
	most := max(d.Max, 1) // a target ask's Max is how many objects this stage takes
	var picks []int
	for i, o := range d.Options {
		if len(picks) >= most || o.Obj == 0 {
			continue
		}
		ob := s.e.G.Obj(o.Obj)
		if ob == nil || ob.Face() == nil || !want[cards.NormalizeName(ob.Face().Name)] {
			continue
		}
		if !slices.Contains(picks, i) {
			picks = append(picks, i)
		}
	}
	if len(picks) == 0 || len(picks) < d.Min {
		return in, false
	}
	return answer(in, picks...), true
}

// open reports whether the game has reached turn 1's first precombat main
// phase: the span a start item replays begins there, so an action taken
// earlier (an upkeep activation) would be missing from it.
func (s *focusState) open() bool {
	return s.e != nil && (s.e.G.Turn > 1 || s.e.G.Turn == 1 && s.e.G.Step >= state.StepMain1)
}

// hasLine reports whether the focus waits for a scripted combat (Attack,
// Block).
func (s *focusState) hasLine() bool { return s.f.Attack != "" || s.f.Block != "" }

// lineDone reports whether the scripted combat has been declared: the attack,
// and the block when the line has one.
func (s *focusState) lineDone() bool {
	return (s.f.Attack == "" || s.attacked) && (s.f.Block == "" || s.blocked)
}

// line answers the attack and block declarations of a scripted combat: the
// active seat attacks with exactly one Attack object (its first option), and
// the defending seat blocks an Attack object with exactly one Block object,
// or declares no blocks when the line has none, so the creature the focus
// play targets is in combat as the play needs and no other creature is (a
// focus creature that taps for its play stays home). ok is false for any
// other decision, and once the line is declared.
func (s *focusState) line(d decision.Decision, in decision.Intent) (decision.Intent, bool) {
	if !s.hasLine() || !s.open() {
		return in, false
	}
	switch {
	case d.Kind == decision.KAttackers && s.f.Attack != "" && !s.attacked:
		for i, o := range d.Options {
			if s.attackObjs[o.Obj] {
				s.attacked = true
				in = answer(in, i)
				return in, true
			}
		}
	case d.Kind == decision.KBlockers && s.attacked && !s.blocked:
		if s.f.Block == "" {
			s.blocked = true
			in = answer(in)
			return in, true
		}
		pick := -1 // the first option blocking an Attack object, else the first of a Block object
		for i, o := range d.Options {
			if !s.blockObjs[o.Obj] {
				continue
			}
			if s.attackObjs[o.Attacker] {
				pick = i
				break
			}
			if pick < 0 {
				pick = i
			}
		}
		if pick >= 0 {
			s.blocked = true
			in = answer(in, pick)
			return in, true
		}
	}
	return in, false
}

// act replaces the bot's answer with the focus action when d offers it.
// Before turn 1's first precombat main phase the focus seat passes priority
// instead (holdPass): a land it tapped for mana in the upkeep would empty
// before main 1 and leave the forced line unaffordable.
func (b focusBot) act(d decision.Decision, in decision.Intent, err error) (decision.Intent, error) {
	st := b.st
	if err != nil {
		return in, err
	}
	if ma := st.manaPick; ma != nil {
		st.manaPick = nil
		if i := st.manaChoice(d, ma); i >= 0 {
			in = answer(in, i)
			return in, nil
		}
	}
	if out, ok := st.line(d, in); ok {
		// the scripted combat is declared whether or not the focus has acted
		// (a play in the declare attackers step comes before the blocks)
		return out, nil
	}
	// Before the done check: a cast focus marks done when it takes the cast
	// option, and the target ask follows that answer.
	if out, ok := st.target(d, in); ok {
		return out, nil
	}
	if st.done || st.f.Kind == "" {
		return in, err
	}
	if i := st.declineOpening(d); i >= 0 {
		return answer(in, i), nil
	}
	if st.f.Kind == "block" {
		return st.block(d, in), nil
	}
	if !st.open() || st.hasLine() && !st.lineDone() {
		// before main 1, or before the scripted combat the play waits for
		// (its target is not in combat yet, and mana tapped for it in main 1
		// would empty before combat)
		return st.holdPass(d, in), nil
	}
	switch d.Kind {
	case decision.KPriority:
		for i, o := range d.Options {
			if !st.objs[o.Obj] {
				continue
			}
			ok := false
			switch st.f.Kind {
			case "ability":
				ok = o.Kind == "ability" && o.Ability == st.f.Ability
				if ma := st.manaAbility(o.Obj); ma != nil && o.Kind == "activate" {
					ok = true
					st.manaPick = ma
				}
			case "cast":
				ok = o.Kind == "cast" && (st.f.Mode == "" || o.Mode == st.f.Mode)
			case "land":
				ok = o.Kind == "play_land"
			}
			if ok {
				st.took()
				return answer(in, i), nil
			}
		}
		if i := st.manaFor(d); i >= 0 {
			in = answer(in, i)
			return in, nil
		}
	case decision.KAttackers:
		if st.f.Kind != "attack" {
			break
		}
		for i, o := range d.Options {
			if !st.objs[o.Obj] {
				continue
			}
			st.took()
			if !slices.Contains(in.Choices, i) {
				in.Choices = append(in.Choices, i)
				slices.Sort(in.Choices)
			}
			return in, nil
		}
	}
	return in, nil
}

// declineOpening is the "No" option of an opening-hand action (a Leyline's
// "begin the game with it on the battlefield", Gemstone Caverns) on a focus
// object of the focus seat whose focus is to cast or play that card, else -1.
// The opening round runs after a scripted start is dealt, so a yes put the
// card on the battlefield before main 1 and its cast was never offered: play
// refused every such start as start-not-exercised.
func (s *focusState) declineOpening(d decision.Decision) int {
	if d.Kind != decision.KChoose || int(d.Player) != s.f.Seat || s.f.Kind != "cast" && s.f.Kind != "land" {
		return -1
	}
	for i, o := range d.Options {
		if o.Kind == "opening_no" && s.objs[o.Obj] {
			return i
		}
	}
	return -1
}

// block is act for a "block" focus: the defending focus seat blocks with the
// focus card; any other seat attacks (the focus seat's opponent, whose bot
// might otherwise decline to).
func (s *focusState) block(d decision.Decision, in decision.Intent) decision.Intent {
	if !s.open() {
		return s.holdPass(d, in)
	}
	switch d.Kind {
	case decision.KAttackers:
		if int(d.Player) == s.f.Seat || len(in.Choices) > 0 || len(d.Options) == 0 {
			return in
		}
		in.Choices = []int{0}
	case decision.KBlockers:
		if int(d.Player) != s.f.Seat {
			return in
		}
		for i, o := range d.Options {
			if s.objs[o.Obj] {
				s.took()
				in.Choices = []int{i}
				return in
			}
		}
	}
	return in
}
