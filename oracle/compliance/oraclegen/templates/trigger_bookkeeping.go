package templates

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// The bookkeeping-trigger template (levelb.BookkeepingSub). A Static$ True
// line that records or clears a fact never reaches the stack and changes no
// snapshot field, so a one-step scenario cannot observe it in either engine.
// Its consequence is its CONSUMER's behaviour -- the sibling trigger that
// reads the fact -- possibly on a later turn. The template therefore stages
// the bookkeeping trigger's cause, lets it run, and observes the consumer:
//
//   - record (Spider-Man 2099's land played from a non-hand zone, Oblivious
//     Bookworm's turned-face-up imprint): the record's cause, then the
//     consumer's window. When the cause also satisfies the consumer by itself
//     on turn 1 (a disguise cast ENTERS face down, which the Bookworm counts
//     too), the cause's preparation runs on turn 1 and its recorded event on
//     p0's next turn, where the consumer window is observed again.
//   - reset (a Cleanup/Upkeep/TurnBegin line): the fact is recorded on turn 1
//     (by the sibling record line's cause, or by the consumer's own
//     resolution), the consumer window runs, the scenario advances to p0's
//     next turn and runs the consumer window again.
//   - leaves (a self leaves-the-battlefield cleanup): the consumer records the
//     fact, the source is flickered (CR 400.7: it returns as a new object),
//     and the consumer runs again.
//
// Every plan carries a CONTROL the generator replays but does not emit: the
// same scenario without the bookkeeping trigger's cause (record), with the
// consumer repeated on the same turn (a self-recording reset), or without
// the flicker (leaves). The item is served only when gorge's observation
// differs from its control, so no scenario passes vacuously; the emitted
// item is then compared with XMage like any other (CR 603.2 for the
// consumer, CR 400.7 for the flicker).

// bkSeg is one segment of a bookkeeping plan: a cause's setup and steps, or
// bare steps, settled (every resolve that empties the stack appended) when
// settle is set. window marks a consumer observation window.
type bkSeg struct {
	cause  *triggerCause
	steps  []oraclegen.Step
	xab    []string
	settle bool
	window bool
}

// bkWindow is the step range [from, to] of one observed consumer window;
// steps [from, cause] are the consumer's cause, the rest the resolves that
// settled it.
type bkWindow struct{ from, cause, to int }

// bkBuilt is a played plan: the scenario, its parallel XMage ability texts,
// gorge's result and the step range of every window.
type bkBuilt struct {
	sc      oraclegen.Scenario
	xab     []string
	res     rules.OracleResult
	windows []bkWindow
	// fired[w] reports that window w's consumer reached the stack.
	fired []bool
}

// bookkeepingFires serves one levelb.BookkeepingSub requirement.
func bookkeepingFires(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "trigger bookkeeping: " + why}
	}
	idx, err := strconv.Atoi(req.Slot)
	if err != nil {
		return skip("index " + req.Slot)
	}
	bk, ok := levelb.BookkeepingOf(f, idx)
	if !ok {
		return skip("no consumer reads the fact")
	}
	consumer := &f.Triggers[bk.Consumer]
	csub, cgap, _ := levelb.ClassifyTrigger(f, consumer)
	if cgap != "" {
		return skip("consumer " + cgap)
	}
	ccauses, why := triggerRecipe(reg, f, name, consumer, csub)
	if why != "" || len(ccauses) == 0 {
		return skip("consumer has no recipe: " + why)
	}
	if len(ccauses) > bkMaxConsumerCauses {
		// The recipe lists its causes best first (a long probe list such as
		// every spell a mana-value filter accepts); the plans pair them, so
		// the head bounds the replays.
		ccauses = ccauses[:bkMaxConsumerCauses]
	}
	cslot := strconv.Itoa(bk.Consumer)
	var plans [][]bkSeg
	var controls [][]bkSeg
	var checks []func(plan, control bkBuilt) bool
	add := func(plan, control []bkSeg, check func(plan, control bkBuilt) bool) {
		plans, controls, checks = append(plans, plan), append(controls, control), append(checks, check)
	}
	differ := func(plan, control bkBuilt) bool {
		return bkSignature(reg, name, f, cslot, plan, len(plan.windows)-1) != bkSignature(reg, name, f, cslot, control, len(control.windows)-1)
	}
	switch bk.Kind {
	case levelb.BookkeepingRecord:
		rcauses, why := bookkeepingRecordCauses(reg, f, name, &f.Triggers[idx])
		if why != "" {
			return skip(why)
		}
		for i := range rcauses {
			rc := &rcauses[i]
			for j := range ccauses {
				cc := &ccauses[j]
				if bkCollide(rc, cc) {
					continue
				}
				// Same turn: the record, then the consumer; the control drops
				// the record's own event (the cause's last step).
				add([]bkSeg{bkCauseSeg(rc), bkWindowSeg(cc)},
					[]bkSeg{bkCauseSegWithout(rc), bkWindowSeg(cc)}, differ)
				// Split: the cause's preparation and a consumer window on
				// turn 1, the recorded event and the observed window on p0's
				// next turn.
				if len(rc.steps) > 1 {
					first := bkCauseSegWithout(rc)
					last := bkLastStepSeg(rc)
					add([]bkSeg{first, bkWindowSeg(cc), bkAdvance(), last, bkWindowSeg(cc)},
						[]bkSeg{first, bkWindowSeg(cc), bkAdvance(), bkWindowSeg(cc)}, differ)
				}
			}
		}
	case levelb.BookkeepingReset:
		if bk.Recorder != bk.Consumer {
			rcauses, why := bookkeepingRecordCauses(reg, f, name, &f.Triggers[bk.Recorder])
			if why != "" {
				return skip(why)
			}
			for i := range rcauses {
				for j := range ccauses {
					rc, cc := &rcauses[i], &ccauses[j]
					if bkCollide(rc, cc) {
						continue
					}
					// The fact the record set on turn 1 is gone on turn 3: the
					// two windows must differ. The control is the plan itself.
					plan := []bkSeg{bkCauseSeg(rc), bkWindowSeg(cc), bkAdvance(), bkWindowSeg(cc)}
					add(plan, nil, func(p, _ bkBuilt) bool {
						return bkSignature(reg, name, f, cslot, p, 0) != bkSignature(reg, name, f, cslot, p, 1)
					})
				}
			}
			break
		}
		for _, top := range bkLibraryVariants {
			for i := range ccauses {
				for j := range ccauses {
					c1, c2 := bkWithLibrary(ccauses[i], top), ccauses[j]
					if bkCollide(&c1, &c2) {
						continue
					}
					// The consumer records the fact itself on turn 1 and fires
					// again on p0's next turn; the control repeats it on the
					// same turn, where the held fact must change what the
					// consumer does (it does not fire, or does nothing).
					add([]bkSeg{bkWindowSeg(&c1), bkAdvance(), bkWindowSeg(&c2)},
						[]bkSeg{bkWindowSeg(&c1), bkSameTurnRepeat(&c2), bkWindowSeg(&c2)},
						func(p, c bkBuilt) bool {
							return bkFired(p, 0) && bkFired(p, 1) &&
								bkSignature(reg, name, f, cslot, p, 1) != bkSignature(reg, name, f, cslot, c, 1)
						})
				}
			}
		}
	case levelb.BookkeepingLeaves:
		if bk.Recorder != bk.Consumer {
			return skip("leaves cleanup without a self-recording consumer")
		}
		flick, ok := bookkeepingFlicker(reg, f, name)
		if !ok {
			return skip("no flicker probe for the source")
		}
		for _, top := range bkLibraryVariants {
			for i := range ccauses {
				for j := range ccauses {
					c1, c2 := bkWithLibrary(ccauses[i], top), ccauses[j]
					if i == j || bkCollide(&c1, &c2) {
						continue
					}
					plan := []bkSeg{bkWindowSeg(&c1), flick, bkWindowSeg(&c2)}
					control := []bkSeg{bkWindowSeg(&c1), bkWindowSeg(&c2)}
					add(plan, control, differ)
				}
			}
		}
	default:
		return skip("kind " + bk.Kind)
	}
	for k := range plans {
		plan, ok := bookkeepingPlay(reg, f, name, cslot, plans[k])
		if !ok {
			continue
		}
		var control bkBuilt
		if controls[k] != nil {
			control, ok = bookkeepingPlay(reg, f, name, cslot, controls[k])
			if !ok {
				continue
			}
		}
		if bk.Kind == levelb.BookkeepingLeaves && !checks[k](plan, control) {
			// The consumer fires either way (Superior Foes exiles a card both
			// times): the fact the cleanup released is the FIRST exiled
			// card's play permission. Play it: the plan must allow it and
			// the control must not.
			played, ok := bookkeepingPlayExiledLand(reg, f, name, cslot, plans[k], controls[k], plan)
			if !ok {
				continue
			}
			plan = played
		} else if !checks[k](plan, control) {
			continue
		}
		return bookkeepingItem(reg, f, name, req, cslot, bk, plan)
	}
	return skip(bk.Kind + ": no plan whose consumer observation differs from its control")
}

// bkMaxConsumerCauses bounds the consumer causes the plans pair.
const bkMaxConsumerCauses = 4

// bkLibraryVariants are the p0 library tops a self-recording consumer is
// tried with: none (the filler), creature spells (a "look at the top card,
// you may cast it" consumer records only when it casts; eight, so a castable
// card is still on top after both turns' scries and draws), and two distinct
// lands (an exile-the-top-card consumer, whose first exiled card is played).
var bkLibraryVariants = [][]string{
	nil,
	bkRepeat("Grizzly Bears", 8),
	{"Forest", "Mountain"},
}

func bkRepeat(name string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = name
	}
	return out
}

func bkWithLibrary(c triggerCause, top []string) triggerCause {
	if len(top) > 0 && len(c.libraryTop) == 0 {
		c.libraryTop = append([]string(nil), top...)
	}
	return c
}

// bkCollide reports two causes whose cards share a name in hand or exile: a
// step ref "p0:<name>" would then be ambiguous once the first copy moved.
// The same cause twice collides exactly when it uses such cards.
func bkCollide(a, b *triggerCause) bool {
	names := map[string]bool{}
	for _, n := range append(append([]string(nil), a.hand...), a.exile...) {
		names[n] = true
	}
	for _, n := range append(append([]string(nil), b.hand...), b.exile...) {
		if names[n] {
			return true
		}
	}
	return false
}

func bkCauseSeg(c *triggerCause) bkSeg {
	steps := append(append([]oraclegen.Step(nil), c.prelude...), c.steps...)
	xab := make([]string, len(steps))
	copy(xab, c.preludeXAbility)
	if c.xability != nil {
		copy(xab[len(c.prelude):], c.xability)
	}
	return bkSeg{cause: c, steps: steps, xab: xab, settle: true}
}

// bkCauseSegWithout is the cause minus its last step (the recorded event);
// the setup is kept so plan and control differ by that event alone.
func bkCauseSegWithout(c *triggerCause) bkSeg {
	s := bkCauseSeg(c)
	s.steps, s.xab = s.steps[:len(s.steps)-1], s.xab[:len(s.xab)-1]
	return s
}

// bkLastStepSeg is the cause's last step alone (its setup rides the
// preparation segment).
func bkLastStepSeg(c *triggerCause) bkSeg {
	s := bkCauseSeg(c)
	n := len(s.steps)
	return bkSeg{steps: s.steps[n-1:], xab: s.xab[n-1:], settle: true}
}

func bkWindowSeg(c *triggerCause) bkSeg {
	s := bkCauseSeg(c)
	s.window = true
	return s
}

// bkAdvance moves to p0's next turn (turn 3) and its first main phase. The
// upkeep stop names p0 so both engines skip p1's turn (the driver's
// same-seat turn advance); main1 then stays in that turn.
func bkAdvance() bkSeg {
	return bkSeg{steps: []oraclegen.Step{
		{Op: "pass_to", Seat: 0, Step: "upkeep", Active: "p0"},
		{Op: "pass_to", Seat: 0, Step: "main1"},
	}, xab: []string{"", ""}}
}

// bkSameTurnRepeat reaches the same turn's next chance to repeat cause c: an
// attack cause waits for the next attackers declaration (an additional
// combat phase the consumer itself added); any other cause runs where the
// scenario stands. It only ever builds a control, never an emitted item.
func bkSameTurnRepeat(c *triggerCause) bkSeg {
	steps := append(append([]oraclegen.Step(nil), c.prelude...), c.steps...)
	if len(steps) > 0 && steps[0].Op == "attack" {
		return bkSeg{steps: []oraclegen.Step{{Op: "pass_to", Seat: 0, Decision: "attackers"}}, xab: []string{""}}
	}
	return bkSeg{}
}

// bookkeepingRecordCauses are the causes of a record line's event.
func bookkeepingRecordCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) ([]triggerCause, string) {
	switch t.ModeKind() {
	case cards.TriggerLandPlayed:
		return landPlayedZoneRecipe(reg, f, t)
	case cards.TriggerTurnFaceUp:
		causes, why, _ := turnedFaceUpOtherCause(reg, t, name)
		return causes, why
	}
	return nil, "no record cause for mode " + t.Mode
}

// bookkeepingFlicker returns the step that exiles the source and returns it
// (CR 400.7: a new object): Cloudshift for a creature, Ghostly Flicker on the
// source and a setup Island for any other artifact, creature or land.
func bookkeepingFlicker(reg *cards.Registry, f *cards.Face, name string) (bkSeg, bool) {
	if f.IsCreature() {
		st, ok := castProbe(reg, flickerProbe, "p0:"+name)
		return bkSeg{
			cause: &triggerCause{hand: []string{flickerProbe}},
			steps: []oraclegen.Step{st}, xab: []string{""}, settle: true,
		}, ok
	}
	if !f.IsArtifact() && !f.IsLand() {
		return bkSeg{}, false
	}
	st, ok := castProbe(reg, bkArtifactFlicker, "p0:"+name, "p0:"+bkFlickerPartner)
	return bkSeg{
		cause: &triggerCause{hand: []string{bkArtifactFlicker}, battlefield: []string{bkFlickerPartner}},
		steps: []oraclegen.Step{st}, xab: []string{""}, settle: true,
	}, ok
}

const (
	// bkArtifactFlicker exiles two target artifacts, creatures and/or lands
	// you control and returns them ({2}{U} instant).
	bkArtifactFlicker = "Ghostly Flicker"
	// bkFlickerPartner is Ghostly Flicker's second target.
	bkFlickerPartner = "Island"
)

// bookkeepingPlay builds and replays a plan: the setup of every cause, the
// baseline, then each segment's steps, settled where asked. A declined
// "may" pick is re-scripted to take it (the fact is recorded on the taken
// branch), and a declined free cast of the looked-at card is cast.
func bookkeepingPlay(reg *cards.Registry, f *cards.Face, name, slot string, segs []bkSeg) (bkBuilt, bool) {
	var b bkBuilt
	p0 := oraclegen.Seat{Battlefield: []string{name}}
	var p1 oraclegen.Seat
	for _, s := range segs {
		if s.cause == nil {
			continue
		}
		if !bkMergeCause(&p0, &p1, s.cause) {
			return b, false
		}
	}
	b.sc = oraclegen.Scenario{
		Setup:        map[string]oraclegen.Seat{"p0": p0, "p1": p1},
		SetupAnswers: oraclegen.OpeningHandAnswers(f),
	}
	oraclegen.Baseline(b.sc.Setup, f)
	for _, s := range segs {
		from := len(b.sc.Steps)
		b.sc.Steps = append(b.sc.Steps, s.steps...)
		b.xab = append(b.xab, s.xab...)
		if s.settle || s.window {
			if !bookkeepingSettle(reg, &b) {
				return b, false
			}
		}
		if s.window {
			b.windows = append(b.windows, bkWindow{from: from, cause: from + len(s.steps) - 1, to: len(b.sc.Steps) - 1})
		}
	}
	sc, _ := oraclegen.ChooseTargets(b.sc, b.res.Decisions)
	res, ok := oraclegen.PlaysThrough(reg, sc)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) != len(sc.Steps)+1 {
		return b, false
	}
	b.sc, b.res = sc, res
	for w := range b.windows {
		b.fired = append(b.fired, bkWindowFires(reg, name, f, slot, b, w))
	}
	return b, true
}

// bkWindowFires replays the scenario up to window w's cause and then the
// trigger template's detection variants (passes that stop with the
// consumer's ability on the stack, since a resolve step resolves through
// it), and reports whether the consumer's ability reached the stack.
func bkWindowFires(reg *cards.Registry, name string, f *cards.Face, slot string, b bkBuilt, w int) bool {
	win := b.windows[w]
	prefix := b.sc.Steps[:win.cause+1]
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	priority := oraclegen.Step{Op: "pass_to", Decision: "priority"}
	for _, tail := range [][]oraclegen.Step{nil, passes, append(append([]oraclegen.Step(nil), passes...), priority), {priority}} {
		sc := b.sc
		sc.Steps = append(append([]oraclegen.Step(nil), prefix...), tail...)
		raw, _ := json.Marshal(sc)
		res, err := rules.RunOracleScenarioJSON(reg, raw)
		if err != nil || len(res.Snapshots) <= win.from+1 {
			continue
		}
		if abilityOnStack(res.Snapshots[win.from+1:], stackSourceWants(reg, name, f), slot) {
			return true
		}
	}
	return false
}

// bookkeepingSettle appends the resolves that empty the stack, taking every
// declined optional pick, until the scenario settles with nothing declined.
func bookkeepingSettle(reg *cards.Registry, b *bkBuilt) bool {
	for round := 0; round < 4; round++ {
		raw, _ := json.Marshal(b.sc)
		res, err := rules.RunOracleScenarioJSON(reg, raw)
		if err != nil || len(res.Snapshots) != len(b.sc.Steps)+1 || bkStepFailed(res.Fails) {
			return false
		}
		if len(res.Snapshots[len(res.Snapshots)-1].Stack) != 0 {
			n, settled, ok := oraclegen.Settle(reg, b.sc)
			if !ok {
				return false
			}
			for i := 0; i < n; i++ {
				b.sc.Steps = append(b.sc.Steps, oraclegen.Step{Op: "resolve"})
				b.xab = append(b.xab, "")
			}
			res = settled
		}
		b.res = res
		yes, changed := oraclegen.MayYes(b.sc, res.Decisions)
		yes, played := bkFreeCastYes(yes, res.Decisions)
		if !changed && !played {
			return true
		}
		b.sc = yes
	}
	return false
}

// bkStepFailed reports a step or harness failure (the runner's unused-target
// self-check is not one: ChooseTargets rewrites those steps).
func bkStepFailed(fails []string) bool {
	for _, f := range fails {
		if strings.Contains(f, rules.OracleUnusedTargetMarker) {
			continue
		}
		if strings.HasPrefix(f, "step ") || strings.Contains(f, "harness:") {
			return true
		}
	}
	return false
}

// bkFreeCastYes answers a declined "you may cast the looked-at card" offer
// (a resolve-time play ask the fallback passed) with its first option.
func bkFreeCastYes(sc oraclegen.Scenario, ds []rules.OracleDecision) (oraclegen.Scenario, bool) {
	out := sc
	out.Steps = append([]oraclegen.Step(nil), sc.Steps...)
	changed := false
	for _, d := range ds {
		if d.Resume != "play" || len(d.Picks) != 0 || d.First == "" || d.Step < 0 || d.Step >= len(out.Steps) ||
			!strings.HasSuffix(d.Via, "fallback") || d.GorgeKind != "modes" {
			continue
		}
		st := out.Steps[d.Step]
		st.Answers = append(append([]oraclegen.Answer(nil), st.Answers...), oraclegen.Answer{Kind: d.GorgeKind, Pick: []string{d.First}})
		out.Steps[d.Step] = st
		changed = true
	}
	return out, changed
}

// bkMergeCause adds a cause's setup to the seats. Causes whose shape needs
// the trigger template's own placement (the source in hand, counters, a
// back face, a tapped source, an Aura bearer) are refused.
func bkMergeCause(p0, p1 *oraclegen.Seat, c *triggerCause) bool {
	if c.selfInHand || c.castSelfX || c.selfBackUp || c.selfFrontUp || c.auraBearer != "" ||
		len(c.counters) > 0 || len(c.opponentCounters) > 0 || len(c.tapped) > 0 ||
		c.activateCost != "" || c.preludeActivationCost != "" || c.choicesAsTargets {
		return false
	}
	p0.Hand = appendFixtureCounts(p0.Hand, c.hand)
	p0.Exile = appendFixtureCounts(p0.Exile, c.exile)
	p0.Battlefield = appendFixtureCounts(p0.Battlefield, c.battlefield)
	p0.Graveyard = appendFixtureCounts(p0.Graveyard, c.graveyard)
	if len(c.libraryTop) > 0 {
		if len(p0.LibraryTop) > 0 && strings.Join(p0.LibraryTop, "|") != strings.Join(c.libraryTop, "|") {
			return false
		}
		p0.LibraryTop = append([]string(nil), c.libraryTop...)
	}
	p1.Hand = appendFixtureCounts(p1.Hand, c.opponentHand)
	p1.Graveyard = appendFixtureCounts(p1.Graveyard, c.opponentGraveyard)
	p1.Battlefield = appendFixtureCounts(p1.Battlefield, c.opponentBattlefield)
	return true
}

// bkFired reports whether the consumer's ability reached the stack inside
// window w.
func bkFired(b bkBuilt, w int) bool {
	return w >= 0 && w < len(b.fired) && b.fired[w]
}

// bkSignature is what window w did: whether the consumer fired, and each
// seat's life, hand, graveyard, exile and battlefield deltas across it.
func bkSignature(reg *cards.Registry, name string, f *cards.Face, slot string, b bkBuilt, w int) string {
	if w < 0 || w >= len(b.windows) {
		return "no window"
	}
	win := b.windows[w]
	if win.to+2 > len(b.res.Snapshots) {
		return "short"
	}
	before, after := b.res.Snapshots[win.from], b.res.Snapshots[win.to+1]
	sig := fmt.Sprintf("fired=%v", bkFired(b, w))
	for i := range after.Players {
		if i >= len(before.Players) {
			break
		}
		a, z := before.Players[i], after.Players[i]
		sig += fmt.Sprintf(" p%d:life%+d hand%+d gy%+d ex%+d", i, z.Life-a.Life, len(z.Hand)-len(a.Hand),
			len(z.Graveyard)-len(a.Graveyard), len(z.Exile)-len(a.Exile))
	}
	sig += fmt.Sprintf(" bf%+d", len(after.Permanents)-len(before.Permanents))
	return sig
}

// bookkeepingPlayExiledLand appends, to a leaves plan whose consumer fires
// either way, the play of the land the FIRST consumer window exiled: the
// plan (source flickered between the windows) must play it, the control
// (no flicker) must not.
func bookkeepingPlayExiledLand(reg *cards.Registry, f *cards.Face, name, cslot string, plan, control []bkSeg, played bkBuilt) (bkBuilt, bool) {
	if len(played.windows) == 0 {
		return bkBuilt{}, false
	}
	w := played.windows[0]
	before, after := played.res.Snapshots[w.from], played.res.Snapshots[w.to+1]
	land := ""
	for _, n := range after.Players[0].Exile {
		if bkHas(before.Players[0].Exile, n) {
			continue
		}
		if c, ok := reg.Lookup(n); ok && len(c.Faces) > 0 && c.Faces[0].IsLand() {
			land = n
			break
		}
	}
	if land == "" {
		return bkBuilt{}, false
	}
	play := bkSeg{steps: []oraclegen.Step{{Op: "play", Seat: 0, Card: "p0:" + land}}, xab: []string{""}, settle: true}
	p, ok := bookkeepingPlay(reg, f, name, cslot, append(append([]bkSeg(nil), plan...), play))
	if !ok {
		return bkBuilt{}, false
	}
	if _, ok := bookkeepingPlay(reg, f, name, cslot, append(append([]bkSeg(nil), control...), play)); ok {
		return bkBuilt{}, false
	}
	return p, true
}

// bookkeepingItem emits the served plan. A last window in which the
// consumer did not fire (a reset that withdrew the consumer's condition)
// leaves no changed field to freeze, so its first step carries the
// trigger_on_stack want:false expectation.
func bookkeepingItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, cslot string, bk levelb.Bookkeeping, b bkBuilt) (oraclegen.Item, *oraclegen.Skip) {
	sc := b.sc
	if n := len(b.windows); n > 0 && !bkFired(b, n-1) {
		w := b.windows[n-1]
		want := false
		last := w.from
		for i := w.from; i <= w.to; i++ {
			if sc.Steps[i].Op == "pass_to" {
				last = i
			}
		}
		st := sc.Steps[last]
		st.Expect = append(append([]oraclegen.Expect(nil), st.Expect...), oraclegen.Expect{TriggerOnStack: "p0:" + name, Want: &want})
		sc.Steps = append([]oraclegen.Step(nil), sc.Steps...)
		sc.Steps[last] = st
		res, ok := oraclegen.PlaysThrough(reg, sc)
		if !ok || len(res.Fails) != 0 {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "trigger bookkeeping: the not-fired expectation does not hold"}
		}
		b.res = res
	}
	sc, castSteps := oraclegen.ChooseTargets(sc, b.res.Decisions)
	cr := []string{"603.2"}
	if bk.Kind == levelb.BookkeepingLeaves {
		// The flickered source returns as a new object.
		cr = append(cr, "400.7")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, TriggerFires.Version, cr, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(b.res, sc, oraclegen.ModeNumbers(f), castSteps)
	for _, x := range b.xab {
		if x != "" {
			it.XAbility = append([]string(nil), b.xab...)
			break
		}
	}
	for i, st := range sc.Steps {
		text := i < len(it.XAbility) && it.XAbility[i] != ""
		if stepNeedsXAbility(st) != text {
			return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "trigger bookkeeping: activate step without an XMage ability text"}
		}
	}
	return it, nil
}

func bkHas(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
