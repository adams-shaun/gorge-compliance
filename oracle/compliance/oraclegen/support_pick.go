package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// supportAnswers is one Support counter_pick's target answers, retimed to the
// step whose card/ability XMage asks them at.
type supportAnswers struct {
	step    int
	answers []XAnswer
}

// supportPick reports the target answers for a Support counter_pick decision
// and the announcement step they belong on, or ok=false when the decision is
// not a Support pick this route can place.
//
// gorge asks Support$ N at RESOLUTION as a KChoose with ResumeKind
// "counter_pick" (engine/effects/counters_put.go's putCounterSupport), but
// XMage declares it as a target on the source itself: SupportEffect adds a
// TargetCreaturePermanent(0, N) to the instant/sorcery's spell ability
// (SupportEffect.java:25) and SupportAbility adds a TargetPermanent(0, N) to
// the enters-the-battlefield triggered ability (SupportAbility.java:37). The
// ask therefore happens when the object is ANNOUNCED -- the cast step of a
// spell, the step a permanent's trigger is put on the stack, the activate
// step of an activated ability -- and TestPlayer reads it from the TARGET
// queue. A "choice" on the resolution step is never consumed (Unity of
// Purpose, Nissa's Judgment).
//
// The source is resolved by counterPickSource (counter_pick_source.go): the
// stack snapshot at the decision's own step is the authority, with the step's
// own cast/activate card as the fallback, and the resolved ability's effect
// chain must carry PutCounter's Support$ parameter. The announcement step is
// then read from the same snapshots: the first checkpoint at which the source
// stack object is present, minus one, is the step that announced it. That is
// structural, not card- or timing-table-based, so a spell, an ETB trigger, an
// attack trigger and an activated support all land on the step their own
// object first reached the stack.
func supportPick(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, d rules.OracleDecision) (supportAnswers, bool) {
	if reg == nil || d.Step < 0 || d.Resume != "counter_pick" {
		return supportAnswers{}, false
	}
	obj, matched, onStack := counterPickSource(reg, sc, snaps, d, supportMatch)
	if !matched {
		return supportAnswers{}, false
	}
	step := d.Step
	if onStack {
		var ok bool
		step, ok = supportAnnounceStep(snaps, d.Step, obj)
		if !ok {
			// No snapshot shows the object's announcement: withhold rather
			// than move the answer to a step the replay may not consume at.
			return supportAnswers{}, false
		}
	} else if d.Step >= len(sc.Steps) ||
		(sc.Steps[d.Step].Op != "cast" && sc.Steps[d.Step].Op != "activate") {
		// The fallback source came from the decision's own step; only a
		// cast/activate step announces a target there.
		return supportAnswers{}, false
	}
	as := supportTargetAnswers(d)
	if len(as) == 0 || step < 0 || step > d.Step {
		return supportAnswers{}, false
	}
	return supportAnswers{step: step, answers: as}, true
}

// supportAnnounceStep returns the scenario step during which want was put on
// the stack. snaps[0] is the setup checkpoint and snaps[i+1] the checkpoint
// after step i, so the first snapshot index j holding want with snaps[j-1]
// not holding it means step j-1 announced it.
func supportAnnounceStep(snaps []rules.OracleSnapshot, step int, want rules.OracleSnapStack) (int, bool) {
	if step < 0 || step >= len(snaps) || !snapshotHasStack(snaps[step], want) {
		return 0, false
	}
	for j := step; j >= 1; j-- {
		if !snapshotHasStack(snaps[j-1], want) {
			return j - 1, true
		}
	}
	// Present at setup: no gameplay step announced it.
	return 0, false
}

// snapshotHasStack reports whether the checkpoint holds the same stack object
// (same kind, source and printed trigger index). The controller is not part of
// the identity: a copy keeps the bound source ref, and the trigger index is
// what tells two abilities of one source apart.
func snapshotHasStack(snap rules.OracleSnapshot, want rules.OracleSnapStack) bool {
	for _, o := range snap.Stack {
		if o.Kind == want.Kind && o.Source == want.Source && o.Trigger == want.Trigger {
			return true
		}
	}
	return false
}

// supportTargetAnswers is the target queue for one Support pick: one answer per
// chosen creature in pick order, then the explicit close when gorge chose
// fewer than its (already clamped) Max. XMage's TargetPermanent asks for the
// next target while a legal candidate remains; a partial "up to N" pick needs
// the [target_skip] close or its next chooseTarget falls through to the base
// player. Max is the clamp over the eligible creatures, so a pick of every
// eligible creature needs no close even when the printed Support number is
// larger.
func supportTargetAnswers(d rules.OracleDecision) []XAnswer {
	as := make([]XAnswer, 0, len(d.Picks)+1)
	for k, label := range d.Picks {
		as = append(as, XAnswer{Seat: d.Seat, Kind: "target", Value: targetPickValue(d, k, label)})
	}
	if len(d.Picks) < d.Max {
		as = append(as, XAnswer{Seat: d.Seat, Kind: "target", Value: "[target_skip]"})
	}
	return as
}

// supportMatch reports whether one ability is PutCounter's Support$ arm.
func supportMatch(sa *cards.SA) bool {
	return sa.API == "PutCounter" && strings.TrimSpace(sa.Params["Support"]) != ""
}
