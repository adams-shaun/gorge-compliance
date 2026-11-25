// Package xprep makes a lowered xengine item something the XMage driver can
// replay. The driver runs in strict choose mode: every decision the replay
// poses needs a scripted answer (Item.XAnswers), every activate step the rule
// text XMage selects the ability by (Item.XAbility), and a card XMage spells
// differently needs Item.XMageName. The single-card generator fills those per
// card; a lowered window or game holds many cards, so Prepare assembles the
// same pieces per item, from the result gorge's own runner gave the item.
//
// An item Prepare cannot serve is refused for XMage with a reason from the
// closed Reasons list; it is never guessed at.
package xprep

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/rules"
)

// Refusal reasons: a closed list.
const (
	// ReasonSpelling: the item holds a card XMage spells differently
	// (Item.XMageName names one card only).
	ReasonSpelling = decks.ReasonXMageSpelling
	// ReasonModeCollision: two cards that are cast, activated or played in
	// the item give one mode label different numbers; the answer map is keyed
	// by label.
	ReasonModeCollision = "xmage_mode_collision"
	// ReasonAbilityUnknown: an activate step has no XMage rule-text prefix (a
	// token or a card the corpus does not know, an ambiguous face, a step that
	// names no ability index).
	ReasonAbilityUnknown = "xmage_ability_unknown"
	// ReasonRetarget: rewriting the cast steps' targets from gorge's own target
	// decisions changed what gorge does with the item.
	ReasonRetarget = "xmage_retarget_diverges"
	// ReasonNoResult: there is no gorge result to script the answers from.
	ReasonNoResult = "xmage_no_result"
	// ReasonNoXMageCard: the item names a card no XMage set lists, so the driver
	// could not place it. The game is Forge's alone.
	ReasonNoXMageCard = "xmage_lacks_card"
	// ReasonWindowOnly: a tape item; XMage replays windows only (design O4:
	// the driver's turn model cannot follow a tape, so tapes go to Forge and
	// the kernel).
	ReasonWindowOnly = "xmage_window_only"
	// ReasonColourMismatch: a fixed-colour mana ability's XMage text names a
	// colour other than the one gorge picked for the activation. Dropping the
	// answer would hide a disagreement between the two engines' ability
	// selection, so the item is refused instead.
	ReasonColourMismatch = "xmage_colour_mismatch"
	// ReasonModalTrigger: a modal enters trigger's CR 603.3c mode choice (and
	// the asks it opens) falls after the window's last checkpoint, so gorge's
	// result holds no decision for it, and completing it from gorge's own
	// continued replay did not decide it.
	ReasonModalTrigger = "xmage_modal_trigger_unresolved"
	// ReasonCommander: the XMage driver seats a TwoPlayerDuel and refuses
	// command-zone setup, so a Commander item cannot be replayed there.
	ReasonCommander = "xmage_commander"
)

// Reasons is every reason Prepare refuses with.
var Reasons = []string{ReasonSpelling, ReasonModeCollision, ReasonAbilityUnknown, ReasonRetarget, ReasonNoResult, ReasonNoXMageCard, ReasonWindowOnly, ReasonColourMismatch, ReasonModalTrigger, ReasonCommander}

// tapeID matches the id of a tape item: "...:tape" or a prefix "...:tape:t<N>".
var tapeID = regexp.MustCompile(`:tape(:t\d+)?$`)

// IsTape reports whether id names a tape item (a whole tape or one of its
// turn prefixes) rather than a window.
func IsTape(id string) bool { return tapeID.MatchString(id) }

// Refusal is an item that was not prepared, and why.
type Refusal struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

func refuse(id, reason, format string, a ...any) *Refusal {
	return &Refusal{ID: id, Reason: reason, Detail: fmt.Sprintf(format, a...)}
}

// Prepare is l's item with the XMage fields filled from g, gorge's own result
// for it (SelfCheck's Check.Result), or the Refusal that says why it cannot.
// The result is a pure function of its arguments.
//
//   - Answers: the cast and activate steps' targets are rewritten to gorge's
//     own target picks (oraclegen.ChooseTargets) and the rest of the decisions
//     are scripted by oraclegen.XAnswersForScenario, which also restores a
//     divided spell's shares (oraclegen.CastDivisions). The mode numbers are the
//     union over every face of every card the item holds (stepped cards first).
//   - A modal enters trigger that resolves after the window's last checkpoint
//     is completed from gorge's own continued replay (completeModalTrigger);
//     when that completion cannot decide it, the item is refused rather than
//     replayed with an unscripted ask.
//   - Ability prefixes: oraclegen.XMageAbility of the step card's face.
//   - Spellings: an item holding one card XMage spells differently is served
//     with Item.Card and Item.XMageName stamped (the driver carries exactly
//     one such spelling); an item holding two or more is refused.
//
// Item.XTargetSkips is the lowering's own (lower.Lowered plans it from the
// cast's target decisions); Prepare keeps a step's plan only while the
// retarget leaves that step's targets as the plan was derived from, else the
// driver keeps its own handling of the step.
func Prepare(reg *cards.Registry, sp *decks.Spellings, l lower.Lowered, g rules.OracleResult) (oraclegen.Item, *Refusal) {
	it := l.Item
	if it.Format == oraclegen.FormatCommander {
		return it, refuse(it.ID, ReasonCommander, "format commander; the XMage driver plays no Commander game")
	}
	if bad := differing(reg, sp, it); len(bad) > 0 {
		corpus, xm, ok := loneSpelling(reg, sp, it)
		if !ok {
			return it, refuse(it.ID, ReasonSpelling, "%s", strings.Join(bad, "; "))
		}
		it.Card, it.XMageName = corpus, xm
	}
	if len(g.Snapshots) == 0 {
		return it, refuse(it.ID, ReasonNoResult, "gorge result has no snapshots")
	}
	modes, why := modeNumbers(reg, it, g.Decisions)
	if why != "" {
		return it, refuse(it.ID, ReasonModeCollision, "%s", why)
	}
	abilities, ref := prefixes(reg, it, g)
	if ref != nil {
		return it, ref
	}
	if bad := unlisted(reg, sp, it); len(bad) > 0 {
		return it, refuse(it.ID, ReasonNoXMageCard, "%s", strings.Join(bad, "; "))
	}
	sc, castSteps := oraclegen.ChooseTargets(it.Scenario, g.Decisions)
	if why := sameOutcome(reg, it, sc, g); why != "" {
		return it, refuse(it.ID, ReasonRetarget, "%s", why)
	}
	it.XTargetSkips = keepTargetSkips(it.XTargetSkips, it.Scenario.Steps, sc.Steps)
	it.Scenario = sc
	g.Decisions = oraclegen.RetimeEntryTargets(sc.Steps, g.Snapshots, keepTokenLooks(playStepUnlessPays(sc.Steps, g.Decisions)))
	g.Decisions = append(g.Decisions, pendingResolutionDecisions(reg, it, sc, g)...)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, g, sc, modes, castSteps)
	if len(it.XAnswers) == 0 {
		// Nothing to script: still one (empty) entry per step, as the
		// generator's activate and combat templates do.
		it.XAnswers = make([][]oraclegen.XAnswer, len(sc.Steps))
	}
	if ref := completeModalTrigger(reg, &it, sc, g, modes); ref != nil {
		return it, ref
	}
	for i := range sc.Steps {
		if comboColourStep(abilities, i) {
			// The prefix names the colour, so XMage asks no colour question and
			// a queued colour answer would be left over. Drop only the colour
			// answers: a trigger's target, an ordering or a may answer queued
			// on the same step must stay or the FIFO queue desyncs.
			colour := strings.TrimSuffix(strings.TrimPrefix(abilities[i], comboPrefix), "}")
			kept, mismatched := dropFixedColourAnswers(it.XAnswers[i], colour)
			if mismatched != "" {
				return it, refuse(it.ID, ReasonColourMismatch, "step %d: combo mana text %q names {%s} but the queued answer is %q", i, abilities[i], colour, mismatched)
			}
			it.XAnswers[i] = kept
			continue
		}
		colour, fixed := fixedColourStep(abilities, i)
		if !fixed {
			continue
		}
		// The ability text itself names the colour, so XMage selects that
		// fixed ability and asks nothing; a queued colour answer would be a
		// leftover that desyncs the FIFO choice queue. A pick or queued answer
		// naming another colour is a disagreement, not a leftover: refuse it
		// rather than drop it silently.
		if picked, ok := fixedColourPick(g.Decisions, i); ok && picked != colour {
			return it, refuse(it.ID, ReasonColourMismatch, "step %d: fixed mana text %q names {%s} but gorge picked {%s}", i, abilities[i], colour, picked)
		}
		kept, mismatched := dropFixedColourAnswers(it.XAnswers[i], colour)
		if mismatched != "" {
			return it, refuse(it.ID, ReasonColourMismatch, "step %d: fixed mana text %q names {%s} but the queued answer is %q", i, abilities[i], colour, mismatched)
		}
		it.XAnswers[i] = kept
	}
	it.XAbility = abilities
	it.XPos = passPositions(sc.Steps, g.Snapshots)
	it.XAnswers = addDieRollAnswers(it.XAnswers, it.DieRolls, len(sc.Steps))
	return it, nil
}

// addDieRollAnswers scripts each recorded die roll on the step that made it.
// XMage's TestPlayer.rollDieResult consumes a "[die_roll]: N" token from the
// choice queue, which ScenarioReplay queues from a kind "die" answer, so the
// driver forces the tape's natural roll instead of rolling its own.
//
// The die leads its step's answers, mirroring oraclegen's stepFlips: the
// CHOICE queue is FIFO and shared by the die token and every queued label,
// and a die the step's action rolls before it asks anything (Ebony Fly
// animates only after the roll) must be at the head. Appending it behind a
// "yes" leaves the queue ["Yes", "[die_roll]: 3"], and TestPlayer.rollDieResult
// consumes only a head that starts with DIE_ROLL: it strict-misses and the
// follower rolls its own die. Rolls outside the item's steps (a truncated
// tail) are not scripted.
func addDieRollAnswers(answers [][]oraclegen.XAnswer, rolls []oraclegen.DieRoll, steps int) [][]oraclegen.XAnswer {
	if len(rolls) == 0 {
		return answers
	}
	if len(answers) < steps {
		grown := make([][]oraclegen.XAnswer, steps)
		copy(grown, answers)
		answers = grown
	}
	// Collect per step in log order, then prepend the batch: prepending each
	// roll would reverse a multi-die step's order.
	lead := make([][]oraclegen.XAnswer, steps)
	for _, r := range rolls {
		if r.Step < 0 || r.Step >= steps {
			continue
		}
		lead[r.Step] = append(lead[r.Step], oraclegen.XAnswer{
			Seat: r.Seat, Kind: "die", Value: strconv.Itoa(r.Natural),
		})
	}
	for step := range lead {
		if len(lead[step]) > 0 {
			answers[step] = append(lead[step], answers[step]...)
		}
	}
	return answers
}

// pendingResolutionDecisions returns the decisions a stack object poses when
// it resolves only after the window's last checkpoint. A lowered window cut
// after the pass that hands priority away leaves a spell (or an ability) on
// the stack, so gorge's own result holds no decision for the enters trigger
// that resolution puts on the stack; XMage runs on to the stop boundary and
// asks it with nothing scripted ("Missing TARGET def", Disruptive Stormbrood's
// "destroy up to one target artifact or enchantment"). The completion replays
// the same scenario one priority round further through gorge's own runner and
// retimes the asks it poses onto the window's last step, where the driver
// still holds its queue. Nothing is guessed: an unwindowed resolution that
// asks nothing adds nothing, and a continuation that fails leaves the old
// strict miss in place rather than inventing an answer.
func pendingResolutionDecisions(reg *cards.Registry, it oraclegen.Item, sc oraclegen.Scenario, g rules.OracleResult) []rules.OracleDecision {
	if len(sc.Steps) == 0 || len(g.Snapshots) != len(sc.Steps)+1 {
		return nil
	}
	last := len(sc.Steps) - 1
	if sc.Steps[last].Op != "pass" || len(g.Snapshots[last+1].Stack) == 0 {
		return nil
	}
	players := len(g.Snapshots[last+1].Players)
	if players < 2 {
		players = 2
	}
	steps := slices.Clone(sc.Steps)
	seat := sc.Steps[last].Seat
	for pass := 0; pass < players; pass++ {
		seat = (seat + 1) % players
		steps = append(steps, oraclegen.Step{Op: "pass", Seat: seat})
		probe := sc
		probe.Steps = steps
		res, err := rules.RunOracleScenarioJSON(reg, it.RawScenario(probe))
		if err != nil || len(res.Fails) > 0 || len(res.Snapshots) != len(steps)+1 {
			return nil
		}
		// The pass moved the game on instead of resolving an object at the
		// cut's own step: the driver's stop boundary is already past it, so a
		// later step's trigger is not this window's ask.
		after := res.Snapshots[len(res.Snapshots)-1]
		if after.Turn != g.Snapshots[last+1].Turn || after.Step != g.Snapshots[last+1].Step {
			return nil
		}
		var span []rules.OracleDecision
		for _, d := range res.Decisions {
			if d.Step >= len(sc.Steps) {
				span = append(span, d)
			}
		}
		if len(span) > 0 {
			for _, d := range span {
				if d.Kind == "mode" {
					// A modal enters trigger's CR 603.3c mode choice is
					// completeModalTrigger's: it also corrects a surveil look's
					// queue, and its span carries the targets that follow. Leave
					// that path alone instead of racing it with a mode answer.
					return nil
				}
			}
			for i := range span {
				span[i].Step = last
			}
			return span
		}
		if len(after.Stack) == 0 {
			return nil
		}
	}
	return nil
}

// completeModalTrigger scripts the CR 603.3c mode choice of a modal enters
// trigger whose resolution falls after the item's last checkpoint (and the
// target, arrange and choice asks that mode opens). A lowered window cut at
// the pass that hands priority away leaves the permanent's spell still on the
// stack, so gorge's own result records no decision for the trigger; XMage
// nevertheless resolves the spell while its driver runs on to the end of the
// turn and asks MODE (and then its targets) with nothing scripted.
//
// The completion replays the same scenario one step further through gorge's
// own runner: the appended pass_to settles the pending trigger with the
// runner's documented fallback (the first Min eligible modes, then those
// asks' own fallbacks), so the decisions are gorge's, not a reimplementation
// of CR 700.2 legality. The translator that scripts every other item
// (oraclegen.XAnswers) then turns them into XMage answers on the window's
// last step, where the driver still holds them when it runs past the cut.
//
// The completion is only attempted when a modal enters trigger of a cast or
// play step has no recorded mode decision (recordedTriggerMode) and the
// checkpoint shows the trigger can fire (entersTriggerFires); a window where
// gorge did answer the mode keeps exactly its own answer. A continuation that
// poses no decision at all means the trigger did not fire, so the window's
// answers are kept unchanged. Anything else the completion cannot decide — a
// cut in an unknown step, a failure in gorge's continued run, another
// trigger's mode asked first, an untranslatable span — refuses the item with
// ReasonModalTrigger rather than guessing.
func completeModalTrigger(reg *cards.Registry, it *oraclegen.Item, sc oraclegen.Scenario, g rules.OracleResult, modes map[string]int) *Refusal {
	if len(sc.Steps) == 0 || len(it.XAnswers) != len(sc.Steps) || len(g.Snapshots) == 0 {
		return nil
	}
	type pending struct {
		step   int
		card   string
		labels map[string]int
	}
	var pend []pending
	for i, st := range sc.Steps {
		if st.Op != "cast" && st.Op != "play" {
			continue
		}
		c, face, ok := stepCardIn(reg, *it, sc.Steps, i)
		if !ok {
			continue
		}
		f := c.Faces[face]
		valid, modal := modalEntersCharm(f)
		if !modal {
			continue
		}
		if !entersTriggerFires(valid, st.Card, g.Snapshots, i) {
			continue
		}
		labels := oraclegen.ModeNumbers(f)
		if recordedTriggerMode(g.Decisions, st.Card, st.Op, i, labels) {
			continue
		}
		pend = append(pend, pending{i, st.Card, labels})
	}
	if len(pend) == 0 {
		return nil
	}
	if len(pend) > 1 {
		return refuse(it.ID, ReasonModalTrigger, "%d unresolved modal enters triggers (%s, %s); their ask order is not decidable", len(pend), pend[0].card, pend[1].card)
	}
	p := pend[0]
	last := len(sc.Steps) - 1
	if sc.Steps[last].Op != "pass" {
		return refuse(it.ID, ReasonModalTrigger, "step %d casts %q with a modal enters trigger, but the window does not end on a pass", p.step, p.card)
	}
	next, ok := stepAfterPass(g.Snapshots[len(g.Snapshots)-1].Step)
	if !ok {
		return refuse(it.ID, ReasonModalTrigger, "the window ends in %q, whose next step is unknown", g.Snapshots[len(g.Snapshots)-1].Step)
	}
	probe := sc
	probe.Steps = append(slices.Clone(sc.Steps), oraclegen.Step{
		Op: "pass_to", Step: next, Active: fmt.Sprintf("p%d", g.Snapshots[len(g.Snapshots)-1].Active),
	})
	res, err := rules.RunOracleScenarioJSON(reg, it.RawScenario(probe))
	if err != nil {
		return refuse(it.ID, ReasonModalTrigger, "%s: gorge could not continue the window: %v", p.card, err)
	}
	if len(res.Fails) > 0 {
		return refuse(it.ID, ReasonModalTrigger, "%s: gorge could not continue the window: %s", p.card, res.Fails[0])
	}
	var span []rules.OracleDecision
	for _, d := range res.Decisions {
		if d.Step == len(sc.Steps) {
			span = append(span, d)
		}
	}
	if len(span) == 0 {
		// Gorge's own continuation posed no decision at all: the trigger did
		// not fire (a Card.Self+untapped land that entered tapped, or an
		// ability removed modeless), so there is nothing to script. Keep the
		// window's own answers rather than refusing an item the follower can
		// replay; a refusal here would turn a clean row into not_sent.
		return nil
	}
	if span[0].Kind != "mode" || !modeDecisionOf(span[0], p.card) {
		return refuse(it.ID, ReasonModalTrigger, "%s: the first ask after the window is not its mode decision", p.card)
	}
	// The span is the trigger's own ask sequence: the mode, then the targets,
	// arranges and choices it opens, up to the next modal ask (another
	// trigger's mode). Decision order is the only binding gorge records
	// between a mode and its follow-up asks, and XMage asks them in the same
	// order. A discard picker shares the mode kind but sits inside the
	// trigger's sequence, so only a charm mode of another source ends the
	// span.
	for j := 1; j < len(span); j++ {
		if span[j].Kind == "mode" && span[j].Resume != "discard" && !modeDecisionOf(span[j], p.card) {
			span = span[:j]
			break
		}
	}
	// The trigger's own mode numbers win over the item-wide union: the union is
	// keyed by mode label across the whole item, so the firing card's own
	// printed order is the one XMage asks.
	pm := make(map[string]int, len(modes)+len(p.labels))
	for k, v := range modes {
		pm[k] = v
	}
	for k, v := range p.labels {
		pm[k] = v
	}
	for j := range span {
		span[j].Step = last
	}
	// The span's decisions are retimed onto the window's last step so their
	// answers lead it, but the checkpoint that proves a top library placement
	// still belongs to the continuation step (len(sc.Steps)). Reindex that
	// one snapshot onto the retimed step; without it the router would have no
	// destination evidence and would not reverse a top arrange.
	spanSnaps := make([]rules.OracleSnapshot, len(sc.Steps)+1)
	if after := len(sc.Steps) + 1; after < len(res.Snapshots) {
		spanSnaps[last+1] = res.Snapshots[after]
	}
	add := oraclegen.XAnswersWithSnapshots(span, len(sc.Steps), pm, spanSnaps)[last]
	if len(add) == 0 || add[0].Kind != "mode" {
		return refuse(it.ID, ReasonModalTrigger, "%s: gorge's continuation decided no mode for the trigger", p.card)
	}
	add = arrangeLibrarySelection(add, span)
	it.XAnswers[last] = append(it.XAnswers[last], add...)
	return nil
}

// arrangeLibrarySelection corrects oraclegen's keep-all arrange answer for the
// surveil/scry look: that arm scripts the look selection as a target skip
// because the generator's scenarios reach XMage's target-shaped
// chooseTarget. The replay driver instead exposes the look through the choice
// queue (ScriptedChoicePlayer.doSurveil/doScry's scriptedLibrarySelection), so
// a target skip would be left unused, and the kept-card order
// (putCardsOnTopOfLibrary) asks once per card but the topmost one. Replace the
// trailing target skip with the choice-queue skip that keeps every card, then
// the order answers: gorge's picks are the final top-first order, so the
// last len-1 picks, reversed, are the order the dialog asks (the pick never
// named ends on top). A partial selection's answers already carry their own
// skip and order.
func arrangeLibrarySelection(add []oraclegen.XAnswer, span []rules.OracleDecision) []oraclegen.XAnswer {
	for _, d := range span {
		if d.GorgeKind != "arrange" || d.Min != 0 || d.Options < 2 || len(d.Picks) != d.Options ||
			(d.ArrangeKind != "graveyard" && d.ArrangeKind != "bottom") {
			continue
		}
		if len(add) == 0 {
			return add
		}
		last := add[len(add)-1]
		if last.Kind != "target" || last.Value != "[target_skip]" {
			continue
		}
		add = append(add[:len(add)-1], oraclegen.XAnswer{Seat: d.Seat, Kind: "choice", Value: "[choice_skip]"})
		for k := len(d.Picks) - 1; k >= 1; k-- {
			add = append(add, oraclegen.XAnswer{Seat: d.Seat, Kind: "choice", Value: d.Picks[k]})
		}
		return add
	}
	return add
}

// modalEntersCharm reports whether the face has an enters-the-battlefield
// trigger (including "enters or attacks") whose body is a Charm with more than
// one mode, and returns that trigger's ValidCard filter. Only a Card.Self
// trigger is read: that is the one a cast or play step of the card itself can
// fire.
func modalEntersCharm(f *cards.Face) (string, bool) {
	for i := range f.Triggers {
		tr := &f.Triggers[i]
		if !strings.EqualFold(tr.Mode, "ChangesZone") || tr.Params["Destination"] != "Battlefield" {
			continue
		}
		valid := tr.Params["ValidCard"]
		if !strings.HasPrefix(valid, "Card.Self") {
			continue
		}
		if tr.Effect == nil || tr.Effect.API != "Charm" {
			continue
		}
		if len(effects.CharmOf(tr.Effect).Modes) > 1 {
			return valid, true
		}
	}
	return "", false
}

// entersTriggerFires reports whether the Card.Self changes-zone trigger with
// the given ValidCard filter can fire for the permanent the step at index
// step put on the battlefield. A cast step's permanent is still on the stack
// at that checkpoint, so only a play step is decidable; a missing checkpoint
// or permanent leaves the trigger pending (the conservative reading).
//
// The tapped/untapped predicates are the decidable half of the filter and the
// land cycle's "enters tapped unless ..." is the common case: a
// Card.Self+untapped trigger of a land that entered tapped never fires, and
// treating it as pending would refuse a window that correctly asks nothing.
func entersTriggerFires(valid, ref string, snaps []rules.OracleSnapshot, step int) bool {
	if step+1 >= len(snaps) {
		return true
	}
	wantUntapped, wantTapped := false, false
	for _, cond := range strings.FieldsFunc(valid, func(r rune) bool { return r == '+' || r == ',' }) {
		switch {
		case strings.EqualFold(strings.TrimSpace(cond), "untapped"):
			wantUntapped = true
		case strings.EqualFold(strings.TrimSpace(cond), "tapped"):
			wantTapped = true
		}
	}
	if !wantUntapped && !wantTapped {
		return true
	}
	for _, p := range snaps[step+1].Permanents {
		if p.Ref != ref {
			continue
		}
		return (!wantUntapped || !p.Tapped) && (!wantTapped || p.Tapped)
	}
	return true
}

// recordedTriggerMode reports whether gorge's result already holds a mode
// decision for the modal enters trigger of a step. A cast's spell mode is
// decided on the cast step itself, while its enters trigger can only be
// decided later (the spell must resolve first); a played land's trigger is
// decided on the play step.
func recordedTriggerMode(ds []rules.OracleDecision, ref, op string, step int, labels map[string]int) bool {
	for _, d := range ds {
		if d.Kind != "mode" {
			continue
		}
		if op == "cast" && d.Step <= step || op == "play" && d.Step < step {
			continue
		}
		if !modeDecisionOf(d, ref) {
			continue
		}
		if len(d.Picks) == 0 {
			return true // a declined mode is still the trigger's decision
		}
		for _, pick := range d.Picks {
			if _, ok := labels[pick]; ok {
				return true
			}
		}
	}
	return false
}

// modeDecisionOf reports whether a mode decision names the card ref as its
// source. A mode decision carries the source on each option (and on each
// pick); the "#N" duplicate suffix is ignored.
func modeDecisionOf(d rules.OracleDecision, ref string) bool {
	for _, r := range d.OptionRefs {
		if sameCardRef(r, ref) {
			return true
		}
	}
	for _, r := range d.PickRefs {
		if sameCardRef(r, ref) {
			return true
		}
	}
	return false
}

// sameCardRef compares two scenario object refs, ignoring the "#N" suffix a
// duplicate carries.
func sameCardRef(a, b string) bool { return suffixCut(a) == suffixCut(b) }

// suffixCut drops a trailing "#N" from a scenario ref.
func suffixCut(ref string) string {
	if j := strings.LastIndexByte(ref, '#'); j >= 0 && j+1 < len(ref) && strings.Trim(ref[j+1:], "0123456789") == "" {
		return ref[:j]
	}
	return ref
}

// stepAfterPass is the step a pass leaves when both seats pass on an empty
// stack. A cut in the end step or a step with no successor is not followed.
func stepAfterPass(step string) (string, bool) {
	switch step {
	case "untap":
		return "upkeep", true
	case "upkeep":
		return "draw", true
	case "draw":
		return "main1", true
	case "main1":
		return "begin-combat", true
	case "begin-combat":
		return "declare-attackers", true
	case "declare-attackers":
		return "declare-blockers", true
	case "declare-blockers":
		return "combat-damage", true
	case "combat-damage":
		return "end-combat", true
	case "end-combat":
		return "main2", true
	case "main2":
		return "end", true
	}
	return "", false
}

// playStepUnlessPays rewrites the paid as-enters "unless you pay" decision of a
// land's play step to the yes/no shape XMage's TapSourceUnlessPaysEffect asks.
// Gorge records it as a two-option mode decision; the generic router now emits
// the boolean yes for every paid unless cost too, but this rewrite keeps the
// play-step shape named locally and answers it the same way (a decline is
// already "choice no" from the generic router).
func playStepUnlessPays(steps []oraclegen.Step, ds []rules.OracleDecision) []rules.OracleDecision {
	out := append([]rules.OracleDecision(nil), ds...)
	for i := range out {
		d := &out[i]
		if d.Step < 0 || d.Step >= len(steps) || steps[d.Step].Op != "play" ||
			d.Kind != "mode" || d.Resume != "unless_pay" || d.Options < 2 ||
			len(d.Picks) == 0 || !paidUnlessLabel(d.Picks[0]) {
			continue
		}
		// xanswersSetup's yesno branch emits choice "yes" for a "Pay ..." pick.
		d.Kind = "yesno"
	}
	return out
}

// TokenLook reports whether d is a surveil/scry arrange (graveyard or bottom,
// Min 0) whose offered cards are all tokens, whatever it answered. A token that left the battlefield cannot move to another zone
// (CR 111.8), so gorge's pick over it moves nothing: binning the token is the
// same final state as keeping it. The followers do not share that state.
// Vanish from Sight's "owner puts it on top, then surveil 1" leaves gorge
// looking at the token with the card under it still in the library, while a
// follower that drops the token on arrival looks at that card instead, so a
// scripted bin (the token's name, found nowhere in the look) is unmatched, or
// an empty pick bins the card under the token. Selecting by "every option is a
// token" instead of by card keeps the next token-on-top card covered. Every
// engine's lowering of such a look goes through this one predicate: xprep's
// keep-all skip for XMage and forge-export's naming of the cards under the
// token for Forge.
//
// A look of N>=2 that mixes tokens and real cards is not matched: the
// follower's look shifts by the tokens it dropped, and no single keep-all
// answer is the gorge outcome there.
func TokenLook(d rules.OracleDecision) bool {
	if d.GorgeKind != "arrange" || d.Min != 0 || d.Options < 1 ||
		(d.ArrangeKind != "graveyard" && d.ArrangeKind != "bottom") ||
		len(d.OptionRefs) != d.Options || len(d.ArrangeLabels) != d.Options {
		return false
	}
	for _, ref := range d.OptionRefs {
		if !strings.Contains(ref, ":token:") {
			return false
		}
	}
	return true
}

// keepTokenLooks rewrites every TokenLook to the keep-all answer, which lowers
// to XMage's choice-queue skip. Keep-all is the one answer that is the gorge
// outcome whether or not the follower still holds the token. The input is not
// mutated.
func keepTokenLooks(ds []rules.OracleDecision) []rules.OracleDecision {
	out := append([]rules.OracleDecision(nil), ds...)
	for i := range out {
		d := &out[i]
		// A pick that already keeps every card lowers to the skip as it is.
		if !TokenLook(*d) || len(d.PickIdx) == d.Options {
			continue
		}
		d.Picks = slices.Clone(d.ArrangeLabels)
		d.PickRefs = slices.Clone(d.OptionRefs)
		d.ObjectPicks = slices.Clone(d.OptionRefs)
		d.PickIdx = make([]int, d.Options)
		for k := range d.PickIdx {
			d.PickIdx[k] = k
		}
	}
	return out
}

// paidUnlessLabel is whether an unless-pay pick's label names paying the cost
// rather than declining it. It mirrors oraclegen's unexported unlessPolarity;
// the labels are the engine's, so the two must stay in sync.
func paidUnlessLabel(label string) bool {
	l := strings.ToLower(strings.TrimSpace(label))
	if l == "" {
		return false
	}
	for _, neg := range []string{"don't", "do not", "decline", "no", "refuse"} {
		if strings.HasPrefix(l, neg) {
			return false
		}
	}
	return true
}

// passPositions is Item.XPos: for each pass step i, where gorge stood in
// snaps[i+1] (the checkpoint after step i; snaps[0] is the setup one), and
// whether that pass moved the turn or step or removed an object from the
// stack. Nil when the snapshots do not line up with the steps, which leaves
// the driver on the pass shapes it knows.
//
// Gorge runs both damage steps of a first-strike combat (CR 510.4) under the
// one step name, so they are told apart here. The first holds from entering
// the name with a first or double striker in combat until the non-active seat
// passes on an empty stack: it can only hold priority there once the active
// seat has passed too, so that pass ends the step (CR 117.4). This reads
// forward only, so a window cut short inside the first step is still placed.
// An additional step (CR 500.9) likewise repeats the name of the one before it;
// newStepInstance marks the pass that enters it as moved.
func passPositions(steps []oraclegen.Step, snaps []rules.OracleSnapshot) []*oraclegen.XPos {
	if len(snaps) != len(steps)+1 {
		return nil
	}
	var out []*oraclegen.XPos
	any, inFirst := false, false
	// run is the passes in succession on the current stack: every seat passing
	// one after another resolves its top object (CR 117.4), which no snapshot
	// diff can show when the resolution's own trigger stands in its place.
	// round is every pass in a row since the last action, step entry or
	// resolution: only a full round of those passes can end a step (CR 117.4),
	// and the pass that completes it need not be the non-active seat's when an
	// action split the round.
	run, round := 0, 0
	for i, st := range steps {
		if st.Op != "pass" {
			out = append(out, nil)
			run, round = 0, 0
			continue
		}
		any = true
		round++
		before, after := snaps[i], snaps[i+1]
		moved := before.Turn != after.Turn || before.Step != after.Step
		resolves := stackLost(before.Stack, after.Stack)
		switch {
		case moved || resolves || len(before.Stack) == 0:
			run = 0
		default:
			run++
			if run >= max(len(before.Players), 2) {
				resolves, run = true, 0
			}
		}
		switch {
		case moved:
			inFirst = after.Step == stepCombatDamage && firstStrikerInCombat(before)
		case inFirst:
			// The pass that completes the round on an empty stack ends the
			// first damage step (CR 117.4): a mana ability between the passes
			// only hands priority back, and the round's last pass can then be
			// the active seat's. Priority is not read here: the regular damage
			// step can pause on a damage-division ask, an as-unblocked
			// election or a state-based-action decision before priority is
			// given, so the checkpoint still shows the passer holding it.
			if len(before.Stack) == 0 && round >= max(len(before.Players), 2) {
				inFirst, moved = false, true
			}
		case newStepInstance(before, after, round):
			// The step name did not change, but the pass ended one step and
			// began an additional one of the same name (CR 500.9), such as an
			// extra end step.
			moved = true
		}
		// A pass that entered a new step -- a turn or step change, the end of
		// the first damage step, or an additional same-named step -- or
		// resolved an object starts the next round: the new step gives
		// priority to the active seat, and a resolution hands it back (CR
		// 117.4). Without this reset, a normal round that ended one step would
		// leave its passes counted in the next one.
		if moved || resolves {
			round = 0
		}
		step := after.Step
		if inFirst {
			step = oraclegen.StepFirstStrikeDamage
		}
		out = append(out, &oraclegen.XPos{
			Turn: after.Turn, Step: step, Active: after.Active,
			Moved:    moved,
			Resolves: resolves,
		})
	}
	if !any {
		return nil
	}
	return out
}

// newStepInstance is whether a pass that left the turn and step name alone
// still ended the step and began a new step of the same name. Ending a step
// needs a full round of passes with no action in between (CR 117.4), so round
// counts the passes since the last action, step entry or resolution; a pass
// that follows another player's action -- a mana ability while holding
// priority, say -- only hands priority on and leaves the game in the same step
// instance. The round's last pass is not assumed to be the non-active seat's:
// an action can leave it with the active seat, which is still the last to pass
// (the split pair's active pass). A pass over a non-empty stack resolves the
// top object (CR 117.4) and is the stack's business, not the step's; a fresh
// step gives priority to the active seat (CR 117.3a).
func newStepInstance(before, after rules.OracleSnapshot, round int) bool {
	return round >= max(len(before.Players), 2) &&
		len(before.Stack) == 0 && !after.Over &&
		before.Turn == after.Turn && before.Step == after.Step &&
		after.Priority == after.Active
}

// stepCombatDamage is gorge's name for the combat damage step.
const stepCombatDamage = "combat-damage"

// firstStrikerInCombat is whether an attacking or blocking creature has first
// strike or double strike, which gives the combat a first damage step.
func firstStrikerInCombat(s rules.OracleSnapshot) bool {
	for _, p := range s.Permanents {
		if p.Attacking || p.Blocking {
			if slices.Contains(p.Keywords, "First Strike") || slices.Contains(p.Keywords, "Double Strike") {
				return true
			}
		}
	}
	return false
}

// stackLost is whether after lacks an object before holds (compared as a
// multiset of kind, source and controller): a pass never removes an object
// but by resolving it, and a trigger that resolution puts on does not hide it.
func stackLost(before, after []rules.OracleSnapStack) bool {
	left := map[rules.OracleSnapStack]int{}
	for _, o := range after {
		left[o]++
	}
	for _, o := range before {
		if left[o] == 0 {
			return true
		}
		left[o]--
	}
	return false
}

// sameOutcome is "" when gorge's runner, given sc and the item's recorded die
// rolls, ends in the snapshots g holds, else what differs.
func sameOutcome(reg *cards.Registry, it oraclegen.Item, sc oraclegen.Scenario, g rules.OracleResult) string {
	res, err := rules.RunOracleScenarioJSON(reg, it.RawScenario(sc))
	switch {
	case err != nil:
		return "rerun: " + err.Error()
	case len(res.Fails) > 0:
		return "rerun fails: " + res.Fails[0]
	case !reflect.DeepEqual(res.Snapshots, g.Snapshots):
		return "rerun snapshots differ from the item's result"
	}
	return ""
}

// refName is the card name a scenario ref ("p0:Forest#2") names; ok is false
// for a token, an ability, a player or anything else that is no corpus card.
func refName(ref string) (string, bool) {
	n := ref
	if i := strings.IndexByte(n, ':'); i >= 2 && n[0] == 'p' && strings.Trim(n[1:i], "0123456789") == "" {
		n = n[i+1:]
	} else if i >= 0 {
		return "", false
	}
	if strings.HasPrefix(n, "token:") || strings.HasPrefix(n, "ability:") {
		return "", false
	}
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n, n != ""
}

// isTokenRef reports whether a scenario ref names a token ("p0:token:Treasure
// Token#2").
func isTokenRef(ref string) bool {
	if i := strings.IndexByte(ref, ':'); i >= 2 && ref[0] == 'p' && strings.Trim(ref[1:i], "0123456789") == "" {
		return strings.HasPrefix(ref[i+1:], "token:")
	}
	return false
}

// abilitySourceName is the face name an activate step's card ref names,
// including a token ref's face ("p0:token:Soldier#2" -> "Soldier"). It is the
// sourceName XMageGrantedAbilityPrefix rewrites a self-referential cost with.
func abilitySourceName(st oraclegen.Step) string {
	n := st.Card
	if i := strings.IndexByte(n, ':'); i >= 2 && n[0] == 'p' && strings.Trim(n[1:i], "0123456789") == "" {
		n = n[i+1:]
	}
	n = strings.TrimPrefix(n, "token:")
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n
}

// CardNames is every card name the item's setup and steps hold, sorted: the
// names a follower must have in its card database. A player ref, a token ref
// ("pN:token:Name") and an ability ref are not cards.
func CardNames(it oraclegen.Item) []string { return cardNames(it) }

// cardNames is every card name the item's setup and steps hold, sorted.
func cardNames(it oraclegen.Item) []string {
	seen := map[string]bool{}
	add := func(names ...string) {
		for _, n := range names {
			if n != "" {
				seen[n] = true
			}
		}
	}
	addRef := func(refs ...string) {
		for _, r := range refs {
			if n, ok := refName(r); ok {
				add(n)
			}
		}
	}
	for _, s := range it.Setup {
		add(s.Battlefield...)
		add(s.Tapped...)
		add(s.BackFace...)
		add(s.Hand...)
		add(s.Graveyard...)
		add(s.Exile...)
		add(s.Library...)
		add(s.LibraryTop...)
		for n := range s.Counters {
			add(n)
		}
	}
	for _, st := range it.Steps {
		addRef(st.Card, st.AttachedTo)
		addRef(st.Targets...)
		addRef(st.Attackers...)
		for _, b := range st.Blocks {
			addRef(b[0], b[1])
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// differing is the item's cards XMage spells differently, as "Corpus -> XMage".
func differing(reg *cards.Registry, sp *decks.Spellings, it oraclegen.Item) []string {
	var out []string
	for _, n := range cardNames(it) {
		if xm, ok := sp.XMage(reg, n); ok && xm != n {
			out = append(out, n+" -> "+xm)
		}
	}
	return out
}

// loneSpelling is the corpus and XMage spellings of the item's one differing
// card, when the driver's single Item.Card/Item.XMageName pair can carry it:
// exactly one distinct card differs, and the item names neither a different
// card nor a different XMage spelling already.
func loneSpelling(reg *cards.Registry, sp *decks.Spellings, it oraclegen.Item) (corpus, xm string, ok bool) {
	for _, n := range cardNames(it) {
		x, known := sp.XMage(reg, n)
		if !known || x == n {
			continue
		}
		if corpus != "" {
			return "", "", false
		}
		corpus, xm = n, x
	}
	if corpus == "" || (it.Card != "" && it.Card != corpus) || (it.XMageName != "" && it.XMageName != xm) {
		return "", "", false
	}
	return corpus, xm, true
}

// unlisted is the item's corpus cards no manifest lists under any spelling; a
// player ref or a token name is not a card.
func unlisted(reg *cards.Registry, sp *decks.Spellings, it oraclegen.Item) []string {
	var out []string
	for _, n := range cardNames(it) {
		if _, known := reg.Lookup(n); known && !sp.Lists(reg, n) {
			out = append(out, n)
		}
	}
	return out
}

// stepCardIn is the corpus card and face index step i of steps names; ok is
// false for a token, an ability or a card the corpus does not know. A
// permanent the seat's setup places on its back face is face 1, as the Forge
// export reads it. A permanent the item cast back face up (cast_mode "modal_spell", CR 712.12) stays on its back face for
// every later step naming the same ref (CR 712.8e), so the step reads the back
// face's abilities, not the front face the ref's name spells. The latest
// earlier cast of the ref decides; a later cast without modal_spell is the
// front face again.
func stepCardIn(reg *cards.Registry, it oraclegen.Item, steps []oraclegen.Step, i int) (*cards.Card, int, bool) {
	st := steps[i]
	c, face, ok := stepCardFace(reg, it, st)
	if !ok || len(c.Faces) < 2 {
		return c, face, ok
	}
	for j := i; j >= 0; j-- {
		p := steps[j]
		if p.Card != st.Card || (p.Op != "cast" && p.Op != "play") {
			continue
		}
		if strings.EqualFold(p.CastMode, "modal_spell") {
			face = 1
		}
		break
	}
	return c, face, ok
}

func stepCardFace(reg *cards.Registry, it oraclegen.Item, st oraclegen.Step) (*cards.Card, int, bool) {
	n, ok := refName(st.Card)
	if !ok {
		return nil, 0, false
	}
	c, ok := reg.Lookup(n)
	if !ok || len(c.Faces) == 0 {
		return nil, 0, false
	}
	face := 0
	for i, f := range c.Faces {
		if f.Name == n {
			face = i
			break
		}
	}
	if seat, _, has := strings.Cut(st.Card, ":"); has {
		for _, b := range it.Setup[seat].BackFace {
			if b == n && len(c.Faces) > 1 {
				face = 1
			}
		}
	}
	return c, face, true
}

// modeNumbers is the union of oraclegen.ModeNumbers over every face of every
// card the item casts, activates or plays, or why two cards disagree on a
// label. A card the item only places (battlefield, hand, graveyard, ...) can
// own a modal trigger or an enters mode that resolves in the window, so its
// labels are added too (CR 700.2: the printed mode number is what XMage
// reads, not gorge's position among the offered modes). Such a bystander's
// label never overrides a stepped card's; a label that bystanders number
// differently is dropped, and refused only when a recorded decision actually
// picks it (the printed number is then unknowable from the label alone).
func modeNumbers(reg *cards.Registry, it oraclegen.Item, ds []rules.OracleDecision) (map[string]int, string) {
	out := map[string]int{}
	owner := map[string]string{}
	done := map[string]bool{}
	add := func(into map[string]int, who map[string]string, n string, c *cards.Card) (string, bool) {
		for _, f := range c.Faces {
			m := oraclegen.ModeNumbers(f)
			labels := make([]string, 0, len(m))
			for k := range m {
				labels = append(labels, k)
			}
			sort.Strings(labels)
			for _, k := range labels {
				if oraclegen.IsPrintedMaxKey(k) {
					// A printed maximum is not a mode number: two cards that
					// share a label may print different maxima, and the first
					// owner's stays (a stepped card is added before bystanders).
					if _, seen := into[k]; !seen {
						into[k] = m[k]
					}
					continue
				}
				if prev, seen := into[k]; seen && prev != m[k] {
					return fmt.Sprintf("mode %q is %d on %s and %d on %s", k, prev, who[k], m[k], n), false
				}
				if _, seen := into[k]; !seen {
					into[k], who[k] = m[k], n
				}
			}
		}
		return "", true
	}
	for _, st := range it.Steps {
		if st.Op != "cast" && st.Op != "activate" && st.Op != "play" {
			continue
		}
		n, ok := refName(st.Card)
		if !ok || done[n] {
			continue
		}
		done[n] = true
		c, ok := reg.Lookup(n)
		if !ok {
			continue
		}
		if why, ok := add(out, owner, n, c); !ok {
			return nil, why
		}
	}
	picked := map[string]bool{}
	for _, d := range ds {
		for _, p := range d.Picks {
			picked[p] = true
		}
	}
	extra := map[string]int{}
	extraOwner := map[string]string{}
	ambiguous := map[string]string{}
	for _, n := range cardNames(it) {
		if done[n] {
			continue
		}
		c, ok := reg.Lookup(n)
		if !ok {
			continue
		}
		one := map[string]int{}
		oneOwner := map[string]string{}
		add(one, oneOwner, n, c) // a card's own faces may disagree; the first face wins
		labels := make([]string, 0, len(one))
		for k := range one {
			labels = append(labels, k)
		}
		sort.Strings(labels)
		for _, k := range labels {
			if prev, seen := extra[k]; seen && prev != one[k] {
				ambiguous[k] = fmt.Sprintf("mode %q is %d on %s and %d on %s", k, prev, extraOwner[k], one[k], n)
			} else if !seen {
				extra[k], extraOwner[k] = one[k], n
			}
		}
	}
	labels := make([]string, 0, len(extra))
	for k := range extra {
		labels = append(labels, k)
	}
	sort.Strings(labels)
	for _, k := range labels {
		if _, seen := out[k]; seen {
			continue
		}
		if why, bad := ambiguous[k]; bad {
			if picked[k] {
				return nil, why
			}
			continue
		}
		out[k] = extra[k]
	}
	return out, ""
}

// prefixes is Item.XAbility: parallel to the steps, the XMage rule-text prefix
// of every activate step's ability and of a cast step that is the Plot,
// Suspend or Foretell special action, and "" elsewhere (nil when no step carries one). An activate step
// whose prefix is unknown is a Refusal.
//
// A granted/gained/keyword-granted step is resolved through the same
// oraclegen.ResolveActivatedAbility forge-export and the neutral exporter use,
// and its prefix is derived from the resolved *cards.SA: the printed face's
// XMage text would select the wrong ability (the index aliases printed 0 for
// an SVar grant). A step whose label resolves to no grant still refuses rather
// than shipping the printed prefix.
func prefixes(reg *cards.Registry, it oraclegen.Item, g rules.OracleResult) ([]string, *Refusal) {
	cache := map[*cards.Face]map[int]string{}
	cached := map[*cards.Face]string{}
	var out []string
	for i, st := range it.Steps {
		// Plot is not an activate step: gorge offers it as a cast option
		// (cast_mode "plot") and XMage replays it as the PlotAbility special
		// action, selected by its rule text "Plot {cost}". The Forge face's
		// K:Plot:<cost> keyword gives the cost; a face without one falls back
		// to the bare "Plot" prefix, which still selects the only PlotAbility.
		if st.Op == "cast" && strings.EqualFold(st.CastMode, "plot") {
			if out == nil {
				out = make([]string, len(it.Steps))
			}
			out[i] = "Plot"
			if c, face, ok := stepCardIn(reg, it, it.Steps, i); ok {
				if cost, ok := c.Faces[face].KeywordCostParam("Plot"); ok && cost != "" {
					out[i] = "Plot " + xmageManaText(cost)
				}
			}
			continue
		}
		// Suspend from hand is the same shape (CR 702.62a, 116.2f): gorge
		// records the special action as a cast step with cast_mode "suspend"
		// and XMage replays it as the SuspendAbility special action, selected
		// by its rule-text prefix "Suspend N" (the time-counter count the
		// Forge keyword K:Suspend:<N>:<cost> leads with, or "X"). XMage's own
		// SuspendTest selects it by that prefix; the driver scopes the pick
		// to the step's card.
		if st.Op == "cast" && strings.EqualFold(st.CastMode, "suspend") {
			if out == nil {
				out = make([]string, len(it.Steps))
			}
			out[i] = "Suspend"
			if c, face, ok := stepCardIn(reg, it, it.Steps, i); ok {
				if param, ok := c.Faces[face].KeywordParam("Suspend"); ok {
					if n, _, _ := strings.Cut(param, ":"); strings.TrimSpace(n) != "" {
						out[i] = "Suspend " + strings.TrimSpace(n)
					}
				}
			}
			continue
		}
		// Foretell from hand is the same shape (CR 702.143a, 116.2h): a cast
		// step with cast_mode "foretell", replayed as XMage's ForetellAbility
		// special action, whose rule text leads "Foretell {cost}" (the Forge
		// K:Foretell:<cost> keyword gives the cost). A face without one falls
		// back to the bare "Foretell" prefix, which the driver scopes to the
		// step's card.
		if st.Op == "cast" && strings.EqualFold(st.CastMode, "foretell") {
			if out == nil {
				out = make([]string, len(it.Steps))
			}
			out[i] = "Foretell"
			if c, face, ok := stepCardIn(reg, it, it.Steps, i); ok {
				if cost, ok := c.Faces[face].KeywordCostParam("Foretell"); ok && cost != "" {
					out[i] = "Foretell " + xmageManaText(cost)
				}
			}
			continue
		}
		if st.Op != "activate" {
			continue
		}
		switch sa, kind := oraclegen.ResolveActivatedAbility(reg, it, i, st, g); kind {
		case oraclegen.ActivatedGranted:
			// The recipient's own texts decide whether the cost alone selects
			// the granted ability: "{T}" also starts an Island's "{T}: Add {U}.".
			var recipient *cards.Face
			if c, face, ok := stepCardIn(reg, it, it.Steps, i); ok {
				recipient = c.Faces[face]
			} else if isTokenRef(st.Card) {
				if tf, ok := oraclegen.TokenStepFace(reg, it, i, st, g); ok {
					recipient = tf
				}
			}
			p, ok := oraclegen.XMageGrantedAbilityPrefix(sa, abilitySourceName(st), recipient)
			if !ok {
				return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d activates %q by granted-ability label %q, whose XMage rule text is unknown", i, st.Card, st.Ability)
			}
			if out == nil {
				out = make([]string, len(it.Steps))
			}
			out[i] = p
			continue
		case oraclegen.ActivatedGrantedUnresolved:
			return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d activates %q by a granted-ability label that resolves to no unique grant", i, st.Card)
		}
		if st.Ability != "" {
			return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d activates %q by granted-ability label %q, which resolves to no grant", i, st.Card, st.Ability)
		}
		if st.AbilityIndex == nil {
			return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d activates %q by label %q, which names no ability index", i, st.Card, st.Ability)
		}
		var f *cards.Face
		if c, face, ok := stepCardIn(reg, it, it.Steps, i); ok {
			f = c.Faces[face]
		} else if isTokenRef(st.Card) {
			// A token's activated ability is the one its creating effect's
			// token script defines (CR 111.1, 111.4): resolve the ref to that
			// compiled script and take its face's XMage text.
			tf, ok := oraclegen.TokenStepFace(reg, it, i, st, g)
			if !ok {
				return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d activates token %q, which no compiled token script with an ability %d names", i, st.Card, *st.AbilityIndex)
			}
			f = tf
		} else {
			return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d activates %q, which is no corpus card", i, st.Card)
		}
		m, have := cache[f]
		if !have {
			m, cached[f] = oraclegen.XMageAbility(f)
			cache[f] = m
		}
		if why := cached[f]; why != "" {
			return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d: %s: %s", i, f.Name, why)
		}
		p := m[*st.AbilityIndex]
		if p == "" {
			return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d: %s has no XMage text for ability %d", i, f.Name, *st.AbilityIndex)
		}
		if out == nil {
			out = make([]string, len(it.Steps))
		}
		out[i] = comboColourPrefix(p, f, *st.AbilityIndex, st)
	}
	return out, nil
}

// xmageManaText renders a Forge mana cost ("RW RW", "3", "G") in XMage's
// rule-text braces, including hybrid halves: "RW" is "{R/W}". It mirrors the
// generator's identically named helper in oraclegen/templates, which xprep
// must not import: that package is the Level B generator above this one. The
// two renderings must agree on the cost a step pays, so keep them in sync.
func xmageManaText(cost string) string {
	var b strings.Builder
	for _, sym := range strings.Fields(cost) {
		switch {
		case sym == "0":
			b.WriteString("{0}")
		case strings.Trim(sym, "0123456789") == "":
			b.WriteString("{" + sym + "}")
		case len(sym) == 1:
			b.WriteString("{" + sym + "}")
		case len(sym) == 2 && strings.Contains("WUBRG", sym[:1]) && strings.Contains("WUBRG", sym[1:]):
			b.WriteString("{" + sym[:1] + "/" + sym[1:] + "}")
		case len(sym) == 2 && sym[0] == '2' && strings.Contains("WUBRG", sym[1:]):
			b.WriteString("{2/" + sym[1:] + "}")
		default:
			b.WriteString("{" + sym + "}")
		}
	}
	return b.String()
}

// comboColourStep reports that prefixes named a colour for step i.
func comboColourStep(abilities []string, i int) bool {
	return i < len(abilities) && strings.HasPrefix(abilities[i], comboPrefix) && strings.HasSuffix(abilities[i], "}")
}

const comboPrefix = "{T}: Add {"

// comboColourPrefix names the colour step makes when the ability is a tap for one of
// several colours (Temple of Deceit's "{T}: Add {U} or {B}"). XMage prints that as one
// ability per colour, so the bare cost would always pick the first.
//
// The prefix XMageAbility returned is either the bare "{T}" or, when another mana
// ability shares that cost (Gleaming Bastion's "{T}: Add {C}."), "{T}" extended to
// the first colour member ("{T}: Add {W}"). Both are rewritten to the recorded
// colour; a prefix ending in a period is a fixed one-colour text and stays. Other
// recorded answers on the step (an arrange ask) do not hide the colour pick.
func comboColourPrefix(p string, f *cards.Face, ability int, st oraclegen.Step) string {
	if p != "{T}" && (!strings.HasPrefix(p, comboPrefix) || strings.HasSuffix(p, ".")) {
		return p
	}
	if ability < 0 || ability >= len(f.Abilities) {
		return p
	}
	colour, ok := recordedComboColour(st)
	if !ok {
		return p
	}
	if _, combo := effects.ComboColours(effects.ManaOf(f.Abilities[ability]).Produced); !combo {
		return p
	}
	return comboPrefix + colour + "}"
}

// recordedComboColour is the one colour a step's recorded answers pick, from an
// answer with a single pick naming a mana colour ("B" or "Add B"). Zero or several
// such answers name no colour.
func recordedComboColour(st oraclegen.Step) (string, bool) {
	colour, n := "", 0
	for _, a := range st.Answers {
		if len(a.Pick) != 1 {
			continue
		}
		label := strings.TrimPrefix(strings.TrimSpace(a.Pick[0]), "Add ")
		if len(label) == 1 && strings.Contains("WUBRGC", label) {
			colour = label
			n++
		}
	}
	return colour, n == 1
}

// fixedColourStep reports the colour letter an activate step's XMage ability
// text names when it is a fixed single-colour mana text of the form
// "{T}: Add {X}." (X one of W, U, B, R, G). A basic-typed dual land gets one
// such intrinsic ability per basic type, and XMage selects it by this text.
// The trailing period distinguishes it from the Combo prefix comboColourPrefix
// rewrites to ("{T}: Add {X}", no period), which XMage asks a colour for.
func fixedColourStep(abilities []string, i int) (string, bool) {
	if i >= len(abilities) || !strings.HasPrefix(abilities[i], comboPrefix) || !strings.HasSuffix(abilities[i], "}.") {
		return "", false
	}
	colour := strings.TrimSuffix(strings.TrimPrefix(abilities[i], comboPrefix), "}.")
	if len(colour) != 1 || !strings.Contains("WUBRG", colour) {
		return "", false
	}
	return colour, true
}

// fixedColourPick is the colour letter gorge picked for the fixed-colour mana
// activation at step, from the pick's "Add B" label (or a bare "B").
func fixedColourPick(ds []rules.OracleDecision, step int) (string, bool) {
	for _, d := range ds {
		if d.Step != step || d.Via != "mana ability" {
			continue
		}
		for _, p := range d.Picks {
			label := strings.TrimPrefix(strings.TrimSpace(p), "Add ")
			if len(label) == 1 && strings.Contains("WUBRG", label) {
				return label, true
			}
		}
	}
	return "", false
}

// colourChoiceLetter translates an XMage colour-choice value to its WUBRG
// letter; ok is false for a choice answer that names no colour.
func colourChoiceLetter(v string) (string, bool) {
	switch v {
	case "White":
		return "W", true
	case "Blue":
		return "U", true
	case "Black":
		return "B", true
	case "Red":
		return "R", true
	case "Green":
		return "G", true
	}
	return "", false
}

// dropFixedColourAnswers removes the colour answers queued for an activate
// step whose XMage text already names the colour: XMage selects that fixed
// ability and poses no colour question, so a queued colour would be left over
// and poison the next choice in the FIFO queue. A colour answer naming another
// colour is returned as mismatched instead of dropped: that is a disagreement
// Prepare refuses, never a silent loss. Answers of any other kind, and choice
// answers that name no colour, stay.
func dropFixedColourAnswers(answers []oraclegen.XAnswer, colour string) (kept []oraclegen.XAnswer, mismatched string) {
	for _, a := range answers {
		if a.Kind == "choice" {
			if got, ok := colourChoiceLetter(a.Value); ok {
				if got != colour {
					return nil, a.Value
				}
				continue
			}
		}
		kept = append(kept, a)
	}
	return kept, ""
}

// keepTargetSkips is the lowering's XTargetSkips plan restricted to the steps
// whose targets the retarget left unchanged: a plan names positions in the
// step's flat target list, so a rewritten list invalidates it. before and
// after are the item's steps before and after oraclegen.ChooseTargets.
func keepTargetSkips(plan [][]oraclegen.XTargetSkip, before, after []oraclegen.Step) [][]oraclegen.XTargetSkip {
	if len(plan) == 0 {
		return nil
	}
	var out [][]oraclegen.XTargetSkip
	for i, list := range plan {
		if len(list) == 0 || i >= len(before) || i >= len(after) || !slices.Equal(before[i].Targets, after[i].Targets) {
			continue
		}
		if out == nil {
			out = make([][]oraclegen.XTargetSkip, len(plan))
		}
		out[i] = list
	}
	return out
}
