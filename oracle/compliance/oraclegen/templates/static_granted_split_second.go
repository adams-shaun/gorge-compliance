package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Level-B observation of a static that grants spells split second (Samut,
// Tyrant of Naktamun's "Instant and sorcery spells you control have split
// second"). CR 702.61a: "As long as this spell is on the stack, players can't
// cast other spells or activate abilities that aren't mana abilities." The
// grant shows in what its caster is offered while the granted spell is on
// the stack: the scenario casts the card in the level-A way, casts a probe
// instant, floats the mana for a second instant and asserts that second
// instant is NOT offered. The control replays the same steps without the
// source and asserts it IS offered, so the refusal is the grant's doing.

// splitSecondProbe is the granted spell (an instant with no targets and no
// choices) and splitSecondResponse the instant the caster would cast in
// response, with the mana that pays for it.
const (
	splitSecondProbe        = "Accumulated Knowledge"
	splitSecondResponse     = "Shock"
	splitSecondResponseMana = "R"
)

// staticSplitSecondItem serves a stack-scoped AddKeyword$ Split second grant.
func staticSplitSecondItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !strings.EqualFold(strings.TrimSpace(st.ParamStr(cards.PKAddKeyword)), "Split second") ||
		!strings.EqualFold(strings.TrimSpace(st.ParamStr(cards.PKAffectedZone)), "Stack") {
		return oraclegen.Item{}, false
	}
	affected := st.ParamStr(cards.PKAffected)
	if !strings.Contains(affected, "Instant") {
		return oraclegen.Item{}, false
	}
	pc, ok := reg.Lookup(splitSecondProbe)
	if !ok || len(pc.Faces) == 0 {
		return oraclegen.Item{}, false
	}
	if _, ok := reg.Lookup(splitSecondResponse); !ok {
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
	tail := func(want bool) []oraclegen.Step {
		return []oraclegen.Step{
			{Op: "cast", Seat: 0, Card: "p0:" + splitSecondProbe, Mana: pool},
			{Op: "mana", Seat: 0, Mana: splitSecondResponseMana},
			{Op: "pass_to", Seat: 0, Decision: "priority", Expect: []oraclegen.Expect{
				{Offered: &oraclegen.Offered{Seat: 0, Kind: offerKindCast, Card: "p0:" + splitSecondResponse}, Want: boolPtr(want)},
			}},
		}
	}
	setup := func(drop string) map[string]oraclegen.Seat {
		s := withSpellInHand(base.Scenario.Setup, splitSecondProbe, drop)
		return withSpellInHand(s, splitSecondResponse, "")
	}
	with := base.Scenario
	with.Setup = setup("")
	with.Steps = append(append([]oraclegen.Step(nil), base.Steps...), tail(false)...)
	res, ok := runStatic(reg, with)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	without := base.Scenario
	without.Setup = setup(name)
	without.Steps = tail(true)
	if cres, cok := runStatic(reg, without); !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "702.61a"}, with)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, res, with, oraclegen.ModeNumbers(f), nil)
	return it, true
}
