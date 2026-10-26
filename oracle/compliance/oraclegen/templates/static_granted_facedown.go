package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Level-B observation of a static that grants an activated ability to the
// FACE-DOWN creatures its controller controls (Etrata, Deadly Fugitive:
// "Face-down creatures you control have '{2}{U}{B}: Turn this creature face
// up. If you can't, exile it, then you may cast the exiled card without
// paying its mana cost.'"). A face-down permanent is a 2/2 with no name and
// no abilities (CR 708.2a), so the grant is the only activated ability it has
// (CR 613.1f applies the layer-6 grant to the face-down object). Setup places
// every permanent face up, so the probe is cast face down for {3} (its
// Disguise cast, oraclegen.FaceDownProbe) and resolved; then the granted
// ability is activated on it and resolves, turning it face up -- a snapshot
// field both engines carry (face_down, and the probe's name revealed). The
// control is the same face-down cast without the source: the activation is
// not offered, so the step fails.

// staticGrantFaceDownItem serves the shape; ok is false for any other static.
func staticGrantFaceDownItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	affected := st.ParamStr(cards.PKAffected)
	if !st.HasParam(cards.PKAddAbility) || !strings.Contains(affected, "faceDown") || !strings.Contains(affected, "YouCtrl") {
		return oraclegen.Item{}, false
	}
	sa := cards.ResolveSVar(f.SVars, strings.TrimSpace(st.ParamStr(cards.PKAddAbility)))
	if sa == nil || sa.API == "Mana" || grantedLoyaltyCost(sa) >= 0 {
		return oraclegen.Item{}, false
	}
	pool, why := activationCostIn(sa.ParamStr(cards.PKCost), "battlefield", "")
	if why != "" {
		return oraclegen.Item{}, false
	}
	desc := strings.TrimSpace(sa.ParamStr(cards.PKSpellDescription))
	if desc == "" {
		return oraclegen.Item{}, false
	}
	xtext := grantedAbilityXMageCost(sa.ParamStr(cards.PKCost))
	if xtext == "" {
		return oraclegen.Item{}, false
	}
	probe := oraclegen.FaceDownProbe(reg, name)
	if probe == "" {
		return oraclegen.Item{}, false
	}
	ref := "p0:" + probe
	label := strings.SplitN(desc, ".", 2)[0]
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: ref, Mana: oraclegen.FaceDownCastPool, CastMode: "disguised"},
		{Op: "resolve"},
		{Op: "activate", Seat: 0, Card: ref, Mana: pool, Ability: label},
		{Op: "resolve"},
	}
	sc := staticScenario(f, name, []string{name}, []string{probe}, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	faceUp := false
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if p.Controller == 0 && p.Name == probe && !p.FaceDown {
			faceUp = true
		}
	}
	if !faceUp {
		return oraclegen.Item{}, false
	}
	control := staticScenario(f, name, nil, []string{probe}, steps)
	if cres, cok := runStatic(reg, control); cok && len(cres.Fails) == 0 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"613.1f", "708.2a", "702.168d"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, res, sc, oraclegen.ModeNumbers(f), nil)
	it.XAbility = []string{"", "", xtext, ""}
	return it, true
}

// grantedAbilityXMageCost is XMage's rule-text prefix for a granted
// ability's pure mana cost ("2 U B" -> "{2}{U}{B}"), or "" for a cost with a
// non-mana part the prefix cannot spell.
func grantedAbilityXMageCost(cost string) string {
	var b strings.Builder
	for _, tok := range strings.Fields(cost) {
		switch {
		case len(tok) == 1 && strings.Contains("WUBRGC", tok):
			b.WriteString("{" + tok + "}")
		case strings.Trim(tok, "0123456789") == "":
			b.WriteString("{" + tok + "}")
		default:
			return ""
		}
	}
	return b.String()
}
