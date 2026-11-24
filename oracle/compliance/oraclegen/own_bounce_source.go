package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// isOwnBounceSA reports a mandatory "return a <permanent> you control to its
// owner's hand" effect: Forge's hidden ChangeZone from the battlefield to the
// hand that the controller answers by choosing one of their own permanents
// (Kor Skyfisher, the Karoo and bounce-land family). XMage poses these through
// ReturnToHandChosenControlledPermanentEffect, a TargetControlledPermanent
// the effect asks with chooseTarget, so TestPlayer reads it from the TARGET
// queue. A different chooser, an optional or "up to" count, a pay-or-bounce
// and a multi-object ChangeNum are other effect classes and stay off this
// route.
func isOwnBounceSA(sa *cards.SA) bool {
	if sa == nil || sa.API != "ChangeZone" {
		return false
	}
	p := sa.Params
	if !strings.EqualFold(p["Hidden"], "True") || !strings.EqualFold(p["Mandatory"], "True") ||
		!strings.EqualFold(p["Origin"], "Battlefield") || !strings.EqualFold(p["Destination"], "Hand") ||
		!strings.Contains(p["ChangeType"], "YouCtrl") {
		return false
	}
	for _, k := range []string{"Chooser", "DefinedPlayer", "Optional", "UnlessCost", "UnlessPayer", "ChooseFromDefined", "ValidTgts"} {
		if p[k] != "" {
			return false
		}
	}
	if n := strings.TrimSpace(p["ChangeNum"]); n != "" && n != "1" {
		return false
	}
	return true
}

// ownBounceInChain reports whether the ability chain starting at sa carries
// an own-bounce effect.
func ownBounceInChain(sa *cards.SA) bool {
	seen := map[*cards.SA]bool{}
	for ; sa != nil && !seen[sa]; sa = sa.Sub {
		seen[sa] = true
		if isOwnBounceSA(sa) {
			return true
		}
	}
	return false
}

// ownBounceStep reports a step whose posing ability is an own-bounce effect.
// It resolves the source the way digSourceForStep does: the stack snapshot at
// the decision's step is the authority (top object first, a trigger by its
// printed index, an activation by the preceding activate step), and the
// step's own cast/activate card is the fallback for a step whose stack the
// snapshot does not show.
func ownBounceStep(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, step int) bool {
	if reg == nil || step < 0 {
		return false
	}
	if step < len(snaps) {
		for _, o := range snaps[step].Stack {
			if o.Kind != "ability" {
				continue
			}
			if o.Trigger != "" {
				if idx, err := strconv.Atoi(o.Trigger); err == nil && ownBounceAtTrigger(reg, o.Source, idx) {
					return true
				}
				continue
			}
			for i := step - 1; i >= 0 && i < len(sc.Steps); i-- {
				st := sc.Steps[i]
				if st.Op == "activate" && st.Card == o.Source && st.AbilityIndex != nil {
					if ownBounceAtAbility(reg, st.Card, *st.AbilityIndex) {
						return true
					}
					break
				}
			}
		}
	}
	if step < len(sc.Steps) && sc.Steps[step].Card != "" {
		return ownBounceOnCard(reg, sc.Steps[step].Card)
	}
	return false
}

func ownBounceAtTrigger(reg *cards.Registry, ref string, idx int) bool {
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, ref) {
		if f != nil && idx >= 0 && idx < len(f.Triggers) && ownBounceInChain(f.Triggers[idx].Effect) {
			return true
		}
	}
	return false
}

func ownBounceAtAbility(reg *cards.Registry, ref string, idx int) bool {
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range digFaces(c, ref) {
		if f != nil && idx >= 0 && idx < len(f.Abilities) && ownBounceInChain(f.Abilities[idx]) {
			return true
		}
	}
	return false
}

func ownBounceOnCard(reg *cards.Registry, ref string) bool {
	c, ok := reg.Lookup(oraclediffRefName(ref))
	if !ok {
		return false
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, sa := range f.Abilities {
			if ownBounceInChain(sa) {
				return true
			}
		}
		for _, tr := range f.Triggers {
			if ownBounceInChain(tr.Effect) {
				return true
			}
		}
	}
	return false
}

// ownBounceShape is the decision shape of the pick: one mandatory hidden pick.
func ownBounceShape(d rules.OracleDecision) bool {
	return d.Resume == "hidden_pick" && d.Min == 1 && d.Max == 1 && len(d.Picks) == 1 && len(d.PickRefs) == 1
}

// ownBounceTargetAnswer is the target-queue answer for an own-bounce pick, or
// "" when decision i is not one. A same-name pick no exact spelling can name
// stays withheld (hiddenPickUnanswerable), as on the choice route.
func (r *answerRouting) ownBounceTargetAnswer(i int) string {
	d := r.ds[i]
	if !ownBounceShape(d) || !r.ownBounce[d.Step] || hiddenPickUnanswerable(d) {
		return ""
	}
	return targetPickValue(d, 0, oraclediffRefName(d.PickRefs[0]))
}

// optionalBounceChoiceAnswer is the CHOICE-queue answer for an own-bounce pick
// that follows its trigger's own "you may" confirmation (Tazeem Raptor's "you
// may return a land you control to its owner's hand": OptionalDecider$ on the
// trigger, the same mandatory ChangeZone body as Kor Skyfisher's). XMage's
// optional form asks the pick as an up-to TargetControlledPermanent through
// player.choose (TestPlayer.makeChoose, the CHOICE queue; measured "Missing
// CHOICE def ... Select up to one land you control" with the TARGET answer
// left over), where the mandatory form is chooseTarget. "" for any other
// decision.
func (r *answerRouting) optionalBounceChoiceAnswer(i int) string {
	d := r.ds[i]
	if i == 0 || !ownBounceShape(d) || !r.ownBounce[d.Step] || hiddenPickUnanswerable(d) {
		return ""
	}
	prev := r.ds[i-1]
	if !sameAsker(prev, d) || prev.Kind != "yesno" || prev.GorgeKind != "trigger_optional" ||
		prev.Resume != "optional" || pickKind(prev, 0) != "yes" {
		return ""
	}
	return disambiguatedObjectChoice(d, 0, oraclediffRefName(d.PickRefs[0]))
}
