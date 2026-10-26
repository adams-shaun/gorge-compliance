package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Level-B observation of a continuous static that grants SPELLS a keyword whose
// rules text is an optional additional cost that copies the spell when paid
// (Raiding Schemes' "Each noncreature spell you cast has conspire", Silverquill,
// the Disputant's "Each instant and sorcery spell you cast has casualty 1").
// The grant lands on a spell on the stack, which no snapshot shows, and the
// keyword does nothing unless its cost is paid; so the scenario casts a probe
// spell the grant's Affected$ filter accepts ELECTING the granted cost (the
// runner's cast_mode: "conspired" taps two creatures that share a colour with
// the spell, CR 702.78a; "casualty" sacrifices a creature with power N or
// greater, CR 702.153a). The cast checkpoint shows the copy trigger above the
// probe and the resolve checkpoint the copy's effect (the probe draws twice).
// The control casts the same probe plainly without the source and must show
// no trigger: the mode is the grant's, since the probe prints no keyword.

// grantedCopyCost is one served keyword: its head, the cast mode electing it
// and the CR rule the item cites.
type grantedCopyCost struct {
	head, mode, cr string
}

var grantedCopyCosts = []grantedCopyCost{
	{head: "Conspire", mode: "conspired", cr: "702.78a"},
	{head: "Casualty", mode: "casualty", cr: "702.153a"},
}

// Harmonize is a green sorcery with no target and no choice ("Draw three
// cards."), so the copy asks nothing either engine must be told, and Grizzly
// Bears are green creatures with power 2: two of them pay conspire (sharing
// the probe's green) and one pays casualty 1 or 2.
const (
	grantedCopyProbe   = "Harmonize"
	grantedCopyPayer   = "Grizzly Bears"
	grantedCopyPayerID = "p0:" + grantedCopyPayer
)

// grantedCopyCostOf is the served keyword st grants on the stack, if st is a
// stack-scoped grant of exactly one of them.
func grantedCopyCostOf(st cards.Static) (grantedCopyCost, bool) {
	if !strings.EqualFold(strings.TrimSpace(st.ParamStr(cards.PKAffectedZone)), "Stack") {
		return grantedCopyCost{}, false
	}
	kws := strings.Split(st.ParamStr(cards.PKAddKeyword), " & ")
	if len(kws) != 1 {
		return grantedCopyCost{}, false
	}
	head := cards.KeywordHead(strings.TrimSpace(kws[0]))
	for _, g := range grantedCopyCosts {
		if strings.EqualFold(head, g.head) {
			return g, true
		}
	}
	return grantedCopyCost{}, false
}

// withPayers adds n copies of the cost payer to p0's battlefield.
func withPayers(setup map[string]oraclegen.Seat, n int) map[string]oraclegen.Seat {
	out := make(map[string]oraclegen.Seat, len(setup))
	for k, v := range setup {
		out[k] = v
	}
	p0 := out["p0"]
	bf := append([]string(nil), p0.Battlefield...)
	for i := 0; i < n; i++ {
		bf = append(bf, grantedCopyPayer)
	}
	p0.Battlefield = bf
	out["p0"] = p0
	return out
}

// staticGrantedCopyCostItem serves the shape: the elected probe cast puts the
// copy trigger on the stack and the copy resolves, with the source; the plain
// probe cast shows no trigger without it.
func staticGrantedCopyCostItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	g, ok := grantedCopyCostOf(st)
	if !ok {
		return oraclegen.Item{}, false
	}
	pc, ok := reg.Lookup(grantedCopyProbe)
	if !ok || len(pc.Faces) == 0 {
		return oraclegen.Item{}, false
	}
	if _, ok := reg.Lookup(grantedCopyPayer); !ok {
		return oraclegen.Item{}, false
	}
	pool, why := oraclegen.PoolFor(pc.Faces[0].ManaCost)
	if why != "" {
		return oraclegen.Item{}, false
	}
	base, why := staticBase(reg, c, f, name, req, staticProbePlan{}, nil)
	if why != "" {
		return oraclegen.Item{}, false
	}
	payers := 2
	if g.mode == "casualty" {
		payers = 1
	}
	ref := "p0:" + grantedCopyProbe
	want, none := true, false
	// A casualty sacrifice is named on the cast step, as Bargain's is (the
	// runner answers gorge's sacrifice ask with it; the XMage driver pays the
	// same object). A forced conspire tap of exactly two creatures poses no
	// ask, so it carries none.
	var answers []oraclegen.Answer
	if g.mode == "casualty" {
		answers = []oraclegen.Answer{{Kind: "choose", Pick: []string{grantedCopyPayer}}}
	}
	for _, as := range [][]oraclegen.Answer{answers, nil} {
		cast := oraclegen.Step{Op: "cast", Seat: 0, Card: ref, Mana: pool, CastMode: g.mode, Answers: as,
			Expect: []oraclegen.Expect{{TriggerOnStack: ref, Want: &want}}}
		with := base.Scenario
		with.Setup = withPayers(withSpellInHand(with.Setup, grantedCopyProbe, ""), payers)
		with.Steps = append(append([]oraclegen.Step(nil), base.Steps...), cast, oraclegen.Step{Op: "resolve", Seat: 0})
		res, ok := runStatic(reg, with)
		if !ok || len(res.Fails) != 0 || len(res.Snapshots) < 2 ||
			!triggerOnStack(res.Snapshots[len(res.Snapshots)-2], ref) {
			continue
		}
		control := oraclegen.Step{Op: "cast", Seat: 0, Card: ref, Mana: pool,
			Expect: []oraclegen.Expect{{TriggerOnStack: ref, Want: &none}}}
		without := base.Scenario
		without.Setup = withPayers(withSpellInHand(without.Setup, grantedCopyProbe, name), payers)
		without.Steps = []oraclegen.Step{control}
		if cres, cok := runStatic(reg, without); !cok || len(cres.Fails) != 0 {
			continue
		}
		it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", g.cr}, with)
		it.XAnswers = oraclegen.XAnswersForScenario(reg, res, with, oraclegen.ModeNumbers(f), nil)
		return it, true
	}
	return oraclegen.Item{}, false
}
