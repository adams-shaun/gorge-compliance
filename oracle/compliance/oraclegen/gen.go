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

	"github.com/adams-shaun/gorge/engine/cards"
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
	// an Item field, never a Step one, so Raw() never sends it to gorge's
	// runner (which cannot force a roll at this pin). A follower that must
	// not roll its own die reads it: XMage scripts each roll through
	// xmage_answers (kind "die"), and a Forge driver forces item.die_rolls
	// before running the step. Empty for every item that rolls no die, so
	// existing item bytes are unchanged.
	DieRolls []DieRoll `json:"die_rolls,omitempty"`
	// CoinFlips is every coin flip the item's steps made, in log order. Like
	// DieRolls it is an Item field, never a Step one, so Raw() never sends it
	// to gorge's runner. A Forge driver must force each result before running
	// the step (its FlipCoinEffect flips from its own seeded stream, so a card
	// like Mana Clash loops a different number of times); XMage already
	// scripts the runner's flips through xmage_answers (kind "flip"). Empty
	// for every item that flips no coin, so existing item bytes are unchanged.
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

// Raw is the scenario alone, as gorge's runner decodes it.
func (it Item) Raw() []byte {
	b, _ := json.Marshal(it.Scenario)
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
