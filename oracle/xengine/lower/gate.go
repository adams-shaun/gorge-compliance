package lower

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/pincap"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/events"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// Refusal reasons that are not a bare field:<Type>.<Name>.
const (
	reasonContinuous  = "field:Engine.continuous"
	reasonPhaseTrig   = "trigger:upkeep_or_draw"
	reasonByName      = "setup_by_name:"       // + back_face | counters
	reasonCountersLow = "counters_below_entry" // fewer counters than the card enters with
	reasonZone        = "zone:"                // + the zone name
	reasonCard        = "card:no_front_face"
	// reasonAuraUnattached is a battlefield Aura attached to no permanent (it
	// can only be so in transit) and to no player: setup places an Aura
	// attached (CR 303.4f), so there is no bearer to name.
	reasonAuraUnattached = "aura_unattached"
	// reasonOpeningHand is a hand card that can act before turn 1 (CR 103.6a,
	// Leyline and the other MayEffectFromOpeningHand cards): the runner's setup
	// drive answers the opening-hand ask, so the card would leave the hand.
	reasonOpeningHand = "opening_hand_effect"
	// reasonSelfExile is a card exiled by its own effect (warp). Its recast
	// permission from exile is derived from the event log (warpRecastAvailable),
	// so no gate field shows it and setup's plain exile placement cannot grant it.
	reasonSelfExile = "exile_self"
	// reasonSpeedLatch is a window whose setup would consume the inherent
	// CR 702.179d speed trigger: setup places the non-active seat's life below
	// 20 as a life-loss LifeChange, and the active seat already has speed 1..3,
	// so the once-per-turn trigger is queued, resolved and latched at turn 1.
	// The decision is setup state alone: the tape may show no in-window fall
	// (the setup LifeChange is the trigger) and cannot show the speed.
	reasonSpeedLatch = "setup_speed_latch"
	// reasonCountersEntry is a battlefield permanent whose counter state setup
	// cannot restate: an entry counter grant that is dynamic (K:etbCounter with
	// an X/Count$ amount), so the placement re-runs the grant and setup cannot
	// know how many counters to add on top of it; or a gorge internal counter
	// marker (the regeneration "Shield", CR 701.19a, is a replacement effect,
	// not a counter) that the live digest omits and a scenario cannot place.
	reasonCountersEntry = "counters_entry_unrestatable"
	// reasonEntryTap is a non-active seat's UNTAPPED battlefield card whose
	// placement runs an "enters tapped" replacement. The runner's placement
	// re-runs it (like the entry counter grant), and with no answer to the
	// replacement's unless-cost it declines and taps the permanent; the
	// non-active seat has no untap step before the cut, so the replay can never
	// show it untapped again. The active seat is exempt: its turn-1 untap step
	// restores what the placement tapped.
	reasonEntryTap = "setup_entry_tap"
	// reasonEntryCounterOther is a battlefield permanent whose replacement puts
	// counters on OTHER permanents as they enter (Deanna Troi's "other creatures
	// you control enter with an additional +1/+1 counter"). The runner's
	// placement re-runs it on every permanent placed after the replacer, so a
	// placed permanent carries one counter more than the live game's, and the
	// extra depends on placement order; setup has no field to subtract it.
	reasonEntryCounterOther = "setup_entry_counter_other"
	// reasonPrepareEntry is a placed battlefield card whose own entry
	// replacement grants Prepared (K:ETBReplacement, or a printed
	// R:Event$ Moved body). The runner's placement re-runs the replacement,
	// which mints the CR 722.3c exile copy; the copy enters the setup
	// checkpoint's exile list, but the scenario schema has no prepared field
	// that could remove it, and the live game may have cast it long ago, so the
	// replay would show one exile card too many.
	reasonPrepareEntry = "setup_entry_prepared"
	// reasonSagaEntry is a placed battlefield Saga. The runner's placement
	// enters it with its lore counter and fires chapter I (and the counters
	// field adds the rest as CounterChange events, each firing its chapter), so
	// the chapter abilities the live game resolved turns ago run again in the
	// setup drive, answered by the harness's fallback (a "destroy up to one
	// target creature" chapter killed the opponent's creature before step 0).
	// No scenario field says a chapter already resolved.
	reasonSagaEntry = "setup_entry_saga"
	// reasonSetupAnswer is a window whose setup drive poses a decision no
	// scenario answer scripts. SetupAnswers carries only the as-enters choices
	// capture could read from state (a permanent's chosen colour); every other
	// ask is missing, so the runner takes its fallback. The fallback's pick is
	// not the cut's and can change state the self-check cannot see (a card
	// discarded from a hidden hand, a library order), so the window is
	// unplaceable rather than merely divergent. The reason names the decision
	// kind, e.g. setup_answer:mode.
	reasonSetupAnswer = "setup_answer:"
	// reasonPTTurnHistory is a battlefield permanent whose P/T is a static
	// function of what happened earlier THIS turn (Duelist of the Mind: power
	// is the cards its controller drew this turn). A window starts after the
	// turn's draw step, but the placed game has an empty turn history, so the
	// placed P/T differs from the live one and setup has no field for the
	// history.
	reasonPTTurnHistory = "pt_turn_history"
	// The setup drive expresses state as events: a by-name tapped placement is
	// a Tap, a counters field a CounterChange, a life field a LifeChange. A
	// placed permanent that triggers on one of those events would resolve a
	// trigger the scenario schema cannot say already resolved (CR 603.2), so
	// the window is refused.
	reasonSetupTaps       = "setup_trigger:taps"
	reasonSetupCounterAdd = "setup_trigger:counter_added"
	reasonSetupLifeGained = "setup_trigger:life_gained"
	reasonSetupLifeLost   = "setup_trigger:life_lost"
	// reasonSetupCounterRepl is a placed battlefield card with an AddCounter
	// replacement (Hardened Scales, Michelangelo, Weirdness to 11). The
	// counters field is a CounterChange event, so the placement re-runs the
	// replacement and the permanent gets a different net than the cut.
	reasonSetupCounterRepl = "setup_replacement:counter_added"
)

// reasonFieldPlayerCounters is the refusal of a cut whose seat holds player
// counters while the pinned gorge oracleSeat has no player_counters setup
// field. The gate's playerTable classifies the field as placeable, so the
// refusal is added here, once, from the pin's capability; it disappears with
// the pin bump that adds the field.
const reasonFieldPlayerCounters = "field:Player.Counters"

// pinPlayerCounters is pincap.PlayerCounters, a var so a test can exercise the
// placement branch before the pin carries the runner's player_counters field.
var pinPlayerCounters = pincap.PlayerCounters

// cut is the live engine at one cut point, reduced to what a window needs: the
// setup it can be rebuilt from, or why it cannot be. Seats are the live game's.
type cut struct {
	Entry  int   // tape index of the cut's decision
	Turn   int32 // the live turn
	Active int
	// Commander is true in a Commander game: the harness plays one exactly
	// when game.Play set Config.Commanders, so a seat's Commanders tells.
	Commander bool
	// Setup is one seat's placed state; Attach the attach prelude steps.
	Setup  [2]oraclegen.Seat
	Attach []oraclegen.Step
	// Answers is, per seat, the as-enters asks the seat's placements pose
	// during the setup drive, in placement order.
	Answers [2][]oraclegen.Answer
	// Refs is the runner ref (live seat numbering) setup will bind each placed
	// card to; MaxID is the highest object id alive at the cut.
	Refs  map[state.ObjID]string
	MaxID state.ObjID
	// Reasons is why the window is refused, sorted and without duplicates; empty
	// when it is expressible.
	Reasons []string
}

// capture reads the live engine at a cut point. It only reads. xmage says the
// window is replayed by XMage, which narrows the placeable setup tokens.
func capture(e *rules.Engine, entry int, xmage bool) *cut {
	return captureAt(e, entry, xmage, false)
}

// captureStart is capture at the cut of a scripted start (lower.Span): the
// game's genesis applied a rules.StartSpec, so the cut is turn 1's first main
// and the scenario replay applies the same position at genesis and drives
// through the same upkeep. A placed permanent's beginning-of-upkeep/draw
// trigger therefore fires at the step the live game fired it, not a second
// time as a later window's setup drive would; Span scripts an optional one off
// (its state is already in the cut), so captureStart skips reasonPhaseTrig for
// an optional trigger only. A mandatory trigger still refuses: the setup drive
// would resolve it and the scenario has no answer saying it already resolved.
func captureStart(e *rules.Engine, entry int, xmage bool) *cut {
	return captureAt(e, entry, xmage, true)
}

// startPhaseTriggerDeclines is one setup answer per battlefield card, back
// face or setup token whose optional upkeep or draw trigger will ask again
// while the scenario drive reaches turn 1's first main. The cut already carries
// the state that trigger left (a verse counter, a life payment), so the answer
// declines it: accepting would apply the effect twice. A start whose probe asks
// a fallback yes/no and has no such card gets no answer, so Span keeps its
// refusal.
func startPhaseTriggerDeclines(reg *cards.Registry, it oraclegen.Item) []oraclegen.Answer {
	seats := make([]string, 0, len(it.Scenario.Setup))
	for k := range it.Scenario.Setup {
		seats = append(seats, k)
	}
	sort.Strings(seats)
	var out []oraclegen.Answer
	for _, k := range seats {
		seat := it.Scenario.Setup[k]
		for _, name := range seat.Battlefield {
			c, ok := reg.Lookup(name)
			if !ok || len(c.Faces) == 0 {
				continue
			}
			face := c.Faces[0]
			if slices.Contains(seat.BackFace, name) && len(c.Faces) > 1 {
				face = c.Faces[1]
			}
			if n := optionalPhaseTriggerCount(face); n > 0 {
				for range n {
					out = append(out, oraclegen.Answer{Kind: string(decision.KTriggerOptional), Pick: []string{"no"}})
				}
			}
		}
		// A setup token is a battlefield permanent too: its optional upkeep
		// trigger asks on the drive exactly like a card's.
		for _, tok := range seat.Tokens {
			if c, ok := reg.Token(tok.Script); ok && c != nil && len(c.Faces) > 0 {
				for range optionalPhaseTriggerCount(c.Faces[0]) {
					out = append(out, oraclegen.Answer{Kind: string(decision.KTriggerOptional), Pick: []string{"no"}})
				}
			}
		}
	}
	return out
}

// isUpkeepOrDrawPhase is a Phase trigger that fires at the beginning of an
// upkeep or draw step.
func isUpkeepOrDrawPhase(t *cards.Trigger) bool {
	if !strings.EqualFold(t.Mode, "Phase") {
		return false
	}
	ph := strings.ToLower(t.Params["Phase"])
	return strings.Contains(ph, "upkeep") || strings.Contains(ph, "draw")
}

// optionalPhaseAsk is a Phase trigger its controller may decline with a plain
// trigger_optional yes/no. The may-ask can live on the Execute SVar's body
// instead of the trigger line (gorge's mayAskFromSVar); either spelling asks
// the same setup-drive question.
func optionalPhaseAsk(f *cards.Face, t *cards.Trigger) bool {
	if t.Params["OptionalDecider"] != "" || strings.EqualFold(t.Params["Optional"], "True") {
		return true
	}
	body := f.SVars[t.Params["Execute"]]
	return body != "" && (strings.Contains(body, "Optional$") || strings.Contains(body, "OptionalDecider$"))
}

// optionalPhaseTriggerCount is how many upkeep or draw Phase triggers of f its
// controller may decline: only such triggers pose a setup-drive yes/no the
// start can answer, and each asks its own question.
func optionalPhaseTriggerCount(f *cards.Face) int {
	if f == nil {
		return 0
	}
	n := 0
	for i := range f.Triggers {
		if t := &f.Triggers[i]; isUpkeepOrDrawPhase(t) && optionalPhaseAsk(f, t) {
			n++
		}
	}
	return n
}

// mandatoryPhaseTriggered is true when an upkeep or draw Phase trigger is not
// optional. Span can script an optional ask off, so only a mandatory one keeps
// the start refused.
func mandatoryPhaseTriggered(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for i := range f.Triggers {
		if t := &f.Triggers[i]; isUpkeepOrDrawPhase(t) && !optionalPhaseAsk(f, t) {
			return true
		}
	}
	return false
}

// MandatoryUpkeepTriggers is f's beginning-of-upkeep or draw Phase triggers
// that are not optional: the list mandatoryPhaseTriggered asks about, and
// the shape captureStart refuses a battlefield permanent for (the setup
// drive would resolve one and the scenario has no answer saying it already
// resolved). A generator deciding whether its own drive takes the permanent
// off the battlefield before the cut needs the gate's list, not its own
// reading of optionality.
func MandatoryUpkeepTriggers(f *cards.Face) []*cards.Trigger {
	if f == nil {
		return nil
	}
	var out []*cards.Trigger
	for i := range f.Triggers {
		if t := &f.Triggers[i]; isUpkeepOrDrawPhase(t) && !optionalPhaseAsk(f, t) {
			out = append(out, t)
		}
	}
	return out
}

// TriggerRemovesSelf is whether a trigger's compiled Execute takes its own
// card off the battlefield: an echo Sacrifice naming no other kind, target
// or bearer (the population gorge's own UnlessCost tests pin as a Sacrifice
// with no SacValid$ or Defined$), an untargeted Destroy of the card itself,
// or a ChangeZone moving only the card itself off the battlefield. A trigger
// that reads targets is no self-removal: The Abyss ("destroy all nonartifact
// creatures you don't control", ValidTgts$ ControlledBy the triggered
// player) and Crumbling Ashes ("destroy target creature with a +1/+1 counter
// on it") are DB$ Destroy with ValidTgts$ and leave the card standing, and a
// ChangeZone reading ChangeType$ or DefinedPlayer$ does not move the card
// itself. An uncompilable Execute is no removal: the refusal stands.
func TriggerRemovesSelf(t *cards.Trigger) bool {
	e := t.Effect
	if e == nil {
		return false
	}
	selfish := func() bool {
		d := e.Params["Defined"]
		return d == "" || d == "Self" || d == "TriggeredCard"
	}
	targets := e.Params["ValidTgts"] != "" || e.Params["Target"] != ""
	switch e.API {
	case "Sacrifice":
		return !targets && e.Params["SacValid"] == "" && selfish()
	case "Destroy":
		return !targets && selfish()
	case "ChangeZone":
		return !targets && selfish() && e.Params["ChangeType"] == "" && e.Params["DefinedPlayer"] == "" &&
			e.Params["Origin"] == "Battlefield" && e.Params["Destination"] != "Battlefield"
	}
	return false
}

// StartCardRefusal is why a scripted start cannot place c's face f on the
// battlefield, or "": a mandatory beginning-of-upkeep or draw trigger (the
// setup drive would resolve it before the cut, and a start has no answer
// saying it already resolved), or a card naming the
// startedTheTurnUntapped filter word (the turn-change fold stamps that field
// on every untapped battlefield object, and the gate refuses it once any card
// in the game names the word, which every start's untapped lands then are).
// It is the start gate's own read, so a cost planner can keep its named
// refusal instead of emitting a start the lowering refuses. It is
// deliberately conservative about the first half: an echo whose UnlessCost
// the drive can pay (Earthlink's {2}, Thelon's Chant's {G}) keeps the card
// standing at the cut, where captureAt refuses it, and only the finished
// start knows whether its drive can pay — so the generator sweeps the
// finished start and grants that exception there
// (scen.startGateRefusal/upkeepRemovalExcused), never here on effect shape
// alone.
func StartCardRefusal(c *cards.Card, f *cards.Face) string {
	if f == nil {
		return "start:no-face"
	}
	if mandatoryPhaseTriggered(f) {
		return "start:upkeep-trigger"
	}
	if StartTurnStampMention(c) {
		return "start:started-turn-untapped"
	}
	return ""
}

// StartTurnStampMention is the mention half of StartCardRefusal: true when c
// names the startedTheTurnUntapped filter word. captureAt reads that word
// across the whole game (walker.gameMentions scans every object, in every
// zone), and the turn-change fold stamps the field on every untapped
// battlefield object once any card names it, so a card the start places in a
// hand, library or graveyard stamps the live field exactly as one on the
// battlefield does. The generator reads it for the start's off-battlefield
// placements, whose mandatory-trigger half StartCardRefusal would over-refuse
// (captureAt refuses a phase trigger only for a battlefield object), and for
// a battlefield card whose upkeep half the finished start excuses
// (scen.upkeepRemovalExcused).
func StartTurnStampMention(c *cards.Card) bool {
	return cardMentions(c, "startedTheTurnUntapped")
}

// captureAt is capture with the scripted-start distinction: start is true when
// the cut is the genesis position of a rules.StartSpec.
func captureAt(e *rules.Engine, entry int, xmage, start bool) *cut {
	g := e.G
	c := &cut{Entry: entry, Turn: g.Turn, Active: int(g.Active), Refs: map[state.ObjID]string{}, MaxID: state.ObjID(len(g.Objs))}
	w := &walker{g: g, why: map[string]bool{}, auras: map[state.ObjID]bool{}, xmage: xmage, start: start}
	w.struct_(reflect.ValueOf(g).Elem(), "Game", gameTable, nil)
	for i := range g.Players {
		w.seat = i
		w.struct_(reflect.ValueOf(&g.Players[i]).Elem(), "Player", playerTable, nil)
		if len(g.Players[i].Commanders) > 0 {
			c.Commander = true
		}
	}
	for i := range g.Objs {
		o := &g.Objs[i]
		// A resolved ability is parked off the stack as an object that is no
		// card; one still on the stack is refused with the stack itself.
		if o.Zone == state.ZCeased || o.Ability != nil && o.Zone != state.ZStack {
			continue
		}
		switch o.Zone {
		case state.ZBattlefield, state.ZHand, state.ZLibrary, state.ZGraveyard, state.ZExile:
		case state.ZCommand:
			// A commander card in its command zone is the setup command list.
			if o.Card == nil || g.CommanderOf(o.ID) != o.ID {
				w.why[reasonZone+o.Zone.String()] = true
			}
		default:
			w.why[reasonZone+o.Zone.String()] = true
		}
		w.struct_(reflect.ValueOf(o).Elem(), "Object", objectTable, o)
		if o.Zone == state.ZBattlefield && o.IsToken {
			w.checkToken(o)
		}
		if o.Zone == state.ZBattlefield && phaseTriggered(o) && (!start || mandatoryPhaseTriggered(o.Face())) {
			w.why[reasonPhaseTrig] = true
		}
		if o.Zone == state.ZBattlefield && slices.Contains(e.Derived(o.ID).Types, "Aura") {
			w.auras[o.ID] = true
			if o.AttachedTo == 0 && !o.HasAttachedPlayer {
				w.why[reasonAuraUnattached] = true
			}
		}
		if o.Zone == state.ZHand {
			if f := o.Face(); f != nil {
				if _, ok := f.KeywordParam("MayEffectFromOpeningHand"); ok {
					w.why[reasonOpeningHand] = true
				}
			}
		}
	}
	// e.RegisteredContinuous is AddContinuous's registry, not the printed
	// static scanner's list: every entry is a continuous effect a resolution
	// created (a pump, an Effect-API grant, a copy effect, a StaticEffect$
	// rider such as Enduring Innocence returning as a non-creature
	// enchantment). Setup starts from the card faces and replays no
	// resolution, so no entry here is state setup can re-create. Refusing the
	// whole registry (not a duration subset -- an undurationed StaticEffect$
	// grant is exactly the sibling the subset missed) keeps the next such
	// effect from shipping a printed face over a changed live permanent.
	if len(e.RegisteredContinuous()) > 0 {
		w.why[reasonContinuous] = true
	}
	// A seat's counters are placeable only when the pinned runner's setup has
	// the player_counters field. Keep the pre-pin refusal until then: a
	// lowered item that carried the field would fail gorge's strict decoder,
	// and one that silently dropped it would not reproduce the cut.
	if !pinPlayerCounters() {
		for i := range g.Players {
			for _, pc := range g.Players[i].Counters {
				if pc.N != 0 {
					w.why[reasonFieldPlayerCounters] = true
					break
				}
			}
			if w.why[reasonFieldPlayerCounters] {
				break
			}
		}
	}
	if len(w.why) == 0 {
		w.place(c)
	}
	if len(w.why) == 0 {
		w.setupTriggers(c)
	}
	c.Reasons = sortedKeys(w.why)
	return c
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// phaseTriggered is true for a permanent with a beginning-of-upkeep or
// draw-step trigger: the setup's drive from genesis to the cut passes through
// the upkeep and would fire it a second time.
func phaseTriggered(o *state.Object) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	return PhaseTriggeredFace(f)
}

// PhaseTriggeredFace is phaseTriggered for a card face: a Phase trigger in the
// upkeep or draw step. It is the one predicate the start generator and the
// lowering gate both read, so an enabler the generator places and a permanent
// the gate refuses cannot disagree on what "incidental upkeep trigger" means.
func PhaseTriggeredFace(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for i := range f.Triggers {
		if isUpkeepOrDrawPhase(&f.Triggers[i]) {
			return true
		}
	}
	return false
}

// startLife is each seat's opening life in the cut's game: what a setup life
// at it leaves unplaced as a LifeChange.
func startLife(c *cut) int32 {
	if c.Commander {
		return game.CommanderStartingLife
	}
	return 20
}

// setupTriggers refuses a window whose setup drive would resolve a trigger or
// replacement of a placed permanent: the drive expresses setup state as real
// events (a by-name Tap, a CounterChange for the counters field, a LifeChange
// for the life field, CR 603.2/614.1c), and the scenario schema cannot say that
// trigger/replacement already resolved. It runs after place, on an otherwise
// expressible cut, and matches the engine's own TriggerMode vocabulary plus the
// AddCounter replacement event so a new spelling cannot slip past.
func (w *walker) setupTriggers(c *cut) {
	var tapped, counters, gained, lost bool
	for s := range c.Setup {
		if len(c.Setup[s].Tapped) > 0 {
			tapped = true
		}
		if len(c.Setup[s].Counters) > 0 {
			counters = true
		}
		// Player counters are PlayerCounterChange events in the setup drive,
		// not CounterChange: a "whenever you get a counter" trigger still
		// fires and the schema cannot say it already resolved.
		if len(c.Setup[s].PlayerCounters) > 0 {
			counters = true
		}
		// A placed token's counters are CounterChange events too
		// (engine/rules/oracle_setup_tokens.go), so they re-run a
		// battlefield AddCounter replacement or counter trigger exactly as
		// a card's do.
		for _, tk := range c.Setup[s].Tokens {
			if len(tk.Counters) > 0 {
				counters = true
			}
		}
		if life := c.Setup[s].Life; life != nil {
			if *life > startLife(c) {
				gained = true
			}
			if *life < startLife(c) {
				lost = true
			}
		}
	}
	if !tapped && !counters && !gained && !lost {
		return
	}
	grants := w.staticTriggerGrants()
	for i := range w.g.Objs {
		o := &w.g.Objs[i]
		// A resolved ability parked off the stack is not a placed permanent
		// (capture skips it before the gate sees it).
		if o.Zone != state.ZBattlefield || o.Ability != nil {
			continue
		}
		f := o.Face()
		if f != nil && counters && counterAddReplacement(f) {
			w.why[reasonSetupCounterRepl] = true
		}
		check := func(t *cards.Trigger) {
			switch t.ModeKind() {
			case cards.TriggerTaps, cards.TriggerTapAll:
				if tapped {
					w.why[reasonSetupTaps] = true
				}
			case cards.TriggerCounterAdded, cards.TriggerCounterAddedOnce,
				cards.TriggerCounterAddedAll, cards.TriggerCounterTypeAddedAll:
				if counters {
					w.why[reasonSetupCounterAdd] = true
				}
			case cards.TriggerLifeGained:
				if gained {
					w.why[reasonSetupLifeGained] = true
				}
			case cards.TriggerLifeLost, cards.TriggerLifeLostAll:
				if lost {
					w.why[reasonSetupLifeLost] = true
				}
			}
		}
		if f != nil {
			for j := range f.Triggers {
				check(&f.Triggers[j])
			}
		}
		// A static's AddTrigger$ grant is not on the affected object's face:
		// it lives on the granting permanent, so the printed-trigger walk above
		// misses it. Walk every battlefield static and apply the grant to the
		// permanents its Affected$ names, through the same engine matcher the
		// layer scan uses, then the same mode switch as a printed trigger.
		for j := range grants {
			if grants[j].affects(w, o) {
				check(&grants[j].trigger)
			}
		}
	}
}

// staticTriggerGrant is one trigger a battlefield static grants through
// AddTrigger$: the SVar-parsed trigger body, the Affected$ spec it is granted
// to, and the granting object. The spec's "you" is the granting permanent's
// controller, exactly as engine/rules/layers.go's static scan reads it.
type staticTriggerGrant struct {
	trigger  cards.Trigger
	affected string
	source   state.ObjID
	you      state.PlayerID
}

// staticTriggerGrants resolves every battlefield static's AddTrigger$ bodies,
// including the "A & B" multi-name form cards.SplitGrantNames and the engine's
// own layer scan read. The grant is only collected; staticTriggerGrant.affects
// decides which permanents receive it. A body that fails to parse grants
// nothing, the same fail-closed read the layer scan takes.
func (w *walker) staticTriggerGrants() []staticTriggerGrant {
	var out []staticTriggerGrant
	for i := range w.g.Objs {
		src := &w.g.Objs[i]
		if src.Zone != state.ZBattlefield || src.Ability != nil {
			continue
		}
		f := src.Face()
		if f == nil {
			continue
		}
		for si := range f.Statics {
			st := &f.Statics[si]
			if !strings.EqualFold(strings.TrimSpace(st.Mode), "Continuous") {
				continue
			}
			raw := strings.TrimSpace(st.ParamStr(cards.PKAddTrigger))
			if raw == "" {
				continue
			}
			affected := strings.TrimSpace(st.ParamStr(cards.PKAffected))
			if affected == "" {
				affected = "Card.Self"
			}
			for _, name := range cards.SplitGrantNames(raw) {
				if t, ok := cards.ParseTriggerLine(f.SVars[name]); ok {
					out = append(out, staticTriggerGrant{
						trigger: t, affected: affected, source: src.ID, you: src.Controller,
					})
				}
			}
		}
	}
	return out
}

// affects reports whether the granting static's Affected$ spec reaches o.
func (g staticTriggerGrant) affects(w *walker, o *state.Object) bool {
	return effects.MatchesSpecFrom(w.g, g.affected, o.ID, g.you, g.source)
}

// walker applies the field tables to the live state.
type walker struct {
	g    *state.Game
	why  map[string]bool
	seat int // the seat Player fields are being read for
	// xmage says XMage replays the window, so a setup token it cannot place is
	// refused; a Forge-only run places any token by its script stem.
	xmage bool
	// start says this cut is a scripted start (lower.Span): its setup re-runs
	// the placements and focus play that produced the live state, so a state
	// the start itself derives is not an unplaceable field. Game.DayNight is
	// the one such field today (the start's own entry replacements and
	// daybound/nightbound placements plus the turn-1 untap check re-derive it).
	start bool
	// mention memoises gameMentions per word for this capture.
	mention map[string]bool
	// exilerRead memoises whether any card of the game reads HasExiledBy
	// (ExiledByYou, Exiler); exilerKnown says it was computed.
	exilerRead, exilerKnown bool
	// entryRead memoises whether any card of the game reads this turn's zone
	// entries (turnEntryReaders); entryKnown says it was computed.
	entryRead, entryKnown bool
	// stems is the reverse of Game.Tokens, built on first use (tokenScript).
	stems map[*cards.Card]string
	// auras is the battlefield Auras of the capture: place lists these in the
	// runner's attached field instead of an attach prelude step.
	auras map[state.ObjID]bool
}

// turnEntryReaders are the script words through which gorge reads Game.Entered
// and the EnteredThisTurn of an object off the battlefield (pinned gorge,
// engine/effects):
//   - ThisTurnEntered: Count$ThisTurnEntered_* (count_body_heads.go) and the
//     words ThisTurnEntered, ThisTurnEnteredFrom_<Zone> (filter_word.go);
//   - Morbid: the dotted Morbid head (count_body_heads.go);
//   - LeftGraveyardThisTurn, LeftBattlefieldThisTurn: count_cov3.go heads;
//   - Landfall, Void: count_cov3.go condition branches;
//   - SacrificedThisTurn, SacrificedPermanentTypes: sacrificedThisTurn and
//     sacrificedPermanentTypes (count_cov3.go; the latter is the
//     SacrificedPermanentTypesThisTurn head, which does not contain the former);
//   - Descended, descended: descendedThisTurn (Count$YouDescendedThisTurn,
//     the descended player qualifier; count_player.go, player_filter.go).
//
// targetedPermanentLKI (conditions.go) also reads an off-battlefield
// EnteredThisTurn, but only for a target that left the battlefield this turn,
// which a card that died before the cut cannot be after it.
var turnEntryReaders = []string{
	"ThisTurnEntered", "Morbid", "LeftGraveyardThisTurn", "LeftBattlefieldThisTurn",
	"Landfall", "Void", "SacrificedThisTurn", "SacrificedPermanentTypes",
	"Descended", "descended",
}

// entryReader is true when any card of the game, in any zone, reads this
// turn's zone entries.
func (w *walker) entryReader() bool {
	if !w.entryKnown {
		w.entryRead, w.entryKnown = w.gameMentions(turnEntryReaders...), true
	}
	return w.entryRead
}

// struct_ visits every exported field of v (a struct of the named type) and adds
// field:<Type>.<Name> for a refuse field that is live. An unclassified field is
// itself a refusal, so a gorge bump never silently widens a window.
func (w *walker) struct_(v reflect.Value, typ string, table map[string]entry, o *state.Object) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		en, ok := table[f.Name]
		if !ok {
			w.why[fmt.Sprintf("field:%s.%s", typ, f.Name)] = true
			continue
		}
		if en.Class != refuse || !w.live(typ, f.Name, v.Field(i), o) {
			continue
		}
		w.why[fmt.Sprintf("field:%s.%s", typ, f.Name)] = true
	}
}

// live is true when a refuse field carries state. The default is "not at its
// zero value"; a turn stamp is live only when it names the cut's turn, and the
// zone entries of the draw step (library to hand) and of card-less abilities,
// the this-turn resolution tally and the runtime SVars no card in the game
// names are not history setup could have changed or a card could read. Zone
// entries, and the entry flag of an object off the battlefield, are live only
// when a card in the game reads them (turnEntryReaders). An
// exile link is live only when a card can read it; a self-exile (warp) is
// refused as exile_self.
func (w *walker) live(typ, name string, v reflect.Value, o *state.Object) bool {
	switch {
	case typ == "Game" && name == "DayNight":
		// A scripted start re-runs the entry replacements, daybound/nightbound
		// placements and the turn-1 untap check that produced the live
		// designation, so the designation is derived state, not a field setup
		// must restate (CR 731.1). A window cut mid-game has no such
		// derivation and keeps the refusal. IsZero, not Int: the field is an
		// unsigned enum at this pin.
		return !v.IsZero() && !w.start
	case typ == "Object" && name == "ChosenColor":
		// The setup drive answers a placed permanent's as-enters colour ask
		// from the battlefield loop, so only a battlefield object has a
		// placement to answer. The engine clears the field when a permanent
		// leaves the battlefield, which makes the other zones latent rather
		// than impossible; they still have no answer and must refuse.
		return !v.IsZero() && o.Zone != state.ZBattlefield
	case typ == "Player" && name == "LandsPlayed":
		return v.Int() > 0 && w.seat == int(w.g.Active)
	case typ == "Player" && name == "LandsPlayedOffHand":
		return v.Len() > 0 && w.seat == int(w.g.Active)
	case typ == "Object" && stackOnly[name]:
		return o.Zone == state.ZStack && v.Len() > 0
	case typ == "Object" && name == "ActivatedThisTurn":
		return v.Int() > 0 && hasActivationLimit(o)
	case typ == "Object" && name == "TurnedFaceUpThisTurn":
		// The TurnFaceUp fold sets it and the TurnChange and zone-change folds
		// clear it (CR 400.7), so it only records a turn-up this turn; setup
		// cannot place that past and its only reader is the
		// turnedFaceUpThisTurn filter word (Kaust, Eyes of the Glade).
		return v.Bool() && o.Zone == state.ZBattlefield && w.gameMentions("turnedFaceUpThisTurn")
	case typ == "Object" && name == "RegeneratedThisTurn":
		// The TurnChange and zone-change folds reset it (CR 400.7), so it only
		// carries this turn's regeneration count (CR 701.19); its only reader
		// is the Count$RegeneratedThisTurn head (Spiny Starfish).
		return v.Int() > 0 && w.gameMentions("RegeneratedThisTurn")
	case typ == "Object" && turnStamps[name]:
		return v.Int() == int64(w.g.Turn)
	case typ == "Object" && name == "CastFlags":
		// Only the cast-provenance bits a battlefield reader consults are live:
		// ImpendingDormant (CR 702.176a) strips Creature while a TIME counter
		// remains, and PrototypeCharacteristics (CR 718.3a) replaces the printed
		// P/T. Setup places the permanent, not the cast, so it can restate
		// neither. Every other bit is read at entry or on the stack.
		if o.Zone != state.ZBattlefield {
			return false
		}
		if o.ImpendingDormant() {
			return true
		}
		_, _, _, prototype := o.PrototypeCharacteristics()
		return prototype
	case typ == "Object" && name == "EnteredThisTurn":
		if !v.Bool() || (o.Zone == state.ZHand && o.EnteredFrom == state.ZLibrary) {
			return false
		}
		// A battlefield permanent's flag is read by the engine with no script
		// word (powerUpReduceCost); off the battlefield only the readers see it.
		return o.Zone == state.ZBattlefield || w.entryReader()
	case typ == "Game" && name == "Entered":
		if !w.entryReader() {
			return false
		}
		for _, z := range w.g.Entered {
			if z.To == state.ZHand && z.From == state.ZLibrary {
				continue
			}
			// A card-less object is an ability that went on the stack; every
			// reader of Entered matches a card spec or a card fact.
			if o := w.g.Obj(z.Obj); o == nil || o.Card == nil {
				continue
			}
			return true
		}
		return false
	case typ == "Object" && name == "StartedTurnUntapped":
		// The TurnChange fold stamps it on every untapped battlefield object, so
		// it is set in nearly every mid-game cut; its only reader is the
		// startedTheTurnUntapped filter word.
		return v.Bool() && o.Zone == state.ZBattlefield && w.gameMentions("startedTheTurnUntapped")
	case typ == "Game" && name == "EnteredLastTurn":
		// One turn of history, read only through Count$LastTurnEntered_*
		// (Ephara, God of the Polis; count_body_heads.go evalEnteredHead).
		return v.Len() > 0 && w.gameMentions("LastTurnEntered")
	case typ == "Game" && name == "ResolvedThisTurn":
		return v.Len() > 0 && w.gameMentions("ResolvedThisTurn")
	case typ == "Game" && name == "MaxDistinctOnStackThisTurn":
		// The TurnChange fold clears it, so it only carries this turn's
		// high-water mark (Count$MaxDistinctOnStack, Geometric Weird); a game
		// with no card naming MaxDistinctOnStack does not depend on it.
		return v.Int() > 0 && w.gameMentions("MaxDistinctOnStack")
	case typ == "Player" && name == "SearchedLibraryThisTurn":
		// Set by any completed library search (CR 701.23), so it is live in
		// cuts after a search; its only reader is the Count$SearchedLibrary.<spec>
		// head (Archive Trap, count_history_heads.go), so a game with no card
		// naming SearchedLibrary does not depend on the watcher.
		return v.Bool() && w.gameMentions("SearchedLibrary")
	case typ == "Player" && name == "AttackedSeatsLastTurn":
		// The record of the seat's last completed turn is set by any attack, so
		// it is non-zero in most mid-game cuts; its only readers are the
		// attackedYouTheirLastTurn player property (O-Kagachi, Avenge,
		// Weathered Sentinels), so it is history no other card can see.
		return v.Uint() != 0 && w.gameMentions("attackedYouTheirLastTurn")
	case typ == "Object" && name == "AttackedPlayersThisTurn":
		// Set for each player a creature was declared attacking this turn
		// (CR 508.1b) and cleared at TurnChange and on leaving the battlefield;
		// setup cannot restate the history. Its one reader is the
		// attackedBySourceThisTurn player property (Port Razer), so a game with
		// no card naming it does not depend on the record.
		return v.Uint() != 0 && w.gameMentions("attackedBySourceThisTurn")
	case typ == "Object" && name == "DealtDamageToPlayersThisTurnHere":
		// The per-seat damage record (CR 120.1) is set by the DamageProvenance
		// fold and cleared at TurnChange and on leaving the battlefield; setup
		// cannot restate which player the object damaged. Its only reader is
		// Forge's dealtDamageToOppThisTurn filter word (corpus carriers
		// Dunerider Outlaw and Whirling Dervish), so a game with no card naming
		// that word does not depend on the record.
		return v.Uint() != 0 && w.gameMentions("dealtDamageToOppThisTurn")
	case typ == "Object" && name == "ExiledCards":
		if selfExile(o) {
			w.why[reasonSelfExile] = true
			return false
		}
		return v.Len() > 0 && cardMentions(o.Card, "ExiledWith")
	case typ == "Object" && name == "ExiledWith":
		if v.IsZero() {
			return false
		}
		if o.ExiledWith == o.ID {
			w.why[reasonSelfExile] = true
			return false
		}
		src := w.g.Obj(o.ExiledWith)
		return src != nil && cardMentions(src.Card, "ExiledWith")
	case typ == "Object" && name == "Haunting":
		// The haunt link joins an exiled card to the creature it haunts
		// (CR 702.55b). Setup places cards, not a link to a creature the
		// exiled card's own trigger found, so it is refused when live: only
		// the HauntedBy reader in a filter (effects/filter.go) can see it.
		return !v.IsZero() && w.gameMentions("HauntedBy")
	case typ == "Object" && name == "HasExiledBy":
		if !v.Bool() {
			return false
		}
		if selfExile(o) {
			w.why[reasonSelfExile] = true
			return false
		}
		if !w.exilerKnown {
			w.exilerRead, w.exilerKnown = w.gameMentions("ExiledByYou") || w.gameMentions("Exiler"), true
		}
		return w.exilerRead
	case typ == "Object" && name == "RuntimeSVars":
		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		for _, k := range keys {
			if w.gameMentions(k) {
				return true
			}
		}
		return false
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Map:
		return v.Len() > 0
	}
	return !v.IsZero()
}

// selfExile is true for a card in exile that exiled itself: it is its own
// exiler and is listed in its own ExiledCards.
func selfExile(o *state.Object) bool {
	return o.Zone == state.ZExile && (o.ExiledWith == o.ID || slices.Contains(o.ExiledCards, o.ID))
}

// frontName is the name setup lists a card under: its first face, the name the
// runner's dealt library card carries.
func frontName(o *state.Object) string {
	if o.Card == nil || len(o.Card.Faces) == 0 {
		return ""
	}
	return o.Card.Faces[0].Name
}

// entryTapReplacement reports whether the face's own battlefield-entry
// replacement taps it. The runner's setup placement keeps the ordinary
// battlefield move, so it re-runs the replacement (like the entry counter
// grant); an "enters tapped unless <cost>" body defaults to the decline when
// no answer serves it, and the permanent ends tapped whatever the cut says.
func entryTapReplacement(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for i := range f.Repls {
		r := &f.Repls[i]
		if r.Event != "Moved" {
			continue
		}
		if dest, ok := r.Params["Destination"]; ok && !strings.EqualFold(dest, "Battlefield") {
			continue
		}
		for s := r.With; s != nil; s = s.Sub {
			if s.API == "Tap" && strings.EqualFold(s.Params["ETB"], "True") {
				return true
			}
		}
	}
	return false
}

// movedCounterBodies resolves every Moved battlefield-entry replacement on f
// to the counters its ReplaceWith$ body (and SubAbility chain) puts on entry.
// The placement re-runs a Moved replacement (CR 614.1c), so a grant on the
// entering card itself is subtracted from the counters field by setupCounters.
// selfGrants are the unconditional, literal grants setup can subtract; other
// reports a body that puts counters on another object (or an unknown target),
// and restatable is false for a grant setup cannot restate (a condition on the
// entry, a dynamic amount/type, or a PutCounterAll). Either of the last two is
// a refusal: a wrong net silently ships the wrong permanent.
//
// The body comes from r.With, already resolved through ReplaceWith$ (both the
// K:etbCounter and K:ETBReplacement:Other keyword forms and printed
// R:Event$ Moved lines), and the walk follows SubAbility because a body can
// split its grants (Dust Animus: two +1/+1 then a lifelink counter).
func movedCounterBodies(f *cards.Face) (selfGrants []state.Counter, other, restatable bool) {
	restatable = true
	if f == nil {
		return nil, false, true
	}
	for i := range f.Repls {
		r := &f.Repls[i]
		if r.Event != "Moved" {
			continue
		}
		if dest, ok := r.Params["Destination"]; ok && !strings.EqualFold(dest, "Battlefield") {
			continue
		}
		vc := strings.TrimSpace(r.Params["ValidCard"])
		selfEntry := vc == "" || strings.Contains(strings.ToLower(vc), "self")
		exactSelf := vc == "" || strings.EqualFold(vc, "Card.Self")
		for s := r.With; s != nil; s = s.Sub {
			switch {
			case s.API == "PutCounterAll" || s.API == "PutCounterPlayer":
				if selfEntry {
					restatable = false
				} else {
					other = true
				}
				continue
			case s.API != "PutCounter" || !strings.EqualFold(strings.TrimSpace(s.Params["ETB"]), "True"):
				continue
			}
			if !selfEntry {
				// The carrier's replacement serves other entering permanents:
				// the placement re-runs it for every permanent placed after it,
				// and the extra counters depend on placement order.
				other = true
				continue
			}
			if !exactSelf || !counterParamsRestatable(r.Params) || !counterParamsRestatable(s.Params) {
				// A condition on the entry (Card.Self+kicked, CheckSVar$,
				// Revolt$, ...) or a dynamic amount/type: the live entry may
				// differ from the placement's, so no net can be known here.
				restatable = false
				continue
			}
			kind, n, ok := counterKindAmount(s.Params)
			if !ok {
				restatable = false
				continue
			}
			if !counterTargetsSelf(s.Params) {
				// The source's own entry put counters on another object: the
				// live object already carries them and the placement re-runs
				// the body against a different board.
				other = true
				continue
			}
			selfGrants = append(selfGrants, state.Counter{Kind: kind, N: n})
		}
	}
	return selfGrants, other, restatable
}

// counterParamsRestatable is false when a replacement body's parameters make
// its counter grant depend on the entry (a condition or an optional choice)
// rather than a literal amount setup can subtract.
func counterParamsRestatable(p map[string]string) bool {
	for k, v := range p {
		if strings.TrimSpace(v) == "" {
			continue
		}
		switch {
		case strings.HasPrefix(k, "Condition"),
			k == "CheckSVar", k == "SVarCompare", k == "Revolt", k == "Adamant",
			k == "IsPresent", k == "Optional":
			return false
		}
	}
	return true
}

// counterKindAmount reads a PutCounter body's literal kind and amount. A
// dynamic kind or amount (X, Count$, DiceResult, ...) is not restatable.
func counterKindAmount(p map[string]string) (string, int32, bool) {
	kind := strings.TrimSpace(p["CounterType"])
	if kind == "" || strings.Contains(kind, "$") || strings.Contains(kind, ".") {
		return "", 0, false
	}
	num := strings.TrimSpace(p["CounterNum"])
	if num == "" {
		return kind, 1, true
	}
	n, err := strconv.Atoi(num)
	if err != nil || n < 0 {
		return "", 0, false
	}
	return kind, int32(n), true
}

// counterTargetsSelf is true when a PutCounter body's Defined$ names the
// entering card: the default, Self, and ReplacedCard are all the object the
// Moved replacement is replacing.
func counterTargetsSelf(p map[string]string) bool {
	switch d := strings.ToLower(strings.TrimSpace(p["Defined"])); d {
	case "", "self", "replacedcard", "replacedcardlki":
		return true
	default:
		return false
	}
}

// entryCounterOtherReplacement reports whether the face carries a battlefield-
// entry replacement that puts counters on a permanent other than itself: a
// Moved replacement whose ValidCard names more than the card itself and whose
// body is a PutCounter on the ETB. The setup placement re-runs it for every
// permanent placed after the carrier (see reasonEntryCounterOther).
func entryCounterOtherReplacement(f *cards.Face) bool {
	_, other, _ := movedCounterBodies(f)
	return other
}

// ptTurnHistoryMarkers are the Count$ spellings whose value is read from the
// turn's history (draws, casts, entries, life change) and so is empty in a
// placed game. "Lost" is deliberately absent: it also matches
// Count$PlayerCountHasLost, a game-long count of eliminated players, not the
// turn history this refusal is about. The turn-scoped life-lost spellings
// (LifeYouLostThisTurn, LifeOppsLostThisTurn) all carry "ThisTurn".
var ptTurnHistoryMarkers = []string{"ThisTurn", "Drew", "Gained"}

// ptTurnHistory reports whether a static of the face sets or adds P/T from a
// Count$ SVar that reads the current turn's history, either as the P/T value
// itself or as the CheckSVar condition that gates the static (Ulna Alley
// Shopkeep's "as long as you gained life this turn"). The placed game starts
// with an empty history, so the live P/T cannot be re-derived (see
// reasonPTTurnHistory).
func ptTurnHistory(f *cards.Face) bool {
	if f == nil {
		return false
	}
	ptKeys := []string{"SetPower", "SetToughness", "AddPower", "AddToughness"}
	for i := range f.Statics {
		st := &f.Statics[i]
		hasPT := false
		for _, key := range ptKeys {
			if _, ok := st.Params[key]; ok {
				hasPT = true
				break
			}
		}
		if !hasPT {
			continue
		}
		for _, key := range ptKeys {
			if v, ok := st.Params[key]; ok && ptHistoryValue(f, v) {
				return true
			}
		}
		for _, key := range []string{"CheckSVar", "ConditionCheckSVar"} {
			if v, ok := st.Params[key]; ok && ptHistoryValue(f, v) {
				return true
			}
		}
	}
	return false
}

// ptHistoryValue resolves v through the face's SVar table (the Count$ body a
// static names) and reports whether it reads this turn's history.
func ptHistoryValue(f *cards.Face, v string) bool {
	if sv, ok := f.SVars[strings.TrimSpace(v)]; ok {
		v = sv
	}
	for _, m := range ptTurnHistoryMarkers {
		if strings.Contains(v, m) {
			return true
		}
	}
	return false
}

// entryPrepareReplacement reports whether the face's own battlefield-entry
// replacement grants it Prepared (CR 722.3a). The runner's setup placement
// keeps the ordinary battlefield move, so a replacement that grants Prepared
// re-runs and mints the CR 722.3c exile copy (grantPreparedCopy), an extra
// exile object the setup checkpoint sees and the cut may not have.
func entryPrepareReplacement(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for i := range f.Repls {
		r := &f.Repls[i]
		if r.Event != "Moved" {
			continue
		}
		if dest, ok := r.Params["Destination"]; ok && !strings.EqualFold(dest, "Battlefield") {
			continue
		}
		for s := r.With; s != nil; s = s.Sub {
			if s.API != "AlterAttribute" || strings.EqualFold(s.Params["Activate"], "False") {
				continue
			}
			for _, a := range strings.Split(s.Params["Attributes"], ",") {
				if strings.EqualFold(strings.TrimSpace(a), "Prepared") {
					return true
				}
			}
		}
	}
	return false
}

// place fills c.Setup, c.Refs and c.Attach from the live game, adding to w.why
// whatever setup cannot say.
func (w *walker) place(c *cut) {
	g := w.g
	var bf, toks [2][]*state.Object
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone == state.ZBattlefield {
			// A permanent that enters prepared mints an exile copy on the
			// runner's placement; no setup field can remove it.
			if entryPrepareReplacement(o.Face()) {
				w.why[reasonPrepareEntry] = true
			}
			if n, _ := cards.SagaChapters(o.Face()); n > 0 && o.Ability == nil {
				w.why[reasonSagaEntry] = true
			}
			if ptTurnHistory(o.Face()) {
				w.why[reasonPTTurnHistory] = true
			}
			if entryCounterOtherReplacement(o.Face()) {
				w.why[reasonEntryCounterOther] = true
			}
			if o.Owner != o.Controller {
				w.why["field:Object.Controller"] = true
				continue
			}
			if o.IsToken {
				// Tokens are listed in id order and bound after every card.
				toks[o.Owner] = append(toks[o.Owner], o)
				continue
			}
			bf[o.Owner] = append(bf[o.Owner], o)
		}
	}
	for s := range g.Players {
		p := state.PlayerID(s)
		sort.SliceStable(bf[s], func(a, b int) bool {
			if bf[s][a].Timestamp != bf[s][b].Timestamp {
				return bf[s][a].Timestamp < bf[s][b].Timestamp
			}
			return bf[s][a].ID < bf[s][b].ID
		})
		var seat oraclegen.Seat
		counts := map[string]int{}
		bind := func(o *state.Object) string {
			n := frontName(o)
			if n == "" {
				w.why[reasonCard] = true
			}
			counts[n]++
			ref := fmt.Sprintf("p%d:%s", s, n)
			if counts[n] > 1 {
				ref = fmt.Sprintf("%s#%d", ref, counts[n])
			}
			c.Refs[o.ID] = ref
			return n
		}
		names := func(ids []state.ObjID) []string {
			out := []string{}
			for _, id := range ids {
				o := g.Obj(id)
				if o == nil || o.Ability != nil {
					continue
				}
				if o.Owner != p {
					w.why["field:Object.Owner"] = true
					continue
				}
				out = append(out, bind(o))
			}
			return out
		}
		// The runner binds refs in setup order: battlefield, hand, graveyard,
		// exile, then the library top.
		back, counters := map[string]map[bool]bool{}, map[string]map[string]int32{}
		mixed := map[string]bool{}
		netOf := map[string]string{}
		// tapRefs are the seat-relative refs of the tapped placements; tapState
		// is, per name, the tapped states its placements carry.
		var tapRefs []string
		tapNames := map[string]string{}
		tapState := map[string]map[bool]bool{}
		for _, o := range bf[s] {
			n := bind(o)
			seat.Battlefield = append(seat.Battlefield, n)
			if s != c.Active && !o.Tapped && entryTapReplacement(o.Face()) {
				w.why[reasonEntryTap] = true
			}
			if o.ChosenColor != "" {
				c.Answers[s] = append(c.Answers[s], oraclegen.Answer{Kind: "choose", Pick: []string{o.ChosenColor}})
			}
			if tapState[n] == nil {
				tapState[n] = map[bool]bool{}
			}
			tapState[n][o.Tapped] = true
			if o.Tapped {
				ref := strings.TrimPrefix(c.Refs[o.ID], fmt.Sprintf("p%d:", s))
				tapRefs = append(tapRefs, ref)
				tapNames[ref] = n
			}
			if back[n] == nil {
				back[n] = map[bool]bool{}
			}
			if o.FaceIdx > 1 {
				w.why["field:Object.FaceIdx"] = true
			}
			back[n][o.FaceIdx == 1] = true
			net, restatable := setupCounters(o)
			if !restatable {
				w.why[reasonCountersEntry] = true
			}
			key := ""
			for _, k := range sortedMapKeys(net) {
				if net[k] < 0 {
					w.why[reasonCountersLow] = true
				}
				if net[k] > 0 {
					key += fmt.Sprintf("%s=%d;", k, net[k])
				}
			}
			if prev, seen := netOf[n]; seen && prev != key {
				mixed["counters"] = true
			}
			netOf[n] = key
			if key != "" && counters[n] == nil {
				counters[n] = map[string]int32{}
				for k, v := range net {
					if v > 0 {
						counters[n][k] = v
					}
				}
			}
		}
		for _, n := range seat.Battlefield {
			if len(back[n]) > 1 {
				mixed["back_face"] = true
			}
		}
		for _, k := range sortedKeys(mixed) {
			w.why[reasonByName+k] = true
		}
		// Turn 1's untap step untaps only the active seat (p0 once relabelled),
		// so its tapped permanents are taps after the untap, one object each.
		// Any other seat keeps what setup put on it, so the by-name field says
		// it when every placement of the name is tapped (the Forge fork reads
		// only that field); mixed placements of one name need the per-object
		// field.
		byName := map[string]bool{}
		for _, ref := range tapRefs {
			n := tapNames[ref]
			if s != c.Active && !tapState[n][false] {
				if !byName[n] {
					byName[n] = true
					seat.Tapped = append(seat.Tapped, n)
				}
				continue
			}
			seat.TappedAfterUntap = append(seat.TappedAfterUntap, ref)
		}
		seat.Hand = names(g.Zone(state.ZHand, p))
		seat.Graveyard = names(g.Zone(state.ZGraveyard, p))
		seat.Exile = names(g.Zone(state.ZExile, p))
		seat.Command = names(g.Zone(state.ZCommand, p))
		seat.LibraryTop = names(g.Zone(state.ZLibrary, p))
		// A commander outside the command zone is a Commanders placement
		// (CR 903.9); its tax count rides CommanderCasts by front name.
		for k, id := range g.Players[s].Commanders {
			o := g.Obj(id)
			if o == nil {
				w.why[reasonCard] = true
				continue
			}
			n := frontName(o)
			if c.Refs[id] == "" {
				// Not bound by any zone list above (a ceased object).
				w.why[reasonZone+o.Zone.String()] = true
				continue
			}
			if o.Zone != state.ZCommand {
				seat.Commanders = append(seat.Commanders, n)
			}
			if k < len(g.Players[s].CmdCasts) && g.Players[s].CmdCasts[k] > 0 {
				if seat.CommanderCasts == nil {
					seat.CommanderCasts = map[string]int32{}
				}
				seat.CommanderCasts[n] = g.Players[s].CmdCasts[k]
			}
		}
		for _, n := range dedupe(seat.Battlefield) {
			if back[n][true] && !back[n][false] {
				seat.BackFace = append(seat.BackFace, n)
			}
			if cs := counters[n]; len(cs) > 0 {
				if seat.Counters == nil {
					seat.Counters = map[string]map[string]int32{}
				}
				seat.Counters[n] = cs
			}
		}
		life := g.Players[s].Life
		seat.Life = &life
		seat.Speed = g.Players[s].Speed
		if pinPlayerCounters() {
			// The runner's player_counters is the COMPLETE count at the
			// setup checkpoint, so copy the cut's counters as read, a spent
			// kind's retained zero entry included (a follower whose setup game
			// produced the kind moves down to it). Sorted by kind: no map walk
			// reaches the item bytes.
			for _, pc := range g.Players[s].Counters {
				seat.PlayerCounters = append(seat.PlayerCounters,
					oraclegen.SetupPlayerCounter{Kind: strings.ToUpper(pc.Kind), N: pc.N})
			}
			sort.SliceStable(seat.PlayerCounters, func(a, b int) bool {
				return seat.PlayerCounters[a].Kind < seat.PlayerCounters[b].Kind
			})
		}
		c.Setup[s] = seat
	}
	w.placeTokens(c, toks)
	w.placeCommanderDamage(c)
	// An attachment names its bearer's ref, so both seats and their tokens are
	// bound first.
	for s := range g.Players {
		for _, o := range bf[s] {
			if o.AttachedTo == 0 {
				continue
			}
			b := g.Obj(o.AttachedTo)
			if b == nil || b.Zone != state.ZBattlefield || c.Refs[b.ID] == "" {
				w.why["field:Object.AttachedTo"] = true
				continue
			}
			if w.auras[o.ID] {
				// An Aura waits for no prelude: the XMage side would put it
				// into the graveyard at the first priority (CR 704.5m), and
				// the runner would leave it in the library with no bearer
				// placed yet (CR 303.4g). The runner places it attached,
				// last, from the seat-relative ref.
				c.Setup[s].Attached = append(c.Setup[s].Attached, oraclegen.SetupAttach{
					Card: strings.TrimPrefix(c.Refs[o.ID], fmt.Sprintf("p%d:", s)),
					To:   c.Refs[b.ID],
				})
				continue
			}
			c.Attach = append(c.Attach, oraclegen.Step{Op: "attach", Seat: s, Card: c.Refs[o.ID], AttachedTo: c.Refs[b.ID]})
		}
	}
}

// placeCommanderDamage sets each victim seat's CommanderDamage, keyed by the
// commander's full ref. It runs once every ref is bound. The damage index is
// gorge's dense commander order (foldCmdDamage, commanderDenseIndex): seat
// order, then each seat's Commanders order.
func (w *walker) placeCommanderDamage(c *cut) {
	g := w.g
	dense := 0
	for s := range g.Players {
		for _, id := range g.Players[s].Commanders {
			for v := range g.Players {
				if dense >= len(g.Players[v].CmdDamage) || g.Players[v].CmdDamage[dense] <= 0 {
					continue
				}
				if c.Refs[id] == "" {
					w.why[reasonCard] = true
					continue
				}
				if c.Setup[v].CommanderDamage == nil {
					c.Setup[v].CommanderDamage = map[string]int32{}
				}
				c.Setup[v].CommanderDamage[c.Refs[id]] = g.Players[v].CmdDamage[dense]
			}
			dense++
		}
	}
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func sortedMapKeys(m map[string]int32) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// hasInternalCounterMarker reports whether o carries a nonzero gorge internal
// counter marker (the regeneration "Shield", "Deathtouched", the
// cumulative-upkeep paid marker). These ride Object.Counters because the engine
// has no per-object status field, but they are not counters: the live digest
// drops them (game.counterMap), a live regeneration shield is a replacement
// effect (CR 701.19a) rather than state setup can place, and a placement that
// copied the marker would mint a real CR 122.1c shield counter the live game
// never had. Every lowering path that turns Object.Counters into scenario
// counters goes through this helper, so a marker the state helper learns next
// is covered here too. A zero amount is not state (a spent marker leaves its
// entry behind) and is ignored.
func hasInternalCounterMarker(o *state.Object) bool {
	for _, ct := range o.Counters {
		if ct.N != 0 && state.InternalCounterMarker(ct.Kind) {
			return true
		}
	}
	return false
}

// setupCounters is the counters setup must ADD to o's placement: what o carries
// less what a fresh copy of the card enters with (a planeswalker's printed
// loyalty), which the runner's counters field is added to. A kind the card
// carries fewer of than it enters with comes out negative, which setup cannot
// say. The second return is false when the entry grant is dynamic and so cannot
// be restated: the placement re-runs the grant (CR 614.1c), but how many
// counters it makes is known only at that entry. It is also false when o
// carries a nonzero internal counter marker: the marker is not a counter and a
// live regeneration shield is state setup cannot restate.
func setupCounters(o *state.Object) (map[string]int32, bool) {
	net := map[string]int32{}
	if hasInternalCounterMarker(o) {
		return net, false
	}
	for _, ct := range o.Counters {
		if state.InternalCounterMarker(ct.Kind) {
			continue // zero amount: nothing to add
		}
		net[ct.Kind] += ct.N
	}
	for _, gr := range events.EntryCounterGrants(&state.Object{Card: o.Card, FaceIdx: o.FaceIdx}, false) {
		net[gr.Kind] -= gr.Amount
	}
	// A Moved replacement (K:etbCounter, K:ETBReplacement:Other and the
	// printed R:Event$ Moved lines) is re-run by the setup placement, so its
	// self grants must be subtracted here exactly like the EntryCounterGrants
	// above. A body setup cannot restate (a condition on the entry, a dynamic
	// amount, a grant on another object) is refused instead of shipping a
	// wrong net: the live entry and the placement's entry can disagree.
	if f := o.Face(); f != nil {
		grants, _, restatable := movedCounterBodies(f)
		if !restatable {
			return net, false
		}
		for _, gr := range grants {
			net[gr.Kind] -= gr.N
		}
	}
	return net, true
}

// counterAddReplacement reports whether the face replaces the AddCounter
// event (Hardened Scales, Doubling Season, Michelangelo, Weirdness to 11). A
// battlefield permanent carrying one is re-run by the setup drive's
// CounterChange, so a placed counter field can be doubled or incremented.
func counterAddReplacement(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for i := range f.Repls {
		if f.Repls[i].Event == "AddCounter" {
			return true
		}
	}
	// A static grant (S:Mode$ Continuous | AddReplacementEffect$ Name) names
	// an SVar whose R: body is the replacement; the layer system registers it
	// as a derived replacement, so it is not in f.Repls.
	for i := range f.Statics {
		for _, name := range cards.SplitGrantNames(f.Statics[i].ParamStr(cards.PKAddReplacementEffect)) {
			if r, ok := cards.ParseReplacementLine(f.SVars[name]); ok && r.Event == "AddCounter" {
				return true
			}
		}
	}
	return false
}

// hasActivationLimit is true for a face with an ability gated by an
// ActivationLimit$ (the only reader of Object.ActivatedThisTurn).
func hasActivationLimit(o *state.Object) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	for _, sa := range f.Abilities {
		for k := range sa.Params {
			if strings.Contains(k, "ActivationLimit") {
				return true
			}
		}
	}
	return false
}
