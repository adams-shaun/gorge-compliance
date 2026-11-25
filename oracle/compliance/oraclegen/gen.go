// Package oraclegen is the shared core of the level-A oracle scenario
// generator: the scenario types, fixtures, gorge-side settling and the
// XMage answer scripting. Each template (play a land, cast and resolve,
// counter a spell) lives in its own file under oraclegen/templates with its
// own version; templates.Generate picks one per card (docs/design/2026-10-02-xmage-compliance-oracle-design.md
// section 6). Scenarios use
// the oracle-scenario schema of gorge's runner (scenarios/oracle), so gorge's runner and the XMage driver
// both replay them.
//
// The generator may run gorge to choose a fixture (which target to offer,
// how many resolves the stack needs). That does not bias the verdict: the
// expectation is XMage's snapshot, never gorge's.
package oraclegen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/rules"
)

// xValue is the X every generated X spell is cast with.
const xValue = 2

// Seat is one player's setup.
type Seat struct {
	Battlefield []string `json:"battlefield,omitempty"`
	Tapped      []string `json:"tapped,omitempty"`
	// TappedAfterUntap taps single battlefield permanents the moment the
	// setup drive enters the scenario turn's first main phase, after every
	// untap step (CR 502.3): the board at the cut when the recorded player
	// tapped them earlier in the turn. Each entry is a seat-relative
	// battlefield ref (the part of a runner ref after "pN:"): "Forest" is
	// the first placement of the name, "Grizzly Bears#2" the second. Unlike
	// Tapped it names ONE object, only on the battlefield. The tap stands in
	// for an unspecified earlier action, so it queues no trigger.
	TappedAfterUntap []string `json:"tapped_after_untap,omitempty"`
	// BackFace names battlefield cards setup places on their back face (face
	// index 1). It is emitted as a FlipFace event, so the placement replays
	// from the log like every other setup op. A name that is not also in
	// Battlefield is an error: only a permanent already on the battlefield can
	// be flipped.
	BackFace  []string `json:"back_face,omitempty"`
	Hand      []string `json:"hand,omitempty"`
	Graveyard []string `json:"graveyard,omitempty"`
	Exile     []string `json:"exile,omitempty"`
	// Command is the seat's cards in the command zone, which the runner
	// seats as the seat's commanders (Config.Commanders) in a
	// FormatCommander scenario; it binds their refs after Exile and before
	// LibraryTop (its placement order is battlefield, hand, graveyard,
	// library, exile, command, library_top). Only a FormatCommander scenario
	// may name one: in a constructed scenario the runner would move a
	// library card into the command zone with no commander status.
	Command []string `json:"command,omitempty"`
	// Commanders makes these placements of the seat (battlefield, hand,
	// graveyard, exile or library) its commanders, as if they had started in
	// the command zone (gorge's oracleSeat.Commanders, CR 903.9). A commander
	// in the command zone is named in Command instead.
	Commanders []string `json:"commanders,omitempty"`
	// CommanderCasts starts a commander's tax count (gorge's
	// oracleSeat.CommanderCasts, CR 903.8): front name -> n >= 1.
	CommanderCasts map[string]int32 `json:"commander_casts,omitempty"`
	// CommanderDamage starts this seat's commander damage (gorge's
	// oracleSeat.CommanderDamage, CR 903.10a): the commander's full ref
	// "pN:<name>", N its owner seat -> n.
	CommanderDamage map[string]int32 `json:"commander_damage,omitempty"`
	Library         []string         `json:"library,omitempty"`
	LibraryTop      []string         `json:"library_top,omitempty"`
	// Counters puts counters on this seat's battlefield cards at setup:
	// card name -> counter kind (LOYALTY, P1P1) -> how many, added to what
	// the card enters with. Like Tapped it names every placement of that
	// card. A planeswalker's loyalty headroom and a "creature with a +1/+1
	// counter" target fixture ride it.
	Counters map[string]map[string]int32 `json:"counters,omitempty"`
	// Tokens are the tokens the seat controls at setup, in creation order (the
	// runner's setup "tokens"): the runner mints them after the seat's card
	// placements, so a "pN:token:Name#k" ref binds as in the recorded game.
	// Placement fires no trigger and no CreateToken replacement.
	Tokens []SetupToken `json:"tokens,omitempty"`
	// Attached places listed battlefield Auras already attached to a bearer
	// (the runner's setup "attached", CR 303.4f): the runner places each
	// last, after every seat's cards and tokens, and fires no trigger for it.
	Attached []SetupAttach `json:"attached,omitempty"`
	// Speed is the seat's starting speed, 0..4 (the runner's setup field).
	Speed int32 `json:"speed,omitempty"`
	// Life is the seat's starting life total when set (the runner's and the
	// XMage driver's setup "life"); nil keeps the format's starting life. An
	// activation gated on "at least N life" (Ayli's CheckSVar$ X |
	// SVarCompare$ GEY over Count$YourLifeTotal) rides it.
	Life *int32 `json:"life,omitempty"`
	// PlayerCounters is the seat's complete counter set at setup (the runner's
	// and the XMage driver's setup "player_counters"): ENERGY for a PayEnergy
	// cost, POISON and EXPERIENCE for a card that reads them. A sorted slice,
	// never a map walk, so the item bytes are deterministic. The pinned gorge
	// rules.oracleSeat gains the field in the commit the PayEnergy starts need;
	// the lower gate keeps refusing Player.Counters until pincap sees it.
	PlayerCounters []SetupPlayerCounter `json:"player_counters,omitempty"`
}

// SetupPlayerCounter is one player counter a seat's setup holds: gorge's
// counter kind and amount. It is ordered by kind in PlayerCounters.
type SetupPlayerCounter struct {
	Kind string `json:"kind"`
	N    int32  `json:"n"`
}

// SetupAttach is one Aura a seat's setup places attached. Card is a
// seat-relative battlefield ref (the part of a runner ref after "pN:", as in
// TappedAfterUntap) and To the bearer's full ref, of either seat or a setup
// token.
type SetupAttach struct {
	Card string `json:"card"`
	To   string `json:"to"`
}

// SetupToken is one token a seat's setup places: the Forge token-script stem
// and the token's COMPLETE counter set (a placed token enters with none of its
// own, so unlike Seat.Counters this is not added on top of entry counters).
type SetupToken struct {
	Script   string           `json:"script"`
	Counters map[string]int32 `json:"counters,omitempty"`
}

// WithCounters returns s with n more counters of kind on its card name. The
// map is copied, so a fixture's seat is never mutated through a shared map.
func WithCounters(s Seat, name, kind string, n int32) Seat {
	s.Counters = cloneCounters(s.Counters)
	if s.Counters == nil {
		s.Counters = map[string]map[string]int32{}
	}
	if s.Counters[name] == nil {
		s.Counters[name] = map[string]int32{}
	}
	s.Counters[name][kind] += n
	return s
}

func cloneCounters(m map[string]map[string]int32) map[string]map[string]int32 {
	if m == nil {
		return nil
	}
	out := make(map[string]map[string]int32, len(m))
	for card, kinds := range m {
		k := make(map[string]int32, len(kinds))
		for kind, n := range kinds {
			k[kind] = n
		}
		out[card] = k
	}
	return out
}

// Step is one scenario step (a subset of the runner's op set).
type Step struct {
	Op      string   `json:"op"`
	Seat    int      `json:"seat"`
	Card    string   `json:"card,omitempty"`
	Mana    string   `json:"mana,omitempty"`
	Targets []string `json:"targets,omitempty"`
	// CastMode selects a non-default cast option of the cast step by its
	// decision.Option.Mode (the runner's cast_mode): "optionalcost" pays a
	// self-spell OptionalCost static, "bargained" elects a Bargain, a Kicker's
	// "kicked", etc. Empty selects the ordinary cast, and it is empty on every
	// level-A step, so level-A items are byte-identical.
	CastMode string `json:"cast_mode,omitempty"`
	// Attackers/Defender drive the attack op; Blocks the block op. They
	// carry a creature into combat so a "target attacking or blocking
	// creature" slot has a legal target.
	Attackers []string    `json:"attackers,omitempty"`
	Defender  string      `json:"defender,omitempty"`
	Blocks    [][2]string `json:"blocks,omitempty"`
	// Step/Decision are the pass_to op's stop conditions (a phase step name
	// or a pending decision kind).
	Step     string   `json:"step,omitempty"`
	Active   string   `json:"active,omitempty"`
	Decision string   `json:"decision,omitempty"`
	Answers  []Answer `json:"answers,omitempty"`
	// AbilityIndex selects an activated ability by its IR index (the index
	// into the source's Face().Abilities), the anchor decision.Option.Ability
	// carries and rules' `activate` step resolves. It lets a level-B activate
	// step name an ability without pasting the script's SpellDescription
	// text. Nil on every level-A step, so level-A items are byte-identical.
	AbilityIndex *int `json:"ability_index,omitempty"`
	// Ability selects an `activate` option by its label (the runner's
	// oracleStep.Ability): a special action the engine offers under its own
	// option kind, such as a Room's "Unlock <door name>", has no IR ability
	// index; a granted, gained or keyword-granted activated ability (an
	// AddAbilities$/AddAbility$/GainsAbilitiesOf$/AddKeyword$ grant) has an
	// index that aliases a printed one, so the lowering writes the engine's
	// offered option label beside it. Empty on every other step.
	Ability string `json:"ability,omitempty"`
	// A scenario step may move a card into a zone; the move op stamps the
	// object as having entered this turn (a board-history target such as
	// ThisTurnEntered@Graveyard needs that).
	To string `json:"to,omitempty"`
	// AttachedTo is the bearer ref for an attach setup operation.
	AttachedTo string `json:"attached_to,omitempty"`
	// Expect asserts an observation at this step (the runner's oracleExpect
	// vocabulary, engine/rules/oracle_run.go). Nil or empty on every level-A step,
	// so level-A items are byte-identical. A generated scenario uses it to
	// hold gorge to a "nothing happens" static: with no state change, the
	// frozen snapshot has no field to compare, so the claim rides an
	// explicit expectation instead.
	Expect []Expect `json:"expect,omitempty"`
}

// Expect is one assertion attached to a Step. It is the subset of the
// runner's oracleExpect vocabulary a generated level-B scenario emits.
// TriggerOnStack names a source ref whose trigger must (Want true, the
// default) or must not (Want false) be a stack entry; StackSize pins the
// stack length. Both read as engine/rules/oracle_run.go's oracleExpect fields do.
type Offered struct {
	Seat  int    `json:"seat"`
	Kind  string `json:"kind"`
	Card  string `json:"card"`
	Label string `json:"label,omitempty"`
}

type CanBlock struct {
	Blocker  string `json:"blocker"`
	Attacker string `json:"attacker"`
	// MaxBlockers asserts the MinMaxBlocker Max$ bound the (blocker,
	// attacker) pair's option carries; nil asserts no bound.
	MaxBlockers *int `json:"max_blockers,omitempty"`
}

// AttackRequired asserts whether Attacker's option at the pending
// declare-attackers decision carries the MustAttack requirement
// (engine/rules/oracle_run.go's oracleAttackRequired).
type AttackRequired struct {
	Attacker string `json:"attacker"`
}

// CanAttack asserts whether Attacker is among the attackers the pending
// declare-attackers decision offers (engine/rules/oracle_can_attack.go). Defender,
// when set, requires the matched option to name the attacked battlefield
// permanent (a planeswalker ref); MaxAttackers, when set, additionally
// asserts the AttackRestrict cap the matched option's Group publishes
// (0 = no restriction).
type CanAttack struct {
	Attacker string `json:"attacker"`
	// Defender, when set, names the planeswalker the attack would be
	// declared against (the option's Battle), not just its controller.
	Defender     string `json:"defender,omitempty"`
	MaxAttackers *int   `json:"max_attackers,omitempty"`
}

type Expect struct {
	TriggerOnStack string     `json:"trigger_on_stack,omitempty"`
	StackSize      *int       `json:"stack_size,omitempty"`
	Offered        *Offered   `json:"offered,omitempty"`
	CanBlock       *CanBlock  `json:"can_block,omitempty"`
	CanAttack      *CanAttack `json:"can_attack,omitempty"`
	// AttackRequired is the runner's per-attacker MustAttack requirement
	// assertion (rules oracleExpect.AttackRequired).
	AttackRequired *AttackRequired `json:"attack_required,omitempty"`
	// LookAtLibraryTop is the runner's per-seat "may look at the top card of
	// their library" assertion (rules oracleExpect.LookAtLibraryTop).
	LookAtLibraryTop map[string]bool `json:"look_at_library_top,omitempty"`
	// LookAt is the same read over a battlefield object (rules
	// oracleExpect.LookAt, mayLookAtObject): a Continuous MayLookAt$ grant
	// over a face-down permanent is hidden information no snapshot field
	// carries. The key names the seat, the value the object ref.
	LookAt map[string]string `json:"look_at,omitempty"`
	// MaxHandSize is the runner's per-seat effective CR 514.1 maximum hand
	// size assertion (rules oracleExpect.MaxHandSize), a Continuous
	// SetMaxHandSize$ grant no snapshot field carries.
	MaxHandSize map[string]int `json:"max_hand_size,omitempty"`
	Want        *bool          `json:"want,omitempty"`
}

// Answer is a queued answer for gorge's runner (kind = decision kind).
type Answer struct {
	Kind string   `json:"kind"`
	Pick []string `json:"pick"`
}

// DieRoll is one die roll an item's step makes. Step is the scenario step
// whose action (or resolution) rolled it, Seat the roller, Sides the die and
// Natural the unmodified result. Result is the recorded result after any
// Modifier$; a follower forces Natural, then applies its own modifier exactly
// as the live game did. The list is flat and in log order, so a step that
// rolls several dice keeps every one.
type DieRoll struct {
	Step    int `json:"step"`
	Seat    int `json:"seat"`
	Sides   int `json:"sides"`
	Natural int `json:"natural"`
	Result  int `json:"result,omitempty"`
}

// CoinFlip is one coin flip an item's step makes. Step is the scenario step
// whose action (or resolution) flipped it, Seat the flipper and Win whether
// the flip was won (heads). Forced marks a flip a FlipCoinMod static turned
// heads without a draw (Edgar, King of Figaro): the follower reproduces it
// through the same static, so a driver that forces results skips it. Draws is
// how many results the flip takes from the follower's generator: absent for
// the ordinary one, 2 under a FlipCoinDoubler static (Krark's Thumb). The
// list is flat and in log order, so a step that flips several coins (Mana
// Clash's repeat loop) keeps every one.
type CoinFlip struct {
	Step   int  `json:"step"`
	Seat   int  `json:"seat"`
	Win    bool `json:"win"`
	Forced bool `json:"forced,omitempty"`
	Draws  int  `json:"draws,omitempty"`
}

// runnerCoinFlip is one CoinFlip in the shape gorge's runner decodes
// (rules.oracleCoinFlip): it has no Forced field (the runner rejects unknown
// fields), and its draws is a pointer so the ordinary one-draw flip stays
// absent while a forced flip's 0 tells the runner's forced-flip queue to skip
// it. A doubler flip keeps its 2.
type runnerCoinFlip struct {
	Step  int  `json:"step"`
	Seat  int  `json:"seat"`
	Win   bool `json:"win"`
	Draws *int `json:"draws,omitempty"`
}

// runnerCoinFlips maps the item's flips to the runner's record shape. It is
// the one place Forced/Draws become the runner's draws accounting, so a flip
// the live game forced is never queued for a forced result the runner will
// never ask for.
func runnerCoinFlips(fs []CoinFlip) []runnerCoinFlip {
	if len(fs) == 0 {
		return nil
	}
	out := make([]runnerCoinFlip, 0, len(fs))
	for _, f := range fs {
		r := runnerCoinFlip{Step: f.Step, Seat: f.Seat, Win: f.Win}
		switch {
		case f.Forced:
			draws := 0
			r.Draws = &draws
		case f.Draws > 0:
			draws := f.Draws
			r.Draws = &draws
		}
		out = append(out, r)
	}
	return out
}

// ThreeSeats is Scenario.Players for a multiplayer scenario: p0 and two
// opponents, so one opponent can lose the game while it goes on (CR 800.4a).
const ThreeSeats = 3

// FormatCommander is Scenario.Format for a Commander game: the runner's
// oracleFormatCodes key for its Commander format. It is the only non-empty
// value this repo writes ("constructed" is the empty string).
const FormatCommander = "commander"

// Scenario is one generated scenario in the runner's schema.
type Scenario struct {
	Name string   `json:"name"`
	Turn int      `json:"turn,omitempty"`
	CR   []string `json:"cr"`
	Why  string   `json:"why"`
	// Players is the seat count (the runner's and the XMage driver's
	// "players"): 0 keeps the two-seat game, and omitempty keeps every
	// two-seat item's bytes, and so its scenario sha, unchanged. A scenario
	// whose observation needs a player to lose while the game goes on (CR
	// 800.4a) seats ThreeSeats.
	Players int `json:"players,omitempty"`
	// Format is the game format: "" is a constructed game (20 life);
	// FormatCommander is a two-seat Commander game (CR 903): 40 starting
	// life, commanders from each seat's Command, the tax and commander
	// damage live. omitempty keeps every other item's bytes.
	Format string          `json:"format,omitempty"`
	Setup  map[string]Seat `json:"setup"`
	// SetupAnswers answer decisions posed while the runner drives from
	// genesis to turn 1 (a permanent's "may begin the game" ask).
	SetupAnswers []Answer `json:"setup_answers,omitempty"`
	Steps        []Step   `json:"steps"`
}

// Item is one pipeline line: the scenario plus its identity. The XMage
// driver ignores the extra fields; Raw() strips them for gorge's runner.
//
// XAnswers scripts, per step, the answers XMage's strict choose mode needs
// for the decisions the step poses (targets of a trigger, a mode, a "may").
// They are derived from the decisions gorge's deterministic runner made, so
// both engines answer alike; a decision only one engine poses still shows
// up, as an XMage harness error or a gorge leftover.
type Item struct {
	ID       string `json:"id"`
	Card     string `json:"card"`
	Template string `json:"template"`
	// XMageName is the card's spelling in XMage's card database when it
	// differs from Card (the corpus spelling): Forge prints "Dáin Ironfoot",
	// XMage stores "Dain Ironfoot". The XMage driver adds and casts the card
	// under XMageName and rewrites its snapshots back to Card, so both sides
	// name it alike. Empty means the two spellings are equal.
	XMageName string      `json:"xmage_name,omitempty"`
	XAnswers  [][]XAnswer `json:"xmage_answers,omitempty"`
	// XAbility is parallel to Scenario.Steps: entry i is the XMage rule-text
	// prefix of step i's activated ability ("{T}", "Equip {2}"), or "" for a
	// step that is not an activate. It lives on the Item, not the Step,
	// because rules' runner decodes steps with DisallowUnknownFields and
	// would reject an unknown per-step field. Empty for every level-A item.
	XAbility []string `json:"xmage_ability,omitempty"`
	// XTargetSkips is parallel to Scenario.Steps: entry i lists the cast
	// step's completely omitted optional XMage target objects, each with the
	// offset in Targets where the driver queues its "[target_skip]" (see
	// XTargetSkip). Like XAbility it is an Item field, never a Step one, so
	// Raw() and gorge's runner never see it. Nil for every item that is not
	// the supported independent-0..1 shape, which keeps those bytes unchanged
	// and the driver on its legacy trailing skip.
	XTargetSkips [][]XTargetSkip `json:"xmage_target_skips,omitempty"`
	// XPos is parallel to Scenario.Steps: entry i is, for a pass step of a
	// recorded game (an xengine tape or window), where gorge stood after it
	// (see XPos); nil for every other step. The XMage driver cannot tell from
	// the steps alone which pass resolves a stack object and which moves the
	// game to the next step (CR 117.4), so it follows these. Like XAbility it
	// is an Item field, never a Step one. Empty for every non-xengine item.
	XPos []*XPos `json:"xmage_pos,omitempty"`
	// Payment is parallel to Scenario.Steps: entry i is the pool mana a cast
	// or activate step must spend, as WUBRGC letters (one per unit), when
	// gorge's log recorded the actual payment. It is the multiset, not an
	// order: a follower that offers manual mana payment (XMage's TestPlayer,
	// Forge's chooseManaFromPool) unlocks the units in whatever order its cost
	// asks for. Empty entry or nil slice for every other step. Like XAbility
	// it is an Item field, never a Step one, so Raw() never sends it to
	// gorge's runner (which decodes steps with DisallowUnknownFields).
	Payment []string `json:"payment,omitempty"`
	// DieRolls is every die roll the item's steps made, in log order. It is
	// an Item field, never a Step one. Raw() sends it to gorge's runner as the
	// scenario's top-level die_rolls (gorge 9b1be11b19), which forces each
	// natural result instead of drawing from its seeded generator (CR 706.2),
	// so every Raw() caller replays the recorded roll. A follower that must
	// not roll its own die reads it too: XMage scripts each roll through
	// xmage_answers (kind "die"), and a Forge driver forces item.die_rolls
	// before running the step. Empty for every item that rolls no die, so
	// existing item bytes are unchanged.
	DieRolls []DieRoll `json:"die_rolls,omitempty"`
	// CoinFlips is every coin flip the item's steps made, in log order. Like
	// DieRolls it is an Item field, never a Step one. Raw() sends it to
	// gorge's runner as the scenario's top-level coin_flips (gorge's
	// forcedFlips, CR 705.1), which forces each random flip to the recorded
	// result instead of drawing from its seeded generator, so every Raw()
	// caller replays the recorded flips; a Forced flip goes as draws 0, which
	// the runner's forced-flip queue skips. A Forge driver must force each
	// result before running the step too (its FlipCoinEffect flips from its
	// own seeded stream, so a card like Mana Clash loops a different number of
	// times); XMage already scripts the runner's flips through xmage_answers
	// (kind "flip"). Empty for every item that flips no coin, so existing item
	// bytes are unchanged.
	CoinFlips []CoinFlip `json:"coin_flips,omitempty"`
	// Ignore names snapshot fields the comparison leaves out for this
	// scenario: library_top after the card shuffles a library.
	Ignore []string `json:"ignore,omitempty"`
	// Compare opts a snapshot field into the comparison. Empty (omitempty)
	// keeps a level-A item byte-identical.
	Compare []string `json:"compare,omitempty"`
	Scenario
}

// StepFirstStrikeDamage is XPos.Step for the first of a first-strike combat's
// two damage steps (CR 510.4), which gorge's step names do not separate.
const StepFirstStrikeDamage = "first-strike-damage"

// XPos is the position gorge reached after one pass step: its turn, step
// (gorge's name: "main1", "begin-combat", ..., or StepFirstStrikeDamage) and
// active seat. Moved is true when the pass changed the turn or step (both
// players passed in succession with an empty stack, CR 500.2), Resolves when it
// removed an object from the stack (both passed with a non-empty stack, CR 117.4).
type XPos struct {
	Turn     int32  `json:"turn"`
	Step     string `json:"step"`
	Active   int    `json:"active"`
	Moved    bool   `json:"moved,omitempty"`
	Resolves bool   `json:"resolves,omitempty"`
}

// XAnswer is one scripted XMage answer: Kind target (Value a card name, a
// seat "pN", or "[target_skip]"), mode (Value the 1-based mode number),
// choice (Value "yes"/"no" or an option label), flip (Value "win"/"lose"),
// or die (Value the natural die result).
type XAnswer struct {
	Seat  int    `json:"seat"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Raw is the scenario plus the item's recorded die rolls and coin flips, as
// gorge's runner decodes it. It is the one helper for runner JSON that includes
// the tape, so a caller that replays the item's steps forces the same rolls and
// flips; a caller that changes the step list must keep the tape's steps in
// range (or drop the tape). The runner fails a roll whose sides or seat
// disagree with the engine's and one a step leaves unconsumed; a flip record
// whose seat disagrees, or one asked past the last record, is a harness fail.
func (it Item) Raw() []byte { return it.RawScenario(it.Scenario) }

// RawScenario is Raw with sc in place of the item's scenario. A probe that
// replays the item with retargeted or extended steps (xprep's retarget rerun,
// pending-resolution and modal-trigger continuations) goes through here too, so
// it forces the same rolls and flips. sc must keep every step a recorded roll
// or flip is on, or gorge's runner fails the unconsumed roll.
func (it Item) RawScenario(sc Scenario) []byte {
	b, _ := json.Marshal(struct {
		Scenario
		DieRolls  []DieRoll        `json:"die_rolls,omitempty"`
		CoinFlips []runnerCoinFlip `json:"coin_flips,omitempty"`
	}{sc, it.DieRolls, runnerCoinFlips(it.CoinFlips)})
	return b
}

// CutSteps keeps the first n steps and every per-step side table parallel to
// them. It is the one place an item is shortened, so a side table added later
// cannot be forgotten at a call site: the repro cut and the tape-turn prefix
// both go through here.
func (it *Item) CutSteps(n int) {
	n = max(0, min(n, len(it.Steps)))
	it.Steps = it.Steps[:n]
	if len(it.XAnswers) > n {
		it.XAnswers = it.XAnswers[:n]
	}
	if len(it.XAbility) > n {
		it.XAbility = it.XAbility[:n]
	}
	if len(it.XPos) > n {
		it.XPos = it.XPos[:n]
	}
	if len(it.Payment) > n {
		it.Payment = it.Payment[:n]
	}
	if len(it.XTargetSkips) > n {
		it.XTargetSkips = it.XTargetSkips[:n]
	}
	if len(it.DieRolls) > 0 {
		var out []DieRoll
		for _, r := range it.DieRolls {
			if r.Step < n {
				out = append(out, r)
			}
		}
		it.DieRolls = out
	}
	if len(it.CoinFlips) > 0 {
		var out []CoinFlip
		for _, f := range it.CoinFlips {
			if f.Step < n {
				out = append(out, f)
			}
		}
		it.CoinFlips = out
	}
}

// Skip explains why a card got no scenario.
type Skip struct {
	Card   string `json:"card"`
	Reason string `json:"reason"`
}

// mayYes queues, on the step that posed it, an answer taking option 0 for
// every decision gorge's fallback left empty although it offered options.
// A payment-tap ask (Convoke/Improvise/Waterbend/Harmonize's "Tap <name>
// for 1/for <colour>/to waterbend for 1", a creature-mana "instead of
// paying 1", a Harmonize "reduce by N") is never force-answered: the tap
// changes which mana the cast spends, and the two engines must agree on the
// payment, so the injected pick leaves gorge pool mana floating that a
// pool-paid cast spends (The Wandering Rescuer, Lofty Dreams). The empty
// fallback answer is kept: both engines then pay the whole cost from the
// pool.
func mayYes(sc Scenario, ds []rules.OracleDecision) (Scenario, bool) {
	out := sc
	out.Steps = append([]Step(nil), sc.Steps...)
	changed := false
	for _, d := range ds {
		if d.Step < 0 || d.Step >= len(out.Steps) || d.Options == 0 || d.First == "" ||
			d.Via == "target" || d.Via == "answer" || (d.GorgeKind != "choose" && d.GorgeKind != "target") {
			continue
		}
		if len(d.Picks) == 0 && tapPaymentAsk(d.First) {
			continue
		}
		if len(d.Picks) > 0 && (d.Picks[0] != d.First || (len(d.PickIdx) > 0 && d.PickIdx[0] != 0)) {
			// The runner's fallback deliberately answered past option 0
			// (the "done" it takes for an optional triggered cost's mana
			// window no potential mana can pay, Esoteric Duplicator):
			// pinning option 0 would tap the source it declined to tap.
			// Option 0 can share the pick's label (a CR 704.5j keep among
			// same-named legends, where the runner keeps the member that
			// survives the batch: Grub, Notorious Auntie's token), so the
			// index decides, not the label.
			continue
		}
		st := out.Steps[d.Step]
		st.Answers = append(append([]Answer(nil), st.Answers...), Answer{Kind: d.GorgeKind, Pick: []string{d.First}})
		out.Steps[d.Step] = st
		changed = true
	}
	return out, changed
}

// tapPaymentAsk reports an option label from pay.ConvokeAsk's optional
// tap-for-payment menu. The family is matched by label because a
// rules.OracleDecision carries no option kinds when the fallback declined
// it; every member names the tapped permanent and the contribution. A mana
// wheel's "Tap <name> for mana" and a mandatory pay ask (Min 1, so gorge's
// fallback answered it and Picks is non-empty) do not match.
func tapPaymentAsk(label string) bool {
	if !strings.HasPrefix(label, "Tap ") {
		return false
	}
	if strings.HasSuffix(label, " for 1") || strings.HasSuffix(label, " instead of paying 1") {
		return true
	}
	if strings.Contains(label, " (reduce by ") {
		return true
	}
	for _, c := range []string{"W", "U", "B", "R", "G"} {
		if strings.HasSuffix(label, " for "+c) {
			return true
		}
	}
	return false
}

// searchPicks queues, on the hidden-library-search step that posed it, an
// answer taking the search's first eligible card. Gorge's deterministic
// fallback declines an "up to N" hidden search (CR 701.19b fail-to-find),
// and XMage poses a mandatory library search (Terramorphic Expanse's
// TargetCardInLibrary 1..1) that rejects the resulting [target_skip] with
// "Wrong skip command found". A declined search whose option list is
// non-empty has a legal card, so re-running with the first eligible card
// makes both engines find the same card. An optional "you may search"
// shape poses a search_confirm yes/no first; when that confirm was declined
// no search pick is posed, so there is nothing to queue, and when it was
// accepted the search was actually made, so forcing its pick agrees with
// XMage too. The answer is only kept when the replay consumes it (see the
// caller's PlaysThrough guard), so a search XMage never reaches falls back
// to the decline pair.
func searchPicks(sc Scenario, ds []rules.OracleDecision, look bool) (Scenario, bool) {
	out := sc
	out.Steps = append([]Step(nil), sc.Steps...)
	changed := false
	for i, d := range ds {
		if d.Step < 0 || d.Step >= len(out.Steps) || d.Options == 0 ||
			d.Kind != "choose_n" || len(d.Picks) != 0 {
			continue
		}
		pick := ""
		switch {
		case d.Resume == "search" && d.First != "":
			pick = d.First
		case look && declinedLookPick(d) && !lookedAck(ds, i):
			// A declined two-option choose-one from an exile/library look
			// (Fireglass Mentor's "Choose one of them" over the Dig'ed exiled
			// cards): XMage poses the ask as a mandatory TargetCardInExile
			// 1..1 that rejects even the skip token ("Wrong skip command
			// found"), so gorge must take the first looked-at card too. The
			// rerun's pick is re-scripted onto XMage's target queue by
			// RetargetForcedLookPicks; the caller's PlaysThrough guard keeps
			// the rerun only when the replay consumes the answer. The look
			// flag is the serving template's card-side proof: the same
			// decision shape on an ask XMage poses as an "up to" makeChoose
			// agrees with the routing's own skip, and a forced pick there
			// would move gorge's board for nothing (measured: Speedball, New
			// Warrior). A look_ack ahead of the ask is the Zimone's
			// Experiment shape the measured declined pair answers, so it is
			// excluded here.
			pick = d.OptionRefs[0]
		case look && declinedLookedOnePick(d) && lookedAck(ds, i):
			// A declined one-card chooser after a hidden-zone look the served
			// trigger chain proves (Whiskervale Forerunner's "You may reveal a
			// creature card ... from among them"): XMage's ask is
			// player.choose(cards, TargetCardInLibrary(filter)) -- a
			// mandatory 1..1 makeChoose that rejects the skip token -- so
			// gorge must take the lone eligible card too. The pick stays on
			// XMage's choice queue (no RetargetForcedLookPicks: that is the
			// two-option exile form), and the chooseUse that follows the
			// pick is scripted from the rerun's own decisions.
			pick = d.OptionRefs[0]
		default:
			continue
		}
		st := out.Steps[d.Step]
		st.Answers = append(append([]Answer(nil), st.Answers...), Answer{Kind: d.GorgeKind, Pick: []string{pick}})
		out.Steps[d.Step] = st
		changed = true
	}
	return out, changed
}

// declinedLookPick is the declined shape of the exile/library look chooser:
// "choose one of them" over two looked-at objects, up to one, nothing
// picked, option 0's label empty (the options are the looked-at objects).
func declinedLookPick(d rules.OracleDecision) bool {
	return d.Resume == "choice" && d.Options == 2 && d.Min == 0 && d.Max == 1 &&
		len(d.OptionRefs) == 2 && d.First == ""
}

// declinedLookedOnePick is the declined shape of a one-card look chooser:
// "choose a card" over exactly one looked-at eligible object, up to one,
// nothing picked, no option label (the option is the object).
func declinedLookedOnePick(d rules.OracleDecision) bool {
	return d.Resume == "choice" && d.Options == 1 && d.Min == 0 && d.Max == 1 &&
		len(d.OptionRefs) == 1 && d.First == ""
}

// lookedAck reports a look_ack by the same seat in decision i's step earlier
// in the list: the Zimone's Experiment shape whose declined answer is the
// measured "no" + target_skip pair (answer_routing.go), never a forced pick.
func lookedAck(ds []rules.OracleDecision, i int) bool {
	for _, d := range ds[:i] {
		if d.Resume == "look_ack" && sameAsker(d, ds[i]) {
			return true
		}
	}
	return false
}

// RetargetForcedLookPicks rewrites the XMage answers of the forced first-card
// picks SearchPicks queued for a declined exile/library look (Fireglass
// Mentor): XMage poses that ask as a mandatory TargetCardInExile on the
// target queue, so the makeChoose choice form the generic pick path emits
// would be a wrong-queue answer. The value is the generic target answer's
// own form (targetPickValue), so a same-named pair stays disambiguated
// exactly as every target ask is. Only callers that just re-ran a scenario
// SearchPicks changed call it; a decision list with no forced pick is
// returned unchanged.
func RetargetForcedLookPicks(xa [][]XAnswer, ds []rules.OracleDecision) [][]XAnswer {
	for _, d := range ds {
		if d.Step < 0 || d.Step >= len(xa) || len(d.Picks) != 1 || d.Picks[0] != "" ||
			len(d.PickKinds) != 1 || d.PickKinds[0] != "card" ||
			d.Kind != "choose_n" || d.Resume != "choice" || d.Options != 2 ||
			d.Min != 0 || d.Max != 1 || len(d.OptionRefs) != 2 || d.First != "" ||
			len(d.PickRefs) != 1 {
			continue
		}
		choice := disambiguatedObjectChoice(d, 0, oraclediffRefName(d.PickRefs[0]))
		for i, a := range xa[d.Step] {
			if a.Seat == d.Seat && a.Kind == "choice" && a.Value == choice {
				xa[d.Step][i] = XAnswer{d.Seat, "target", targetPickValue(d, 0, oraclediffRefName(d.PickRefs[0]))}
				break
			}
		}
	}
	return xa
}

// playsThrough replays sc exactly and reports whether gorge performed
// every step and ended with an empty stack. Both leftover targets and
// answers are failures here: callers must rewrite targets to gorge's actual
// picks before replaying the generated scenario.
func playsThrough(reg *cards.Registry, sc Scenario) (rules.OracleResult, bool) {
	res, ok := probeTargets(reg, sc)
	return res, ok && len(res.Fails) == 0
}

// probeTargets runs a preliminary fixture that may over-offer targets. Only
// unused-target failures are excused here, before chooseTargets replaces the
// fixture slots; a reversed cast, or any other failure, is never a success.
func probeTargets(reg *cards.Registry, sc Scenario) (rules.OracleResult, bool) {
	b, _ := json.Marshal(sc)
	res, err := rules.RunOracleScenarioJSON(reg, b)
	if err != nil || len(res.Snapshots) != len(sc.Steps)+1 || castAborted(sc, res.Snapshots, res.Transcript) {
		return res, false
	}
	for _, f := range res.Fails {
		if !strings.Contains(f, rules.OracleUnusedTargetMarker) {
			return res, false
		}
	}
	return res, len(res.Snapshots[len(res.Snapshots)-1].Stack) == 0
}

type charmMode struct{ svar, label string }

// CharmCombination is one legal, ordered set of mode picks and their target
// chains. Combinations follow Choices$ order; repeated picks appear only when
// CanRepeatModes$ is true.
type CharmCombination struct {
	Modes []CharmMode
	Slots []Slot
}

// CharmCombinations enumerates legal combinations from the minimum pick count
// upward, in Choices$ order within each size. Ordinary charms default to one
// pick, preserving their historical ordering.
// optionalCostCharmNum reads a CharmNum$ that names an SVar of the form
// Count$OptionalGenericCostPaid.<paid>.<unpaid> with literal branches, and
// returns the two counts in ascending order.
func optionalCostCharmNum(f *cards.Face, charmNum string) (int, int, bool) {
	body, ok := f.SVars[strings.TrimSpace(charmNum)]
	if !ok {
		return 0, 0, false
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(body), "Count$OptionalGenericCostPaid.")
	if !ok {
		return 0, 0, false
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 2 {
		return 0, 0, false
	}
	a, errA := strconv.Atoi(parts[0])
	b, errB := strconv.Atoi(parts[1])
	if errA != nil || errB != nil || a <= 0 || b <= 0 {
		return 0, 0, false
	}
	return min(a, b), max(a, b), true
}

func CharmCombinations(f *cards.Face) []CharmCombination {
	modes := charmModes(f)
	if len(modes) == 0 {
		return nil
	}
	pickCount, minCount, repeat := 1, 1, false
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" || sa.API != "Charm" {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(sa.Params["CharmNum"])); err == nil && n > 0 {
			pickCount = n
		}
		if n, err := strconv.Atoi(strings.TrimSpace(sa.Params["MinCharmNum"])); err == nil && n > 0 {
			minCount = n
		} else if strings.TrimSpace(sa.Params["MinCharmNum"]) == "" {
			minCount = pickCount
		}
		if lo, hi, ok := optionalCostCharmNum(f, sa.Params["CharmNum"]); ok {
			// The count depends on the cast's optional-cost election (Pyrrhic
			// Strike's "if this spell's additional cost was paid, choose both
			// instead"): offer both counts, the smaller first, so a declined
			// cast keeps its historical first plan and a paid one finds a
			// plan of its own size.
			minCount, pickCount = lo, hi
		}
		repeat = strings.EqualFold(strings.TrimSpace(sa.Params["CanRepeatModes"]), "True")
		break
	}
	var out []CharmCombination
	var selected []CharmMode
	// emit appends the current selection as one combination.
	emit := func() {
		combo := CharmCombination{Modes: append([]CharmMode(nil), selected...)}
		for _, mode := range selected {
			combo.Slots = append(combo.Slots, ChainSlotSpecs(f, mode.svar)...)
		}
		out = append(out, combo)
	}
	// allDistinct reports whether the current selection repeats no mode.
	allDistinct := func() bool {
		seen := make(map[string]bool, len(selected))
		for _, mode := range selected {
			if seen[mode.svar] {
				return false
			}
			seen[mode.svar] = true
		}
		return true
	}
	// visit enumerates combinations of count modes from start. distinct
	// constrains the walk to strictly increasing positions (repeat-free
	// combinations only); otherwise positions may repeat, and the leaf
	// skips any all-distinct selection so the two walks do not overlap.
	var visit func(start, count int, distinct bool)
	visit = func(start, count int, distinct bool) {
		if len(selected) == count {
			if !distinct && allDistinct() {
				return
			}
			emit()
			return
		}
		for i := start; i < len(modes); i++ {
			selected = append(selected, modes[i])
			next := i
			if distinct {
				next++
			}
			visit(next, count, distinct)
			selected = selected[:len(selected)-1]
		}
	}
	// Within each pick count the repeat-free combinations come first, in
	// Choices$ order; only a CanRepeatModes$ charm then adds the repeating
	// ones. A non-repeat charm takes the single distinct walk, so its output
	// is byte-identical to the historical one.
	for count := minCount; count <= pickCount; count++ {
		visit(0, count, true)
		if repeat {
			visit(0, count, false)
		}
	}
	return out
}

// charmModes lists the spell's charm modes in Choices$ order.
func charmModes(f *cards.Face) []charmMode {
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" {
			continue
		}
		if sa.API != "Charm" {
			return nil
		}
		var out []charmMode
		for _, name := range strings.Split(sa.Params["Choices"], ",") {
			if name = strings.TrimSpace(name); name != "" {
				out = append(out, charmMode{name, effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)})
			}
		}
		return out
	}
	return nil
}

// targetZone is the zone a target filter draws from: an explicit TgtZone$,
// else a non-battlefield Origin$, else Stack for a slot that names a spell
// or ability on the stack (TargetType$ Spell/SpellAbility/Activated/
// Triggered, or ValidTgts$ with inZoneStack).
//
// charmMode selects the narrower stack rule a charm's SVar mode needs: a
// mode whose ValidTgts$ also names a battlefield type (Icy Reception's
// "creature or legendary spell", Theorix Charm's "noncreature card") keeps
// its battlefield fixture, because the historical generator read it that
// way and its verdict is pinned to the battlefield scenario. A top-level
// Counter (Precise Redaction, Countersculpt) is routed before this by
// targetsSpell, so its broader reading is unchanged.
func targetZone(params map[string]string, charmMode bool) string {
	if z := params["TgtZone"]; z != "" {
		return z
	}
	if o := params["Origin"]; o != "" {
		// An Origin$ that names the battlefield alone is the default zone
		// and stays unsuffixed (no slot change for the common case); an
		// Origin$ that adds Stack ("Battlefield,Stack") is kept so the
		// slot records both alternatives.
		if !strings.Contains(o, "Battlefield") || strings.Contains(o, "Stack") {
			return o
		}
	}
	if charmMode {
		if charmModeTargetsStack(params) {
			return "Stack"
		}
		return ""
	}
	if abilityTargetsStack(params) {
		return "Stack"
	}
	return ""
}

// zoneNamesStack reports whether a TgtZone$ list names Stack (it may be a
// comma-separated combo such as "Stack,Battlefield").
func zoneNamesStack(z string) bool {
	for _, part := range strings.Split(z, ",") {
		if strings.EqualFold(strings.TrimSpace(part), "Stack") {
			return true
		}
	}
	return false
}

// abilityTargetsStack reports whether an ability's target is a spell or
// ability on the stack, judged from its target vocabulary: a "Spell"-
// family TargetType$, or a ValidTgts$ naming the inZoneStack zone. An
// explicit non-stack zone (TgtZone$ / Origin$) wins, so a card targeting an
// instant card in a graveyard is not mistaken for a stack target.
func abilityTargetsStack(params map[string]string) bool {
	if strings.Contains(params["ValidTgts"], "inZoneStack") {
		return true
	}
	if params["TargetType"] == "" {
		return false
	}
	for _, part := range strings.Split(params["TargetType"], ",") {
		base := strings.SplitN(strings.TrimSpace(part), ".", 2)[0]
		switch base {
		case "Spell", "SpellAbility", "Activated", "Triggered", "Instant", "Sorcery":
			return true
		}
	}
	return false
}

// charmModeTargetsStack is abilityTargetsStack narrowed for a charm SVar
// mode: an ability-type TargetType$ (SpellAbility/Activated/Triggered) is
// always stack-only, but a Spell/Instant/Sorcery target is routed to the
// stack only when its ValidTgts$ cannot be read as a battlefield permanent.
// A mode whose ValidTgts$ names a battlefield base type (Creature, Card, ...)
// keeps the battlefield fixture its committed verdict was generated for.
func charmModeTargetsStack(params map[string]string) bool {
	if strings.Contains(params["ValidTgts"], "inZoneStack") {
		return true
	}
	if params["TargetType"] == "" {
		return false
	}
	for _, part := range strings.Split(params["TargetType"], ",") {
		base := strings.SplitN(strings.TrimSpace(part), ".", 2)[0]
		switch base {
		case "SpellAbility", "Activated", "Triggered":
			return true
		}
	}
	for _, part := range strings.Split(params["TargetType"], ",") {
		base := strings.SplitN(strings.TrimSpace(part), ".", 2)[0]
		switch base {
		case "Spell", "Instant", "Sorcery":
			return !validTgtsNamesBattlefield(params["ValidTgts"])
		}
	}
	return false
}

// validTgtsBattlefieldBases are the ValidTgts$ base types that the
// battlefield candidate table can serve. A filter that names any of them is
// satisfiable on the battlefield, so a charm mode's Spell-family
// TargetType$ on such a filter is not forced onto the stack.
var validTgtsBattlefieldBases = map[string]bool{
	"any": true, "creature": true, "player": true, "opponent": true,
	"permanent": true, "card": true, "artifact": true, "enchantment": true,
	"land": true, "planeswalker": true,
}

// validTgtsNamesBattlefield reports whether a ValidTgts$ filter names a
// battlefield-card base type. An inZoneStack qualifier wins (the card is on
// the stack), and only the first alternative is consulted -- the fixture
// builder serves a filter from its first alternative's shape. An empty
// filter names no type and is never battlefield-satisfiable here.
func validTgtsNamesBattlefield(validTgts string) bool {
	if strings.Contains(validTgts, "inZoneStack") {
		return false
	}
	first := strings.SplitN(validTgts, ",", 2)[0]
	base := strings.ToLower(strings.SplitN(strings.TrimSpace(first), ".", 2)[0])
	return validTgtsBattlefieldBases[base]
}

// SlotIsStack reports whether a target filter (as TargetSlots/ChainSlots
// encode it) draws only from the stack. A slot that also names a non-stack
// zone ("Stack,Battlefield") is served by the ordinary fixture -- the
// battlefield candidate -- so it is not a stack slot.
func SlotIsStack(filter string) bool {
	i := strings.LastIndexByte(filter, '@')
	if i < 0 {
		return false
	}
	z := filter[i+1:]
	if !zoneNamesStack(z) {
		return false
	}
	for _, part := range strings.Split(z, ",") {
		if p := strings.TrimSpace(part); p != "" && !strings.EqualFold(p, "Stack") {
			return false
		}
	}
	return true
}

// firstThreeTurnScenarioTurn recognizes the compiled first-three-turns cast
// lockout. Fixtures cast as seat 0 in a two-seat game, so global turn 2*4-1
// is that seat's fourth turn (TurnsTaken >= 4), when the lockout ends.
func firstThreeTurnScenarioTurn(f *cards.Face) (int, bool) {
	if f == nil {
		return 0, false
	}
	for _, st := range f.Statics {
		if st.Mode != "CantBeCast" || st.Params["SVarCompare"] != "LE3" {
			continue
		}
		v := st.Params["CheckSVar"]
		if body, ok := f.SVars[v]; ok {
			v = body
		}
		if v == "Count$YourTurns" {
			return 7, true
		}
	}
	return 0, false
}

// NewItem names a template's scenario. The template's version is part of
// the id and the scenario name, so bumping one template's version stales
// only that template's verdicts (oracle/compliance/oraclegen/templates).
func NewItem(f *cards.Face, card, template string, version int, sc Scenario) Item {
	if turn, ok := firstThreeTurnScenarioTurn(f); ok {
		sc.Turn = turn
	}
	sc.Name = fmt.Sprintf("gen%d-%s", version, template)
	sc.CR = []string{"601.2"}
	sc.Why = "generated level-A scenario"
	it := Item{ID: fmt.Sprintf("%s/%s/v%d", card, template, version), Card: card, Template: template, Scenario: sc}
	if CanShuffleLibrary(f) {
		it.Compare = []string{CompareNoLibraryOrder}
	}
	return it
}

// baseline gives every cast scenario something for "up to one target"
// triggers and library searches to find: XMage poses those decisions even
// with no legal choice (and strict mode then needs a scripted skip), while
// gorge skips them. An opposing creature and a varied library top make
// both engines ask.
func baseline(setup map[string]Seat, f *cards.Face) {
	p1 := setup["p1"]
	has := false
	for _, n := range p1.Battlefield {
		if n == "Grizzly Bears" {
			has = true
		}
	}
	if !has {
		p1.Battlefield = append(p1.Battlefield, "Grizzly Bears")
	}
	setup["p1"] = p1
	if randomLibraryPick(f) {
		// An effect that CHOOSES a card from the library at random (Getaway
		// Barrel's "put a random creature card from among them onto the
		// battlefield") picks with each engine's own RNG, so a varied top made
		// the permanent's identity differ run to run (gorge Ornithopter, XMage
		// Grizzly Bears). One NAME repeated 13 times (the largest reveal the
		// generated rows inspect) makes every random pick choose the same name
		// -- the 13 entries are distinct card objects whose names match, which
		// is what the name-level frozen snapshot compares.
		p0 := setup["p0"]
		p0.LibraryTop = repeat("Grizzly Bears", 13)
		setup["p0"] = p0
	} else if searchesLibrary(f) {
		p0 := setup["p0"]
		p0.LibraryTop = []string{"Jace Beleren", "Grizzly Bears", "Forest", "Glorious Anthem", "Shock", "Plains", "Ornithopter"}
		setup["p0"] = p0
	}
}

// randomLibraryPick reports whether one of the card's effects selects a card
// from a library with AtRandom$ True (Getaway Barrel's ChooseCard). Such a
// scenario needs a single-name library top: the selection is random, and the
// two engines' RNGs do not agree.
func randomLibraryPick(f *cards.Face) bool {
	randomFromLibrary := func(line string) bool {
		return strings.Contains(line, "AtRandom$ True") &&
			(strings.Contains(line, "ChoiceZone$ Library") || strings.Contains(line, "Origin$ Library"))
	}
	for _, sa := range f.Abilities {
		if randomFromLibrary(sa.Line) {
			return true
		}
	}
	for _, body := range f.SVars {
		if randomFromLibrary(body) {
			return true
		}
	}
	for _, t := range f.Triggers {
		if t.Effect != nil && randomFromLibrary(t.Effect.Line) {
			return true
		}
	}
	return false
}

func searchesLibrary(f *cards.Face) bool {
	for _, sa := range f.Abilities {
		if sa.Kind == "SP" && sa.API == "Discover" {
			// Discover (CR 701.57) exiles from the top of the library until a
			// nonland card is found, so the scenario must seed a discoverable
			// card; otherwise gorge finds none and no Treasures are made while
			// XMage, whose library is not empty, creates them.
			return true
		}
		if strings.Contains(sa.Line, "Origin$ Library") {
			return true
		}
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "Origin$ Library") {
			return true
		}
	}
	for _, t := range f.Triggers {
		if t.Effect != nil && strings.Contains(t.Effect.Line, "Origin$ Library") {
			return true
		}
	}
	return false
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func withHand(s Seat, name string) Seat {
	s.Hand = append([]string{name}, s.Hand...)
	return s
}

// settle runs the cast in gorge and returns how many resolve steps empty
// the stack (at most 4); ok is false when gorge cannot cast with this
// fixture.
func settle(reg *cards.Registry, sc Scenario) (int, rules.OracleResult, bool) {
	for n := 1; n <= 4; n++ {
		try := sc
		try.Steps = append(append([]Step(nil), sc.Steps...), make([]Step, n)...)
		for i := len(sc.Steps); i < len(try.Steps); i++ {
			try.Steps[i] = Step{Op: "resolve"}
		}
		b, _ := json.Marshal(try)
		res, err := rules.RunOracleScenarioJSON(reg, b)
		if err != nil || len(res.Snapshots) == 0 {
			return 0, res, false
		}
		for _, f := range res.Fails {
			// The static fixture over-offers targets on purpose; the generator
			// rewrites each cast step to gorge's actual picks afterwards, and
			// verifies the rewrite with PlaysThrough. So the runner's
			// unused-target self-check is expected here and is not a reason to
			// reject the fixture; every other step/harness fail is.
			if strings.Contains(f, rules.OracleUnusedTargetMarker) {
				continue
			}
			if strings.HasPrefix(f, "step ") || strings.Contains(f, "harness:") {
				return 0, res, false
			}
		}
		if castAborted(try, res.Snapshots, res.Transcript) {
			continue
		}
		last := res.Snapshots[len(res.Snapshots)-1]
		if len(res.Snapshots) == len(try.Steps)+1 && len(last.Stack) == 0 {
			return n, res, true
		}
	}
	return 0, rules.OracleResult{}, false
}

// castAborted rejects a scenario where the runner reports a cast abort or a
// cast step leaves its card in its origin hand zone. CR 601.2c/733.1 reverses
// an illegal cast; treating the resulting empty stack as a successful settle
// would publish a scenario that never cast the named spell.
func castAborted(sc Scenario, snaps []rules.OracleSnapshot, transcript []string) bool {
	for _, line := range transcript {
		if strings.Contains(strings.ToLower(line), "cast aborted") {
			return true
		}
	}
	if len(snaps) != len(sc.Steps)+1 {
		return false
	}
	for i, st := range sc.Steps {
		if st.Op != "cast" {
			continue
		}
		seatRef, name, ok := strings.Cut(st.Card, ":")
		if !ok || !strings.HasPrefix(seatRef, "p") {
			continue
		}
		var seat int
		if _, err := fmt.Sscanf(seatRef, "p%d", &seat); err != nil || seat < 0 || seat >= len(snaps[i].Players) {
			continue
		}
		before := countName(snaps[i].Players[seat].Hand, name)
		after := countName(snaps[i+1].Players[seat].Hand, name)
		if before > 0 && after >= before {
			return true
		}
	}
	return false
}

func countName(names []string, want string) int {
	n := 0
	for _, name := range names {
		if name == want {
			n++
		}
	}
	return n
}

// castModalSteps is the set of steps whose mode decision is the cast spell's
// OWN announcement (CR 601.2b: gorge's ResumeKind "cast_modes"). Only there is
// the runner's fallback target decision the spell's target. A modal cast
// TRIGGER's mode and target are chosen as the trigger is put on the stack
// (CR 603.3c, ResumeKind "modes"), which can fall on the same cast step
// (Ojutai Exemplars' "whenever you cast a noncreature spell, choose one"); its
// target must stay a scripted TARGET answer, not become the spell's own.
func castModalSteps(ds []rules.OracleDecision) map[int]bool {
	modal := map[int]bool{}
	for _, d := range ds {
		if d.Kind == "mode" && d.Step >= 0 && d.Resume == "cast_modes" {
			modal[d.Step] = true
		}
	}
	return modal
}

// chooseTargets rewrites the target-carrying step's Targets from the target
// decisions gorge's deterministic runner actually made, in order. A
// target-carrying step is a cast (XMage's castSpell) or an activated
// ability's activate step (activateAbility): both take the step's Targets
// directly. The static fixture only promises a legal candidate per slot; it
// over-offers -- an "up to N" slot gorge declines, a token slot with no token
// on the board, a slot in a mixed chain -- and XMage rejects a target list
// whose count does not match the ability's, so the scenario must carry
// exactly gorge's picks (the target decision's PickRefs, in order).
//
// castSteps is the set of step indices whose targets were taken from such a
// step (the steps held on the Item): xanswers sends those through castSpell
// or activateAbility, not through a scripted target answer, while a target
// decision posed during a resolve step still needs an XMage answer. A step
// that posed no target decision (every slot skipped, or an ability with no
// targets) has its fixture targets cleared, so the surplus never reaches
// XMage. Level-A scenarios hold only cast steps, so their bytes and
// castSteps are unchanged.
func chooseTargets(sc Scenario, ds []rules.OracleDecision) (Scenario, map[int]bool) {
	out := sc
	out.Steps = append([]Step(nil), sc.Steps...)
	// carriesTargets is the op set whose own targets XMage consumes: cast
	// through castSpell, activate through activateAbility.
	carriesTargets := func(op string) bool { return op == "cast" || op == "activate" }
	// A modal cast's target is posed after the mode pick, so the runner's
	// fallback target decision ("cast fallback") is deterministic but not
	// Via "target"; fold it into the step too. Left as an xanswer it sat in
	// XMage's target queue until the NEXT target ask (a copied Shock's
	// any-target), which then rejected the name (Choreographed Sparks).
	modal := castModalSteps(ds)
	chosen := map[int][]string{}
	castSteps := map[int]bool{}
	for _, d := range ds {
		if d.Kind != "target" || d.Step < 0 || d.Step >= len(out.Steps) || !carriesTargets(out.Steps[d.Step].Op) {
			continue
		}
		if d.Via != "target" && !(modal[d.Step] && out.Steps[d.Step].Op == "cast") {
			continue
		}
		// A copy's CR 707.10c new-target election is never a cast step's own
		// targets: XMage poses it as per-slot chooseUse dialogs, not through
		// castSpell, so it must not be folded into the step's fixture targets.
		if d.Resume == "copy_targets" {
			continue
		}
		chosen[d.Step] = append(chosen[d.Step], d.PickRefs...)
		castSteps[d.Step] = true
	}
	for i := range out.Steps {
		if !carriesTargets(out.Steps[i].Op) {
			continue
		}
		if _, ok := castSteps[i]; ok {
			out.Steps[i].Targets = chosen[i]
			continue
		}
		// A cast that posed no target decision: drop the fixture's surplus.
		out.Steps[i].Targets = nil
	}
	return out, castSteps
}

// arrangeEmptyGraveyard reports whether an arrange decision answered the empty
// kept set of an optional (Min 0) graveyard-destination ask at step time: the
// shape an upkeep Surveil's step-time fallback poses in a scenario that
// revisits the upkeep, where every looked-at card went to the graveyard.
func arrangeEmptyGraveyard(d rules.OracleDecision) bool {
	return arrangeEmpty(d) && d.ArrangeKind == "graveyard"
}

// arrangeEmpty reports whether an arrange decision kept none of the cards an
// optional (Min 0) ask looked at at step time, sending them all away: to the
// graveyard (surveil) or the bottom of the library (scry).
func arrangeEmpty(d rules.OracleDecision) bool {
	return d.Step >= 0 && d.Min == 0 && len(d.PickIdx) == 0 && len(d.ArrangeLabels) > 0 &&
		len(d.ArrangeLabels) == d.Options
}

// xanswersSetup turns gorge's recorded decisions into XMage's scripted
// answers, grouped by the step that posed them. setup is the scenario's setup
// (nil when the caller has none): it lets a trigger-order decision recorded
// before step zero be retimed as setup_choice when its sources are the
// setup-placed permanents (triggerOrderSetupPosed). flips are the run's
// recorded coin-flip results (rules.OracleResult.Flips): a drive-window flip
// (step < 0) queues on the setup family, a step flip leads its own step's
// answer stream, each as the XMage flip token the choice queue consumes.
// The callers with a scenario pass one xanswersExtra: digs resolves a Dig
// decision's step to its source card's IR (DestinationZone2), how
// xanswersForScenario tells the exile family from the battlefield/graveyard
// one, and pips carries the per-step flexible-pip plan (phyrexianPipPlan) so
// a step that pays an earlier pip with mana and a later one with life scripts
// each of XMage's per-pip asks positionally instead of one flat yes.
// Callers with no scenario/snapshots omit it and keep the historical
// shape-only routing.
func xanswersSetup(ds []rules.OracleDecision, steps int, modes map[string]int, castSteps map[int]bool, setup map[string]Seat, flips []rules.OracleFlip, extras ...xanswersExtra) [][]XAnswer {
	var digSources map[int]digSource
	var pipPlans map[int][]pipAsk
	var casts map[int]bool
	var activates map[int]int
	var searchZones map[int]searchZoneAsk
	var forage map[int][]string
	var ownBounce map[int]bool
	var ownGraveyardExile map[int]bool
	var tapThenUntap map[int]bool
	var bolster map[int]bool
	var optionalTriggerSearch map[int]bool
	var craft map[int]bool
	var supports map[int]supportAnswers
	var snaps []rules.OracleSnapshot
	var orderAsks map[int]bool
	var sources map[int][]string
	var allPlayerUnless map[int]bool
	var seats int
	var chooseUseOrders map[int][]XAnswer
	var removeCounterCosts []removeCounterCostAsk
	for _, ex := range extras {
		if sources == nil {
			sources = ex.sources
		}
		if allPlayerUnless == nil {
			allPlayerUnless = ex.allPlayerUnless
		}
		if seats == 0 {
			seats = ex.seats
		}
		if chooseUseOrders == nil {
			chooseUseOrders = ex.chooseUseOrders
		}
		if ownBounce == nil {
			ownBounce = ex.ownBounce
		}
		if ownGraveyardExile == nil {
			ownGraveyardExile = ex.ownGraveyardExile
		}
		if tapThenUntap == nil {
			tapThenUntap = ex.tapThenUntap
		}
		if bolster == nil {
			bolster = ex.bolster
		}
		if optionalTriggerSearch == nil {
			optionalTriggerSearch = ex.optionalTriggerSearch
		}
		if craft == nil {
			craft = ex.craft
		}
		if supports == nil {
			supports = ex.supports
		}
		if forage == nil {
			forage = ex.forage
		}
		if digSources == nil {
			digSources = ex.digs
		}
		if pipPlans == nil {
			pipPlans = ex.pips
		}
		if snaps == nil {
			snaps = ex.snaps
		}
		if orderAsks == nil {
			orderAsks = ex.orderAsks
		}
		if casts == nil {
			casts = ex.casts
		}
		if activates == nil {
			activates = ex.activates
		}
		if searchZones == nil {
			searchZones = ex.searchZones
		}
		if removeCounterCosts == nil {
			removeCounterCosts = ex.removeCounterCosts
		}
	}
	// A trigger-cost sacrifice the engine folded into its pay election still
	// needs its one sacrificed permanent on XMage's choice queue ahead of the
	// effect's own picks (loneTriggerCostSacrifice, from the scenario caller).
	var loneSacrifice map[int][]string
	for _, ex := range extras {
		if loneSacrifice == nil {
			loneSacrifice = ex.loneSacrifice
		}
	}
	// A hand_move/library_placement goes top-first (reverse the answers for
	// XMage's successive top asks) or bottom-first (keep them) by where the
	// after-step checkpoint shows the picks, not by a list of cards. An
	// explicit changezone_alternative "bottom" overrides a checkpoint that
	// cannot tell a bottom-into-empty-library placement from a top one.
	topPlacement := LibraryTopPlacements(ds, snaps)
	out := make([][]XAnswer, steps)
	any := false
	// The plan's pip decisions: the ordinal of each pip decision among its
	// step's pip decisions (the cost order XMage asks them in) and whether the
	// plan covers that ordinal. A covered decision is scripted from the plan;
	// an uncovered one (no plan for the step, a mis-derived cost) keeps the
	// historical single-yes/skip behaviour.
	pipPlanned := make([]bool, len(ds))
	pipOrdinal := make([]int, len(ds))
	seenPips := map[int]int{}
	for i, d := range ds {
		pipOrdinal[i] = -1
		if !isPipDecision(d) {
			continue
		}
		plan, ok := pipPlans[d.Step]
		if !ok {
			continue
		}
		k := seenPips[d.Step]
		seenPips[d.Step]++
		pipOrdinal[i] = k
		if k < len(plan) && plan[k].asks {
			pipPlanned[i] = true
		}
	}
	modal := castModalSteps(ds)
	routing := newAnswerRouting(ds, digSources)
	if ownBounce != nil {
		routing.ownBounce = ownBounce
	}
	if ownGraveyardExile != nil {
		routing.ownGraveyardExile = ownGraveyardExile
	}
	routing.tapThenUntap = tapThenUntap
	if searchZones != nil {
		routing.searchZones = searchZones
	}
	routing.sources = sources
	routing.allPlayerUnless = allPlayerUnless
	routing.seats = seats
	routing.chooseUseOrders = chooseUseOrders
	searchZoneSuppress := searchZoneRedundantBooleans(ds, searchZones)
	// RemoveCounter's CounterType$ Any/All election is one per-counter run of
	// KChoose picks in gorge but one multi-amount dialog in XMage: fold the
	// run into one "X=<n>" amount per kind message before the generic choice
	// route scripts the raw kind labels (remove_counter_answers.go).
	counterAnswers, counterOwned := foldRemoveCounterKindAnswers(ds, snaps)
	// A "Remove a counter from {this}" activation cost is XMage's other
	// multi-amount: RemoveCountersSourceCost always messages every counter kind
	// on the source, even when gorge picked the lone kind unasked. Script the
	// step's amount answers and own the counter-kind picks the engine recorded
	// for that cost (remove_counter_answers.go).
	costAnswers, costOwned := foldRemoveCounterCostAnswers(ds, snaps, removeCounterCosts)
	costEmitted := map[int]bool{}
	// Trigger-order name answers are demoted back to the inert text form when
	// the step also scripts other asks (see the trigger_order case).
	var trigSpans []*trigSpan
	var pendingTrig *trigSpan
	// Setup ETB replacement choices (colour or creature type) are answered
	// before XMage places the seeded permanents, so they must LEAD step zero's
	// answer stream: the driver queues them before build() and XMage's as-enters
	// dialog consumes the head of the choice queue. Collect them apart rather
	// than appending in decision order, which would put a step-0 gameplay
	// answer that happens to precede the setup decision (Lifecraft Engine's
	// crew pick) in front of the as-enters dialog. Prepended after the loop.
	var setupAnswers []XAnswer
	// Setup-posed trigger orders (a seeded permanent's ETB order pends while
	// build() places it) share that pre-build queue. They queue AFTER every
	// as-enters choice: XMage poses the placement dialogs during build() and
	// the ordering ask only at the first priority, so a colour answer must
	// precede a name even when the runner recorded the order first.
	var setupTrigAnswers []XAnswer
	// The setup drive's other answers (a modal pick, an optional boolean or a
	// labelled choice the runner answered with its fallback) queue after the
	// as-enters choices and the trigger orders: XMage consumes the placement
	// dialogs during build(), the ordering ask at the first priority, and
	// these asks as the setup drive reaches their trigger.
	var setupAsks []XAnswer
	// Drive-window coin flips (Mana Crypt's turn-1 upkeep) queue after the
	// as-enters choices: the placement dialogs are consumed during build()
	// and the flip while the drive walks to the target turn. A flip that
	// shares the drive with a setup ask (setupAsks) has no recorded
	// interleaving against it — the flips are read off the log after the
	// drive — so the queue order between the two is the drive's phase order
	// only where the scenario poses no setup ask (the measured flip rows:
	// Mana Crypt trigger/activate pose none).
	var setupFlips []XAnswer
	// Gameplay-window flips, per step, in log order; prepended ahead of the
	// step's decision answers once the decisions are placed (a gameplay
	// flip resolves before the step's scripted dialogs re-read the queue;
	// the measured rows script nothing else on the flip's step).
	stepFlips := make([][]XAnswer, steps)
	// Support target answers are collected apart because their step precedes
	// the decision that poses them: the support target is the source's FIRST
	// target, so it must lead its announcement step's target queue even when
	// that step also scripts another target's answer (a support spell's
	// sub-ability target). Prepending them after demoteTriggerOrderSpans keeps
	// the trigger-order span indices valid.
	supportLead := make([][]XAnswer, steps)
	for _, f := range flips {
		v := map[bool]string{true: "win", false: "lose"}[f.Win]
		if f.Step < 0 {
			setupFlips = append(setupFlips, XAnswer{f.Seat, "setup_flip", v})
			any = true
			continue
		}
		if f.Step < steps {
			stepFlips[f.Step] = append(stepFlips[f.Step], XAnswer{f.Seat, "flip", v})
			any = true
		}
	}
	for i, d := range ds {
		if d.Step < 0 {
			switch {
			case IsSetupChoice(d):
				pick := d.Picks[0]
				if colour, ok := bareColourLabel(d, 0); ok {
					pick = colour
				}
				setupAnswers = append(setupAnswers, XAnswer{d.Seat, "setup_choice", pick})
				any = true
			case isSetupNumber(d):
				a, _ := SetupNumberChoice(d)
				setupAnswers = append(setupAnswers, a)
				any = true
			case IsSetupTargetDecline(d):
				// The driver queues a target skip before build() so XMage
				// declines the optional ask instead of auto-picking.
				setupAnswers = append(setupAnswers, XAnswer{d.Seat, "setup_target", "[target_skip]"})
				any = true
			case triggerOrderSetupPosed(d, setup):
				// The last pick is never consumed: XMage asks only while more
				// than one ability is pending and pushes the last without an
				// ask (same drop as the gameplay name form below).
				for _, key := range setupTriggerOrderKeys(d) {
					setupTrigAnswers = append(setupTrigAnswers, XAnswer{d.Seat, "setup_choice", key})
				}
				any = true
			default:
				if as := setupDriveAnswers(d, modes); len(as) > 0 {
					setupAsks = append(setupAsks, as...)
					any = true
				}
			}
			continue
		}
		if searchZoneSuppress[i] {
			// The mapped SearchLibraryGraveyard* effect folds the optional
			// search choices into its own library chooseUse.
			continue
		}
		if sa, ok := supports[i]; ok {
			// XMage asks a Support pick as a target when the source is
			// announced, not at the resolution step gorge recorded it on
			// (support_pick.go). The answers lead the announcement step.
			if sa.step >= 0 && sa.step < steps && len(sa.answers) > 0 {
				supportLead[sa.step] = append(supportLead[sa.step], sa.answers...)
				any = true
			}
			continue
		}
		folded := d.Via == "target" || (d.Kind == "target" && modal[d.Step])
		if d.Step >= steps || (folded && d.Resume != "copy_targets" && castSteps[d.Step]) {
			// A cast step's own targets reach XMage through castSpell; a
			// target decision posed at a resolve step is scripted below. A
			// modal cast's fallback target is folded the same way (the mode
			// pick precedes it, so the runner records it Via "cast fallback").
			// A copy's new-target election is never a cast step's targets even
			// if it shares the step, so it is scripted below regardless.
			continue
		}
		if costOwned[i] {
			// Emit the cost's whole amount run once, at the position of its
			// first counter-kind pick; the rest of the run is owned silently.
			if as := costAnswers[d.Step]; len(as) > 0 && !costEmitted[d.Step] {
				out[d.Step] = append(out[d.Step], as...)
				costEmitted[d.Step] = true
				any = true
			}
			continue
		}
		if counterOwned[i] {
			if as := counterAnswers[i]; len(as) > 0 {
				out[d.Step] = append(out[d.Step], as...)
				any = true
			}
			continue
		}
		if as, ok := bandAnswers(d); ok {
			// A band declaration (CR 702.22c) is the pair of asks XMage's
			// Combat.handleBanding poses: a chooseUse "Form a band ...?" for
			// each member, then a TargetControlledPermanent pick of that
			// member. Gorge records one KChoose per accepted member or
			// decline, so an accepted object pick is yes + its ref and the
			// decline label is no. The generic choice route would queue the
			// gorge label, which chooseUse cannot read (it wants Yes/No) and
			// which would then be consumed by the target pick.
			if len(as) > 0 {
				out[d.Step] = append(out[d.Step], as...)
				any = true
			}
			continue
		}
		if as, owned := routing.route(i); owned {
			if len(as) > 0 {
				out[d.Step] = append(out[d.Step], as...)
				any = true
			}
			continue
		}
		if len(d.Picks) == 1 && hasNamePick(d) {
			// XMage's "choose a card name" dialog always ranges over every
			// card name in its pool (CR 201.3), so it is posed even when
			// gorge's deck-scoped name universe offered a single option
			// (Cabal Therapy, Phyrexian Revoker, Silverquill Silencer). The
			// pick is that dialog's choice answer whether or not a named
			// search follows (the Ancient Vendetta case), and it is never a
			// forced one-option ask. Setup-time name choices keep their
			// setup_choice queue above.
			out[d.Step] = append(out[d.Step], XAnswer{d.Seat, "choice", d.Picks[0]})
			any = true
			continue
		}
		if forcedSingleOption(d) && !pipPlanned[i] && !forcedChoicePosed(ds, i) && !announcesSacrificeX(ds, i) && !castAnnouncesX(d, casts) && !activationAnnouncesX(d, activates) {
			// A forced one-option ask: XMage does not pose it. A forced
			// target-kind pick is the exception -- one legal opponent is still
			// a chooseTarget XMage asks for -- and so is a mode ask, which
			// XMage keeps posing even when only one mode is affordable.
			continue
		}
		var as []XAnswer
		switch d.Kind {
		case "target":
			// A CR 603.3d chain link's forced single target (Mechanical
			// Mobster's "target creature you control" with only itself) is
			// scripted like any other: XMage's Targets.chooseTargets asks
			// every target object that still has a candidate, so a missing
			// answer is "Missing TARGET def" (measured on SPM-s103 xe:SPM:0:3:w10,
			// with the first object picked and again with it skipped; an earlier
			// std-pass note that XMage picks it unasked no longer holds).
			as = targetDecisionAnswers(routing, i)
		case "mode":
			if d.Resume == "cipher" {
				// Cipher's encode (CR 702.99a) is one KModes decision in gorge:
				// no pick declines, one pick names the creature. XMage asks a
				// boolean chooseUse, then (when encoding) the creature target;
				// the numeric mode queue never sees it.
				if len(d.PickRefs) == 0 {
					as = append(as, XAnswer{d.Seat, "choice", "no"})
					break
				}
				as = append(as, XAnswer{d.Seat, "choice", "yes"})
				for k, ref := range d.PickRefs {
					// The pick's label is "Encode on <name>"; the target
					// queue matches the creature's own name.
					as = append(as, XAnswer{d.Seat, "target", targetPickValue(d, k, oraclediffRefName(ref))})
				}
				break
			}
			if d.Resume == "play" {
				// Optional Play effects (Discover, Cascade, and other carriers)
				// are XMage chooseUse asks, not numeric mode choices. Gorge's
				// recorded pick means cast; no pick means decline to hand.
				yes := len(d.Picks) > 0
				as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[yes]})
				break
			}
			if d.Resume == "unless_pay" || d.Resume == "unless_decline" {
				// An unless-pay (UnlessCost$) is XMage's boolean chooseUse,
				// not a mode ask; the engine models it as a mode decision
				// whose picked label is the cost paid or the decline ("Don't
				// pay"). Every XMage unless-pays effect -- sacrifice, exile
				// or tap the source, counter the spell, do the effect unless
				// a player pays -- asks chooseUse for ANY payable cost, mana,
				// life, energy or object components (the cost classes only
				// differ in cost.canPay/pay). Answer the boolean from the
				// pick: yes when gorge paid, no when it declined. An object
				// cost's picks remain the following "unless_cost" choose_n,
				// which the ordinary choose_n route below already joins into
				// one aliased choice; leaving the paid pick as a numeric mode
				// answer sends chooseUse to TestPlayer's AI instead.
				if routing.allPlayerUnless[i] {
					// The "any player may have <source> deal N damage to
					// them" punisher: XMage offers its chooseUse to every
					// player in range, even after one accepts, while gorge
					// stops at the first payer. Script one boolean per payer
					// seat, not one per recorded decision, or the payer
					// XMage still asks is left to its AI (Browbeat).
					as = append(as, routing.allPlayerUnlessAnswers(i)...)
					break
				}
				as = append(as, XAnswer{d.Seat, "choice", unlessPolarity(d)})
				break
			}
			if pickKind(d, 0) == "discard" || d.Resume == "discard" {
				// Gorge's discard card picker is KModes; XMage uses
				// TargetDiscard.choose -> makeChoose (not chooseMode). A
				// discard among same-named cards needs the exact-object
				// discriminator, exactly as the ordinary choice path does.
				for k, ref := range d.PickRefs {
					// discardLabel is the card name XMage's TargetDiscard matches;
					// the pick LABEL is often "Discard <name>", which would not
					// match. Only the exact-object discriminator is added.
					as = append(as, XAnswer{d.Seat, "choice", disambiguatedObjectChoice(d, k, oraclediffRefName(ref))})
				}
				if d.Max > len(d.PickRefs) {
					as = append(as, XAnswer{d.Seat, "choice", "[choice_skip]"})
				}
				break
			}
			// gorge offers only the modes with legal targets, so an option
			// index is not always the mode number; the label is when it names
			// a charm mode. A label outside the charm map is still a mode ask
			// for a non-charm modal (a plain modal), so fall back to the
			// option's 1-based position; the discard-style picker that shares
			// the kind is routed above.
			for k, i := range d.PickIdx {
				if m, ok := modeNumberFor(d, k, modes); ok && m == ModeChoiceQueue {
					// A DB$ GenericChoice | SetChosenMode$ True body (a
					// Theros-style Siege): XMage reads the pick through its
					// ChooseModeEffect -> controller.choose(Outcome.Neutral,
					// Choice, game), the CHOICE queue, showing the option
					// LABEL ("Abzan", "Khans") -- never a numeric mode. The
					// label is what gorge's mode decision already carries.
					as = append(as, XAnswer{d.Seat, "choice", d.Picks[k]})
					continue
				}
				if m, ok := modeNumberFor(d, k, modes); ok && m == ModeKWChoiceQueue {
					// A Pump KWChoice$ ("gains your choice of ..."): XMage's
					// GainsChoiceOfAbilitiesEffect asks ONE ChoiceImpl over the
					// candidate keyword abilities' rule texts, read from the
					// CHOICE queue by label, never the numeric mode queue
					// (Lunar Avenger measured: "Missing CHOICE def ... Choose an
					// ability to gain", mode 1 left unused). The rule text is
					// the keyword name in lower case.
					as = append(as, XAnswer{d.Seat, "choice", xmageKeywordRuleText(d.Picks[k])})
					continue
				}
				if m, ok := modeNumberFor(d, k, modes); ok && m == ModeKWBespokeChoiceQueue {
					// A Pump KWChoice$ on a bespoke-dialog face (Angelic
					// Skirmisher): the CHOICE queue by XMage's own label.
					as = append(as, XAnswer{d.Seat, "choice", xmageBespokeChoiceLabel(d.Picks[k])})
					continue
				}
				if m, ok := modeNumberFor(d, k, modes); ok && (m == ModeYesQueue || m == ModeNoQueue) {
					// A two-way GenericChoice XMage asks as chooseUse: a
					// Timetwister-style per-player "may shuffle" (Turtles in
					// Time) or a source-proven xmageChooseUseFaces punisher
					// (Terrapact Intimidator's targeted opponent). The answer
					// is yes/no for whichever seat chose. A yes branch that
					// moves the looked-at window to the library bottom in any
					// order carries XMage's follow-up card-order asks
					// (chooseUseBottomOrders, Petals of Insight).
					as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[m == ModeYesQueue]})
					if m == ModeYesQueue {
						as = append(as, routing.chooseUseOrders[d.Step]...)
					}
					continue
				}
				n := i + 1
				if m, ok := modeNumberFor(d, k, modes); ok {
					n = m
				}
				as = append(as, XAnswer{d.Seat, "mode", fmt.Sprint(n)})
			}
			if max(d.Max, printedModeMax(d, modes)) > len(d.Picks) {
				// XMage keeps choosing modes up to Max -- the printed CharmNum
				// when gorge clamped Max to the modes it could offer (Sentinel
				// of Lost Lore); stop it with the mode queue's skip token
				// (including when gorge picked none).
				as = append(as, XAnswer{d.Seat, "mode", "[mode_skip]"})
			}
		case "yesno":
			yes := len(d.PickIdx) > 0 && d.PickIdx[0] == 0
			if len(d.Picks) > 0 {
				l := strings.ToLower(d.Picks[0])
				yes = strings.HasPrefix(l, "yes") || strings.HasPrefix(l, "accept") || strings.HasPrefix(l, "pay") || l == "true"
			}
			as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[yes]})
		case "choose_n":
			if d.Resume == "changezone_dest_alt" {
				// gorge's DestinationAlternative ask (Dina's Guidance's "put it
				// into your hand or graveyard"). XMage asks it as a boolean
				// chooseUse("Hand", "Graveyard"), so a zone label is not an
				// answer (it is left over, unconsumed): script yes for gorge's
				// primary destination (option 0, also its default) and no for
				// the alternative.
				yes := len(d.PickIdx) == 0 || d.PickIdx[0] == 0
				as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[yes]})
				break
			}
			if routing.divideOwned[i] {
				// The division ask's shares were emitted with its target
				// ask (dividedTargetAnswers).
				continue
			}
			if routing.manaHoistedPick(i) {
				// The colour answer was hoisted to the trigger-order span's
				// queue position (manaHoistSpan); emitting it here too would
				// double it.
				continue
			}
			if d.Resume == "damage_split" {
				if routing.ownsSplit(i) {
					// The shares were emitted at the target ask's own queue
					// position (before that ask's chain skips); emitting them
					// here as well would duplicate every "^X=" answer.
					continue
				}
				// Divided damage: the engine's split ask repeats one option index
				// per damage assigned (Fury, Forked Bolt, Twin Bolt). XMage's
				// chooseTargetAmount consumes one "<ref>^X=<share>" per chosen
				// target on the target queue, not makeChoose choices.
				as = damageSplitAnswers(d)
				break
			}
			if d.Resume == "twopiles_pick" && len(d.Picks) == 1 {
				// The pile pick (Curator of Destinies, Fact or Fiction) is
				// XMage's opponent.chooseUse("Face-down", "Face-up"), a
				// boolean choice (RevealAndSeparatePilesEffect's Steam
				// Augury asks Player.choosePile instead; the driver's
				// ScriptedChoicePlayer.choosePile reads the same Yes/No),
				// never gorge's "First pile"/"Second pile"
				// label (measured 2026-10-14: the label is "Found wrong
				// choice command"). Taking pile A -- the separator's pile,
				// the face-down pile XMage's TargetCard selected -- is the
				// boolean true.
				as = append(as, XAnswer{d.Seat, "choice", twoPilesPileAnswer(d)})
				break
			}
			if d.Resume == "mana_color" && len(d.ManaColours) > 1 {
				// A Produced$ "Combo <colours>" ask whose offered span
				// exceeds one colour is XMage's AddManaInAnyCombinationEffect
				// at every unit count: one multi-amount message per offered
				// colour, in the set's own order, zeroes included (Muerra,
				// Trash Tactician's Add R / Add G pair). The set rides the
				// decision (ManaColours); a produced-Any ask stays on the
				// routing below, which is what its colour dialog and WUBRG
				// multi-amount already agree with. A SINGLE-colour Combo set
				// (engine/effects/params/produced.go returns ok for one colour too)
				// keeps the ordinary choice routing: no committed row exists
				// to measure its dialog shape against, and switching it to
				// amount answers on a hunch would serve an unmeasured guess.
				as = manaColourAllocation(d)
				break
			}
			if d.Resume == "mana_color" && d.Min == d.Max && d.Max > 1 {
				// A multi-amount allocation (Combo Any, Desolation of Smaug):
				// one unit per picked option, options laid out unit*5+colour
				// over WUBRG. XMage poses one multi-amount message per colour
				// and needs every one filled, zeroes included.
				as = manaAllocationAnswers(d)
				break
			}
			if pickKind(d, 0) == "trigger_cost_pay" && len(d.Picks) == 1 && nonManaTriggerCostPay(d) {
				// The pay side of a triggered optional cost XMage poses as a
				// chooseUse. Gorge elected the cost, so answer XMage's
				// chooseUse yes. This must run BEFORE payment(): payment()
				// classifies by the "Pay " label prefix, which every non-mana
				// cost phrase ("Pay 1 life", "Pay 2 energy", "Pay 2 B and pay
				// 1 life") also carries, so letting it skip first would drop
				// exactly the asks DoIfCostPaid poses whenever canPay holds
				// (Melded Moxite's "Discard a card and draw two cards?",
				// Vampire Gourmand's "Sacrifice another creature and ...?").
				// A pure mana pay is resolved earlier by triggerCostAnswer
				// (TriggerCostMana); a raw-mana label that is not a pay kind
				// stays excluded by nonManaTriggerCostPay. A blight cost is a
				// non-mana cost like any other here: its creature pick follows
				// the accepted chooseUse (Boggart Mischief).
				as = append(as, XAnswer{d.Seat, "choice", "yes"})
				if names := forage[i]; len(names) > 0 {
					// The exile arm's three graveyard cards: XMage's
					// ForageCost asks them through a TargetCardInYourGraveyard
					// makeChoose once the chooseUse is accepted; gorge exiles
					// them without an ask (forageSettle), so they come from
					// the checkpoint.
					as = append(as, XAnswer{d.Seat, "choice", strings.Join(names, "^")})
				}
				if names := loneSacrifice[i]; len(names) > 0 {
					// A lone sacrifice the engine folded into this pay
					// election: XMage's card-specific effect (The Gitrog,
					// Ravenous Ride's TargetSacrifice(0, 1), no chooseUse of
					// its own) reads the same CHOICE queue through
					// makeChoose, and its next pick is the effect's own hand
					// pick. Emitting the checkpoint's sacrificed permanent
					// here keeps the land answer off the sacrifice ask; the
					// leading yes stays for the DoIfCostPaid costs that do
					// pose a chooseUse.
					as = append(as, XAnswer{d.Seat, "choice", strings.Join(names, "^")})
				}
				break
			}
			if pipPlanned[i] {
				// A plan-covered pip: XMage poses this pip's own "Pay 2 life
				// instead of ... ?" chooseUse at its cost-order position
				// (handlePhyrexianCosts/handlePhyrexianManaCosts, printed
				// pips then granted ones, both engines in AnnouncePip order),
				// so the answer is this pip's recorded face, not a shared
				// yes: a mana-face pip scripts no and a life-face one yes
				// (dismember's pip1-mana/pip2-life mixed step).
				as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[pipPlans[d.Step][pipOrdinal[i]].life]})
				break
			}
			if phyrexianLifePicks(d) {
				// XMage asks "Pay 2 life instead of {X} (phyrexian cost)?"
				// before it will pay a Phyrexian pip (AbilityImpl
				// .handlePhyrexianCosts at CR 601.2b; ManaCostsImpl
				// .handlePhyrexianManaCosts for a cost paid later), so a
				// recorded life face is a chooseUse "yes", not payment UI.
				// Keyed on the engine's pay_life option kind, so every
				// source of the ask takes it (cast, activate, triggered
				// cost, K'rrik's PayLifeInsteadOf grant). A hybrid or
				// twobrid face (pay_W/pay_generic) is still paid from the
				// pool without a question and keeps the payment() skip.
				for range d.Picks {
					as = append(as, XAnswer{d.Seat, "choice", "yes"})
				}
				break
			}
			if payment(d.Picks) {
				// Hybrid/phyrexian halves are payment UI; XMage pays from
				// the pool without asking.
				continue
			}
			if staged, ok := stagedManaAllocationAnswers(d); ok {
				// A cost-staged "any combination of colors" allocation
				// (Conversion Apparatus's "Pay 3 energy: Add three mana in
				// any combination of colors"): the cost stage poses the ask
				// as a plain choose (no "mana_color" resume), so the
				// allocation arms above never see it, and the pick labels
				// carry the cost prefix. XMage's AddManaInAnyCombinationEffect
				// poses the same "Distribute mana among colors" multi-amount
				// for it as for a bare-resume allocation, and its AI answer
				// distributes wherever it likes (xproof 11251811: gorge WWU,
				// xmage WUB) -- so it takes the same WUBRG amount answers,
				// zeroes included.
				as = staged
				break
			}
			if n := colourPickCount(d); n > 1 && n == len(d.Picks) {
				// A multi-unit "any combination of colors" mana ability
				// (Baxter Building's "Add four mana in any combination of
				// colors", Amount$ 4) is not one XMage colour dialog. XMage
				// asks a multi-amount distribution among colours, so a joined
				// "^" colour selection throws "Missing choice in multi
				// amount". Emit nothing and let XMage distribute it itself,
				// as it did before this routing existed.
				continue
			}
			if len(d.Picks) == 1 && strings.HasPrefix(d.Picks[0], "X = ") {
				answer := XAnswer{d.Seat, "choice", "X=" + strings.TrimPrefix(d.Picks[0], "X = ")}
				as = append(as, answer)
				for n := 1; activationAnnouncesX(d, activates) && n < activates[d.Step]; n++ {
					// XMage's VariableManaCost and a separate VariableCostImpl
					// announce X independently (Ludevic, Necrogenius's mana
					// {X} then ExileXFromYourGraveCost).
					as = append(as, answer)
				}
				break
			}
			if len(d.Picks) == 1 && pickKind(d, 0) == "number" {
				if _, err := strconv.Atoi(d.Picks[0]); err == nil {
					// "Choose a number": XMage's getAmount reads an "X=<n>"
					// choice, exactly like announceX (TestPlayer.getAmount).
					as = append(as, XAnswer{d.Seat, "choice", "X=" + d.Picks[0]})
					break
				}
			}
			if len(d.Picks) == 1 && pickKind(d, 0) == "paylife" {
				// "Pay any amount of life" (Phyrexian Processor): XMage asks
				// controller.getAmount(0, life, ...), and TestPlayer.getAmount
				// reads only an "X=<n>" choice, so the engine's "<n> life"
				// label is forwarded as the amount. Keyed on the engine's
				// paylife option kind (etbPayLifeOptions), not the label text.
				if n, err := strconv.Atoi(strings.TrimSuffix(d.Picks[0], " life")); err == nil {
					as = append(as, XAnswer{d.Seat, "choice", "X=" + strconv.Itoa(n)})
					break
				}
			}
			if yn, ok := yesNo(d); ok {
				as = append(as, XAnswer{d.Seat, "choice", yn})
				break
			}
			if d.Resume == "sacrifice" && d.Min == 0 && len(d.Picks) > 0 {
				// An accepted optional sacrifice ("any opponent may sacrifice
				// a creature"): XMage asks chooseUse before the pick
				// (DesecrationDemon.java:77, DoIfCostPaid), so the pick needs
				// a "yes" ahead of it. The decline already scripts "no".
				as = append(as, XAnswer{d.Seat, "choice", "yes"})
			}
			if hiddenPickWithheld(d, snaps) {
				// A hidden-zone pick (a graveyard "you may return a land
				// card") among same-named cards no exact answer can name: the
				// bare label would let XMage return whichever one it meets
				// first. Script nothing; the replay reports the missing
				// answer instead of a false divergence (CR 608.2d).
				break
			}
			if len(d.Picks) > 1 && allChoiceQueue(d, craft[i]) {
				// One makeChoose dialog consumes ONE definition, whose own
				// parser splits on '^' into the multi-card selection (Dig's
				// "put two of them into your hand", a discard-two). Emitting
				// one answer per pick would let each pick answer a separate
				// dialog and leave the rest as an unused leftover.
				as = append(as, XAnswer{d.Seat, "choice", JoinedChoice(d)})
			} else {
				for k, label := range d.Picks {
					switch pickQueue(d, k, label, bolster[i], craft[i]) {
					case "skip":
						// XMage resolves this pick inside its computer player (a
						// library search) or pays it from the pool (a mana-tapping
						// cost); a scripted answer would only be an unused leftover.
						continue
					case "target":
						as = append(as, XAnswer{d.Seat, "target", targetPickValue(d, k, label)})
						continue
					}
					// The choice queue: makeChoose shows the option's label, which
					// for an unlabelled object pick is the object's name.
					if label == "" && k < len(d.PickRefs) {
						label = oraclediffRefName(d.PickRefs[k])
					}
					if colour, ok := bareColourLabel(d, k); ok {
						label = colour
					}
					label = disambiguatedObjectChoice(d, k, label)
					if colour, ok := manaColourLabel(label); ok {
						label = colour
					}
					as = append(as, XAnswer{d.Seat, "choice", label})
				}
			}
			switch {
			case len(d.Picks) == 0 && optionalTriggerSearch[i]:
				// A declined search inside a trigger XMage asks as an optional
				// trigger (chooseUse): the "no" ends it, no search follows, so
				// an "up to" target skip would be left over (Hexhaven
				// Invigorator; optionalTriggerSearchDecline).
				as = append(as, XAnswer{d.Seat, "choice", "no"})
			case len(d.Picks) == 0:
				// Declined: XMage may pose it as a yes/no or as an "up to"
				// pick; script both (measured: Zimone's Experiment agrees
				// only with this pair).
				as = append(as, XAnswer{d.Seat, "choice", "no"}, XAnswer{d.Seat, "target", "[target_skip]"})
			case d.Max > len(d.Picks) && len(as) > 0 && as[len(as)-1].Kind == "target":
				// Fewer than "up to N" on the target queue: stop XMage
				// picking more.
				as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
			case d.Max > len(d.Picks) && len(as) > 0:
				// A short makeChoose; its queue has its own skip token.
				as = append(as, XAnswer{d.Seat, "choice", "[choice_skip]"})
			}
		case "order":
			if d.GorgeKind == "trigger_order" {
				// XMage's chooseTriggeredAbility matches a scripted choice
				// against the ability's rule text (getRule) or its source
				// object's NAME. Gorge's label carries the card's
				// TriggerDescription$, which is XMage's rule wording only by
				// coincidence ("put a +1/+1 counter on this creature" against
				// XMage's "...put a +1/+1 counter on {this}"), so a text
				// choice misses and the replay's fallback player orders the
				// stack itself (the Adrenaline Jockey/trigger#0.1 drift). Name
				// the pick's source object instead: that is the name XMage
				// matches (a ref would carry the "@" alias form the trigger
				// ask rejects). XMage asks for the next ability only while
				// more than one is pending (its loop pushes the last
				// remaining one without an ask), so the final pick is never
				// consumed and is dropped here.
				//
				// The name only steers when the picks' sources are DISTINCT:
				// two simultaneous triggers off ONE source (Thundertrap
				// Trainer's ETB and Offspring, both p0:Thundertrap Trainer)
				// answer with the same name, which XMage cannot tell apart --
				// there the rule text is the only distinguishing answer, and
				// it already agrees. So a same-source order keeps the old
				// inert text form; only a distinct-source order names each
				// source. (This is also what keeps the level-A cast-resolve
				// scenarios whose order ask is incidental -- Thundertrap
				// Trainer, Molten Man -- byte-identical to the frozen verdict.)
				//
				// A plain name is also CONSUMABLE by any later makeChoose XMage
				// poses, unlike the inert rule text: when the step's answers
				// also script other asks, a leftover name (the ask XMage posed
				// at another point than gorge recorded, e.g. Baron Strucker's
				// ETB order) derails them. So the name form is kept only when
				// this step scripts nothing else; shared steps keep the old
				// inert text form (demoteTriggerOrderSpans).
				if !triggerOrderNamesDistinct(d) {
					// A same-source span whose step later asks a mana colour
					// pick (Ashling, Rimebound's OrTriggeredAbility mana
					// trigger): the colour dialog fires while the span's inert
					// rule texts sit at the queue head and pops the first one
					// -- the text form is unconsumable only while no other
					// dialog fires. So the colour answer leads the stream and
					// the span scripts nothing; any later order ask falls to
					// the replay's fallback player, as the inert form already
					// let it.
					if ans, ok := routing.manaHoistSpan(i); ok {
						as = append(as, ans)
						break
					}
					// XMage's rule-text form steers the ask and is consumed
					// by it (xmageTriggerOrderKeys); picks it cannot key
					// apart keep the inert text form.
					if keys, ok := xmageTriggerOrderKeys(d); ok {
						for _, key := range keys {
							as = append(as, XAnswer{d.Seat, "choice", key})
						}
						break
					}
					as = append(as, triggerOrderTextAnswers(d)...)
					break
				}
				n := len(d.Picks)
				for k := 0; k < n-1; k++ {
					as = append(as, XAnswer{d.Seat, "choice", triggerOrderChoice(d, k)})
				}
				if len(as) > 0 {
					pendingTrig = &trigSpan{step: d.Step, count: len(as), old: demotedTriggerOrderAnswers(d), textBlind: triggerOrderTextBlind(d)}
				}
				break
			}
			// Arrange first asks which cards move; its follow-up ordering is
			// also on XMage's choice queue. Keeping all cards is a choice skip.
			if d.GorgeKind == "arrange" && arrangeEmpty(d) && (d.ArrangeKind == "graveyard" || d.ArrangeKind == "bottom") {
				// Every looked-at card goes away (gorge's empty kept pile).
				// XMage's selection IS the cards that go, so script each looked-at card's name on the choice queue: a
				// leading card name takes precedence over the skip
				// (librarySelectionQueue), the queue then ends with nothing
				// further to consume, and no order answers follow. The generic
				// partial-selection arm below would emit [choice_skip], which
				// XMage reads as keep-all -- the opposite outcome.
				for _, label := range d.ArrangeLabels {
					as = append(as, XAnswer{d.Seat, "choice", label})
				}
			} else if d.GorgeKind == "arrange" {
				forcedOrder := d.Min == d.Max && d.Max == d.Options
				// The producer, not the option Kind, decides which question the
				// forced order answers: a full top reorder
				// (effRearrangeTopOfLibrary), a CR 401.4 library_placement
				// arrangement on top, and a Dig all-to-bottom remainder all
				// offer every card with one shared Kind, but they ask opposite
				// orders. gorge's ResumeKind plus the after-step library top
				// (topPlacement) is what tells them apart.
				fullTopReorder := FullTopReorder(d) ||
					d.ArrangeKind == "library_placement" && topPlacement[i] &&
						len(d.Picks) == d.Options
				if forcedOrder && fullTopReorder {
					// gorge's picks are the final top-first order, but XMage's
					// putCardsOnTopOfLibrary asks only while more than one card
					// is left and auto-tops the card never named (last one
					// chosen is topmost). Script the last N-1 picks reversed:
					// picks[0] is the topmost card and stays unnamed.
					for k := len(d.Picks) - 1; k >= 1; k-- {
						as = append(as, XAnswer{d.Seat, "choice", d.Picks[k]})
					}
				} else if forcedOrder && d.ArrangeKind == "dig_bottom" && len(d.Picks) > 0 {
					// A Dig's all-to-bottom remainder: gorge's answer order is
					// the bottom order, picks[0] highest. XMage's
					// putCardsOnBottomOfLibrary asks "card ORDER to put on the
					// BOTTOM" only while more than one card is left (last one
					// chosen is bottommost) and places the final card unasked,
					// so script the first N-1 picks: a scripted last pick was
					// left over ("(Choices of PlayerA) Count are not equal
					// (found [Forest])", Growing Rites of Itlimoc's four).
					for _, label := range d.Picks[:len(d.Picks)-1] {
						as = append(as, XAnswer{d.Seat, "choice", label})
					}
				} else if forcedOrder {
					// A forced bottom order (Min == Max == Options) has no
					// selection dialog: the answer order IS the bottom order.
					for _, label := range d.Picks {
						as = append(as, XAnswer{d.Seat, "choice", label})
					}
				} else if len(d.PickIdx) == d.Options {
					// Keeping every card where it is: XMage's surveil/scry
					// selection is a TargetCard on the target queue, and a
					// skip dismisses it. Measured against XMage on the std
					// pass (Refute Destiny, Proctor of Potential, ... -- 25
					// cards that agree only with this answer); no ORDER
					// answer follows, XMage keeps the cards in place.
					as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
				} else if binned, kept, ok := libraryArrangePartition(d.ArrangeLabels, d.PickIdx, d.Options); ok {
					// XMage's selection is pile B, the cards that go to the
					// graveyard/bottom; the labels after the skip are pile A's
					// top order, in PickIdx order. Naming the kept cards as the
					// selection sends them away too and drops the order.
					for _, label := range binned {
						as = append(as, XAnswer{d.Seat, "choice", label})
					}
					if len(kept) > 0 {
						as = append(as, XAnswer{d.Seat, "choice", "[choice_skip]"})
						for _, label := range kept {
							as = append(as, XAnswer{d.Seat, "choice", label})
						}
					}
				}
				// A decision without the full offered label list cannot be
				// split into the two piles (libraryArrangePartition refuses
				// it) and is left unscripted: guessing a split would send the
				// kept cards away. gorge records the labels of every KArrange.
			} else {
				continue
			}
		default:
			continue
		}
		if topPlacement[i] && HandMoveBatch(d) && orderAsks[i] {
			// XMage asks the hand selection first, then its "card ORDER to
			// put … from the TOP" dialog N-1 times and auto-tops the card
			// never named. The selection answer above already names all
			// picks; the order answers follow it, last N-1 picks reversed,
			// so picks[0] stays the unnamed topmost card. Only a verified
			// order-dialog effect gets these answers: a self put-back
			// (Brainstorm, Dream Cache) has no such ask, and an extra
			// choice would derail the next dialog.
			for k := len(d.Picks) - 1; k >= 1; k-- {
				as = append(as, XAnswer{d.Seat, "choice", d.Picks[k]})
			}
		}
		if len(as) > 0 {
			if pendingTrig != nil {
				pendingTrig.start = len(out[d.Step])
				trigSpans = append(trigSpans, pendingTrig)
				pendingTrig = nil
			}
			out[d.Step] = append(out[d.Step], as...)
			any = true
		}
	}
	// A cost ask on a source with a single counter kind poses no engine
	// decision, so it has no loop position to ride: its amount messages follow
	// the step's recorded answers (XMage chooses targets before it pays costs).
	// Ascending step order keeps the output independent of map iteration.
	for step := 0; step < steps; step++ {
		if costEmitted[step] {
			continue
		}
		if as := costAnswers[step]; len(as) > 0 {
			out[step] = append(out[step], as...)
			any = true
		}
	}
	if !any {
		return nil
	}
	// Gameplay flips lead their steps: prepend in log order, which the
	// per-step slices accumulated, ahead of every decision answer the loop
	// appended. AFTER demoteTriggerOrderSpans, whose span start/count were
	// recorded against the loop's pre-prepend indices (gen.go above): a
	// prepend first would shift them onto the step's other answers and splice
	// the inert text over them (dropping the scripted answers). Demoting on
	// the unshifted list keeps the flips leading either way.
	demoteTriggerOrderSpans(out, trigSpans)
	for st := range supportLead {
		if len(supportLead[st]) > 0 {
			out[st] = append(supportLead[st], out[st]...)
		}
	}
	for st, fs := range stepFlips {
		if len(fs) > 0 {
			out[st] = append(fs, out[st]...)
		}
	}
	if len(setupAnswers) > 0 || len(setupTrigAnswers) > 0 || len(setupAsks) > 0 || len(setupFlips) > 0 {
		// Setup answers are read from xmage_answers[0] before build(), even
		// when the scenario has no gameplay steps.
		if len(out) == 0 {
			out = append(out, nil)
		}
		lead := make([]XAnswer, 0, len(setupAnswers)+len(setupTrigAnswers)+len(setupAsks)+len(setupFlips))
		lead = append(lead, setupAnswers...)
		lead = append(lead, setupFlips...)
		lead = append(lead, setupTrigAnswers...)
		lead = append(lead, setupAsks...)
		out[0] = append(lead, out[0]...)
	}
	return out
}

// hasNamePick reports whether any pick of d is the engine's card-name option
// kind. XMage poses its card-name dialog over every name in its card pool
// whatever universe the engine scoped the ask to (CR 201.3), so such an ask
// always reaches the choice queue and is never a forced one-option fold. It
// matches the pick KIND, not pick 0, so a name pick that moves position or
// shares a decision with another pick cannot slip past.
func hasNamePick(d rules.OracleDecision) bool {
	for k := range d.Picks {
		if pickKind(d, k) == "name" {
			return true
		}
	}
	return false
}

// forcedSingleOption reports a one-option ask XMage never poses: exactly one
// legal answer (Min==Max==Options==1) that is not a target, order or mode ask.
// A target pick is still a chooseTarget XMage asks for even with one legal
// option, a mode ask is posed up to Max picks even when only one mode is
// affordable, and a min-0 single option is a real decline XMage still asks
// (Break Out, Destined Confrontation).
func forcedSingleOption(d rules.OracleDecision) bool {
	if d.Kind == "mode" && d.Resume == "generic_players" && d.Options == 1 && d.Min == 1 && d.Max == 1 {
		// A per-player punisher's forced one-mode ask (Rottenmouth Viper's
		// "you lose 4 life unless ..."): XMage does not pose it; the unless
		// choice that follows is the real decision.
		return true
	}
	if hasNamePick(d) {
		// A card-name dialog is always posed over XMage's whole card pool
		// (CR 201.3), however narrowly gorge's deck-scoped name universe
		// scoped the ask (Cabal Therapy, Phyrexian Revoker, Silverquill
		// Silencer). It is a real choice-queue ask, never a forced fold.
		return false
	}
	if d.Kind == "target" || d.Kind == "order" || d.Kind == "mode" {
		return false
	}
	if d.Retarget {
		// A forced "change the target" with one other legal target is still
		// XMage's chooseTarget: its loop counts the old target among the
		// possible ones, and unscripted its AI keeps picking the old target.
		return false
	}
	if loneTriggerCostDecline(d) {
		// A triggered optional cost gorge could not pay (a "may discard your
		// hand" over an empty hand, a "may pay {2}" over an empty pool) is
		// offered decline-only, but XMage's DoIfCostPaid still poses its
		// chooseUse and its AI answers yes, paying the cost gorge declined
		// (Byway Barterer, Esoteric Duplicator). The decline is a real ask.
		return false
	}
	if hasTargetPick(d) {
		return false
	}
	if hasCostPick(d) {
		// A tap, sacrifice or exile COST chooser is a real ask even with one
		// legal object: XMage's TargetPermanent("another untapped creature")
		// still poses the choice, and a dropped answer leaves the cost unpaid,
		// so TestPlayer reports "Can't find ability" (Saddle/Crew with one
		// untapped creature left). The decision's picks are object identities,
		// not the single-option LABEL asks this predicate exists for.
		return false
	}
	if phyrexianLifePicks(d) {
		// The life face of a Phyrexian pip is XMage's chooseUse ("Pay 2
		// life instead of {R}?"), and XMage poses it even when gorge's
		// feasibility gate offered ONLY that face: a cast with no coloured
		// mana (Gut Shot's {R/P}, Dismember's {B/P}{B/P}) records
		// Options == 1. Dropping the one-option ask leaves the driver's
		// queue empty, and its starved-ask arm answers no -- the opposite
		// of gorge's recorded pick. Every other payment face (pay_W,
		// pay_generic) is still dropped by the final Options check.
		return false
	}
	if announcesX(d) {
		// An X announcement is always posed (XMage's announceXMana), even
		// when the available mana leaves one legal value: an "{X}{X}, {T}"
		// activation (Gogo, Master of Mimicry) with CC offers only X = 1,
		// and an unscripted announce left the cost unpaid.
		return false
	}
	return d.Options == 1 && d.Min == 1 && d.Max == 1
}

// hasCostPick reports whether any pick pays a cost through a chooser XMage
// poses even when the choice is forced: a tap (Crew/Saddle), sacrifice or
// exile cost. These picks name object identities, so they can never be the
// lone-label ask forcedSingleOption drops.
func hasCostPick(d rules.OracleDecision) bool {
	for _, k := range d.PickKinds {
		switch k {
		case "tapcost", "sacrifice", "exilecost":
			return true
		}
	}
	return false
}

// announcesX reports whether d is the single positive "X = <n>" announcement
// of a cost that names X more than once ({X}{X}, the runner's XMulti mark).
// XMage's unscripted announce does not pay the repeated X when the pool leaves
// one legal value (Gogo's unspent CC), so it is scripted. A forced single-X
// announce is owned by castAnnouncesX/activationAnnouncesX (a real
// VariableManaCost step) or left to XMage's own default (a Craft keyword's
// XMin count: Saheeli's Lattice, The Enigma Jewel). A forced announce that
// sizes a sacrifice cost is the exception (announcesSacrificeX).
func announcesX(d rules.OracleDecision) bool {
	if !d.XMulti || len(d.Picks) != 1 || pickKind(d, 0) != "x" {
		return false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(d.Picks[0], "X = "))
	return err == nil && n > 0 && strings.HasPrefix(d.Picks[0], "X = ")
}

// castAnnouncesX reports a forced "X = <n>" announcement made while casting a
// spell (a cast step of the scenario). XMage announces a spell's X over 0..any
// (VariableManaCost's max is unbounded; TestPlayer.announceX shows "(any
// value)"), so it poses the ask however few values gorge's pool allowed: a
// forced X = 0 cast with no spare mana (The Goose Mother, Lost in the Maze,
// Meathook Massacre II's {X}{X}) strict-misses "Announce the value for {X}"
// when it is left unscripted. A triggered cost's X stays with announcesX /
// announcesSacrificeX/xTriggerCostGroup; nor is a spell whose X also sizes a
// non-mana cost part (castXSizesCost: the step is left out of casts).
func castAnnouncesX(d rules.OracleDecision, casts map[int]bool) bool {
	return announcesXAtStep(d, casts)
}

// activationAnnouncesX reports the same forced "X = <n>" announcement on an
// ACTIVATION whose own cost names {X} (Talon of Pain's "{X}, {T}, Remove X
// charge counters": gorge offered only X = 0 with no charge counters, and
// XMage's announceX still strict-missed "Announce the value for {X} (any
// value)"). The mapped count is one per announcing cost: a VariableManaCost
// and a separate VariableCostImpl each ask (Ludevic, Necrogenius's {X} plus
// ExileXFromYourGraveCost), while a counter-removal cost that reuses the mana
// X (Talon) counts once. Craft/XMin is a keyword, not an activation cost, and
// stays out.
func activationAnnouncesX(d rules.OracleDecision, activates map[int]int) bool {
	return activates[d.Step] > 0 && announcesXShape(d)
}

// announcesXAtStep is the shared shape of a forced single-pick X announcement
// on an owned step.
func announcesXAtStep(d rules.OracleDecision, steps map[int]bool) bool {
	return steps[d.Step] && announcesXShape(d)
}

// announcesXShape is the forced single-pick X announcement, never the forced
// one-option label asks these predicates exist to protect.
func announcesXShape(d rules.OracleDecision) bool {
	return d.Kind == "choose_n" && len(d.Picks) == 1 && pickKind(d, 0) == "x" &&
		strings.HasPrefix(d.Picks[0], "X = ")
}

// announcesSacrificeX reports a forced positive "X = <n>" announcement that
// sizes a Sac<X/...> additional cost: the same seat's next decision in the
// step picks exactly n objects to sacrifice (Rottenmouth Viper's "sacrifice
// any number of nonland permanents", X = the count). Gorge offers only the X
// the pool can then afford, but XMage's announce ranges over every
// sacrificeable count and its AI picks its own (measured: X = 0, the cost
// paid with Sol Ring's mana and nothing sacrificed), leaving the scripted
// sacrifice unused. Such an announce is scripted.
func announcesSacrificeX(ds []rules.OracleDecision, i int) bool {
	d := ds[i]
	if len(d.Picks) != 1 || pickKind(d, 0) != "x" || !strings.HasPrefix(d.Picks[0], "X = ") {
		return false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(d.Picks[0], "X = "))
	if err != nil || n <= 0 || i+1 >= len(ds) {
		return false
	}
	next := ds[i+1]
	if next.Step != d.Step || next.Seat != d.Seat || len(next.Picks) != n {
		return false
	}
	for k := range next.Picks {
		if pickKind(next, k) != "sacrifice" {
			return false
		}
	}
	return true
}

// forcedChoicePosed reports whether a forced one-option choose_n ask is still
// posed by XMage's makeChoose (Unstable Glyphbridge's "choose a creature").
// A ChangeTargets redirect shares the same decision shape (Resume "choice")
// but is not a choice ask: XMage retargets through chooseTarget, which
// auto-selects a sole candidate in non-strict mode and reads the target queue
// otherwise, so a scripted choice would never be consumed. The redirect is
// told apart by the earlier cast that targeted a stack object (Bolt Bend,
// Redirect Lightning).
func forcedChoicePosed(ds []rules.OracleDecision, i int) bool {
	d := ds[i]
	if d.Kind != "choose_n" || d.Resume != "choice" {
		return false
	}
	for _, e := range ds[:i] {
		if e.Kind != "target" || e.Step >= d.Step {
			continue
		}
		for k := range e.Picks {
			switch pickKind(e, k) {
			case "spell", "ability", "trigger":
				return false
			}
		}
	}
	return true
}

// unlessPolarity is the yes/no answer to an unless-pay boolean ask: the
// picked option's label says whether the cost was paid. A decline wording
// ("Don't pay", "Decline") is "no"; a payment wording is "yes".
func unlessPolarity(d rules.OracleDecision) string {
	if len(d.Picks) == 0 {
		return "no"
	}
	l := strings.ToLower(strings.TrimSpace(d.Picks[0]))
	for _, neg := range []string{"don't", "do not", "decline", "no", "refuse"} {
		if strings.HasPrefix(l, neg) {
			return "no"
		}
	}
	// Any other label names the cost being paid ("Pay {2}", "Sacrifice an
	// artifact").
	return "yes"
}

// modeNumberFor resolves the k-th pick's 1-based XMage mode number from its
// label. ok is false when the pick's label names no charm mode -- a
// non-charm modal or a discard-style picker sharing the "mode" kind; the
// caller then falls back to the option's own 1-based position. A label that
// names a SetChosenMode$ True GenericChoice returns ModeChoiceQueue, telling
// the caller to answer on XMage's choice queue instead of the mode queue.
func modeNumberFor(d rules.OracleDecision, k int, modes map[string]int) (int, bool) {
	if k < 0 || k >= len(d.Picks) {
		return 0, false
	}
	m, ok := modes[d.Picks[k]]
	return m, ok
}

// allChoiceQueue reports whether every pick in a decision reaches XMage's
// choice queue (makeChoose). A decision with a target or skipped pick is not
// one makeChoose dialog and must not be joined with '^'. It asks pickQueue,
// the same per-pick route the answer emission uses, so every shape-gated route
// (a fixed-size exile cost, a Craft material cost) is joined or kept separate
// exactly as its answers are.
func allChoiceQueue(d rules.OracleDecision, craft bool) bool {
	if d.Retarget {
		return false
	}
	if hasExileCostPick(d) && !(d.Min > 0 && d.Min == d.Max) {
		// An open-ended exile cost (a group exile's "one or more") settles one
		// card per TargetImpl.choose iteration, so each pick is its own
		// makeChoose record; joining them would leave all but the first card
		// unexiled. A fixed-size selection (min == max) is the one joined
		// record.
		return false
	}
	for k, label := range d.Picks {
		if pickQueue(d, k, label, false, craft) != "choice" {
			return false
		}
	}
	return true
}

// perPlayerTargetAnswers answers a TargetsForEachPlayer$ ask: one target for
// each seat in the match, in seat order starting at 0, using the pick that
// seat's controller made or a target skip when the seat chose none. XMage's
// ForEachPlayerTargetsAdjuster poses every seat's ask, even a seat with no
// legal candidate.
func perPlayerTargetAnswers(d rules.OracleDecision) []XAnswer {
	seatPick := map[int]string{}
	for _, ref := range d.PickRefs {
		s, ok := refSeat(ref)
		if !ok {
			continue
		}
		if _, dup := seatPick[s]; dup {
			continue
		}
		v := ref
		if !isSeat(ref) {
			v = oraclediffRefName(ref)
		}
		seatPick[s] = v
	}
	n := d.SeatCount
	if n < 1 {
		n = 1
	}
	out := make([]XAnswer, 0, n)
	for s := 0; s < n; s++ {
		if v, ok := seatPick[s]; ok {
			out = append(out, XAnswer{d.Seat, "target", v})
		} else if s == d.Seat && d.PerOpponent {
			// The controller's own seat without a pick: the corpus's
			// per-player targets are "for each opponent" (Celebrate the
			// Mountain-king, Omega, Riptide Gearhulk), which XMage never
			// asks the controller for -- a skip here would be consumed as
			// the opponent's answer (measured on the std pass).
			continue
		} else {
			out = append(out, XAnswer{d.Seat, "target", "[target_skip]"})
		}
	}
	return out
}

// refSeat returns the seat a scenario ref belongs to ("p1:Grizzly Bears" or
// "p1" -> 1). It is how a per-player target answer finds the seat whose ask
// the pick answers.
func refSeat(ref string) (int, bool) {
	if !strings.HasPrefix(ref, "p") {
		return 0, false
	}
	r := ref[1:]
	if i := strings.IndexByte(r, ':'); i >= 0 {
		r = r[:i]
	}
	n, err := strconv.Atoi(r)
	if err != nil {
		return 0, false
	}
	return n, true
}

// targetDecisionAnswers scripts one target decision. A TargetsForEachPlayer$
// ask is answered per seat, a DividedAsYouChoose$ ask in XMage's divided
// "<ref>^X=<share>" form, anything else one answer per pick. Every form is
// followed by a target skip for each "up to N" chain slot the engine settled
// without posing it (XMage still asks those), and preceded by one for each
// such slot settled BEFORE this ask (leadingUnposedSkips).
func targetDecisionAnswers(r *answerRouting, i int) []XAnswer {
	d := r.ds[i]
	as := leadingUnposedSkips(d)
	switch {
	case d.Resume == "copy_targets" && d.Divided == 0 && !d.PerPlayer:
		// A copy's CR 707.10c new-target election: XMage asks one chooseUse
		// per inherited target before the pick, so this is not one target
		// dialog and never takes the trailing "up to N" skip below. A divided
		// (TargetAmount) or per-player copy keeps its existing branch: the
		// per-slot replace stream for those is unproven, so it is left alone.
		as = append(as, copyTargetAnswers(d)...)
	case d.PerPlayer:
		// TargetsForEachPlayer$ (CR 601.2c): XMage asks one target
		// per player, in seat order starting at seat 0. Answer each
		// seat with the pick its controller made, or a target skip
		// when the seat chose none.
		as = append(as, perPlayerTargetAnswers(d)...)
	case d.Divided > 0 && len(d.PickRefs) > 0:
		// A TargetAmount slot (distribute counters, divided damage): XMage's
		// chooseTargetAmount takes one "<ref>^X=<share>" per target and
		// completes when the allocation is answered.
		as = append(as, dividedTargetAnswers(r, i)...)
	default:
		for k := range d.PickRefs {
			as = append(as, singleTargetAnswer(d, k))
		}
		if len(d.PickRefs) < d.Max && !poolExhausted(d) {
			// Fewer picks than the "up to N" ask allows: XMage keeps
			// asking, so stop it with the target queue's skip token. This
			// also covers a slot gorge never posed at all (zero picks).
			as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
		}
	}
	for n := 0; n < d.UnposedSlots; n++ {
		as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
	}
	return as
}

// poolExhausted reports an ask whose picks took every candidate gorge offered:
// XMage completes such an object by itself (TargetImpl.isChoiceCompleted's
// moreSelectCount == 0), so a closing skip would be left unused ("Targets ...
// Count are not equal (found [[target_skip]])": Eddymurk Crab's "up to two
// target creatures" with itself the only creature). A decision that records
// no candidate count keeps the skip.
func poolExhausted(d rules.OracleDecision) bool {
	return d.Options > 0 && d.Options <= len(d.PickRefs)
}

// singleTargetAnswer is the TARGET-queue answer for one pick: a seat ref, an
// exact identity ref (a same-name alias, a permanent's current face, or an
// inexact ref's exact owner prefix), or the ref's name with a same-name
// marker. It is the one home for that spelling, shared by the ordinary target
// ask and a copy's per-slot replacement, so the two cannot drift.
func singleTargetAnswer(d rules.OracleDecision, k int) XAnswer {
	ref := d.PickRefs[k]
	if isSeat(ref) {
		return XAnswer{d.Seat, "target", ref}
	}
	// XMage's chooseTarget parses the same copy marker as makeChoose and
	// filters on isCopy(), so a target that shares its name with another
	// offered object needs the discriminator here too (a token copy of a
	// card, Extravagant Replication). An alias pick keeps its full ref, which
	// the driver's targetName maps to the object's @alias.
	p := ClassifySameName(d, k)
	if p.Alias != "" {
		return XAnswer{d.Seat, "target", ref}
	}
	if p.Unanswerable {
		// The ref is rank-derived, but its owner prefix may still separate
		// the candidates (two same-named cards in different graveyards). The
		// driver's alias binding scopes a non-token ref to that seat, so
		// "<seat>:<name>" reaches the picked card instead of the first
		// same-named one XMage matches by name alone (CR 601.2c).
		if owner := ownerQualifiedRef(d, k); owner != "" {
			return XAnswer{d.Seat, "target", owner}
		}
	}
	if currentFaceTargetRef(d, k) {
		// The option label names a different object than the ref -- the
		// permanent's current face (a transformed DFC, a face-down card). Its
		// current name selects the live object when no other option shares
		// it; else the ref's identity alias.
		if name, ok := uniqueCurrentFaceName(d, k); ok {
			return XAnswer{d.Seat, "target", name}
		}
		return XAnswer{d.Seat, "target", ref}
	}
	_, marker := SameNameAmbiguity(d, k)
	return XAnswer{d.Seat, "target", oraclediffRefName(ref) + marker}
}

// dividedTargetAnswers answers a divided target ask with each pick's share of
// the total. A divided target ask with several recipients is paired with the
// engine's own damage_split decision (that split's recipients are exactly the
// ask's), whose shares are then emitted here -- at the ask's own queue
// position, ahead of the chain skips its caller appends. Counters have no such
// decision: the engine deals them round-robin over the picks.
func dividedTargetAnswers(r *answerRouting, i int) []XAnswer {
	d := r.ds[i]
	n := len(d.PickRefs)
	if j, ok := r.targetSplit[i]; ok {
		return damageSplitAnswers(r.ds[j])
	}
	if j, ok := r.targetDivide[i]; ok {
		// The announced division: every target takes 1, and each pick of
		// the division ask adds 1 to the target at that option index.
		as := make([]XAnswer, 0, n)
		for k, ref := range d.PickRefs {
			share := 1
			for _, p := range r.ds[j].PickIdx {
				if p == k {
					share++
				}
			}
			as = append(as, XAnswer{d.Seat, "target", ref + "^X=" + strconv.Itoa(share)})
		}
		return as
	}
	as := make([]XAnswer, 0, n)
	for k, ref := range d.PickRefs {
		share := d.Divided / n
		if k < d.Divided%n {
			share++
		}
		if share == 0 {
			continue
		}
		// Keep the scenario ref (including #N), as the damage split does.
		as = append(as, XAnswer{d.Seat, "target", ref + "^X=" + strconv.Itoa(share)})
	}
	return as
}

// damageSplitAnswers turns a "damage_split" KChoose into one target answer
// per chosen target, its Value the ref plus "^X=<share>". The engine's split
// answer is a multiset over option indexes: a target receiving k damage has
// its index repeated k times, and PickIdx/Picks/PickRefs are parallel. Targets
// are emitted in first-appearance order of the option index, exactly as the
// engine assigns them.
func damageSplitAnswers(d rules.OracleDecision) []XAnswer {
	share := map[int]int{}
	var order []int
	for _, i := range d.PickIdx {
		if _, seen := share[i]; !seen {
			order = append(order, i)
		}
		share[i]++
	}
	ref := map[int]string{}
	for k, i := range d.PickIdx {
		if k < len(d.PickRefs) {
			ref[i] = d.PickRefs[k]
		}
	}
	as := make([]XAnswer, 0, len(order))
	for _, i := range order {
		name := ref[i]
		if name == "" {
			// A snapshot without refs: fall back to the option label, which
			// for a damage-split permanent is the card name.
			if i < len(d.Picks) {
				name = d.Picks[i]
			}
		}
		// Keep the scenario ref (including #N): XMage's targetName uses it
		// to resolve two same-name permanents to distinct setup aliases.
		as = append(as, XAnswer{d.Seat, "target", name + "^X=" + strconv.Itoa(share[i])})
	}
	return as
}

// manaAllocationAnswers turns a "mana_color" allocation (Min==Max>1) into one
// amount answer per WUBRG colour, zeroes included: XMage's
// getMultiAmountWithIndividualConstraints iterates the effect's manaSymbols in
// ColoredManaSymbol order and requires an "X=<n>" for each. A pick's colour is
// its option label ("Add W", the authority), falling back to its index mod 5
// (the engine/effects/mana_effect.go layout: unit*5 + colourIndex).
func manaAllocationAnswers(d rules.OracleDecision) []XAnswer {
	counts := map[byte]int{}
	for k := range d.Picks {
		if code, ok := allocationColour(d, k); ok {
			counts[code]++
		}
	}
	as := make([]XAnswer, 0, 5)
	for _, code := range "WUBRG" {
		as = append(as, XAnswer{d.Seat, "amount", strconv.Itoa(counts[byte(code)])})
	}
	return as
}

// allocationColour names the WUBRG colour one picked option allocates.
func allocationColour(d rules.OracleDecision, k int) (byte, bool) {
	if k < len(d.Picks) {
		if label := d.Picks[k]; strings.HasPrefix(label, "Add ") && len(label) == 5 {
			if strings.IndexByte("WUBRG", label[4]) >= 0 {
				return label[4], true
			}
		}
	}
	if k < len(d.PickIdx) {
		return "WUBRG"[d.PickIdx[k]%5], true
	}
	return 0, false
}

// tokenChoiceNames returns a GenericChoice's Choices$ names when there are at
// least two and every one names a DB$ Token body on the face, else nil.
func tokenChoiceNames(f *cards.Face, choices string) []string {
	var names []string
	for _, name := range strings.Split(choices, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if svarParams(f.SVars[name])["DB"] != "Token" {
			return nil
		}
		names = append(names, name)
	}
	if len(names) < 2 {
		return nil
	}
	return names
}

// yesNo recognises a bare two-way boolean choice. The engine's option kind
// and exact label/ref identity must both agree; composed choices such as

// ModeChoiceQueue is the sentinel position modeNumbers stores for a mode
// label that reaches XMage through its CHOICE queue (controller.choose)
// rather than the numeric mode queue: the Choices$ label of a
// DB$ GenericChoice | SetChosenMode$ True body (the Theros-style Sieges).
// XMage's ChooseModeEffect does controller.choose(Outcome.Neutral, Choice,
// game), which shows the option LABEL; chooseMode/setModeChoice (the numeric
// queue) is never posed for it. A real charm position is 1-based, so 0 is
// never a valid mode number and is unambiguous.
const ModeChoiceQueue = 0

// ModeYesQueue and ModeNoQueue are the sentinels for the two labels of a
// GenericChoice XMage poses as player.chooseUse (the yes/no choice queue),
// never as a mode: a DB$ GenericChoice | AILogic$ Timetwister body (Turtles in
// Time, a per-player "may shuffle your hand and graveyard" ask) and every
// two-option face in xmageChooseUseFaces (choice_dialog.go, e.g. Terrapact
// Intimidator's "target opponent may have you create two Lander tokens").
// Choices$ lists the yes label first, the no label second. Negative, so
// neither collides with a real 1-based charm position nor with
// ModeChoiceQueue.
const (
	ModeYesQueue = -1
	ModeNoQueue  = -2
)

// ModeKWChoiceQueue is the sentinel for a Pump KWChoice$ pick ("gains your
// choice of X, Y or Z"). XMage's GainsChoiceOfAbilitiesEffect builds one
// ChoiceImpl over the candidate abilities' RULE TEXTS and reads it from the
// CHOICE queue by label; the numeric mode queue never consumes that answer
// (measured on Lunar Avenger: "Missing CHOICE def ... Choose an ability to
// gain", with the scripted mode 1 left unused). Negative like the other queue
// sentinels, so it never collides with a real 1-based charm position.
const ModeKWChoiceQueue = -3

// ModeKWBespokeChoiceQueue is the sentinel for a Pump KWChoice$ pick on a face
// whose XMage class builds its own Choice dialog with its own labels
// (xmageKWChoiceBespokeFaces: Angelic Skirmisher's "Choose ability for your
// creatures" over "First strike", "Vigilance", "Lifelink"). Still the CHOICE
// queue, but the answer is the XMage label, not the keyword rule text.
const ModeKWBespokeChoiceQueue = -4

// modeNumbers maps each charm mode's label (as gorge's mode decision
// shows it) to its 1-based position in its Choices$ list, for every Charm
// on the face -- the spell's own and any modal trigger's. It ALSO carries
// every DB$ GenericChoice | SetChosenMode$ True label and every source-proven
// Pump KWChoice$ label, mapped to the ModeChoiceQueue / ModeKWChoiceQueue
// sentinels: those picks are the same "mode" decision kind to gorge but a
// different XMage queue, and no in-band discriminator on the decision
// separates them (a mid-resolution Charm shares Resume "modes"), so the face
// shape is the authority.
// printedMaxPrefix marks the ModeNumbers entries that carry a charm's PRINTED
// maximum rather than a mode number (see printedMaxKey).
const printedMaxPrefix = "\x00printed_max\x00"

// printedMaxKey is the ModeNumbers key under which a charm mode label carries
// its printed CharmNum$ (stored negated, so the value can never be mistaken
// for a 1-based mode number or a queue sentinel). gorge clamps a mode
// decision's Max to the modes it can offer (CR 700.2a), but XMage's
// Modes.choose keeps asking up to the printed maximum, so the closing
// mode skip must be keyed on the printed bound. The label is the only handle
// a decision gives back to its card, hence one entry per mode label.
func printedMaxKey(label string) string { return printedMaxPrefix + label }

// IsPrintedMaxKey reports whether a ModeNumbers key is a printed-maximum
// entry rather than a mode label.
func IsPrintedMaxKey(k string) bool { return strings.HasPrefix(k, printedMaxPrefix) }

// printedCharmMax is the literal CharmNum$ of a non-repeating Charm (0 when
// absent, non-literal or repeatable: those keep the decision's own Max).
func printedCharmMax(params map[string]string) int {
	if strings.EqualFold(strings.TrimSpace(params["CanRepeatModes"]), "True") {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(params["CharmNum"]))
	if err != nil || n < 2 {
		return 0
	}
	return n
}

// printedModeMax is the printed maximum XMage will loop a mode ask to for the
// decision's picked cards (0 when none of the picks carries one).
func printedModeMax(d rules.OracleDecision, modes map[string]int) int {
	for _, label := range d.Picks {
		if n, ok := modes[printedMaxKey(label)]; ok && n < 0 {
			return -n
		}
	}
	return 0
}

func modeNumbers(f *cards.Face) map[string]int {
	out := map[string]int{}
	// A ChoiceRestriction$ Charm (Zuko, Conflicted's "choose one that hasn't
	// been chosen") is offered WITHOUT the modes already used, by gorge and
	// by XMage alike, and XMage's scripted mode number indexes the modes still
	// on offer, not Choices$: Zuko's second ask offers three modes and XMage
	// resolved the scripted absolute "2" as the second REMAINING mode (Add
	// {R}), not the second printed one. Such a charm's labels stay out of the
	// map, so the answer falls to the pick's 1-based position in the offered
	// list -- the number XMage's chooseMode reads.
	addCharm := func(choices, restriction string, params map[string]string) {
		if restriction != "" {
			return
		}
		printed := printedCharmMax(params)
		for i, name := range strings.Split(choices, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			label := effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)
			out[label] = i + 1
			if printed > 0 {
				out[printedMaxKey(label)] = -printed
			}
		}
	}
	for _, body := range f.SVars {
		p := svarParams(body)
		if p["DB"] != "GenericChoice" || !strings.EqualFold(p["AILogic"], "Timetwister") {
			continue
		}
		for i, name := range strings.Split(p["Choices"], ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			sentinel := ModeNoQueue
			if i == 0 {
				sentinel = ModeYesQueue
			}
			out[effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)] = sentinel
		}
	}
	// "Create your choice of a <token> or a <token>" (a DB$ GenericChoice
	// whose every Choices$ entry is a DB$ Token body, no SetChosenMode$ and
	// no AILogic$ Timetwister): XMage writes the card's own OneShotEffect,
	// never a modal ability. Measured over the 20 Standard sets' four
	// carriers: the two-token form is player.chooseUse with the first token
	// as the yes label (Tireless Provisioner, Ant-Man's Army: "Food" /
	// "Treasure"), the three-token form a ChoiceImpl over the token names
	// (Transmutation Font's Blood/Clue/Food, The Third Doctor's
	// Clue/Food/Treasure), whose labels are the bodies' SpellDescription$.
	// The choice is an activated ability's own body (Transmutation Font's
	// AB$ GenericChoice) or a trigger's DB$ SVar body (Tireless Provisioner).
	var tokenChoices []map[string]string
	for _, sa := range f.Abilities {
		if sa.API == "GenericChoice" {
			tokenChoices = append(tokenChoices, sa.Params)
		}
	}
	for _, body := range f.SVars {
		if p := svarParams(body); p["DB"] == "GenericChoice" {
			tokenChoices = append(tokenChoices, p)
		}
	}
	for _, p := range tokenChoices {
		if p["SetChosenMode"] != "" || strings.EqualFold(p["AILogic"], "Timetwister") {
			continue
		}
		names := tokenChoiceNames(f, p["Choices"])
		for i, name := range names {
			label := effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)
			switch {
			case len(names) > 2:
				out[label] = ModeChoiceQueue
			case i == 0:
				out[label] = ModeYesQueue
			default:
				out[label] = ModeNoQueue
			}
		}
	}
	// A two-option GenericChoice XMage poses as ONE boolean chooseUse
	// (choice_dialog.go): the first Choices$ label is the true branch and the
	// second the false branch, answered yes/no on the CHOICE queue for the
	// seat that chose (Terrapact Intimidator's targeted opponent). The seat
	// is already the decision's Seat; only the queue and the value change.
	if pairs, ok := chooseUseLabels(f); ok {
		for _, pair := range pairs {
			out[pair[0]] = ModeYesQueue
			out[pair[1]] = ModeNoQueue
		}
	}
	// The SetChosenMode labels first, so a real Charm mode of the same
	// label (a corpus impossibility) would keep its numeric position below.
	for _, body := range f.SVars {
		p := svarParams(body)
		if p["DB"] != "GenericChoice" || !strings.EqualFold(p["SetChosenMode"], "True") {
			continue
		}
		for _, name := range strings.Split(p["Choices"], ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out[effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)] = ModeChoiceQueue
		}
	}
	// A plain GenericChoice XMage poses as a Choice dialog over the labels
	// (choice_dialog.go): the choice queue, by label, like SetChosenMode.
	for _, label := range choiceDialogLabels(f) {
		if _, taken := out[label]; !taken {
			out[label] = ModeChoiceQueue
		}
	}
	// A Pump KWChoice$ ("gains your choice of X, Y or Z") is XMage's
	// GainsChoiceOfAbilitiesEffect: one ChoiceImpl over the candidate
	// abilities' RULE TEXTS, read from the CHOICE queue by label
	// (choice_dialog.go). The answer is the keyword's lower-case rule text,
	// not gorge's KWChoice$ spelling ("Flying" -> "flying", "Rampage:3" ->
	// "rampage 3"), so it gets its own sentinel.
	for _, label := range kwChoiceDialogLabels(f) {
		if _, taken := out[label]; !taken {
			out[label] = ModeKWChoiceQueue
		}
	}
	// The bespoke-dialog KWChoice$ faces: same queue, XMage's own labels.
	for _, label := range kwChoiceBespokeLabels(f) {
		if _, taken := out[label]; !taken {
			out[label] = ModeKWBespokeChoiceQueue
		}
	}
	for _, sa := range f.Abilities {
		if sa.API == "Charm" {
			addCharm(sa.Params["Choices"], sa.Params["ChoiceRestriction"], sa.Params)
		}
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "Charm") {
			if c := svarParams(body)["Choices"]; c != "" {
				addCharm(c, svarParams(body)["ChoiceRestriction"], svarParams(body))
			}
		}
	}
	return out
}

// yesNo recognises a bare two-way boolean choice. The engine's option kind
// and exact label/ref identity must both agree; composed choices such as
// "Yes — discard" are ordinary makeChoose picks, not boolean answers. Older
// snapshots without PickKinds use the same exact-label/ref rule. The one
// composed exception is the unless-discard election (kind "unless" "Yes —
// discard one <spec>" / kind "ordinary" "No — discard normally"): XMage
// models it as DoIfCostPaid, so it is a chooseUse yes/no.
func yesNo(d rules.OracleDecision) (string, bool) {
	if (d.Options != 2 && !loneTriggerCostDecline(d)) || len(d.Picks) != 1 || len(d.PickRefs) != 1 {
		return "", false
	}
	switch kind := pickKind(d, 0); kind {
	case "trigger_cost_decline":
		// The "do not pay" side of a triggered optional cost is XMage's
		// DoIfCostPaid chooseUse answered no; its AI defaults to yes and
		// would pay what gorge declined.
		return "no", true
	case "yes", "no":
		// The engine's own boolean option ("Yes — shuffle", a may
		// trigger): XMage's chooseUse.
		return kind, true
	case "gift_promise":
		// The promise side is chooseUse yes whichever option index the
		// engine listed it at (Jolly Gerbils' Blooming Blast offers "Don't
		// promise a gift" first, so the promise is option 1).
		return "yes", true
	case "gift_decline":
		return "no", true
	case "unless":
		// "Discard one <spec> instead" (Thirst for Discovery): XMage's
		// DoIfCostPaid chooseUse before the DiscardCardCost target, so the
		// elected side is chooseUse yes and the card pick follows as the
		// cost's own choice. The label is never a literal choice.
		return "yes", true
	case "ordinary":
		// The same ask's decline side ("No — discard normally"): chooseUse no.
		if d.Picks[0] == "No — discard normally" {
			return "no", true
		}
	case "endure_counters":
		// Endure is XMage's chooseUse "Put N +1/+1 counters on
		// <it> or create a N/N Spirit token?" (EndureSourceEffect.doEndure,
		// true text "Add counters"); TestPlayer reads only Yes/No for a
		// text-choice chooseUse ("Use Yes/No in unit tests for text
		// choices"), so the label strict-missed (Dusyut Earthcarver,
		// Inspirited Vanguard).
		return "yes", true
	case "endure_spirit":
		return "no", true
	case "altaddcost", "primary":
		// An optional additional cost, a gift promise and a two-way
		// primary/secondary pick are chooseUse asks in XMage too
		// (measured: Silence the Echo, Kitnap, Lost in Space agree only
		// with a yes/no answer). Option 0 is the "do it" side.
		l := strings.ToLower(d.Picks[0])
		for _, neg := range []string{"do not", "don't", "no", "decline", "skip"} {
			if strings.HasPrefix(l, neg) {
				return "no", true
			}
		}
		if len(d.PickIdx) == 1 && d.PickIdx[0] == 0 {
			return "yes", true
		}
		return "no", true
	case "":
		// A snapshot without PickKinds: only an exact Yes/No label.
		label := strings.ToLower(strings.TrimSpace(d.Picks[0]))
		if d.Picks[0] == d.PickRefs[0] && (label == "yes" || label == "no") {
			return label, true
		}
	}
	return "", false
}

// bandAnswers scripts a band declaration for XMage's Combat.handleBanding:
// a chooseUse for each member, then that member's card selection. Gorge records
// one band pick per accepted member ("pN:<creature>") and one per decline (the
// "No more creatures in <leader>'s band" label), so the pick's shape, not its
// label text, decides the answer. Keyed on the engine pick kind "band": the next
// band-declaration flavour (a future "bands with other") takes the same route
// without another label census.
//
// The member is a kind "choice", not "target": XMage reaches the band's
// TargetControlledPermanent through Target.choose -> Player.choose, whose
// TestPlayer answer is makeChoose on the CHOICE queue. A "target" answer would
// go to the separate targets queue that chooseTarget reads, and makeChoose
// would consume the next band's yes/no instead (measured: "Found wrong choice
// command ... No ... Select an attacking creature to band with").
//
// The member value is the same-name-aware object choice, not the raw scenario
// ref. XMage's band target filters out the leader, so a plain name is unique
// among the remaining candidates; a raw ref is not. When two same-name copies
// attack, registerAliases can bind a scenario ref to the copy that is the
// current leader (its occurrence count follows battlefield iteration, not
// setup order), and the target then selects nothing: measured on the SC15
// certdecks as "Found wrong choice command (invalid target ...):
// @p0:Kjeldoran Warrior ... selected 0, more possible 2" for the second
// copy's band ask. disambiguatedObjectChoice keeps the exact-ref alias for a
// genuinely ambiguous same-name pick and the plain name otherwise.
func bandAnswers(d rules.OracleDecision) ([]XAnswer, bool) {
	if len(d.PickKinds) == 0 {
		return nil, false
	}
	for _, kind := range d.PickKinds {
		if kind != "band" {
			return nil, false
		}
	}
	as := make([]XAnswer, 0, len(d.Picks)*2)
	for k := range d.Picks {
		ref := ""
		if k < len(d.PickRefs) {
			ref = d.PickRefs[k]
		}
		if isScenarioRefShaped(ref) {
			memberName := bandMemberLiveName(d, k)
			if memberName == "" {
				memberName = oraclediffRefName(ref)
			}
			// The label's member field is the object's live face by
			// construction, so it names the pick even when it differs from
			// the ref's setup name (a transformed DFC): the same-name alias
			// and the owner-qualified ref must both stay available despite
			// namesObject comparing against the setup name.
			member := disambiguatedObjectChoiceNamed(d, k, memberName, true)
			as = append(as, XAnswer{d.Seat, "choice", "yes"}, XAnswer{d.Seat, "choice", member})
			continue
		}
		as = append(as, XAnswer{d.Seat, "choice", "no"})
	}
	return as, true
}

// bandMemberLiveName returns the accepted member's LIVE name from pick k's
// label "Band <member> with <leader>", or "" when the label has another shape.
// The pick's ref keeps the object's setup identity -- a transform DFC's front
// face -- while the label is built from the object's current face
// (rules/banding.go askNextBand, CR 712.8). Answering the ref's front name
// selects nothing once the DFC has transformed (Kessig Prowler // Sinuous
// Predator: "Band Sinuous Predator with Timber Wolves" over ref
// p0:Kessig Prowler). The leader's live name comes from option 0's decline
// label ("No more creatures in <leader>'s band"), so a " with " inside either
// card name cannot split the member wrongly. Without that label (a synthetic
// decision) a single " with " is the separator; two or more may belong to the
// leader's own name ("Dance with Devils"), so the fallback fails closed to "".
func bandMemberLiveName(d rules.OracleDecision, k int) string {
	if k < 0 || k >= len(d.Picks) {
		return ""
	}
	member, ok := strings.CutPrefix(d.Picks[k], "Band ")
	if !ok || member == "" {
		return ""
	}
	if leader := bandLeaderLiveName(d.First); leader != "" {
		if name, ok := strings.CutSuffix(member, " with "+leader); ok && name != "" {
			return name
		}
	}
	if strings.Count(member, " with ") == 1 {
		if i := strings.Index(member, " with "); i > 0 {
			return member[:i]
		}
	}
	return ""
}

// bandLeaderLiveName extracts <leader> from a band ask's decline label.
func bandLeaderLiveName(decline string) string {
	const prefix, suffix = "No more creatures in ", "'s band"
	leader, ok := strings.CutPrefix(decline, prefix)
	if !ok {
		return ""
	}
	leader, ok = strings.CutSuffix(leader, suffix)
	if !ok {
		return ""
	}
	return leader
}

// loneTriggerCostDecline is a triggered-cost ask offered with only its
// decline option (engine/rules/cumulative.go: the cost is unpayable for gorge),
// answered with it, over a cost XMage's DoIfCostPaid asks about regardless
// (rules.OracleDecision.CostPosed). A decline-only ask over any other cost is
// not posed by XMage and stays forced away.
func loneTriggerCostDecline(d rules.OracleDecision) bool {
	return d.CostPosed && d.Kind == "choose_n" && d.Options == 1 && len(d.Picks) == 1 &&
		len(d.PickKinds) == 1 && d.PickKinds[0] == "trigger_cost_decline"
}

// hasTargetPick reports whether any picked option reaches XMage's target
// queue (a real TargetXxx), which XMage poses even when the engine offered
// exactly one legal option.
func hasTargetPick(d rules.OracleDecision) bool {
	for k := range d.Picks {
		switch pickKind(d, k) {
		case "permanent", "player", "opponent_choice":
			return true
		}
	}
	return false
}

// pickKind is the engine option kind of the k-th pick. A snapshot written
// before PickKinds existed returns "", which every caller treats as the
// choice queue.
func pickKind(d rules.OracleDecision, k int) string {
	if k >= 0 && k < len(d.PickKinds) {
		if (d.Resume == "opp_pick" || d.Resume == "choice") && d.PickKinds[k] == "player" {
			// The TargetingPlayer$ Opponent flow's controller-facing
			// which-opponent ask: XMage's ChoicePlayer, the choice queue.
			return "opponent_choice"
		}
		return d.PickKinds[k]
	}
	return ""
}

// xmQueue says which TestPlayer queue an engine option kind reaches. The
// choice queue is makeChoose/setChoice -- XMage's choose(Cards, TargetCard),
// choose(Choice) and choose(ChoicePlayer) all land there. The target queue is
// addTarget/chooseTarget, reached by a real TargetXxx. "skip" is for a pick
// XMage never asks TestPlayer about: a library search (TestPlayer.searchLibrary
// delegates to the computer player, exactly as doSurveil does) and a
// mana-tapping cost (paid from the pool). The engine option kind, not the
// label text, is the authority -- the mechanism the census test pins.
func xmQueue(kind, label string) string {
	switch kind {
	case "search":
		// A library search (TargetCardInLibrary) is answered from the target
		// queue: measured on the std pass, 47 search carriers (Shared Roots,
		// Nature's Rhythm, Solemn Simulacrum, ...) agree only with a target
		// answer.
		return "target"
	case "exilecost":
		// An exile COST is not a TargetXxx target selection:
		// ExileFromGraveCost/ExileTargetCost/ExileFromHandCost (and
		// ExileXFromYourGraveCost's generated ExileFromGraveCost) all pay with
		// getTargets().choose() -> TargetImpl.choose -> player.choose, which
		// TestPlayer serves from makeChoose, the CHOICE queue (Ludevic's
		// ExileXFromYourGraveCost left a target answer unused). A fixed-size
		// selection is one joined makeChoose record; an open-ended group exile
		// is one record per card (allChoiceQueue keeps it unjoined). Craft's
		// material is the sole target-queue exception, marked by craft in
		// pickQueue (craft_cost_pick.go). The census pins this kind mapping.
		return "choice"
	case "trigger_cost_pay", "trigger_cost_pay_manual":
		return "skip"
	case "mana":
		if _, ok := manaColourLabel(label); ok {
			// A mana ability's "add one mana of any colour" pick is a real
			// choice dialog; only a mana-tapping cost is paid silently.
			return "choice"
		}
		return "skip"
	case "permanent", "player":
		return "target"
	case "opponent_choice":
		return "choice"
	}
	return "choice"
}

// triggerOrderChoice is the XMage choice for pick k of a trigger-order
// decision: the pick's source object's plain NAME ("Adrenaline Jockey"),
// which XMage's chooseTriggeredAbility matches against the ability's source
// object name. Gorge's label carries the card's TriggerDescription$, which is
// XMage's rule wording only by coincidence ("put a +1/+1 counter on this
// creature" against XMage's "...put a +1/+1 counter on {this}"), so the text
// form misses and the replay's fallback player orders the stack itself (the
// Adrenaline Jockey/trigger#0.1 drift). A plain name, not a ref: the trigger
// ask resolves no aliases (TestPlayer's assertAliasSupportInChoices), so the
// driver's "@" alias form would fail the replay outright. A pick whose ref is
// not a scenario ref (an emblem or other sourceless trigger; PickRefs then
// carries the label) falls back to the label's rule text, the pre-name
// behaviour.
func triggerOrderChoice(d rules.OracleDecision, k int) string {
	if k < len(d.PickRefs) {
		if ref := d.PickRefs[k]; isScenarioRefShaped(ref) {
			return oraclediffRefName(ref)
		}
	}
	if k < len(d.Picks) {
		return triggerRule(d.Picks[k])
	}
	return ""
}

// triggerOrderNamesDistinct reports whether XMage can order the picks by their
// source object NAMES. XMage's chooseTriggeredAbility matches a choice against
// the source's name, and its ask loop auto-pushes the last remaining ability,
// so a source named more than once is only a problem when its picks are
// DIFFERENT abilities: two triggers of the same ability of the same object are
// interchangeable (CR 603.3b, their mutual order is unobservable). Collapse
// each (ref, rule text) pair -- one interchangeable ability -- and require at
// least two such groups whose source names are unique. The Harmonic Prodigy
// case is one group for Harmonic Prodigy's Prowess and one for Soul-Scar
// Mage's two Prowess triggers: the names order Prodigy against Soul-Scar, and
// the second Soul-Scar is the auto-pushed last pick. Thundertrap Trainer's ETB
// and Offspring share a ref but not a text, so they stay two groups with one
// name and take the rule-text form. A pick with no scenario ref falls back to
// text anyway, so it is skipped here (it cannot collide); an order with fewer
// than two scenario refs keeps that old text fallback. One pick is never
// ambiguous.
func triggerOrderNamesDistinct(d rules.OracleDecision) bool {
	if len(d.Picks) < 2 {
		return true
	}
	groups := map[string]bool{}
	var refs []string
	allRefs := true
	for k := range d.Picks {
		if k >= len(d.PickRefs) || !isScenarioRefShaped(d.PickRefs[k]) {
			allRefs = false
			continue
		}
		ref := d.PickRefs[k]
		key := ref + "\x00" + strings.TrimSpace(triggerRule(d.Picks[k]))
		if groups[key] {
			continue
		}
		groups[key] = true
		refs = append(refs, ref)
	}
	if len(refs) < 2 {
		// One named source is ambiguous only when every pick is a scenario ref
		// (two triggers of one object). A non-ref pick falls back to text, the
		// pre-name behaviour.
		return !allRefs
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		name := oraclediffRefName(ref)
		if name == "" {
			continue
		}
		if seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}

// isScenarioRefShaped reports whether s is a "p<seat>:<name>[#k]" object ref
// (the shape isScenarioRef on the driver accepts), not a bare label.
func isScenarioRefShaped(s string) bool {
	i := strings.IndexByte(s, ':')
	return i > 1 && s[0] == 'p' && strings.Trim(s[1:i], "0123456789") == ""
}

// IsScenarioRefShaped is the exported form of isScenarioRefShaped, for
// template answer writers that must never emit "@" before a non-ref label.
func IsScenarioRefShaped(s string) bool {
	return isScenarioRefShaped(s)
}

// trigSpan records where one trigger-order decision's name answers sit in a
// step's answer list, and the inert text answers that replace them when the
// step also scripts other asks.
type trigSpan struct {
	step  int
	start int
	count int
	old   []XAnswer
	// textBlind marks a span whose picks share rule text (two Prowess
	// triggers): the inert text form cannot tell them apart, so XMage orders
	// them itself and the extra text entry stays queued for a later ask.
	// Such a span keeps its source-name answers even on a shared step.
	textBlind bool
}

// triggerOrderTextBlind reports whether two picks carry the same rule text
// (after the source prefix is dropped), so only the source name can order them.
func triggerOrderTextBlind(d rules.OracleDecision) bool {
	seen := map[string]bool{}
	for _, label := range d.Picks {
		t := strings.TrimSpace(triggerRule(label))
		if seen[t] {
			return true
		}
		seen[t] = true
	}
	return false
}

// triggerOrderTextAnswers is the pre-name behaviour: the label's rule text
// for every pick. XMage's rule wording matches gorge's TriggerDescription$
// only by coincidence, so these are inert -- never consumed, never steering
// the order -- and against most dialogs unconsumable; NOT against a firing
// colour dialog, which pops the head and throws (measured: Ashling,
// Rekindled/trigger#1.1). manaHoistSpan replaces the span with the colour
// answer when the step later asks one.
func triggerOrderTextAnswers(d rules.OracleDecision) []XAnswer {
	out := make([]XAnswer, 0, len(d.Picks))
	for _, label := range d.Picks {
		out = append(out, XAnswer{d.Seat, "choice", triggerRule(label)})
	}
	return out
}

// demotedTriggerOrderAnswers is what a distinct-source trigger order demotes
// to on a step that scripts other asks too: XMage's rule-text keys
// (xmageTriggerOrderKeys) when the picks key apart, else the inert text form.
// A key is consumed only by the trigger ask it steers (TestPlayer's
// chooseTriggeredAbility matches getRule() by prefix) and names no object, so
// no other makeChoose takes it, which is the reason names are demoted at all.
// The verbatim text form is not inert in strict mode: the order ask finds no
// match and stops the replay at "Missing CHOICE def ... Triggered list"
// (Clement, the Worrywort beside Valley Mightcaller, whose step also scripts
// Clement's target; Harvestrite Host beside Hinterland Sanctifier).
func demotedTriggerOrderAnswers(d rules.OracleDecision) []XAnswer {
	if keys, ok := xmageTriggerOrderKeys(d); ok {
		out := make([]XAnswer, 0, len(keys))
		for _, key := range keys {
			out = append(out, XAnswer{d.Seat, "choice", key})
		}
		return out
	}
	return triggerOrderTextAnswers(d)
}

// demoteTriggerOrderSpans rewrites a step's trigger-order name answers back to
// the inert text form when the step's answer list also carries other asks'
// answers: a plain name is consumable by any later makeChoose (a target or
// order dialog), so a leftover one derails them, while the text form is not
// (measured: Baron Strucker/static#0.0, whose ETB order ask XMage poses at
// another point than gorge recorded, replays agree only with the text form).
// The text form is not inert against EVERY dialog: a firing colour dialog
// pops and throws on it, which is the manaHoistSpan routing's job. A step
// whose ONLY answers are trigger-order names keeps the name form: the
// names are consumed by the order ask itself and steer it. A span whose
// picks share rule text (trigSpan.textBlind) is never demoted: the text form
// cannot order identical triggers and leaves a stray answer queued
// (BLB Dawn's Truce with two Prowess triggers).
func demoteTriggerOrderSpans(out [][]XAnswer, spans []*trigSpan) {
	if len(spans) == 0 {
		return
	}
	byStep := map[int][]*trigSpan{}
	for _, sp := range spans {
		byStep[sp.step] = append(byStep[sp.step], sp)
	}
	steps := make([]int, 0, len(byStep))
	for step := range byStep {
		steps = append(steps, step)
	}
	sort.Ints(steps)
	for _, step := range steps {
		sp := byStep[step]
		if step >= len(out) {
			continue
		}
		covered := map[int]bool{}
		for _, s := range sp {
			for k := 0; k < s.count; k++ {
				covered[s.start+k] = true
			}
		}
		shared := false
		for i := range out[step] {
			if !covered[i] {
				shared = true
				break
			}
		}
		if !shared {
			continue
		}
		for i := len(sp) - 1; i >= 0; i-- {
			s := sp[i]
			if s.textBlind {
				continue
			}
			tail := append([]XAnswer(nil), out[step][s.start+s.count:]...)
			out[step] = append(out[step][:s.start], append(append([]XAnswer(nil), s.old...), tail...)...)
		}
	}
}

// triggerRule drops gorge's "<SourceName>: " prefix from a trigger-order
// label: XMage's chooseTriggeredAbility compares its choice against the
// ability's rule text (getRule), which carries no source prefix.
func triggerRule(label string) string {
	if i := strings.Index(label, ": "); i >= 0 {
		return label[i+2:]
	}
	return label
}

// manaColourLabel maps gorge's "Add W" mana option to the colour name
// XMage's colour chooser shows (its Choice key is "White", not "Add W"). A
// colour option of a costed mana ability names the cost first ("Pay 1: Add W",
// "Pay 2 life: Add W": pay.ManaAbilityCostPrefix), and XMage asks the same
// colour dialog for it, so the cost prefix is dropped before matching.
func manaColourLabel(label string) (string, bool) {
	if i := strings.LastIndex(label, ": "); i >= 0 {
		label = label[i+2:]
	}
	if strings.HasPrefix(label, "Add ") && len(label) == 5 {
		return manaColour(label[4])
	}
	return "", false
}

// bareColourLabel maps a bare colour word ("white", any case) to the
// capitalised colour name XMage's colour chooser shows ("White"). XMage's
// ChoiceColor keys are the names, while a resolution-time "choose a colour"
// decision records the picked option's label, which can reach a transcript as
// the bare word (Midvast Protector's ETB, Sygg, River Guide's protection
// grant). The gate is the engine's option KIND "color" -- the fixed WUBRG list
// every colour ask builds -- never the label text, so a card or object named
// "Red" is an object pick and keeps its label. A transcript written before
// PickKinds existed falls back to the ChooseColor resume kind, whose option
// list is the same fixed colour list.
func bareColourLabel(d rules.OracleDecision, k int) (string, bool) {
	if k < 0 || k >= len(d.Picks) {
		return "", false
	}
	if k < len(d.PickKinds) && d.PickKinds[k] != "" {
		if d.PickKinds[k] != "color" {
			return "", false
		}
	} else if d.Resume != "choosecolor" {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(d.Picks[k])) {
	case "white":
		return "White", true
	case "blue":
		return "Blue", true
	case "black":
		return "Black", true
	case "red":
		return "Red", true
	case "green":
		return "Green", true
	}
	return "", false
}

// colourPickCount counts the picks of a decision that name a single mana
// colour (a mana ability's colour option, with or without a cost prefix).
// When every pick of a multi-pick decision is such an option, XMage models
// the ask as a multi-amount distribution among colours, not one colour
// dialog, so the decision must not emit a joined "^" colour selection.
func colourPickCount(d rules.OracleDecision) int {
	n := 0
	for k, label := range d.Picks {
		if label == "" && k < len(d.PickRefs) {
			label = oraclediffRefName(d.PickRefs[k])
		}
		if _, ok := manaColourLabel(label); ok {
			n++
		}
	}
	return n
}

func manaColour(code byte) (string, bool) {
	switch code {
	case 'W':
		return "White", true
	case 'U':
		return "Blue", true
	case 'B':
		return "Black", true
	case 'R':
		return "Red", true
	case 'G':
		return "Green", true
	default:
		return "", false
	}
}

func payment(picks []string) bool {
	if len(picks) == 0 {
		return false
	}
	for _, p := range picks {
		if !strings.HasPrefix(p, "Pay ") {
			return false
		}
		// "Pay 1: Add W" is a costed mana ability's colour pick, a real
		// XMage colour dialog, not a hybrid/phyrexian payment half.
		if _, colour := manaColourLabel(p); colour {
			return false
		}
	}
	return true
}

// phyrexianLifePicks reports whether every pick is the two-life face of a
// Phyrexian pip (the engine's pay_life option, from pay.ManaAsk's
// announcement or a triggered cost's pipAsk). It is the ONE payment face
// XMage poses as a chooseUse ("Pay 2 life instead of {X}?"), so the generator
// must script it; every other flexible face (pay_W/pay_generic) is paid from
// the pool without a question.
func phyrexianLifePicks(d rules.OracleDecision) bool {
	if len(d.Picks) == 0 {
		return false
	}
	for k := range d.Picks {
		if pickKind(d, k) != "pay_life" {
			return false
		}
	}
	return true
}

func isSeat(s string) bool {
	return len(s) >= 2 && s[0] == 'p' && strings.Trim(s[1:], "0123456789") == ""
}

// SameNamePick describes one object pick that shares its name with another
// offered object, and the XMage answer that selects it exactly.
//
// Ambiguous is set only when the pick and a sibling are DISTINCT objects
// (their scenario refs differ). Two options that resolve to the same ref are
// the same object -- a repeated trigger source, a dual-mode index -- and
// XMage's name match already selects it, so they are not ambiguous and need
// no discriminator.
//
// At most one of CopyMarker and Alias is set:
//
//   - CopyMarker ("[only copy]"/"[no copy]") is XMage's isCopy() filter. It
//     is used only when it leaves EXACTLY ONE candidate: a token among
//     same-named cards, or a card among same-named tokens. Its spelling
//     carries no space before the bracket; TestPlayer.makeChoose strips
//     exactly 9/11 characters.
//   - Alias ("@<ref>") is the driver's exact-object form
//     (TestPlayer.hasObjectTargetNameOrAlias): a bound alias names one
//     object and no other. It is used when the copy filter is not unique --
//     two cards, two tokens, or a card and a token that share a name -- and
//     the scenario ref is therefore the only thing that separates them.
//     registerAliases binds a setup ref to its object; the driver also binds
//     the ref of a mid-game permanent (a token copy) before the ask.
//
// A pick with neither (a bare name) is NOT uniquely identified and is the
// defect the same-name census counts as unresolved.
type SameNamePick struct {
	Ambiguous  bool
	Distinct   int    // distinct same-name offered refs, pick included
	CopyMarker string // "[only copy]"/"[no copy]" when the copy filter is unique
	Alias      string // "@<ref>" when only the exact ref separates the candidates
	// Unanswerable is set when the copy filter is not unique AND the pick's
	// ref is not an identity XMage can bind (a rank-derived library/hand
	// card). The name-only answer is unsound, so the census counts the item
	// as unproven rather than resolved. A target ask can still carry the
	// ref's exact owner prefix when that prefix separates the candidates
	// (ownerQualifiedRef), and a choice pick as its "@<ref>" alias form.
	Unanswerable bool
}

// pickRefExact reports whether pick k's ref is an identity XMage's alias
// binding reconstructs exactly: a token, or a setup battlefield permanent.
// A rank-derived ref (an anonymous library/hand card) is not. A hand-built
// decision that omits the provenance list is treated as exact (its refs are
// synthesized identity fixtures, not live ranks).
func pickRefExact(d rules.OracleDecision, k int) bool {
	if k >= 0 && k < len(d.PickRefsInexact) {
		return !d.PickRefsInexact[k]
	}
	return true
}

// ownerQualifiedRef returns pick k's ref when its owner prefix alone still
// identifies one object, even though the ref is rank-derived: the ref carries
// no "#k" rank and no other same-named offered ref belongs to the same seat.
// XMage's alias binding scopes a non-token ref to that seat's own objects, so
// a card picked from p1's graveyard ("p1:Dawn-Blessed Pennant") reaches p1's
// card, not the first same-named card in either graveyard (CR 601.2c).
//
// Known limit: only the OFFERED refs are visible here. Gorge's rank-free ref
// is k==1 among the owner's live same-named objects in EVERY zone, while the
// driver's alias binding ranks battlefield and hand before graveyard, so a
// same-named card the owner has on the battlefield or in hand (not offered)
// can still win the driver's binding. A decision cannot rule that out; the
// result is a visible replay mismatch, never a silent pass.
//
// An empty result means the owner does not separate the candidates -- two
// same-named objects of one seat, or a ranked ref whose ObjID rank the
// driver's zone order cannot reproduce -- so the conservative bare-name form
// stays.
func ownerQualifiedRef(d rules.OracleDecision, k int) string {
	if k < 0 || k >= len(d.PickRefs) || pickRefExact(d, k) {
		return ""
	}
	ref := d.PickRefs[k]
	seat, ok := refSeat(ref)
	if !ok || !strings.Contains(ref, ":") || strings.Contains(ref, ":token:") {
		return ""
	}
	if i := strings.LastIndexByte(ref, '#'); i >= 0 && i+1 < len(ref) &&
		strings.Trim(ref[i+1:], "0123456789") == "" {
		return ""
	}
	name := oraclediffRefName(ref)
	for _, candidate := range d.OptionRefs {
		if candidate == ref || !strings.EqualFold(oraclediffRefName(candidate), name) {
			continue
		}
		if s, ok := refSeat(candidate); ok && s == seat {
			return ""
		}
	}
	return ref
}

// ClassifySameName reports how pick k of d is disambiguated.
func ClassifySameName(d rules.OracleDecision, k int) SameNamePick {
	if k >= len(d.PickRefs) || !strings.Contains(d.PickRefs[k], ":") {
		return SameNamePick{}
	}
	ref := d.PickRefs[k]
	name := oraclediffRefName(ref)
	pickToken := strings.Contains(ref, ":token:")
	distinct := map[string]bool{}
	sameKind := 0
	for _, candidate := range d.OptionRefs {
		if !strings.EqualFold(oraclediffRefName(candidate), name) {
			continue
		}
		distinct[candidate] = true
		if strings.Contains(candidate, ":token:") == pickToken {
			sameKind++
		}
	}
	if len(distinct) < 2 {
		// The pick's name is unambiguous, or every same-named option is the
		// same object (a repeated trigger source), which a bare name still
		// selects exactly.
		return SameNamePick{}
	}
	p := SameNamePick{Ambiguous: true, Distinct: len(distinct)}
	if sameKind == 1 {
		// Exactly one candidate passes the copy filter: the token-or-card
		// distinction is unique, so the bare name plus the marker selects it.
		if pickToken {
			p.CopyMarker = "[only copy]"
		} else {
			p.CopyMarker = "[no copy]"
		}
		return p
	}
	// Two or more candidates of the same kind (or a card and a token plus a
	// same-kind sibling): the copy filter leaves more than one. Only the
	// pick's own scenario ref identifies it, and only when XMage can bind
	// that ref by identity. A rank-derived ref would bind the WRONG object
	// (or nothing), so it is not emitted: the item is unanswerable.
	if !pickRefExact(d, k) {
		p.Unanswerable = true
		return p
	}
	p.Alias = "@" + ref
	return p
}

// InNameMatchMechanism reports whether pick k of d reaches an XMage name /
// scenario-ref match at all. Excluded are the mechanisms that do NOT:
//
//   - an arrange order (Surveil/Scry/Dig "put them back"): XMage shows the
//     looked-at cards as an ordering list; two identical basics are
//     interchangeable and the compared snapshot cannot tell which one moved,
//     so disambiguating changes nothing.
//   - a library search (pickKind search, or a dig/hideaway pick): XMage's
//     computer player resolves it and the card's identity among identical
//     same-named basics does not change the compared state.
//
// It is the outer scope of the same-name census. IsNameSelection additionally
// drops a pick no exact answer can identify (an inexact ref whose copy filter
// leaves several same-kind candidates).
func InNameMatchMechanism(d rules.OracleDecision, k int) bool {
	if d.Kind == "order" {
		return false
	}
	if pickKind(d, k) == "search" {
		return false
	}
	switch d.Resume {
	case "dig", "search", "dig_arrange", "hideaway_arrange":
		return false
	}
	return true
}

// IsNameSelection reports whether pick k of d is answered by an object name
// or scenario ref that XMage's own name matching consumes (makeChoose,
// chooseTarget, TargetDiscard) AND an exact answer exists. It is the scope
// of the same-name census: every in-scope ambiguous pick must resolve, and a
// pick with no exact answer (ClassifySameName.Unanswerable) is counted as
// unproven, never as resolved.
func IsNameSelection(d rules.OracleDecision, k int) bool {
	return InNameMatchMechanism(d, k) && !ClassifySameName(d, k).Unanswerable
}

// SameNameAmbiguity is ClassifySameName's boolean summary: whether the pick
// shares its name with a distinct sibling, and the copy marker when that
// filter is unique (empty for an alias pick). It is the compatibility form
// the census used before Alias existed; new callers use ClassifySameName.
func SameNameAmbiguity(d rules.OracleDecision, k int) (ambiguous bool, marker string) {
	p := ClassifySameName(d, k)
	return p.Ambiguous, p.CopyMarker
}

// namesObject reports whether a choice LABEL is an object-name answer (or
// empty, meaning "use the object's name"). An activated ability or mana
// choice carries an action label ("Activate Llanowar Elves for mana") that
// XMage's makeChoose matches literally, exactly like an endure choice
// (TestYesNoRequiresAnEngineBoolean); replacing it with the object's scenario
// ref would answer a different question. The exact-ref alias is an
// object-NAME answer, so it is only correct when the label names the object.
func namesObject(label, ref string) bool {
	if label == "" || label == ref || strings.HasPrefix(label, "@") {
		return true
	}
	return strings.EqualFold(label, oraclediffRefName(ref))
}

// hiddenPickWithheld reports whether a hidden_pick decision has a pick that
// shares its name with another offered object and no exact spelling:
// ClassifySameName finds no alias or unique copy marker (the ref is
// rank-derived) and no owner prefix separates the candidates (both objects
// are one seat's). A bare name there reaches XMage's first same-named card
// (Pothole Mole returning the setup Forest, xe:RAND:1:5:w13), so the choice
// is withheld. Only a label that names the object is affected: a pick with an
// action label keeps its literal answer.
//
// An ask whose every offered object is a copy of the pick's one name held by
// the pick's own seat (offersOnlyCopiesOf) is NOT withheld. Which copy returns
// cannot change what the compared state shows when the offered copies are all
// the zone's cards -- the zone keeps the same name sequence whichever one
// leaves -- so the bare label is exact enough. Withholding it instead leaves
// the ask to XMage's own AI, which takes the maximum of an "up to N" pick
// (Rickety Gazebo: mill four, return up to two permanents; gorge returns the
// one scripted Wastes, XMage's AI returns two). Pothole Mole's ask offers a
// different name too, and a copy sitting between other cards moves the
// zone's order, so it stays withheld.
//
// snaps widen that interchangeability to a graveyard's own copies
// (graveyardPickCopiesInterchangeable): every copy of one name in one
// contiguous run is equally valid even when the ask offers other names, as
// Acolyte of Affliction's seven Swamps behind "may return a permanent card".
// A caller without snapshots cannot prove the run and keeps the withhold.
func hiddenPickWithheld(d rules.OracleDecision, snaps []rules.OracleSnapshot) bool {
	if d.Resume != "hidden_pick" {
		return false
	}
	for k := range d.PickRefs {
		if k >= len(d.Picks) || !ClassifySameName(d, k).Unanswerable ||
			!namesObject(d.Picks[k], d.PickRefs[k]) {
			continue
		}
		if ownerQualifiedRef(d, k) != "" || offersOnlyCopiesOf(d, k) ||
			graveyardPickCopiesInterchangeable(d, k, snaps) {
			continue
		}
		return true
	}
	return false
}

// hiddenPickUnanswerable is hiddenPickWithheld without snapshots: a
// shape-only caller cannot prove a copy run interchangeable, so it keeps the
// conservative withhold.
func hiddenPickUnanswerable(d rules.OracleDecision) bool { return hiddenPickWithheld(d, nil) }

// graveyardPickCopiesInterchangeable reports whether pick k's same-named
// offered copies are interchangeable in the picker's graveyard: every offered
// same-named object is one of that seat's non-token copies, the graveyard held
// all of them in one contiguous run at the start of the step, and the step
// removed exactly the copies this decision picked, leaving one run behind.
//
// A graveyard delta identifies the zone (a hand/library pick leaves it
// unchanged) and the run proves the outcome: removing any copy from a
// contiguous same-name run leaves the same name sequence, so the bare name is
// an exact answer for every copy (Acolyte of Affliction's seven Swamps).
// Pothole Mole's two Forests sit either side of a Swamp -- not one run -- so
// they are not interchangeable and stay withheld. A filtered ask (only the
// graveyard's lands offered) is covered too: the run is read from the
// graveyard itself, not from the offered list.
func graveyardPickCopiesInterchangeable(d rules.OracleDecision, k int, snaps []rules.OracleSnapshot) bool {
	if d.Step < 0 || d.Step+1 >= len(snaps) || k < 0 || k >= len(d.PickRefs) ||
		len(d.PickRefs) == 0 || len(d.OptionRefs) == 0 {
		return false
	}
	ref := d.PickRefs[k]
	if strings.Contains(ref, ":token:") {
		return false
	}
	name := oraclediffRefName(ref)
	seat, ok := refSeat(ref)
	if !ok {
		return false
	}
	offered := 0
	for _, candidate := range d.OptionRefs {
		if !strings.EqualFold(oraclediffRefName(candidate), name) {
			continue
		}
		if s, ok := refSeat(candidate); !ok || s != seat || strings.Contains(candidate, ":token:") {
			return false
		}
		offered++
	}
	if offered < 2 {
		return false
	}
	// The picks of this name are the cards the step took out of the graveyard;
	// their count is the delta the after-step checkpoint must show.
	gone := 0
	for _, pick := range d.PickRefs {
		if s, ok := refSeat(pick); ok && s == seat &&
			strings.EqualFold(oraclediffRefName(pick), name) && !strings.Contains(pick, ":token:") {
			gone++
		}
	}
	if gone == 0 {
		return false
	}
	before := seatGraveyard(snaps[d.Step], seat)
	after := seatGraveyard(snaps[d.Step+1], seat)
	if countFolded(before, name) < offered || !oneRun(before, name) {
		return false
	}
	if n := countFolded(after, name); n != countFolded(before, name)-gone || (n > 0 && !oneRun(after, name)) {
		return false
	}
	return true
}

// seatGraveyard returns seat's graveyard in zone order at snap, or nil when
// the snapshot has no such seat.
func seatGraveyard(snap rules.OracleSnapshot, seat int) []string {
	for _, p := range snap.Players {
		if p.Seat == seat {
			return p.Graveyard
		}
	}
	return nil
}

// countFolded counts the names equal to name, ignoring case.
func countFolded(names []string, name string) int {
	n := 0
	for _, s := range names {
		if strings.EqualFold(s, name) {
			n++
		}
	}
	return n
}

// oneRun reports whether names holds at least one copy of name and every copy
// occupies one contiguous run. A name broken by another card is not one run:
// removing either copy then leaves a different name sequence.
func oneRun(names []string, name string) bool {
	seen, inside, closed := false, false, false
	for _, s := range names {
		if strings.EqualFold(s, name) {
			if closed {
				return false
			}
			seen, inside = true, true
			continue
		}
		if inside {
			inside, closed = false, true
		}
	}
	return seen
}

// offersOnlyCopiesOf reports whether every object decision d offers is a
// same-named object of pick k's own seat: the candidates differ only in
// identity, never in name or owner.
func offersOnlyCopiesOf(d rules.OracleDecision, k int) bool {
	if k < 0 || k >= len(d.PickRefs) || len(d.OptionRefs) == 0 {
		return false
	}
	name := oraclediffRefName(d.PickRefs[k])
	seat, ok := refSeat(d.PickRefs[k])
	if !ok {
		return false
	}
	for _, candidate := range d.OptionRefs {
		if !strings.EqualFold(oraclediffRefName(candidate), name) || strings.Contains(candidate, ":token:") {
			return false
		}
		if s, ok := refSeat(candidate); !ok || s != seat {
			return false
		}
	}
	return !strings.Contains(d.PickRefs[k], ":token:")
}

// ExactRefAlias is the "@ref" answer (or the ref's exact owner-qualified form)
// that names decision d's pick k when its name alone matches more than one
// offered object, or "" when the name is unambiguous (ClassifySameName). Cost
// paths that script a batch as one answer per pick use it so each single binds
// the exact object, as JoinedChoice does for the joined answer.
func ExactRefAlias(d rules.OracleDecision, k int) string {
	if k >= len(d.Picks) || k >= len(d.PickRefs) {
		return ""
	}
	p := ClassifySameName(d, k)
	names := namesObject(d.Picks[k], d.PickRefs[k])
	if p.Alias != "" && names {
		return p.Alias
	}
	if p.Unanswerable && names {
		if owner := ownerQualifiedRef(d, k); owner != "" {
			return "@" + owner
		}
	}
	return ""
}

// JoinedChoice is the one "^"-joined answer XAnswersForScenario scripts for a
// multi-pick decision that one makeChoose dialog consumes: each pick spelled as
// XMage's dialog lists it (a same-name object by its exact ref, a mana colour
// by its colour name, a bare colour word by its capitalised colour name).
// Callers that drop or match that answer use it, so they see the same spelling
// the scenario carries.
func JoinedChoice(d rules.OracleDecision) string {
	labels := make([]string, 0, len(d.Picks))
	for k, label := range d.Picks {
		if label == "" && k < len(d.PickRefs) {
			label = oraclediffRefName(d.PickRefs[k])
		}
		if colour, ok := bareColourLabel(d, k); ok {
			label = colour
		}
		label = disambiguatedObjectChoice(d, k, label)
		if colour, ok := manaColourLabel(label); ok {
			label = colour
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, "^")
}

// disambiguatedObjectChoice returns the choice-queue answer for pick k: the
// exact-ref alias when the copy marker cannot separate the candidates AND the
// label is an object-name answer, the owner-qualified ref when an inexact ref's
// owner still separates them, else the label with the copy marker appended (no
// space before the bracket).
func disambiguatedObjectChoice(d rules.OracleDecision, k int, label string) string {
	nameIsObject := k < len(d.PickRefs) && namesObject(label, d.PickRefs[k])
	return disambiguatedObjectChoiceNamed(d, k, label, nameIsObject)
}

// disambiguatedObjectChoiceNamed is disambiguatedObjectChoice with the
// object-name test supplied by the caller. A band pick's label spells the
// member's LIVE face (CR 712.8), so a transformed DFC's name no longer equals
// oraclediffRefName(ref), the setup front face namesObject compares against.
// The band label's "Band <member> with <leader>" shape proves the member field
// is an object name, so bandAnswers passes nameIsObject=true and a transformed
// pick still gets the exact-ref alias when a same-name sibling is offered.
func disambiguatedObjectChoiceNamed(d rules.OracleDecision, k int, label string, nameIsObject bool) string {
	p := ClassifySameName(d, k)
	if p.Alias != "" && k < len(d.PickRefs) && nameIsObject {
		return p.Alias
	}
	if p.Unanswerable && k < len(d.PickRefs) && nameIsObject {
		if owner := ownerQualifiedRef(d, k); owner != "" {
			// "@<ref>", the same spelling as the alias branch: a joined
			// multi-pick answer binds every segment through the "@" path, and
			// a bare "pN:<name>" segment would match no alias and no card.
			return "@" + owner
		}
	}
	return label + p.CopyMarker
}

// targetPickValue is the target-queue answer for pick k of d: the seat ref
// as offered, the exact-ref alias when only the ref separates same-named
// candidates (the driver's targetName maps a bound ref to its @alias), the
// owner-qualified ref when an inexact ref's owner still separates them, else
// the object name with the copy marker appended. XMage's chooseTarget parses
// the same copy marker as makeChoose, so a target among same-named objects
// needs the discriminator too.
func targetPickValue(d rules.OracleDecision, k int, label string) string {
	v := label
	if k < len(d.PickRefs) {
		v = d.PickRefs[k]
	}
	if isSeat(v) {
		return v
	}
	p := ClassifySameName(d, k)
	if p.Alias != "" {
		return d.PickRefs[k]
	}
	if p.Unanswerable {
		// The owner prefix is exact even when the rank is not: a graveyard
		// card picked from p1's graveyard is "p1:<name>", which the driver
		// binds to p1's own object instead of the first same-named card.
		if owner := ownerQualifiedRef(d, k); owner != "" {
			return owner
		}
	}
	v = oraclediffRefName(v)
	_, marker := SameNameAmbiguity(d, k)
	return v + marker
}

// oraclediffRefName strips a scenario ref to the object name.
func oraclediffRefName(ref string) string {
	n := ref
	if i := strings.IndexByte(n, ':'); i >= 0 && strings.HasPrefix(n, "p") {
		n = n[i+1:]
	}
	n = strings.TrimPrefix(n, "token:")
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n
}

func hasType(f *cards.Face, t string) bool {
	for _, x := range f.Types {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}

// poolFor turns a Forge mana cost ("2 R R", "W/U", "X G") into the exact
// pool letters that pay it: generic as colorless, hybrid as its first half.
func poolFor(cost string) (string, string) {
	cost = strings.TrimSpace(cost)
	if cost == "" || strings.EqualFold(cost, "no cost") {
		return "", "no mana cost"
	}
	var b strings.Builder
	for _, sym := range strings.Fields(cost) {
		switch {
		case sym == "0":
		case strings.Trim(sym, "0123456789") == "":
			var n int
			fmt.Sscanf(sym, "%d", &n)
			b.WriteString(strings.Repeat("C", n))
		case len(sym) == 1 && strings.Contains("WUBRGC", sym):
			b.WriteString(sym)
		case len(sym) == 2 && strings.Contains("WUBRG", sym[:1]) && strings.Contains("WUBRG", sym[1:]):
			// Forge spells hybrid {W/B} as "WB": pay the first half.
			b.WriteString(sym[:1])
		case len(sym) == 2 && sym[0] == '2' && strings.Contains("WUBRG", sym[1:]):
			// {2/W}: pay the coloured half.
			b.WriteString(sym[1:])
		case strings.Contains(sym, "/"):
			h := strings.Split(sym, "/")
			switch {
			case strings.EqualFold(h[1], "P"):
				b.WriteString(h[0])
			case len(h[0]) == 1 && strings.Contains("WUBRGC", h[0]):
				b.WriteString(h[0])
			case strings.Trim(h[0], "0123456789") == "":
				// {2/W}: pay the coloured half.
				b.WriteString(h[1])
			default:
				return "", "mana symbol " + sym
			}
		case sym == "X":
			// X = xValue, scripted for both engines (Generate).
			b.WriteString(strings.Repeat("C", xValue))
		default:
			return "", "mana symbol " + sym
		}
	}
	return b.String(), ""
}

// playerTargetHead reports whether a ValidTgts$ filter names a player rather
// than a card. A zone qualifier (Origin$/TgtZone$) describes where an effect
// finds its cards, so it must never be appended to a player target: doing so
// turned "Opponent" into "Opponent@Hand" (Cruelclaw's Heist, Ruthless
// Negotiation, Soul Search, Aggressive Negotiations), which no fixture can
// satisfy. The head is the first comma-separated alternative before its first
// '.' predicate.
func playerTargetHead(filter string) bool {
	head := strings.ToLower(strings.SplitN(strings.Split(filter, ",")[0], ".", 2)[0])
	return head == "player" || head == "opponent"
}

// openingHandAnswers declines the "you may begin the game with this card" ask
// for a K:MayEffectFromOpeningHand card. The runner's setup fallback otherwise
// takes option 0 ("Yes"), which starts the card on the battlefield and makes
// the later cast step "not offered"; the scenario wants the card in hand.
func openingHandAnswers(f *cards.Face) []Answer {
	if _, ok := f.KeywordParam("MayEffectFromOpeningHand"); ok {
		return []Answer{{Kind: "choose", Pick: []string{"no"}}}
	}
	return nil
}

// twoPilesPileAnswer maps gorge's pile pick to XMage's boolean chooseUse
// answer: pile A ("First pile", the separator's face-down pile) is "yes",
// pile B ("Second pile", the face-up pile) is "no".
func twoPilesPileAnswer(d rules.OracleDecision) string {
	if len(d.PickKinds) == 1 && d.PickKinds[0] == "pile-b" {
		return "no"
	}
	return "yes"
}

// pickQueue is xmQueue for the k-th pick, except that a search of ANOTHER
// player's library (Ancient Vendetta's "search target opponent's ... library")
// reaches XMage's choice queue: only a search of your own library is the
// TargetCardInLibrary target ask (measured on the std pass). bolster marks
// the decision as a Bolster tie election, whose counter_pick XMage asks
// through chooseTarget (bolster_pick.go). craft marks a Craft keyword's
// material exile, which CraftAbility pays through chooseTarget rather than the
// choice queue every other exile cost uses (craft_cost_pick.go).
func pickQueue(d rules.OracleDecision, k int, label string, bolster, craft bool) string {
	if bolster && pickKind(d, k) == "counter_pick" {
		return "target"
	}
	if craft && pickKind(d, k) == "exilecost" {
		// Craft material is a TargetCardInGraveyardBattlefieldOrStack chosen
		// by CraftAbility.CraftCost.pay -> player.chooseTarget: the TARGET
		// queue, one answer per material. The kind-level choice mapping must
		// not capture it just because a Craft cost also has min == max.
		return "target"
	}
	if d.Retarget {
		// A ChangeTargets redirect (Bolt Bend) is XMage's chooseTarget:
		// the new target rides the target queue even though gorge poses it
		// as a choose ask, so a "choice" answer is never consumed and the
		// original target survives.
		return "target"
	}
	q := xmQueue(pickKind(d, k), label)
	if q == "target" && pickKind(d, k) == "search" && k < len(d.PickRefs) {
		if s, ok := refSeat(d.PickRefs[k]); ok && s != d.Seat {
			return "choice"
		}
	}
	return q
}
