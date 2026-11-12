// Package follower is the registry of the engines xengine compares gorge
// against. Each entry names what the harness needs to know about one follower:
// how its items are prepared, which runner replays them, and the driver quirks
// the classifier must not mistake for a rules difference. The harness reads
// this table instead of comparing engine names, so a new follower is one more
// entry here plus the quirks its driver actually has.
//
// The package is a leaf: it imports nothing from the harness, so the oracle
// comparison and the xengine commands can both depend on it.
package follower

import "strings"

// The prep stages a follower's items go through before its runner sees them.
const (
	// PrepXPrep is `xengine xprep`: it writes the prepared items to
	// <play>/xitems.jsonl and its refusals to <play>/xprep.jsonl, which
	// `xengine diff` reads as the repro input and as class not_sent.
	PrepXPrep = "xprep"
	// PrepForgeExport is `oraclediff forge-export`: it turns a scenario into
	// a Forge request, which carries gorge's decisions.
	PrepForgeExport = "forge-export"
	// PrepFollowerExport is `oraclediff follower-export -engine NAME`: a
	// forge-export request plus the engine-neutral identities ("neutral"),
	// after the runner's --preflight has refused the items naming a card the
	// follower lacks. The refusals go to <chunk>/<Name>-prep.jsonl, which
	// `xengine diff` reads as class not_sent.
	PrepFollowerExport = "follower-export"
)

// Follower is one follower engine's entry. Every field is read by a harness
// branch that once compared the engine's name; a zero field is the behaviour
// an engine without that quirk gets.
type Follower struct {
	// Name is the engine's id: the -engine value, a row's "engine" field and
	// a finding's engine.
	Name string
	// Display is the name messages and errors print ("no Forge result").
	Display string
	// Runner is the script that replays a JSONL of the follower's inputs:
	// `RUNNER IN.jsonl OUT.jsonl` (REPRO.md quotes it).
	Runner string
	// Prep is the stage that turns an item into the runner's input
	// (PrepXPrep, PrepForgeExport or PrepFollowerExport).
	Prep string
	// StampsEngine is a driver that writes "engine":"<Name>" on every row.
	// A row without it is then refused, since a row from another follower
	// fed in by mistake would compare as this one's. A follower that does
	// not stamp its rows only has a stamp that names another engine refused.
	StampsEngine bool
	// ConsumesPayment is a driver that applies the item's recorded pool spend
	// (Item.Payment) as its manual payment, so its pool split is gorge's and
	// a pool difference after a cast is real, not a payment choice.
	ConsumesPayment bool
	// TurnCapMsg starts the driver's harness message for its turn cap. A
	// whole-game tape that stops with it is class turn_cap. "" is no cap.
	TurnCapMsg string
	// CastModes are the cast_mode values the driver can act on. A cast with
	// any other mode is cast the ordinary way, so a difference at or after
	// it is the driver's gap (class harness). nil is a driver that follows
	// every cast_mode.
	CastModes []string
	// SkipsDamageStep is a driver whose engine goes straight to end of
	// combat when no creature is left in it (class forge_skips_damage_step).
	SkipsDamageStep bool
	// StopsInCleanup is a driver whose engine hands out priority in the
	// cleanup step after an end-the-turn effect (class harness).
	StopsInCleanup bool
	// TrailingMisses is a driver that poses a cleanup discard or an optional
	// trigger's yes/no past the item's last step. Such a strict miss alone is
	// not a decision_shape finding.
	TrailingMisses bool
	// TapsWithCast is a driver that runs a mana tap and the cast it leads to
	// within one priority when its seat holds priority out of turn or in
	// response, so the tap's checkpoint already shows the cast
	// (classify.SkipEarlyCasts).
	TapsWithCast bool
	// TriggerOrderByName is a driver that matches a scripted trigger order by
	// the ability's text or its source's name, so it cannot order two
	// triggers from same-named objects apart (class harness).
	TriggerOrderByName bool
	// LacksCardMsg is the text in a harness message that names a card the
	// follower's database does not hold; the card's name follows it. ""
	// is a follower whose misses are not told apart from other harness rows.
	LacksCardMsg string
	// JVM is a runner that starts a Java VM. The sweep holds such a follower
	// to the box's JVM slots (scripts/xengine-sweep.sh jvm_slot) before and
	// after it takes the heavy lock; a follower without one runs under the
	// heavy lock alone.
	JVM bool
}

// forgeCastModes are the cast_mode values the Forge driver can act on:
// StepMachine.spellForMode picks the plot special action, an Adventure or Omen
// half, a modal back face or a warp cast out of Card.getAllPossibleAbilities,
// and the optional-cost modes pay the kicker, buyback, entwine, offspring or
// replicate they name. Any other mode (flashback, foretell, escape, ...) is
// still cast the ordinary way, so the driver takes the card's first spell
// ability and the difference after it is the driver's gap.
var forgeCastModes = []string{"", "optionalcost",
	"plot", "adventure_alt", "omen_alt", "modal_spell", "warped",
	"kicked", "kicked1", "kicked2", "kickedboth", "buyback", "entwined",
	"offspring", "replicated", "multikicked"}

// registry holds the followers in the order usage text lists them.
var registry = []Follower{
	{
		Name:    "xmage",
		Display: "XMage",
		Runner:  "scripts/xmage-oracle-run.sh",
		Prep:    PrepXPrep,
		// XMage queues the recorded multiset as its manual payment
		// (ScenarioReplay.beginPayment).
		ConsumesPayment:    true,
		TapsWithCast:       true,
		TriggerOrderByName: true,
		LacksCardMsg:       "Couldn't find a card:",
		JVM:                true,
	},
	{
		Name:         "forge",
		Display:      "Forge",
		Runner:       "scripts/forge-oracle-run.sh",
		Prep:         PrepForgeExport,
		StampsEngine: true,
		// TODO(H1 audit): the Forge driver reads item.payment too (Request.java,
		// StepMachine.java, ScriptedController.java; see
		// docs/design/2026-11-10-followers/audit-contract.md section 2.5), so
		// this is probably true. H1 keeps today's behaviour: classify treated only XMage as
		// consuming a payment, and a Forge pool split after such a cast is
		// still a payment_choice.
		ConsumesPayment: false,
		TurnCapMsg:      "passed turn ",
		CastModes:       forgeCastModes,
		SkipsDamageStep: true,
		StopsInCleanup:  true,
		TrailingMisses:  true,
		JVM:             true,
	},
	{
		Name:    "phase",
		Display: "phase.rs",
		Runner:  "scripts/phase-oracle-run.sh",
		// follower-export: the neutral identities and the --preflight refusals.
		Prep:         PrepFollowerExport,
		StampsEngine: true,
		// The driver spends payment[i] units from the pool first.
		ConsumesPayment: true,
		// Ordinary casts only until a later ticket maps cast modes.
		CastModes: []string{""},
		// The harness text emitted when a card is missing at run time.
		LacksCardMsg: "phase lacks card: ",
		// A Rust binary: the heavy lock alone.
		JVM: false,
	},
	{
		Name:    "manabrew",
		Display: "Manabrew",
		Runner:  "scripts/manabrew-oracle-run.sh",
		// follower-export: the preflight refusals; Manabrew reads the same abilities
		// script lines as Forge and ignores "neutral".
		Prep:         PrepFollowerExport,
		StampsEngine: true,
		// The driver adds payment[i] to the pool and pays from the pool only.
		ConsumesPayment: true,
		// The modes the driver follows (step_machine.rs CAST_MODES): the ordinary cast, the
		// play modes the engine's action space lists (adventure_alt, warped) and the optional
		// costs its cast prompts ask for. Not followed: omen_alt, modal_spell, plot (no such
		// cast or special action in the engine), kicked2, kickedboth (one kicker only),
		// offspring, optionalcost (no cast prompt for them).
		CastModes: []string{"", "adventure_alt", "warped", "kicked", "kicked1", "buyback", "entwined", "multikicked", "replicated"},
		// The harness text emitted when a card is missing at run time.
		LacksCardMsg: "manabrew lacks card: ",
		// A Rust binary: the heavy lock alone.
		JVM: false,
	},
}

// Lookup is the follower named name.
func Lookup(name string) (Follower, bool) {
	for _, f := range registry {
		if f.Name == name {
			return f, true
		}
	}
	return Follower{}, false
}

// Known reports whether name is a registered follower.
func Known(name string) bool {
	_, ok := Lookup(name)
	return ok
}

// Names lists the registered followers' names, in registry order.
func Names() []string {
	out := make([]string, len(registry))
	for i, f := range registry {
		out[i] = f.Name
	}
	return out
}

// FollowsCastMode reports whether the driver acts on cast_mode mode.
func (f Follower) FollowsCastMode(mode string) bool {
	if f.CastModes == nil {
		return true
	}
	for _, m := range f.CastModes {
		if m == mode {
			return true
		}
	}
	return false
}

// EnvPrefix is the prefix of the follower's sweep environment variables:
// XENGINE_ and the name upper-cased, any byte outside [A-Z0-9] as '_'
// ("xmage" -> XENGINE_XMAGE).
func (f Follower) EnvPrefix() string {
	var b strings.Builder
	b.WriteString("XENGINE_")
	for _, r := range strings.ToUpper(f.Name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// RunnerEnv names the variable that overrides the follower's runner in the
// sweep (XENGINE_XMAGE_RUNNER).
func (f Follower) RunnerEnv() string { return f.EnvPrefix() + "_RUNNER" }

// MemEnv names the variable that sets the follower's heavy-lock --mem in the
// sweep (XENGINE_XMAGE_MEM).
func (f Follower) MemEnv() string { return f.EnvPrefix() + "_MEM" }

// WaitEnv names the variable that sets the follower's heavy-lock --wait in
// the sweep (XENGINE_XMAGE_WAIT).
func (f Follower) WaitEnv() string { return f.EnvPrefix() + "_WAIT" }

// All is every registered follower, in registry order.
func All() []Follower {
	return append([]Follower(nil), registry...)
}
