package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Level-B observation of a continuous static that grants SPELLS a keyword
// whose rules text is a cast trigger (Prismari, the Inspiration's "Instant and
// sorcery spells you cast have storm", Quandrix, the Proof's cascade). The
// grant lands on a spell on the stack, which no permanent snapshot shows and
// whose keywords neither engine's stack entry carries; what the grant does is
// put the keyword's cast trigger on the stack above the spell (CR 702.40a
// storm, CR 702.85a cascade). The scenario casts the card in the level-A way,
// then casts a probe spell the grant's Affected$ filter accepts; the cast
// checkpoint shows an ability whose source is the probe. The control casts
// the same probe without the source and must show none, so the trigger is the
// grant's doing and not a printed one.

// grantedSpellTriggerHeads are the keyword heads whose grant this observation
// serves: each is a cast trigger that goes on the stack unconditionally (its
// copies or cascade happen when it resolves), so its presence alone observes
// the grant.
var grantedSpellTriggerHeads = []string{"Storm", "Cascade"}

// grantedSpellTriggerProbes are the probe spells, by the card type the
// grant's Affected$ filter names: a sorcery and an instant with no targets and
// no choices, so the cast poses nothing either engine must be told.
var grantedSpellTriggerProbes = []string{"Divination", "Accumulated Knowledge"}

// grantedSpellTriggerShape reports whether st is a stack-scoped grant of
// only keywords this observation serves.
func grantedSpellTriggerShape(st cards.Static) bool {
	if !strings.EqualFold(strings.TrimSpace(st.ParamStr(cards.PKAffectedZone)), "Stack") {
		return false
	}
	kws := st.ParamStr(cards.PKAddKeyword)
	if kws == "" {
		return false
	}
	for _, kw := range strings.Split(kws, " & ") {
		head := cards.KeywordHead(strings.TrimSpace(kw))
		found := false
		for _, h := range grantedSpellTriggerHeads {
			found = found || strings.EqualFold(head, h)
		}
		if !found {
			return false
		}
	}
	return true
}

// staticGrantedSpellTriggerItem serves the shape: the probe's cast trigger is
// on the stack with the source and absent without it.
func staticGrantedSpellTriggerItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !grantedSpellTriggerShape(st) {
		return oraclegen.Item{}, false
	}
	base, why := staticBase(reg, c, f, name, req, staticProbePlan{}, nil)
	if why != "" {
		return oraclegen.Item{}, false
	}
	for _, probe := range grantedSpellTriggerProbes {
		pc, ok := reg.Lookup(probe)
		if !ok || len(pc.Faces) == 0 {
			continue
		}
		pool, pwhy := oraclegen.PoolFor(pc.Faces[0].ManaCost)
		if pwhy != "" {
			continue
		}
		ref := "p0:" + probe
		want, none := true, false
		cast := oraclegen.Step{Op: "cast", Seat: 0, Card: ref, Mana: pool,
			Expect: []oraclegen.Expect{{TriggerOnStack: ref, Want: &want}}}
		with := base.Scenario
		with.Setup = withSpellInHand(with.Setup, probe, "")
		with.Steps = append(append([]oraclegen.Step(nil), base.Steps...), cast)
		res, ok := runStatic(reg, with)
		if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 || !triggerOnStack(res.Snapshots[len(res.Snapshots)-1], ref) {
			continue
		}
		control := cast
		control.Expect = []oraclegen.Expect{{TriggerOnStack: ref, Want: &none}}
		without := base.Scenario
		without.Setup = withSpellInHand(without.Setup, probe, name)
		without.Steps = []oraclegen.Step{control}
		if cres, cok := runStatic(reg, without); !cok || len(cres.Fails) != 0 {
			continue
		}
		it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, grantedSpellTriggerCR(st), with)
		it.XAnswers = oraclegen.XAnswersForScenario(res, with, oraclegen.ModeNumbers(f), nil)
		return it, true
	}
	return oraclegen.Item{}, false
}

// grantedSpellTriggerCR is the item's CR citation: the layer-6 grant and the
// granted keyword's trigger rule.
func grantedSpellTriggerCR(st cards.Static) []string {
	cr := []string{"611.3"}
	for _, kw := range strings.Split(st.ParamStr(cards.PKAddKeyword), " & ") {
		switch strings.ToLower(cards.KeywordHead(strings.TrimSpace(kw))) {
		case "storm":
			cr = append(cr, "702.40a")
		case "cascade":
			cr = append(cr, "702.85a")
		}
	}
	return cr
}
