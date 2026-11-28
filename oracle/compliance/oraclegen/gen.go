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
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
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

// yesNo recognises a bare two-way boolean choice. The engine's option kind
// and exact label/ref identity must both agree; composed choices such as

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

// ModeGenericBespokeChoiceQueue is the sentinel for a plain GenericChoice pick
// on a face whose XMage class builds its own Choice dialog with its own labels
// (xmageGenericChoiceBespokeFaces: Pemmin's Aura / Shorecrasher Elemental's
// "Select how to boost" over "+1/-1", "-1/+1"). The CHOICE queue, answered
// with the XMage label (xmageGenericBespokeChoiceLabel).
const ModeGenericBespokeChoiceQueue = -5

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
