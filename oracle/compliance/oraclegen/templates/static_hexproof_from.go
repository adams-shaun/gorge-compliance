package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// staticHexproofFromItem serves a grant of colour hexproof-from (Tam, Mindful
// First-Year's "Each other creature you control has hexproof from each of its
// colors"): a quality keyword outside both default vocabularies, which the
// opt-in CompareKeywordsHexproofFrom fold compares by colour
// (oracle/compliance/oraclediff/keywords_hexproof_from.go). The scenario is the
// ordinary probe scenario; it is served when a probe's folded keywords differ
// from its printed ones, and the item opts in to the fold.
func staticHexproofFromItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	kws := st.ParamStr(cards.PKAddKeyword)
	if kws == "" {
		return oraclegen.Item{}, false
	}
	for _, kw := range strings.Split(kws, " & ") {
		if !strings.HasPrefix(strings.TrimSpace(kw), "Hexproof:") {
			return oraclegen.Item{}, false
		}
	}
	base, why := staticBase(reg, c, f, name, req, staticProbePlan{}, nil)
	if why != "" {
		return oraclegen.Item{}, false
	}
	res, err := rules.RunOracleScenarioJSON(reg, base.Raw())
	if err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	snap := res.Snapshots[len(res.Snapshots)-1]
	observed := false
	for _, p := range snap.Permanents {
		if p.Token {
			continue
		}
		pc, ok := reg.Lookup(p.Name)
		if !ok || len(pc.Faces) == 0 {
			continue
		}
		printed := oraclediff.ComparedKeywordsHexproofFrom(pc.Faces[0].Keywords, p.Colors)
		if oraclediff.ComparedKeywordsHexproofFrom(p.Keywords, p.Colors) != printed {
			observed = true
		}
	}
	if !observed {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "613", "702.11d"}, base.Scenario)
	it.XAnswers = base.XAnswers
	it.Ignore = base.Ignore
	it.XAbility = base.XAbility
	it.Compare = []string{oraclediff.CompareKeywordsHexproofFrom}
	return it, true
}
