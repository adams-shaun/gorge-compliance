package templates

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// teamworkProbes are the teamwork spells the trigger.tapped-teamwork cause
// casts, tried in order. Beast Mode ({1}{G} instant, Teamwork 1: "Target
// creature gets +2/+2 and gains trample until end of turn. Also put a +1/+1
// counter on that creature if this spell was cast using teamwork.") has the
// lowest teamwork threshold, so any creature source of power 1 or more pays
// it alone, and one creature target.
var teamworkProbes = []string{"Beast Mode", "Team Tactics"}

// teamworkTriggerRecipe builds the cause for levelb.TeamworkTappedSub: p0
// casts a teamwork probe electing teamwork (the runner's cast_mode
// "teamworked", CR 702.194a's optional additional cost) and names the source
// as the creature tapped to pay it, so the source becomes tapped to pay a
// teamwork cost. The probe targets the source, the one creature p0 is sure to
// control. ok is false for any other sub-family.
func teamworkTriggerRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) ([]triggerCause, string, bool) {
	if sub != levelb.TeamworkTappedSub {
		return nil, "", false
	}
	if !f.IsCreature() {
		return nil, "teamwork tap needs a creature source", true
	}
	var causes []triggerCause
	for _, probe := range teamworkProbes {
		st, ok := castProbe(reg, probe, "p0:"+name)
		if !ok || probe == name {
			continue
		}
		st.CastMode = "teamworked"
		st.Answers = []oraclegen.Answer{{Kind: "choose", Pick: []string{name}}}
		causes = append(causes, triggerCause{hand: []string{probe}, steps: []oraclegen.Step{st}})
	}
	if len(causes) == 0 {
		return nil, "probe not in corpus", true
	}
	return causes, "", true
}
