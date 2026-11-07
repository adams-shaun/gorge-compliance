// Package classify turns the comparison of one lowered xengine item against
// one follower engine's replay of it into a Finding: one class per (item,
// engine), first match wins. It reuses oraclediff.CompareOpts
// unchanged, so the first difference is the one every other oracle pass
// reports; the classifier only decides what KIND of difference it is.
package classify

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
)

// The classes, in the order Classify tries them.
const (
	ClassLowerGap   = "lower_gap"
	ClassXMageLacks = "xmage_lacks"
	ClassHarness    = "harness"
	// ClassNotSent is an item xprep left out on purpose: the follower has no
	// row for it and nothing went wrong.
	ClassNotSent = "not_sent"
	// ClassTurnCap is a whole-game tape that agreed as far as the Forge
	// driver's turn cap, which a window fits inside and a tape does not.
	ClassTurnCap       = "turn_cap"
	ClassDecisionShape = "decision_shape"
	// ClassPaymentChoice is the decision_shape family's mana-payment case:
	// the only difference is which legal pool units paid a cost neither
	// engine fixed, so it is the player's choice (CR 601.2h), not a rules
	// divergence. It never covers any other pool difference.
	ClassPaymentChoice = "payment_choice"
	// ClassForgeSkipsDamageStep is a known Forge difference, not a gorge one:
	// when no creature is left in combat (the attackers all died or left it
	// after they were declared, or a first-strike combat's second damage step
	// has none), Forge goes straight to end of combat. Its PhaseHandler skips
	// declare blockers and the damage steps when the combat holds no attacker
	// as the phase begins (isSkippingPhase), and gives the damage step no
	// priority when Combat.assignCombatDamage(false) finds nothing. CR 508.8
	// skips those steps only when no creature was declared as an attacker; a
	// combat that had one keeps its steps and the players priority in each
	// (CR 506.1, 509.1, 510.1, 117.3a). The item's passes then run out of step
	// with Forge, so nothing past that checkpoint is compared.
	//
	// The common case is a first-strike combat where no creature lacks first
	// strike: CR 510.4 still gives a second combat damage step, but Forge's
	// COMBAT_DAMAGE phase assigns nothing and so gives no priority
	// (PhaseHandler.onPhaseBegin, COMBAT_DAMAGE). In a sample of twelve such
	// rows every attacker was a lone first striker; the driver cannot supply
	// the priority round, since it only answers the priorities Forge gives.
	ClassForgeSkipsDamageStep = "forge_skips_damage_step"
	// ClassCrewTypeDependency is a type-adding static that misses a Vehicle
	// that is a creature, in either follower. The static's layer-4 type
	// addition and layer-7b base P/T depend (CR 613.8a) on the effect that
	// makes the Vehicle an artifact creature: applying that effect changes
	// which objects "creatures you control" covers, so it is applied first
	// whatever the timestamps. Both followers order the static's layer-4
	// change by timestamp instead: XMage's filter has already skipped the
	// not-yet-creature Vehicle, and Forge's subtype table drops the creature
	// subtype while the intermediate type is not yet a creature. Neither
	// restores it after the animation, while the layer-7b P/T is recomputed
	// on the now-creature Vehicle, so it ends up 6/6 without the added type.
	// gorge applies the dependency, so the difference is the followers', not
	// gorge's. The animation is a crew (Dune Drifter), an external animate
	// ability (Guidelight Matrix) or a self-animating static already on the
	// battlefield when the window opens (Midnight Mangler); the classifier
	// reads the Vehicle's type change from the snapshots, not the step that
	// caused it. See docs/upstream-bugs.md for the XMage and Forge source.
	ClassCrewTypeDependency = "crew_type_dependency"
	ClassRNG                = "rng"
	ClassProjectionGap      = "projection_gap"
	ClassDiverge            = "diverge"
	ClassAgree              = "agree"
)

// Classes lists every class, in report order.
var Classes = []string{
	ClassAgree, ClassDiverge, ClassRNG, ClassProjectionGap, ClassDecisionShape,
	ClassPaymentChoice, ClassForgeSkipsDamageStep, ClassCrewTypeDependency,
	ClassHarness, ClassNotSent, ClassTurnCap, ClassXMageLacks, ClassLowerGap,
}

// Finding is the classified outcome for one item and one follower engine.
type Finding struct {
	ID     string `json:"id"`
	Engine string `json:"engine"`
	Class  string `json:"class"`
	// Checkpoint and Field are the first difference (CompareOpts); Gorge and
	// Other its two values (Other is the follower's).
	Checkpoint string `json:"checkpoint,omitempty"`
	Field      string `json:"field,omitempty"`
	Gorge      string `json:"gorge,omitempty"`
	Other      string `json:"other,omitempty"`
	Msg        string `json:"msg,omitempty"`
	// Step is the scenario step of the checkpoint (-1 for "setup" and for a
	// finding with no checkpoint).
	Step int `json:"step"`
}

// GapFinding is the lower_gap finding of an item whose self-check did not
// pass. Such an item is never sent to a follower, so it has no engine.
func GapFinding(item lower.Lowered, c lower.Check) Finding {
	return Finding{
		ID: item.Item.ID, Class: ClassLowerGap, Step: c.Step,
		Field: c.Field, Gorge: c.Gorge, Other: c.Tape,
		Msg: fmt.Sprintf("self-check %s at step %d", c.Status, c.Step),
	}
}

// Classify compares gorge's result g for item with the follower's x and
// classes the difference. t is the tape the item was lowered from (it may be
// nil, in which case nothing is ever an rng finding). engine is "xmage" or
// "forge".
func Classify(item lower.Lowered, t *game.Tape, g rules.OracleResult, x oraclediff.XResult, engine string) Finding {
	f := Finding{ID: item.Item.ID, Engine: engine, Step: -1}
	if engine == "xmage" {
		x = SkipEarlyCasts(item.Item, g, x)
	}
	stopped := ""
	if pg, px, ok := Partial(item.Item, g, x); ok {
		stopped = x.Harness
		g, x = pg, px
	}
	v := oraclediff.CompareOpts(g, nil, x, item.Item.Compare, item.Item.Ignore...)
	f.Checkpoint, f.Field, f.Gorge, f.Other = v.Checkpoint, v.Field, v.Gorge, v.XMage
	f.Step = stepOf(v.Checkpoint)

	if engine == "forge" {
		if i := skippedDamageStep(item, g, x); i > 0 {
			f.Class, f.Checkpoint, f.Field = ClassForgeSkipsDamageStep, g.Snapshots[i].Checkpoint, "step"
			f.Gorge, f.Other, f.Step = g.Snapshots[i].Step, x.Snapshots[i].Step, stepOf(g.Snapshots[i].Checkpoint)
			return f
		}
	}
	switch {
	case v.Status == oraclediff.XMageLacks ||
		(v.Status == oraclediff.Harness && engine == "xmage" && xmageLacks(v.Msg, t)):
		f.Class, f.Msg = ClassXMageLacks, v.Msg
	case v.Status == oraclediff.Harness && engine == "forge" && strings.Contains(item.Item.ID, ":tape") && strings.Contains(v.Msg, turnCapMsg):
		f.Class, f.Msg = ClassTurnCap, v.Msg
	case v.Status == oraclediff.Harness:
		who := v.Engine
		if who == "xmage" {
			who = engine // CompareOpts names the follower "xmage" whichever it is
		}
		f.Class, f.Msg = ClassHarness, who+": "+v.Msg
	case (x.StrictMiss != "" || x.Leftover != "") && !trailingMiss(x, len(item.Item.Steps), engine):
		f.Class = ClassDecisionShape
		f.Msg = strings.TrimSpace("strict_miss: " + x.StrictMiss + " leftover: " + x.Leftover)
	case v.Status == oraclediff.Agree:
		f.Class = ClassAgree
	case forgeStopsInCleanup(x, v, f.Step, engine):
		f.Class = ClassHarness
		f.Msg = "forge: a player holds priority in the cleanup step after an end-the-turn effect"
	case forgeIgnoresCastMode(item, v, f.Step, engine) != "":
		f.Class = ClassHarness
		f.Msg = "forge: " + forgeIgnoresCastMode(item, v, f.Step, engine)
	case xmageSameNameTriggerOrder(item, v, f.Step, engine) != "":
		f.Class = ClassHarness
		f.Msg = "xmage: " + xmageSameNameTriggerOrder(item, v, f.Step, engine)
	case paymentChoice(item, g, x, v, f.Step, engine):
		f.Class = ClassPaymentChoice
		f.Msg = "pool split after a cast/activate whose payment was not fixed"
	case rngField(v.Field) && (rngDrawn(item, t, f.Step) || shuffledEarlier(item, f.Step)):
		f.Class = ClassRNG
	case crewTypeDependency(g, v):
		f.Class = ClassCrewTypeDependency
	case projectionGap(v):
		f.Class = ClassProjectionGap
	default:
		f.Class = ClassDiverge
		if stopped != "" {
			f.Msg = "the driver then stopped: " + stopped
		}
	}
	return f
}

// SkipEarlyCasts takes gorge's checkpoints for the mana taps leading up to a cast in
// x, the XMage row. The driver runs such a tap and the cast within one priority when
// its seat holds priority out of turn or in response, so the tap's checkpoint already
// shows the cast. The cast's own checkpoint is still compared.
func SkipEarlyCasts(item oraclegen.Item, g rules.OracleResult, x oraclediff.XResult) oraclediff.XResult {
	if len(x.Snapshots) > len(g.Snapshots) || len(item.Steps)+1 < len(x.Snapshots) {
		return x
	}
	var snaps []rules.OracleSnapshot
	for i, st := range item.Steps {
		if st.Op != "activate" || i+1 >= len(x.Snapshots) || !leadsToCast(item.Steps, i) {
			continue
		}
		if snaps == nil {
			snaps = append(snaps, x.Snapshots...)
		}
		snaps[i+1] = g.Snapshots[i+1]
	}
	if snaps != nil {
		x.Snapshots = snaps
	}
	return x
}

// leadsToCast reports that the steps after the mana tap at i are more taps by its seat,
// then that seat's cast.
func leadsToCast(steps []oraclegen.Step, i int) bool {
	for j := i + 1; j < len(steps); j++ {
		switch {
		case steps[j].Seat != steps[i].Seat:
			return false
		case steps[j].Op == "cast":
			return true
		case steps[j].Op != "activate":
			return false
		}
	}
	return false
}

// turnCapMsg starts the Forge driver's report of its turn cap.
const turnCapMsg = "passed turn "

// Partial is gorge's result and a harness row x, both cut to the checkpoints the
// driver reached, when the engines already differ there: the driver stopped because
// the game had parted from the script, so the difference is the finding and the stop
// is its consequence. ok is false for any other row, which keeps its harness verdict.
func Partial(item oraclegen.Item, g rules.OracleResult, x oraclediff.XResult) (rules.OracleResult, oraclediff.XResult, bool) {
	n := len(x.Snapshots)
	if x.Harness == "" || n == 0 || n > len(g.Snapshots) {
		return g, x, false
	}
	cg, cx := g, x
	cg.Snapshots, cx.Harness = g.Snapshots[:n], ""
	if oraclediff.CompareOpts(cg, nil, cx, item.Compare, item.Ignore...).Status != oraclediff.Diverge {
		return g, x, false
	}
	return cg, cx, true
}

// crewTypeDependency reports the first difference v at a permanent whose
// canonical line differs only by one extra type on gorge's side, on an
// artifact creature Vehicle. That is the shape a type-adding static leaves
// when it misses the effect that makes the Vehicle a creature it depends on
// (CR 613.8a): the follower's layer-7b P/T still lands, so the P/T, counters,
// colours, keywords, tap state and every other field agree and only the added
// type is absent.
//
// The class reads the animation from the snapshots instead of the step that
// caused it. At a scenario step the preceding checkpoint must show the same
// permanent as a non-creature and the diverging one as a creature: that
// covers a crew, an external "target Vehicle becomes an artifact creature"
// ability and a self-animation alike, while a non-animation activate, or an
// activate after the Vehicle was already a creature, cannot match. At "setup"
// the window opens after the animation, so the type change happened in the
// hidden game; the shape alone is the class, and the animation's timestamp
// relative to the static is the same 613.8a dependency.
//
// A difference anywhere else, a Vehicle that never became a creature, or an
// extra "creature" (the animation itself) is not this class.
func crewTypeDependency(g rules.OracleResult, v oraclediff.Verdict) bool {
	if v.Status != oraclediff.Diverge || v.Field != "permanents" {
		return false
	}
	d := stepOf(v.Checkpoint)
	if d+1 >= len(g.Snapshots) {
		return false
	}
	gp, ok := permForLine(g.Snapshots[d+1].Permanents, v.Gorge)
	if !ok || !hasLineType(gp.Types, "creature") {
		return false
	}
	gTypes, ok := lineTypes(v.Gorge)
	if !ok {
		return false
	}
	xTypes, ok := lineTypes(v.XMage)
	if !ok {
		return false
	}
	extra, ok := oneExtraType(gTypes, xTypes)
	if !ok || strings.EqualFold(extra, "creature") {
		return false
	}
	if !hasLineType(xTypes, "artifact") || !hasLineType(xTypes, "creature") || !hasLineType(xTypes, "vehicle") {
		return false
	}
	// Removing the one extra type must reproduce the follower's whole line:
	// any other difference (P/T, counters, tap state, keywords) is not this
	// class.
	without, ok := lineWithoutType(v.Gorge, extra)
	if !ok || without != v.XMage {
		return false
	}
	if d < 0 {
		// Setup: the animation is already in the hidden game before step 0.
		return true
	}
	prev, ok := permFor(g.Snapshots[d].Permanents, gp)
	if !ok || hasLineType(prev.Types, "creature") {
		return false
	}
	return true
}

// lineWithoutType removes one type token from the bracketed type list of a
// canonical permanent line and leaves the rest of the line untouched.
func lineWithoutType(line, extra string) (string, bool) {
	open := strings.IndexByte(line, '[')
	close := strings.IndexByte(line, ']')
	if open < 0 || close < open {
		return "", false
	}
	types := strings.Fields(line[open+1 : close])
	var kept []string
	for _, t := range types {
		if !strings.EqualFold(t, extra) {
			kept = append(kept, t)
		}
	}
	if len(kept) != len(types)-1 {
		return "", false
	}
	return line[:open+1] + strings.Join(kept, " ") + line[close:], true
}

// permForLine returns the permanent whose canonical line prefix is line's:
// the comparator names a card by controller, owner and current name.
func permForLine(ps []rules.OracleSnapPerm, line string) (rules.OracleSnapPerm, bool) {
	for _, p := range ps {
		prefix := fmt.Sprintf("c%d o%d %s [", p.Controller, p.Owner, p.Name)
		if strings.HasPrefix(line, prefix) {
			return p, true
		}
	}
	return rules.OracleSnapPerm{}, false
}

// lineTypes reads the space-separated type list between the first brackets of
// a canonical permanent line.
func lineTypes(line string) ([]string, bool) {
	open := strings.IndexByte(line, '[')
	close := strings.IndexByte(line, ']')
	if open < 0 || close < open {
		return nil, false
	}
	return strings.Fields(line[open+1 : close]), true
}

// oneExtraType returns a's one type that b lacks, when b holds nothing a
// lacks. The comparison folds case because the two engines spell types
// differently.
func oneExtraType(a, b []string) (string, bool) {
	inB := make(map[string]bool, len(b))
	for _, t := range b {
		inB[strings.ToLower(t)] = true
	}
	inA := make(map[string]bool, len(a))
	var extra []string
	for _, t := range a {
		inA[strings.ToLower(t)] = true
		if !inB[strings.ToLower(t)] {
			extra = append(extra, t)
		}
	}
	for _, t := range b {
		if !inA[strings.ToLower(t)] {
			return "", false
		}
	}
	if len(extra) != 1 {
		return "", false
	}
	return extra[0], true
}

// hasLineType reports whether the canonical line type list holds t.
func hasLineType(types []string, t string) bool {
	for _, x := range types {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}

// permFor returns the permanent in ps that is the same object as p. The ref
// is stable across checkpoints; the controller, owner and current name are
// the fallback when a row omits it.
func permFor(ps []rules.OracleSnapPerm, p rules.OracleSnapPerm) (rules.OracleSnapPerm, bool) {
	for _, q := range ps {
		if p.Ref != "" && q.Ref != "" {
			if q.Ref == p.Ref {
				return q, true
			}
			continue
		}
		if q.Controller == p.Controller && q.Owner == p.Owner && strings.EqualFold(q.Name, p.Name) {
			return q, true
		}
	}
	return rules.OracleSnapPerm{}, false
}

// skippedDamageStep is the checkpoint at which Forge left for end of combat
// while gorge entered declare blockers or a damage step with nothing left in
// combat to use it, with every checkpoint before it agreeing; 0 if the item
// has none. The first difference must be that step, after the pass that ends
// the step before it (declare attackers, declare blockers, or a first-strike
// combat's first damage step): the non-active seat's on an empty stack.
// Gorge's checkpoint must otherwise agree with Forge's once the step is set
// aside: a combat that dealt damage would show it in life, permanents or
// graveyards, and that stays a divergence.
func skippedDamageStep(item lower.Lowered, g rules.OracleResult, x oraclediff.XResult) int {
	for i := 1; i < min(len(g.Snapshots), len(x.Snapshots)); i++ {
		gs, xs := g.Snapshots[i], x.Snapshots[i]
		if gs.Step == xs.Step {
			continue
		}
		prev := g.Snapshots[i-1]
		if (gs.Step != "combat-damage" && gs.Step != "declare-blockers") || xs.Step != "end-combat" ||
			(prev.Step != "combat-damage" && prev.Step != "declare-blockers" && prev.Step != "declare-attackers") ||
			len(prev.Stack) != 0 || i > len(item.Item.Steps) {
			return 0
		}
		if st := item.Item.Steps[i-1]; st.Op != "pass" || st.Seat == gs.Active {
			return 0
		}
		// Everything but the step name agrees, up to and including this
		// checkpoint: the step is set to Forge's in a copy of gorge's.
		upto := append([]rules.OracleSnapshot(nil), g.Snapshots[:i+1]...)
		upto[i].Step = xs.Step
		if oraclediff.CompareOpts(rules.OracleResult{Snapshots: upto}, nil,
			oraclediff.XResult{Snapshots: x.Snapshots[:i+1]}, item.Item.Compare, item.Item.Ignore...).Status != oraclediff.Agree {
			return 0
		}
		return i
	}
	return 0
}

// forgeStopsInCleanup reports a Forge difference at step d where Forge stands
// in the cleanup step with a player holding priority. Time Stop and its kind
// end the turn at once (CR 723.1) and no player receives priority in cleanup
// unless a state-based action or a trigger needs it (CR 514.3); Forge's
// PhaseHandler.endTurnByEffect enters CLEANUP from inside the resolution, and
// the resolution's own onStackResolved then hands out priority. Gorge goes on
// to the next turn's first priority. The item's passes run out of step with
// Forge from there, so nothing past it compares.
func forgeStopsInCleanup(x oraclediff.XResult, v oraclediff.Verdict, d int, engine string) bool {
	if engine != "forge" || v.Status != oraclediff.Diverge || d < 0 || d+1 >= len(x.Snapshots) {
		return false
	}
	return x.Snapshots[d+1].Step == "cleanup" && (v.Field == "turn" || v.Field == "step")
}

// forgeElectedCastModes are the cast_mode values the Forge driver can act on:
// StepMachine.spellForMode picks the plot special action, an Adventure or Omen
// half, a modal back face or a warp cast out of Card.getAllPossibleAbilities,
// and the optional-cost modes pay the kicker, buyback, entwine, offspring or
// replicate they name. Any other mode (flashback, foretell, escape, ...) is
// still cast the ordinary way, so the driver takes the card's first spell
// ability and the difference after it is the driver's gap.
var forgeElectedCastModes = []string{"", "optionalcost",
	"plot", "adventure_alt", "omen_alt", "modal_spell", "warped",
	"kicked", "kicked1", "kicked2", "kickedboth", "buyback", "entwined",
	"offspring", "replicated", "multikicked"}

// forgeIgnoresCastMode names, for a Forge difference at step d, the first cast
// step at or before d whose cast_mode the driver does not follow. From that
// cast on, Forge's game is a different one (another spell on the stack, other
// mana spent, nothing plotted), so the difference is the driver's gap and not
// a rules difference. "" for anything else.
func forgeIgnoresCastMode(item lower.Lowered, v oraclediff.Verdict, d int, engine string) string {
	if engine != "forge" || v.Status != oraclediff.Diverge || d < 0 {
		return ""
	}
	for i, st := range item.Item.Steps {
		if i > d {
			break
		}
		if st.Op == "cast" && !slices.Contains(forgeElectedCastModes, st.CastMode) {
			return fmt.Sprintf("the driver does not follow cast_mode %q (step %d, %s)", st.CastMode, i, st.Card)
		}
	}
	return ""
}

// xmageSameNameTriggerOrder names, for an XMage difference at step d, the first
// step at or before d that orders simultaneous triggers from objects with one
// name. XMage's driver matches a scripted trigger order by the ability's rule
// text or its source's name, so two such triggers (two Otter tokens with
// Prowess, one holding a counter) cannot be told apart and XMage orders them
// itself; from there its game differs by the driver's gap, not by a rule.
// "" for anything else.
func xmageSameNameTriggerOrder(item lower.Lowered, v oraclediff.Verdict, d int, engine string) string {
	if engine != "xmage" || v.Status != oraclediff.Diverge || d < 0 {
		return ""
	}
	for i, st := range item.Item.Steps {
		if i > d {
			break
		}
		for _, a := range st.Answers {
			if a.Kind != "trigger_order" {
				continue
			}
			seen := map[string]bool{}
			for _, p := range a.Pick {
				n := oraclediff.RefName(p)
				if seen[n] {
					return fmt.Sprintf("the driver cannot order triggers of objects named %q apart (step %d)", n, i)
				}
				seen[n] = true
			}
		}
	}
	return ""
}

// paymentChoice reports a pool-only difference on the paying seat at or right
// after a cast or activate whose recorded pool spend had a real colour choice
// the follower did not have to take. The difference is then which legal pool
// units paid (CR 601.2h), not a rules divergence. A step with no recorded
// spend, or one that spent a single colour, is not a payment choice (see
// paymentStep). paymentStep is false for an engine that begins manual payment
// with the recorded multiset (XMage): then the follower's split is gorge's, so
// a pool difference is real. Forge's out-of-tree driver still documents
// Item.Payment without consuming it, so a Forge pool split after such a cast is
// a payment choice until that driver lands (cmd/oraclediff/forge.go). Every
// other differing field makes this false, so a pool difference that comes with
// any other change still diverges.
//
// A legal split only chooses which colours pay, not how many units remain: the
// differing pool must belong to the payment step's own seat and hold the same
// number of units as gorge's. Mana on the opponent's seat, or mana appearing or
// vanishing, is a real pool difference (a production or timing bug), never a
// choice.
func paymentChoice(item lower.Lowered, g rules.OracleResult, x oraclediff.XResult, v oraclediff.Verdict, step int, engine string) bool {
	if v.Status != oraclediff.Diverge || !strings.HasSuffix(v.Field, ".pool") || step < 0 {
		return false
	}
	seat, ok := paymentStep(item, g, step, engine)
	if !ok {
		seat, ok = paymentStep(item, g, step-1, engine)
	}
	if !ok {
		return false
	}
	if v.Field != fmt.Sprintf("p%d.pool", seat) {
		return false
	}
	if len(v.Gorge) != len(v.XMage) {
		return false
	}
	ignore := make([]string, 0, len(item.Item.Ignore)+1)
	ignore = append(ignore, item.Item.Ignore...)
	ignore = append(ignore, "pool")
	return oraclediff.CompareOpts(g, nil, x, item.Item.Compare, ignore...).Status == oraclediff.Agree
}

// paymentStep reports whether lowered step i is a cast or activate whose
// recorded pool spend offered a real colour choice for the follower to make
// differently, and the seat that paid. A step with no recorded spend (gorge
// spent nothing, or the item predates the recorder) is not a payment choice:
// no colour was chosen, so a pool difference there is a real one. The payment
// step's starting pool must also hold two or more colours; a one-colour pool
// fixed no choice, so a follower's pool difference there is a real one too.
// engine consumesPayment reports whether that driver applies the recorded
// multiset; when it does, a pool difference is real, not a choice.
func paymentStep(item lower.Lowered, g rules.OracleResult, i int, engine string) (seat int, ok bool) {
	if i < 0 || i >= len(item.Item.Steps) {
		return 0, false
	}
	switch item.Item.Steps[i].Op {
	case "cast", "activate":
	default:
		return 0, false
	}
	if i >= len(item.Item.Payment) || item.Item.Payment[i] == "" {
		return 0, false
	}
	if consumesPayment(engine) {
		return 0, false
	}
	seat = item.Item.Steps[i].Seat
	return seat, poolColours(g, seat, i) >= 2
}

// poolColours is how many distinct colours the payment step's starting pool
// held. The checkpoint before step i is snapshot i (snapshot 0 is the setup),
// so it carries the mana the cast or activation had available, including any
// explicit mana-tap steps lowered ahead of it.
func poolColours(g rules.OracleResult, seat, step int) int {
	if step < 0 || step >= len(g.Snapshots) {
		return 0
	}
	for _, p := range g.Snapshots[step].Players {
		if p.Seat == seat {
			return distinctColours(p.Pool)
		}
	}
	return 0
}

// distinctColours is how many distinct colours a pool string names. A
// one-colour pool (including an empty one) fixed no colour choice, so a
// follower's pool difference cannot be the player's legal split.
func distinctColours(pool string) int {
	var seen [128]bool
	n := 0
	for i := 0; i < len(pool); i++ {
		c := pool[i]
		if c >= 128 || seen[c] {
			continue
		}
		seen[c] = true
		n++
	}
	return n
}

// consumesPayment is the driver capability, not a class of difference: XMage
// queues the recorded multiset as its manual payment (ScenarioReplay.beginPayment).
// The Forge bridge is outside this repository and does not read Item.Payment
// yet, so only XMage consumes one.
func consumesPayment(engine string) bool { return engine == "xmage" }

// missStep matches the step of the only two strict misses Forge poses past the
// item's last step: "Missing cleanup discard answer at step 12: ..." and
// "Missing yesno answer at step 6: optional trigger ...". Any other kind
// (order, surveil, target, mode, ...) is a real shape finding wherever it sits.
var missStep = regexp.MustCompile(`^Missing (?:cleanup discard answer at step (\d+)|yesno answer at step (\d+): optional trigger)`)

// trailingMiss reports a Forge strict miss that is only a question Forge poses
// after the item's last step and nothing else: a cleanup discard when the hand
// is over seven, or an optional trigger's yes/no once the last pass lets the
// trigger resolve. Forge asks it inside that last pass, while gorge stops at
// the pass with the same decision pending and unlogged, so no checkpoint
// observes it and it says nothing about decision shape. The comparison of
// every snapshot then decides the class. A leftover (gorge answered something
// Forge never asked) is a real shape difference, so it is never trailing, and
// a miss at any step the item scripts, or of any other kind, is not either.
func trailingMiss(x oraclediff.XResult, steps int, engine string) bool {
	if engine != "forge" || x.Leftover != "" || x.StrictMiss == "" {
		return false
	}
	m := missStep.FindStringSubmatch(x.StrictMiss)
	if m == nil {
		return false
	}
	n, err := strconv.Atoi(m[1] + m[2])
	return err == nil && n == steps
}

// Credited reports whether an item with this class counts toward the
// "compared" coverage counter: the follower replayed it to a verdict about
// the engine (agree or any divergence), not a gap in a driver or the lowering.
func Credited(class string) bool {
	switch class {
	case ClassAgree, ClassDiverge, ClassRNG, ClassProjectionGap, ClassDecisionShape,
		ClassPaymentChoice, ClassCrewTypeDependency:
		return true
	}
	return false
}

// stepOf is the scenario step of a checkpoint name: "setup" is -1,
// "step N (op)" is N, anything else -1.
func stepOf(cp string) int {
	rest, ok := strings.CutPrefix(cp, "step ")
	if !ok {
		return -1
	}
	n, _, _ := strings.Cut(rest, " ")
	i, err := strconv.Atoi(n)
	if err != nil {
		return -1
	}
	return i
}

// xmageLacks reports an XMage database miss of any card the tape's decks
// hold (the item has no single scenario card).
func xmageLacks(msg string, t *game.Tape) bool {
	if t == nil {
		return false
	}
	for _, d := range t.Decks {
		for _, n := range d.Cards {
			if oraclediff.XMageLacksCard(msg, n) {
				return true
			}
		}
	}
	return false
}

// rngField is true for the fields a shuffle or a random pick changes: a
// library's top cards, a hand, a graveyard's order.
func rngField(field string) bool {
	for _, s := range []string{".library_top", ".hand", ".graveyard"} {
		if strings.HasSuffix(field, s) {
			return true
		}
	}
	return false
}

// shuffledEarlier reports a window whose library order is not compared (a
// shuffle left it out, see Lowered.HasRNG) and whose first shuffle came at or
// before step d: a card drawn or milled from there is each engine's own, so a
// hand or graveyard difference at d is the shuffle's, not a rules difference.
func shuffledEarlier(item lower.Lowered, d int) bool {
	return item.HasRNG && d >= item.RNGStep
}

// rngDrawn reports an engine RNG draw on the tape between the last agreeing
// checkpoint and the diverging step: the draws committed by the entries the
// step d carries out (all those before the setup checkpoint, with the genesis
// shuffle, when d is the setup).
func rngDrawn(item lower.Lowered, t *game.Tape, d int) bool {
	if t == nil || len(t.Entries) == 0 {
		return false
	}
	lo, hi := item.FirstEntry, item.FirstEntry-1
	if d < 0 {
		if t.GenesisRNG > 0 {
			return true
		}
		lo = 0
	} else {
		for i := d - 1; i >= 0 && i < len(item.StepEntry); i-- {
			if e := item.StepEntry[i]; e >= 0 {
				lo = e + 1
				break
			}
		}
		hi = lo - 1
		if d < len(item.StepEntry) && item.StepEntry[d] >= 0 {
			hi = item.StepEntry[d]
		}
	}
	for i := max(lo, 0); i <= hi && i < len(t.Entries); i++ {
		if t.Entries[i].RNG > 0 {
			return true
		}
	}
	return false
}

// projectionGap is a difference the comparison vocabulary cannot decide yet:
// an unmapped keyword ("other:<kw>") on either side, or tokens the engines
// name differently (such fields are gaps, never divergences).
func projectionGap(v oraclediff.Verdict) bool {
	if strings.Contains(v.Gorge, "other:") || strings.Contains(v.XMage, "other:") {
		return true
	}
	return v.Field == "permanents" && tokenNameGap(v.Gorge, v.XMage)
}

// tokenNameGap: every permanent only one side has is a token (the comparison
// renders a token's name as "token"; the remaining difference is its type
// list, the engines' token vocabulary), and once the type lists are dropped
// the two sides' lines are the same.
func tokenNameGap(g, x string) bool {
	if g == "" || x == "" {
		return false
	}
	gl, xl := strings.Split(g, "; "), strings.Split(x, "; ")
	if len(gl) != len(xl) {
		return false
	}
	strip := func(ls []string) (out []string, ok bool) {
		for _, l := range ls {
			if !strings.Contains(l, " token [") {
				return nil, false
			}
			if i, j := strings.IndexByte(l, '['), strings.IndexByte(l, ']'); i >= 0 && j > i {
				l = l[:i] + l[j+1:]
			}
			out = append(out, l)
		}
		return out, true
	}
	gs, ok1 := strip(gl)
	xs, ok2 := strip(xl)
	if !ok1 || !ok2 {
		return false
	}
	sort.Strings(gs)
	sort.Strings(xs)
	return strings.Join(gs, "\n") == strings.Join(xs, "\n")
}
